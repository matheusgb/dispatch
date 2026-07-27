# Baseline do tier 3

**Medido** em 2026-07-26, ambiente `local`: PostgreSQL, Redis, Redpanda
v24.3.1 (3 partições por tópico) e `dependency-simulator` via docker
compose; `delivery-api`, `lunchrush-worker`, `tracking-ingest`,
`tracking-projector` e `notification-worker` testados tanto via
`docker compose --profile app` quanto dentro de um cluster `kind` próprio
(`deploy/kubernetes/base`), com a infra externa acessada via
`Service`/`Endpoints` manuais (ver ADR 0011).

## Correção

- `TestOutbox_RelayPublishesAndMarks`,
  `TestOutbox_CrashBeforeMarkRepublishesButInboxDedupsEffect`,
  `TestInbox_DedupesRepeatedEventID`: outbox transacional, relay,
  republicação depois de uma falha simulada entre o ack do Kafka e a marca
  de publicado, e dedup do efeito de negócio pelo inbox — contra um
  Redpanda real, com `-race` limpo.
- Poison pill: uma mensagem JSON malformada publicada direto no tópico foi
  para `lunchrush.delivery-events.dlq` sem travar a partição; o consumidor
  continuou respondendo `/healthz` durante e depois.
- Invariante 1/2 sob o pipeline distribuído: `TestLunchRush_*` do tier 1
  seguem passando: a exclusividade de atribuição não mudou, só passou a
  coexistir com eventos assíncronos.
- `docker exec lunchrush-redpanda-1 rpk group describe lunchrush-worker` com
  4 réplicas do `kind` escaladas (mais uma do compose, 5 membros no total):
  só 3 aparecem atribuídos a uma partição, confirmando a invariante de
  capacidade descrita no ADR 0010.

## Carga

- **LoadGen golden path, modo `-distributed`** (concorrência 3, 20
  ordens): **0 erros**, 19 concluídas, 1 recusada, 57 posições de GPS
  aceitas e todas confirmadas na projeção. Duração total 1m56s (~6s por
  ordem em média). Evidência em `loadgen-tier-3-golden.md`.
- **LoadGen sob concorrência 8** (40 ordens): 23 timeouts do próprio
  LoadGen esperando `offered` dentro de 30s. Toda entrega "que errou" foi
  conferida depois no PostgreSQL: todas chegaram a `offered`; sem
  `assign` a tempo, o `lunchrush-worker` reciclou corretamente ao expirar.
  Achado real, não bug: ver ADR 0009 (latência do relay do outbox) e
  `loadgen-tier-3-alta-concorrencia.md`.
- **Validação completa via docker compose** (15 ordens, concorrência 3):
  0 erros, confirmando que a mesma lógica funciona igual empacotada em
  container, não só com `go run` local. Evidência em
  `loadgen-tier-3-docker-compose.md`.
- **Validação completa via kind** (uma entrega manual, ponta a ponta):
  `created -> offered` em 6 tentativas de poll (~3s), `assign`, `pickup`,
  `deliver` todos `204`, GPS aceito por `tracking-ingest` e visível em
  `tracking-projector` cerca de 2s depois. Prova que o pipeline funciona
  dentro do cluster, não só localmente.

## Latência ponta a ponta (achado principal do tier)

Uma entrega isolada, sem concorrência, leva **~3,8s** de `created` até
`offered`. Isso não é uma chamada de rede: é o caminho
`created -> ready_for_lunchrush -> offered` atravessando o relay do outbox
(publica a cada 1s) duas vezes. Ver ADR 0009 para a decisão de manter esse
intervalo e o motivo.

## Observabilidade

`/metrics` exposto em todos os cinco serviços (workers ganharam um servidor
HTTP só para isso, `internal/platform/workerhttp`), scrape configurado no
Prometheus (`observability/prometheus/prometheus.yml`).
