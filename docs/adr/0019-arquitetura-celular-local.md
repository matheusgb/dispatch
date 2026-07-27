# ADR 0019: arquitetura celular local, com isolamento lógico rotulado como tal

## Contexto

O tier 5 pede células geográficas: cada célula com compute, banco, log de
eventos e Redis próprios, mais um cell router que encontra a célula certa
sem consultar todas as bases. Rodar 2-3 stacks completos (Postgres + Redis
+ Kafka + 5 serviços cada) nesta máquina compartilhada, já apertada de
memória mesmo depois de o `edge-lab` ser parado (ver
`docs/limitacoes-simulacao-local.md`), não é viável sem sacrificar o resto
do trabalho desta sessão (TLA+, fencing, LoadGen).

## Decisão

Duas células lógicas (`cell-a`, `cell-b`), cada uma com seu próprio
processo `delivery-api`, cada uma com seu próprio banco de dados
PostgreSQL (`lunchrush_cell_a`, `lunchrush_cell_b`), **no mesmo container
PostgreSQL** que o resto do laboratório já usa. `cmd/cellrouter` é um
reverse proxy mínimo (Go, `net/http/httputil`) que lê `X-Cell-ID` e
encaminha para o backend certo, sem consultar nenhum banco — o "diretório
pequeno" do roadmap, implementado como mapa estático carregado de
variável de ambiente (`CELLS=cell-a=http://...,cell-b=http://...`), não
DynamoDB Global Tables (fora de alcance sem AWS real).

Deploy: `docker-compose.yml` original mais um override,
`deploy/compose/cells.yml`, profile `cells`, sem duplicar Postgres/Redis/
Kafka (reaproveita os já existentes do `docker compose --profile app`).

## Por que não 2-3 stacks completos

- **Memória**: cada stack completo (Postgres + Redis + Redpanda + 5
  serviços Go) já consome centenas de MB; três deles simultâneos, mais o
  stack principal ainda rodando para os outros experimentos desta sessão
  (TLC, k6, backup/recovery), ultrapassaria a memória disponível nesta
  máquina compartilhada;
- **O que a arquitetura celular precisa provar neste tier**: que o
  roteamento não faz full-scan, e que os dados de uma célula não vazam
  para outra. As duas propriedades são demonstráveis com bancos lógicos
  separados; Kafka e Redis por célula são replicação do mesmo padrão já
  provado nos tiers 2-4 (projeção descartável, outbox transacional), não
  uma questão nova que este tier precise reprovar;
- **Honestidade de rótulo**: o roadmap já aceita essa saída
  explicitamente ("o projeto poderá começar com células lógicas... ou
  rotula honestamente o resultado como isolamento lógico"). O teste de
  noisy neighbor (`docs/benchmarks/tier-5-cells/README.md`) mede
  exatamente o tamanho do vazamento causado por essa escolha (p95 de
  cell-b sobe de 14ms para 24ms quando cell-a é sobrecarregada de
  propósito) em vez de simplesmente declarar "isolado" sem medir.

## Evidência real

- roteamento por `X-Cell-ID` sem consulta a banco: `cmd/cellrouter/main.go`;
- isolamento de dados: uma entrega criada em `cell-a` tem `count(*) = 0`
  quando consultada em `lunchrush_cell_b`, e vice-versa (query real, não
  descrita, ver benchmark);
- noisy neighbor: 40 VUs de k6 saturando `cell-a` (3.320 req/s, 31,68% de
  erro proposital) enquanto uma sonda sequencial mede `cell-b` ao mesmo
  tempo: degradação real e pequena (p95 14ms → 24ms), não catastrófica,
  não escondida.

## Alternativas consideradas

- **Três stacks completos, um por célula**: rejeitada por memória, como
  descrito acima; documentada como direção futura se a máquina permitir.
- **`kind` com namespaces por célula**: mais fiel ao "múltiplos
  namespaces Kubernetes" citado no roadmap, mas o cluster `kind` deste
  laboratório (tier 3/4) já está fora de uso nesta sessão para poupar
  memória junto com o `edge-lab`; fica registrado como alternativa válida
  para uma sessão futura com mais fôlego de memória.
- **Kafka e Redis por célula neste experimento**: adiado; o padrão já
  está provado nos tiers 2-4 e reproduzi-lo aqui só multiplicaria
  containers sem testar uma propriedade nova do tier 5.

## Consequências

- o roteamento e o isolamento de dados por célula estão provados com
  execução real;
- o isolamento de recurso físico (CPU, I/O do Postgres compartilhado,
  rede Docker compartilhada) não está provado — está medido como
  vazamento pequeno e não perfeito, rotulado como tal;
- `cell_id` e `home_cell`/`courier_session_epoch` (migration 0006) já
  existem no schema para quando uma sessão futura decidir estender para
  3 células hard-isolated ou `kind` com namespaces.

## Status

Aceita.
