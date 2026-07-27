# Runbook: backup e recuperação distribuída (tier 4, pendência fechada antes do tier 5)

Terceira e última lacuna do tier 4 deixada por restrição de tempo/memória
de uma sessão anterior. O roadmap é explícito: "Restaurar apenas
PostgreSQL não prova recuperação do sistema". Este runbook documenta uma
execução real, contra o ambiente `docker compose --profile app` local
(não `aws-benchmark`/RDS real, fora de escopo, ver
`docs/limitacoes-simulacao-local.md`), coordenando banco, outbox e Kafka.

## O que este laboratório tem, e o que não tem

Este ambiente **não** tem WAL archiving contínuo nem PITR configurado, só
`pg_dump` lógico sob demanda. Isso significa que o RPO real deste
laboratório é o intervalo entre backups, não "zero até o último commit"
como um Aurora/RDS com backup contínuo prometeria. Essa é uma limitação
honesta, não escondida: um ambiente de produção real usaria WAL
archiving/PITR para reduzir a janela.

## Passo a passo executado (evidência real, não descrita)

### 1. Ponto de restauração (T0)

```text
$ date -u
2026-07-26T22:17:36Z

$ docker exec lunchrush-postgres-1 pg_dump -U lunchrush -Fc lunchrush -f /tmp/lunchrush_backup_t0.dump
# 9.162.762 bytes

$ psql ... -c "select count(*) deliveries, count(*) outbox_total, ..."
 deliveries | outbox_total | outbox_pending | outbox_max_id
      16174 |        97361 |          96275 |         98630

$ rpk topic describe lunchrush.delivery-events -p   # docs/benchmarks/tier-4-backup-recovery/kafka-hwm-t0.txt
PARTITION  HIGH-WATERMARK
0          374
1          355
2          384
```

### 2. Atividade depois do backup (simula produção continuando)

`k6 run loadtest/k6/smoke.js` (5 VUs, 10s): +435 entregas, jornada
completa cada uma (created → ready → offered → assigned → picked_up →
delivered), gerando outbox events proporcionais.

### 3. Ponto de "crash" (T1), 39s depois de T0

```text
$ date -u
2026-07-26T22:18:15Z

 deliveries | outbox_total | outbox_pending | outbox_max_id
      16609 |       100015 |          98892 |        101284

PARTITION  HIGH-WATERMARK
0          382
1          366
2          391
```

Gap real entre T0 e T1: **435 entregas**, **2.654 outbox events**, **26
mensagens** efetivamente publicadas no Kafka (as 3 partições foram de
374/355/384 para 382/366/391).

### 4. Restauração (para um banco separado, `lunchrush_restore`, para não
destruir o ambiente compartilhado do resto desta sessão, mecanicamente
idêntico a restaurar por cima do banco original)

```text
$ psql -d postgres -c "CREATE DATABASE lunchrush_restore OWNER lunchrush;"
$ pg_restore -U lunchrush -d lunchrush_restore /tmp/lunchrush_backup_t0.dump

$ psql -d lunchrush_restore -c "select count(*) deliveries_restored, ..."
 deliveries_restored | outbox_restored | outbox_max_id_restored
                16174 |            97361 |                   98630
```

A restauração bateu exatamente com a foto de T0: prova mecânica de que o
`pg_dump`/`pg_restore` funciona de ponta a ponta contra este schema real
(inclusive as constraints únicas de `deliveries` e `outbox_events`, que
`pg_restore` recria).

### 5. Reconciliação: o que a restauração perdeu, e o que isso significa de verdade

```sql
select count(*) as gap_total,
       count(*) filter (where published_at is not null) as gap_ja_publicado_antes_do_restore
from outbox_events
where id > 98630 and id <= 100015;
--  gap_total | gap_ja_publicado_antes_do_restore
--       1385 |                                 0
```

