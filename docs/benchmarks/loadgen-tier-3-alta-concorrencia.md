# Relatório LoadGen — 40 ordens em concorrência 8, prazo de 30s

**Nota de leitura:** os "erros" abaixo são timeouts do LoadGen esperando
`offered`, não perda de dado nem dupla atribuição. Investigação em
`docs/adr/0009-latencia-do-outbox-relay.md`: o caminho `created -> offered`
atravessa o relay do outbox duas vezes (uma para `ready_for_lunchrush`, outra
para `offered`), e o relay publica em lote a cada 1 segundo. Sob concorrência
8, essa espera composta ocasionalmente passa de 30s. Toda entrega que
"errou" aqui foi conferida no PostgreSQL depois: todas chegaram em
`offered` e, sem `assign` do LoadGen a tempo, o `lunchrush-worker` as
reciclou corretamente ao expirar. Comparar com `loadgen-tier-3-golden.md`
(concorrência 3, prazo 30s, 0 erros).

- seed: 31415
- ordens simuladas: 40
- entregadores no pool: 10
- duração total: 1m37.814707982s

## Resultado por desfecho

| Desfecho | Quantidade |
| --- | --- |
| concluídas (assign -> pickup -> deliver) | 15 |
| recusadas | 2 |
| expiradas | 0 |
| erros | 23 |

## Idempotência

- chaves repetidas testadas: 8
- repetições que devolveram o mesmo ID: 8

## Disputa por entregador

- total de tentativas de atribuição rejeitadas por entregador ocupado, absorvidas por retry no pool: 0

## Tracking de GPS

- posições enviadas (entregas concluídas): 45
- posições que avançaram a projeção de última posição: 45

## Amostra de falhas

- ordem 0 (entrega 705d41da-7c94-4d7d-b8ca-5aa017589ff2): lunchrush-worker não moveu a entrega para offered dentro do prazo
- ordem 1 (entrega e3bdb514-9059-4b2c-ba01-c5bc65c76dac): lunchrush-worker não moveu a entrega para offered dentro do prazo
- ordem 3 (entrega 316ebe32-43e8-4ea2-af9c-4c9166f76cfc): lunchrush-worker não moveu a entrega para offered dentro do prazo
- ordem 5 (entrega f9c7c9b8-26a6-4cbd-acaa-8e57094950eb): lunchrush-worker não moveu a entrega para offered dentro do prazo
- ordem 6 (entrega 1c0489d7-03de-4d51-8b1d-13a71200f1cf): lunchrush-worker não moveu a entrega para offered dentro do prazo
- ordem 7 (entrega 30f693f9-6e3e-47ef-b541-6b815b3e41dd): lunchrush-worker não moveu a entrega para offered dentro do prazo
- ordem 9 (entrega 1dcb3a73-a28a-4697-9bcc-eac51de2697a): lunchrush-worker não moveu a entrega para offered dentro do prazo
- ordem 12 (entrega 5f30f705-bb4d-4c91-a7b3-3c8e0657efec): lunchrush-worker não moveu a entrega para offered dentro do prazo
- ordem 13 (entrega fbd17e8c-1668-4a0c-adf7-4b1fc7de7b2f): lunchrush-worker não moveu a entrega para offered dentro do prazo
- ordem 16 (entrega 646b0bf1-5dfc-4676-a617-2ff5f77aef50): lunchrush-worker não moveu a entrega para offered dentro do prazo
