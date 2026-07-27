# Runbook: Redis indisponível

## Sintoma

Latência maior nas rotas `GET /deliveries/{id}/position` e
`GET /deliveries/{id}/stream`; logs com `"msg":"redis indisponível para
leitura de tracking, usando postgres"` ou `"...para atualizar cache..."`.

## Diagnóstico

1. Confirmar que o Redis está de fato fora: `redis-cli -h <host> ping`, ou
   `docker compose ps redis`.
2. Confirmar que as leituras continuam corretas: `GET
   /deliveries/{id}/position` deve continuar respondendo `200` com o valor
   certo, só mais lento. Se estiver respondendo erro, o problema não é
   isolado ao Redis: parar aqui e investigar o PostgreSQL.
3. Checar a métrica `lunchrush_positions_current_total` continua subindo:
   se sim, a ingestão de GPS continua funcionando (ela nunca depende do
   Redis para persistir).

## Ação

Nenhuma ação de emergência é necessária: o sistema já está desenhado para
operar sem o Redis, só mais lento (ver ADR 0003). A prioridade é restaurar
o Redis, não o dado nele:

1. Subir o Redis de volta (`docker compose start redis`, ou o equivalente
   do orquestrador em uso).
2. Não é preciso "aquecer" o cache manualmente: a primeira leitura de cada
   entrega depois da religação já popula o Redis via cache-aside.
3. Confirmar que a latência das rotas de tracking volta ao normal
   (dashboard "lunchrush: RED e negócio" no Grafana, painel "Duration p95
   por rota").

## O que este runbook não cobre

- Redis com dados incorretos mas disponível: isso não deveria acontecer,
  porque o cache só é populado a partir do PostgreSQL; se acontecer, é bug
  em `internal/tracking/cache.go`, não uma operação de runbook;
- múltiplas instâncias de Redis ou cluster: o tier 2 usa uma instância
  única, sem sharding nem replicação.
