// Sem build tag: este teste não precisa de Postgres nem de Kafka, roda em
// qualquer `go test ./...`. dispatch.tracking-positions não passa pela
// outbox (docs/adr/0007-tracking-nao-usa-outbox.md), é publicado direto
// pelo handler HTTP de cmd/tracking-ingest, que é package main e não
// exporta o tipo do payload — não dá para chamá-lo em processo como os
// outros contract tests deste pacote fazem com internal/dispatch e
// internal/fencing. Em vez de subir tracking-ingest inteiro com um broker
// Kafka real só para isto, este teste faz uma checagem estática: extrai as
// tags `json:"..."` do struct publicado em
// cmd/tracking-ingest/main.go (o literal anônimo dentro de
// handlePositions que vira o payload Kafka) e confere que batem
// exatamente com os `properties` de PositionPayload em
// contracts/asyncapi/dispatch-events.yaml. Pega o mesmo tipo de drift (um
// campo renomeado só de um dos dois lados) sem exigir infraestrutura viva.
package contract

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

var jsonTagRE = regexp.MustCompile(`json:"([a-zA-Z0-9_]+)`)

func TestContract_TrackingPositionWireFormatMatchesAsyncAPI(t *testing.T) {
	root := repoRoot(t)

	src, err := os.ReadFile(filepath.Join(root, "cmd/tracking-ingest/main.go"))
	if err != nil {
		t.Fatalf("ler cmd/tracking-ingest/main.go: %v", err)
	}

	// O literal publicado no Kafka (handlePositions, `body, err :=
	// json.Marshal(struct{...}{...})`) começa em "DeliveryID string" logo
	// após "body, err := json.Marshal(struct {" e termina no primeiro "}{".
	start := indexOf(t, string(src), "body, err := json.Marshal(struct {")
	end := indexOf(t, string(src)[start:], "}{") + start
	block := string(src)[start:end]

	matches := jsonTagRE.FindAllStringSubmatch(block, -1)
	if len(matches) == 0 {
		t.Fatalf("nenhuma tag json encontrada no literal publicado — cmd/tracking-ingest/main.go pode ter mudado de forma; atualize este teste e contracts/asyncapi/dispatch-events.yaml juntos")
	}
	var wireFields []string
	for _, m := range matches {
		wireFields = append(wireFields, m[1])
	}
	sort.Strings(wireFields)

	raw, err := os.ReadFile(filepath.Join(root, "contracts/asyncapi/dispatch-events.yaml"))
	if err != nil {
		t.Fatalf("ler AsyncAPI: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsear AsyncAPI: %v", err)
	}
	schema := doc["components"].(map[string]any)["schemas"].(map[string]any)["PositionPayload"].(map[string]any)
	props := schema["properties"].(map[string]any)
	var documentedFields []string
	for k := range props {
		documentedFields = append(documentedFields, k)
	}
	sort.Strings(documentedFields)

	if len(wireFields) != len(documentedFields) {
		t.Fatalf("contrato quebrado: campos publicados no wire %v (%d) != campos documentados em PositionPayload %v (%d)", wireFields, len(wireFields), documentedFields, len(documentedFields))
	}
	for i := range wireFields {
		if wireFields[i] != documentedFields[i] {
			t.Fatalf("contrato quebrado em [%d]: wire tem %q, AsyncAPI documenta %q (wire completo=%v, doc completo=%v)", i, wireFields[i], documentedFields[i], wireFields, documentedFields)
		}
	}
}

func indexOf(t *testing.T, s, substr string) int {
	t.Helper()
	i := indexOfPlain(s, substr)
	if i < 0 {
		t.Fatalf("não encontrei %q em cmd/tracking-ingest/main.go — arquivo mudou de forma, atualize este teste", substr)
	}
	return i
}

func indexOfPlain(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
