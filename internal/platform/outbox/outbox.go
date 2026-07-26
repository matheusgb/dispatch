// Package outbox implementa o padrão transactional outbox: o evento é
// gravado na mesma transação do efeito de domínio (Enqueue), e um relay
// publica de forma assíncrona depois do commit. Se o relay morrer entre
// publicar e marcar como publicado, o evento é publicado de novo no
// próximo ciclo: at-least-once por desenho, nunca at-most-once.
package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Event struct {
	ID          string
	AggregateID string
	Topic       string
	Kind        string
	Payload     []byte
}

// Enqueue grava o evento dentro da transação tx do chamador. Precisa ser
// chamado antes do commit do efeito de domínio: outbox e efeito são
// atômicos, ou os dois acontecem, ou nenhum.
func Enqueue(ctx context.Context, tx pgx.Tx, aggregateID, topic, kind string, payload any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	eventID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (event_id, aggregate_id, topic, kind, payload)
		VALUES ($1, $2, $3, $4, $5)
	`, eventID, aggregateID, topic, kind, body)
	if err != nil {
		return "", err
	}
	return eventID, nil
}

// Publisher é o produtor Kafka, abstraído para o relay ser testável sem
// broker real.
type Publisher interface {
	Publish(ctx context.Context, topic, key string, value []byte) error
}

type Relay struct {
	pool      *pgxpool.Pool
	publisher Publisher
	logger    *slog.Logger
}

func NewRelay(pool *pgxpool.Pool, publisher Publisher, logger *slog.Logger) *Relay {
	return &Relay{pool: pool, publisher: publisher, logger: logger}
}

// PublishPending publica todo evento com published_at nulo e marca cada um
// imediatamente após o ack do broker, um por vez: um relay morto no meio
// do lote deixa os eventos seguintes intactos para o próximo ciclo.
// Devolve quantos eventos foram publicados.
func (r *Relay) PublishPending(ctx context.Context, limit int) (int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, event_id, aggregate_id, topic, kind, payload
		FROM outbox_events
		WHERE published_at IS NULL
		ORDER BY id
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, err
	}

	type pending struct {
		id      int64
		eventID string
		topic   string
		kind    string
		payload []byte
	}
	var items []pending
	for rows.Next() {
		var p pending
		var aggregateID string
		if err := rows.Scan(&p.id, &p.eventID, &aggregateID, &p.topic, &p.kind, &p.payload); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	published := 0
	for _, p := range items {
		body, err := json.Marshal(Envelope{EventID: p.eventID, Kind: p.kind, Payload: p.payload})
		if err != nil {
			r.logger.Error("serializar envelope do outbox", "event_id", p.eventID, "error", err)
			continue
		}
		if err := r.publisher.Publish(ctx, p.topic, p.eventID, body); err != nil {
			r.logger.Warn("publicar evento do outbox falhou, tenta no próximo ciclo", "event_id", p.eventID, "error", err)
			continue
		}
		tag, err := r.pool.Exec(ctx, `
			UPDATE outbox_events SET published_at = $1 WHERE id = $2 AND published_at IS NULL
		`, time.Now(), p.id)
		if err != nil {
			r.logger.Error("marcar evento do outbox como publicado", "event_id", p.eventID, "error", err)
			continue
		}
		if tag.RowsAffected() > 0 {
			published++
		}
	}
	return published, nil
}

// Envelope é o formato decodificado por quem consome: todo evento carrega
// seu event_id (para o inbox) e kind (para o consumidor decidir a ação).
type Envelope struct {
	EventID string          `json:"event_id"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

func DecodeEnvelope(data []byte) (Envelope, error) {
	var e Envelope
	err := json.Unmarshal(data, &e)
	return e, err
}
