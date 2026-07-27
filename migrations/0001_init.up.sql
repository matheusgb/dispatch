CREATE TABLE couriers (
    id         uuid PRIMARY KEY,
    name       text NOT NULL,
    available  boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE deliveries (
    id          uuid PRIMARY KEY,
    state       text NOT NULL CHECK (state IN (
        'created', 'ready_for_lunchrush', 'offered', 'assigned',
        'picked_up', 'delivered', 'declined', 'expired', 'canceled'
    )),
    courier_id  uuid REFERENCES couriers(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Invariante 2: um entregador possui no máximo uma entrega ativa. A entrega
-- ativa é a que está em assigned ou picked_up; offered ainda não é exclusiva
-- porque uma oferta pode ser recusada ou expirar sem nunca ter sido aceita.
CREATE UNIQUE INDEX idx_deliveries_one_active_per_courier
    ON deliveries (courier_id)
    WHERE state IN ('assigned', 'picked_up');

CREATE TABLE delivery_transitions (
    id           bigserial PRIMARY KEY,
    delivery_id  uuid NOT NULL REFERENCES deliveries(id),
    from_state   text NOT NULL,
    to_state     text NOT NULL,
    occurred_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_delivery_transitions_delivery_id
    ON delivery_transitions (delivery_id, occurred_at);

-- Ledger de idempotência (invariante 5). A chave tem escopo por caller e
-- operação: a mesma chave usada em duas operações diferentes não colide.
CREATE TABLE idempotency_keys (
    caller        text NOT NULL,
    operation     text NOT NULL,
    key           text NOT NULL,
    payload_hash  text NOT NULL,
    status_code   int NOT NULL,
    response_body jsonb NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    PRIMARY KEY (caller, operation, key)
);
