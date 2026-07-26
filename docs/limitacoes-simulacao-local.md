# Limitações da simulação local

Este projeto não usa conta AWS real nem nenhum provedor de nuvem pago. A
partir do tier 4, o roadmap original em `dispatch.md` pede EKS, MSK, Aurora
DSQL, DynamoDB Global Tables, AWS FIS e um segundo provedor de nuvem. Nada
disso é criado de verdade aqui. Este documento registra, tier por tier, o
que foi simulado localmente com bibliotecas maduras e o que não tem
equivalente local honesto, para que nenhum resultado deste repositório seja
lido como prova de operação em nuvem real.

A regra geral: quando existe uma alternativa local madura que exercita o
mesmo comportamento observável (protocolo, API, contrato), ela é usada e
identificada como substituição, não como limitação. Quando não existe
alternativa local capaz de reproduzir o comportamento que importa (latência
real entre regiões, custo real, garantias de disponibilidade de um serviço
gerenciado), o item entra como limitação abaixo, sem tentar forjar uma prova.

## Tier 3: as duas primeiras substituições já entram aqui

O roadmap original coloca Kafka e Kubernetes no tier 3, um antes de EKS e
MSK aparecerem no diagrama do tier 4 como serviços gerenciados AWS. Este
projeto já usa as versões locais desde o tier 3, não porque simula AWS
cedo demais, mas porque o próprio roadmap pede Kafka e `kind` local
*antes* da versão gerenciada:

| Peça do roadmap | Substituição local | Nota |
| --- | --- | --- |
| Kafka (backbone de eventos) | Redpanda v24.3.1 | mesmo protocolo Kafka; `kafka-go` fala com os dois sem diferença de código |
| Kubernetes local | `kind` (cluster próprio, nome `dispatch`, para não colidir com o do edge) | idêntico ao que o tier 4 usa antes de qualquer serviço gerenciado entrar |

Quando o tier 4 trocar Redpanda por "MSK" e o `kind` local por "EKS", a
diferença registrada na tabela abaixo é sobre o que o **serviço gerenciado
da AWS** adicionaria (multi-AZ real, IAM, cobrança), não sobre Kafka ou
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
ElastiCache ficam atrás do LocalStack Pro, que é pago; nenhum módulo
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
(`kindest/node:v1.31.0`). O `helm install` avisa isso explicitamente e o
`ScaledObject` funcionou corretamente na prática (escalou `dispatch-worker`
de 0 a 3 réplicas por lag real do consumer group, ver ADR 0014), mas o
combo não é o que a documentação oficial do KEDA testa.

### Itens do tier 4 não executados por restrição de tempo e memória local

A máquina deste laboratório roda em paralelo com outro laboratório
(`edge-lab`), compartilhando CPU e memória. Nesta passada do tier 4, os
seguintes itens do escopo (marcados como "se sobrar fôlego" no roadmap
interno) não foram executados, e nenhum número relacionado a eles deve
ser lido como medido: SBOM/scan/assinatura de imagem (`syft`, `grype`,
`cosign`), teste de carga dedicado ao tier 4 (steady state + spike 3x +
soak reduzido) e o runbook de backup/recuperação distribuída. Ver
`docs/benchmarks/tier-4-what-breaks-next.md` para o detalhamento.

## Tier 5: células e multi-região

| Peça do roadmap | Substituição local | O que se perde |
| --- | --- | --- |
| Múltiplas células, cada uma com seu próprio compute e dados | múltiplos `docker compose` independentes, cada um numa porta e rede própria | isolamento lógico, não isolamento físico de hardware, rede ou provedor |
| Aurora DSQL como autoridade de fencing multi-região | PostgreSQL local com o mesmo desenho de `dispatch_fences` e `active_assignments`, sem replicação geográfica | sem consistência forte entre regiões reais; a autoridade roda em um único nó local |
| DynamoDB Global Tables como diretório | tabela PostgreSQL local fazendo o papel de diretório | sem replicação multi-região real, sem o modelo de conflito eventual documentado pela AWS |
| TLA+ e model checking | TLA+ real, executado com o TLC. Isso **não é uma simulação**: é a mesma ferramenta que seria usada contra a AWS | nenhuma perda: a especificação formal não depende de infraestrutura |
| Latência real entre regiões | não simulada com números inventados; qualquer latência citada é rotulada como Premissa, nunca como Medido | nenhum número de latência inter-região deste repositório pode ser citado como medição real |

## Tier 6: portabilidade entre provedores

| Peça do roadmap | Substituição local | O que se perde |
| --- | --- | --- |
| Segundo provedor de nuvem (`cloud-b`) | um segundo `docker compose` independente, com sua própria rede Docker, representando "outro provedor" | não existe um segundo provedor de verdade: IAM, rede, DNS, billing e os limites reais de um provedor diferente não são exercitados |
| Egress, custo de transferência entre clouds | não medido; qualquer valor citado é rotulado como Premissa | nenhum custo real de rede entre provedores é produzido aqui |
| Matriz de portabilidade | preenchida com evidência de contrato local (mesma imagem OCI, mesmo schema, mesmos testes) rodando nos dois `docker compose` | prova portabilidade de contrato e formato, não prova portabilidade de infraestrutura gerenciada real |

## O que continua sendo prova real, mesmo local

- corretude sob concorrência (`-race`, testes de disputa, constraints do banco);
- o protocolo Kafka via Redpanda é o mesmo protocolo que o MSK expõe: outbox,
  consumer groups, partições e replay são reais, não simulados;
- Kubernetes via `kind` usa o mesmo `kubectl` e os mesmos manifests que um
  EKS real aceitaria; probes, HPA, NetworkPolicy e rollout são exercitados
  de verdade, só o control plane gerenciado é que não existe;
- a especificação TLA+ e o simulador determinístico (LunchRush) não perdem
  nada por serem locais: são ferramentas de verificação, não de
  infraestrutura.

Qualquer número deste repositório que pareça uma métrica de produção AWS
deve ser lido como ambiente local, salvo indicação contrária explícita.
