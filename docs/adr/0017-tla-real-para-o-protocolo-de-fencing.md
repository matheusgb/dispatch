# ADR 0017: TLA+ real (TLC) para o protocolo de fencing, com mutation test

## Contexto

O tier 5 pede verificação formal do protocolo que impede split-brain de
atribuição entre células/regiões: dois writers concorrentes, lease
expirando, epoch, failover, rejeição de token antigo, handoff de courier.
O roadmap é explícito que TLA+ "não é uma simulação" e exige rodar o model
checker de verdade, não só escrever a especificação como texto.

## Decisão

Especificação em `docs/tla/LunchRushFencing.tla` (constantes: `Writers`,
`Deliveries`, `Couriers`, `MaxEpoch`), TLC real (`tla2tools.jar` 2.19,
baixado localmente, `docs/tla/tools/`, fora do controle de versão por ser
um binário de terceiros de 2,2MB, ver `.gitignore`), executado com
`java -jar tla2tools.jar -workers 4 -config LunchRushFencing.cfg
LunchRushFencing.tla`.

### O que o modelo representa

- **epoch e lease por shard** (`epoch`, `owner`, `leaseValid`): o mesmo
  desenho de `lunchrush_fences` do roadmap (`shard_id`, `epoch`,
  `owner_region`, `lease_until`);
- **`knownTokens`**: o conjunto de pares `<<writer, epoch>>` que cada
  writer já teve alguma vez. Cresce a cada `Promote`, mas tokens antigos
  nunca são removidos: isso modela mensagem atrasada, retry com estado
  velho e principalmente **auto-recuperação**: o mesmo writer perde a
  lease e volta a ser dono com um epoch maior, mas ainda pode ter uma
  escrita em trânsito com o epoch anterior;
- **`Assign(w, d, c, e)`**: cria um assignment usando o token `<<w, e>>`;
  a guarda `e = epoch` é o fencing real (equivale ao `UPDATE ... WHERE
  epoch = ?` do roadmap). `owner = w` sozinho não basta, e o mutation test
  abaixo prova exatamente isso;
- **handoff de courier**: `CourierHandoffStart/Confirm/Activate`, com
  `courierState` impedindo um novo `Assign` enquanto o courier está em
  `draining`/`handoff_confirmed`.

### Propriedades verificadas

- `Safety` (invariante): nenhuma entrega com dois assignments ativos,
  nenhum courier com dois assignments ativos, epoch nunca regride, e
  `staleAssignHappened` nunca vira `TRUE` (nenhuma escrita usou um token
  diferente do epoch vigente);
- `EventuallyRecovers` (vivacidade, propriedade `~>`): sempre que a lease
  cai, o sistema volta a ter lease válida, **sob duas premissas
  explícitas** documentadas no próprio módulo: fairness fraca de `Promote`
  por writer, e capacidade de promoção não esgotada (`epoch < MaxEpoch`,
  um teto artificial deste modelo finito: o `epoch` real do banco é um
  `bigint`, nunca esgota).

### Evidência real

```text
$ java -jar tools/tla2tools.jar -workers 4 -config LunchRushFencing.cfg LunchRushFencing.tla
...
Model checking completed. No error has been found.
4009 states generated, 1086 distinct states found, 0 states left on queue.
```

Saída completa em `docs/tla/tlc-output-correct.txt`.

### Mutation test

`docs/tla/mutation/LunchRushFencing_no_epoch_guard.tla` é uma cópia exata
do módulo com uma única linha removida: a guarda `e = epoch` de `Assign`.
Rodado com o mesmo `.cfg`, o TLC encontra um contraexemplo real em 4
passos: um writer perde a lease, se auto-promove (mesmo owner, epoch
maior), e ainda consegue gravar um assignment usando o token antigo,
porque a checagem restante (`owner = w`) sozinha não detecta o epoch
obsoleto. `staleAssignHappened` vira `TRUE`, violando `Safety`. Saída
completa em `docs/tla/mutation/tlc-output-mutation.txt`, leitura guiada em
`docs/tla/mutation/README.md`. O modelo original, sem a mutação, passa
sem erro no mesmo espaço de estados.

Esse contraexemplo é o motivo prático de o desenho de `internal/fencing`
validar epoch **e** owner_region **e** lease na mesma condição do `UPDATE`
(ver ADR 0018), nunca um substituto do outro: checar só o owner não
detecta um writer que se auto-recuperou mas ainda tem uma escrita
antiga em trânsito.

## O que o modelo não cobre

- múltiplos shards simultâneos (contenção entre shards é medida no
  benchmark de código, não aqui: um shard já basta para as propriedades
  de segurança do protocolo);
- o conteúdo do evento de outbox (fora do escopo do protocolo de
  ownership);
- particionamento de rede como topologia explícita: aproximado pelo fato
  de tokens antigos permanecerem utilizáveis em `knownTokens`, que é o que
  importa para a propriedade de segurança (o efeito observável de uma
  partição é uma escrita atrasada com epoch velho, exatamente o que o
  modelo testa).

## Alternativas consideradas

- **Não rodar o TLC, só descrever o protocolo em prosa:** rejeitada. O
  roadmap explicitamente distingue TLA+ real de simulação, e a primeira
  versão desta mesma especificação (antes de ser corrigida) tinha dois
  bugs de modelagem que só apareceram rodando o TLC de verdade: um
  invariante mal formulado (contava atribuições por identidade de writer
  em vez de por par delivery-courier, gerando falso positivo) e uma
  propriedade de vivacidade sem fairness suficiente (o TLC achou um
  comportamento onde o sistema só fazia handoff de courier para sempre,
  nunca promovendo). Os dois foram corrigidos com evidência do próprio
  TLC, não por inspeção manual.
- **PlusCal em vez de TLA+ puro:** TLA+ puro escolhido por já ser
  suficientemente pequeno (9 ações) para não precisar da camada de
  tradução do PlusCal.

## Consequências

- o protocolo de fencing tem uma especificação formal pequena, executável,
  com mutation test real provando que a especificação captura a
  propriedade que importa (senão o mutation test também passaria sem
  achar nada);
- o espaço de estados é pequeno de propósito (2 writers, 2 entregas, 2
  couriers, `MaxEpoch = 4`, 1086 estados) para caber nesta máquina
  compartilhada em segundos; não prova nada sobre 3+ shards, cardinalidade
  maior de couriers/entregas, ou tempos reais de lease: isso é papel do
  simulador determinístico (ADR do LoadGen estendido) e do benchmark de
  código (`internal/fencing`).

## Status

Aceita.
