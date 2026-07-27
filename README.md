![Banner do lunch-rush](assets/lunch-rush-banner.svg)

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26">
  <img src="https://img.shields.io/badge/tiers-6%2F6-6f42c1" alt="6 de 6 tiers">
  <img src="https://img.shields.io/badge/status-conclu%C3%ADdo-2ea44f" alt="Status concluído">
  <img src="https://img.shields.io/badge/infra-kafka%20%2B%20kubernetes%20%2B%20multi--regi%C3%A3o-1e50d8" alt="Kafka, Kubernetes e multi-região">
</p>

**Um sistema único de entregas, evoluindo por seis tiers até uma arquitetura
celular multi-região com prova formal de protocolo.**

Uma plataforma de entregas precisa encontrar um entregador, acompanhar o
deslocamento e manter cliente e operação informados mesmo sob disputa concorrente,
picos de tráfego e falhas parciais. O lunch-rush constrói essa plataforma em Go,
começando por um monólito modular correto e terminando numa arquitetura celular
com failover entre dois "provedores" e um protocolo de fencing verificado em TLA+.

[Catálogo dos tiers](#catálogo-dos-tiers) ·
[Rotas de estudo](#rotas-de-estudo) ·
[Resultados medidos](#resultados-medidos) ·
[Como executar](#como-executar)

> [!NOTE]
> O roadmap está fechado no tier 6 (`tier-6.0.0`) e não vai receber novos tiers.
> Mudanças futuras ficam restritas a correções, manutenção de dependências e
> atualização de evidências que deixarem de ser reproduzíveis.

## A ideia em uma imagem

```text
entrega criada
       |
       v
disputa concorrente por um entregador (exatamente um vencedor)
       |
       v
mensageria distribuída com outbox/inbox (duplicata nunca vira dois efeitos)
       |
       v
infraestrutura provada, não só declarada (Terraform, Helm, KEDA, SBOM assinado)
       |
       v
protocolo de fencing verificado formalmente (TLA+) contra split-brain multi-região
       |
       v
mesmo artefato sobrevivendo à troca de "provedor"
```

Diferente de coleções de microprojetos independentes, o lunch-rush é um sistema só:
todo o código pertence ao mesmo domínio, mora no mesmo repositório e evolui tier
por tier sem reescrever o tier anterior.

## O que existe aqui

- **Um sistema único**, não uma coleção de microprojetos: todo o código pertence ao
  mesmo domínio e evolui no mesmo repositório.
- **Go 1.26** do primeiro ao último tier.
- **Seis tiers taggeados** (`tier-1.0.0` a `tier-6.0.0`), cada um fechado só com
  teste de corrida, benchmark, experimento de falha e ADR, nunca só por
  funcionalidade implementada.
- **Toda alegação rotulada** como Premissa, Meta ou Medido, nunca um número solto.
- **Protocolo de fencing verificado formalmente** em TLA+ (TLC real, não só
  code review) e exercitado em código de produção local.
- **Nada de AWS real**: a partir do tier 4 o roadmap original pedia conta paga;
  o que foi simulado localmente e a lacuna correspondente estão documentados,
  nunca escondidos atrás de um número (`docs/limitacoes-simulacao-local.md`).

## Mapa dos tiers

```text
tier 1  monólito modular: idempotência e disputa concorrente por entregador
 |
 +-- tier 2  tracking de GPS, Redis, autenticação, rate limit, observabilidade
 |
 +-- tier 3  distribuído: Kafka, outbox/inbox, serviços separados
 |
 +-- tier 4  "AWS" simulada: Terraform/LocalStack, Helm, KEDA, SLOs, supply chain
 |
 +-- tier 5  arquitetura celular, fencing multi-shard, TLA+ real
 |
 +-- tier 6  portabilidade: dois "provedores", failover de fencing entre eles
```

A ordem segue a profundidade do sistema: primeiro a correção do domínio dentro de
um processo só, depois a comunicação entre processos, depois a infraestrutura que
sustenta isso em escala, depois o protocolo que impede split-brain quando o sistema
vira multi-região, depois a prova de que o mesmo artefato sobrevive a uma troca de
provedor.

## Catálogo dos tiers

| Tier | Conceitos visíveis | Pergunta principal | Prova reproduzida |
| --- | --- | --- | --- |
| [Tier 1 · monólito modular](./docs/passo-a-passo/tier-1.md) | PostgreSQL, `UPDATE` condicional, idempotência | 20 tentativas concorrentes pelo mesmo entregador produzem quantas atribuições? | 20 tentativas, exatamente 1 vencedora (`docs/benchmarks/tier-1-baseline.md`) |
| [Tier 2 · tracking e resiliência local](./docs/passo-a-passo/tier-2.md) | Redis como projeção, auth, rate limit, observabilidade | o que acontece com a leitura de posição quando o Redis, fonte rápida, cai? | 0 falhas de leitura, fallback direto para o PostgreSQL (`docs/benchmarks/chaos-tier-2.md`) |
| [Tier 3 · distribuído com Kafka](./docs/passo-a-passo/tier-3.md) | outbox/inbox, poison pill, DLQ, consumer groups | uma duplicata sobrevive a um crash entre publicar e confirmar sem virar dois efeitos? | crash simulado entre ack e marca: republica, inbox deduplica (`docs/adr/0009-latencia-do-outbox-relay.md`) |
| [Tier 4 · "AWS" simulada e supply chain](./docs/passo-a-passo/tier-4.md) | Terraform/LocalStack, Helm, KEDA, SLOs, SBOM assinado | alguém sem ter escrito uma linha desse código consegue provisionar, escalar e auditar a cadeia de build com segurança? | KEDA escala 0 a 6 réplicas por lag real; 5 imagens com SBOM, 1 assinada e verificada (`docs/benchmarks/tier-4-baseline.md`) |
| [Tier 5 · arquitetura celular e TLA+](./docs/passo-a-passo/tier-5.md) | `internal/fencing`, epoch/lease, TLA+/TLC, LoadGen com rede virtual | dois writers concorrentes com epoch diferente podem produzir dois assignments para a mesma entrega? | 0 violação em 1086 estados do TLC; 20 tentativas com epoch velho, 0 sucesso (`docs/adr/0017-tla-real-para-o-protocolo-de-fencing.md`) |
| [Tier 6 · portabilidade entre provedores](./docs/passo-a-passo/tier-6.md) | dois `docker compose` independentes, `cmd/cloudfailover`, dependência oculta | o que realmente muda quando o "provedor" muda? | mesmo digest OCI nos dois lados; failover com RTO de 11,54s e writer antigo rejeitado em 10/10 (`docs/benchmarks/tier-6-baseline.md`) |

## Rotas de estudo

### Quero correção de domínio e concorrência

```text
tier 1 -> tier 2
```

O tier 1 isola a disputa concorrente por um entregador num monólito só. O tier 2
mantém essa correção e adiciona um segundo caminho de escrita quente: GPS via Redis.

### Quero decisões de mensageria distribuída

```text
tier 2 -> tier 3
```

O tier 2 ainda é um processo só falando com Postgres e Redis. O tier 3 quebra isso
em serviços separados comunicando por Kafka, e a duplicata vira o problema central.

### Quero infraestrutura como prova, não como slide

```text
tier 3 -> tier 4
```

O tier 3 prova a lógica de domínio distribuída. O tier 4 prova que a infraestrutura
em volta, provisionamento, deploy, escala, cadeia de build, também é real e
auditável, não só declarada.

### Quero protocolo formal e multi-região

```text
tier 4 -> tier 5
```

O tier 4 entrega uma plataforma operável numa região só. O tier 5 divide isso em
células e verifica formalmente, em TLA+, que o protocolo de fencing impede dois
writers ativos ao mesmo tempo.

### Quero o roadmap inteiro, do monólito ao multi-cloud

```text
tier 1 -> tier 2 -> tier 3 -> tier 4 -> tier 5 -> tier 6
```

## Resultados medidos

Estes números pertencem aos ambientes locais descritos nas evidências. Eles tornam
o comportamento visível, mas não formam um teste universal de desempenho nem
representam capacidade de produção. Todo rótulo é Premissa, Meta ou Medido, nunca
um número solto.

| Resultado                                    | Valor                     | Relatório |
| --------------------------------------------- | ------------------------- | --------- |
| atribuições concorrentes corretas             | 20 tentativas, 1 vencedora | `docs/benchmarks/tier-1-baseline.md` |
| outbox: crash simulado entre ack e marca      | republica; inbox deduplica o efeito | `docs/adr/0009-latencia-do-outbox-relay.md` |
| poison pill                                   | vai para a DLQ, partição não trava | `docs/benchmarks/chaos-tier-3.md` |
| réplicas de consumer além das partições       | ociosas, confirmado com 5 membros/3 atribuídos | `docs/adr/0010-consumer-replicas-limitadas-por-particoes.md` |
| LoadGen distribuído, golden path              | 0 erros, 19/20 concluídas, GPS ponta a ponta | `docs/benchmarks/loadgen-tier-3-golden.md` |
| validação via docker compose                  | 0 erros, mesma lógica em container | `docs/benchmarks/loadgen-tier-3-docker-compose.md` |
| validação via kind (Kubernetes real)          | jornada completa + GPS via port-forward | `docs/passo-a-passo/tier-3.md` |
| latência `created -> offered` isolada         | ~3,8s (dois hops pelo relay do outbox) | `docs/adr/0009-latencia-do-outbox-relay.md` |
| chaos: Redis fora do ar                       | 0 falhas de leitura, latência maior | `docs/benchmarks/chaos-tier-2.md` |
| chaos: delivery-api morto sob carga           | 0 entregadores com dupla atribuição | `docs/benchmarks/chaos-tier-2.md` |
| chaos: 300ms de latência no PostgreSQL        | 0% de falha, p95 de 4,5ms para 1,5s | `docs/benchmarks/chaos-tier-2.md` |
| Terraform contra LocalStack                   | bucket S3 + segredo no Secrets Manager criados de verdade | `docs/adr/0012-terraform-contra-localstack.md` |
| deploy via Helm no `kind`                     | 5 Deployments 2/2, HPA, PDB, NetworkPolicy | `docs/benchmarks/tier-4-helm-evidencia.txt` |
| KEDA escalando por lag do consumer group      | 0 a 3 réplicas com lag artificial de 40 msgs | `docs/benchmarks/tier-4-keda-evidencia.txt` |
| chaos: pod kill do delivery-api no `kind`     | 100/100 requisições OK via Service | `docs/benchmarks/chaos-tier-4.md` |
| chaos: Redpanda pausado                       | escrita continua; outbox publica tudo em <5s após voltar | `docs/benchmarks/chaos-tier-4.md` |
| SLOs como código (burn-rate multi-janela)     | 6 grupos de regras carregados, 0 erro de avaliação | `docs/adr/0015-slo-como-codigo.md` |
| SBOM + scan + assinatura de imagem            | 5 imagens com SBOM, 0 vulnerabilidade encontrada, 1 assinada e verificada | `docs/adr/0016-sbom-scan-e-assinatura-de-imagem.md` |
| teste de carga dedicado (steady + spike 3x)   | 0% erro em ~144 mil requisições, p95 de 7,33ms | `docs/benchmarks/tier-4-load/README.md` |
| backup/recuperação distribuída                | RPO real de 39s, restauração validada, gap medido contra Kafka | `docs/runbooks/backup-e-recuperacao-distribuida.md` |
| TLA+ real (TLC) do protocolo de fencing       | 0 violação em 1086 estados; mutation test acha contraexemplo em 4 passos | `docs/adr/0017-tla-real-para-o-protocolo-de-fencing.md` |
| fencing multi-shard: writer com epoch velho   | 20 tentativas concorrentes, 0 sucessos, 20 rejeições | `docs/adr/0018-fencing-lease-e-epoch.md` |
| arquitetura celular local + noisy neighbor    | isolamento de dados provado; p95 sobe de 14ms para 24ms sob célula vizinha saturada | `docs/benchmarks/tier-5-cells/README.md` |
| LoadGen com rede/relógio virtuais             | 2 execuções, mesma seed, relatórios idênticos; 171/171 clock skew seguros | `docs/benchmarks/tier-5-loadgen-netfault/README.md` |
| soak reduzido (tier 5)                        | 2000 ordens, 1800 concluídas, 5295 posições de GPS, 0 violação | `docs/benchmarks/tier-5-baseline.md` |
| mesmo digest OCI em cloud-a e cloud-b          | confirmado por `docker inspect`, sem rebuild em cloud-b | `docs/adr/0021-objetivo-e-limites-da-estrategia-multi-cloud.md` |
| contrato HTTP/dados nos dois provedores        | k6 smoke 0% erro e `go test -race` de integração `ok` nos dois bancos | `docs/tier-6-matriz-portabilidade.md` |
| Terraform separado por provedor                | 8 recursos aplicados e destruídos em cloud-a e cloud-b, independentes | `docs/benchmarks/tier-6-portability/terraform-evidence.md` |
| failover de fencing entre cloud-a e cloud-b    | RTO 11,54s, RPO de 5 assignments, writer antigo 10/10 rejeitado | `docs/benchmarks/tier-6-baseline.md` |
| dependência oculta compartilhada revelada      | `pull access denied` em cloud-b ao remover a imagem compartilhada | `docs/benchmarks/tier-6-portability/shared-dependency-transcript.txt` |

Os arquivos dentro de `docs/benchmarks/` guardam os resultados completos,
requisições, métricas e logs produzidos por cada experimento.

## Tecnologias usadas

| Área | Ferramentas e conceitos |
| --- | --- |
| Linguagem e ambiente | Go 1.26, monólito modular único, Docker |
| Dados | PostgreSQL (`pgx`), Redis, `golang-migrate` |
| Mensageria | Kafka (Redpanda local), outbox/inbox, KEDA (escala por lag de consumer group) |
| Autenticação e segurança | JWT assinado, rate limit, SBOM + scan + assinatura de imagem |
| Observabilidade | Prometheus, Grafana, SLOs como código (burn-rate multi-janela) |
| Plataforma | Kubernetes (`kind`), Helm, ArgoCD (GitOps), Terraform, LocalStack |
| Prova formal e chaos | TLA+/TLC, mutation testing, Toxiproxy, scripts de chaos local reproduzíveis |
| Multi-região e multi-cloud | arquitetura celular, `cellrouter`, dois "provedores" independentes, `cloudfailover` |
| Geração de carga | k6, LoadGen (simulador determinístico do próprio domínio) |

Uma tecnologia só entra quando um ADR responde por que ela é necessária agora, não
porque o roadmap a listava adiante.

## Como executar

### Pré-requisitos

- Go `1.26`;
- Docker com Compose;
- um cluster `kind` e Terraform para os tiers 4 em diante;
- `k6` para os testes de carga dedicados;
- Java para o TLA+ real do tier 5 (`tla2tools.jar`).

Clone o repositório:

```bash
git clone https://github.com/matheusgb/lunch-rush.git
cd lunch-rush
```

Suba a stack completa:

```bash
docker compose --profile app --profile observability up -d --build
```

Sobe PostgreSQL, Redis, Redpanda, `dependency-simulator`, os cinco serviços
(`delivery-api`, `lunchrush-worker`, `tracking-ingest`, `tracking-projector`,
`notification-worker`), Prometheus e Grafana (`http://localhost:3000`, login
anônimo local). Portas publicadas fora do padrão (`8083`, `8084`, `8085`, `8092`)
evitam colidir com outro laboratório já rodando no mesmo host, ver
`docker-compose.yml`.

Para rodar em Kubernetes local via Helm (tier 4, ver ADR 0013; substitui o
Kustomize do tier 3, que continua em `deploy/kubernetes/` como histórico
congelado):

```bash
make helm-up   # cria o cluster kind "lunch-rush", builda as imagens e instala o chart Helm
make keda-up   # instala o KEDA e liga o ScaledObject de lunchrush-worker (escala por lag)
make kind-down # destrói o cluster
```

Para provisionar S3 e Secrets Manager contra o LocalStack (tier 4, nunca AWS
real, ver ADR 0012):

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
make load-loadgen      # LoadGen, requer o delivery-api no ar
```

Para o TLA+ real (tier 5, ver ADR 0017):

```bash
curl -sSfL https://github.com/tlaplus/tlaplus/releases/latest/download/tla2tools.jar \
  -o docs/tla/tools/tla2tools.jar
java -jar docs/tla/tools/tla2tools.jar -workers 4 \
  -config docs/tla/LunchRushFencing.cfg docs/tla/LunchRushFencing.tla
```

Para a arquitetura celular local (tier 5, ver ADR 0019):

```bash
docker compose -f docker-compose.yml -f deploy/compose/cells.yml \
  --profile app --profile cells up -d --build \
  delivery-api-cell-a delivery-api-cell-b cellrouter
```

Para o segundo "provedor" simulado e o failover de fencing entre eles (tier 6,
nunca uma cloud real, ver ADR 0021):

```bash
docker compose --profile app build
docker compose --profile app up -d
docker compose -f docker-compose.cloud-b.yml --profile app up -d
# confirma o mesmo digest:
docker inspect lunch-rush-delivery-api-1 --format '{{.Image}}'
docker inspect cloudb-delivery-api-1 --format '{{.Image}}'
```

Passo a passo completo do failover (backup, interrupção, promoção, rejeição do
writer antigo, RTO/RPO): `docs/passo-a-passo/tier-6.md`.

Passo a passo completo, com o que esperar em cada comando, para todos os tiers:
`docs/passo-a-passo/tier-1.md` até `tier-6.md`.

## Como explorar o projeto

Uma leitura curta costuma seguir esta ordem:

1. Abra este README e veja em que tier o roadmap está no [catálogo](#catálogo-dos-tiers).
2. Siga o `docs/passo-a-passo/tier-N.md` do tier escolhido se quiser rodar antes de
   ler o código.
3. Veja os testes que protegem a invariante daquele tier.
4. Leia o fluxo central dentro de `internal/`.
5. Rode o experimento (LoadGen, chaos, TLA+) e compare a saída com
   `docs/benchmarks/`.
6. Leia o ADR do tier antes de propor mudar uma decisão de arquitetura.

## Anatomia do repositório

```text
lunch-rush/
├── cmd/                 pontos de entrada executáveis (delivery-api,
│                        lunchrush-worker, tracking-*, loadgen, cellrouter,
│                        cloudfailover, migrate...)
├── internal/            domínio e infraestrutura (delivery, courier,
│                        lunchrush, tracking, notification, fencing, platform)
├── docs/                ADRs, benchmarks, passo a passo, TLA+, limites da
│                        simulação local
├── deploy/               Helm, Kubernetes, ArgoCD, Dockerfiles
├── infra/terraform/       um root por ambiente/provedor (aws-lab, cloud-a, cloud-b)
├── test/                 integração, invariantes, contrato, e2e
├── chaos/local/          scripts de chaos local reproduzíveis
└── migrations/           schema versionado do PostgreSQL
```

Não existe um segundo módulo Go nem um domínio paralelo: tudo aqui evolui contra o
mesmo `go.mod` e a mesma base de dados lógica, mesmo depois de virar distribuído e
depois celular.

## Limites do projeto

O lunch-rush prova, com execução real, correção sob concorrência e idempotência
(`-race`, constraints, disputa concorrente), entrega distribuída at-least-once com
efeito deduplicado (outbox, inbox, Kafka via Redpanda), um protocolo de fencing
verificado formalmente em TLA+ e exercitado em código, e a sobrevivência desse
mesmo protocolo e do mesmo artefato a uma troca de "provedor", com RTO/RPO medidos.

Ele não prova, e nunca alegou provar, operação real em produção, alta
disponibilidade real entre zonas ou regiões AWS, ou independência real de dois
provedores de nuvem pagos. Toda vez que uma peça do roadmap original dependia de
conta AWS real, de um segundo provedor pago, ou de escala além do que uma máquina
compartilhada aguenta, a substituição local e o que se perde com ela estão
documentados, tier a tier, em `docs/limitacoes-simulacao-local.md`. Nunca
escondidos atrás de um número que pareça medição de produção. O que ficou fora de
cada tier por escolha de escopo, não por esquecimento, está mapeado em
`docs/benchmarks/tier-N-what-breaks-next.md`.

## Fim

O lunch-rush transforma seis perguntas sobre entrega, correção
concorrente, mensageria distribuída, infraestrutura provada, protocolo formal,
isolamento celular e portabilidade, em um único sistema, testado tier por tier
contra infraestrutura real local. Escolha um tier no catálogo, siga o passo a
passo e rode o experimento. O resultado mais útil não é decorar uma ferramenta.
É enxergar onde uma decisão de arquitetura distribuída aguenta carga e falha real,
e onde a prova para de valer.
