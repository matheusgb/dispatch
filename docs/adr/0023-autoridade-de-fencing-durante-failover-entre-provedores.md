# ADR 0023: autoridade de fencing durante failover entre provedores

## Contexto

O tier 6 pede que o failover entre `cloud-a` e `cloud-b` reuse a mesma
autoridade de fencing do tier 5 (`internal/fencing`, epoch + lease,
`lunchrush_fences`/`active_assignments`/`assignment_history`), promovendo
entre provedores da mesma forma que promove entre células. O tier 5 (ADR
0018) já documentou a limitação central dessa autoridade: ela roda em um
único Postgres local, single-node, sem réplica geográfica. A pergunta que
este ADR responde: o que muda, de fato, quando a promoção passa a
acontecer entre dois Postgres **fisicamente separados** (um por provedor),
em vez de um único Postgres compartilhado por duas células lógicas (como
no tier 5)?

## Decisão

Implementar o failover cross-provedor como uma sequência de operações,
todas reusando `internal/fencing.Service` sem nenhuma alteração de
protocolo, orquestradas por uma ferramenta de operador nova,
`cmd/cloudfailover` (subcomandos `seed`, `promote`, `assign`, `fence`):

1. `cloud-a` promove o shard (`Promote(shardID, "cloud-a", lease)`) e
   recebe escritas reais (`CreateAssignment`), writer ativo;
2. `pg_dump` de `cloud-a` no ponto T0 (mesmo padrão do runbook de backup
   do tier 4, `docs/runbooks/backup-e-recuperacao-distribuida.md`);
3. `cloud-a` continua recebendo escrita depois de T0 (simula produção
   continuando entre o backup e a queda);
4. interrupção real: `docker compose stop delivery-api lunchrush-worker`
   em `cloud-a` (os processos que escreviam pelo `cloud-a` ficam
   indisponíveis de verdade, não é só uma flag);
5. o dump T0 é restaurado no Postgres de `cloud-b` (`pg_restore`), banco
   fisicamente separado;
6. `cloud-b` promove o mesmo shard (`Promote(shardID, "cloud-b", lease)`):
   só funciona porque a lease do fence restaurado (herdada de `cloud-a`,
   epoch 1) já expirou no relógio real decorrido entre os passos 1 e 6,
   exatamente a regra que impede duas promoções simultâneas
   (`ErrLeaseNotExpired` se a lease ainda fosse válida);
7. um writer com o epoch antigo de `cloud-a` (epoch 1, `owner_region:
   cloud-a`) tenta `CreateAssignment` contra o Postgres de `cloud-b`: 100%
   rejeitado com `ErrStaleFence`;
8. o writer novo de `cloud-b` (epoch 2, `owner_region: cloud-b`) escreve
   com sucesso.

## Por que isso é mais forte que a prova do tier 5, não mais fraca

O tier 5 promovia entre duas células que compartilhavam o mesmo processo
Postgres (ADR 0019: "isolamento lógico", explicitamente rotulado como tal).
Aqui, `cloud-a` e `cloud-b` são dois containers Postgres diferentes, cada
um com seu próprio volume, processo e rede Docker: a promoção só funciona
porque os dados foram restaurados de um lado para o outro via `pg_dump`/
`pg_restore` real, não porque os dois lados sempre viram a mesma tabela.
Isso testa uma coisa que o tier 5 não testava: o protocolo de fencing
sobrevive a uma cópia de dados real entre dois bancos fisicamente
distintos, não só a uma troca de `owner_region` na mesma linha.

## Ainda assim, o que continua limitado

- a autoridade nunca é replicada continuamente entre `cloud-a` e `cloud-b`:
  a "promoção" depende de um `pg_dump`/`pg_restore` manual (ou de um
  runbook automatizável, mas não de replicação síncrona ou assíncrona
  contínua). Isso é consistente com o roadmap: "se a autoridade global
  permanecer hospedada em cloud-a, o documento deverá reconhecer que o
  failover ainda depende do provedor principal", mas aqui a dependência é
  mais estreita ainda: o failover depende de um humano (ou script) ter
  tirado um backup recente o suficiente, não de `cloud-a` estar acessível
  no momento do failover;
- **RPO não é zero.** O gap medido nesta execução foi de 5 assignments
  confirmados entre o `pg_dump` e a interrupção, ver
  `docs/benchmarks/tier-6-portability/failover-transcript.txt`. Esse gap é
  real e mostra exatamente por que "RPO zero presumido" está na lista do
  que não entra no tier 6;
- RTO medido nesta execução (~11,5s) é dominado pelo tempo de
  `docker compose stop` + `pg_dump`/`pg_restore` + `dropdb`/`createdb` de
  um banco de laboratório pequeno; não generaliza para um banco de
  produção com gigabytes de dados, nem para uma rede real entre dois
  provedores geograficamente distantes.

## Alternativas consideradas

- **Réplica lógica contínua (`pg_logical`/CDC) entre `cloud-a` e
  `cloud-b`**: mais realista, mas exigiria uma nova peça de
  infraestrutura só para este experimento; o roadmap já aceita RPO
  diferente de zero para o plano de dados assíncrono, e o objetivo deste
  tier é provar o protocolo de fencing, não construir um pipeline de CDC
  novo. Fica registrado como candidato de sessão futura em
  `docs/benchmarks/tier-6-what-breaks-next.md`.
- **Autoridade de fencing hospedada num terceiro serviço, independente de
  `cloud-a` e `cloud-b`**: mais próxima do "serviço baseado em consenso
  com presença nas duas clouds" que o roadmap cita como alternativa;
  rejeitada por escopo (exigiria um novo componente de coordenação,
  Raft ou equivalente, fora do que os tiers anteriores já validaram);
  também candidato futuro.

## Evidência real

`docs/benchmarks/tier-6-portability/failover-transcript.txt`, execução
completa com timestamps reais: seed de 30 pares, 10 assignments bem
sucedidos em `cloud-a` antes do backup, 5 depois do backup (o gap de
RPO), interrupção real dos processos, `pg_dump`/`pg_restore` reais,
promoção de `cloud-b` (epoch 1 → 2), 10/10 tentativas do writer antigo
rejeitadas em `cloud-b`, 5/5 tentativas do writer novo aceitas,
reconciliação final: 15 assignments em `cloud-a` (congelado) e 15 em
`cloud-b` (10 restaurados + 5 novos pós-promoção).

## Status

Aceita.
