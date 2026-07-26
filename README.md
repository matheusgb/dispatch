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
tier atual: 3, concluído
Clientes -> delivery-api ---------> outbox -> Kafka -> dispatch-worker
                                                     -> notification-worker
Entregador -> tracking-ingest ----> Kafka -> tracking-projector -> Postgres + Redis
```

## Invariantes já exigidas até este tier

1. Uma entrega possui no máximo um entregador ativo.
2. Um entregador possui no máximo uma entrega ativa.
3. Uma transição de estado só ocorre a partir de um estado permitido.
4. Um estado terminal nunca retorna a um estado anterior.
5. Repetir uma requisição com a mesma chave de idempotência produz um único
   efeito de negócio.
6. Um comando confirmado como durável não desaparece silenciosamente.
7. Uma posição com sessão ou sequência anterior nunca substitui uma posição
   mais recente.
8. Uma duplicata pode ser reprocessada, mas não duplica o efeito — agora
   também através de Kafka, outbox e inbox, não só dentro de uma
   transação Postgres.
9. Todo evento de outbox confirmado é publicado ou permanece visível como
   pendente.

Detalhes e a partir de qual tier cada invariante entra:
`docs/requisitos-tier-1.md`.

## Placar de resultados

| Resultado                                    | Valor                     | Relatório |
| --------------------------------------------- | ------------------------- | --------- |
| atribuições concorrentes corretas             | 20 tentativas → 1 vencedora | `docs/benchmarks/tier-1-baseline.md` |
| outbox: crash simulado entre ack e marca      | republica; inbox deduplica o efeito | `docs/adr/0009-latencia-do-outbox-relay.md` |
| poison pill                                   | vai para a DLQ, partição não trava | `docs/benchmarks/chaos-tier-3.md` |
| réplicas de consumer além das partições       | ociosas, confirmado com 5 membros/3 atribuídos | `docs/adr/0010-consumer-replicas-limitadas-por-particoes.md` |
| LunchRush distribuído, golden path            | 0 erros, 19/20 concluídas, GPS ponta a ponta | `docs/benchmarks/lunchrush-tier-3-golden.md` |
| validação via docker compose                  | 0 erros, mesma lógica em container | `docs/benchmarks/lunchrush-tier-3-docker-compose.md` |
| validação via kind (Kubernetes real)          | jornada completa + GPS via port-forward | `docs/passo-a-passo/tier-3.md` |
| latência `created -> offered` isolada         | ~3,8s (dois hops pelo relay do outbox) | `docs/adr/0009-latencia-do-outbox-relay.md` |
| chaos: Redis fora do ar                       | 0 falhas de leitura, latência maior | `docs/benchmarks/chaos-tier-2.md` |
| chaos: delivery-api morto sob carga           | 0 entregadores com dupla atribuição | `docs/benchmarks/chaos-tier-2.md` |
| chaos: 300ms de latência no PostgreSQL        | 0% de falha, p95 de 4,5ms para 1,5s | `docs/benchmarks/chaos-tier-2.md` |

Todos os números acima são **Medido** em ambiente local de desenvolvimento,
não em produção. Os rótulos usados neste repositório são Premissa, Meta e
Medido, nunca um número solto.

## Como executar

```bash
docker compose --profile app --profile observability up -d --build
```

Sobe PostgreSQL, Redis, Redpanda, `dependency-simulator`, os cinco
serviços (`delivery-api`, `dispatch-worker`, `tracking-ingest`,
`tracking-projector`, `notification-worker`), Prometheus e Grafana
(`http://localhost:3000`, login anônimo local). Portas publicadas fora do
padrão (`8083`, `8084`, `8085`, `8092`) evitam colidir com outro laboratório
já rodando no mesmo host — ver `docker-compose.yml`.

Para rodar em Kubernetes local:

```bash
make kind-up    # cria o cluster kind "dispatch", builda e aplica os manifests
make kind-down  # destrói o cluster
```

Testes e carga:

```bash
make test              # unitários
make test-race         # com o detector de corrida
make test-integration  # requer Postgres, Redis e Redpanda locais, já migrados
make load-smoke        # k6, requer o delivery-api no ar
make load-lunchrush    # LunchRush, requer o delivery-api no ar
```

Passo a passo completo, com o que esperar em cada comando:
`docs/passo-a-passo/tier-1.md`, `tier-2.md` e `tier-3.md`.

## Estágio atual e próximo gate

Tier 3 concluído: Kafka (Redpanda) com outbox transacional e relay, inbox
com dedup por consumidor, DLQ para poison pill, `dispatch-worker` reagindo
a eventos em vez de chamada manual, `tracking-ingest`/`tracking-projector`
separados do lifecycle, `notification-worker` assíncrono, e manifests
Kubernetes reais (probes, HPA, NetworkPolicy deny-by-default,
PodDisruptionBudget, sem root) validados num cluster `kind` de verdade.

Não entregue neste tier, por não ser possível nesta forma de trabalho:
demonstração em vídeo. Schema Protobuf/AsyncAPI e evolução de schema
ficaram fora por escopo, não por limite técnico (ver
`docs/benchmarks/tier-3-what-breaks-next.md`).

O tier 4 (AWS simulada localmente — ver
`docs/limitacoes-simulacao-local.md`) começa a partir daqui.
