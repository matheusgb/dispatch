# Baseline do tier 6: portabilidade entre "clouds" locais

Commit desta execução: ver `git log -1` no momento do commit que inclui
este arquivo. Data: 2026-07-26. Ambiente: laboratório local (esta
máquina), dois stacks `docker compose` independentes (`cloud-a` = stack
principal, `cloud-b` = `docker-compose.cloud-b.yml`), sem nenhuma cloud
real envolvida (ver `docs/limitacoes-simulacao-local.md`, seção Tier 6).

Todo número abaixo é **Medido** nesta execução, não meta nem premissa,
salvo indicação contrária explícita.

## 1. Artefato: mesmo digest nos dois stacks

```
$ docker inspect lunchrush-delivery-api-1 --format '{{.Image}}'
sha256:e3c37da8c260f47e852ffc5734cf1bdf9537a1ff6282b86b476eb096addcfa43
$ docker inspect cloudb-delivery-api-1 --format '{{.Image}}'
sha256:e3c37da8c260f47e852ffc5734cf1bdf9537a1ff6282b86b476eb096addcfa43
```

Idêntico. `docker-compose.cloud-b.yml` não builda nenhuma imagem própria
(ver ADR 0021).

## 2. Contrato HTTP: k6 smoke nos dois stacks

Mesmo script (`loadtest/k6/smoke.js`), mesmo perfil (5 VUs, 10s),
diferindo só em `BASE_URL`.

| Métrica | cloud-a (`:8083`) | cloud-b (`:18083`) |
| --- | --- | --- |
| `http_reqs` | 3.915 | 3.915 |
| throughput | 356,8 req/s | 388,8 req/s |
| `http_req_failed` | 0,00% (0/3915) | 0,00% (0/3915) |
| `http_req_duration` p95 | 4,70 ms | 3,91 ms |
| iterações completas | 435/435 | 435/435 |

Relatórios completos: `docs/benchmarks/tier-6-portability/k6-smoke-cloud-a.json`,
`k6-smoke-cloud-b.json`.

A diferença de throughput e p95 entre os dois (cloud-b ligeiramente mais
rápido) não tem causa investigada nesta sessão — ambos os stacks
competiam pelos mesmos recursos físicos da máquina ao mesmo tempo; a
diferença fica registrada como observação, não como conclusão sobre qual
"provedor" é mais rápido (os dois são o mesmo hardware).

## 3. Contrato de dados: testes de integração nos dois bancos

Mesmo binário de teste (`go test -tags=integration -race -count=1
./test/integration/...`), sem nenhuma alteração de código entre as duas
execuções, só `DATABASE_URL`/`KAFKA_BROKERS`:

| Ambiente | Resultado | Duração |
| --- | --- | --- |
| cloud-a (`localhost:5432`, `localhost:19092`) | `ok` | 20,629s |
| cloud-b (`localhost:15432`, `localhost:29093`) | `ok` | 16,067s |

Inclui os dois testes de fencing multi-shard (`TestFencing_StaleEpochWriterNeverWrites`,
`TestFencing_TwoConcurrentPromotesOnlyOneEpochWins`) passando com `-race`
nos dois bancos, sem nenhuma alteração de `internal/fencing`.

## 4. Infraestrutura: Terraform por provedor

| Ambiente | `apply` | Recursos | `destroy` |
| --- | --- | --- | --- |
| `cloud-a` (LocalStack `:4566`) | sucesso | 8 criados | sucesso, 8 destruídos |
| `cloud-b` (LocalStack `:14566`) | sucesso | 8 criados | sucesso, 8 destruídos |

Detalhe completo: `docs/benchmarks/tier-6-portability/terraform-evidence.md`.

## 5. Failover cross-cloud: RTO e RPO medidos

Cenário completo em `docs/benchmarks/tier-6-portability/failover-transcript.txt`,
usando `cmd/cloudfailover` (reusa `internal/fencing` do tier 5 sem
alteração de protocolo).

| Métrica | Valor medido |
| --- | --- |
| assignments confirmados em cloud-a antes do backup | 10 |
| assignments confirmados em cloud-a depois do backup (janela de risco) | 5 |
| assignments restaurados em cloud-b (do backup) | 10 |
| **RPO** (assignments perdidos na janela dump→interrupção) | **5** (33% do que tinha sido confirmado) |
| janela de tempo dump→interrupção | 0,58s |
| **RTO** (interrupção→cloud-b promovido e aceitando escrita) | **11,54s** |
| tentativas do writer antigo (epoch velho) contra cloud-b, pós-promoção | 10/10 rejeitadas (`ErrStaleFence`) |
| tentativas do writer novo (epoch novo) contra cloud-b, pós-promoção | 5/5 aceitas |
| reconciliação final (cloud-a congelado vs cloud-b pós-recuperação) | 15 = 15 |

O RTO medido (~11,5s) é dominado pelo tempo de `docker compose stop` +
`pg_dump`/`pg_restore`/`dropdb`/`createdb` de um banco de laboratório
pequeno (dezenas de linhas). Não generaliza para um banco de produção
real nem para latência de rede real entre dois provedores geograficamente
distantes — ver `docs/adr/0023` para a discussão completa.

## 6. Dependência oculta revelada

Ver `docs/benchmarks/tier-6-portability/shared-dependency-transcript.txt`:
remover a imagem `lunchrush-delivery-api` do daemon Docker local (depois de
parar todos os containers que a referenciavam, nos dois stacks) faz
`cloud-b` falhar ao recriar seu container com `pull access denied`. A
dependência oculta real desta configuração é o processo de build/registry
de imagem, compartilhado pelas duas "clouds" — não Postgres, Redis, Kafka
ou rede, que já são duplicados e isolados por stack.

## Ambiente

- host: máquina compartilhada única (não duas máquinas/regiões reais);
- Docker Engine local, dois projetos compose (`lunchrush` = cloud-a,
  `cloudb` = cloud-b), redes Docker separadas;
- Postgres 17-alpine, Redis 8-alpine, Redpanda v24.3.1, LocalStack 3.8.1,
  Terraform com provider `hashicorp/aws` 5.100.0, k6 (versão do sistema),
  Go 1.26.5.
