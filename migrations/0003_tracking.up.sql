-- Histórico de GPS: append-only, tolerante a atraso e a chegada fora de
-- ordem. Cada evento aceito entra aqui, mesmo que seja mais antigo que o
-- estado atual conhecido: o log serve para auditoria e replay, não é a
-- fonte da posição mais recente.
CREATE TABLE tracking_positions (
    id                     bigserial PRIMARY KEY,
    delivery_id            uuid NOT NULL REFERENCES deliveries(id),
    tracking_session_epoch bigint NOT NULL,
    sequence               bigint NOT NULL,
    latitude               double precision NOT NULL,
    longitude              double precision NOT NULL,
    accuracy_m             double precision,
    recorded_at            timestamptz NOT NULL,
    received_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (delivery_id, tracking_session_epoch, sequence)
);

CREATE INDEX idx_tracking_positions_delivery
    ON tracking_positions (delivery_id, tracking_session_epoch DESC, sequence DESC);

-- Projeção da última posição conhecida. Monotônica por
-- (tracking_session_epoch, sequence): uma escrita só substitui o estado
-- atual se for estritamente mais nova (invariante 7). O epoch muda quando o
-- app reinicia a sessão de tracking ou quando a entrega troca de
-- entregador; a partir do tier 3 isso é reforçado por quem emite o epoch,
-- aqui é responsabilidade de quem chama a API.
CREATE TABLE delivery_tracking_state (
    delivery_id            uuid PRIMARY KEY REFERENCES deliveries(id),
    tracking_session_epoch bigint NOT NULL,
    sequence               bigint NOT NULL,
    latitude               double precision NOT NULL,
    longitude              double precision NOT NULL,
    accuracy_m             double precision,
    recorded_at            timestamptz NOT NULL,
    updated_at             timestamptz NOT NULL DEFAULT now()
);
