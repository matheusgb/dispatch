#!/usr/bin/env bash
# Sobe (ou reaproveita) um cluster kind de um nó só chamado "lunchrush"
# (nome diferente do "edge-lab" usado por outro laboratório no mesmo
# host), constrói as cinco imagens do tier 3/4, carrega no cluster e
# instala o chart deploy/helm/lunchrush (tier 4, substitui o Kustomize de
# deploy/kubernetes/ usado no tier 3, ver ADR 0013).
#
# Pré-requisito: a infra compartilhada (Postgres, Redis, Redpanda,
# dependency-simulator) precisa estar de pé no docker compose do host:
#   docker compose --profile app up -d postgres redis redpanda \
#     dependency-simulator redpanda-topics migrate
set -euo pipefail
cd "$(dirname "$0")/.."

CLUSTER_NAME="lunchrush"
RELEASE_NAME="lunchrush"
CHART_DIR="deploy/helm/lunchrush"

if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  echo "criando cluster kind '$CLUSTER_NAME' (um nó só, para caber no ambiente local)"
  cat <<EOF | kind create cluster --name "$CLUSTER_NAME" --config -
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
EOF
fi

echo "construindo imagens"
for svc in delivery-api lunchrush-worker tracking-ingest tracking-projector notification-worker; do
  docker build -f "deploy/compose/Dockerfile.$svc" -t "lunchrush-$svc:kind" .
  kind load docker-image "lunchrush-$svc:kind" --name "$CLUSTER_NAME"
done

# O nó do kind é um container Docker; para alcançar a infra publicada no
# host pelo docker compose, usamos o gateway da rede docker do próprio
# cluster kind (ADR 0011). O IP muda por ambiente, então a substituição
# acontece só no --set do helm, nunca em deploy/helm/lunchrush/values.yaml.
GATEWAY_IP=$(docker network inspect "kind" --format '{{ (index .IPAM.Config 0).Gateway }}')
echo "infra externa acessível via $GATEWAY_IP (gateway da rede docker do kind)"

helm upgrade --install "$RELEASE_NAME" "$CHART_DIR" \
  --kube-context "kind-$CLUSTER_NAME" \
  --set "externalInfra.hostGatewayIP=${GATEWAY_IP}" \
  "$@"

echo "aguardando rollout"
for dep in delivery-api lunchrush-worker tracking-ingest tracking-projector notification-worker; do
  kubectl --context "kind-$CLUSTER_NAME" -n lunchrush rollout status deployment/"$dep" --timeout=120s
done

echo "pronto. port-forward de exemplo:"
echo "  kubectl --context kind-$CLUSTER_NAME -n lunchrush port-forward svc/delivery-api 8080:80"
