# Relatório LoadGen

- seed: 20260717
- ordens simuladas: 200
- entregadores no pool: 20
- duração total: 5.289223853s

## Resultado por desfecho

| Desfecho | Quantidade |
| --- | --- |
| concluídas (assign -> pickup -> deliver) | 161 |
| recusadas | 20 |
| expiradas | 19 |
| erros | 0 |

## Idempotência

- chaves repetidas testadas: 40
- repetições que devolveram o mesmo ID: 40

## Disputa por entregador

- total de tentativas de atribuição rejeitadas por entregador ocupado, absorvidas por retry no pool: 0

