// Comando lunchrush-worker reage a eventos de lifecycle e decide sozinho
// quando uma entrega fica pronta para despacho e quando ela é oferecida,
// além de reciclar ofertas vencidas. É o mesmo lunchrush.Service do tier 1,
// só que acionado por evento em vez de por chamada HTTP manual.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/matheusgb/lunch-rush/internal/lunchrush"
	"github.com/matheusgb/lunch-rush/internal/platform/db"
	"github.com/matheusgb/lunch-rush/internal/platform/inbox"
	"github.com/matheusgb/lunch-rush/internal/platform/kafka"
	"github.com/matheusgb/lunch-rush/internal/platform/outbox"
	"github.com/matheusgb/lunch-rush/internal/platform/topics"
	"github.com/matheusgb/lunch-rush/internal/platform/workerhttp"

	"github.com/jackc/pgx/v5/pgxpool"
	kafkago "github.com/segmentio/kafka-go"
)

const consumerName = "lunchrush-worker"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("configuração inválida: DATABASE_URL não definido")
		os.Exit(1)
	}
	brokers := strings.Split(envOr("KAFKA_BROKERS", "localhost:19092"), ",")
	offerTTL := envDuration("OFFER_TTL_SECONDS", 120*time.Second)

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

	lunchrushSvc := lunchrush.NewService(pool, lunchrush.SystemClock{})
	dlq := kafka.NewProducer(brokers)
	defer dlq.Close()
	consumer := kafka.NewConsumer(brokers, topics.DeliveryEvents, consumerName, dlq, logger)
	defer consumer.Close()

	workerhttp.Serve(envOr("HTTP_ADDR", ":8080"), logger)
	go expireOffersLoop(ctx, lunchrushSvc, logger)

	logger.Info("lunchrush-worker consumindo", "topic", topics.DeliveryEvents, "offer_ttl", offerTTL)
	if err := consumer.Run(ctx, handler(pool, lunchrushSvc, offerTTL, logger)); err != nil && ctx.Err() == nil {
		logger.Error("consumer encerrou com erro", "error", err)
		os.Exit(1)
	}
}

func handler(pool *pgxpool.Pool, svc *lunchrush.Service, offerTTL time.Duration, logger *slog.Logger) kafka.Handler {
	return func(ctx context.Context, msg kafkago.Message) error {
		env, err := outbox.DecodeEnvelope(msg.Value)
		if err != nil {
			return err
		}

		var payload struct {
			DeliveryID string `json:"delivery_id"`
		}
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			return err
		}

		_, err = inbox.Once(ctx, pool, consumerName, env.EventID, func(ctx context.Context) error {
			switch env.Kind {
			case topics.KindDeliveryCreated:
				return svc.MarkReadyForLunchRush(ctx, payload.DeliveryID)
			case topics.KindDeliveryReadyForLunchRush:
				return svc.Offer(ctx, payload.DeliveryID, offerTTL)
			default:
				return nil // outros kinds não interessam a este consumidor.
			}
		})
		if err != nil {
			logger.Warn("processar evento de lifecycle", "kind", env.Kind, "delivery_id", payload.DeliveryID, "error", err)
		}
		return nil // erro de negócio não vai para a DLQ: é esperado (ex.: disputa).
	}
}

// expireOffersLoop recicla ofertas vencidas. Não é acionado por evento
// porque é baseado em tempo, não em uma mudança de estado que alguém
// publicou.
func expireOffersLoop(ctx context.Context, svc *lunchrush.Service, logger *slog.Logger) {
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

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	return def
}
