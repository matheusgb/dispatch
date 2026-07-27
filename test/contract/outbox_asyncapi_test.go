//go:build integration

package contract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/matheusgb/lunch-rush/internal/courier"
	"github.com/matheusgb/lunch-rush/internal/delivery"
	"github.com/matheusgb/lunch-rush/internal/fencing"
	"github.com/matheusgb/lunch-rush/internal/lunchrush"
)

// asyncAPIRequiredFields carrega contracts/asyncapi/lunchrush-events.yaml e
// devolve os campos `required` do schema indicado, seguindo `allOf` quando
// necessário (as duas variantes de Envelope usadas neste repositório).
func asyncAPIRequiredFields(t *testing.T, schemaName string) []string {
	t.Helper()
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "contracts/asyncapi/lunchrush-events.yaml"))
	if err != nil {
		t.Fatalf("ler AsyncAPI: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsear AsyncAPI: %v", err)
	}

	components := doc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	schema, ok := schemas[schemaName].(map[string]any)
	if !ok {
		t.Fatalf("schema %q não encontrado em contracts/asyncapi/lunchrush-events.yaml", schemaName)
	}
	if req, ok := schema["required"].([]any); ok {
		return toStrings(req)
	}
	t.Fatalf("schema %q não tem campo required", schemaName)
	return nil
}

func toStrings(in []any) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = v.(string)
	}
	return out
}

func assertJSONHasExactKeys(t *testing.T, raw []byte, want []string) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decodificar payload real: %v (raw=%s)", err, raw)
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("contrato quebrado: payload real não tem o campo %q documentado em contracts/asyncapi/lunchrush-events.yaml (payload=%s)", k, raw)
		}
	}
	for k := range got {
		found := false
		for _, w := range want {
			if w == k {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("contrato quebrado: payload real tem o campo %q que não está documentado como required em contracts/asyncapi/lunchrush-events.yaml (payload=%s)", k, raw)
		}
	}
}

// TestContract_DeliveryLifecyclePayloadMatchesAsyncAPI cria uma entrega
// real e percorre o lifecycle via internal/lunchrush (o mesmo código que o
// delivery-api real usa), depois lê o payload de verdade gravado por
// internal/platform/outbox.Enqueue e confirma que bate exatamente com o
// schema DeliveryLifecyclePayload documentado.
func TestContract_DeliveryLifecyclePayloadMatchesAsyncAPI(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	want := asyncAPIRequiredFields(t, "DeliveryLifecyclePayload")

	deliveries := delivery.NewRepository(pool)
	created, _, err := deliveries.Create(ctx, delivery.CreateRequest{Caller: "order-service", Key: t.Name()})
	if err != nil {
		t.Fatalf("criar entrega: %v", err)
	}

	svc := lunchrush.NewService(pool, lunchrush.FixedClock{At: time.Now()})
	if err := svc.MarkReadyForLunchRush(ctx, created.ID); err != nil {
		t.Fatalf("ready_for_lunchrush: %v", err)
	}

	var payload []byte
	if err := pool.QueryRow(ctx, `
		SELECT payload FROM outbox_events WHERE aggregate_id = $1 AND kind = 'delivery.ready_for_lunchrush'
	`, created.ID).Scan(&payload); err != nil {
		t.Fatalf("buscar payload gravado no outbox: %v", err)
	}

	assertJSONHasExactKeys(t, payload, want)
}

// TestContract_AssignmentConfirmedPayloadMatchesAsyncAPI cria a fence, faz
// um CreateAssignment real (internal/fencing, tier 5) e confirma que o
// evento assignment.confirmed publicado bate com o schema documentado.
func TestContract_AssignmentConfirmedPayloadMatchesAsyncAPI(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	want := asyncAPIRequiredFields(t, "AssignmentConfirmedPayload")

	deliveries := delivery.NewRepository(pool)
	created, _, err := deliveries.Create(ctx, delivery.CreateRequest{Caller: "order-service", Key: t.Name()})
	if err != nil {
		t.Fatalf("criar entrega: %v", err)
	}

	couriers := courier.NewRepository(pool)
	c, err := couriers.Register(ctx, "entregador-contrato")
	if err != nil {
		t.Fatalf("cadastrar entregador: %v", err)
	}

	fsvc := fencing.NewService(pool, fencing.SystemClock{})
	fence, err := fsvc.Promote(ctx, "shard-contract-test", "region-a", time.Hour)
	if err != nil {
		t.Fatalf("promover fence: %v", err)
	}

	if _, err := fsvc.CreateAssignment(ctx, "shard-contract-test", "region-a", fence.Epoch, created.ID, c.ID, 1); err != nil {
		t.Fatalf("criar assignment: %v", err)
	}

	var payload []byte
	if err := pool.QueryRow(ctx, `
		SELECT payload FROM outbox_events WHERE aggregate_id = $1 AND kind = 'assignment.confirmed'
	`, created.ID).Scan(&payload); err != nil {
		t.Fatalf("buscar payload gravado no outbox: %v", err)
	}

	assertJSONHasExactKeys(t, payload, want)
}
