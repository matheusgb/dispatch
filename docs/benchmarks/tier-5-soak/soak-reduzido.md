# Relatório LunchRush

- seed: 20260726777
- ordens simuladas: 2000
- entregadores no pool: 300
- duração total: 4m53.588679481s

## Resultado por desfecho

| Desfecho | Quantidade |
| --- | --- |
| concluídas (assign -> pickup -> deliver) | 1800 |
| recusadas | 100 |
| expiradas | 99 |
| erros | 1 |

## Idempotência

- chaves repetidas testadas: 203
- repetições que devolveram o mesmo ID: 203

## Disputa por entregador

- total de tentativas de atribuição rejeitadas por entregador ocupado, absorvidas por retry no pool: 3

## Tracking de GPS

- posições enviadas (entregas concluídas): 5295
- posições descartadas pela rede virtual (nunca enviadas): 286
- posições que avançaram a projeção de última posição: 5180

## Rede e relógio virtuais (tier 5)

- entregadores com "crash" de sessão simulado (nova tracking_session_epoch): 85
- tentativas de clock skew (reenvio de posição antiga): 171
- tentativas de clock skew que não regrediram a posição atual: 171

## Amostra de falhas

- ordem 944 (entrega 88c6e7d2-50ca-4cb5-b8fa-57f18895a2c7): projeção não alcançou a sequência 0 do epoch 1 dentro do prazo
