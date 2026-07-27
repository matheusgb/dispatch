package lunchrush

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/matheusgb/lunch-rush/internal/delivery"
)

// recordTransition grava a trilha de auditoria dentro da mesma transação da
// mudança de estado: uma transição confirmada sem o registro correspondente
// não é uma opção válida neste projeto.
func recordTransition(ctx context.Context, tx pgx.Tx, deliveryID string, from, to delivery.State, at time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO delivery_transitions (delivery_id, from_state, to_state, occurred_at)
		VALUES ($1, $2, $3, $4)
	`, deliveryID, from, to, at)
	return err
}
