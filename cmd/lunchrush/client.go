package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// client é um cliente HTTP fino sobre os serviços do dispatch. Não
// reimplementa retry, backoff ou pool de conexões: usa o http.Client
// padrão, que já resolve keep-alive e timeouts.
//
// A partir do tier 3, lifecycle/dispatch (delivery-api), ingestão de GPS
// (tracking-ingest) e leitura de tracking (tracking-projector) podem ser
// três hosts diferentes. Os campos de URL ficam vazios por padrão e caem
// de volta para baseURL, então o mesmo binário continua funcionando contra
// o monólito do tier 1/2 sem mudar nenhuma flag.
type client struct {
	baseURL      string
	trackingURL  string
	projectorURL string
	http         *http.Client
}

func newClient(baseURL, trackingURL, projectorURL string) *client {
	if trackingURL == "" {
		trackingURL = baseURL
	}
	if projectorURL == "" {
		projectorURL = baseURL
	}
	return &client{
		baseURL: baseURL, trackingURL: trackingURL, projectorURL: projectorURL,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *client) do(ctx context.Context, method, path string, headers map[string]string, body any) (int, []byte, error) {
	return c.doAt(ctx, c.baseURL, method, path, headers, body)
}

func (c *client) doAt(ctx context.Context, base, method, path string, headers map[string]string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, respBody, nil
}

type deliveryView struct {
	ID        string  `json:"id"`
	State     string  `json:"state"`
	CourierID *string `json:"courier_id,omitempty"`
}

func (c *client) createDelivery(ctx context.Context, caller, key string) (deliveryView, int, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/deliveries", map[string]string{
		"X-Caller":        caller,
		"Idempotency-Key": key,
	}, nil)
	if err != nil {
		return deliveryView{}, 0, err
	}
	if status != http.StatusCreated {
		return deliveryView{}, status, fmt.Errorf("criar entrega: status %d: %s", status, body)
	}
	var d deliveryView
	if err := json.Unmarshal(body, &d); err != nil {
		return deliveryView{}, status, err
	}
	return d, status, nil
}

func (c *client) markReady(ctx context.Context, id string) (int, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/deliveries/"+id+"/ready", nil, nil)
	if err != nil {
		return 0, err
	}
	if status != http.StatusNoContent {
		return status, fmt.Errorf("marcar pronta: status %d: %s", status, body)
	}
	return status, nil
}

func (c *client) offer(ctx context.Context, id string, ttlSeconds int) (int, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/deliveries/"+id+"/offer", nil, map[string]int{"ttl_seconds": ttlSeconds})
	if err != nil {
		return 0, err
	}
	if status != http.StatusNoContent {
		return status, fmt.Errorf("oferecer: status %d: %s", status, body)
	}
	return status, nil
}

func (c *client) decline(ctx context.Context, id string) (int, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/deliveries/"+id+"/decline", nil, nil)
	if err != nil {
		return 0, err
	}
	if status != http.StatusNoContent {
		return status, fmt.Errorf("recusar: status %d: %s", status, body)
	}
	return status, nil
}

func (c *client) assign(ctx context.Context, id, courierID string) (int, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/deliveries/"+id+"/assign", nil, map[string]string{"courier_id": courierID})
	if err != nil {
		return 0, err
	}
	if status != http.StatusNoContent {
		return status, fmt.Errorf("atribuir: status %d: %s", status, body)
	}
	return status, nil
}

func (c *client) pickUp(ctx context.Context, id string) (int, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/deliveries/"+id+"/pickup", nil, nil)
	if err != nil {
		return 0, err
	}
	if status != http.StatusNoContent {
		return status, fmt.Errorf("coletar: status %d: %s", status, body)
	}
	return status, nil
}

func (c *client) deliver(ctx context.Context, id string) (int, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/deliveries/"+id+"/deliver", nil, nil)
	if err != nil {
		return 0, err
	}
	if status != http.StatusNoContent {
		return status, fmt.Errorf("concluir: status %d: %s", status, body)
	}
	return status, nil
}

func (c *client) getDelivery(ctx context.Context, id string) (deliveryView, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/deliveries/"+id, nil, nil)
	if err != nil {
		return deliveryView{}, err
	}
	if status != http.StatusOK {
		return deliveryView{}, fmt.Errorf("consultar: status %d: %s", status, body)
	}
	var d deliveryView
	if err := json.Unmarshal(body, &d); err != nil {
		return deliveryView{}, err
	}
	return d, nil
}

func (c *client) registerCourier(ctx context.Context, name string) (string, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/couriers", nil, map[string]string{"name": name})
	if err != nil {
		return "", err
	}
	if status != http.StatusCreated {
		return "", fmt.Errorf("cadastrar entregador: status %d: %s", status, body)
	}
	var v struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", err
	}
	return v.ID, nil
}

func (c *client) issueToken(ctx context.Context, adminSecret, caller string) (string, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/auth/tokens", map[string]string{
		"X-Admin-Secret": adminSecret,
	}, map[string]string{"caller": caller})
	if err != nil {
		return "", err
	}
	if status != http.StatusCreated {
		return "", fmt.Errorf("emitir token: status %d: %s", status, body)
	}
	var v struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", err
	}
	return v.Token, nil
}

func (c *client) recordPosition(ctx context.Context, token, id string, epoch, sequence int, lat, lon float64) (bool, error) {
	status, body, err := c.doAt(ctx, c.trackingURL, http.MethodPost, "/deliveries/"+id+"/positions", map[string]string{
		"Authorization": "Bearer " + token,
	}, map[string]any{
		"tracking_session_epoch": epoch,
		"sequence":               sequence,
		"latitude":               lat,
		"longitude":              lon,
	})
	if err != nil {
		return false, err
	}
	if status != http.StatusAccepted {
		return false, fmt.Errorf("registrar posição: status %d: %s", status, body)
	}
	// tracking-ingest não devolve "current": ele só publica no Kafka, quem
	// sabe se a posição avançou a projeção é o tracking-projector. O
	// chamador confirma consultando currentPosition depois de um instante.
	return true, nil
}

func (c *client) currentPosition(ctx context.Context, token, id string) (status int, sequence int, err error) {
	status, body, err := c.doAt(ctx, c.projectorURL, http.MethodGet, "/deliveries/"+id+"/position", map[string]string{
		"Authorization": "Bearer " + token,
	}, nil)
	if err != nil || status != http.StatusOK {
		return status, 0, err
	}
	var v struct {
		Sequence int `json:"sequence"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return status, 0, err
	}
	return status, v.Sequence, nil
}

func (c *client) setAvailability(ctx context.Context, id string, available bool) error {
	status, body, err := c.do(ctx, http.MethodPost, "/couriers/"+id+"/availability", nil, map[string]bool{"available": available})
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return fmt.Errorf("disponibilizar entregador: status %d: %s", status, body)
	}
	return nil
}
