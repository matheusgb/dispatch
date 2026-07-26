# dispatch

Uma plataforma de última milha precisa encontrar um entregador, acompanhar o
deslocamento e manter cliente e operação informados mesmo sob disputa
concorrente, picos de tráfego e falhas parciais. Este repositório constrói
essa plataforma em Go, começando por um monólito modular correto e evoluindo,
tier por tier, até uma arquitetura celular multi-região com prova formal de
protocolo.

A partir do tier 4, o roadmap original pede AWS real. Este projeto não usa
conta de nuvem paga: veja `docs/limitacoes-simulacao-local.md` para o que é
simulado com ferramentas locais maduras e o que não tem equivalente honesto.

```text
tier atual: 1, concluído
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
| k6 smoke (jornada completa, 5 VUs, 10s)       | 0% de falha, p95 3,61 ms  | local    | `docs/benchmarks/k6-smoke-tier-1.txt` |
| LunchRush (200 ordens, pool dimensionado)     | 0 erros, 40/40 chaves repetidas resolvidas corretamente | local | `docs/benchmarks/lunchrush-tier-1.md` |
| LunchRush (pool de entregadores escasso)      | 240 disputas resolvidas sem dupla atribuição | local | `docs/benchmarks/lunchrush-tier-1-pool-escasso.md` |

Todos os números acima são **Medido** em ambiente local de desenvolvimento,
não em produção. Os rótulos usados neste repositório são Premissa, Meta e
Medido, nunca um número solto.

## Como executar

```bash
docker compose up -d postgres
export DATABASE_URL="postgres://dispatch:dispatch@localhost:5432/dispatch?sslmode=disable"
go run ./cmd/migrate up
go run ./cmd/delivery-api
```

Testes e carga:

```bash
make test              # unitários, incluindo as 100 mil sequências de estado
make test-race         # com o detector de corrida
make test-integration  # requer o Postgres do docker compose acima, já migrado
make load-smoke        # k6, requer o delivery-api no ar
make load-lunchrush    # LunchRush, requer o delivery-api no ar
```

Passo a passo completo, com o que esperar em cada comando, em
`docs/passo-a-passo/tier-1.md`.

## Estágio atual e próximo gate

Tier 1 concluído: núcleo correto e mensurável em Go (lifecycle da entrega,
cadastro e disponibilidade de entregador, oferta e expiração com relógio
injetável, aceite concorrente, ciclo completo até `delivered`, API HTTP com
graceful shutdown, métricas RED/negócio, OpenAPI, LunchRush e smoke em k6).

Não entregue neste tier, por não ser possível nesta forma de trabalho:
demonstração em vídeo (sem gravação de tela disponível) e release formal no
GitHub além da tag `tier-1.0.0` e do próprio histórico de commits.

O tier 2 (Redis, tracking de GPS, SSE, observabilidade, autenticação e
chaos local) começa a partir daqui.
