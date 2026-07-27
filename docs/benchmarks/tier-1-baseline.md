# Baseline do tier 1

**Medido** em 2026-07-26, commit local (antes da tag `tier-1.0.0`), ambiente
`local` (docker-compose, PostgreSQL 17 em container único, sem limites de CPU
ou memória impostos), CPU do gerador e do sistema testado: a mesma máquina
(13th Gen Intel Core i9-13980HX). Execução sequencial, não paralela; um único
pool de conexões `pgxpool` sem `MaxConns` explícito.

## Correção

- `TestStateMachine_RandomWalks`: 100.000 sequências de 8 passos cada,
  transições aleatórias sobre todo o alfabeto de estados, nenhuma transição
  inválida aceita e nenhum estado terminal regrediu.
- `go test -fuzz=FuzzTransition -fuzztime=30s`: 18.049.426 execuções
  (~596 mil/s), nenhuma falha, nenhum panic.
- `go test -race ./...` e `go test -tags=integration -race ./test/integration/...`:
  sem data race relatada.
- `TestLunchRush_TwentyConcurrentAssignsProduceExactlyOne`: 20 tentativas
  concorrentes de aceite para a mesma entrega e o mesmo entregador produzem
  exatamente 1 atribuição, sempre, em execução repetida.
- `TestLunchRush_CourierCannotHoldTwoActiveDeliveries`: a constraint única de
  `deliveries.courier_id` impede a segunda atribuição ativa ao mesmo
  entregador.

## Desempenho (em memória, sem I/O)

| Benchmark | Resultado | Alocações |
| --- | --- | --- |
| `BenchmarkTransition` | 19,30 ns/op | 0 B/op, 0 allocs/op |
| `BenchmarkDelivery_Apply` | 185,9 ns/op | 400 B/op, 3 allocs/op |

## Desempenho (round trip real ao PostgreSQL local)

| Benchmark | Resultado | Alocações |
| --- | --- | --- |
| `BenchmarkDelivery_Create` (criação idempotente completa) | 1,098 ms/op | 1748 B/op, 42 allocs/op |
| `BenchmarkLunchRush_Assign` (aceite condicional) | 0,970 ms/op | 570 B/op, 17 allocs/op |

## Gargalo localizado

O perfil de CPU (`go tool pprof -top`) do benchmark contra o PostgreSQL local
mostra apenas **10,89% de utilização de CPU** ao longo dos 19,56 s de duração
da execução. Das amostras coletadas, 34,27% ficaram em `runtime.futex` e
33,80% em `internal/runtime/syscall/linux.Syscall6`, com `pgproto3.Flush` e
`pgproto3.Receive` (protocolo do PostgreSQL) somando mais da metade do tempo
de CPU restante.

**Conclusão:** o caminho de criação e o de atribuição, no tier 1, não são
limitados por CPU da aplicação. Eles são limitados pelo round trip de rede
até o PostgreSQL, executado sequencialmente sobre um pool sem paralelismo
configurado no benchmark. Isso é esperado e coerente com a decisão do ADR
0001: o tier 1 não tenta otimizar um caminho que ainda não tem carga real
medida. O próximo limite conhecido está descrito em
`tier-1-what-breaks-next.md`.

## LoadGen (carga semântica de caixa preta)

`go run ./cmd/loadgen` contra o `delivery-api` real, com jornada completa
(criar, marcar pronta, oferecer, aceitar/recusar/expirar, coletar, concluir):

- `loadgen-tier-1.md`: 200 ordens, pool de 20 entregadores, concorrência 20.
  161 concluídas, 20 recusadas, 19 expiradas, **0 erros**. 40 repetições de
  chave de idempotência testadas, todas devolveram o mesmo ID.
- `loadgen-tier-1-pool-escasso.md`: mesmo cenário com pool de apenas 5
  entregadores para 150 ordens em concorrência 30, para forçar disputa real.
  240 tentativas de atribuição rejeitadas por entregador ocupado, todas
  absorvidas por retry no pool; 22 ordens esgotaram o pool de retry sem
  encontrar entregador livre, que é o comportamento correto sob demanda
  maior que oferta, não uma falha de aplicação.

Nenhuma dupla atribuição, nenhum efeito duplicado por chave repetida, em
nenhum dos dois cenários.

## k6 (smoke)

`loadtest/k6/smoke.js`, 5 VUs por 10s, jornada feliz completa por iteração
(criar, marcar pronta, oferecer, cadastrar e disponibilizar entregador,
atribuir, coletar, concluir, consultar): 430 iterações, 3.870 requisições,
**0% de falha**, p95 de 3,61 ms. Saída completa em `k6-smoke-tier-1.txt`,
resumo estruturado em `k6-smoke-tier-1.json`.
