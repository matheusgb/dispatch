# Limitações da simulação local

Este projeto não usa conta AWS real nem nenhum provedor de nuvem pago. A
partir do tier 4, o roadmap original em `lunch-rush.md` pede EKS, MSK, Aurora
DSQL, DynamoDB Global Tables, AWS FIS e um segundo provedor de nuvem. Nada
disso é criado de verdade aqui. Este documento registra, tier por tier, o
que foi simulado localmente com bibliotecas maduras e o que não tem
equivalente local honesto. O objetivo é que nenhum resultado deste
repositório seja lido como prova de operação em nuvem real.

A regra geral: quando existe uma alternativa local madura que exercita o
mesmo comportamento observável (protocolo, API, contrato), ela é usada e
identificada como substituição, não como limitação. Quando não existe
alternativa local capaz de reproduzir o comportamento que importa (latência
real entre regiões, custo real, garantias de disponibilidade de um serviço
gerenciado), o item entra como limitação abaixo, sem tentar forjar uma prova.

## Tier 3: as duas primeiras substituições já entram aqui

O roadmap original coloca Kafka e Kubernetes no tier 3, antes de EKS e MSK
aparecerem no diagrama do tier 4 como serviços gerenciados AWS. Este
projeto já usa as versões locais desde o tier 3. Não é simular AWS cedo
demais: o próprio roadmap pede Kafka e `kind` local *antes* da versão
gerenciada.

| Peça do roadmap | Substituição local | Nota |
| --- | --- | --- |
| Kafka (backbone de eventos) | Redpanda v24.3.1 | mesmo protocolo Kafka; `kafka-go` fala com os dois sem diferença de código |
| Kubernetes local | `kind` (cluster próprio, nome `lunchrush`, para não colidir com o do edge) | idêntico ao que o tier 4 usa antes de qualquer serviço gerenciado entrar |

Quando o tier 4 trocar Redpanda por "MSK" e o `kind` local por "EKS", a
diferença registrada na tabela abaixo é sobre o que o **serviço gerenciado
da AWS** adicionaria (multi-AZ real, IAM, cobrança). Não é sobre Kafka ou
Kubernetes em si, que já são as ferramentas reais desde aqui.

## Tier 4: plataforma "AWS" localmente simulada

| Peça do roadmap | Substituição local | O que se perde |
| --- | --- | --- |
| EKS | `kind` (Kubernetes-in-Docker) | não existe control plane gerenciado, nem IAM de nó, nem cobrança, nem os limites reais de escala do EKS |
| MSK | Redpanda (compatível com o protocolo Kafka) | sem réplicas cross-AZ reais, sem o modelo de custo do MSK, sem os limites operacionais documentados da AWS |
| RDS/Aurora PostgreSQL | PostgreSQL em container único ou `docker compose` com réplica de leitura manual | sem Multi-AZ real, sem failover gerenciado, sem métricas de um serviço gerenciado |
| ElastiCache for Redis | Redis em container | idêntico em protocolo; perde-se apenas o failover gerenciado |
| S3 | LocalStack (quando aplicável) ou sistema de arquivos local | sem durabilidade de objeto real, sem custo de egress real |
| AWS FIS | Toxiproxy e Chaos Mesh dentro do `kind` | cobre falha de rede e de pod; não cobre falha de zona de disponibilidade real nem o modelo de permissão do FIS |
| Terraform contra AWS | Terraform contra LocalStack, quando o provider suporta | LocalStack open source não implementa EKS nem MSK; os módulos que dependem desses serviços ficam documentados, não aplicados |
| Multi-AZ real, RPO/RTO medidos em zona de verdade | três instâncias locais rotuladas como "zona", sem isolamento de rede físico real | qualquer RTO/RPO medido aqui é do ambiente local, não de uma zona AWS |

### Terraform contra LocalStack: só S3 e Secrets Manager/KMS

`infra/terraform/` (ver ADR 0012) só tem módulo para os serviços que o
LocalStack **community** (edição gratuita) implementa de forma honesta:
S3 e Secrets Manager (com KMS por baixo). EKS, MSK, RDS/Aurora e
ElastiCache ficam atrás do LocalStack Pro, que é pago. Nenhum módulo
Terraform foi escrito para eles neste laboratório, para não fingir um
recurso que nunca aceitaria uma conexão de verdade.

Limitação adicional encontrada na prática: `aws_s3_bucket_lifecycle_configuration`
trava `terraform apply` indefinidamente contra o LocalStack 3.8.1 (o
provider nunca considera a configuração propagada, mesmo com o LocalStack
respondendo 200 a cada tentativa). O recurso foi removido do módulo
`storage`; detalhes em ADR 0012.

### KEDA numa versão de Kubernetes não suportada oficialmente

