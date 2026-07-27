# Baseline do tier 2

**Medido** em 2026-07-26, ambiente `local` (docker compose com o perfil
`app`: PostgreSQL, Redis, `delivery-api`, `dependency-simulator`; perfil
`observability`: Prometheus e Grafana provisionados).

## Correção

- `TestTracking_LatePositionNeverOverridesNewer`,
  `TestTracking_NewEpochSupersedesOldSequence`,
  `TestTracking_DuplicatePositionIsIdempotent`: monotonicidade por
  `(tracking_session_epoch, sequence)` (invariante 7) e idempotência do log
  append-only (invariante 8), com `-race` limpo.
- `TestTrackingCache_ReadThroughOnMiss`,
  `TestTrackingCache_FallsBackToPostgresWhenRedisIsDown`: cache-aside e
  fallback do Redis, incluindo apontar o cliente para um endereço
  inalcançável e confirmar que a leitura não falha.
- Autorização por recurso confirmada manualmente: token do dono da entrega
  recebe `200`, token de outro caller recebe `403`, requisição sem token
  recebe `401` (ver passo a passo).
- Rate limit por caller confirmado: burst de ~40 requisições seguido de
  `429` (`ratelimit.PerCaller`, 20 rps / burst 40).

## Carga

- **k6 smoke** (`loadtest/k6/smoke.js`): jornada completa de lifecycle,
  5 VUs, 10s, 0% de falha, p95 3,61ms (herdado do tier 1, revalidado).
- **k6 tracking** (`loadtest/k6/tracking-smoke.js`): emissão de token,
  ingestão de GPS e leitura da posição atual, 5 VUs (um caller cada), 10s,
  **0% de falha**, p95 3,78ms. Evidência em `k6-tracking-tier-2.txt` e
  `.json`.
- **LoadGen golden path** (`loadgen-tier-2-golden.md`): 30 ordens,
  concorrência 1, ciclo completo com GPS, **0 erros**, 72 posições
  enviadas e todas avançaram a projeção.
- **LoadGen sob identidade compartilhada** (
  `loadgen-tier-2-rate-limit-compartilhado.md`): 100 ordens, concorrência
  5, todas usando o mesmo caller `loadgen` para GPS. 69 erros, quase
  todos `429`: achado real documentado em `tier-2-what-breaks-next.md`, não
  um bug do rate limit.

## Chaos

Ver `chaos-tier-2.md`: Redis removido sem falha de leitura, `delivery-api`
morto no meio da carga sem dupla atribuição, latência de 300ms injetada no
PostgreSQL via Toxiproxy sem nenhuma requisição falhando (só mais lentas).

## Observabilidade

Dashboard `lunchrush — RED e negócio` provisionado no Grafana
(`observability/grafana/provisioning`), com rate/errors/duration por rota e
métricas de negócio (entregas por resultado, ingestão de GPS, notificações
por resultado). Confirmado com tráfego real do LoadGen: métrica
`lunchrush_deliveries_completed_total` no Prometheus bateu com a contagem do
relatório do LoadGen (83 em ambos).
