#!/usr/bin/env bash
# Instala o operador do KEDA no cluster kind "lunchrush" (via Helm, chart
# oficial kedacore/keda) e liga o ScaledObject de lunchrush-worker no chart
# da aplicação. Rode depois de scripts/helm-deploy.sh.
#
# ATENÇÃO: KEDA 2.20 (a versão do repositório oficial no momento deste
# tier) declara suporte formal a partir do Kubernetes 1.33; o kind usado
# aqui roda 1.31 (kindest/node:v1.31.0). O install segue funcionando (ver
# docs/adr/0014-keda-lag-de-consumer-group.md), mas o aviso da própria
# instalação fica registrado aqui como limitação conhecida, não escondida.
set -euo pipefail
cd "$(dirname "$0")/.."

CLUSTER_NAME="lunchrush"
KEDA_NAMESPACE="keda"

helm repo add kedacore https://kedacore.github.io/charts >/dev/null 2>&1 || true
helm repo update kedacore

helm upgrade --install keda kedacore/keda \
  --kube-context "kind-$CLUSTER_NAME" \
  --namespace "$KEDA_NAMESPACE" \
  --create-namespace

echo "aguardando operador do KEDA"
kubectl --context "kind-$CLUSTER_NAME" -n "$KEDA_NAMESPACE" rollout status deployment/keda-operator --timeout=90s
kubectl --context "kind-$CLUSTER_NAME" -n "$KEDA_NAMESPACE" rollout status deployment/keda-operator-metrics-apiserver --timeout=90s

echo "ligando o ScaledObject de lunchrush-worker no chart da aplicação"
GATEWAY_IP=$(docker network inspect "kind" --format '{{ (index .IPAM.Config 0).Gateway }}')
helm upgrade --install lunchrush deploy/helm/lunchrush \
  --kube-context "kind-$CLUSTER_NAME" \
  --set "externalInfra.hostGatewayIP=${GATEWAY_IP}" \
  --set keda.enabled=true

echo "pronto. status do ScaledObject:"
kubectl --context "kind-$CLUSTER_NAME" -n lunchrush get scaledobject lunchrush-worker
