# Relatório LoadGen

- seed: 20260726
- ordens simuladas: 40
- entregadores no pool: 15
- duração total: 19.154808569s

## Resultado por desfecho

| Desfecho | Quantidade |
| --- | --- |
| concluídas (assign -> pickup -> deliver) | 32 |
| recusadas | 4 |
| expiradas | 4 |
| erros | 0 |

## Idempotência

- chaves repetidas testadas: 9
- repetições que devolveram o mesmo ID: 9

## Disputa por entregador

- total de tentativas de atribuição rejeitadas por entregador ocupado, absorvidas por retry no pool: 0

## Tracking de GPS

- posições enviadas (entregas concluídas): 96
- posições descartadas pela rede virtual (nunca enviadas): 10
- posições que avançaram a projeção de última posição: 85

## Rede e relógio virtuais (tier 5)

- entregadores com "crash" de sessão simulado (nova tracking_session_epoch): 7
- tentativas de clock skew (reenvio de posição antiga): 5
- tentativas de clock skew que não regrediram a posição atual: 5

