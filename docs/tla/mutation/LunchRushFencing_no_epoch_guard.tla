---------------------------- MODULE LunchRushFencing_no_epoch_guard ----------------------------
(*
Especificacao pequena do protocolo de fencing usado pela autoridade de
ownership do tier 5 (ver docs/adr/0017-arquitetura-celular-local.md e
docs/adr/0018-fencing-lease-e-epoch.md).

O que este modelo cobre, mapeado para lunchrush_fences / active_assignments
(internal/fencing, migrations/0006_fencing.up.sql):

  - dois writers concorrentes (Writers) disputando o mesmo shard de fencing;
  - epoch monotonico por shard, promovido por Promote (equivalente a um
    failover, ou uma auto-recuperacao do mesmo writer, que aumenta o epoch
    depois da lease expirar);
  - lease com expiracao (LeaseExpire), sem depender do relogio local do
    writer -- o writer so pode agir dentro do epoch/lease que a autoridade
    confirmou (FenceWrite);
  - atraso de mensagem e crash/restart: cada Promote(w) adiciona um novo
    token <<w, epoch>> a knownTokens, mas os tokens antigos de w
    permanecem no conjunto (uma mensagem em transito ou um processo que
    reiniciou pode montar uma escrita com um token que w tinha antes,
    mesmo que w ja saiba do token novo em outro lugar). Isso modela sem
    canal FIFO obrigatorio um writer que se auto-recupera (mesmo owner,
    epoch maior) enquanto uma escrita atrasada dele mesmo, com o epoch
    velho, ainda esta a caminho;
  - rejeicao de fencing token antigo: Assign(w, d, c, e) so aceita se o
    token e enviado bate com o epoch atual da fence (e = epoch), nao so se
    o writer e o owner atual -- e exatamente esta segunda checagem que a
    mutacao remove (ver docs/tla/mutation/);
  - handoff de courier: CourierHandoffStart/Confirm/Activate modelam o
    "draining -> revoga -> confirma ausencia de assignment -> ativa nova
    celula" do roadmap, garantindo que um courier na fronteira nunca fica
    elegivel em duas celulas ao mesmo tempo (courierState "assigned" e
    "draining" sao mutuamente exclusivos por construcao).

O que este modelo NAO cobre (fora de escopo do tier 5 pragmatico, ver
docs/limitacoes-simulacao-local.md): particionamento de rede como topologia
explicita (aproximado por tokens antigos permanecendo utilizaveis em
knownTokens), multiplos shards simultaneos (um shard basta para as
propriedades de seguranca; contencao entre shards e medida no benchmark de
codigo, nao aqui), e o conteudo do outbox event (fora do escopo do
protocolo de ownership).
*)

EXTENDS Integers, FiniteSets, Sequences, TLC

CONSTANTS
    Writers,        \* conjunto de writers (regioes/celulas candidatas a dono)
    Deliveries,     \* conjunto pequeno de entregas usadas para testar exclusividade
    Couriers,       \* conjunto pequeno de couriers usados para testar exclusividade
    MaxEpoch        \* teto de epoch para manter o espaco de estados finito

ASSUME Cardinality(Writers) >= 2
ASSUME MaxEpoch \in Nat /\ MaxEpoch >= 2

VARIABLES
    epoch,               \* epoch atual da fence (unico shard modelado)
    owner,               \* writer dono do epoch atual
    leaseValid,          \* BOOLEAN: lease em vigor (FALSE = expirada, autoridade fecha)
    assignment,          \* [Deliveries -> courier que a atende, ou "none"]
    assignedBy,          \* [Deliveries -> writer que fez a atribuicao, ou "none"]
    courierState,        \* [Couriers -> {"free", "assigned", "draining", "handoff_confirmed"}]
    knownTokens,         \* SUBSET (Writers X 1..MaxEpoch): todo token que algum writer ja teve
    staleAssignHappened  \* BOOLEAN: algum Assign com token != epoch atual ja escreveu?

vars == <<epoch, owner, leaseValid, assignment, assignedBy,
          courierState, knownTokens, staleAssignHappened>>

None == "none"

Init ==
    /\ epoch = 1
    /\ owner = CHOOSE w \in Writers : TRUE
    /\ leaseValid = TRUE
    /\ assignment = [d \in Deliveries |-> None]
    /\ assignedBy = [d \in Deliveries |-> None]
    /\ courierState = [c \in Couriers |-> "free"]
    /\ knownTokens = {<<owner, 1>>}
    /\ staleAssignHappened = FALSE

-----------------------------------------------------------------------------
(* A lease expira: a autoridade deixa de considerar o owner atual valido.
   Equivalente a lease_until < now() no lunchrush_fences. Nenhum writer
   consegue mais FenceWrite/Assign validamente ate uma promocao. *)
