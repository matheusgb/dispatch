package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/matheusgb/dispatch/internal/delivery"
	"github.com/matheusgb/dispatch/internal/platform/auth"
	"github.com/matheusgb/dispatch/internal/tracking"
)

type issueTokenRequest struct {
	Caller string `json:"caller"`
}

// handleIssueToken é o substituto simplificado de um servidor OIDC: emite
// um token de curta duração para um caller, protegido por um segredo
// compartilhado. Não há fluxo de login de usuário porque não há usuário
// final no tier 2, só serviços e o LunchRush/k6 chamando em nome de um
// caller simulado.
func (s *Server) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	if s.adminSecret == "" || r.Header.Get("X-Admin-Secret") != s.adminSecret {
		writeError(w, http.StatusUnauthorized, "segredo administrativo inválido")
		return
	}
	var req issueTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Caller == "" {
		writeError(w, http.StatusUnprocessableEntity, "caller é obrigatório")
		return
	}
	token, err := s.issuer.IssueToken(req.Caller)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"token": token})
}

// authorizeOwner impede que um caller consulte o tracking de uma entrega
// que não é dele. Uma entrega sem created_by_caller (não deveria existir a
// partir do tier 2, mas pode existir por migração de dados antigos) nega
// por padrão: falhar fechado é a escolha aqui.
func (s *Server) authorizeOwner(w http.ResponseWriter, r *http.Request, deliveryID string) bool {
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

type recordPositionRequest struct {
	Epoch      int64     `json:"tracking_session_epoch"`
	Sequence   int64     `json:"sequence"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	AccuracyM  *float64  `json:"accuracy_m,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

func (s *Server) handleRecordPosition(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req recordPositionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "corpo inválido")
		return
	}
	if req.RecordedAt.IsZero() {
		req.RecordedAt = time.Now()
	}
	if req.Latitude < -90 || req.Latitude > 90 || req.Longitude < -180 || req.Longitude > 180 {
		writeError(w, http.StatusUnprocessableEntity, "coordenada fora do intervalo válido")
		return
	}

	current, err := s.trackingCache.RecordPosition(r.Context(), tracking.Position{
		DeliveryID: id, Epoch: req.Epoch, Sequence: req.Sequence,
		Latitude: req.Latitude, Longitude: req.Longitude, AccuracyM: req.AccuracyM, RecordedAt: req.RecordedAt,
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	positionsIngestedTotal.Inc()
	if current {
		positionsCurrentTotal.Inc()
		if b, err := json.Marshal(tracking.Position{
			DeliveryID: id, Epoch: req.Epoch, Sequence: req.Sequence,
			Latitude: req.Latitude, Longitude: req.Longitude, AccuracyM: req.AccuracyM, RecordedAt: req.RecordedAt,
		}); err == nil {
			s.broker.Publish(id, b)
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"current": current})
}

func (s *Server) handleCurrentPosition(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.authorizeOwner(w, r, id) {
		return
	}
	p, err := s.trackingCache.CurrentPosition(r.Context(), id)
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

func (s *Server) handlePositionHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.authorizeOwner(w, r, id) {
		return
	}
	history, err := s.trackingRepo.History(r.Context(), id, 50)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, history)
}

// handleStream é o caminho de tempo real: SSE com fallback de polling pelo
// próprio cliente via GET /deliveries/{id}/position se a conexão cair. É
// best-effort de propósito: uma atualização perdida não é reenviada.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.authorizeOwner(w, r, id) {
		return
	}

	// http.NewResponseController atravessa os wrappers de middleware (que
	// implementam Unwrap, ver statusRecorder em middleware.go) até chegar
	// no http.ResponseController real: é o jeito padrão, desde Go 1.20, de
	// alcançar Flush e SetWriteDeadline por baixo de camadas de
	// http.ResponseWriter empilhadas.
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
