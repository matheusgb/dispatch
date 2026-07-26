// Comando notification-worker consome eventos de lifecycle e dispara
// notificações transacionais para o dependency-simulator. Tira a
// notificação do caminho síncrono da requisição HTTP: o cliente que
// atribuiu ou concluiu uma entrega não espera o provedor externo
// responder.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/matheusgb/dispatch/internal/notification"
	"github.com/matheusgb/dispatch/internal/platform/db"
	"github.com/matheusgb/dispatch/internal/platform/inbox"
	"github.com/matheusgb/dispatch/internal/platform/kafka"
	"github.com/matheusgb/dispatch/internal/platform/outbox"
	"github.com/matheusgb/dispatch/internal/platform/topics"
	"github.com/matheusgb/dispatch/internal/platform/workerhttp"

	"github.com/jackc/pgx/v5/pgxpool"
	kafkago "github.com/segmentio/kafka-go"
)

const consumerName = "notification-worker"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("configuração inválida: DATABASE_URL não definido")
		os.Exit(1)
	}
	brokers := strings.Split(envOr("KAFKA_BROKERS", "localhost:19092"), ",")
	providerURL := envOr("NOTIFICATION_PROVIDER_URL", "http://localhost:8090")

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

	client := notification.NewClient(providerURL, logger)
	dlq := kafka.NewProducer(brokers)
	defer dlq.Close()
	consumer := kafka.NewConsumer(brokers, topics.DeliveryEvents, consumerName, dlq, logger)
	defer consumer.Close()

	workerhttp.Serve(envOr("HTTP_ADDR", ":8080"), logger)

	logger.Info("notification-worker consumindo", "topic", topics.DeliveryEvents)
	if err := consumer.Run(ctx, handler(pool, client, logger)); err != nil && ctx.Err() == nil {
		logger.Error("consumer encerrou com erro", "error", err)
		os.Exit(1)
	}
}

var kindToNotification = map[string]string{
	topics.KindDeliveryAssigned:  "assigned",
	topics.KindDeliveryDelivered: "delivered",
}

func handler(pool *pgxpool.Pool, client *notification.Client, logger *slog.Logger) kafka.Handler {
	return func(ctx context.Context, msg kafkago.Message) error {
		env, err := outbox.DecodeEnvelope(msg.Value)
		if err != nil {
			return err
		}
		notifKind, relevant := kindToNotification[env.Kind]
		if !relevant {
			return nil
		}

		var payload struct {
			DeliveryID string `json:"delivery_id"`
		}
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			return err
		}

		_, err = inbox.Once(ctx, pool, consumerName, env.EventID, func(ctx context.Context) error {
			client.Notify(ctx, notification.Event{DeliveryID: payload.DeliveryID, Kind: notifKind})
			return nil
		})
		if err != nil {
			logger.Warn("processar evento para notificação", "kind", env.Kind, "error", err)
		}
		return nil
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
