// Package tracking ingere posições de GPS e mantém a última posição
// conhecida por entrega. O histórico é append-only e tolerante a atraso; a
// última posição é monotônica por (tracking_session_epoch, sequence): uma
// posição atrasada nunca substitui uma mais recente (invariante 7).
package tracking

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound indica que a entrega não tem nenhuma posição registrada.
var ErrNotFound = errors.New("tracking: nenhuma posição registrada")

type Position struct {
	DeliveryID string    `json:"delivery_id"`
	Epoch      int64     `json:"tracking_session_epoch"`
	Sequence   int64     `json:"sequence"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	AccuracyM  *float64  `json:"accuracy_m,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// RecordPosition grava o evento no log append-only (idempotente por
// (delivery_id, epoch, sequence): reenviar o mesmo ponto não duplica a
// linha) e tenta avançar a projeção de última posição. current indica se
// esta posição passou a ser a mais recente conhecida.
func (r *Repository) RecordPosition(ctx context.Context, p Position) (current bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO tracking_positions
			(delivery_id, tracking_session_epoch, sequence, latitude, longitude, accuracy_m, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (delivery_id, tracking_session_epoch, sequence) DO NOTHING
	`, p.DeliveryID, p.Epoch, p.Sequence, p.Latitude, p.Longitude, p.AccuracyM, p.RecordedAt); err != nil {
		return false, err
	}

	var appliedID string
	err = tx.QueryRow(ctx, `
		INSERT INTO delivery_tracking_state
			(delivery_id, tracking_session_epoch, sequence, latitude, longitude, accuracy_m, recorded_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (delivery_id) DO UPDATE SET
			tracking_session_epoch = EXCLUDED.tracking_session_epoch,
			sequence                = EXCLUDED.sequence,
			latitude                = EXCLUDED.latitude,
			longitude               = EXCLUDED.longitude,
			accuracy_m              = EXCLUDED.accuracy_m,
			recorded_at             = EXCLUDED.recorded_at,
			updated_at              = now()
		WHERE (delivery_tracking_state.tracking_session_epoch, delivery_tracking_state.sequence)
		    < (EXCLUDED.tracking_session_epoch, EXCLUDED.sequence)
		RETURNING delivery_id
	`, p.DeliveryID, p.Epoch, p.Sequence, p.Latitude, p.Longitude, p.AccuracyM, p.RecordedAt).Scan(&appliedID)
	switch {
	case err == nil:
		current = true
	case errors.Is(err, pgx.ErrNoRows):
		current = false
	default:
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return current, nil
}

// CurrentPosition lê a projeção monotônica direto do PostgreSQL. É o
// caminho de fallback quando o Redis (ver internal/tracking/cache.go) está
// indisponível.
func (r *Repository) CurrentPosition(ctx context.Context, deliveryID string) (Position, error) {
	p := Position{DeliveryID: deliveryID}
	err := r.pool.QueryRow(ctx, `
		SELECT tracking_session_epoch, sequence, latitude, longitude, accuracy_m, recorded_at
		FROM delivery_tracking_state WHERE delivery_id = $1
	`, deliveryID).Scan(&p.Epoch, &p.Sequence, &p.Latitude, &p.Longitude, &p.AccuracyM, &p.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Position{}, ErrNotFound
	}
	if err != nil {
		return Position{}, err
	}
	return p, nil
}

// History devolve as últimas N posições aceitas no log, mais recente
// primeiro. Pode conter pontos fora de ordem: o log não filtra nada.
func (r *Repository) History(ctx context.Context, deliveryID string, limit int) ([]Position, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT tracking_session_epoch, sequence, latitude, longitude, accuracy_m, recorded_at
		FROM tracking_positions
		WHERE delivery_id = $1
		ORDER BY tracking_session_epoch DESC, sequence DESC
		LIMIT $2
	`, deliveryID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Position
	for rows.Next() {
		p := Position{DeliveryID: deliveryID}
		if err := rows.Scan(&p.Epoch, &p.Sequence, &p.Latitude, &p.Longitude, &p.AccuracyM, &p.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
