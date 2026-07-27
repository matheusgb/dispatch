# Relatório LoadGen: identidade compartilhada esgota o rate limit

**Nota de leitura:** os 429 abaixo não são bug do rate limit, são uma
limitação real do próprio LoadGen: todas as ordens simuladas enviam GPS
autenticadas como o mesmo caller (`loadgen`), então elas competem pelo
mesmo bucket de 20 req/s com burst 40. Em produção, cada entregador teria
seu próprio token; aqui, sem tokens por entregador, a concorrência entre
ordens vira concorrência dentro da mesma identidade. Limitação registrada
em `docs/learning/tier-2.md`. Comparar com `loadgen-tier-2-golden.md`, que
usa concorrência 1 e fica sem erros.

- seed: 777
- ordens simuladas: 100
- entregadores no pool: 15
- duração total: 12.468921165s

## Resultado por desfecho

| Desfecho | Quantidade |
| --- | --- |
| concluídas (assign -> pickup -> deliver) | 10 |
| recusadas | 11 |
| expiradas | 10 |
| erros | 69 |

## Idempotência

- chaves repetidas testadas: 20
- repetições que devolveram o mesmo ID: 20

## Disputa por entregador

- total de tentativas de atribuição rejeitadas por entregador ocupado, absorvidas por retry no pool: 810

## Tracking de GPS

- posições enviadas (entregas concluídas): 30
- posições que avançaram a projeção de última posição: 30

## Amostra de falhas

- ordem 13 (entrega 08904f3c-6fb4-48fb-a4cc-cb53e1723055): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 14 (entrega 22bc445f-99ca-430d-8ab0-987afdffaf0c): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 15 (entrega 8a76e7af-2a00-4489-83b4-b0b9a2ac85b1): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 16 (entrega 67ae9948-047d-4159-9a0c-a2ccfd32a53f): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 17 (entrega a7d566fe-4fad-4ef5-8b0d-a8c473e2fc1d): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 18 (entrega 048f1f49-a607-40b8-aa73-f33754db67c4): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 19 (entrega 6cfdf130-e883-494d-8e23-3ccf17021463): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 20 (entrega 972c55b3-536d-46ad-80e1-5aa02cd75f9f): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 21 (entrega 4d33e3c6-f517-4f67-8783-2feac018c916): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 22 (entrega 9a9777e3-82a0-4223-8bb2-545fc061442f): registrar posição: status 429: {"error":"limite de requisições excedido"}