**Achado real, não esperado:** nenhum dos 1.385 eventos de outbox no gap
já tinha sido publicado no Kafka antes do ponto de restauração. Isso
aconteceu porque o relay do outbox estava seriamente atrasado durante esta
sessão (96-99% dos eventos pendentes o tempo todo, ver seção abaixo):
sob essa condição específica, restaurar para T0 não corre o risco "clássico"
do runbook (um efeito já propagado para fora do banco, sem o registro
correspondente depois do restore): simplesmente não haveria nada
inconsistente para reconciliar no lado do Kafka neste cenário particular.
Isso não prova que o caso mais difícil (evento já publicado e já
consumido por um efeito externo, como uma notificação) esteja resolvido
em geral; prova que, **nesta execução**, ele não ocorreu, e documenta por
quê. O caso difícil já tem prova separada e anterior:
`docs/adr/0009-latencia-do-outbox-relay.md` e o cenário C de
`docs/benchmarks/chaos-tier-4.md` (Redpanda pausado, outbox acumula e
publica tudo depois, sem duplicar efeito, graças à inbox/`consumed_events`
com `PRIMARY KEY (consumer, event_id)`).

**Achado colateral (vira item de `docs/benchmarks/tier-5-what-breaks-next.md`):**
o relay do outbox, sob a carga combinada desta sessão (k6 de steady+spike,
TLC, SBOM/scan rodando em paralelo na mesma máquina), manteve 96-99% dos
eventos pendentes o tempo todo e publicou em lotes de ~100 a cada
~1min40s, muito mais lento que o esperado (~1s por ciclo, ver README). Não
investigamos a causa raiz aqui (fora do escopo deste runbook, que é sobre
recuperação, não sobre profiling do relay), mas fica registrado como
candidato real a investigação: contenção de CPU compartilhada nesta
máquina de laboratório, ou um gargalo genuíno do relay sob esta taxa de
chegada, que só profiling decide.

### 6. Reconstrução do Redis

`docker compose stop redis` / `start redis` (mesmo padrão já provado em
`docs/benchmarks/chaos-tier-4.md`, cenário B): o Redis sobe vazio
(`DBSIZE 0`) depois do restart, e o `tracking-projector` já demonstrou
(tier 2 ADR 0003, tier 4 chaos B) que reconstrói a projeção por
cache-aside no próximo `GET`, sem exigir passo manual. Não repetimos a
chamada autenticada de posição aqui (exigiria emitir um token JWT válido,
fora do escopo direto desta restauração); a evidência de reconstrução
automática já está registrada nas duas referências acima e não muda com
este runbook.

### 7. Limpeza

```text
$ psql -d postgres -c "DROP DATABASE lunchrush_restore;"
$ docker exec lunchrush-postgres-1 rm -f /tmp/lunchrush_backup_t0.dump
```

O banco de restauração e o dump temporário não ficam no ambiente depois
do drill; a evidência (contagens, saída do `rpk`) fica neste documento.

## RPO medido nesta execução

| Classe de dado | RPO medido |
| --- | --- |
| lifecycle de entrega (PostgreSQL) | 39s (intervalo real entre `pg_dump` e o ponto de "crash" simulado); em produção real seria o intervalo entre backups agendados, ou próximo de zero com WAL archiving/PITR (não configurado neste laboratório) |
| eventos de outbox não publicados | mesmos 39s; adicionalmente sujeitos ao atraso do relay observado (96-99% pendente durante esta sessão) |
| Kafka (log já publicado) | não perdido: o log do Redpanda é independente do PostgreSQL e não foi tocado neste drill |
| Redis | 0 (projeção descartável, reconstrução automática já provada) |

## O que este runbook não fez (limitação honesta)

- não promoveu de fato `lunchrush_restore` como novo primário do
  `delivery-api` (evitado para não destruir o ambiente compartilhado do
  resto da sessão); a restauração mecânica (passo 4) e a análise de gap
  (passo 5) já provam a parte que dependia de execução real, e o passo de
  "apontar o serviço para o banco restaurado" é operacionalmente um
  troca de `DATABASE_URL` e restart, sem lógica nova para testar;
- não configurou WAL archiving/PITR (ver seção acima);
- não exercitou o caso "evento já publicado E já consumido" neste drill
  específico porque o relay estava atrasado demais para isso acontecer
  na janela testada; a prova desse caso já existe em
  `docs/benchmarks/chaos-tier-4.md` (cenário C) e não foi refeita aqui.