O chart `kedacore/keda` instalado (KEDA 2.20) declara suporte formal a
partir do Kubernetes 1.33; o `kind` deste laboratório roda 1.31
(`kindest/node:v1.31.0`). O `helm install` avisa isso explicitamente. Na
prática, o `ScaledObject` funcionou corretamente: escalou `lunchrush-worker`
de 0 a 3 réplicas por lag real do consumer group (ver ADR 0014). Mas o
combo não é o que a documentação oficial do KEDA testa.

### Itens do tier 4 fechados numa segunda passada (antes do tier 5)

Uma primeira passada do tier 4 não executou três itens por restrição de
tempo e memória compartilhada com o `edge-lab`. Antes de iniciar o tier 5,
o `edge-lab` foi parado (containers Docker, não o código) para liberar
memória, e os três itens foram fechados com evidência real:

- **SBOM/scan/assinatura de imagem**: `syft` (SBOM SPDX), `grype` (scan de
  vulnerabilidades, nenhuma encontrada nesta execução) e `cosign` (chave
  local, não keyless) contra as 5 imagens do `docker compose`. Ver ADR
  0016 e `docs/benchmarks/supply-chain/`.
- **Teste de carga dedicado**: `k6` com steady state (10 VUs) e spike de
  3x (30 VUs) contra o `delivery-api` real, 0% de erro em ~144 mil
  requisições, p95 de 7,33ms. Ver `docs/benchmarks/tier-4-load/README.md`.
- **Runbook de backup/recuperação distribuída**: `pg_dump`/`pg_restore`
  reais, comparação do gap de dados contra o high-watermark do Kafka, e
  RPO medido (39s nesta execução, refletindo o intervalo real entre
  backup e "crash" simulado, não um número teórico). Ver
  `docs/runbooks/backup-e-recuperacao-distribuida.md`.

O soak reduzido (contínuo por várias horas) continua não executado nesta
passada. É decisão de escopo, não esquecimento: o tempo desta sessão foi
priorizado para o tier 5 (TLA+, fencing, células, simulador), que o
usuário pediu explicitamente para não pular. Ver
`docs/benchmarks/tier-4-what-breaks-next.md` para o registro histórico
completo (mantido como estava, com uma nota de que os três itens acima
deixaram de ser pendência).

## Tier 5: células e multi-região

| Peça do roadmap | Substituição local | O que se perde |
| --- | --- | --- |
| Múltiplas células, cada uma com seu próprio compute e dados | duas células lógicas no mesmo `docker compose` (`deploy/compose/cells.yml`): `delivery-api` próprio por célula, banco PostgreSQL lógico próprio (`lunchrush_cell_a`/`b`), mas o **mesmo processo PostgreSQL físico** e a mesma rede Docker | isolamento lógico de dados (provado: uma entrega de uma célula nunca existe na outra), não isolamento físico de hardware, rede, processo de banco ou provedor. O teste de noisy neighbor (`docs/benchmarks/tier-5-cells/`) mediu o vazamento real (p95 de uma célula sobe ~1,7x quando a outra é saturada), não escondeu |
| Aurora DSQL como autoridade de fencing multi-região | PostgreSQL local single-node com o mesmo desenho de `lunchrush_fences` e `active_assignments` (`internal/fencing`), sem replicação geográfica (ADR 0018) | sem consistência forte entre regiões reais; sem medir latência real de OCC entre regiões; a autoridade roda em um único nó local, único ponto de falha que Aurora DSQL não teria |
| DynamoDB Global Tables como diretório de roteamento | mapa estático `cell_id -> URL` carregado de variável de ambiente no `cmd/cellrouter` (ADR 0019) | sem replicação multi-região real, sem o modelo de conflito eventual documentado pela AWS, sem descoberta dinâmica (o diretório é fixo no start, não se atualiza sozinho) |
| MSK Replicator (Kafka cross-region) | não implementado nesta sessão: cada célula usa o mesmo Redpanda compartilhado do `docker compose`, sem replicação assíncrona entre células | nenhuma prova de replicação de log entre células/regiões; `event_id` global e deduplicação cross-region citados no roadmap não foram exercitados |
| TLA+ e model checking | TLA+ real, executado com o TLC (2.19), mutation test real com contraexemplo capturado. Isso **não é uma simulação**: é a mesma ferramenta que seria usada contra a AWS | nenhuma perda: a especificação formal não depende de infraestrutura. Espaço de estados pequeno de propósito (2 writers, 1 shard, 1086 estados), não modela contenção entre múltiplos shards nem N > 2 células |
| Latência real entre regiões | não simulada com números inventados; qualquer latência citada é rotulada como Premissa, nunca como Medido | nenhum número de latência inter-região deste repositório pode ser citado como medição real |
| Simulador determinístico com "milhões de operações" | LoadGen estendido com rede/relógio virtuais, reprodutibilidade provada (dois runs idênticos, mesma seed, relatórios byte a byte iguais), soak reduzido documentado em `docs/benchmarks/tier-5-baseline.md` com o volume real alcançado | ordens de grandeza abaixo de "mais de 100 milhões de eventos em 24 horas" (meta original do roadmap para AWS real); a redução e o número real estão declarados, nunca extrapolados |
| Soak de 24h / 100M eventos | soak reduzido nesta máquina compartilhada, duração e volume documentados como medidos, não como a meta original | RPO/RTO/throughput sob 24h contínuas de carga real nunca foram observados; qualquer extrapolação linear seria uma alegação não testada |

