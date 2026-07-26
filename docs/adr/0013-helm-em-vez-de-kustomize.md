# ADR 0013: Helm no lugar de Kustomize para os workloads do tier 4

## Contexto

`deploy/kubernetes/base/` (Kustomize, tier 3) tinha cinco manifests quase
idênticos: `delivery-api.yaml`, `dispatch-worker.yaml`,
`tracking-ingest.yaml`, `tracking-projector.yaml` e
`notification-worker.yaml`. A diferença entre eles era pequena (porta,
recursos, se tem `readinessProbe`, se tem `PodDisruptionBudget`, se tem
`HorizontalPodAutoscaler`), mas cada um repetia o `Deployment` e o
`Service` inteiros. O tier 4 pede Helm explicitamente ("Helm para
empacotar a repetição já conhecida nos manifests do tier 3"), então este
ADR decide como.

## Decisão

Um único chart em `deploy/helm/dispatch/`, com um template
(`templates/workloads.yaml`) que itera sobre `values.yaml:workloads` (um
mapa por serviço) e gera `Deployment` + `Service` + opcionalmente
`PodDisruptionBudget`, `HorizontalPodAutoscaler` e `ScaledObject` do KEDA
(ver ADR 0014) por entrada. Isso reduz a repetição de verdade: adicionar
um sexto workload é uma entrada nova em `values.yaml`, não um sexto
arquivo YAML de 80 linhas.

`deploy/kubernetes/` (Kustomize) permanece no repositório como registro
histórico do tier 3 (os docs `docs/passo-a-passo/tier-3.md` e
`docs/benchmarks/tier-3-baseline.md` citam `kind-deploy.sh` e os manifests
de lá); não é reescrito nem apagado, só deixa de ser o caminho usado a
partir do tier 4. `scripts/helm-deploy.sh` é o script novo, equivalente ao
`scripts/kind-deploy.sh` do tier 3, trocando `kubectl apply -k` por
`helm upgrade --install`.

## Bug encontrado durante a validação: documentos YAML sem separador se fundem

A primeira versão do template não tinha `---` no início de cada iteração
do `range`. Resultado: o documento YAML do `HorizontalPodAutoscaler` de um
workload, sem separador antes do próximo `Deployment` do laço seguinte,
virava **um único documento** aos olhos do parser YAML, e como
`apiVersion`, `kind`, `metadata` e `spec` são chaves repetidas dentro
desse documento combinado, o parser ficava com a última ocorrência de cada
chave, ou seja: o HPA desaparecia silenciosamente, substituído pelo
`Deployment` seguinte. `helm lint`, `helm template` e até `kubectl apply
--dry-run=client` não acusam esse tipo de erro (o YAML gerado é
sintaticamente válido, só semanticamente errado); só apareceu ao rodar
`kubectl get hpa` contra o cluster real e ver zero recursos, apesar do
`helm template` mostrar dois `kind: HorizontalPodAutoscaler` no texto
gerado.

Corrigido adicionando `---` logo após o `{{- range }}`, garantindo que
toda iteração comece com um separador de documento, independente de quais
blocos condicionais (`pdb`, `hpa`, `keda`) a iteração anterior tenha
emitido por último.

## Evidência

Deploy real no cluster kind `dispatch` (nome escolhido para não colidir
com o cluster `edge-lab` do outro laboratório no mesmo host):

```text
$ helm lint deploy/helm/dispatch
==> Linting deploy/helm/dispatch
1 chart(s) linted, 0 chart(s) failed

$ helm upgrade --install dispatch deploy/helm/dispatch --kube-context kind-dispatch \
    --set externalInfra.hostGatewayIP=172.19.0.1
STATUS: deployed

$ kubectl --context kind-dispatch -n dispatch get deploy
delivery-api          2/2
dispatch-worker       2/2
notification-worker   2/2
tracking-ingest       2/2
tracking-projector    2/2

$ kubectl --context kind-dispatch -n dispatch get hpa
delivery-api      Deployment/delivery-api      cpu: <unknown>/70%   2   6
tracking-ingest   Deployment/tracking-ingest   cpu: <unknown>/70%   2   8
```

`cpu: <unknown>` é esperado: este `kind` não tem `metrics-server`
instalado (fora do escopo mínimo deste tier, já que o HPA de CPU não é o
autoscaler que importa aqui, KEDA por lag é, ver ADR 0014); o `HPA` existe
e está corretamente ligado ao `Deployment`, só sem métrica para agir.

Smoke test via `kubectl port-forward` contra o `delivery-api` do cluster
real: `GET /healthz` e `GET /readyz` responderam `200` (medido).

## Bug adicional encontrado e corrigido durante a validação

`tracking-ingest` e `tracking-projector` usavam `readinessProbe` apontando
para `/readyz`, copiado por engano do padrão de `delivery-api` (único
serviço que expõe esse endpoint, ver
`internal/platform/httpapi/server.go`). Os dois primeiros rollouts
travaram (`0 of 2 updated replicas are available`) com a probe devolvendo
`404`. Corrigido com um campo `readinessPath` por workload em
`values.yaml` (default `/healthz`, só `delivery-api` sobrescreve para
`/readyz`), igual ao que `deploy/kubernetes/base/tracking-ingest.yaml`
já fazia corretamente antes desta migração.

## Alternativas consideradas

- **Manter Kustomize e só adicionar `helm template` como wrapper:**
  rejeitada; o pedido do tier é usar Helm de verdade (repositório de
  charts, `values.yaml`, `helm upgrade --install`), não simular Helm em
  cima de Kustomize.
- **Um chart por workload:** rejeitada; os cinco serviços compartilham
  quase toda a estrutura (mesma imagem base, mesmo padrão de probes,
  mesma infra externa), um único chart parametrizado é mais fiel ao nível
  de repetição real do sistema.

## Consequências

- `deploy/kubernetes/` fica congelado como veio do tier 3, não recebe mais
  mudanças; qualquer novo workload ou ajuste de manifest a partir daqui
  entra em `deploy/helm/dispatch/values.yaml`;
- o bug de documentos YAML sem separador é uma lição geral sobre `range`
  em templates Helm: todo laço que gera múltiplos documentos precisa de
  `---` explícito no início de cada iteração, não só entre blocos
  condicionais dentro dela.

## Status

Aceita.