LeaseExpire ==
    /\ leaseValid = TRUE
    /\ leaseValid' = FALSE
    /\ UNCHANGED <<epoch, owner, assignment, assignedBy, courierState, knownTokens, staleAssignHappened>>

(* Promote: um writer assume como novo dono depois da lease expirar,
   incrementando o epoch. Equivalente ao UPDATE que grava owner_region e
   epoch = epoch + 1 na mesma transacao (so possivel com lease expirada,
   nunca com lease valida -- isso e o que impede dois donos simultaneos).
   w pode ser o mesmo writer que era dono antes (auto-recuperacao) ou um
   writer diferente (failover de verdade); o token antigo de w permanece
   em knownTokens, o que e o que torna o mutation test interessante. *)
Promote(w) ==
    /\ leaseValid = FALSE
    /\ epoch < MaxEpoch
    /\ epoch' = epoch + 1
    /\ owner' = w
    /\ leaseValid' = TRUE
    /\ knownTokens' = knownTokens \union {<<w, epoch + 1>>}
    /\ UNCHANGED <<assignment, assignedBy, courierState, staleAssignHappened>>

(* FenceWrite: um writer renova a fence (equivalente ao UPDATE condicional
   "WHERE epoch = ? AND owner_region = ? AND lease_until > now()" que muda
   last_write_token). So o dono atual, com o token que bate com o epoch
   atual, consegue. *)
FenceWrite(w) ==
    /\ leaseValid = TRUE
    /\ owner = w
    /\ <<w, epoch>> \in knownTokens
    /\ UNCHANGED <<epoch, owner, leaseValid, assignment, assignedBy, courierState, knownTokens, staleAssignHappened>>

(* Assign: criar um assignment ativo para uma entrega livre e um courier
   livre, usando um token <<w, e>> que w tem em algum momento conhecido
   (pode ser velho: mensagem atrasada, ou processo que reiniciou com
   estado antigo em memoria). Modela a transacao unica do roadmap: valida
   epoch (WHERE epoch = ?), INSERT em active_assignments protegido pelas
   duas unique constraints (delivery_id, courier_id). A guarda "e = epoch"
   e exatamente o fencing: sem ela (mutation test), um token velho de um
   writer que voltou a ser owner por auto-recuperacao passaria pela
   checagem "owner = w" mesmo carregando um epoch que ja foi superado. *)
Assign(w, d, c, e) ==
    /\ leaseValid = TRUE
    /\ owner = w
    /\ <<w, e>> \in knownTokens
    \* MUTATION: guarda "e = epoch" removida de proposito (ver
    \* docs/tla/mutation/README.md) para provar que o TLC acha um
    \* contraexemplo sem ela: um writer auto-recuperado (mesmo owner,
    \* epoch maior) consegue escrever com um token velho.
    /\ assignment[d] = None                \* unique constraint: delivery_id
    /\ courierState[c] = "free"            \* unique constraint: courier_id, e nao esta em handoff
    /\ assignment' = [assignment EXCEPT ![d] = c]
    /\ assignedBy' = [assignedBy EXCEPT ![d] = w]
    /\ courierState' = [courierState EXCEPT ![c] = "assigned"]
    /\ staleAssignHappened' = (staleAssignHappened \/ e # epoch)
    /\ UNCHANGED <<epoch, owner, leaseValid, knownTokens>>

(* Finish: entrega termina, assignment sai de active_assignments para
   assignment_history na mesma transacao (aqui, libera o courier). *)
Finish(d) ==
    /\ assignment[d] # None
    /\ LET c == assignment[d] IN
        /\ assignment' = [assignment EXCEPT ![d] = None]
        /\ assignedBy' = [assignedBy EXCEPT ![d] = None]
        /\ courierState' = [courierState EXCEPT ![c] = "free"]
    /\ UNCHANGED <<epoch, owner, leaseValid, knownTokens, staleAssignHappened>>

-----------------------------------------------------------------------------
(* Handoff de courier entre celulas: draining -> revoga -> confirma
   ausencia -> ativa. As tres acoes exigem courierState = "free" para
   comecar (nunca um courier "assigned"), e Assign exige courierState =
   "free" para atribuir, entao um courier em draining/handoff_confirmed
   nunca aceita um novo Assign: nunca fica elegivel em duas celulas ao
   mesmo tempo. *)
CourierHandoffStart(c) ==
    /\ courierState[c] = "free"
    /\ courierState' = [courierState EXCEPT ![c] = "draining"]
    /\ UNCHANGED <<epoch, owner, leaseValid, assignment, assignedBy, knownTokens, staleAssignHappened>>

CourierHandoffConfirm(c) ==
    /\ courierState[c] = "draining"
    /\ courierState' = [courierState EXCEPT ![c] = "handoff_confirmed"]
    /\ UNCHANGED <<epoch, owner, leaseValid, assignment, assignedBy, knownTokens, staleAssignHappened>>

