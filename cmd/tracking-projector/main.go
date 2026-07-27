// Comando tracking-projector consome lunchrush.tracking-positions,
// atualiza PostgreSQL e Redis (internal/tracking), e serve a leitura:
// última posição, histórico e SSE. Também faz o papel do realtime-gateway
// do roadmap: não há evidência ainda de que separar os dois compense a
// complexidade de mais um deployable (ver ADR 0008).
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/matheusgb/lunch-rush/internal/delivery"
	"github.com/matheusgb/lunch-rush/internal/platform/auth"
	"github.com/matheusgb/lunch-rush/internal/platform/db"
	"github.com/matheusgb/lunch-rush/internal/platform/kafka"
	"github.com/matheusgb/lunch-rush/internal/platform/sse"
	"github.com/matheusgb/lunch-rush/internal/platform/topics"
	"github.com/matheusgb/lunch-rush/internal/tracking"

	kafkago "github.com/segmentio/kafka-go"
)

const consumerName = "tracking-projector"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("configuração inválida: DATABASE_URL não definido")
		os.Exit(1)
	}
	redisAddr := envOr("REDIS_ADDR", "localhost:6379")
	jwtSecret := os.Getenv("LUNCHRUSH_JWT_SECRET")
	if jwtSecret == "" {
		logger.Error("configuração inválida: LUNCHRUSH_JWT_SECRET não definido")
		os.Exit(1)
	}
	brokers := strings.Split(envOr("KAFKA_BROKERS", "localhost:19092"), ",")
	addr := envOr("HTTP_ADDR", ":8080")

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

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr, DialTimeout: 500 * time.Millisecond, MaxRetries: 1})
	defer rdb.Close()

	trackingRepo := tracking.NewRepository(pool)
	cache := tracking.NewCache(trackingRepo, rdb, 5*time.Minute, logger)
	deliveries := delivery.NewRepository(pool)
	broker := sse.NewBroker()

	dlq := kafka.NewProducer(brokers)
	defer dlq.Close()
	consumer := kafka.NewConsumer(brokers, topics.TrackingPositions, consumerName, dlq, logger)
	defer consumer.Close()

	go func() {
		logger.Info("tracking-projector consumindo", "topic", topics.TrackingPositions)
		if err := consumer.Run(ctx, handlePosition(cache, broker)); err != nil && ctx.Err() == nil {
			logger.Error("consumer encerrou com erro", "error", err)
		}
	}()

	srv := &http.Server{
		Addr:              addr,
		Handler:           newHTTPHandler(deliveries, trackingRepo, cache, broker, auth.NewIssuer(jwtSecret, time.Hour), logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("tracking-projector ouvindo", "addr", addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("servidor encerrou com erro", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
}

// handlePosition não usa internal/platform/inbox: a dedup de tracking já
// vem da unique constraint (delivery_id, epoch, sequence) em
// tracking_positions (ver internal/tracking/tracking.go), então um inbox
// separado seria redundante aqui.
func handlePosition(cache *tracking.Cache, broker *sse.Broker) kafka.Handler {
	return func(ctx context.Context, msg kafkago.Message) error {
		var payload struct {
			DeliveryID string    `json:"delivery_id"`
			Epoch      int64     `json:"tracking_session_epoch"`
			Sequence   int64     `json:"sequence"`
			Latitude   float64   `json:"latitude"`
			Longitude  float64   `json:"longitude"`
			AccuracyM  *float64  `json:"accuracy_m,omitempty"`
			RecordedAt time.Time `json:"recorded_at"`
		}
		if err := json.Unmarshal(msg.Value, &payload); err != nil {
			return err
		}

		p := tracking.Position{
			DeliveryID: payload.DeliveryID, Epoch: payload.Epoch, Sequence: payload.Sequence,
			Latitude: payload.Latitude, Longitude: payload.Longitude, AccuracyM: payload.AccuracyM, RecordedAt: payload.RecordedAt,
		}
		current, err := cache.RecordPosition(ctx, p)
		if err != nil {
			return err
		}
		if current {
			if b, err := json.Marshal(p); err == nil {
				broker.Publish(p.DeliveryID, b)
			}
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
