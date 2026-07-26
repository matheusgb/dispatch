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
tier atual: 4, itens A-D concluídos
Clientes -> delivery-api ---------> outbox -> Kafka -> dispatch-worker (escala por lag via KEDA)
                                                     -> notification-worker
Entregador -> tracking-ingest ----> Kafka -> tracking-projector -> Postgres + Redis
delivery-api -> S3 (comprovante) e Secrets Manager (JWT), via Terraform contra LocalStack
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
| Terraform contra LocalStack                   | bucket S3 + segredo no Secrets Manager criados de verdade | `docs/adr/0012-terraform-contra-localstack.md` |
| deploy via Helm no `kind`                     | 5 Deployments 2/2, HPA, PDB, NetworkPolicy | `docs/benchmarks/tier-4-helm-evidencia.txt` |
| KEDA escalando por lag do consumer group      | 0 → 3 réplicas com lag artificial de 40 msgs | `docs/benchmarks/tier-4-keda-evidencia.txt` |
| chaos: pod kill do delivery-api no `kind`     | 100/100 requisições OK via Service | `docs/benchmarks/chaos-tier-4.md` |
| chaos: Redpanda pausado                       | escrita continua; outbox publica tudo em <5s após voltar | `docs/benchmarks/chaos-tier-4.md` |
| SLOs como código (burn-rate multi-janela)     | 6 grupos de regras carregados, 0 erro de avaliação | `docs/adr/0015-slo-como-codigo.md` |

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

Para rodar em Kubernetes local via Helm (tier 4, ver ADR 0013; substitui
o Kustomize do tier 3, que continua em `deploy/kubernetes/` como
histórico congelado):

```bash
make helm-up   # cria o cluster kind "dispatch", builda as imagens e instala o chart Helm
make keda-up   # instala o KEDA e liga o ScaledObject de dispatch-worker (escala por lag)
make kind-down # destrói o cluster
```

Para provisionar S3 e Secrets Manager contra o LocalStack (tier 4, nunca
AWS real, ver ADR 0012):

```bash
make tf-aws-lab-up    # sobe o LocalStack e aplica o Terraform
make tf-aws-lab-down  # destrói os recursos e para o LocalStack
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
`docs/passo-a-passo/tier-1.md`, `tier-2.md`, `tier-3.md` e `tier-4.md`.

## Estágio atual e próximo gate

Tier 4 com os itens A-D do escopo concluídos, com evidência real (ver
`docs/benchmarks/tier-4-baseline.md`):

- **Terraform contra LocalStack** (ADR 0012): bucket S3 e segredo no
  Secrets Manager/KMS provisionados de verdade, consumidos em runtime
  pelo `delivery-api`;
- **Helm no lugar de Kustomize** (ADR 0013): um chart parametrizado
  substitui os cinco manifests quase idênticos do tier 3, validado com
  `helm lint` e deploy real no `kind`;
- **KEDA por lag de consumer group** (ADR 0014): `dispatch-worker` escala
  de 0 a 6 réplicas por lag real do Kafka, não por CPU;
- **Chaos reduzido** (`docs/benchmarks/chaos-tier-4.md`): quatro
  cenários contra infraestrutura real (pod kill, falha do Redis,
  Redpanda pausado, latência via Toxiproxy no PostgreSQL);
- **SLOs como código** (ADR 0015): três SLOs com recording rules
  multi-janela e alertas de burn-rate no Prometheus.

Numa segunda passada, antes do tier 5, os três itens que tinham ficado de
fora por restrição de memória compartilhada com o `edge-lab` foram
fechados com evidência real: **SBOM/scan/assinatura de imagem** (ADR 0016,
`docs/benchmarks/supply-chain/`), **teste de carga dedicado** (steady
state + spike 3x, 0% de erro, p95 de 7,33ms,
`docs/benchmarks/tier-4-load/`) e **runbook de backup/recuperação
distribuída** (`docs/runbooks/backup-e-recuperacao-distribuida.md`, RPO
real de 39s medido). EKS, MSK, RDS/Aurora e ElastiCache continuam fora de
escopo local (LocalStack community não os implementa; ver
`docs/limitacoes-simulacao-local.md`). Mapa completo do que ainda falta:
`docs/benchmarks/tier-4-what-breaks-next.md`.

O tier 5 (células, fencing e multi-região) está em andamento — ver seção
abaixo.
