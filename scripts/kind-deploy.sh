#!/usr/bin/env bash
# Sobe um cluster kind de um nó só, constrói as cinco imagens do tier 3,
# carrega no cluster e aplica os manifests. A infra (Postgres, Redis,
# Redpanda, dependency-simulator) continua no docker compose do host: rode
# `docker compose --profile app up -d postgres redis redpanda
# dependency-simulator redpanda-topics migrate` antes deste script.
set -euo pipefail
cd "$(dirname "$0")/.."

CLUSTER_NAME="lunchrush"

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
# cluster kind, que roteia para o host em Docker no Linux. O IP muda por
# ambiente, então a substituição acontece só no manifest renderizado, nunca
# no arquivo versionado em deploy/kubernetes/base.
GATEWAY_IP=$(docker network inspect "kind" --format '{{ (index .IPAM.Config 0).Gateway }}')
echo "infra externa acessível via $GATEWAY_IP (gateway da rede docker do kind)"

kubectl kustomize deploy/kubernetes/base \
  | sed "s/HOST_GATEWAY_IP/${GATEWAY_IP}/g" \
  | kubectl --context "kind-$CLUSTER_NAME" apply -f -

echo "aguardando rollout"
for dep in delivery-api lunchrush-worker tracking-ingest tracking-projector notification-worker; do
  kubectl --context "kind-$CLUSTER_NAME" -n lunchrush rollout status deployment/"$dep" --timeout=120s
done

echo "pronto. port-forward de exemplo:"
echo "  kubectl --context kind-$CLUSTER_NAME -n lunchrush port-forward svc/delivery-api 8080:80"
