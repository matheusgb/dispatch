// Package notification envia notificações transacionais para o
// dependency-simulator. É deliberadamente best-effort e sem retry: o
// roadmap só introduz outbox, at-least-once e deduplicação no tier 3. Aqui
// o objetivo é só ter algo real para o chaos local degradar, não prometer
// uma garantia de entrega que este tier não tem.
package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type Event struct {
	DeliveryID string `json:"delivery_id"`
	Kind       string `json:"kind"`
}

type Client struct {
	baseURL string
	http    *http.Client
	logger  *slog.Logger
}

func NewClient(baseURL string, logger *slog.Logger) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 2 * time.Second}, logger: logger}
}

// Notify dispara uma chamada e não bloqueia quem chamou por mais que o
// timeout do cliente HTTP. Falha é só logada e contada; não há fila, não
// há reprocessamento.
func (c *Client) Notify(ctx context.Context, ev Event) {
	go func() {
		notifyCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		body, err := json.Marshal(ev)
		if err != nil {
			c.logger.Error("serializar notificação", "error", err)
			return
		}
		req, err := http.NewRequestWithContext(notifyCtx, http.MethodPost, c.baseURL+"/notifications", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			notificationsTotal.WithLabelValues(ev.Kind, "error").Inc()
			c.logger.Warn("notificação falhou", "kind", ev.Kind, "error", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 300 {
			notificationsTotal.WithLabelValues(ev.Kind, "rejected").Inc()
			c.logger.Warn("notificação rejeitada pelo provedor", "kind", ev.Kind, "status", resp.StatusCode)
			return
		}
		notificationsTotal.WithLabelValues(ev.Kind, "sent").Inc()
	}()
}
