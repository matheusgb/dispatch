package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/matheusgb/lunch-rush/internal/delivery"
	"github.com/matheusgb/lunch-rush/internal/platform/auth"
	"github.com/matheusgb/lunch-rush/internal/platform/ratelimit"
	"github.com/matheusgb/lunch-rush/internal/platform/sse"
	"github.com/matheusgb/lunch-rush/internal/tracking"
)

type server struct {
	deliveries *delivery.Repository
	repo       *tracking.Repository
	cache      *tracking.Cache
	broker     *sse.Broker
	logger     *slog.Logger
}

func newHTTPHandler(deliveries *delivery.Repository, repo *tracking.Repository, cache *tracking.Cache, broker *sse.Broker, issuer *auth.Issuer, logger *slog.Logger) http.Handler {
	s := &server{deliveries: deliveries, repo: repo, cache: cache, broker: broker, logger: logger}
	limiter := ratelimit.NewPerCaller(20, 40)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.Handle("GET /deliveries/{id}/position", issuer.Middleware(limiter.Middleware(http.HandlerFunc(s.handleCurrentPosition))))
	mux.Handle("GET /deliveries/{id}/positions", issuer.Middleware(limiter.Middleware(http.HandlerFunc(s.handlePositionHistory))))
	mux.Handle("GET /deliveries/{id}/stream", issuer.Middleware(limiter.Middleware(http.HandlerFunc(s.handleStream))))
	return mux
}

func (s *server) authorizeOwner(w http.ResponseWriter, r *http.Request, deliveryID string) bool {
	owner, err := s.deliveries.Owner(r.Context(), deliveryID)
	if err != nil {
		if errors.Is(err, delivery.ErrNotFound) {
			writeError(w, http.StatusNotFound, "entrega não encontrada")
			return false
		}
		s.internalError(w, err)
		return false
	}
	if owner == "" || owner != auth.Caller(r.Context()) {
		writeError(w, http.StatusForbidden, "sem acesso ao tracking desta entrega")
		return false
	}
	return true
}

func (s *server) handleCurrentPosition(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.authorizeOwner(w, r, id) {
		return
	}
	p, err := s.cache.CurrentPosition(r.Context(), id)
	if err != nil {
		if errors.Is(err, tracking.ErrNotFound) {
			writeError(w, http.StatusNotFound, "nenhuma posição registrada")
			return
		}
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *server) handlePositionHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.authorizeOwner(w, r, id) {
		return
	}
	history, err := s.repo.History(r.Context(), id, 50)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *server) handleStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.authorizeOwner(w, r, id) {
		return
	}

	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		s.internalError(w, errors.New("resposta não suporta streaming: "+err.Error()))
		return
	}

	ch, cancel := s.broker.Subscribe(id)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case data, open := <-ch:
			if !open {
				return
			}
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(data)
			_, _ = w.Write([]byte("\n\n"))
			_ = rc.Flush()
		}
	}
}

func (s *server) internalError(w http.ResponseWriter, err error) {
	s.logger.Error("erro interno", "error", err)
	writeError(w, http.StatusInternalServerError, "erro interno")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
