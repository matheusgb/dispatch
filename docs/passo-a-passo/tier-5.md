# Tier 5 passo a passo

Continuação do [tier 4](tier-4.md). Aqui entram fencing multi-shard, TLA+
real, arquitetura celular local e o LoadGen com rede/relógio virtuais.
Você precisa do que o tier 4 já pedia (`docker compose`), mais `java`
(17+, para o TLC) e opcionalmente `syft`/`grype`/`cosign` (fecham
pendências do tier 4, ver ADR 0016).

---

## Passo 1: rodar o TLA+ real do protocolo de fencing

```bash
mkdir -p docs/tla/tools
curl -sSfL https://github.com/tlaplus/tlaplus/releases/latest/download/tla2tools.jar \
  -o docs/tla/tools/tla2tools.jar
cd docs/tla
java -jar tools/tla2tools.jar -workers 4 -config LunchRushFencing.cfg LunchRushFencing.tla
```

**O que você vai ver:** `Model checking completed. No error has been
found.`, seguido de `4009 states generated, 1086 distinct states found`.

**O que roda por baixo:** o TLC (model checker da TLA+ Foundation) explora
todo o espaço de estados alcançável pela especificação (2 writers, 2
entregas, 2 couriers, epoch até 4) e verifica `TypeInvariant` e `Safety`
(nenhuma dupla atribuição, epoch nunca regride, nenhuma escrita com token
obsoleto) em cada estado, mais a propriedade de vivacidade
`EventuallyRecovers`. Ver ADR 0017.

```bash
cd mutation
java -jar ../tools/tla2tools.jar -workers 4 \
  -config LunchRushFencing_no_epoch_guard.cfg LunchRushFencing_no_epoch_guard.tla
```

**O que você vai ver:** `Error: Invariant Safety is violated.`, com um
contraexemplo de 4 estados: um writer se auto-recupera (mesmo owner,
epoch maior) e ainda escreve com um token de epoch antigo.

**O que roda por baixo:** a mesma especificação, com uma única guarda
removida de propósito (`e = epoch` em `Assign`). O contraexemplo prova que
a especificação original captura a propriedade que importa: se o
mutation test não achasse nada, o modelo estaria fraco demais para
significar alguma coisa.

---

## Passo 2: aplicar a migration de fencing e rodar o teste de concorrência

```bash
docker compose --profile app up -d
DATABASE_URL="postgres://lunchrush:lunchrush@localhost:5432/lunchrush?sslmode=disable" go run ./cmd/migrate up
DATABASE_URL="postgres://lunchrush:lunchrush@localhost:5432/lunchrush?sslmode=disable" \
  go test -tags=integration -race -run TestFencing ./test/integration/... -v
```

**O que você vai ver:** `TestFencing_StaleEpochWriterNeverWrites` e
`TestFencing_TwoConcurrentPromotesOnlyOneEpochWins` passando, sem nenhum
data race relatado.

**O que roda por baixo:** `internal/fencing` (ADR 0018) cria
`lunchrush_fences`, `active_assignments` e `assignment_history`
(migration `0006_fencing`); o primeiro teste dispara 20 tentativas de
`CreateAssignment` com um epoch velho ao mesmo tempo que 20 tentativas com
o epoch atual, contra o mesmo shard, e confirma no banco que zero
assignments têm o epoch velho.

---

## Passo 3: subir duas células locais e provar isolamento

```bash
docker exec lunchrush-postgres-1 psql -U lunchrush -d postgres -c "CREATE DATABASE lunchrush_cell_a OWNER lunchrush;"
docker exec lunchrush-postgres-1 psql -U lunchrush -d postgres -c "CREATE DATABASE lunchrush_cell_b OWNER lunchrush;"
DATABASE_URL="postgres://lunchrush:lunchrush@localhost:5432/lunchrush_cell_a?sslmode=disable" go run ./cmd/migrate up
DATABASE_URL="postgres://lunchrush:lunchrush@localhost:5432/lunchrush_cell_b?sslmode=disable" go run ./cmd/migrate up

docker compose -f docker-compose.yml -f deploy/compose/cells.yml \
  --profile app --profile cells up -d --build \
  delivery-api-cell-a delivery-api-cell-b cellrouter
```

