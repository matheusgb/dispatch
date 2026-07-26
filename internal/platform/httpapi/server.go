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
	"github.com/matheusgb/dispatch/internal/notification"
	"github.com/matheusgb/dispatch/internal/platform/auth"
	"github.com/matheusgb/dispatch/internal/platform/idempotency"
	"github.com/matheusgb/dispatch/internal/platform/ratelimit"
	"github.com/matheusgb/dispatch/internal/platform/sse"
	"github.com/matheusgb/dispatch/internal/tracking"
)

const requestTimeout = 5 * time.Second

type Server struct {
	deliveries    *delivery.Repository
	couriers      *courier.Repository
	dispatch      *dispatch.Service
	trackingRepo  *tracking.Repository
	trackingCache *tracking.Cache
	broker        *sse.Broker
	issuer        *auth.Issuer
	adminSecret   string
	notifier      *notification.Client
	logger        *slog.Logger
}

// Deps agrupa as dependências do tier 2 que não existiam no tier 1: o
// tracking (repositório + cache), o broker de SSE e a emissão de tokens.
// Nenhuma delas é opcional a partir daqui, mas manter um struct só para
// isso evita uma lista de parâmetros posicionais cada vez maior em
// NewServer.
type Deps struct {
	Deliveries    *delivery.Repository
	Couriers      *courier.Repository
	Dispatch      *dispatch.Service
	TrackingRepo  *tracking.Repository
	TrackingCache *tracking.Cache
	Broker        *sse.Broker
	Issuer        *auth.Issuer
	AdminSecret   string
	Notifier      *notification.Client
	Logger        *slog.Logger
}

func NewServer(d Deps) http.Handler {
	s := &Server{
		deliveries: d.Deliveries, couriers: d.Couriers, dispatch: d.Dispatch,
		trackingRepo: d.TrackingRepo, trackingCache: d.TrackingCache, broker: d.Broker,
		issuer: d.Issuer, adminSecret: d.AdminSecret, notifier: d.Notifier, logger: d.Logger,
	}
	limiter := ratelimit.NewPerCaller(20, 40)
	// timed é para tudo que fala com o banco e deve ter um deadline curto.
	timed := func(h http.HandlerFunc) http.Handler {
		return withTimeout(requestTimeout)(h)
	}
	// authed é para o tracking: exige token e aplica rate limit por caller,
	// além do deadline padrão.
	authed := func(h http.HandlerFunc) http.Handler {
		return chain(timed(h), s.issuer.Middleware, limiter.Middleware)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.Handle("POST /deliveries", timed(s.handleCreateDelivery))
	mux.Handle("GET /deliveries/{id}", timed(s.handleGetDelivery))
	mux.Handle("POST /deliveries/{id}/ready", timed(s.handleMarkReady))
	mux.Handle("POST /deliveries/{id}/offer", timed(s.handleOfferDelivery))
	mux.Handle("POST /deliveries/{id}/assign", timed(s.handleAssignDelivery))
	mux.Handle("POST /deliveries/{id}/decline", timed(s.handleDeclineDelivery))
	mux.Handle("POST /deliveries/{id}/pickup", timed(s.handlePickUpDelivery))
	mux.Handle("POST /deliveries/{id}/deliver", timed(s.handleDeliverDelivery))
	mux.Handle("POST /couriers", timed(s.handleRegisterCourier))
	mux.Handle("POST /couriers/{id}/availability", timed(s.handleSetAvailability))

	mux.HandleFunc("POST /auth/tokens", s.handleIssueToken)
	mux.Handle("POST /deliveries/{id}/positions", authed(s.handleRecordPosition))
	mux.Handle("GET /deliveries/{id}/position", authed(s.handleCurrentPosition))
	mux.Handle("GET /deliveries/{id}/positions", authed(s.handlePositionHistory))
	// /stream fica de fora de authed: é de longa duração e não pode herdar
	// o deadline curto de withTimeout. Usa auth e rate limit isoladamente.
	mux.Handle("GET /deliveries/{id}/stream", chain(s.issuer.Middleware(http.HandlerFunc(s.handleStream)), limiter.Middleware))

	return chain(mux, requestID, metricsMiddleware, logRequests(d.Logger))
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

func (s *Server) handleMarkReady(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.dispatch.MarkReadyForDispatch(r.Context(), id)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, dispatch.ErrNotCreated):
		writeError(w, http.StatusConflict, err.Error())
	default:
		s.internalError(w, err)
	}
}

const defaultOfferTTL = 2 * time.Minute

type offerRequest struct {
	TTLSeconds int `json:"ttl_seconds"`
}

func (s *Server) handleOfferDelivery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	ttl := defaultOfferTTL
	if r.ContentLength > 0 {
		var req offerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.TTLSeconds > 0 {
			ttl = time.Duration(req.TTLSeconds) * time.Second
		}
	}

	if err := s.dispatch.Offer(r.Context(), id, ttl); err != nil {
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
		s.notifier.Notify(r.Context(), notification.Event{DeliveryID: id, Kind: "assigned"})
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

func (s *Server) handlePickUpDelivery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.dispatch.PickUp(r.Context(), id)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, dispatch.ErrUnexpectedState):
		writeError(w, http.StatusConflict, err.Error())
	default:
		s.internalError(w, err)
	}
}

func (s *Server) handleDeliverDelivery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.dispatch.Deliver(r.Context(), id)
	switch {
	case err == nil:
		deliveriesCompletedTotal.Inc()
		s.notifier.Notify(r.Context(), notification.Event{DeliveryID: id, Kind: "delivered"})
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, dispatch.ErrUnexpectedState):
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
