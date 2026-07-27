//go:build e2e

// Package e2e percorre a jornada completa de uma entrega via HTTP real,
// contra os serviços de verdade subidos por `docker compose --profile app
// up -d --build` (ver README.md e Makefile `make e2e`). Ao contrário de
// test/integration/ (repositório contra Postgres) e test/contract/ (um
// serviço isolado via httptest), aqui delivery-api, tracking-ingest e
// tracking-projector são processos separados de verdade, falando entre si
// por rede Docker, exatamente como em produção.
//
// O cliente HTTP fino abaixo espelha de propósito o estilo de
// cmd/lunchrush/client.go (mesmos nomes de campo JSON, mesma forma de
// chamada); não importa aquele pacote porque cmd/lunchrush é package main
// e não exporta nada, e duplicar um cliente de ~80 linhas é mais barato e
// mais claro que promover um pacote inteiro só para isto.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type client struct {
	baseURL      string
	trackingURL  string
	projectorURL string
	http         *http.Client
}

func newClient() *client {
	return &client{
		baseURL:      env("BASE_URL", "http://localhost:8083"),
		trackingURL:  env("TRACKING_URL", "http://localhost:8084"),
		projectorURL: env("PROJECTOR_URL", "http://localhost:8085"),
		http:         &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *client) do(ctx context.Context, base, method, path string, headers map[string]string, body any) (int, []byte, error) {
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
	return resp.StatusCode, respBody, err
}

// TestE2E_FullDeliveryJourney cria uma entrega, percorre
// create -> ready -> offer -> assign -> pickup -> deliver via HTTP real
// contra o delivery-api do docker compose, e confirma o estado final tanto
// pela resposta HTTP quanto por uma nova consulta.
func TestE2E_FullDeliveryJourney(t *testing.T) {
	c := newClient()
	ctx := context.Background()

	waitHealthy(t, ctx, c, c.baseURL)

	status, body, err := c.do(ctx, c.baseURL, http.MethodPost, "/deliveries", map[string]string{
		"X-Caller":        "e2e-test",
		"Idempotency-Key": t.Name() + "-" + time.Now().Format(time.RFC3339Nano),
	}, map[string]string{})
	if err != nil {
		t.Fatalf("criar entrega: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("criar entrega: status %d: %s", status, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decodificar entrega criada: %v", err)
	}

	step := func(path string, wantStatus int) {
		t.Helper()
		status, body, err := c.do(ctx, c.baseURL, http.MethodPost, "/deliveries/"+created.ID+path, nil, nil)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if status != wantStatus {
			t.Fatalf("%s: status %d, esperava %d: %s", path, status, wantStatus, body)
		}
	}

	step("/ready", http.StatusNoContent)
	step("/offer", http.StatusNoContent)

	status, body, err = c.do(ctx, c.baseURL, http.MethodPost, "/couriers", nil, map[string]string{"name": "entregador-e2e"})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("cadastrar entregador: status %d err %v: %s", status, err, body)
	}
	var courier struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &courier)

	status, body, err = c.do(ctx, c.baseURL, http.MethodPost, "/couriers/"+courier.ID+"/availability", nil, map[string]bool{"available": true})
	if err != nil || status != http.StatusNoContent {
		t.Fatalf("disponibilizar entregador: status %d err %v: %s", status, err, body)
	}

	status, body, err = c.do(ctx, c.baseURL, http.MethodPost, "/deliveries/"+created.ID+"/assign", nil, map[string]string{"courier_id": courier.ID})
	if err != nil || status != http.StatusNoContent {
		t.Fatalf("aceitar oferta: status %d err %v: %s", status, err, body)
	}

	step("/pickup", http.StatusNoContent)
	step("/deliver", http.StatusNoContent)

	status, body, err = c.do(ctx, c.baseURL, http.MethodGet, "/deliveries/"+created.ID, nil, nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("consultar entrega final: status %d err %v: %s", status, err, body)
	}
	var final struct {
		State     string `json:"state"`
		CourierID string `json:"courier_id"`
	}
	if err := json.Unmarshal(body, &final); err != nil {
		t.Fatalf("decodificar entrega final: %v", err)
	}
	if final.State != "delivered" {
		t.Fatalf("estado final é %q, esperava delivered", final.State)
	}
	if final.CourierID != courier.ID {
		t.Fatalf("courier_id final é %q, esperava %q", final.CourierID, courier.ID)
	}
}

// TestE2E_TrackingJourney cria uma entrega, emite um token via
// delivery-api (identidade raiz, ver docs/adr/0005), publica uma posição
// de GPS no tracking-ingest e confirma que o tracking-projector projeta
// essa posição — os três serviços reais conversando por Kafka de verdade,
// não um mock.
func TestE2E_TrackingJourney(t *testing.T) {
	c := newClient()
	ctx := context.Background()
	adminSecret := env("ADMIN_SECRET", "compose-dev-admin-secret")

	waitHealthy(t, ctx, c, c.baseURL)
	waitHealthy(t, ctx, c, c.trackingURL)
	waitHealthy(t, ctx, c, c.projectorURL)

	status, body, err := c.do(ctx, c.baseURL, http.MethodPost, "/deliveries", map[string]string{
		"X-Caller":        "e2e-tracking",
		"Idempotency-Key": t.Name() + "-" + time.Now().Format(time.RFC3339Nano),
	}, map[string]string{})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("criar entrega: status %d err %v: %s", status, err, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &created)

	status, body, err = c.do(ctx, c.baseURL, http.MethodPost, "/auth/tokens", map[string]string{
		"X-Admin-Secret": adminSecret,
	}, map[string]string{"caller": "e2e-tracking"})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("emitir token: status %d err %v: %s", status, err, body)
	}
	var tok struct {
		Token string `json:"token"`
	}
	json.Unmarshal(body, &tok)
	auth := map[string]string{"Authorization": "Bearer " + tok.Token}

	status, body, err = c.do(ctx, c.trackingURL, http.MethodPost, "/deliveries/"+created.ID+"/positions", auth, map[string]any{
		"tracking_session_epoch": 1,
		"sequence":               1,
		"latitude":               -23.55,
		"longitude":              -46.63,
	})
	if err != nil || status != http.StatusAccepted {
		t.Fatalf("publicar posição: status %d err %v: %s", status, err, body)
	}

	deadline := time.Now().Add(15 * time.Second)
	var lastStatus int
	var lastBody []byte
	for time.Now().Before(deadline) {
		lastStatus, lastBody, err = c.do(ctx, c.projectorURL, http.MethodGet, "/deliveries/"+created.ID+"/position", auth, nil)
		if err == nil && lastStatus == http.StatusOK {
			var pos struct {
				Sequence int `json:"sequence"`
			}
			if err := json.Unmarshal(lastBody, &pos); err == nil && pos.Sequence == 1 {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("posição nunca apareceu projetada em %s: último status %d, body %s", c.projectorURL, lastStatus, lastBody)
}

func waitHealthy(t *testing.T, ctx context.Context, c *client, base string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		status, _, err := c.do(ctx, base, http.MethodGet, "/healthz", nil, nil)
		if err == nil && status == http.StatusOK {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("%s nunca ficou saudável: verifique `docker compose --profile app up -d --build`", base)
}
