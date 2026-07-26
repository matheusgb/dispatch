// Package inbox deduplica o processamento de eventos por consumidor
// (invariante 8): at-least-once na entrega do Kafka não pode virar
// at-least-once no efeito de negócio.
package inbox

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Once executa fn no máximo uma vez por (consumer, eventID), na mesma
// transação da marca de processado: ou os dois comitam, ou nenhum.
// processed indica se fn foi executada (false significa que já tinha sido
// processado antes, e fn foi pulada).
func Once(ctx context.Context, pool *pgxpool.Pool, consumer, eventID string, fn func(ctx context.Context) error) (processed bool, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var marked string
	err = tx.QueryRow(ctx, `
		INSERT INTO consumed_events (consumer, event_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
		RETURNING event_id
	`, consumer, eventID).Scan(&marked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // já processado por este consumidor.
		}
		return false, err
	}

	if err := fn(ctx); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
