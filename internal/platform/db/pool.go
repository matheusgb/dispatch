// Package db centraliza a conexão com PostgreSQL usada por todos os
// repositórios do tier 1. Não há mais de um writer por tabela neste tier.
package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect abre um pool com timeout de contexto propagado pelo chamador.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
