#!/usr/bin/env bash
# Cenário D do tier 4 (docs/benchmarks/chaos-tier-4.md, seção "D. Latência
# no PostgreSQL via Toxiproxy"), como script reproduzível.
#
# Hipótese: latência alta no PostgreSQL degrada a latência de
# POST /deliveries, mas não produz erro: mais lento, não incorreto.
#
# Estado estável: N requisições sequenciais de POST /deliveries direto
# contra o Postgres, sem proxy, latência de referência medida.
#
# Injeção: toxic `latency` no proxy Toxiproxy (stream downstream, 300ms
# ± 100ms de jitter) na frente do Postgres.
#
# Observação: as mesmas N requisições, agora atravessando o proxy com o
# toxic ativo, devem ficar visivelmente mais lentas sem nenhuma falhar.
#
# Condição de parada: CUIDADO CONHECIDO — Toxiproxy 2.12.0 tem um bug de
# deadlock na API de controle sob alta concorrência com reset_peer
# (issue https://github.com/Shopify/toxiproxy/issues/558, reprodução
# isolada em toxiproxy-repro/ na raiz de labs, não tocada aqui). Este
# script só dispara requisições SEQUENCIAIS (uma de cada vez), bem abaixo
# dos ~400 clientes concorrentes que disparam o bug. Não paralelize isto.
#
# Recuperação: toxic removido via API do Toxiproxy, container Toxiproxy
# derrubado no fim (trap), sempre.
#
# Este cenário já foi executado e documentado com evidência real em
# docs/benchmarks/chaos-tier-4.md (seção D) e
# docs/benchmarks/tier-4-chaos-d-toxiproxy-postgres-evidencia.txt. Este
# script formaliza os mesmos passos manuais para reexecução futura; não é
# reexecutado automaticamente por padrão neste passe de auditoria (exige
# subir um proxy adicional e reconfigurar a porta do Postgres que o
# delivery-api usa, ver comentário "USO" abaixo) — rode manualmente quando
# quiser reproduzir.
#
# USO:
#   1. docker compose up -d postgres
#   2. ajuste DELIVERY_API_DATABASE_URL para apontar para a porta do proxy
#      (15432 por padrão neste script) antes de subir o delivery-api local
#      (`DATABASE_URL=postgres://lunchrush:lunchrush@localhost:15432/lunchrush?sslmode=disable go run ./cmd/delivery-api`)
#   3. rode este script
set -euo pipefail

TOXIPROXY_CONTAINER="chaos-toxiproxy"
TOXIPROXY_API="${TOXIPROXY_API:-http://localhost:8474}"
PROXY_LISTEN="${PROXY_LISTEN:-0.0.0.0:15432}"
POSTGRES_UPSTREAM="${POSTGRES_UPSTREAM:-postgres:5432}"
BASE_URL="${BASE_URL:-http://localhost:8083}"
NETWORK="${NETWORK:-lunchrush_default}"
REQUESTS="${REQUESTS:-10}"

log() { echo "[chaos/postgres-latency-toxiproxy] $*"; }

cleanup() {
  log "recuperação: removendo toxic e derrubando o container do Toxiproxy"
  curl -sS -X DELETE "$TOXIPROXY_API/proxies/postgres/toxics/latency-downstream" >/dev/null 2>&1 || true
  docker rm -f "$TOXIPROXY_CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

log "subindo Toxiproxy 2.12.0 na rede $NETWORK"
docker run -d --name "$TOXIPROXY_CONTAINER" --network "$NETWORK" \
  -p 8474:8474 -p 15432:15432 \
  ghcr.io/shopify/toxiproxy:2.12.0 >/dev/null

sleep 1
curl -sS -X POST "$TOXIPROXY_API/proxies" -H "Content-Type: application/json" \
  -d "{\"name\":\"postgres\",\"listen\":\"$PROXY_LISTEN\",\"upstream\":\"$POSTGRES_UPSTREAM\"}" >/dev/null

measure() {
  local label="$1"
  local total=0
  for i in $(seq 1 "$REQUESTS"); do
    local key="chaos-latency-${label}-$(date +%s%N)"
    local t0 t1 ms
    t0=$(date +%s%N)
    curl -sS -o /dev/null -X POST "$BASE_URL/deliveries" \
      -H "X-Caller: chaos-latency" -H "Idempotency-Key: $key" -H "Content-Type: application/json" -d '{}'
    t1=$(date +%s%N)
    ms=$(( (t1 - t0) / 1000000 ))
    log "[$label] requisição $i: ${ms}ms"
    total=$((total + ms))
  done
  log "[$label] média de $REQUESTS requisições: $((total / REQUESTS))ms"
}

log "estado estável: $REQUESTS requisições sequenciais, sem toxic"
measure "steady-state"

log "injeção: toxic latency 300ms +-100ms no stream downstream"
curl -sS -X POST "$TOXIPROXY_API/proxies/postgres/toxics" -H "Content-Type: application/json" \
  -d '{"name":"latency-downstream","type":"latency","stream":"downstream","attributes":{"latency":300,"jitter":100}}' >/dev/null

log "observação: $REQUESTS requisições sequenciais, com o toxic ativo"
measure "toxic-ativo"

log "removendo o toxic e confirmando que a API do Toxiproxy segue responsiva (sem sinal do bug de deadlock)"
curl -sS -X DELETE "$TOXIPROXY_API/proxies/postgres/toxics/latency-downstream" >/dev/null
curl -sS -o /dev/null -w "GET /proxies após remover o toxic: %{http_code}\n" "$TOXIPROXY_API/proxies"

log "recuperação: $REQUESTS requisições sequenciais, sem toxic, latência deveria voltar ao patamar do estado estável"
measure "pos-recuperacao"
