# ADR 0020: LoadGen ganha rede e relógio virtuais reaproveitando os handlers reais

## Contexto

O tier 5 pede que o LoadGen ganhe relógio e rede virtuais (drop, atraso,
duplicação, reorder, crash, restart, clock skew, partição control/data
plane, failover no pior momento), reutilizando os handlers reais do
domínio em vez de reimplementar lógica de negócio no simulador.

## Decisão

`cmd/loadgen/netfault.go` decide, a partir do mesmo `rand.Rand` seedado
por ordem que já decide decline/expire/duplicate (`scenario.go`, desde o
tier 1), como perturbar o trajeto de GPS de cada entrega simulada:

- **drop**: um ponto planejado nunca é enviado (`dropped[i] = true`);
- **atraso**: `time.Sleep` determinístico antes de cada envio
  (`--net-delay-ms`/`--net-delay-jitter-ms`);
- **duplicação**: um ponto é reenviado logo depois do original;
- **reorder**: dois pontos adjacentes do mesmo epoch trocam de ordem de
  envio;
- **crash/restart**: a sessão de tracking do entregador reinicia no meio
  do trajeto: `tracking_session_epoch` sobe, `sequence` reinicia em 1,
  exatamente a mesma regra que o app real usaria ao reabrir
  (`internal/tracking`, tier 2);
- **clock skew**: ao final do trajeto, uma tentativa de reenviar a
  primeira posição do primeiro epoch (mais antiga que qualquer coisa já
  confirmada).

Toda perturbação vira uma chamada HTTP real contra o `tracking-ingest`/
`tracking-projector` reais (`client.recordPosition`/`currentPosition`,
inalterados). `netfault.go` não sabe nada sobre monotonicidade, dedup ou
constraints de banco: ele só decide **quais** chamadas fazer, em que
**ordem**, com que **epoch/sequence** e depois de que **atraso**. Quem
garante a invariante 7 (posição monotônica) continua sendo
`internal/tracking`, provado desde o tier 2. Isso é a diferença central
entre "simular uma falha de rede" (o que este ADR faz) e "simular o
sistema" (o que o roadmap proíbe: "evitando um simulador que prove um
sistema diferente do deploy").

### Verificador de histórico

`main.go` soma, no relatório (`summary.ClockSkewTried`,
`.ClockSkewSafe`), quantas tentativas de clock skew aconteceram e quantas
não regrediram a posição atual. Se `ClockSkewSafe < ClockSkewTried`, o
processo termina com código de saída 1 e uma mensagem explícita de
violação de invariante, o mesmo padrão que `Errors > 0` já usava, agora
também para a invariante de domínio sob rede virtual, não só para falha
de rede.

### Reprodutibilidade (Medido, não descrito)

Duas execuções idênticas (`--seed 20260726`, mesmas flags de rede
virtual), com o banco truncado entre as duas para eliminar o efeito da
idempotência real (ver seção abaixo), produziram resultados agregados
idênticos:

```text
run1: concluídas=32 declinadas=4 expiradas=4 erros=0
run2: concluídas=32 declinadas=4 expiradas=4 erros=0
```

E, campo a campo, depois de remover só `delivery_id` (gerado pelo servidor,
não pela seed) e `duration_ns` (tempo de parede, não determinístico): os
dois relatórios são **idênticos** (`docs/benchmarks/tier-5-loadgen-netfault/`,
comparação em Python registrada no benchmark). Em ambas as execuções: 96
posições enviadas, 10 descartadas pela rede virtual, 85 avanços de
projeção, 7 crashes de sessão simulados, 5 tentativas de clock skew, **5
de 5 seguras**.

### Achado real sobre idempotência (não um bug do simulador)

A primeira tentativa de reprodutibilidade, sem truncar o banco entre as
duas execuções, produziu 40 erros na segunda: a chave de idempotência
`loadgen-<seed>-<index>` devolveu, corretamente, a mesma entrega já
criada na primeira execução, mas essa entrega já tinha avançado de
estado, então `/ready` respondeu `409` (`lunchrush: entrega não está em
created`). Isso não é um defeito: é a idempotência do tier 1 funcionando
exatamente como desenhada (mesma chave, mesmo efeito, uma vez só). A seed
determina a *sequência de decisões*, não substitui o estado real do banco
entre execuções, por isso o protocolo de reprodutibilidade documentado
aqui exige `TRUNCATE` (ou um banco novo) entre execuções que devem ser
comparadas.

## O que não foi implementado neste tier

- **Partição control plane / data plane explícita**: o roadmap pede isso
  como um cenário de rede virtual; aqui, o efeito equivalente mais
  próximo é o teste de fencing (`internal/fencing`, ADR 0018) e o modelo
  TLA+ (ADR 0017), que já cobrem "writer sem acesso à autoridade" como um
  caso de primeira classe. Uma partição de rede *dentro do LoadGen*
  entre o cliente e os serviços exigiria um proxy de rede controlável
  (Toxiproxy já é usado para isso nos tiers 2-4); não foi cabeado ao
  LoadGen nesta sessão por tempo;
- **Failover no pior momento coordenado com o LoadGen**: o runbook de
  failover (`docs/runbooks/`) e o teste de fencing cobrem o failover em
  si; orquestrar um failover de fencing NO MEIO de uma execução do
  LoadGen (o "pior momento") fica como próximo passo natural, não
  feito aqui por escopo;
- **Milhões de operações**: a prova de reprodutibilidade acima usa 40
  ordens (~230 chamadas HTTP). O volume maior fica em
  `docs/benchmarks/tier-5-baseline.md`, com o número real alcançado
  nesta máquina, nunca extrapolado.

## Alternativas consideradas

- **Reimplementar a máquina de estados e a lógica de dedup dentro do
  LoadGen, para simular sem precisar de um servidor real no ar**:
  rejeitada explicitamente pelo próprio roadmap ("evitando um simulador
  que prove um sistema diferente do deploy"). O LoadGen continua
  caixa-preta quanto à lógica de negócio, caixa-branca só quanto ao
  protocolo HTTP e às invariantes que consegue observar de fora.
- **Toxiproxy para toda a injeção de falha em vez de lógica no cliente**:
  parcialmente usado nos tiers 2-4 para latência/partição de infra; para
  drop/duplicate/reorder por posição individual, controlar isso pelo
  próprio cliente é mais simples e mais determinístico (a decisão nasce
  do `rand.Rand` seedado, não de uma configuração de proxy externa que
  precisaria seed própria).

## Status

Aceita.
