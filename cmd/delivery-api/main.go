// Comando delivery-api é o monólito modular do tier 1: lifecycle de entrega,
// cadastro de entregador e atribuição, tudo em um único binário sobre
// PostgreSQL.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/matheusgb/dispatch/internal/courier"
	"github.com/matheusgb/dispatch/internal/delivery"
	"github.com/matheusgb/dispatch/internal/dispatch"
	"github.com/matheusgb/dispatch/internal/notification"
	"github.com/matheusgb/dispatch/internal/platform/auth"
	"github.com/matheusgb/dispatch/internal/platform/db"
	"github.com/matheusgb/dispatch/internal/platform/httpapi"
	"github.com/matheusgb/dispatch/internal/platform/sse"
	"github.com/matheusgb/dispatch/internal/tracking"
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
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
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
	notificationProviderURL := os.Getenv("NOTIFICATION_PROVIDER_URL")
	if notificationProviderURL == "" {
		notificationProviderURL = "http://localhost:8090"
	}

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

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr, DialTimeout: 500 * time.Millisecond, MaxRetries: 1})
	defer rdb.Close()
	trackingRepo := tracking.NewRepository(pool)
	trackingCache := tracking.NewCache(trackingRepo, rdb, 5*time.Minute, logger)

	handler := httpapi.NewServer(httpapi.Deps{
		Deliveries: deliveries, Couriers: couriers, Dispatch: dispatchSvc,
		TrackingRepo: trackingRepo, TrackingCache: trackingCache, Broker: sse.NewBroker(),
		Issuer: auth.NewIssuer(jwtSecret, time.Hour), AdminSecret: adminSecret,
		Notifier: notification.NewClient(notificationProviderURL, logger), Logger: logger,
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go expireOffersLoop(ctx, dispatchSvc, logger)

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

// expireOffersLoop recicla ofertas vencidas periodicamente. No tier 3 isso
// se torna um worker próprio; aqui é uma goroutine do mesmo processo, com
// ciclo de vida ligado ao contexto de shutdown.
func expireOffersLoop(ctx context.Context, svc *dispatch.Service, logger *slog.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := svc.ExpireOverdueOffers(ctx)
			if err != nil {
				logger.Error("expirar ofertas vencidas", "error", err)
				continue
			}
			if n > 0 {
				logger.Info("ofertas expiradas recicladas", "count", n)
			}
		}
	}
}
