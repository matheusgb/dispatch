# Tier 2 passo a passo

Continuação do [tier 1](tier-1.md). Aqui o produto ganha tracking de GPS,
Redis, autenticação, rate limit, notificação simulada e observabilidade.
Você precisa de Go 1.26+, Docker com Compose e `k6` (instale com o método
oficial do seu sistema; usei o binário estático da release em
`github.com/grafana/k6/releases`).

---

## Passo 1: subir a stack completa

```bash
docker compose --profile app --profile observability up -d --build
```

**O que você vai ver:** sete containers no ar: `postgres`, `redis`,
`migrate` (roda e sai, `Exited (0)`), `dependency-simulator`,
`delivery-api`, `prometheus`, `grafana`.

**O que roda por baixo:** três `Dockerfile` multi-stage em
[deploy/compose/](../../deploy/compose/), todos terminando em
`gcr.io/distroless/static-debian12:nonroot`. O binário final não tem shell,
não tem gerenciador de pacotes e roda como usuário sem privilégio.

Se as portas `8080` ou `8090` já estiverem ocupadas no seu host por outro
projeto, o `docker-compose.yml` já publica `delivery-api` em `8083` e
`dependency-simulator` em `8092` por esse motivo.

---

## Passo 2: ver o dashboard

Abra `http://localhost:3000` (Grafana, login anônimo habilitado só para
uso local) e `http://localhost:9090` (Prometheus). O dashboard "lunchrush:
RED e negócio" já vem provisionado, em
[observability/grafana/provisioning](../../observability/grafana/provisioning).

**O que roda por baixo:** o Prometheus faz scrape de
`delivery-api:8080/metrics` a cada 5s
([observability/prometheus/prometheus.yml](../../observability/prometheus/prometheus.yml)).
O Grafana lê o Prometheus como datasource, provisionado por arquivo, sem
clique manual.

---

## Passo 3: emitir um token e testar tracking

```bash
BASE=http://localhost:8083
DELIVERY=$(curl -s -X POST $BASE/deliveries -H "X-Caller: order-service" -H "Idempotency-Key: passo-3")
DELIVERY_ID=$(echo "$DELIVERY" | python3 -c "import json,sys;print(json.load(sys.stdin)['id'])")

TOKEN=$(curl -s -X POST $BASE/auth/tokens -H "X-Admin-Secret: compose-dev-admin-secret" \
  -H "Content-Type: application/json" -d '{"caller":"order-service"}' \
  | python3 -c "import json,sys;print(json.load(sys.stdin)['token'])")

curl -s -X POST $BASE/deliveries/$DELIVERY_ID/positions -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"tracking_session_epoch":1,"sequence":1,"latitude":-23.55,"longitude":-46.63}'

curl -s $BASE/deliveries/$DELIVERY_ID/position -H "Authorization: Bearer $TOKEN"
```

**O que você vai ver:** `{"current":true}` na ingestão, e a posição de
volta na consulta.

**O que roda por baixo:**
[internal/tracking/tracking.go](../../internal/tracking/tracking.go) faz
um `UPSERT` condicional: só substitui a posição atual se `(epoch, sequence)`
for estritamente maior que a guardada.
[internal/tracking/cache.go](../../internal/tracking/cache.go) tenta o
Redis primeiro na leitura e cai pro PostgreSQL sem erro se o Redis não
responder.

Tente o mesmo `GET` com um token de outro caller: a resposta é `403`. Sem
token nenhum: `401`.

---

## Passo 4: ver o stream em tempo real

```bash
curl -N $BASE/deliveries/$DELIVERY_ID/stream -H "Authorization: Bearer $TOKEN" &
curl -s -X POST $BASE/deliveries/$DELIVERY_ID/positions -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"tracking_session_epoch":1,"sequence":2,"latitude":-23.56,"longitude":-46.64}'
```

**O que você vai ver:** um `data: {...}` chegando na conexão do `curl -N`
assim que a segunda posição é aceita.

**O que roda por baixo:**
[internal/platform/sse/broker.go](../../internal/platform/sse/broker.go), um
broker em memória: cada assinante é um canal Go, e `Publish` manda para
todos sem bloquear. Funciona com uma réplica só do `delivery-api` (ver ADR
0004 sobre o limite disso).

---

## Passo 5: derrubar o Redis sem derrubar a leitura

```bash
docker compose stop redis
curl -s $BASE/deliveries/$DELIVERY_ID/position -H "Authorization: Bearer $TOKEN"
docker compose start redis
```

**O que você vai ver:** a consulta continua respondendo `200` com o valor
correto mesmo com o Redis parado. Ver `docs/runbooks/redis-indisponivel.md`.

---

## Passo 6: rodar os smoke tests em k6

```bash
BASE_URL=http://localhost:8083 k6 run loadtest/k6/smoke.js
BASE_URL=http://localhost:8083 ADMIN_SECRET=compose-dev-admin-secret k6 run loadtest/k6/tracking-smoke.js
```

**O que você vai ver:** `checks_succeeded: 100.00%` nos dois.

---

## Passo 7: rodar o LoadGen com GPS

```bash
LUNCHRUSH_ADMIN_SECRET=compose-dev-admin-secret go run ./cmd/loadgen \
  -base-url http://localhost:8083 -orders 30 -couriers 10 -concurrency 1 \
  -seed 555 -out /tmp/loadgen-tier2
```

**O que você vai ver:** `erros=0`, com posições enviadas e a contagem de
quantas avançaram a projeção de última posição.

Rode de novo com `-concurrency 5` e mais ordens: você vai ver `429` na
ingestão de GPS. Isso é esperado: o LoadGen usa uma única identidade
(`loadgen`) para todo o tracking, então a concorrência do teste compete
pelo mesmo bucket de rate limit. Ver `docs/benchmarks/tier-2-what-breaks-next.md`.

---

## Passo 8: chaos, matar o delivery-api no meio da carga

```bash
go run ./cmd/loadgen -base-url http://localhost:8083 -orders 80 -couriers 12 \
  -concurrency 8 -seed 9001 -out /tmp/loadgen-chaos &
sleep 2
docker kill lunchrush-delivery-api-1
sleep 2
docker compose up -d delivery-api
wait
```

**O que você vai ver:** boa parte das ordens em andamento durante a queda
falha (conexão recusada). Depois de religar, o banco mostra zero
entregadores com duas entregas ativas:

```bash
PGPASSWORD=lunchrush psql -h localhost -U lunchrush -d lunchrush -c \
  "SELECT courier_id, count(*) FROM deliveries WHERE state IN ('assigned','picked_up') GROUP BY courier_id HAVING count(*) > 1;"
```

**O que você vai ver:** `(0 rows)`. Ver `docs/benchmarks/chaos-tier-2.md`
para o experimento completo, incluindo a injeção de latência via
Toxiproxy.

---

## Resumo

O tier 2 mostra que dá pra ter tracking em tempo real, cache e
autenticação sem abrir mão de nenhuma garantia do tier 1: o Redis cai e a
leitura continua certa, o processo morre e nenhum entregador fica com duas
entregas, o rate limit segura carga mesmo quando ela vem de uma identidade
só (o que também expõe uma simplificação do próprio simulador). Nada disso
precisou de Kafka nem de mais de uma réplica: essa complexidade entra no
tier 3, quando houver evidência de que o tier 2 não aguenta mais.
