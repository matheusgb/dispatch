# Relatório LoadGen

- seed: 555
- ordens simuladas: 30
- entregadores no pool: 10
- duração total: 13.309388089s

## Resultado por desfecho

| Desfecho | Quantidade |
| --- | --- |
| concluídas (assign -> pickup -> deliver) | 24 |
| recusadas | 3 |
| expiradas | 3 |
| erros | 0 |

## Idempotência

- chaves repetidas testadas: 6
- repetições que devolveram o mesmo ID: 6

## Disputa por entregador

- total de tentativas de atribuição rejeitadas por entregador ocupado, absorvidas por retry no pool: 0

## Tracking de GPS

- posições enviadas (entregas concluídas): 72
- posições que avançaram a projeção de última posição: 72

