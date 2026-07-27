#!/usr/bin/env bash
# Encadeia os subcomandos de cmd/cloudfailover (ver comentário do próprio
# main.go) na mesma sequência já executada e documentada com evidência
# real em docs/benchmarks/tier-6-portability/failover-transcript.txt e
# docs/runbooks/failover-de-fencing.md: seed em cloud-a, promoção e
# escrita em cloud-a, backup lógico (pg_dump) de cloud-a, restauração em
# cloud-b, promoção de cloud-b (fencing rejeita o writer antigo de
# cloud-a, aceita o novo de cloud-b).
#
# Pré-requisito: os dois stacks no ar (docker-compose.yml para cloud-a e
# docker-compose.cloud-b.yml para cloud-b, ver `make cloud-up
# PROVIDER=cloud-a` / `make cloud-up PROVIDER=cloud-b`), cada um com seu
# Postgres migrado.
#
# Este script formaliza em código o que já foi provado manualmente; não é
# reexecutado automaticamente neste passe de auditoria por causa do custo
# de memória de manter dois stacks completos no ar ao mesmo tempo nesta
# máquina compartilhada com outros laboratórios (ver chaos/local/README.md
# para a mesma decisão sobre os cenários de chaos que dependem de
# infraestrutura adicional).
set -euo pipefail

CLOUD_A_DB="${CLOUD_A_DB:-postgres://dispatch:dispatch@localhost:5432/dispatch?sslmode=disable}"
CLOUD_B_DB="${CLOUD_B_DB:-postgres://dispatch:dispatch@localhost:15432/dispatch?sslmode=disable}"
SHARD_ID="${SHARD_ID:-shard-cloud-failover-demo}"
SEED_N="${SEED_N:-10}"

log() { echo "[cloud-failover-demo] $*"; }

go_run_cloudfailover() {
  go run ./cmd/cloudfailover "$@"
}

log "1. seed $SEED_N pares em cloud-a"
go_run_cloudfailover seed -db "$CLOUD_A_DB" -n "$SEED_N"

log "2. promove cloud-a como dona do shard $SHARD_ID"
go_run_cloudfailover promote -db "$CLOUD_A_DB" -shard "$SHARD_ID" -region cloud-a -lease 1h

log "3. cloud-a escreve assignments com epoch=1"
go_run_cloudfailover assign -db "$CLOUD_A_DB" -shard "$SHARD_ID" -region cloud-a -epoch 1 -attempts 5

log "4. backup lógico de cloud-a (pg_dump, só as tabelas do domínio)"
DUMP_FILE=$(mktemp)
pg_dump "$CLOUD_A_DB" \
  -t deliveries -t couriers -t dispatch_fences -t active_assignments -t assignment_history \
  --data-only --column-inserts >"$DUMP_FILE"
log "backup em $DUMP_FILE ($(wc -l <"$DUMP_FILE") linhas)"

log "5. restaura o backup em cloud-b"
psql "$CLOUD_B_DB" -v ON_ERROR_STOP=1 -f "$DUMP_FILE" >/dev/null
rm -f "$DUMP_FILE"

log "6. promove cloud-b como nova dona do shard (epoch avança)"
go_run_cloudfailover promote -db "$CLOUD_B_DB" -shard "$SHARD_ID" -region cloud-b -lease 1h

log "7. writer antigo de cloud-a (epoch=1) tenta escrever em cloud-b: deve ser rejeitado"
if go_run_cloudfailover assign -db "$CLOUD_B_DB" -shard "$SHARD_ID" -region cloud-a -epoch 1 -attempts 3 | grep -q '"successes":0'; then
  log "confirmado: writer antigo 100% rejeitado (ErrStaleFence)"
else
  log "ATENÇÃO: writer antigo não foi 100% rejeitado — investigar antes de confiar no failover"
  exit 1
fi

log "8. writer novo de cloud-b escreve com sucesso"
go_run_cloudfailover fence -db "$CLOUD_B_DB" -shard "$SHARD_ID"

log "concluído: ver docs/runbooks/failover-de-fencing.md e docs/benchmarks/tier-6-portability/ para a evidência já documentada desta mesma sequência"
