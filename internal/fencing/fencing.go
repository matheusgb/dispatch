// Package fencing implementa a autoridade de ownership do tier 5: um
// dispatch shard só aceita escrita do writer que tem o epoch vigente,
// dentro da lease vigente. É a mesma família de proteção que
// internal/dispatch já usa desde o tier 1 (UPDATE condicional + unique
// constraint), estendida com epoch e lease para o caso multi-célula, onde
// mais de um writer (região/célula) pode achar que é dono ao mesmo tempo.
//
// O protocolo implementado aqui é o mesmo especificado formalmente em
// docs/tla/DispatchFencing.tla: Promote incrementa o epoch só depois da
// lease expirar (LeaseExpire -> Promote no modelo), CreateAssignment exige
// o epoch vigente na mesma transação do INSERT (Assign no modelo, com a
// guarda "e = epoch" que o mutation test provou ser necessária), e Finish
// move o assignment para o histórico. Ver docs/adr/0018 para o mapeamento
// completo variável-por-variável entre o modelo e este código.
package fencing

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matheusgb/dispatch/internal/platform/outbox"
	"github.com/matheusgb/dispatch/internal/platform/topics"
)

var (
	// ErrStaleFence indica que o epoch/owner_region enviado pelo writer não
	// bate com o que a autoridade tem agora: outro writer já promoveu, ou a
	// lease deste writer já não é a vigente. É a rejeição do fencing token
	// antigo que docs/tla/DispatchFencing.tla prova ser necessária.
	ErrStaleFence = errors.New("fencing: epoch ou owner_region desatualizado")
	// ErrLeaseNotExpired indica que Promote foi chamado com a lease atual
	// ainda válida: só é seguro promover depois que a lease anterior
	// expirou (senão dois writers poderiam achar que são donos ao mesmo
	// tempo, exatamente o que o protocolo impede).
	ErrLeaseNotExpired = errors.New("fencing: lease atual ainda não expirou")
	// ErrDeliveryAlreadyAssigned indica que a entrega já tem um assignment
	// ativo (unique constraint em active_assignments.delivery_id).
	ErrDeliveryAlreadyAssigned = errors.New("fencing: entrega já possui assignment ativo")
	// ErrCourierAlreadyActive indica que o courier já tem um assignment
	// ativo (unique constraint em active_assignments.courier_id).
	ErrCourierAlreadyActive = errors.New("fencing: courier já possui assignment ativo")
	// ErrAssignmentNotFound indica que Finish não encontrou assignment
	// ativo para a entrega informada.
	ErrAssignmentNotFound = errors.New("fencing: nenhum assignment ativo para esta entrega")
)

const uniqueViolation = "23505"

// Clock é a mesma abstração de internal/dispatch: testes e o LunchRush
// controlam o tempo sem esperar o relógio real.
type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type Service struct {
	pool  *pgxpool.Pool
	clock Clock
}

func NewService(pool *pgxpool.Pool, clock Clock) *Service {
	return &Service{pool: pool, clock: clock}
}

// Fence é o estado observável de um dispatch shard, devolvido depois de
// Promote/RenewLease para o writer saber qual epoch usar nos comandos
// seguintes.
type Fence struct {
	ShardID     string
	Epoch       int64
	OwnerRegion string
	LeaseUntil  time.Time
}

