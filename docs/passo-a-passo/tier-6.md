# Tier 6 passo a passo

Continuação do [tier 5](tier-5.md). Aqui entra o segundo "provedor"
simulado (`cloud-b`), o mesmo digest de imagem rodando nos dois stacks, e
o failover da autoridade de fencing entre eles. Você precisa do que o
tier 4 já pedia (`docker compose`, `terraform`), mais `jq` e `bc` (usados
pelos scripts de evidência) e `k6`. Nenhum passo aqui usa AWS real ou um
segundo provedor de nuvem real (ver `docs/limitacoes-simulacao-local.md`,
seção Tier 6).

---

## Passo 1: subir cloud-a e construir a imagem uma única vez

```bash
docker compose --profile app build
docker compose --profile app up -d
```

**O que você vai ver:** os 5 serviços de aplicação e as dependências
(`postgres`, `redis`, `redpanda`) saudáveis em `docker compose ps`.

**O que roda por baixo:** o mesmo build multi-stage de sempre
(`deploy/compose/Dockerfile.*`), produzindo imagens nomeadas
`dispatch-<serviço>` no daemon Docker local — esse nome é o que
`docker-compose.cloud-b.yml` vai reusar no próximo passo, sem rebuild.

## Passo 2: subir cloud-b apontando para a mesma imagem

```bash
docker compose -f docker-compose.cloud-b.yml --profile app up -d
```

**O que você vai ver:** um segundo conjunto de containers, prefixo
`cloudb-`, em portas diferentes (`18083` delivery-api, `18084`
tracking-ingest, `18085` tracking-projector, `15432` Postgres, `16379`
Redis, `19093`/`29093` Redpanda).

**O que roda por baixo:** `docker-compose.cloud-b.yml` não tem nenhuma
diretiva `build:` nos serviços de app — eles referenciam por nome a
imagem que o passo 1 já construiu. Confirme:

```bash
docker inspect dispatch-delivery-api-1 --format '{{.Image}}'
docker inspect cloudb-delivery-api-1 --format '{{.Image}}'
```

**O que você vai ver:** o mesmo `sha256:...` nas duas linhas.

## Passo 3: rodar o mesmo teste de contrato nos dois

```bash
BASE_URL=http://localhost:8083  k6 run loadtest/k6/smoke.js
BASE_URL=http://localhost:18083 k6 run loadtest/k6/smoke.js

DATABASE_URL="postgres://dispatch:dispatch@localhost:5432/dispatch?sslmode=disable" \
  KAFKA_BROKERS="localhost:19092" go test -tags=integration -race -count=1 ./test/integration/...
DATABASE_URL="postgres://dispatch:dispatch@localhost:15432/dispatch?sslmode=disable" \
  KAFKA_BROKERS="localhost:29093" go test -tags=integration -race -count=1 ./test/integration/...
```

**O que você vai ver:** 0% de erro no k6 e `ok` no `go test`, nos dois
ambientes, sem nenhuma diferença de código entre as duas execuções.

**O que roda por baixo:** o mesmo binário de teste, contra dois bancos
fisicamente diferentes — se o contrato observável do sistema dependesse
de algo específico do ambiente além das variáveis já suportadas, um dos
dois lados falharia.

## Passo 4: Terraform separado por provedor

```bash
docker compose --profile aws-lab up -d localstack
docker compose -f docker-compose.cloud-b.yml --profile aws-lab up -d localstack

cd infra/terraform/environments/cloud-a && terraform init && terraform apply -auto-approve
cd ../cloud-b && terraform init && terraform apply -auto-approve
```

**O que você vai ver:** `Apply complete! Resources: 8 added` nos dois,
com nomes de bucket/segredo diferentes (`dispatch-cloud-a-receipts` e
`dispatch-cloud-b-receipts`).

**O que roda por baixo:** dois roots Terraform independentes (`main.tf`,
`.tfstate` próprios), cada um apontado para o `localstack_endpoint` do seu
próprio stack (`:4566` e `:14566`) — dois "provedores" simulados, sem
nenhum recurso compartilhado entre os dois planos.

```bash
cd ../cloud-a && terraform destroy -auto-approve
cd ../cloud-b && terraform destroy -auto-approve
```

**O que você vai ver:** `Destroy complete! Resources: 8 destroyed` nos
dois.

## Passo 5: failover da autoridade de fencing entre cloud-a e cloud-b

