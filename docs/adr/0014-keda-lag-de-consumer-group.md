# ADR 0014: KEDA escalando lunchrush-worker por lag do consumer group

## Contexto

`lunchrush-worker` é um consumidor Kafka (Redpanda no laboratório, ver ADR
0011) do tópico `lunchrush.delivery-events`. Um `HorizontalPodAutoscaler`
de CPU (usado em `delivery-api` e `tracking-ingest`) não é o sinal certo
para este workload: CPU alta pode significar processamento pesado com
fila vazia, e fila cheia pode não gerar CPU alta se o consumidor está
bloqueado em I/O. O sinal que importa de verdade é o lag do consumer
group: quantas mensagens ainda não foram processadas.

## Decisão

Instalar o KEDA (`kedacore/keda`, chart oficial) no cluster `kind`, num
namespace próprio (`keda`, separado de `lunchrush`), e substituir o HPA de
`lunchrush-worker` por um `ScaledObject` com trigger `kafka`, apontando
para o tópico e consumer group reais (não simulados): `lagThreshold: 5`,
`minReplicaCount: 0` (escala a zero quando não há trabalho, algo que um
HPA de CPU não faz), `maxReplicaCount: 6`, `pollingInterval: 5s`,
`cooldownPeriod: 30s`. Ver `deploy/helm/lunchrush/templates/workloads.yaml`
(bloco condicionado a `keda.enabled` e `workload.keda: true`) e
`scripts/keda-install.sh`.

## Bug encontrado: DNS entre namespaces, mesmo problema do ADR 0011 numa forma nova

O `ScaledObject` ficou preso em `KEDAScalerFailed` com
`lookup redpanda on ...: server misbehaving`, apesar de
`bootstrapServers` apontar para o FQDN completo
(`redpanda.lunchrush.svc.cluster.local:9092`). Causa: um cliente Kafka
nunca fala só com o endereço de bootstrap — depois do primeiro contato, o
broker devolve metadados dizendo qual endereço usar para cada partição, e
o cliente reconecta usando *esse* endereço. O Redpanda do docker compose
anuncia a si mesmo como `redpanda:9092` (nome curto, ver ADR 0011). O
operador do KEDA roda no namespace `keda`, não `lunchrush`; o nome curto
`redpanda` resolve via search domain só dentro do namespace de quem
pergunta, então o pod do operador, ao seguir o metadado do broker,
tentava resolver `redpanda` sozinho e falhava exatamente como um pod do
`kind` falhava contra o Redpanda do compose no ADR 0011, só que agora
entre dois namespaces do mesmo cluster.

Resolvido com um `Service` do tipo `ExternalName` no namespace `keda`,
apontando para `redpanda.lunchrush.svc.cluster.local` (ver
`deploy/helm/lunchrush/templates/keda-cross-namespace-dns.yaml`).
`ExternalName` é a ferramenta certa aqui porque o alvo já é um nome DNS
válido dentro do cluster, diferente do `external-infra.yaml` (ADR 0011),
onde o alvo é um IP e por isso precisou de `Endpoints` manuais.

## Evidência: escala de 0 para 3 réplicas por lag artificial

Com `lunchrush-worker` em 0 réplicas (`minReplicaCount: 0`, sem lag),
produzido lag artificial publicando 40 mensagens diretamente no tópico
via `rpk topic produce` (mensagens de teste, não eventos de negócio
válidos: o handler as descarta com `WARN` no log, sem ir para a DLQ,
porque falha de decodificação é tratada como aviso, não erro de negócio,
ver `cmd/lunchrush-worker/main.go`):

```text
$ rpk group describe lunchrush-worker
TOTAL-LAG    40

$ kubectl -n lunchrush get scaledobject lunchrush-worker
READY   ACTIVE
True    True

# ~60s depois (pollingInterval 5s, cooldownPeriod 30s):
$ kubectl -n lunchrush get deploy lunchrush-worker
READY   3/3

$ kubectl -n lunchrush get hpa keda-hpa-lunchrush-worker
TARGETS         MINPODS   MAXPODS   REPLICAS
2667m/5 (avg)   1         6         3

Events:
  Normal  KEDAScaleTargetActivated  Scaled apps/v1.Deployment lunchrush/lunchrush-worker from 0 to 1, triggered by s0-kafka-lunchrush-delivery-events
```

Depois que os 3 consumidores drenaram o lag (`TOTAL-LAG` voltou a `0`),
o `cooldownPeriod` de 30s levaria o `ScaledObject` de volta a 0 réplicas
sem lag ativo — não capturado em evidência separada porque o
comportamento simétrico (desativar por ausência de lag) já está no mesmo
mecanismo documentado pelo evento `KEDAScaleTargetDeactivated`, visto
antes da injeção de lag. Evidência completa em
`docs/benchmarks/tier-4-keda-evidencia.txt`.

## Limitação: versão do Kubernetes

O chart `kedacore/keda` instalado (KEDA 2.20) declara suporte formal a
partir do Kubernetes 1.33; o `kind` usado neste laboratório roda 1.31
(`kindest/node:v1.31.0`, a mesma imagem já usada desde o tier 3). O
`helm install` avisa isso explicitamente
(`WARNING - Running on unsupported Kubernetes version "1.31"`) e segue
funcionando: o `ScaledObject` ficou `Ready: True` e escalou corretamente.
Registrado como limitação conhecida, não como falha escondida.

## Alternativas consideradas

- **HPA de CPU também para lunchrush-worker:** rejeitada pelo motivo do
  Contexto: CPU não é o sinal que representa trabalho pendente para um
  consumidor Kafka.
- **KEDA no mesmo namespace `lunchrush`:** rejeitada; o padrão do próprio
  chart oficial do KEDA é operar cluster-wide a partir de um namespace
  próprio, e mistura-lo com os workloads da aplicação dificultaria
  remover o KEDA inteiro sem afetar `lunchrush`.

## Consequências

- `lunchrush-worker` agora escala de 0 a 6 réplicas puramente por lag,
  sem HPA de CPU concorrente (os dois nunca coexistem no mesmo
  `Deployment`: o chart só cria `HorizontalPodAutoscaler` quando
  `keda.enabled` é falso, ver `values.yaml:workloads.lunchrush-worker.hpa.enabled: false`);
- qualquer novo consumidor Kafka adicionado a este cluster com KEDA
  ligado herda a necessidade do mesmo `Service` `ExternalName` cross
  namespace até que o KEDA e a aplicação dividam namespace (não
  recomendado) ou o Redpanda vire um serviço gerenciado de verdade (fora
  de escopo local, ver `docs/limitacoes-simulacao-local.md`).

## Status

Aceita.
