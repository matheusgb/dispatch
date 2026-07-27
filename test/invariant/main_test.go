//go:build integration

// Package invariant reúne, num único lugar rotulado, os invariantes de
// domínio centrais do dispatch (ver README.md "Invariantes já exigidas até
// este tier"). Não substitui test/integration/: aquele valida
// comportamento específico de repositório/serviço linha a linha; este
// pacote valida a propriedade de alto nível que o roadmap exige, mesmo que
// a implementação mude. Onde um teste equivalente já existe em
// test/integration/, o teste aqui comenta a relação em vez de duplicar a
// mesma asserção.
//
// Requer DATABASE_URL apontando para um banco já migrado, igual a
// test/integration/ (ver `make invariant-test`).
package invariant

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
			active_assignments, assignment_history, dispatch_fences, deliveries, couriers
	`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}
