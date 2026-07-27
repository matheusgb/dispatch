// Comando tracking-ingest só aceita GPS e publica no Kafka: não escreve no
// PostgreSQL. O endpoint só confirma sucesso ao cliente depois do ack
// durável do broker (RequiredAcks=all no producer). É o caminho de escrita
// mais quente do tracking, isolado para escalar e falhar
// independentemente do resto do sistema.
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

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/matheusgb/lunch-rush/internal/platform/auth"
	"github.com/matheusgb/lunch-rush/internal/platform/kafka"
	"github.com/matheusgb/lunch-rush/internal/platform/ratelimit"
	"github.com/matheusgb/lunch-rush/internal/platform/topics"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	addr := envOr("HTTP_ADDR", ":8080")
	jwtSecret := os.Getenv("LUNCHRUSH_JWT_SECRET")
	if jwtSecret == "" {
		logger.Error("configuração inválida: LUNCHRUSH_JWT_SECRET não definido")
		os.Exit(1)
	}
	brokers := strings.Split(envOr("KAFKA_BROKERS", "localhost:19092"), ",")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	producer := kafka.NewProducer(brokers)
	defer producer.Close()

	issuer := auth.NewIssuer(jwtSecret, time.Hour)
	limiter := ratelimit.NewPerCaller(20, 40)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.Handle("POST /deliveries/{id}/positions",
		issuer.Middleware(limiter.Middleware(http.HandlerFunc(handlePositions(producer, logger)))))

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("tracking-ingest ouvindo", "addr", addr)
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

type positionRequest struct {
	Epoch      int64     `json:"tracking_session_epoch"`
	Sequence   int64     `json:"sequence"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	AccuracyM  *float64  `json:"accuracy_m,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

func handlePositions(producer *kafka.Producer, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req positionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"corpo inválido"}`, http.StatusUnprocessableEntity)
			return
		}
		if req.Latitude < -90 || req.Latitude > 90 || req.Longitude < -180 || req.Longitude > 180 {
			http.Error(w, `{"error":"coordenada fora do intervalo válido"}`, http.StatusUnprocessableEntity)
			return
		}
		if req.RecordedAt.IsZero() {
			req.RecordedAt = time.Now()
		}

		body, err := json.Marshal(struct {
			DeliveryID string    `json:"delivery_id"`
			Epoch      int64     `json:"tracking_session_epoch"`
			Sequence   int64     `json:"sequence"`
			Latitude   float64   `json:"latitude"`
			Longitude  float64   `json:"longitude"`
			AccuracyM  *float64  `json:"accuracy_m,omitempty"`
			RecordedAt time.Time `json:"recorded_at"`
		}{id, req.Epoch, req.Sequence, req.Latitude, req.Longitude, req.AccuracyM, req.RecordedAt})
		if err != nil {
			http.Error(w, `{"error":"erro interno"}`, http.StatusInternalServerError)
			return
		}

		// Confirma ao cliente só depois do ack durável do Kafka: se o
		// broker não puder confirmar, a requisição falha explicitamente e
		// o cliente retenta com o mesmo (epoch, sequence), que é
		// idempotente do outro lado (ver internal/tracking).
		if err := producer.Publish(r.Context(), topics.TrackingPositions, id, body); err != nil {
			logger.Error("publicar posição", "delivery_id", id, "error", err)
			http.Error(w, `{"error":"não foi possível confirmar a posição no broker"}`, http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
