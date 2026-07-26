// Package httpapi expõe o monólito modular do tier 1 por HTTP. Não há
// framework escondendo o ciclo da requisição: roteamento é o ServeMux da
// biblioteca padrão, timeouts e deadlines são explícitos.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/matheusgb/dispatch/internal/courier"
	"github.com/matheusgb/dispatch/internal/delivery"
	"github.com/matheusgb/dispatch/internal/dispatch"
	"github.com/matheusgb/dispatch/internal/platform/idempotency"
)

const requestTimeout = 5 * time.Second

type Server struct {
	deliveries *delivery.Repository
	couriers   *courier.Repository
	dispatch   *dispatch.Service
	logger     *slog.Logger
}

func NewServer(deliveries *delivery.Repository, couriers *courier.Repository, dispatchSvc *dispatch.Service, logger *slog.Logger) http.Handler {
	s := &Server{deliveries: deliveries, couriers: couriers, dispatch: dispatchSvc, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("POST /deliveries", s.handleCreateDelivery)
	mux.HandleFunc("GET /deliveries/{id}", s.handleGetDelivery)
	mux.HandleFunc("POST /deliveries/{id}/offer", s.handleOfferDelivery)
	mux.HandleFunc("POST /deliveries/{id}/assign", s.handleAssignDelivery)
	mux.HandleFunc("POST /deliveries/{id}/decline", s.handleDeclineDelivery)
	mux.HandleFunc("POST /couriers", s.handleRegisterCourier)
	mux.HandleFunc("POST /couriers/{id}/availability", s.handleSetAvailability)

	return chain(mux, requestID, withTimeout(requestTimeout), metricsMiddleware, logRequests(logger))
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	// No tier 1 a única dependência externa é o PostgreSQL, verificado pelo
	// pool subjacente no wiring de cmd/delivery-api; aqui apenas confirmamos
	// que o processo aceita tráfego.
	w.WriteHeader(http.StatusOK)
}

type createDeliveryRequest struct{}

func (s *Server) handleCreateDelivery(w http.ResponseWriter, r *http.Request) {
	caller := r.Header.Get("X-Caller")
	key := r.Header.Get("Idempotency-Key")
	if caller == "" || key == "" {
		writeError(w, http.StatusUnprocessableEntity, "X-Caller e Idempotency-Key são obrigatórios")
		return
	}

	resp, replayed, err := s.deliveries.Create(r.Context(), delivery.CreateRequest{Caller: caller, Key: key})
	if err != nil {
		if errors.Is(err, idempotency.ErrConflict) {
			writeError(w, http.StatusConflict, "chave de idempotência já usada com payload diferente")
			return
		}
		s.internalError(w, err)
		return
	}
	if !replayed {
		deliveriesCreatedTotal.Inc()
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleGetDelivery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := s.deliveries.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, delivery.ErrNotFound) {
			writeError(w, http.StatusNotFound, "entrega não encontrada")
			return
		}
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

const defaultOfferTTL = 2 * time.Minute

func (s *Server) handleOfferDelivery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.dispatch.Offer(r.Context(), id, defaultOfferTTL); err != nil {
		if errors.Is(err, dispatch.ErrNotReadyForDispatch) {
			writeError(w, http.StatusConflict, "entrega não está pronta para despacho")
			return
		}
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type assignRequest struct {
	CourierID string `json:"courier_id"`
}

func (s *Server) handleAssignDelivery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req assignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CourierID == "" {
		writeError(w, http.StatusUnprocessableEntity, "courier_id é obrigatório")
		return
	}

	err := s.dispatch.Assign(r.Context(), id, req.CourierID)
	switch {
	case err == nil:
		deliveriesAssignedTotal.Inc()
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, dispatch.ErrNotOffered), errors.Is(err, dispatch.ErrCourierAlreadyActive):
		assignmentConflictsTotal.Inc()
		writeError(w, http.StatusConflict, err.Error())
	default:
		s.internalError(w, err)
	}
}

func (s *Server) handleDeclineDelivery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.dispatch.Decline(r.Context(), id)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, dispatch.ErrNotOffered):
		writeError(w, http.StatusConflict, err.Error())
	default:
		s.internalError(w, err)
	}
}

type registerCourierRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleRegisterCourier(w http.ResponseWriter, r *http.Request) {
	var req registerCourierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, "name é obrigatório")
		return
	}
	c, err := s.couriers.Register(r.Context(), req.Name)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

type availabilityRequest struct {
	Available bool `json:"available"`
}

func (s *Server) handleSetAvailability(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req availabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "corpo inválido")
		return
	}
	err := s.couriers.SetAvailability(r.Context(), id, req.Available)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, courier.ErrNotFound):
		writeError(w, http.StatusNotFound, "entregador não encontrado")
	default:
		s.internalError(w, err)
	}
}

func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.logger.Error("erro interno", "error", err)
	writeError(w, http.StatusInternalServerError, "erro interno")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}
