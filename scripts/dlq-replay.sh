#!/usr/bin/env bash
# Automatiza o passo manual já documentado em docs/runbooks/dlq-replay.md:
# lê uma mensagem específica de uma fila DLQ pelo offset e republica no
# tópico original. Não faz replay em massa de propósito (o runbook já
# explicita essa limitação: uma mensagem que foi parar na DLQ merece
# revisão humana antes de voltar ao fluxo normal — este script só remove o
# atrito manual de montar o comando `rpk` certo).
#
# Uso:
#   scripts/dlq-replay.sh <topico-dlq> <offset>
#   make replay TOPIC=lunchrush.delivery-events.dlq DLQ_ID=<offset>
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "uso: $0 <topico-dlq> <offset>" >&2
  echo "exemplo: $0 lunchrush.delivery-events.dlq 0" >&2
  exit 1
fi

DLQ_TOPIC="$1"
OFFSET="$2"
REDPANDA_CONTAINER="${REDPANDA_CONTAINER:-lunchrush-redpanda-1}"
ORIGINAL_TOPIC="${DLQ_TOPIC%.dlq}"

if [ "$ORIGINAL_TOPIC" = "$DLQ_TOPIC" ]; then
  echo "esperava um tópico terminado em .dlq, recebi: $DLQ_TOPIC" >&2
  exit 1
fi

log() { echo "[dlq-replay] $*" >&2; }

log "lendo offset $OFFSET de $DLQ_TOPIC"
value=$(docker exec "$REDPANDA_CONTAINER" rpk topic consume "$DLQ_TOPIC" -n 1 -o "$OFFSET" | jq -r .value)

if [ -z "$value" ] || [ "$value" = "null" ]; then
  echo "não achei mensagem em $DLQ_TOPIC no offset $OFFSET" >&2
  exit 1
fi

log "mensagem lida (mostrando o kind decodificado, se der para decodificar como envelope JSON):"
echo "$value" | jq -r '.kind // "não decodificável como envelope, republicando bruto"' >&2

log "republicando em $ORIGINAL_TOPIC"
echo "$value" | docker exec -i "$REDPANDA_CONTAINER" rpk topic produce "$ORIGINAL_TOPIC" >/dev/null

log "feito: mensagem do offset $OFFSET de $DLQ_TOPIC republicada em $ORIGINAL_TOPIC"
log "revise a causa raiz antes de repetir isto em produção (ver docs/runbooks/dlq-replay.md)"
