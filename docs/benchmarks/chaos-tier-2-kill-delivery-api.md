# Relatório LoadGen

- seed: 9001
- ordens simuladas: 80
- entregadores no pool: 12
- duração total: 2.297006745s

## Resultado por desfecho

| Desfecho | Quantidade |
| --- | --- |
| concluídas (assign -> pickup -> deliver) | 7 |
| recusadas | 5 |
| expiradas | 0 |
| erros | 68 |

## Idempotência

- chaves repetidas testadas: 13
- repetições que devolveram o mesmo ID: 13

## Disputa por entregador

- total de tentativas de atribuição rejeitadas por entregador ocupado, absorvidas por retry no pool: 444

## Tracking de GPS

- posições enviadas (entregas concluídas): 33
- posições que avançaram a projeção de última posição: 33

## Amostra de falhas

- ordem 2 (entrega 54f3f270-49bd-49dd-8916-e9a72cdd5d6f): Get "http://localhost:8083/deliveries/54f3f270-49bd-49dd-8916-e9a72cdd5d6f": dial tcp 127.0.0.1:8083: connect: connection refused
- ordem 9 (entrega 6fec9049-4bd4-4e41-af38-4d4ce23a4b44): consultar posição atual: status 429
- ordem 10 (entrega d60fc356-8b1d-402b-b4b6-07cee6b72550): consultar posição atual: status 429
- ordem 11 (entrega c2bbefa3-7f78-4254-b5a4-b623cb821b5c): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 12 (entrega 73580773-ce96-445d-8d80-8e7e75827c93): Get "http://localhost:8083/deliveries/73580773-ce96-445d-8d80-8e7e75827c93": dial tcp 127.0.0.1:8083: connect: connection refused
- ordem 13 (entrega 64c70869-8da4-4e4d-8924-0efeb8938bb0): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 14 (entrega 21f8141a-3aea-49c0-9c57-f95cd4def879): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 15 (entrega 384354e9-a8c3-4ffa-96b7-cccec4d08d43): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 16 (entrega 1175a476-e2b0-4961-8a29-9f7068b71142): registrar posição: status 429: {"error":"limite de requisições excedido"}

- ordem 17 (entrega 7b6e2bed-8d84-47c1-bb2c-ed6bbc7c96a1): Get "http://localhost:8083/deliveries/7b6e2bed-8d84-47c1-bb2c-ed6bbc7c96a1": dial tcp 127.0.0.1:8083: connect: connection refused
