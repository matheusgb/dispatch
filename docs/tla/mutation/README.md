# Mutation test do protocolo de fencing

Ver ADR correspondente em `docs/adr/0019-tla-real-e-mutation-test.md`.

`LunchRushFencing_no_epoch_guard.tla` é uma cópia de `../LunchRushFencing.tla`
com uma única mudança deliberada: a ação `Assign` perde a guarda `e = epoch`
(o fencing token deixa de precisar bater com o epoch vigente da autoridade,
mesmo mantendo a checagem `owner = w`). O diff exato:

```diff
     /\ <<w, e>> \in knownTokens
-    /\ e = epoch                           \* fencing: o token tem que ser o vigente
     /\ assignment[d] = None                \* unique constraint: delivery_id
```

## Resultado real do TLC (não descrito, executado)

```text
$ java -jar ../tools/tla2tools.jar -workers 4 \
    -config LunchRushFencing_no_epoch_guard.cfg LunchRushFencing_no_epoch_guard.tla

Error: Invariant Safety is violated.
State 1: epoch=1, owner=w1, leaseValid=TRUE, knownTokens={<<w1,1>>}
State 2: LeaseExpire -> leaseValid=FALSE
State 3: Promote(w1) -> epoch=2, owner=w1 (auto-recuperação: o mesmo writer
         volta a ser dono), knownTokens={<<w1,1>>, <<w1,2>>}
State 4: Assign(w1, d1, c1, 1) -> aceito porque owner=w1 ainda bate e o token
         <<w1,1>> continua em knownTokens; sem a guarda e=epoch, ninguém
         percebe que 1 já não é o epoch atual (2). staleAssignHappened vira
         TRUE, violando Safety (NoStaleAssignEverHappened).
```

Saída bruta completa em `tlc-output-mutation.txt`.

## Leitura do contraexemplo

O cenário achado pelo TLC não é split-brain entre dois writers diferentes.
É mais sutil e mais realista: o mesmo writer `w1` perde a lease e se
auto-recupera. Isso é comum quando o processo só reiniciou e voltou a ser
eleito dono da mesma região.

Um comando de `Assign` que já estava formado com o epoch antigo, por
exemplo enfileirado antes do restart ou reenviado por um retry que não
observou a mudança de epoch, ainda é aceito. Isso acontece porque a única
coisa que a autoridade checava era se o writer que está pedindo é o dono
atual. Isso é verdade, mas insuficiente.

Isso confirma por que o desenho do roadmap (`docs/tla/LunchRushFencing.tla`,
seção "Implementação de referência" do `lunch-rush.md`) exige checar epoch
e owner_region na mesma condição do `UPDATE`, um não substitui o outro.

## Modelo corrigido

`../LunchRushFencing.tla` (a guarda completa) roda sem violação no mesmo
espaço de estados (`../tlc-output-correct.txt`, 1086 estados distintos,
`Model checking completed. No error has been found.`). Nenhuma outra
mudança foi feita entre os dois arquivos além da guarda removida: mesmo
`.cfg`, mesmas constantes (`Writers = {w1, w2}`, `Deliveries = {d1, d2}`,
`Couriers = {c1, c2}`, `MaxEpoch = 4`).
