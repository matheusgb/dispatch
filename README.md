# dispatch

Uma plataforma de última milha precisa encontrar um entregador, acompanhar o
deslocamento e manter cliente e operação informados mesmo sob disputa
concorrente, picos de tráfego e falhas parciais. Este repositório constrói
essa plataforma em Go, começando por um monólito modular correto e evoluindo,
tier por tier, até uma arquitetura celular multi-região com prova formal de
protocolo.

```text
tier atual: 1, início
REST API -> monólito modular -> PostgreSQL
```

## Invariantes já exigidas neste tier

1. Uma entrega possui no máximo um entregador ativo.
2. Um entregador possui no máximo uma entrega ativa.
3. Uma transição de estado só ocorre a partir de um estado permitido.
4. Um estado terminal nunca retorna a um estado anterior.
5. Repetir uma requisição com a mesma chave de idempotência produz um único
   efeito de negócio.
6. Um comando confirmado como durável não desaparece silenciosamente.

Ver a lista completa e a partir de qual tier cada invariante entra em
`docs/requisitos-tier-1.md`.

## Placar de resultados

| Resultado                                    | Valor                     | Ambiente | Relatório |
| --------------------------------------------- | ------------------------- | -------- | --------- |
| atribuições concorrentes corretas             | 20 tentativas → 1 vencedora | local  | `docs/benchmarks/tier-1-baseline.md` |
| sequências de estado sem transição inválida   | 100.000 (fuzz table) + 18.049.426 (fuzz nativo) | local | `docs/benchmarks/tier-1-baseline.md` |
| criação idempotente (round trip PostgreSQL)   | 1,10 ms/op                | local    | `docs/benchmarks/tier-1-baseline.md` |
| aceite condicional (round trip PostgreSQL)    | 0,97 ms/op                | local    | `docs/benchmarks/tier-1-baseline.md` |
| p50/p95/p99 sob carga concorrente real (k6/LunchRush) | ainda não medido   | n/a      | pendente  |

Os números acima são **Medido** em ambiente local de desenvolvimento, não em
um cenário de carga real: falta o LunchRush e o k6 do backlog do tier 1 para
produzir p50/p95/p99 sob concorrência sustentada. Os rótulos usados neste
repositório são Premissa, Meta e Medido, nunca um número solto.

## Como executar

```bash
docker compose up -d postgres
export DATABASE_URL="postgres://dispatch:dispatch@localhost:5432/dispatch?sslmode=disable"
go run ./cmd/migrate up
go run ./cmd/delivery-api
```

Testes:

```bash
make test            # unitários, incluindo as 100 mil sequências de estado
make test-race       # com o detector de corrida
make test-integration # requer o Postgres do docker compose acima, já migrado
```

## Estágio atual e próximo gate

Tier 1: núcleo correto e mensurável em Go concluído (lifecycle da entrega,
cadastro e disponibilidade de entregador, oferta e expiração com relógio
injetável, aceite concorrente, API HTTP com graceful shutdown e métricas
RED/negócio). Passo a passo completo em
`docs/passo-a-passo/tier-1.md`.

Falta, para fechar o tier 1 por completo: o simulador LunchRush, os cenários
de carga em k6, a demonstração gravada e a tag `tier-1.0.0`. O próximo tier
(2) só começa depois que esses três itens estiverem resolvidos e um novo
limite for medido, não antes.
