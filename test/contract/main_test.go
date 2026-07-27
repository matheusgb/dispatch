//go:build integration

// Package contract valida que o que o código realmente produz/expõe bate
// com o que está documentado em contracts/asyncapi/dispatch-events.yaml e
// api/openapi/openapi.yaml. Ao contrário de test/integration/ (comportamento
// interno) e test/invariant/ (propriedade de domínio), aqui a asserção é
// sempre "documentação == realidade".
//
// Requer DATABASE_URL apontando para um banco já migrado (ver
// `make contract-test`).
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
		dsn = "postgres://dispatch:dispatch@localhost:5432/dispatch?sslmode=disable"
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
			active_assignments, assignment_history, dispatch_fences, deliveries, couriers, outbox_events
	`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}
