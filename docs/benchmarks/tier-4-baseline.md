# Baseline do tier 4

**Medido** em 2026-07-26, ambiente `local`: LocalStack 3.8.1 (S3,
Secrets Manager, KMS), cluster `kind` v1.31.0 chamado `dispatch`, chart
Helm `deploy/helm/dispatch`, KEDA 2.20, Prometheus v3.0.1 com regras de
SLO carregadas, infra compartilhada (PostgreSQL, Redis, Redpanda
v24.3.1) via `docker compose`.

## Correção

- `go build ./...`, `gofmt -l .` e `go vet ./...` limpos em todo o
  código novo/alterado (`internal/platform/objectstore`,
  `internal/platform/secrets`, `cmd/delivery-api`,
  `internal/platform/httpapi`).
- `go test -race ./...` e `go test -tags=integration -race ./test/integration/...`
  passando (25,6s a suíte de integração), incluindo os testes herdados
  do tier 3 (outbox, inbox, disputa concorrente, cache do tracking).
- `helm lint deploy/helm/dispatch`: 0 charts com falha.
- `terraform plan` no ambiente `aws-lab`: "No changes" depois de um
  `apply` limpo (bucket S3, segredo no Secrets Manager, chave KMS).

## Infraestrutura provisionada (Terraform contra LocalStack)

- `aws s3 ls` → `dispatch-delivery-receipts` (versionamento `Enabled`,
  SSE-S3, bloqueio de acesso público);
- `aws secretsmanager list-secrets` → `dispatch/jwt-secret`, cifrado por
  uma chave KMS criada junto (`alias/dispatch-aws-lab-jwt`);
- `delivery-api` consumindo os dois em runtime: JWT resolvido do Secrets
  Manager na inicialização, comprovante de cada entrega concluída subido
  para o bucket (best-effort, não bloqueia o efeito de negócio).
- Evidência completa: `docs/benchmarks/tier-4-localstack-evidencia.txt`.

## Deploy (Helm no lugar de Kustomize)

- cinco `Deployment` (`delivery-api`, `dispatch-worker`,
  `tracking-ingest`, `tracking-projector`, `notification-worker`) via
  `helm upgrade --install`, todos `Running` e `1/1`;
- dois `HorizontalPodAutoscaler` (CPU), dois `PodDisruptionBudget`, cinco
  `NetworkPolicy` deny-by-default + liberações explícitas;
- smoke test via `kubectl exec` de dentro do cluster contra o `Service`:
  `GET /healthz` e `GET /readyz` responderam `200`.
- Evidência completa: `docs/benchmarks/tier-4-helm-evidencia.txt`.

## KEDA (autoscaling por lag de consumer group)

- `dispatch-worker` escalado de **0 para 3 réplicas** por lag artificial
  de 40 mensagens no tópico `dispatch.delivery-events` (limiar
  configurado: 5), em menos de 1 minuto (`pollingInterval: 5s`);
- depois do consumo do lag, `TOTAL-LAG` voltou a 0 e o
  `cooldownPeriod` (30s) leva o `Deployment` de volta a 0 réplicas sem
  intervenção manual;
- Evidência completa: `docs/benchmarks/tier-4-keda-evidencia.txt`.

## Chaos (quatro cenários, todos contra infraestrutura real)

| Cenário | Resultado |
| --- | --- |
| Pod kill do `delivery-api` | 100/100 requisições `200` durante a troca de pod |
| Falha do Redis | fluxo completo (`token → criar → ingerir posição → ler posição`) `200` em todas as etapas com o Redis parado |
| Redpanda pausado | `POST /deliveries` continuou `201`; 6 eventos acumulados no outbox publicaram todos em <5s após o `unpause` |
| Latência 300ms±100ms no PostgreSQL via Toxiproxy | latência de `POST /deliveries` subiu de ~2,6ms para 1,8-4,4s; zero erros |

Detalhe completo, formato hipótese → observação → recuperação →
aprendizado, em `docs/benchmarks/chaos-tier-4.md`.

## SLOs como código

Três SLOs (disponibilidade e latência do `delivery-api`, disponibilidade
do `tracking-ingest`), cada um com quatro recording rules (janelas
5m/30m/1h/6h) e dois alertas de burn-rate (rápido: 14.4x; lento: 6x).
Seis grupos de regras carregados no Prometheus sem erro de avaliação;
valores computados corretamente após tráfego real (0% de erro, 0% de
violação de latência em condição normal). Evidência completa:
`docs/benchmarks/tier-4-slo-evidencia.txt`.

## O que não foi medido neste tier

- SBOM, scan de imagem/dependências e assinatura (cosign/syft/grype):
  não executado por restrição de tempo/memória da máquina compartilhada
  com outro laboratório; ver `docs/benchmarks/tier-4-what-breaks-next.md`;
- teste de carga dedicado ao tier 4 (steady state + spike 3x + soak
  curto): não executado nesta passada, mesma restrição acima;
- Multi-AZ real (EKS, MSK, RDS, ElastiCache): fora do escopo local, ver
  `docs/limitacoes-simulacao-local.md`.
