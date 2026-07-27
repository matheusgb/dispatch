# Relatório LoadGen — pool de entregadores deliberadamente escasso

**Nota de leitura:** os 22 "erros" abaixo não são bug. Este cenário usa
propositalmente 5 entregadores para 150 ordens com concorrência 30: em vários
momentos, os 5 já estão todos ativos quando uma nova ordem chega, e o
LoadGen esgota o pool de retry sem encontrar um entregador livre. É
exatamente o comportamento esperado da invariante 2 (um entregador, uma
entrega ativa) sob demanda maior que oferta. Comparar com
`loadgen-tier-1.md`, que usa um pool dimensionado (20 entregadores) e
termina com zero erros.

- seed: 999
- ordens simuladas: 150
- entregadores no pool: 5
- duração total: 3.179283198s

## Resultado por desfecho

| Desfecho | Quantidade |
| --- | --- |
| concluídas (assign -> pickup -> deliver) | 99 |
| recusadas | 15 |
| expiradas | 14 |
| erros | 22 |

## Idempotência

- chaves repetidas testadas: 30
- repetições que devolveram o mesmo ID: 30

## Disputa por entregador

- total de tentativas de atribuição rejeitadas por entregador ocupado, absorvidas por retry no pool: 240

## Amostra de falhas

- ordem 2 (entrega 3f42130e-c486-4ba2-adc3-b5c9f7dbb9c3): nenhum entregador do pool ficou livre: atribuir: status 409: {"error":"lunchrush: entregador já possui entrega ativa"}

- ordem 5 (entrega 0ca1ea40-9bd2-48a9-b076-a9533eeee3c2): nenhum entregador do pool ficou livre: atribuir: status 409: {"error":"lunchrush: entregador já possui entrega ativa"}

- ordem 12 (entrega 2b1632fa-7b75-435a-84cf-cd603227d13e): nenhum entregador do pool ficou livre: atribuir: status 409: {"error":"lunchrush: entregador já possui entrega ativa"}

- ordem 14 (entrega bdcb7632-a810-4923-b37b-9c509666f093): nenhum entregador do pool ficou livre: atribuir: status 409: {"error":"lunchrush: entregador já possui entrega ativa"}

- ordem 15 (entrega 6288846f-6843-4aa7-a8cf-d695c95fa7d9): nenhum entregador do pool ficou livre: atribuir: status 409: {"error":"lunchrush: entregador já possui entrega ativa"}

- ordem 19 (entrega b008dd68-260d-4e19-a76f-35afc05e4f13): nenhum entregador do pool ficou livre: atribuir: status 409: {"error":"lunchrush: entregador já possui entrega ativa"}

- ordem 24 (entrega 7e17f6c1-a2e5-46d9-813c-178082e675ad): nenhum entregador do pool ficou livre: atribuir: status 409: {"error":"lunchrush: entregador já possui entrega ativa"}

- ordem 27 (entrega 20e5fe0d-3ece-4e32-a941-43faaa113b75): nenhum entregador do pool ficou livre: atribuir: status 409: {"error":"lunchrush: entregador já possui entrega ativa"}

- ordem 29 (entrega 98876373-6202-476c-9a93-113ece84e13e): nenhum entregador do pool ficou livre: atribuir: status 409: {"error":"lunchrush: entregador já possui entrega ativa"}

- ordem 37 (entrega ab66bc03-7861-4c75-a55a-dd02282ba4e7): nenhum entregador do pool ficou livre: atribuir: status 409: {"error":"lunchrush: entregador já possui entrega ativa"}