```bash
DB_A="postgres://dispatch:dispatch@localhost:5432/dispatch?sslmode=disable"
DB_B="postgres://dispatch:dispatch@localhost:15432/dispatch?sslmode=disable"

go run ./cmd/cloudfailover seed -db "$DB_A" -n 30
go run ./cmd/cloudfailover promote -db "$DB_A" -shard tier6-crosscloud -region cloud-a -lease 8s
go run ./cmd/cloudfailover assign -db "$DB_A" -shard tier6-crosscloud -region cloud-a -epoch 1 -attempts 10

docker exec dispatch-postgres-1 pg_dump -U dispatch -Fc dispatch -f /tmp/t0.dump
docker cp dispatch-postgres-1:/tmp/t0.dump /tmp/t0.dump

go run ./cmd/cloudfailover assign -db "$DB_A" -shard tier6-crosscloud -region cloud-a -epoch 1 -attempts 5

docker compose stop delivery-api dispatch-worker

docker cp /tmp/t0.dump cloudb-postgres-1:/tmp/t0.dump
docker exec cloudb-postgres-1 dropdb -U dispatch dispatch
docker exec cloudb-postgres-1 createdb -U dispatch -O dispatch dispatch
docker exec cloudb-postgres-1 pg_restore -U dispatch -d dispatch --no-owner /tmp/t0.dump

go run ./cmd/cloudfailover promote -db "$DB_B" -shard tier6-crosscloud -region cloud-b -lease 60s
go run ./cmd/cloudfailover assign -db "$DB_B" -shard tier6-crosscloud -region cloud-a -epoch 1 -attempts 10
go run ./cmd/cloudfailover assign -db "$DB_B" -shard tier6-crosscloud -region cloud-b -epoch 2 -attempts 5
```

**O que você vai ver:** os 10 assignments do writer de `cloud-a` bem
sucedidos antes do backup, os 5 assignments depois do backup também bem
sucedidos (mas fora do dump), a restauração em `cloud-b` trazendo só os
10 primeiros, a promoção de `cloud-b` (epoch 1 → 2), 10/10 tentativas do
writer antigo rejeitadas com `ErrStaleFence`, e 5/5 tentativas do writer
novo aceitas.

**O que roda por baixo:** `cmd/cloudfailover` chama diretamente
`internal/fencing.Service` — o mesmo código do tier 5, sem nenhuma
alteração de protocolo. A promoção em `cloud-b` só funciona porque a
lease do fence restaurado (herdada de `cloud-a`) já expirou no relógio
real; se você repetir o passo com uma lease mais longa, `Promote` retorna
`ErrLeaseNotExpired`. Transcrição completa, com timestamps reais e o
RTO/RPO calculados: `docs/benchmarks/tier-6-portability/failover-transcript.txt`.

## Passo 6: revelar a dependência oculta

```bash
docker compose stop delivery-api
docker rmi -f dispatch-delivery-api
docker compose -f docker-compose.cloud-b.yml stop delivery-api
docker compose -f docker-compose.cloud-b.yml rm -f delivery-api
docker compose -f docker-compose.cloud-b.yml --profile app up -d delivery-api
```

**O que você vai ver:** `Error response from daemon: pull access denied
for dispatch-delivery-api ... requested access to the resource is
denied`.

**O que roda por baixo:** com a imagem removida do daemon (e nenhum
container de nenhum dos dois stacks mais a referenciando), `cloud-b` não
consegue recriar o container — a mesma falha que apareceria contra um
registry real fora do ar. Recupere com:

```bash
docker compose --profile app build delivery-api
docker compose --profile app up -d delivery-api
docker compose -f docker-compose.cloud-b.yml --profile app up -d delivery-api
```

## Passo 7: derrubar os dois stacks

```bash
docker compose -f docker-compose.cloud-b.yml --profile app --profile aws-lab down
docker compose --profile app --profile aws-lab --profile observability down
```

**O que você vai ver:** todos os containers de `cloud-a` e `cloud-b`
parados e removidos, as duas redes Docker desfeitas.

---

Isso fecha o tier 6 e o roadmap de seis tiers de `dispatch.md`. Ver
`docs/tier-6-matriz-portabilidade.md` para a matriz completa e
`docs/limitacoes-simulacao-local.md` para o que cada peça deste passo a
passo não prova.