**O que você vai ver:** três novos containers (`delivery-api-cell-a` na
porta 8094, `delivery-api-cell-b` na 8095, `cellrouter` na 8096).

```bash
curl -X POST http://localhost:8096/deliveries -H "X-Cell-ID: cell-a" \
  -H "X-Caller: demo" -H "Idempotency-Key: demo-a-1"
curl -X POST http://localhost:8096/deliveries -H "X-Cell-ID: cell-b" \
  -H "X-Caller: demo" -H "Idempotency-Key: demo-b-1"
```

**O que você vai ver:** dois `201`, com IDs diferentes.

**O que roda por baixo:** `cmd/cellrouter` lê `X-Cell-ID` e encaminha para
o backend certo sem consultar banco algum (ADR 0019). Consultando
`lunchrush_cell_a` e `lunchrush_cell_b` diretamente, cada entrega só existe
no banco da sua própria célula: isso é prova real de isolamento de dados,
não apenas descrição teórica.

---

## Passo 4: LoadGen com rede e relógio virtuais

```bash
go run ./cmd/loadgen \
  --base-url http://localhost:8083 --tracking-url http://localhost:8084 --projector-url http://localhost:8085 \
  --admin-secret compose-dev-admin-secret --seed 20260726 \
  --orders 40 --couriers 15 --concurrency 8 \
  --net-drop-rate 0.15 --net-delay-ms 20 --net-delay-jitter-ms 30 \
  --net-duplicate-rate 0.3 --net-reorder-rate 0.3 --net-clock-skew-rate 0.3 --net-crash-rate 0.2 \
  --out /tmp/run1
```

**O que você vai ver:**
`concluídas=32 declinadas=4 expiradas=4 erros=0`, seguido de
`clock_skew_tried=5 clock_skew_safe=5 couriers_crashed=7
positions_dropped=10`.

**O que roda por baixo:** `cmd/loadgen/netfault.go` decide, a partir do
`rand.Rand` seedado por ordem, quais posições de GPS enviar, em que
ordem, com que atraso e com que epoch (ADR 0020). Toda perturbação vira
uma chamada HTTP real contra `tracking-ingest`/`tracking-projector`; o
processo sai com código 1 se alguma tentativa de clock skew regredir a
posição atual (não aconteceu nesta execução).

Para provar reprodutibilidade, truncar o banco e rodar de novo com a
mesma seed:

```bash
psql "$DATABASE_URL" -c "TRUNCATE idempotency_keys, delivery_transitions, tracking_positions, \
  delivery_tracking_state, active_assignments, assignment_history, lunchrush_fences, \
  outbox_events, consumed_events, deliveries, couriers CASCADE;"
# repetir o comando acima com --out /tmp/run2
```

**O que você vai ver:** os mesmos números agregados.

**O que roda por baixo:** nada de mágico: a mesma seed produz a mesma
sequência de decisões do `rand.Rand`. Sem truncar o banco, a idempotência
real do tier 1 devolveria as mesmas entregas já criadas na primeira
execução (e já avançadas de estado), causando `409` na segunda. Isso é
um achado real documentado no ADR 0020, não um defeito do simulador.

---

## Limpeza

```bash
docker compose -f docker-compose.yml -f deploy/compose/cells.yml --profile cells \
  stop delivery-api-cell-a delivery-api-cell-b cellrouter
docker compose -f docker-compose.yml -f deploy/compose/cells.yml --profile cells \
  rm -f delivery-api-cell-a delivery-api-cell-b cellrouter
docker exec lunchrush-postgres-1 psql -U lunchrush -d postgres -c "DROP DATABASE lunchrush_cell_a;"
docker exec lunchrush-postgres-1 psql -U lunchrush -d postgres -c "DROP DATABASE lunchrush_cell_b;"
```
