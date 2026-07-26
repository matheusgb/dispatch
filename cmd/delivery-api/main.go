// Comando delivery-api é o serviço de lifecycle e dispatch: cadastro de
// entregador, criação idempotente de entrega, oferta, aceite, coleta e
// entrega. A partir do tier 3, tracking e notificação vivem em serviços
// próprios (tracking-ingest, tracking-projector, notification-worker),
// que reagem aos eventos publicados aqui pelo outbox relay.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/matheusgb/dispatch/internal/courier"
	"github.com/matheusgb/dispatch/internal/delivery"
	"github.com/matheusgb/dispatch/internal/dispatch"
	"github.com/matheusgb/dispatch/internal/platform/auth"
	"github.com/matheusgb/dispatch/internal/platform/db"
	"github.com/matheusgb/dispatch/internal/platform/httpapi"
	"github.com/matheusgb/dispatch/internal/platform/kafka"
	"github.com/matheusgb/dispatch/internal/platform/outbox"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("configuração inválida: DATABASE_URL não definido")
		os.Exit(1)
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	jwtSecret := os.Getenv("DISPATCH_JWT_SECRET")
	if jwtSecret == "" {
		logger.Error("configuração inválida: DISPATCH_JWT_SECRET não definido")
		os.Exit(1)
	}
	adminSecret := os.Getenv("DISPATCH_ADMIN_SECRET")
	if adminSecret == "" {
		logger.Error("configuração inválida: DISPATCH_ADMIN_SECRET não definido")
		os.Exit(1)
	}
	brokers := strings.Split(envOr("KAFKA_BROKERS", "localhost:19092"), ",")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	pool, err := db.Connect(connectCtx, dsn)
	cancel()
	if err != nil {
		logger.Error("conectar ao postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	deliveries := delivery.NewRepository(pool)
	couriers := courier.NewRepository(pool)
	dispatchSvc := dispatch.NewService(pool, dispatch.SystemClock{})

	producer := kafka.NewProducer(brokers)
	defer producer.Close()
	relay := outbox.NewRelay(pool, producer, logger)

	handler := httpapi.NewServer(httpapi.Deps{
		Deliveries: deliveries, Couriers: couriers, Dispatch: dispatchSvc,
		Issuer: auth.NewIssuer(jwtSecret, time.Hour), AdminSecret: adminSecret, Logger: logger,
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go outboxRelayLoop(ctx, relay, logger)

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("delivery-api ouvindo", "addr", addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("servidor encerrou com erro", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("encerrando graciosamente")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown forçado", "error", err)
		}
	}
}

// outboxRelayLoop publica eventos pendentes a cada segundo. Um ciclo que
// morre no meio (processo matado entre o ack do Kafka e o UPDATE que marca
// published_at) deixa o evento pendente para o próximo ciclo: é
// at-least-once por desenho, nunca perde um evento silenciosamente.
func outboxRelayLoop(ctx context.Context, relay *outbox.Relay, logger *slog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := relay.PublishPending(ctx, 100)
			if err != nil {
				logger.Error("publicar eventos pendentes do outbox", "error", err)
				continue
			}
			if n > 0 {
				logger.Info("eventos do outbox publicados", "count", n)
			}
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
