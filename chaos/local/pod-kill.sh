#!/usr/bin/env bash
# Cenário A do tier 4 (docs/benchmarks/chaos-tier-4.md, seção "A. Pod kill
# de uma réplica do delivery-api"), como script reproduzível.
#
# Hipótese: matar um dos pods do delivery-api não derruba nenhuma
# requisição em andamento contra o Service: o Deployment recria o pod e,
# com minAvailable: 1 no PodDisruptionBudget, o outro pod continua
# respondendo o tempo todo.
#
# Estado estável: dois pods Running e 1/1, Service com dois endpoints.
#
# Injeção: uma rajada de GET /healthz de DENTRO do cluster contra o DNS do
# Service (não kubectl port-forward: port-forward escolhe um pod uma vez
# só e não exercita o balanceamento — foi o erro da primeira tentativa
# deste experimento, documentado em chaos-tier-4.md seção A). No meio da
# rajada, kubectl delete pod num dos dois pods do delivery-api.
#
# Observação: conta quantas requisições da rajada responderam algo
# diferente de 200.
#
# Condição de parada: se qualquer requisição falhar, o script continua até
# o fim da rajada (para dar o número completo), mas termina com exit 1.
#
# Recuperação: automática pelo Deployment (ReplicaSet mantém o número
# desejado de réplicas) — o script só confirma que voltou a 2/2 Running.
set -euo pipefail

NAMESPACE="${NAMESPACE:-lunchrush}"
CONTEXT="${CONTEXT:-kind-lunchrush}"
REQUESTS="${REQUESTS:-100}"

log() { echo "[chaos/pod-kill] $*"; }

if ! kubectl --context "$CONTEXT" get ns "$NAMESPACE" >/dev/null 2>&1; then
  log "cluster $CONTEXT / namespace $NAMESPACE não encontrado."
  log "suba com: make helm-up (ver scripts/helm-deploy.sh)"
  exit 1
fi

log "estado estável: pods do delivery-api"
kubectl --context "$CONTEXT" -n "$NAMESPACE" get pods -l app=delivery-api -o wide

log "subindo pod de debug (chaos-curl) para bater no Service de dentro do cluster"
kubectl --context "$CONTEXT" -n "$NAMESPACE" run chaos-curl --image=curlimages/curl:8.10.1 \
  --restart=Never --command -- sleep 300 >/dev/null
trap 'kubectl --context "'"$CONTEXT"'" -n "'"$NAMESPACE"'" delete pod chaos-curl --ignore-not-found >/dev/null 2>&1 || true' EXIT
kubectl --context "$CONTEXT" -n "$NAMESPACE" wait --for=condition=Ready pod/chaos-curl --timeout=60s >/dev/null

log "injeção: rajada de $REQUESTS GET /healthz via Service, matando um pod no meio"
(
  for i in $(seq 1 "$REQUESTS"); do
    if [ "$i" -eq $((REQUESTS / 2)) ]; then
      victim=$(kubectl --context "$CONTEXT" -n "$NAMESPACE" get pods -l app=delivery-api -o jsonpath='{.items[0].metadata.name}')
      # >&2: este bloco inteiro tem o stdout redirecionado para o arquivo
      # de resultados (só códigos HTTP, um por linha); sem isso a linha de
      # log vira uma linha a mais no arquivo e quebra a contagem de
      # sucesso/total mais abaixo (bug real encontrado reexecutando este
      # script, corrigido nesta sessão).
      log "matando pod $victim na requisição $i" >&2
      kubectl --context "$CONTEXT" -n "$NAMESPACE" delete pod "$victim" --wait=false >/dev/null
    fi
    kubectl --context "$CONTEXT" -n "$NAMESPACE" exec chaos-curl -- \
      curl -sS -o /dev/null -w "%{http_code}\n" http://delivery-api/healthz
    sleep 0.2
  done
) >/tmp/chaos-podkill-results.txt

total=$(wc -l </tmp/chaos-podkill-results.txt)
ok=$(grep -c '^200$' /tmp/chaos-podkill-results.txt || true)
log "observação: $ok/$total requisições responderam 200"

log "aguardando o Deployment voltar a 2/2 Running"
kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout status deployment/delivery-api --timeout=60s

if [ "$ok" != "$total" ]; then
  log "hipótese quebrou: $((total - ok)) requisições não responderam 200"
  exit 1
fi

log "hipótese confirmada: $ok/$total, nenhuma requisição perdida durante o pod kill"
