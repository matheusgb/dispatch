# Tier 1 passo a passo

Este documento é a execução do tier 1 do começo ao fim, na ordem em que as
coisas acontecem. Cada passo tem três partes:

- **o comando**, para copiar e colar;
- **o que você vai ver**, para saber se deu certo;
- **o que roda por baixo**, uma descrição superficial do caminho no código.

O [README](../../README.md) explica os conceitos e apresenta os resultados
medidos. Aqui o objetivo é outro: acompanhar a máquina funcionando.

Você precisa de Go 1.26 ou mais novo e Docker com Compose.

---

## Passo 1: subir o PostgreSQL local

```bash
docker compose up -d postgres
```

**O que você vai ver:** o container `dispatch-postgres-1` como `healthy`.

```text
NAME                  IMAGE                STATUS
dispatch-postgres-1   postgres:17-alpine   Up (healthy)
```

**O que roda por baixo:** [docker-compose.yml](../../docker-compose.yml) sobe
um único PostgreSQL, sem réplica e sem limite de recursos: o tier 1 não
distribui nada ainda.

---

## Passo 2: aplicar as migrations

```bash
export DATABASE_URL="postgres://dispatch:dispatch@localhost:5432/dispatch?sslmode=disable"
go run ./cmd/migrate up
```

**O que você vai ver:**

```text
ok
```

**O que roda por baixo:** [cmd/migrate/main.go](../../cmd/migrate/main.go)
usa `golang-migrate` para aplicar, em ordem, os arquivos de
[migrations/](../../migrations/): a tabela de entregas com o `CHECK` dos nove
estados, o índice único parcial que impede um entregador com duas entregas
ativas, a trilha de auditoria e o ledger de idempotência.

---

## Passo 3: rodar os testes unitários da máquina de estados

```bash
make test
```

**O que você vai ver:** o pacote `internal/delivery` passando, incluindo
`TestStateMachine_RandomWalks`.

```text
ok  	github.com/matheusgb/dispatch/internal/delivery	0.03s
```

**O que roda por baixo:**
[internal/delivery/state.go](../../internal/delivery/state.go) declara o
grafo de transições permitidas. O teste em
[state_test.go](../../internal/delivery/state_test.go) gera 100 mil
sequências aleatórias de 8 passos cada e verifica que nenhuma transição fora
do grafo é aceita, e que nenhum estado terminal (`delivered`, `canceled`)
regride.

---

## Passo 4: rodar os testes de integração, incluindo a disputa concorrente

```bash
make test-integration
```

**O que você vai ver:** todos os testes de
[test/integration/](../../test/integration/) passando, entre eles
`TestDispatch_TwentyConcurrentAssignsProduceExactlyOne`.

```text
ok  	github.com/matheusgb/dispatch/test/integration	0.12s
```

**O que roda por baixo:** o teste dispara 20 goroutines chamando
`dispatch.Service.Assign` para a mesma entrega e o mesmo entregador ao mesmo
tempo. Cada chamada executa um `UPDATE ... WHERE state = 'offered'`
([internal/dispatch/dispatch.go](../../internal/dispatch/dispatch.go)): a
primeira transação a comitar move a linha para fora de `offered`, e todas as
outras encontram zero linhas para atualizar. Não existe lock explícito, o
próprio PostgreSQL serializa a disputa.

Rode com `-race` para confirmar a ausência de corrida na aplicação (o banco
já cobre a corrida de dados; o `-race` cobre o código Go em volta):

```bash
export DATABASE_URL="postgres://dispatch:dispatch@localhost:5432/dispatch?sslmode=disable"
go test -tags=integration -race ./test/integration/... -count=1
```

---

## Passo 5: subir a API

```bash
export DATABASE_URL="postgres://dispatch:dispatch@localhost:5432/dispatch?sslmode=disable"
go run ./cmd/delivery-api
```

**O que você vai ver:** um log estruturado confirmando a porta.

```json
{"time":"…","level":"INFO","msg":"delivery-api ouvindo","addr":":8080"}
```

**O que roda por baixo:**
[cmd/delivery-api/main.go](../../cmd/delivery-api/main.go) conecta ao pool do
PostgreSQL, monta o roteador de
[internal/platform/httpapi](../../internal/platform/httpapi) e inicia uma
goroutine que recicla ofertas vencidas a cada 5 segundos. `Ctrl+C` dispara o
graceful shutdown: `http.Server.Shutdown` espera as requisições em
andamento antes de encerrar.

Deixe rodando e abra outro terminal para os próximos passos.

---

## Passo 6: criar uma entrega idempotente

```bash
curl -s -X POST http://localhost:8080/deliveries \
  -H "X-Caller: order-service" -H "Idempotency-Key: pedido-1"
```

**O que você vai ver:**

```json
{"id":"…","state":"created"}
```

Repita exatamente o mesmo comando. **O que você vai ver:** o mesmo `id`, sem
criar uma segunda entrega.

**O que roda por baixo:**
[internal/platform/idempotency/idempotency.go](../../internal/platform/idempotency/idempotency.go)
procura a chave `(order-service, create_delivery, pedido-1)` no ledger antes
de inserir a entrega. Se já existir com o mesmo hash de payload, devolve a
resposta gravada; se existir com outro hash, devolve conflito (`409`) sem
tocar no estado.

---

## Passo 7: cadastrar e disponibilizar um entregador

