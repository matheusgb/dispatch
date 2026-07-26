package delivery

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matheusgb/dispatch/internal/platform/idempotency"
)

// idempotencyTTL é o prazo em que uma repetição da mesma chave ainda é
// resolvida a partir do ledger, em vez de virar uma entrega nova.
const idempotencyTTL = 24 * time.Hour

type CreateRequest struct {
	Caller string
	Key    string
}

type CreateResponse struct {
	ID    string `json:"id"`
	State State  `json:"state"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create cria uma entrega respeitando a chave de idempotência: repetir a
// mesma chave com o mesmo payload devolve a entrega original (invariante 5);
// a mesma chave com payload diferente devolve idempotency.ErrConflict.
// replayed indica se a resposta veio do ledger em vez de uma inserção nova.
func (r *Repository) Create(ctx context.Context, req CreateRequest) (resp CreateResponse, replayed bool, err error) {
	payloadHash, err := idempotency.HashPayload(req)
	if err != nil {
		return CreateResponse{}, false, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return CreateResponse{}, false, err
	}
	defer tx.Rollback(ctx)

	key := idempotency.Key{Caller: req.Caller, Operation: "create_delivery", Value: req.Key}
	result, replayed, err := idempotency.Run(ctx, tx, key, payloadHash, idempotencyTTL, func(ctx context.Context) (idempotency.Result, error) {
		id := uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO deliveries (id, state, created_by_caller) VALUES ($1, $2, $3)
		`, id, Created, req.Caller); err != nil {
			return idempotency.Result{}, err
		}
		body, err := json.Marshal(CreateResponse{ID: id, State: Created})
		if err != nil {
			return idempotency.Result{}, err
		}
		return idempotency.Result{StatusCode: 201, Body: body}, nil
	})
	if err != nil {
		return CreateResponse{}, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return CreateResponse{}, false, err
	}

	if err := json.Unmarshal(result.Body, &resp); err != nil {
		return CreateResponse{}, false, err
	}
	return resp, replayed, nil
}

// Get devolve o estado atual de uma entrega.
func (r *Repository) Get(ctx context.Context, id string) (Delivery, error) {
	d := Delivery{ID: id}
	err := r.pool.QueryRow(ctx, `
		SELECT state, courier_id FROM deliveries WHERE id = $1
	`, id).Scan(&d.State, &d.CourierID)
	if err == pgx.ErrNoRows {
		return Delivery{}, ErrNotFound
	}
	if err != nil {
		return Delivery{}, err
	}
	return d, nil
}

// Owner devolve o caller que criou a entrega, usado pela autorização por
// recurso: só quem criou a entrega pode consultar o tracking dela.
func (r *Repository) Owner(ctx context.Context, id string) (string, error) {
	var owner *string
	err := r.pool.QueryRow(ctx, `SELECT created_by_caller FROM deliveries WHERE id = $1`, id).Scan(&owner)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if owner == nil {
		return "", nil
	}
	return *owner, nil
}
