//go:build integration

package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/matheusgb/dispatch/internal/courier"
	"github.com/matheusgb/dispatch/internal/delivery"
	"github.com/matheusgb/dispatch/internal/dispatch"
	"github.com/matheusgb/dispatch/internal/platform/auth"
	"github.com/matheusgb/dispatch/internal/platform/httpapi"
)

func openAPIPaths(t *testing.T) map[string][]string {
	t.Helper()
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "api/openapi/openapi.yaml"))
	if err != nil {
		t.Fatalf("ler OpenAPI: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsear OpenAPI: %v", err)
	}
	paths := doc["paths"].(map[string]any)
	out := map[string][]string{}
	for path, ops := range paths {
		opsMap := ops.(map[string]any)
		var methods []string
		for method := range opsMap {
			methods = append(methods, method)
		}
		sort.Strings(methods)
		out[path] = methods
	}
	return out
}

// TestContract_OpenAPIPathsMatchRegisteredRoutes é a checagem de paridade:
// todo path documentado em api/openapi/openapi.yaml precisa corresponder a
// uma rota de fato registrada em internal/platform/httpapi/server.go
// NewServer (linhas do mux.Handle), e vice-versa. A lista abaixo é mantida
// manualmente em sincronia com o mux (não há API pública do
// http.ServeMux para introspecção de padrões registrados); um path
// adicionado a um dos dois lados sem o outro quebra este teste.
func TestContract_OpenAPIPathsMatchRegisteredRoutes(t *testing.T) {
	registered := map[string][]string{
		"/healthz":                    {"get"},
		"/readyz":                     {"get"},
		"/deliveries":                 {"post"},
		"/deliveries/{id}":            {"get"},
		"/deliveries/{id}/ready":      {"post"},
		"/deliveries/{id}/offer":      {"post"},
		"/deliveries/{id}/assign":     {"post"},
		"/deliveries/{id}/decline":    {"post"},
		"/deliveries/{id}/pickup":     {"post"},
		"/deliveries/{id}/deliver":    {"post"},
		"/couriers":                   {"post"},
		"/couriers/{id}/availability": {"post"},
		"/auth/tokens":                {"post"},
	}
	documented := openAPIPaths(t)
	delete(documented, "/healthz")
	delete(documented, "/readyz")
	delete(registered, "/healthz")
	delete(registered, "/readyz")
	// /metrics é exposto (promhttp.Handler) mas deliberadamente fora do
	// OpenAPI: é infraestrutura de observabilidade, não contrato de
	// negócio.

	for path, methods := range registered {
		got, ok := documented[path]
		if !ok {
			t.Errorf("rota registrada %s não está documentada em api/openapi/openapi.yaml", path)
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(methods) {
			t.Errorf("rota %s: métodos documentados %v != métodos registrados %v", path, got, methods)
		}
	}
	for path := range documented {
		if _, ok := registered[path]; !ok {
			t.Errorf("path documentado %s não corresponde a nenhuma rota registrada conhecida em internal/platform/httpapi/server.go", path)
		}
	}
}

// TestContract_HTTPLifecycleMatchesDocumentedStatusCodes sobe o
// delivery-api real (mesmo internal/platform/httpapi.NewServer usado por
// cmd/delivery-api) contra Postgres real e percorre create -> ready ->
// offer -> assign -> decline (conflito) -> pickup -> deliver -> get,
// conferindo que cada resposta tem exatamente o status code documentado em
// api/openapi/openapi.yaml para aquele endpoint.
func TestContract_HTTPLifecycleMatchesDocumentedStatusCodes(t *testing.T) {
	truncateAll(t)

	srv := httpapi.NewServer(httpapi.Deps{
		Deliveries: delivery.NewRepository(pool),
		Couriers:   courier.NewRepository(pool),
		Dispatch:   dispatch.NewService(pool, dispatch.FixedClock{At: time.Now()}),
		Issuer:     auth.NewIssuer("contract-test-secret", time.Hour),
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := ts.Client()

	// POST /deliveries -> 201, documentado.
	createReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/deliveries", bytes.NewReader([]byte(`{}`)))
	createReq.Header.Set("X-Caller", "contract-test")
	createReq.Header.Set("Idempotency-Key", t.Name())
	resp := do(t, client, createReq)
	assertStatus(t, "POST /deliveries", resp, http.StatusCreated)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decodificar resposta de criação: %v", err)
	}
	resp.Body.Close()

	// POST /deliveries/{id}/ready -> 204, documentado.
	resp = do(t, client, mustReq(t, http.MethodPost, ts.URL+"/deliveries/"+created.ID+"/ready", nil))
	assertStatus(t, "POST /deliveries/{id}/ready", resp, http.StatusNoContent)

	// POST /deliveries/{id}/offer -> 204, documentado.
	resp = do(t, client, mustReq(t, http.MethodPost, ts.URL+"/deliveries/"+created.ID+"/offer", nil))
	assertStatus(t, "POST /deliveries/{id}/offer", resp, http.StatusNoContent)

	// POST /couriers -> 201, documentado.
	resp = do(t, client, mustReq(t, http.MethodPost, ts.URL+"/couriers", []byte(`{"name":"contract-courier"}`)))
	assertStatus(t, "POST /couriers", resp, http.StatusCreated)
	var c struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&c)
	resp.Body.Close()

	// POST /couriers/{id}/availability -> 204, documentado.
	resp = do(t, client, mustReq(t, http.MethodPost, ts.URL+"/couriers/"+c.ID+"/availability", []byte(`{"available":true}`)))
	assertStatus(t, "POST /couriers/{id}/availability", resp, http.StatusNoContent)

	// POST /deliveries/{id}/assign -> 204, documentado.
	assignBody, _ := json.Marshal(map[string]string{"courier_id": c.ID})
	resp = do(t, client, mustReq(t, http.MethodPost, ts.URL+"/deliveries/"+created.ID+"/assign", assignBody))
	assertStatus(t, "POST /deliveries/{id}/assign", resp, http.StatusNoContent)

	// POST /deliveries/{id}/decline numa entrega já assigned -> 409, documentado
	// ("entrega não estava em oferta").
	resp = do(t, client, mustReq(t, http.MethodPost, ts.URL+"/deliveries/"+created.ID+"/decline", nil))
	assertStatus(t, "POST /deliveries/{id}/decline (conflito)", resp, http.StatusConflict)

	// POST /deliveries/{id}/pickup -> 204, documentado.
	resp = do(t, client, mustReq(t, http.MethodPost, ts.URL+"/deliveries/"+created.ID+"/pickup", nil))
	assertStatus(t, "POST /deliveries/{id}/pickup", resp, http.StatusNoContent)

	// POST /deliveries/{id}/deliver -> 204, documentado.
	resp = do(t, client, mustReq(t, http.MethodPost, ts.URL+"/deliveries/"+created.ID+"/deliver", nil))
	assertStatus(t, "POST /deliveries/{id}/deliver", resp, http.StatusNoContent)

	// GET /deliveries/{id} -> 200, documentado, estado final delivered.
	resp = do(t, client, mustReq(t, http.MethodGet, ts.URL+"/deliveries/"+created.ID, nil))
	assertStatus(t, "GET /deliveries/{id}", resp, http.StatusOK)
	var got struct {
		State string `json:"state"`
	}
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.State != "delivered" {
		t.Fatalf("estado final via HTTP é %q, esperava delivered", got.State)
	}

	// GET /deliveries/{id} de um id inexistente -> 404, documentado.
	resp = do(t, client, mustReq(t, http.MethodGet, ts.URL+"/deliveries/00000000-0000-0000-0000-000000000000", nil))
	assertStatus(t, "GET /deliveries/{id} (404)", resp, http.StatusNotFound)
}

func mustReq(t *testing.T, method, url string, body []byte) *http.Request {
	t.Helper()
	var r *bytes.Reader
	if body != nil {
		r = bytes.NewReader(body)
	} else {
		r = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, url, r)
	if err != nil {
		t.Fatalf("montar requisição %s %s: %v", method, url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

func do(t *testing.T, client *http.Client, req *http.Request) *http.Response {
	t.Helper()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	return resp
}

func assertStatus(t *testing.T, label string, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Errorf("%s: status %d, esperava %d (documentado em api/openapi/openapi.yaml)", label, resp.StatusCode, want)
	}
}
