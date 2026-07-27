#!/usr/bin/env bash
# Cenário B do tier 4 (docs/benchmarks/chaos-tier-4.md, seção "B. Falha do
# Redis com tracking-projector lendo via fallback"), como script
# reproduzível em vez de só relatório.
#
# Hipótese: com o Redis fora do ar, GET /deliveries/{id}/position continua
# respondendo 200 lendo direto do PostgreSQL (ADR 0003: Redis é projeção,
# nunca fonte de verdade).
#
# Estado estável: fluxo completo (emitir token, criar entrega, ingerir
# posição, ler posição) responde 200 em cada etapa com o Redis saudável.
#
# Injeção: `docker compose stop redis` com tracking-ingest e
# tracking-projector seguindo no ar.
#
# Observação: repete o mesmo fluxo completo com o Redis parado; falha o
# script (set -e) se qualquer etapa não responder o status esperado.
#
# Condição de parada: qualquer resposta fora do 2xx esperado aborta o
# script imediatamente (set -e) — não há como continuar avaliando com o
# sistema já fora do invariante que o experimento testa.
#
# Recuperação: `docker compose start redis` no fim, sempre (trap), mesmo
# se o experimento falhar no meio.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8083}"
TRACKING_URL="${TRACKING_URL:-http://localhost:8084}"
PROJECTOR_URL="${PROJECTOR_URL:-http://localhost:8085}"
ADMIN_SECRET="${ADMIN_SECRET:-compose-dev-admin-secret}"
COMPOSE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

log() { echo "[chaos/redis-unavailable] $*"; }

cleanup() {
  log "recuperação: religando o Redis"
  (cd "$COMPOSE_DIR" && docker compose start redis >/dev/null)
}
trap cleanup EXIT

run_flow() {
  local label="$1"
  local key="e2e-chaos-redis-${label}-$(date +%s%N)"

  local create_status create_body
  create_body=$(curl -sS --retry 3 --retry-delay 1 --retry-connrefused \
    -o /tmp/chaos-create.json -w "%{http_code}" \
    -X POST "$BASE_URL/deliveries" \
    -H "X-Caller: chaos-redis" -H "Idempotency-Key: $key" -H "Content-Type: application/json" -d '{}')
  create_status="$create_body"
  [ "$create_status" = "201" ] || { log "[$label] criar entrega falhou: $create_status"; exit 1; }
  local delivery_id
  delivery_id=$(jq -r .id /tmp/chaos-create.json)

  local token_status token
  token_status=$(curl -sS -o /tmp/chaos-token.json -w "%{http_code}" \
    -X POST "$BASE_URL/auth/tokens" -H "X-Admin-Secret: $ADMIN_SECRET" -H "Content-Type: application/json" \
    -d '{"caller":"chaos-redis"}')
  [ "$token_status" = "201" ] || { log "[$label] emitir token falhou: $token_status"; exit 1; }
  token=$(jq -r .token /tmp/chaos-token.json)

  local pos_status
  pos_status=$(curl -sS -o /dev/null -w "%{http_code}" \
    -X POST "$TRACKING_URL/deliveries/$delivery_id/positions" \
    -H "Authorization: Bearer $token" -H "Content-Type: application/json" \
    -d '{"tracking_session_epoch":1,"sequence":1,"latitude":-23.5,"longitude":-46.6}')
  [ "$pos_status" = "202" ] || { log "[$label] publicar posição falhou: $pos_status"; exit 1; }

  sleep 1

  local read_status
  read_status=$(curl -sS -o /dev/null -w "%{http_code}" \
    "$PROJECTOR_URL/deliveries/$delivery_id/position" -H "Authorization: Bearer $token")
  [ "$read_status" = "200" ] || { log "[$label] ler posição falhou: $read_status"; exit 1; }

  log "[$label] fluxo completo: create=201 token=201 position=202 read=200 (ok)"
}

log "estado estável: fluxo completo com Redis saudável"
run_flow "steady-state"

log "injeção: docker compose stop redis"
(cd "$COMPOSE_DIR" && docker compose stop redis >/dev/null)
sleep 2

log "observação: repetindo o fluxo completo com o Redis fora do ar"
run_flow "redis-down"

log "resultado: invariante confirmado (Redis é projeção, não fonte de verdade)"
