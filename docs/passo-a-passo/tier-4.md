# Tier 4 passo a passo

Continuação do [tier 3](tier-3.md). Aqui entram Terraform contra
LocalStack, Helm no lugar de Kustomize, KEDA escalando por lag de
consumer group, chaos engineering contra o cluster real e SLOs como
código. Você precisa do que o tier 3 já pedia (`docker compose`, `kind`,
`kubectl`), mais `terraform`, `helm` e o AWS CLI (`pip install awscli`,
usado só contra LocalStack, nunca contra a AWS real).

---

## Passo 1: provisionar S3 e Secrets Manager com Terraform contra o LocalStack

```bash
docker compose --profile aws-lab up -d localstack
cd infra/terraform/environments/aws-lab
terraform init
terraform apply -auto-approve
```

**O que você vai ver:** o LocalStack sobe saudável em `localhost:4566`. O
`apply` cria um bucket S3 (`lunchrush-delivery-receipts`) e um segredo no
Secrets Manager (`lunchrush/jwt-secret`), cifrado por uma chave KMS
criada junto.

**O que roda por baixo:** o provider `hashicorp/aws` oficial, sem fork,
apontando para `http://localhost:4566` via `endpoints {}` (ver
`infra/terraform/environments/aws-lab/main.tf` e ADR 0012). Nenhuma conta
AWS real é tocada. As credenciais são as estáticas `test`/`test`, aceitas
só pelo LocalStack.

```bash
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1
aws --endpoint-url=http://localhost:4566 s3 ls
aws --endpoint-url=http://localhost:4566 secretsmanager list-secrets
```

**O que você vai ver:** o bucket e o segredo criados no passo anterior,
confirmando que o Terraform aplicou de verdade, não só validou sintaxe.

---

## Passo 2: subir a aplicação com o objectstore e o secrets manager ligados

```bash
docker compose --profile app --profile aws-lab up -d --build
```

**O que você vai ver:** `delivery-api` inicia lendo `LUNCHRUSH_JWT_SECRET`
do Secrets Manager, não da variável de ambiente, quando
`AWS_SECRETS_ENDPOINT` está setado. E sobe um comprovante para o S3 a
cada entrega concluída (`internal/platform/objectstore`).

**O que roda por baixo:** `cmd/delivery-api/main.go` chama
`secrets.ResolveJWTSecret` e `objectstore.New` na inicialização. Se
`AWS_SECRETS_ENDPOINT`/`AWS_S3_ENDPOINT` não estiverem setados, os dois
caem de volta ao comportamento do tier 3 sem erro (ver
`internal/platform/secrets/secretsmanager.go`).

---

## Passo 3: migrar (ou reaproveitar) o cluster `kind` e instalar via Helm

```bash
docker compose --profile app up -d postgres redis redpanda \
  dependency-simulator redpanda-topics migrate
./scripts/helm-deploy.sh
```

**O que você vai ver:** um cluster `kind` chamado `lunchrush` (criado se
não existir), as cinco imagens construídas e carregadas, e
`helm upgrade --install` aplicando `deploy/helm/lunchrush`. Ao final, os
cinco `Deployment` ficam `2/2` (ou o número configurado em
`values.yaml:workloads`).

**O que roda por baixo:** um único chart parametrizado
(`deploy/helm/lunchrush`) substitui os cinco manifests quase idênticos do
`deploy/kubernetes/base` (Kustomize, tier 3, congelado como histórico).
`templates/workloads.yaml` itera sobre `values.yaml:workloads` para gerar
`Deployment`/`Service`/`PodDisruptionBudget`/`HorizontalPodAutoscaler`
por serviço (ver ADR 0013).

```bash
kubectl --context kind-lunchrush -n lunchrush get deploy,svc,hpa,pdb
```

**O que você vai ver:** os cinco `Deployment`, os `Service` internos e
externos (`postgres-external`, `redis-external`, `redpanda-external`,
`redpanda`), dois `HorizontalPodAutoscaler` (`delivery-api`,
`tracking-ingest`) e dois `PodDisruptionBudget`.

