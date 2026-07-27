# Baseline do tier 5

**Medido** em 2026-07-26, ambiente `local`: `docker compose --profile
app` (PostgreSQL 17, Redis 8, Redpanda v24.3.1, delivery-api,
lunchrush-worker, tracking-ingest, tracking-projector,
notification-worker), TLC 2.19 (`tla2tools.jar`), Go 1.26.

## Correção

- `go build ./...`, `gofmt -l .` e `go vet ./...` limpos em todo o código
  novo (`internal/fencing`, `cmd/cellrouter`, `cmd/loadgen/netfault.go`).
- `go test -race ./...` e `go test -tags=integration -race
  ./test/integration/...` passando, incluindo os dois testes novos de
  fencing (`TestFencing_StaleEpochWriterNeverWrites`,
  `TestFencing_TwoConcurrentPromotesOnlyOneEpochWins`) e toda a suíte
  herdada dos tiers 1-4.

## TLA+ real (TLC)

- `docs/tla/LunchRushFencing.tla`: **0 violações** em 1086 estados
  distintos (`Model checking completed. No error has been found.`),
  cobrindo `TypeInvariant`, `Safety` (sem dupla atribuição, sem writer
  antigo escrevendo, epoch monotônico) e a propriedade de vivacidade
  `EventuallyRecovers`;
- mutation test (`docs/tla/mutation/`): guarda de epoch removida de
  `Assign` → TLC encontra um contraexemplo real em 4 passos (writer
  auto-recuperado escreve com token obsoleto); modelo original, sem a
  mutação, não reproduz o contraexemplo;
- ver `docs/adr/0017-tla-real-para-o-protocolo-de-fencing.md`.

## Fencing multi-shard (código real)

- `internal/fencing`: 20 tentativas concorrentes de `CreateAssignment`
  com epoch desatualizado → **0 sucessos**, 20 rejeições
  (`ErrStaleFence`), confirmado por query direta em `active_assignments`
  depois do teste;
- 20 chamadas concorrentes de `Promote` no mesmo shard vazio → exatamente
  1 vencedora;
- ambos com `-race`, sem data race relatado;
- ver `docs/adr/0018-fencing-lease-e-epoch.md`.

## Arquitetura celular local

- duas células lógicas (`cell-a`, `cell-b`), roteamento por `X-Cell-ID`
  sem consulta a banco (`cmd/cellrouter`);
- isolamento de dados confirmado por query direta: uma entrega criada em
  `cell-a` tem `count(*) = 0` quando consultada no banco de `cell-b`, e
  vice-versa;
- noisy neighbor: `cell-a` saturada por k6 (40 VUs, 3.320 req/s, 31,68%
  de erro proposital) enquanto `cell-b` era sondada ao mesmo tempo: p95
  de `cell-b` subiu de **14ms (baseline) para 24ms (durante a
  sobrecarga)**, ~1,7x, não catastrófico, mas real e não escondido
  (isolamento lógico, não físico: mesmo processo PostgreSQL, mesma
  rede Docker);
- ver `docs/adr/0019-arquitetura-celular-local.md` e
  `docs/benchmarks/tier-5-cells/README.md`.

## LoadGen com rede e relógio virtuais

- reprodutibilidade: duas execuções com a mesma seed (`20260726`),
  banco truncado entre elas, produziram relatórios **idênticos campo a
  campo** (exceto `delivery_id`, gerado pelo servidor, e `duration_ns`,
  tempo de parede);
- sob rede virtual (drop 15%, atraso 20-50ms, duplicação 30%, reorder
  30%, clock skew 30%, crash de sessão 20%): 40 ordens, 32 concluídas, 0
  erros, **5 de 5 tentativas de clock skew seguras** (nenhuma regrediu a
  posição atual: invariante 7 preservada sob rede adversarial real, não
  só em unit test);
- ver `docs/adr/0020-loadgen-rede-e-relogio-virtuais.md` e
  `docs/benchmarks/tier-5-loadgen-netfault/README.md`.

## Soak reduzido (volume real, não extrapolado)

O critério de conclusão do tier 5 original pede mais de 100 milhões de
eventos em 24 horas, contra AWS real. Esta máquina compartilhada e local
não sustenta esse volume nem essa duração de forma honesta: a redução
está declarada aqui, não escondida.

**Medido**, `docs/benchmarks/tier-5-soak/soak-reduzido.{json,md}`, seed
`20260726777`, 2000 ordens, pool de 300 entregadores, concorrência 20,
rede virtual ativa (drop 5%, atraso 5-15ms, duplicação/reorder/clock skew
10%, crash de sessão 5%), `docker compose --profile app` local:

| Métrica | Valor |
| --- | --- |
| duração total | 4min53s |
| entregas concluídas | 1.800 / 2.000 (90%) |
| recusadas / expiradas | 100 / 99 |
| erros | 1 (timeout de projeção em 1 de 2000 trajetos, não uma violação de invariante) |
| posições de GPS enviadas | 5.295 |
| posições descartadas pela rede virtual | 286 |
| posições que avançaram a projeção | 5.180 |
| crashes de sessão simulados | 85 |
| tentativas de clock skew | 171 |
| tentativas de clock skew seguras | **171 de 171 (100%)** |
| chaves de idempotência repetidas testadas | 203, todas devolveram o mesmo ID |
| taxa de operações de domínio | ~2.000 entregas em ~294s ⇒ ~6,8 entregas/s, cada uma gerando de 6 a 12 chamadas HTTP reais (criar, pronta, ofertar, atribuir, de 1 a 3 posições de GPS + polls, coletar, entregar) |

**Nenhuma violação de invariante observada**: 0 dupla atribuição, 0
posição regredida (171 de 171 tentativas de clock skew seguras), 0 efeito
duplicado nas 203 repetições de chave de idempotência.

**Distância honesta até a meta original do roadmap:** ~5.300 eventos de
GPS e ~2.000 ordens de lifecycle em 5 minutos locais é uma fração muito
pequena de "mais de 100 milhões de eventos em 24 horas" contra AWS real.
Extrapolar linearmente (ex.: "então em 24h daria X") não é uma alegação
que este documento faz: o gargalo real (courier pool, relay do outbox,
CPU compartilhada) não escala linearmente, como o item 2 de
`docs/benchmarks/tier-5-what-breaks-next.md` já mostra.

## O que este baseline não mede

- contenção entre múltiplos lunchrush shards (só um shard testado);
- failover coordenado com carga do LoadGen rodando ao mesmo tempo (o
  "pior momento" do roadmap);
- latência real entre regiões (nenhuma região AWS real disponível);
- réplica cross-region de Kafka (MSK Replicator, fora de alcance sem AWS
  real).

Mapa completo em `docs/benchmarks/tier-5-what-breaks-next.md`.