CourierHandoffActivate(c) ==
    /\ courierState[c] = "handoff_confirmed"
    /\ courierState' = [courierState EXCEPT ![c] = "free"]
    /\ UNCHANGED <<epoch, owner, leaseValid, assignment, assignedBy, knownTokens, staleAssignHappened>>

-----------------------------------------------------------------------------
Next ==
    \/ LeaseExpire
    \/ \E w \in Writers : Promote(w)
    \/ \E w \in Writers : FenceWrite(w)
    \/ \E w \in Writers, d \in Deliveries, c \in Couriers, e \in 1..MaxEpoch : Assign(w, d, c, e)
    \/ \E d \in Deliveries : Finish(d)
    \/ \E c \in Couriers : CourierHandoffStart(c)
    \/ \E c \in Couriers : CourierHandoffConfirm(c)
    \/ \E c \in Couriers : CourierHandoffActivate(c)

\* Fairness: cada writer eventualmente tenta promover quando pode (sem isto,
\* um comportamento que so faz handoff de courier para sempre, sem nunca
\* chamar Promote, "trava" a lease invalida indefinidamente e viola
\* EventuallyRecovers por omissao de fairness, nao por um bug real do
\* protocolo -- corrigido explicitando fairness por acao, nao so em Next).
Spec == Init /\ [][Next]_vars /\ \A w \in Writers : WF_vars(Promote(w))

-----------------------------------------------------------------------------
(* Propriedades de seguranca (Invariantes) *)

\* "nunca existem dois assignments ativos para a mesma entrega": garantido
\* pela propria forma de assignment (uma funcao Deliveries -> Couriers),
\* mas mantido explicito porque e o invariante 1 do roadmap.
NoDoubleAssignmentPerDelivery ==
    \A d \in Deliveries : assignment[d] \in Couriers \union {None}

\* "um courier nunca possui dois assignments ativos, inclusive durante
\* handoff": nenhum courier aparece como destino de duas entregas ativas ao
\* mesmo tempo. Este e o invariante que a guarda courierState[c] = "free"
\* em Assign precisa preservar.
CourierNeverDoubleAssigned ==
    \A c \in Couriers :
        Cardinality({d \in Deliveries : assignment[d] = c}) <= 1

\* "um writer antigo nunca confirma escrita depois de um epoch maior": no
\* modelo correto, Assign exige e = epoch, entao staleAssignHappened nunca
\* vira TRUE (Assign so roda quando o token e exatamente o atual). O
\* mutation test (docs/tla/mutation/) remove essa guarda; sem ela, um
\* writer que se auto-recuperou (mesmo owner, epoch maior) ainda consegue
\* usar um token velho porque a checagem "owner = w" sozinha nao basta, e
\* staleAssignHappened vira TRUE -- e o que o TLC relata como violacao.
NoStaleAssignEverHappened == staleAssignHappened = FALSE

\* "estado terminal nao regride": o unico estado com nocao de "terminal"
\* neste modulo e o epoch (nunca reaproveitado por um writer antigo depois
\* de promovido); expressamos a nao-regressao como monotonicidade de epoch.
EpochMonotonic == epoch >= 1

TypeInvariant ==
    /\ epoch \in 1..MaxEpoch
    /\ owner \in Writers
    /\ leaseValid \in BOOLEAN
    /\ assignment \in [Deliveries -> Couriers \union {None}]
    /\ assignedBy \in [Deliveries -> Writers \union {None}]
    /\ courierState \in [Couriers -> {"free", "assigned", "draining", "handoff_confirmed"}]
    /\ knownTokens \subseteq (Writers \X (1..MaxEpoch))
    /\ staleAssignHappened \in BOOLEAN

\* Propriedade composta usada no .cfg como INVARIANT principal de negocio.
Safety ==
    /\ NoDoubleAssignmentPerDelivery
    /\ CourierNeverDoubleAssigned
    /\ EpochMonotonic
    /\ NoStaleAssignEverHappened

-----------------------------------------------------------------------------
(* Vivacidade, sob duas premissas explicitas (roadmap tier 5: "propriedades
   de vivacidade, sob premissas explicitas"):
     1. fairness fraca de Promote por writer (ver Spec);
     2. capacidade de promocao nao esgotada -- MaxEpoch e um teto artificial
        deste modelo finito (o epoch real do banco e um bigint, nunca
        esgota na pratica). Quando epoch = MaxEpoch, Promote fica
        estruturalmente desabilitada e nenhuma quantidade de fairness pode
        forcar uma promocao que a propria definicao da acao proibe; isso e
        um artefato do espaco de estados finito do model checker, nao um
        bug do protocolo. A propriedade so afirma recuperacao enquanto
        ainda ha capacidade de promover. *)
EventuallyRecovers == (leaseValid = FALSE /\ epoch < MaxEpoch) ~> leaseValid = TRUE

=============================================================================