// Promote torna ownerRegion o dono do shard, incrementando o epoch. Só
// funciona se não existir fence para o shard ainda (primeira eleição) ou
// se a lease atual já expirou: nunca promove por cima de uma lease válida,
// o que impediria dois donos simultâneos (a mesma regra que
// docs/tla/DispatchFencing.tla chama de LeaseExpire -> Promote).
func (s *Service) Promote(ctx context.Context, shardID, ownerRegion string, leaseDuration time.Duration) (Fence, error) {
	now := s.clock.Now()
	leaseUntil := now.Add(leaseDuration)
	token := uuid.New()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Fence{}, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE dispatch_fences
		SET epoch = epoch + 1, owner_region = $1, lease_until = $2, last_write_token = $3
		WHERE shard_id = $4 AND lease_until < $5
	`, ownerRegion, leaseUntil, token, shardID, now)
	if err != nil {
		return Fence{}, err
	}
	if tag.RowsAffected() == 0 {
		// Shard pode não existir ainda: tenta criar com epoch 1. Se já
		// existir com lease válida, o INSERT falha por PK duplicada e o
		// erro correto (lease ainda válida) é devolvido.
		_, err := tx.Exec(ctx, `
			INSERT INTO dispatch_fences (shard_id, epoch, owner_region, lease_until, last_write_token)
			VALUES ($1, 1, $2, $3, $4)
		`, shardID, ownerRegion, leaseUntil, token)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
				return Fence{}, ErrLeaseNotExpired
			}
			return Fence{}, err
		}
	}

	var epoch int64
	if err := tx.QueryRow(ctx, `SELECT epoch FROM dispatch_fences WHERE shard_id = $1`, shardID).Scan(&epoch); err != nil {
		return Fence{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Fence{}, err
	}
	return Fence{ShardID: shardID, Epoch: epoch, OwnerRegion: ownerRegion, LeaseUntil: leaseUntil}, nil
}

// CreateAssignment é a transação única do roadmap: valida o fencing token
// (epoch + owner_region + lease vigente) na mesma condição do UPDATE que
// renova last_write_token, insere o assignment (protegido pelas duas
// unique constraints) e enfileira o evento de outbox, tudo atômico. Um
// writer com epoch velho nunca chega a inserir: ErrStaleFence antes de
// qualquer efeito.
func (s *Service) CreateAssignment(ctx context.Context, shardID, ownerRegion string, epoch int64, deliveryID, courierID string, courierSessionEpoch int64) (assignmentID string, err error) {
	now := s.clock.Now()
	token := uuid.New()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// 1. Renova a fence só se o token enviado ainda for o vigente: é o
	// UPDATE condicional "WHERE epoch = ? AND owner_region = ? AND
	// lease_until > now()" do roadmap, mapeado para FenceWrite/Assign na
	// especificação TLA+.
	tag, err := tx.Exec(ctx, `
		UPDATE dispatch_fences
		SET last_write_token = $1
		WHERE shard_id = $2 AND epoch = $3 AND owner_region = $4 AND lease_until > $5
	`, token, shardID, epoch, ownerRegion, now)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrStaleFence
	}

	// 2. Insere o assignment, protegido pelas unique constraints.
	id := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO active_assignments
			(assignment_id, delivery_id, courier_id, shard_id, epoch, courier_session_epoch, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, deliveryID, courierID, shardID, epoch, courierSessionEpoch, now)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			if pgErr.ConstraintName == "active_assignments_delivery_id_key" {
				return "", ErrDeliveryAlreadyAssigned
			}
			return "", ErrCourierAlreadyActive
		}
		return "", err
	}

	// 3. Evento de outbox na mesma transação: o assignment e o evento
	// comitam juntos, ou nenhum dos dois (mesmo padrão de
	// internal/dispatch desde o tier 1).
	if _, err := outbox.Enqueue(ctx, tx, deliveryID, topics.DeliveryEvents, topics.KindAssignmentConfirmed, map[string]string{
		"delivery_id":   deliveryID,
		"courier_id":    courierID,
		"shard_id":      shardID,
		"assignment_id": id,
	}); err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

// FinishAssignment move o assignment ativo de uma entrega para o
// histórico, dentro da mesma transação: nunca existem as duas linhas ao
// mesmo tempo, nem nenhuma das duas.
func (s *Service) FinishAssignment(ctx context.Context, deliveryID, reason string) error {
	now := s.clock.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var assignmentID, courierID, shardID string
	var epoch, courierSessionEpoch int64
	var createdAt time.Time
	err = tx.QueryRow(ctx, `
		DELETE FROM active_assignments WHERE delivery_id = $1
		RETURNING assignment_id, courier_id, shard_id, epoch, courier_session_epoch, created_at
	`, deliveryID).Scan(&assignmentID, &courierID, &shardID, &epoch, &courierSessionEpoch, &createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAssignmentNotFound
		}
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO assignment_history
			(assignment_id, delivery_id, courier_id, shard_id, epoch, courier_session_epoch, created_at, finished_at, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, assignmentID, deliveryID, courierID, shardID, epoch, courierSessionEpoch, createdAt, now, reason); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// CurrentFence lê o estado vigente de um shard (usado pelo cell router e
// pelos testes para descobrir o epoch antes de chamar CreateAssignment).
func (s *Service) CurrentFence(ctx context.Context, shardID string) (Fence, error) {
	var f Fence
	f.ShardID = shardID
	err := s.pool.QueryRow(ctx, `
		SELECT epoch, owner_region, lease_until FROM dispatch_fences WHERE shard_id = $1
	`, shardID).Scan(&f.Epoch, &f.OwnerRegion, &f.LeaseUntil)
	return f, err
}