## Tier 6: portabilidade entre provedores

| Peça do roadmap | Substituição local | O que se perde |
| --- | --- | --- |
| Segundo provedor de nuvem (`cloud-b`) | um segundo `docker compose` inteiramente independente (`docker-compose.cloud-b.yml`), rede Docker própria (projeto `cloudb`), Postgres/Redis/Redpanda/LocalStack próprios, faixa de portas nova, rodando a mesma imagem OCI de `cloud-a` sem rebuild (confirmado por `docker inspect`, mesmo digest nos dois) | não existe um segundo provedor de verdade: IAM, rede, DNS, billing e os limites reais de um provedor diferente não são exercitados; ambos os stacks competem pelos mesmos recursos físicos desta única máquina, então nenhuma comparação de desempenho entre "cloud-a" e "cloud-b" tem significado além de ruído de contenção local |
| Egress, custo de transferência entre clouds | não medido; qualquer valor citado é rotulado como Premissa | nenhum custo real de rede entre provedores é produzido aqui |
| Matriz de portabilidade | preenchida com evidência de contrato real nos dois stacks: mesmo digest, `k6 run loadtest/k6/smoke.js` com 0% de erro nos dois, `go test -tags=integration -race` completo passando nos dois bancos, `pg_dump`/`pg_restore` real entre os dois Postgres, dois roots Terraform aplicados e destruídos contra dois LocalStack independentes. Ver `docs/tier-6-matriz-portabilidade.md` | prova portabilidade de contrato e formato, não prova portabilidade de infraestrutura gerenciada real; a linha de Kafka da matriz continua sem prova de replicação cross-stack (cada Redpanda foi populado de forma independente, não por cópia real de um para o outro) |
| Autoridade de fencing multi-região (Aurora DSQL) promovendo entre provedores | o mesmo `internal/fencing` do tier 5, sem alteração de protocolo, promovendo entre dois Postgres **fisicamente separados** (um por stack), via `cmd/cloudfailover` e um `pg_dump`/`pg_restore` real orquestrando a transferência de dados entre eles | RTO (~11,5s nesta execução) e RPO (5 assignments perdidos numa janela de 0,58s) medidos, não estimados, mas dominados por overhead de laboratório (banco pequeno, processo manual), não generalizáveis para um banco de produção real nem para latência real entre provedores geograficamente distantes; a promoção depende de um backup ter sido tirado a tempo, não de replicação contínua. Ver ADR 0023 |
| Dependência compartilhada oculta entre "clouds" | testada de verdade: remover a imagem `lunchrush-delivery-api` do daemon Docker (depois de parar todos os containers que a referenciavam nos dois stacks) faz `cloud-b` falhar ao recriar seu container com `pull access denied` | a dependência real revelada não é rede, banco ou Kafka (já duplicados e isolados por stack), é o processo de build/registry de imagem, único no laboratório. Não testado: uma configuração com registry por provedor, que eliminaria esse acoplamento |
| Terraform separado por provedor | dois roots (`infra/terraform/environments/cloud-a`, `.../cloud-b`), cada um com `.tfstate` próprio, aplicados contra dois LocalStack independentes (portas 4566 e 14566) e destruídos com sucesso | mesma limitação de LocalStack community do tier 4 (ADR 0012): só S3 e Secrets Manager/KMS, nenhum módulo para EKS/MSK/RDS/ElastiCache |

## O que continua sendo prova real, mesmo local

- corretude sob concorrência (`-race`, testes de disputa, constraints do banco);
- o protocolo Kafka via Redpanda é o mesmo protocolo que o MSK expõe: outbox,
  consumer groups, partições e replay são reais, não simulados;
- Kubernetes via `kind` usa o mesmo `kubectl` e os mesmos manifests que um
  EKS real aceitaria; probes, HPA, NetworkPolicy e rollout são exercitados
  de verdade, só o control plane gerenciado é que não existe;
- a especificação TLA+ e o simulador determinístico (LoadGen) não perdem
  nada por serem locais: são ferramentas de verificação, não de
  infraestrutura;
- o protocolo de fencing promovendo entre dois bancos PostgreSQL
  fisicamente separados (tier 6) é uma prova mais forte que a do tier 5
  (dois bancos reais, não duas células no mesmo processo) sobre a mesma
  propriedade: um writer com epoch antigo nunca escreve depois da
  promoção. O que continua sem prova é a rede e a latência reais entre
  dois provedores geograficamente distantes.

Qualquer número deste repositório que pareça uma métrica de produção AWS
deve ser lido como ambiente local, salvo indicação contrária explícita.
