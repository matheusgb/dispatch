#!/usr/bin/env bash
# Cenário C do tier 4 (docs/benchmarks/chaos-tier-4.md, seção "C. Redpanda
# pausado: outbox absorve a indisponibilidade"), como script reproduzível.
#
# Hipótese: com o Redpanda indisponível, POST /deliveries continua
# aceitando pedidos (o efeito de negócio é o INSERT em deliveries + o
# registro em outbox_events, na mesma transação; publicar no Kafka é
# responsabilidade separada do relay do outbox, que pode atrasar sem
# bloquear a escrita, ver docs/adr/0009).
#
# Estado estável: POST /deliveries responde 201 com o Redpanda saudável.
#
# Injeção: `docker pause` no container do Redpanda (processo congelado,
# não só parado, para não gerar TCP RST imediato).
#
# Observação: POST /deliveries continua respondendo 201 com o Redpanda
# pausado; outbox_events acumula linhas com published_at nulo.
#
# Condição de parada: se POST /deliveries deixar de responder 201 durante
# a pausa, o script aborta (a hipótese caiu).
#
# Recuperação: `docker unpause`, sempre (trap); confirma que o backlog de
# outbox_events pendente esvazia sozinho.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8083}"
COMPOSE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REDPANDA_CONTAINER="${REDPANDA_CONTAINER:-lunchrush-redpanda-1}"
DATABASE_URL="${DATABASE_URL:-postgres://lunchrush:lunchrush@localhost:5432/lunchrush?sslmode=disable}"

log() { echo "[chaos/kafka-paused] $*"; }

cleanup() {
  log "recuperação: docker unpause $REDPANDA_CONTAINER"
  docker unpause "$REDPANDA_CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

create_delivery() {
  local key="e2e-chaos-kafka-$(date +%s%N)"
  local status
  status=$(curl -sS -o /dev/null -w "%{http_code}" \
    -X POST "$BASE_URL/deliveries" \
    -H "X-Caller: chaos-kafka" -H "Idempotency-Key: $key" -H "Content-Type: application/json" -d '{}')
  echo "$status"
}

pending_outbox_count() {
  docker exec lunchrush-postgres-1 psql -U lunchrush -d lunchrush -tAc \
    "select count(*) from outbox_events where published_at is null"
}

log "estado estável: três POST /deliveries com o Redpanda saudável"
for i in 1 2 3; do
  status=$(create_delivery)
  [ "$status" = "201" ] || { log "criação $i falhou antes da injeção: $status"; exit 1; }
done

log "injeção: docker pause $REDPANDA_CONTAINER"
docker pause "$REDPANDA_CONTAINER" >/dev/null

log "observação: três POST /deliveries com o Redpanda pausado"
for i in 1 2 3; do
  status=$(create_delivery)
  [ "$status" = "201" ] || { log "criação $i falhou durante a pausa (hipótese quebrou): $status"; exit 1; }
done

pending=$(pending_outbox_count)
log "outbox_events pendentes (published_at IS NULL) com Redpanda pausado: $pending"
if [ "$pending" -lt 1 ]; then
  log "esperava pelo menos 1 evento pendente acumulado, achou $pending"
  exit 1
fi

log "recuperação: docker unpause $REDPANDA_CONTAINER"
docker unpause "$REDPANDA_CONTAINER" >/dev/null
trap - EXIT

# o relay poll a cada ~5s (ver internal/platform/outbox.Relay); logo após
# o unpause, o Redpanda ainda está reconstituindo liderança de partição por
# um instante, então as primeiras 1-2 tentativas de publish podem não
# confirmar — 30s dá margem para pelo menos 5-6 ciclos de poll.
deadline=$((SECONDS + 30))
while [ "$SECONDS" -lt "$deadline" ]; do
  pending=$(pending_outbox_count)
  if [ "$pending" = "0" ]; then
    log "backlog do outbox esvaziou em até $((SECONDS)) s (relay publicou tudo)"
    exit 0
  fi
  sleep 1
done

log "backlog do outbox não esvaziou em 30s (pendentes=$pending) — investigar o relay"
exit 1
