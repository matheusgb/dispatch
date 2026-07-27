// Package courier cadastra entregadores e controla a disponibilidade que o
// lunchrush usa para selecionar candidatos.
package courier

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Courier struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Register cadastra um entregador como indisponível por padrão: ele precisa
// declarar disponibilidade explicitamente antes de receber ofertas.
func (r *Repository) Register(ctx context.Context, name string) (Courier, error) {
	c := Courier{ID: uuid.NewString(), Name: name, Available: false}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO couriers (id, name, available) VALUES ($1, $2, $3)
	`, c.ID, c.Name, c.Available)
	if err != nil {
		return Courier{}, err
	}
	return c, nil
}

// SetAvailability muda a disponibilidade do entregador. Não interfere em uma
// entrega já ativa: a exclusividade de atribuição é regra do lunchrush, não
// da disponibilidade declarada aqui.
func (r *Repository) SetAvailability(ctx context.Context, id string, available bool) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE couriers SET available = $1 WHERE id = $2
	`, available, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AvailableCandidates lista entregadores disponíveis e sem entrega ativa,
// usados pela seleção simples e determinística do tier 1: o mais antigo
// disponível primeiro.
func (r *Repository) AvailableCandidates(ctx context.Context, limit int) ([]Courier, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.name, c.available
		FROM couriers c
		WHERE c.available
		  AND NOT EXISTS (
		      SELECT 1 FROM deliveries d
		      WHERE d.courier_id = c.id AND d.state IN ('assigned', 'picked_up')
		  )
		ORDER BY c.created_at
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Courier
	for rows.Next() {
		var c Courier
		if err := rows.Scan(&c.ID, &c.Name, &c.Available); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
