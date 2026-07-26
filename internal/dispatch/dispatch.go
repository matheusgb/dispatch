// Package dispatch seleciona candidatos, oferece, expira e atribui entregas
// a entregadores. A exclusividade de atribuição (invariantes 1 e 2) é
// garantida pelo banco: um UPDATE condicional decide exatamente um vencedor
// entre tentativas concorrentes para a mesma entrega, e a constraint única
// de deliveries.courier_id impede que o mesmo entregador vença duas ofertas
// concorrentes ao mesmo tempo.
package dispatch

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matheusgb/dispatch/internal/delivery"
	"github.com/matheusgb/dispatch/internal/platform/outbox"
	"github.com/matheusgb/dispatch/internal/platform/topics"
)

var (
	// ErrNotOffered indica que a entrega não está em Offered no momento do
	// aceite: outra tentativa venceu primeiro, ou o estado já avançou.
	ErrNotOffered = errors.New("dispatch: entrega não está em oferta")
	// ErrCourierAlreadyActive indica que o entregador já tem outra entrega
	// ativa: a constraint única do banco rejeitou a atribuição.
	ErrCourierAlreadyActive = errors.New("dispatch: entregador já possui entrega ativa")
	// ErrNotReadyForDispatch indica que a entrega não estava disponível
	// para receber uma oferta.
	ErrNotReadyForDispatch = errors.New("dispatch: entrega não está pronta para despacho")
	// ErrNotCreated indica que a entrega não estava em created ao tentar
	// marcá-la como pronta para despacho.
	ErrNotCreated = errors.New("dispatch: entrega não está em created")
	// ErrUnexpectedState indica que a entrega não estava no estado de
	// origem esperado para a transição simples solicitada.
	ErrUnexpectedState = errors.New("dispatch: entrega não está no estado esperado para esta transição")
)

const uniqueViolation = "23505"

type Service struct {
	pool  *pgxpool.Pool
	clock Clock
}

func NewService(pool *pgxpool.Pool, clock Clock) *Service {
	return &Service{pool: pool, clock: clock}
}

// MarkReadyForDispatch move created -> ready_for_dispatch. No tier 1 isso é
// uma chamada explícita; a partir do tier 3 o dispatch-worker assume essa
// decisão sozinho, a partir de uma regra de seleção.
func (s *Service) MarkReadyForDispatch(ctx context.Context, deliveryID string) error {
	now := s.clock.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE deliveries SET state = $1, updated_at = $2 WHERE id = $3 AND state = $4
	`, delivery.ReadyForDispatch, now, deliveryID, delivery.Created)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotCreated
	}
	if err := recordTransition(ctx, tx, deliveryID, delivery.Created, delivery.ReadyForDispatch, now); err != nil {
		return err
	}
	if err := enqueueDeliveryEvent(ctx, tx, deliveryID, topics.KindDeliveryReadyForDispatch); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// enqueueDeliveryEvent grava o evento de lifecycle no outbox dentro da
// mesma transação da transição de estado (ver internal/platform/outbox):
// o evento e o efeito comitam juntos, ou nenhum dos dois.
func enqueueDeliveryEvent(ctx context.Context, tx pgx.Tx, deliveryID, kind string) error {
	_, err := outbox.Enqueue(ctx, tx, deliveryID, topics.DeliveryEvents, kind, map[string]string{
		"delivery_id": deliveryID,
	})
	return err
}

// Offer move ready_for_dispatch -> offered com um prazo de expiração. Só
// afeta a entrega se ela ainda estiver no estado esperado: uma segunda
// chamada concorrente para a mesma entrega não encontra linha para mudar.
func (s *Service) Offer(ctx context.Context, deliveryID string, ttl time.Duration) error {
	now := s.clock.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE deliveries
		SET state = $1, offered_at = $2, offer_expires_at = $3, updated_at = $2
		WHERE id = $4 AND state = $5
	`, delivery.Offered, now, now.Add(ttl), deliveryID, delivery.ReadyForDispatch)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotReadyForDispatch
	}
	if err := recordTransition(ctx, tx, deliveryID, delivery.ReadyForDispatch, delivery.Offered, now); err != nil {
		return err
	}
	if err := enqueueDeliveryEvent(ctx, tx, deliveryID, topics.KindDeliveryOffered); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Assign é o aceite: offered -> assigned para um courierID específico. É o
