# Matriz de portabilidade (tier 6)

Preenchida com evidência real, contra `cloud-a` (`docker-compose.yml`) e
`cloud-b` (`docker-compose.cloud-b.yml`), dois stacks `docker compose`
independentes, rede Docker própria cada um, faixa de portas diferente
(ver `docker-compose.cloud-b.yml` para a tabela de portas). Nenhum dos
dois é uma cloud real; a limitação central está registrada em
`docs/limitacoes-simulacao-local.md`, seção Tier 6, e em
`docs/adr/0021-objetivo-e-limites-da-estrategia-multi-cloud.md`.

Regra seguida: uma linha só é marcada como "portável, testado" se existe
um comando real executado nos dois stacks com o mesmo resultado. Onde não
há teste real, a linha diz isso explicitamente.

| Camada | Parte portável (testada) | Evidência real | Parte específica do provedor (mesmo local) |
| --- | --- | --- | --- |
| **artefato** | mesma imagem OCI, mesmo digest, sem rebuild em `cloud-b` | `docker inspect --format '{{.Image}}'` idêntico nos dois containers (`sha256:e3c37d...` na execução registrada em `docs/adr/0021`); `docker-compose.cloud-b.yml` não tem nenhuma diretiva `build:` para os serviços de app | nome/tag da imagem no daemon Docker local fazem o papel de "registry": ver `docs/benchmarks/tier-6-portability/shared-dependency-transcript.txt` para a dependência oculta que isso cria |
| **execução** | mesmo `Dockerfile`, mesmas env vars lógicas (`DATABASE_URL`, `KAFKA_BROKERS`, `HTTP_ADDR`, `LUNCHRUSH_JWT_SECRET`), mesmo `ENTRYPOINT`, mesmo usuário não-root (`distroless nonroot`) | os dois stacks sobem os 5 serviços de app com as mesmas variáveis lógicas, só o host/porta muda | não há Deployment/Service Kubernetes neste tier (o Helm chart já existe do tier 4, mas não foi duplicado para `cloud-b` nesta sessão, ver "o que não entra" abaixo); `docker compose` faz o papel de orquestrador local nos dois lados |
| **API e eventos** | mesmos endpoints HTTP, mesmo schema de request/response, mesmos tópicos Kafka e chaves de partição | `k6 run loadtest/k6/smoke.js` idêntico contra `BASE_URL=:8083` (cloud-a) e `BASE_URL=:18083` (cloud-b): 0% erro nos dois, mesma jornada completa (`docs/benchmarks/tier-6-portability/k6-smoke-cloud-a.json`, `k6-smoke-cloud-b.json`) | endpoint/porta de entrada (host:porta) é específico de cada stack local; num provedor real seria o load balancer/gateway |
| **configuração** | nomes lógicos de variável de ambiente idênticos nos dois `docker-compose*.yml` | comparação direta dos blocos `environment:` dos dois arquivos | valor efetivo (`LUNCHRUSH_JWT_SECRET=compose-dev-secret` nos dois, propositalmente igual neste laboratório) viria de um secret store por provedor numa configuração real, não de texto puro no compose |
| **PostgreSQL** | schema idêntico (mesmas migrations, `migrations/0001` a `0006`), export lógico e restauração funcionam entre os dois bancos | `pg_dump` de `cloud-a` restaurado com sucesso em `cloud-b` via `pg_restore`, sem erro de constraint, contagem de `active_assignments` batendo exatamente (10 restaurados = 10 confirmados antes do dump), registrado em `docs/benchmarks/tier-6-portability/failover-transcript.txt`; `go test -tags=integration -race` (mesmo binário de teste) passou integralmente contra os dois bancos (`localhost:5432` e `localhost:15432`) | cada banco é um container Postgres físico separado, sem replicação entre eles; failover, backup e ponto de restauração são coordenados manualmente (ADR 0023), não por um serviço gerenciado |
| **Kafka** | mesmos tópicos, mesmas chaves de partição, mesmo protocolo (Redpanda fala o protocolo Kafka) | `redpanda-topics` cria os mesmos 4 tópicos nos dois stacks (`rpk topic create` idêntico); os testes de integração que dependem de Kafka (outbox, tracking) passaram nos dois (`KAFKA_BROKERS=localhost:19092` e `localhost:29093`) | **lacuna real, não testada**: não existe replicação entre o Redpanda de `cloud-a` e o de `cloud-b`; cada um foi populado de forma independente pela mesma carga, não por cópia de um para o outro. Registrado em `docs/benchmarks/tier-6-what-breaks-next.md` |
| **object storage** | mesmo módulo Terraform (`modules/storage`), mesmo schema de bucket (versionamento, SSE-S3, bloqueio de acesso público) | `terraform apply` idêntico nos dois ambientes (`environments/cloud-a`, `environments/cloud-b`), 8 recursos criados nos dois, confirmado por `curl` direto nos dois LocalStack (`docs/benchmarks/tier-6-portability/terraform-evidence.md`) | dois LocalStack **independentes** (containers, portas e volumes diferentes: 4566 e 14566), representando duas contas separadas; nenhum objeto foi replicado entre eles nesta sessão |
| **observabilidade** | OpenTelemetry/Prometheus já usados desde o tier 2, mesmos nomes de métrica | não testado nesta sessão para `cloud-b` (o perfil `observability` do compose não foi subido no segundo stack, para não duplicar Prometheus/Grafana só para este experimento) | backend de métricas (Prometheus/Grafana) não foi duplicado por provedor nesta sessão; candidato de sessão futura |
| **infraestrutura** | mesmos módulos Terraform (`modules/storage`, `modules/secrets`), roots separados por provedor | `infra/terraform/environments/cloud-a/` e `.../cloud-b/`, cada um com seu próprio `.tfstate` local, aplicados e destruídos com sucesso e independentemente (`docs/benchmarks/tier-6-portability/terraform-evidence.md`) | cada root Terraform aponta para o `localstack_endpoint` do seu próprio stack; contra AWS real seriam duas contas com Terraform Cloud/backend remoto por conta, não testado aqui |
| **operação** | mesmo protocolo de fencing (`internal/fencing`), mesma ferramenta de operador (`cmd/cloudfailover`) promove a autoridade em qualquer um dos dois bancos | failover real executado ponta a ponta: promoção em `cloud-a`, escrita, backup, interrupção real dos processos, restauração em `cloud-b`, promoção em `cloud-b`, rejeição do writer antigo (10/10), aceitação do writer novo (5/5). Ver `docs/benchmarks/tier-6-portability/failover-transcript.txt` | RTO e RPO medidos são deste ambiente local (RTO ~11,5s dominado por `pg_dump`/`pg_restore` de um banco pequeno; RPO de 5 assignments na janela medida), não generalizáveis para um provedor real distante |

## O que não entra (herdado do roadmap, ver `lunch-rush.md` tier 6)

- réplica Kafka real entre `cloud-a` e `cloud-b` (lacuna registrada acima);
- Helm/Kubernetes duplicado por provedor (o Helm chart do tier 4 não foi
  reaplicado a um segundo cluster nesta sessão; ver
  `docs/benchmarks/tier-6-what-breaks-next.md`);
- observabilidade duplicada por provedor;
- qualquer serviço gerenciado além de S3 e Secrets Manager/KMS (mesma
  limitação do LocalStack community desde o tier 4);
- terceiro provedor, active-active para todas as escritas, RPO zero,
  equivalência perfeita entre serviços gerenciados: proibidos
  explicitamente pelo roadmap, nunca tentados aqui.
