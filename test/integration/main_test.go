//go:build integration

// Package integration testa os repositórios e serviços contra um PostgreSQL
// real. Requer DATABASE_URL apontando para um banco já migrado (ver
// docker-compose.yml e cmd/migrate, ou `make test-integration`).
package integration

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