// ponto que o critério de conclusão do tier 1 testa com 20 tentativas
// concorrentes: exatamente uma vence.
func (s *Service) Assign(ctx context.Context, deliveryID, courierID string) error {
	now := s.clock.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE deliveries
		SET state = $1, courier_id = $2, updated_at = $3
		WHERE id = $4 AND state = $5
	`, delivery.Assigned, courierID, now, deliveryID, delivery.Offered)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return ErrCourierAlreadyActive
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotOffered
	}
	if err := recordTransition(ctx, tx, deliveryID, delivery.Offered, delivery.Assigned, now); err != nil {
		return err
	}
	if err := enqueueDeliveryEvent(ctx, tx, deliveryID, topics.KindDeliveryAssigned); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Decline devolve a entrega para ready_for_dispatch depois de uma recusa
// explícita do entregador.
func (s *Service) Decline(ctx context.Context, deliveryID string) error {
	return s.recycle(ctx, deliveryID, delivery.Declined)
}

// PickUp registra a coleta física da entrega: assigned -> picked_up.
func (s *Service) PickUp(ctx context.Context, deliveryID string) error {
	return s.simpleTransition(ctx, deliveryID, delivery.Assigned, delivery.PickedUp, topics.KindDeliveryPickedUp)
}

// Deliver conclui a entrega: picked_up -> delivered. O estado delivered não
// participa mais do índice único de courier ativo (ver migrations/0001), então
// o entregador volta a ficar livre para uma nova atribuição automaticamente.
func (s *Service) Deliver(ctx context.Context, deliveryID string) error {
	return s.simpleTransition(ctx, deliveryID, delivery.PickedUp, delivery.Delivered, topics.KindDeliveryDelivered)
}

func (s *Service) simpleTransition(ctx context.Context, deliveryID string, from, to delivery.State, eventKind string) error {
	now := s.clock.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE deliveries SET state = $1, updated_at = $2 WHERE id = $3 AND state = $4
	`, to, now, deliveryID, from)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUnexpectedState
	}
	if err := recordTransition(ctx, tx, deliveryID, from, to, now); err != nil {
		return err
	}
	if err := enqueueDeliveryEvent(ctx, tx, deliveryID, eventKind); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ExpireOverdueOffers move para expired, e em seguida para
// ready_for_dispatch, toda oferta cujo prazo já passou segundo o relógio
// injetado. Devolve quantas entregas foram recicladas.
func (s *Service) ExpireOverdueOffers(ctx context.Context) (int, error) {
	now := s.clock.Now()
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM deliveries
		WHERE state = $1 AND offer_expires_at IS NOT NULL AND offer_expires_at < $2
	`, delivery.Offered, now)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	count := 0
	for _, id := range ids {
		if err := s.recycle(ctx, id, delivery.Expired); err != nil {
			if errors.Is(err, ErrNotOffered) {
				continue // outra rotina já reciclou esta entrega.
			}
			return count, err
		}
		count++
	}
	return count, nil
}

// recycle move offered -> via (declined ou expired) -> ready_for_dispatch
// nas duas transições declaradas pelo grafo, dentro da mesma transação.
func (s *Service) recycle(ctx context.Context, deliveryID string, via delivery.State) error {
	now := s.clock.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE deliveries SET state = $1, updated_at = $2 WHERE id = $3 AND state = $4
	`, via, now, deliveryID, delivery.Offered)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotOffered
	}
	if err := recordTransition(ctx, tx, deliveryID, delivery.Offered, via, now); err != nil {
		return err
	}
	viaKind := topics.KindDeliveryDeclined
	if via == delivery.Expired {
		viaKind = topics.KindDeliveryExpired
	}
	if err := enqueueDeliveryEvent(ctx, tx, deliveryID, viaKind); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE deliveries
		SET state = $1, courier_id = NULL, offered_at = NULL, offer_expires_at = NULL, updated_at = $2
		WHERE id = $3
	`, delivery.ReadyForDispatch, now, deliveryID); err != nil {
		return err
	}
	if err := recordTransition(ctx, tx, deliveryID, via, delivery.ReadyForDispatch, now); err != nil {
		return err
	}
	if err := enqueueDeliveryEvent(ctx, tx, deliveryID, topics.KindDeliveryReadyForDispatch); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
