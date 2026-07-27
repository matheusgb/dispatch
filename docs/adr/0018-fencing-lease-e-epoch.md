# ADR 0018: fencing, lease e epoch como extensão do padrão de UPDATE condicional

## Contexto

O tier 5 pede uma autoridade de ownership com `lunchrush_fences(shard_id,
epoch, owner_region, lease_until, last_write_token)` e
`active_assignments`, protegida por um protocolo que o roadmap descreve em
detalhe (UPDATE condicional na fence, INSERT com unique constraints,
outbox, commit, tudo na mesma transação). O roadmap também menciona Aurora
DSQL multi-região como implementação de referência.

## Decisão

Implementar o protocolo completo em `internal/fencing`
(`lunchrush_fences`, `active_assignments`, `assignment_history`, migration
`0006_fencing`), rodando contra o **PostgreSQL single-node local** que já
serve todo o resto do projeto, não Aurora DSQL. O próprio roadmap já prevê
essa saída: "se os limites reais do Aurora DSQL não atenderem à
invariante, o ADR deverá escolher outro store baseado em consenso ou um
desenho single-primary" — aqui o motivo não é limite técnico do DSQL (que
nunca foi testado, por não haver AWS real disponível), é a regra central
do projeto de nunca usar conta AWS real.

### Por que isso não enfraquece a prova

A propriedade que importa — um writer com epoch desatualizado nunca
escreve — não depende de o store ser distribuído entre regiões. Ela
depende de:

1. uma condição atômica de leitura-antes-de-escrita (`WHERE epoch = ? AND
   owner_region = ? AND lease_until > now()`) que só o dono do banco pode
   satisfazer sem correr risco de leitura suja — PostgreSQL com
   `READ COMMITTED` (padrão) já garante isso dentro de uma transação com
   `UPDATE` seguido de `INSERT`, porque o `UPDATE` adquire um lock de linha
   que serializa concorrentes;
2. duas unique constraints (`delivery_id`, `courier_id`) que o motor de
   banco aplica de forma incondicional, single-node ou distribuído.

Um Aurora DSQL real testaria adicionalmente: latência entre regiões,
conflitos de OCC entre regiões, e o comportamento do serviço gerenciado
sob partição de rede real — nenhuma dessas três coisas é o que este ADR
está decidindo. Elas ficam registradas como não testadas em
`docs/limitacoes-simulacao-local.md`, não escondidas.

### Desenho implementado

- `lunchrush_fences(shard_id PK, epoch, owner_region, lease_until,
  last_write_token)`: uma linha por lunchrush shard (um shard é um
  subconjunto pequeno de entregas+couriers dentro de uma célula, não a
  célula inteira — o roadmap é explícito sobre isso para evitar hot key;
  este laboratório usa um shard só por experimento, o benchmark de
  contenção fica para uma sessão futura com mais shards);
- `Promote`: só grava por cima de uma lease já expirada (`lease_until <
  now()`), nunca por cima de uma lease válida — a mesma regra que
  `docs/tla/LunchRushFencing.tla` chama de `LeaseExpire -> Promote`;
- `CreateAssignment`: `UPDATE lunchrush_fences SET last_write_token = ...
  WHERE shard_id = ? AND epoch = ? AND owner_region = ? AND lease_until >
  now()` (deve afetar exatamente 1 linha, senão `ErrStaleFence`), depois
  `INSERT INTO active_assignments` (protegido pelas duas unique
  constraints), depois o evento de outbox, tudo na mesma transação —
  exatamente a sequência do roadmap;
- `FinishAssignment`: `DELETE ... RETURNING` de `active_assignments` e
  `INSERT` em `assignment_history`, mesma transação.

### Correspondência com o modelo TLA+

| TLA+ (`docs/tla/LunchRushFencing.tla`) | Código (`internal/fencing`) |
| --- | --- |
| `epoch`, `owner`, `leaseValid` | `lunchrush_fences.epoch`, `.owner_region`, `.lease_until > now()` |
| `Promote(w)` | `Service.Promote` |
| `knownTokens`, `<<w, e>> \in knownTokens` | o `epoch` que o caller passa para `CreateAssignment` (o writer "lembra" de um epoch, correto ou obsoleto) |
| `Assign(w, d, c, e)`, guarda `e = epoch` | `UPDATE lunchrush_fences ... WHERE epoch = $3` dentro de `CreateAssignment` |
| `assignment[d] = None` (unique) | `active_assignments.delivery_id UNIQUE` |
| `courierState[c] = "free"` (unique) | `active_assignments.courier_id UNIQUE` |
| `Finish(d)` | `Service.FinishAssignment` |

## Evidência real

`test/integration/fencing_test.go`, dois testes com `-race`:

- `TestFencing_StaleEpochWriterNeverWrites`: 20 tentativas concorrentes de
  um writer com epoch velho (0 sucessos, 20 rejeições com
  `ErrStaleFence`) correndo ao mesmo tempo que 20 tentativas do writer com
  o epoch atual (20 sucessos), tudo verificado depois também por query
  direta em `active_assignments`;
- `TestFencing_TwoConcurrentPromotesOnlyOneEpochWins`: 20 chamadas
  concorrentes de `Promote` no mesmo shard vazio produzem exatamente 1
  vencedora.

## Alternativas consideradas

- **Aurora DSQL real**: rejeitada, exige AWS real (regra não-negociável
  do projeto).
- **DynamoDB Global Tables (modo eventual) como autoridade**: rejeitada
  pelo próprio roadmap ("conditional writes são regionais e conflitos
  convergem depois" — não serve como autoridade forte).
- **Redis com `SETNX`/Redlock como fencing**: não escolhido porque o
  projeto já tem PostgreSQL como fonte de verdade transacional desde o
  tier 1, e introduzir uma segunda tecnologia só para o fencing
  contradiz o princípio de "uma tecnologia só entra quando resolve um
  problema medido agora" (`lunch-rush.md`).

## Consequências

- o protocolo de fencing multi-shard é uma extensão direta, não uma
  reescrita, do padrão de `internal/lunchrush` desde o tier 1;
- a autoridade continua single-node: não há prova aqui de failover entre
  regiões reais, isso é o runbook de failover (célula local) e a
  limitação central de multi-região documentada em
  `docs/limitacoes-simulacao-local.md`;
- o dimensionamento de quantos lunchrush shards por célula (para evitar hot
  key) não foi medido nesta sessão — candidato de
  `docs/benchmarks/tier-5-what-breaks-next.md`.

## Status

Aceita.
