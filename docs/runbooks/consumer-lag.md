# Runbook: lag de consumer

## Sintoma

Entregas demoram para sair de `created`/`ready_for_lunchrush`, ou posições
de GPS demoram para aparecer em `GET /deliveries/{id}/position`.

## Diagnóstico

1. Checar o lag real do grupo:

   ```bash
   docker exec lunchrush-redpanda rpk group describe lunchrush-worker
   docker exec lunchrush-redpanda rpk group describe tracking-projector
   ```

   A coluna `LAG` por partição é a fonte da verdade. `TOTAL-LAG` alto e
   crescendo é o sintoma real; um valor pequeno e estável é o comportamento
   normal do relay do outbox (ver ADR 0009: o `created -> offered` já leva
   segundos por desenho).

2. Confirmar se o lag é de consumo (grupo lento) ou de publicação (relay
   atrasado): checar `outbox_events` no PostgreSQL.

   ```sql
   SELECT count(*) FROM outbox_events WHERE published_at IS NULL;
   ```

   Muitas linhas paradas aqui é o relay, não o consumer.

3. Se o lag for de consumo, checar quantas réplicas o Deployment tem contra
   o número de partições (ver ADR 0010): mais réplicas que partições não
   ajuda.

## Ação

- **Relay atrasado:** checar os logs do `delivery-api` por
  `"publicar eventos pendentes do outbox"` com erro. Confirmar que o
  Redpanda está saudável (`rpk cluster info`). O relay se recupera sozinho
  quando o broker volta: nenhum evento é perdido, só atrasado.
- **Consumer lento:** confirmar que o número de réplicas está dentro do
  limite de partições (3). Se estiver, e o lag ainda crescer, o próximo
  passo é medir o tempo de processamento por mensagem (ver métricas de
  duração do handler) antes de qualquer mudança de código.
- **Poison pill:** ver `docs/runbooks/dlq-replay.md`.

## O que este runbook não cobre

- Aumentar o número de partições de um tópico existente: isso muda a chave
  de particionamento efetiva para mensagens já enviadas com a mesma chave e
  não é uma operação trivial; não fazer sem um ADR novo.