---

## Passo 4: instalar o KEDA e escalar `lunchrush-worker` por lag real

```bash
./scripts/keda-install.sh
```

**O que você vai ver:** o operador do KEDA instalado no namespace `keda`,
e um `ScaledObject` (`lunchrush-worker`) com `READY: True`. Sem lag no
tópico, `lunchrush-worker` fica em 0 réplicas (`minReplicaCount: 0`).

**O que roda por baixo:** o chart do KEDA oficial (`kedacore/keda`), mais
um `Service` `ExternalName` que este repositório cria no namespace `keda`
apontando para `redpanda.lunchrush.svc.cluster.local`. Sem esse
`Service`, o operador do KEDA, que roda fora do namespace `lunchrush`,
não resolve o nome curto `redpanda` que o broker anuncia de volta (ADR
0014, mesma causa raiz do ADR 0011).

```bash
for i in $(seq 1 40); do echo "teste-lag-$i"; done | \
  docker exec -i lunchrush-redpanda-1 rpk topic produce lunchrush.delivery-events -f '%v\n'
watch -n2 'kubectl --context kind-lunchrush -n lunchrush get scaledobject,hpa,deploy lunchrush-worker'
```

**O que você vai ver:** `TOTAL-LAG` de 40 no `rpk group describe
lunchrush-worker`, o `ScaledObject` marcando `ACTIVE: True`, e o
`Deployment` subindo de 0 para até 3 réplicas em menos de um minuto
(`pollingInterval: 5s`).

---

## Passo 5: rodar os cenários de chaos

Chaos engineering é a
prática de injetar falhas de propósito num sistema para verificar se ele
continua se comportando como esperado. Ver
`docs/benchmarks/chaos-tier-4.md` para os quatro cenários completos (pod
kill, falha do Redis, Redpanda pausado, latência via Toxiproxy no
PostgreSQL), com o comando exato de cada injeção e a evidência coletada.

**Atalho para o cenário mais simples (pod kill):**

```bash
kubectl --context kind-lunchrush -n lunchrush delete pod \
  $(kubectl --context kind-lunchrush -n lunchrush get pod -l app=delivery-api -o jsonpath='{.items[0].metadata.name}')
kubectl --context kind-lunchrush -n lunchrush get pods -l app=delivery-api -w
```

**O que você vai ver:** o pod morto sai de `Terminating`, um novo nasce em
`ContainerCreating` e chega em `1/1 Running` em segundos. O outro pod do
mesmo `Deployment` nunca parou de responder.

---

## Passo 6: subir o Prometheus com os SLOs como código

```bash
docker compose --profile observability up -d prometheus grafana
curl -s http://localhost:9090/api/v1/rules | python3 -m json.tool | grep '"name"'
```

**O que você vai ver:** seis grupos de regras carregados
(`slo-delivery-api-recording`, `slo-delivery-api-alerting`,
`slo-delivery-api-latency-recording`, `slo-delivery-api-latency-alerting`,
`slo-tracking-ingest-recording`, `slo-tracking-ingest-alerting`).

**O que roda por baixo:** `observability/prometheus/prometheus.yml` carrega
`observability/prometheus/rules/*.yml` (ver ADR 0015). Cada SLO tem
recording rules em quatro janelas (5m/30m/1h/6h) e dois alertas de
burn-rate (rápido e lento), técnica do Google SRE Workbook.

```bash
curl -s 'http://localhost:9090/api/v1/query?query=slo:delivery_api_errors:ratio_rate5m'
```

**O que você vai ver:** `0` com tráfego 100% bem-sucedido, não um vetor
vazio (ver o bug do `or on() vector(0)` documentado no ADR 0015).

---

## Encerrando

```bash
kind delete cluster --name lunchrush
docker compose --profile app --profile observability --profile aws-lab down
```

**O que você vai ver:** o cluster `kind` e todos os containers do
`docker compose` removidos. Nada fica cobrando fora deste laboratório:
não há conta AWS real envolvida em nenhum passo acima.
