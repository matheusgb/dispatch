//go:build integration

// Package contract valida que o que o código realmente produz/expõe bate
// com o que está documentado em contracts/asyncapi/lunchrush-events.yaml e
// api/openapi/openapi.yaml. Ao contrário de test/integration/ (comportamento
// interno) e test/invariant/ (propriedade de domínio), aqui a asserção é
// sempre "documentação == realidade".
//
// Requer DATABASE_URL apontando para um banco já migrado (ver
// `make contract-test`).
//
// Compartilha banco/tabelas com test/integration/ e test/invariant/: não
// rode os três na mesma invocação de `go test` sem `-p 1`, ver o
// comentário equivalente em test/integration/main_test.go.
package contract

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://lunchrush:lunchrush@localhost:5432/lunchrush?sslmode=disable"
	}

	ctx := context.Background()
	var err error
	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	os.Exit(m.Run())
}

func truncateAll(t *testing.T) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		TRUNCATE idempotency_keys, delivery_transitions, tracking_positions, delivery_tracking_state,
			active_assignments, assignment_history, lunchrush_fences, deliveries, couriers, outbox_events
	`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}
