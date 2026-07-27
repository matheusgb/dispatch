-- Autoridade de ownership do tier 5 (ver docs/adr/0018-fencing-lease-e-epoch.md
-- e docs/tla/LunchRushFencing.tla, que especifica formalmente o protocolo
-- implementado aqui). Uma linha por lunchrush shard: um shard pequeno,
-- limitado, de entregas+couriers, não uma célula inteira, para evitar hot
-- key (o roadmap é explícito sobre isso).
CREATE TABLE lunchrush_fences (
    shard_id         text PRIMARY KEY,
    epoch            bigint NOT NULL DEFAULT 0,
    owner_region     text NOT NULL,
    lease_until      timestamptz NOT NULL,
    last_write_token uuid NOT NULL
);

-- Assignment ativo: no máximo um por entrega, no máximo um por courier,
-- reforçado pelas duas unique constraints abaixo, exatamente como
-- deliveries.courier_id no tier 1 (ADR 0002) — este é o mesmo padrão
-- estendido para carregar epoch e shard.
CREATE TABLE active_assignments (
    assignment_id          uuid PRIMARY KEY,
    delivery_id            uuid NOT NULL UNIQUE,
    courier_id             uuid NOT NULL UNIQUE,
    shard_id               text NOT NULL REFERENCES lunchrush_fences(shard_id),
    epoch                  bigint NOT NULL,
    courier_session_epoch  bigint NOT NULL,
    created_at             timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_active_assignments_shard ON active_assignments (shard_id);

-- Histórico append-only: quando um assignment termina, a linha sai de
-- active_assignments e entra aqui na mesma transação (nunca as duas coisas
-- ao mesmo tempo, nunca nenhuma das duas).
CREATE TABLE assignment_history (
    assignment_id          uuid PRIMARY KEY,
    delivery_id            uuid NOT NULL,
    courier_id             uuid NOT NULL,
    shard_id               text NOT NULL,
    epoch                  bigint NOT NULL,
    courier_session_epoch  bigint NOT NULL,
    created_at             timestamptz NOT NULL,
    finished_at            timestamptz NOT NULL DEFAULT now(),
    reason                 text NOT NULL
);

CREATE INDEX idx_assignment_history_delivery ON assignment_history (delivery_id);
CREATE INDEX idx_assignment_history_courier ON assignment_history (courier_id);

-- Ownership do courier entre células (roadmap tier 5, seção "Diretório e
-- roteamento"): home_cell é a célula dona do courier: courier_session_epoch
-- muda a cada handoff, igual ao tracking_session_epoch do tier 2 para GPS.
ALTER TABLE couriers ADD COLUMN home_cell text NOT NULL DEFAULT 'cell-a';
ALTER TABLE couriers ADD COLUMN courier_session_epoch bigint NOT NULL DEFAULT 0;
ALTER TABLE couriers ADD COLUMN handoff_state text NOT NULL DEFAULT 'free'
    CHECK (handoff_state IN ('free', 'draining', 'handoff_confirmed'));

-- Roteamento por célula: toda entrega pertence a uma célula, decidida no
-- momento da criação, sem precisar consultar todas as células para
-- descobrir onde uma entrega mora (roadmap: "o roteador encontra a célula
-- sem consultar todos os bancos").
ALTER TABLE deliveries ADD COLUMN cell_id text NOT NULL DEFAULT 'cell-a';
