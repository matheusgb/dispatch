-- Outbox transacional: o evento é gravado na mesma transação do efeito de
-- domínio. Um relay publica de forma assíncrona e marca published_at só
-- depois do ack do Kafka; se o relay morrer entre publicar e marcar, o
-- evento é republicado no próximo ciclo (at-least-once por desenho).
CREATE TABLE outbox_events (
    id           bigserial PRIMARY KEY,
    event_id     uuid NOT NULL UNIQUE,
    aggregate_id uuid NOT NULL,
    topic        text NOT NULL,
    kind         text NOT NULL,
    payload      jsonb NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
);

CREATE INDEX idx_outbox_events_unpublished
    ON outbox_events (created_at)
    WHERE published_at IS NULL;

-- Inbox/dedup por consumidor: um evento com o mesmo event_id processado de
-- novo pelo mesmo consumidor não repete o efeito (invariante 8). A chave é
-- por consumidor porque o mesmo evento pode legitimamente ser processado
-- por mais de um consumidor (ex.: lunchrush-worker e notification-worker).
CREATE TABLE consumed_events (
    consumer     text NOT NULL,
    event_id     uuid NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer, event_id)
);