```bash
curl -s -X POST http://localhost:8080/couriers \
  -H "Content-Type: application/json" -d '{"name":"ana"}'
```

**O que você vai ver:** `{"id":"…","name":"ana","available":false}`. Um
entregador nasce indisponível.

```bash
curl -s -X POST http://localhost:8080/couriers/<COURIER_ID>/availability \
  -H "Content-Type: application/json" -d '{"available":true}'
```

**O que você vai ver:** `204 No Content`.

---

## Passo 8: oferecer e atribuir a entrega

O tier 1 ainda não tem o `dispatch-worker` que move `created` para
`ready_for_dispatch` automaticamente; isso é feito manualmente aqui para
observar o fluxo.

```bash
psql "$DATABASE_URL" -c "UPDATE deliveries SET state='ready_for_dispatch' WHERE id='<DELIVERY_ID>'"

curl -s -o /dev/null -w "offer: %{http_code}\n" \
  -X POST http://localhost:8080/deliveries/<DELIVERY_ID>/offer

curl -s -o /dev/null -w "assign: %{http_code}\n" \
  -X POST http://localhost:8080/deliveries/<DELIVERY_ID>/assign \
  -H "Content-Type: application/json" -d '{"courier_id":"<COURIER_ID>"}'

curl -s http://localhost:8080/deliveries/<DELIVERY_ID>
```

**O que você vai ver:** `offer: 204`, `assign: 204` e, na consulta final,
`"state":"assigned"` com o `courier_id` atribuído.

---

## Passo 9: ler as métricas

```bash
curl -s http://localhost:8080/metrics | grep dispatch_
```

**O que você vai ver:** os contadores de negócio
(`dispatch_deliveries_created_total`, `dispatch_deliveries_assigned_total`,
`dispatch_assignment_conflicts_total`) e o histograma RED
(`dispatch_http_request_duration_seconds`), sem nenhum deles usando
`delivery_id` ou `courier_id` como label.

---

## Passo 10: rodar os benchmarks com profiling

```bash
go test ./internal/delivery/... -run=^$ -bench=. -benchmem \
  -cpuprofile=/tmp/cpu.pprof -memprofile=/tmp/mem.pprof

export DATABASE_URL="postgres://dispatch:dispatch@localhost:5432/dispatch?sslmode=disable"
go test -tags=integration ./test/integration/... -run=^$ -bench=. -benchmem \
  -cpuprofile=/tmp/dispatch-cpu.pprof

go tool pprof -top -nodecount=12 /tmp/dispatch-cpu.pprof
```

**O que você vai ver:** os números reproduzidos em
[docs/benchmarks/tier-1-baseline.md](../benchmarks/tier-1-baseline.md), e no
`pprof`, a maior parte do tempo em `runtime.futex` e chamadas de syscall do
protocolo do PostgreSQL, não em código da aplicação: o gargalo é o round
trip de rede, não CPU.

---

## Passo 11: rodar o smoke test em k6

```bash
BASE_URL=http://localhost:8080 k6 run loadtest/k6/smoke.js
```

**O que você vai ver:** 5 VUs por 10 segundos, cada iteração passando pela
jornada completa (criar, marcar pronta, oferecer, cadastrar entregador,
disponibilizar, atribuir, coletar, concluir, consultar), com
`checks_succeeded: 100.00%`.

**O que roda por baixo:** [loadtest/k6/smoke.js](../../loadtest/k6/smoke.js)
é caixa preta: só fala HTTP, não conhece o domínio. Ele existe para validar
que o script e o ambiente funcionam antes de qualquer cenário maior (load,
spike, breakpoint, soak), que ainda não existem neste tier.

---

## Passo 12: rodar o LunchRush

```bash
go run ./cmd/lunchrush -base-url http://localhost:8080 \
  -orders 200 -couriers 20 -concurrency 20 -out lunchrush-report
```

**O que você vai ver:**

```text
concluídas=161 declinadas=20 expiradas=19 erros=0 duração=5.3s
relatório em lunchrush-report.json e lunchrush-report.md
```

**O que roda por baixo:**
[cmd/lunchrush](../../cmd/lunchrush) conhece o domínio, diferente do k6: ele
decide por seed reproduzível se cada ordem vai ser concluída, recusada ou
vai expirar, testa repetição de chave de idempotência numa fração das
ordens e faz retry no pool de entregadores quando recebe `409` por
entregador ocupado. Para observar disputa real, rode com um pool menor que
a demanda:

```bash
go run ./cmd/lunchrush -base-url http://localhost:8080 \
  -orders 150 -couriers 5 -concurrency 30 -seed 999 -out lunchrush-contencao
```

Os `erros` desse segundo cenário não são bug: são o pool de retry se
esgotando porque a demanda passou a oferta, o que é o comportamento
correto sob a invariante 2. Ver
[docs/benchmarks/lunchrush-tier-1-pool-escasso.md](../benchmarks/lunchrush-tier-1-pool-escasso.md).

---

## Resumo da ópera

O tier 1 prova uma coisa difícil e pequena: vinte tentativas concorrentes
para o mesmo entregador resultam em exatamente uma atribuição, sem lock
explícito, só com um `UPDATE` condicional e uma constraint única do
PostgreSQL. A idempotência usa a mesma ideia, um ledger dentro da mesma
transação do efeito. Nada disso precisou de Kafka, Redis ou orquestração:
o modelo relacional, bem usado, já resolve a disputa que importa neste tier.
