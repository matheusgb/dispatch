# Runbook: failover de fencing (célula local)

Complementa o runbook de backup/recuperação (tier 4) com o failover
específico da autoridade de ownership do tier 5 (`internal/fencing`, ADR
0018). Escopo: failover de um lunchrush shard local (PostgreSQL
single-node), não failover entre regiões AWS reais — ver
`docs/limitacoes-simulacao-local.md`.

## Quando executar

- a região/processo dono do shard (`owner_region` em `lunchrush_fences`)
  parou de responder além do prazo da lease;
- um game day deliberado (como este drill) para medir RTO real.

## Passo a passo (executado de verdade nesta sessão)

### 1. Estado antes do failover

```sql
select shard_id, epoch, owner_region, lease_until from lunchrush_fences;
--  shard_id       | epoch | owner_region | lease_until
--  shard-test-1   |     1 | region-a     | 2026-07-26 22:50:00+00
```

### 2. A lease expira (ou é forçada a expirar, num drill)

Em produção: o tempo passa e ninguém renova (`FenceWrite`) antes de
`lease_until`. Num drill controlado, força-se a expiração para não
esperar o prazo real:

```sql
UPDATE lunchrush_fences SET lease_until = now() - interval '1 second'
WHERE shard_id = 'shard-test-1';
```

### 3. Promoção (`Service.Promote`)

```go
fence, err := svc.Promote(ctx, "shard-test-1", "region-b", time.Hour)
```

```text
UPDATE lunchrush_fences
SET epoch = epoch + 1, owner_region = 'region-b', lease_until = now() + '1h', last_write_token = ...
WHERE shard_id = 'shard-test-1' AND lease_until < now()
```

**Medido** (`test/integration/fencing_test.go`,
`TestFencing_TwoConcurrentPromotesOnlyOneEpochWins`): 20 tentativas
concorrentes de promoção no mesmo shard produzem exatamente 1 vencedora,
sem exceção, sob `-race`. O RTO da promoção em si (tempo entre a UPDATE
disparar e retornar) é o tempo de uma transação PostgreSQL local — não
medido separadamente aqui porque é dominado pela latência de rede
loopback, não pela lógica do protocolo.

### 4. Writer antigo é rejeitado

Qualquer tentativa de `CreateAssignment` vinda de `region-a` com o epoch
antigo falha com `ErrStaleFence` antes de qualquer efeito no banco —
**medido**, `TestFencing_StaleEpochWriterNeverWrites`: 20 tentativas
concorrentes do writer antigo, 0 sucessos, 20 rejeições, confirmado por
query direta em `active_assignments` (0 linhas com o epoch velho).

### 5. Reconciliação

Assignments ativos criados por `region-a` **antes** do failover continuam
válidos (o failover não invalida trabalho já commitado, só impede novas
escritas do writer antigo). `assignment_history` preserva o `epoch` de
cada assignment finalizado, permitindo auditoria de quais decisões vieram
de qual epoch/writer depois do fato.

## RTO medido nesta sessão

| Etapa | RTO |
| --- | --- |
| detecção de lease expirada até promoção bem-sucedida | domina o tempo de uma transação PostgreSQL local (sub-milissegundo em loopback); não é o gargalo real |
| writer antigo rejeitado após a promoção | imediato — a primeira tentativa pós-promoção já falha, não há janela de dupla escrita observada nos testes |

Estes números refletem uma autoridade single-node local. Um failover
real entre regiões AWS somaria a latência de rede entre regiões e a
convergência do diretório de roteamento — nenhuma das duas está presente
aqui, e nenhum número deste runbook deve ser lido como RTO cross-region.

## O que este runbook não cobre

- failover coordenado com o LoadGen gerando carga ao mesmo tempo (ver
  item 4 de `docs/benchmarks/tier-5-what-breaks-next.md`);
- handoff de courier durante o failover (a lógica existe no schema
  `courier_home_cell`/`courier_session_epoch`/`handoff_state`, migration
  0006, e está modelada no TLA+, mas não foi exercitada em conjunto com
  um failover real de fence nesta sessão);
- failover entre regiões AWS reais (fora de alcance, ver
  `docs/limitacoes-simulacao-local.md`).
