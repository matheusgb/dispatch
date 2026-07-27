//go:build integration

// Package integration testa os repositórios e serviços contra um PostgreSQL
// real. Requer DATABASE_URL apontando para um banco já migrado (ver
// docker-compose.yml e cmd/migrate, ou `make test-integration`).
//
// Importante: este pacote compartilha o mesmo banco (e as mesmas tabelas
// truncadas em cada teste) com test/invariant/ e test/contract/. Rodar
// `go test` com os três na mesma invocação (ex: `./test/...`) faz o Go
// testar os pacotes em paralelo por padrão, e um pacote truncando a tabela
// no meio do teste de outro produz falhas espúrias (constraint violation,
// "no rows", contagem errada) que não são bug de produto. Rode cada
// suite na sua própria invocação de `go test` (é o que `make
// test-integration`, `make invariant-test` e `make contract-test` já
// fazem) ou com `go test -p 1` se precisar rodar as três juntas.
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
