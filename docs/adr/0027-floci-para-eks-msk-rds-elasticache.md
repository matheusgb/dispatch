# ADR 0027: floci para os módulos Terraform de EKS, MSK, RDS e ElastiCache

## Contexto

ADR 0012 já decidiu rodar Terraform de verdade contra o LocalStack no
`aws-lab`, mas deixou de fora EKS, MSK, RDS/Aurora e ElastiCache, porque o
LocalStack community não implementa esses quatro serviços de forma honesta
(ficam atrás do LocalStack Pro pago). Essa lacuna está registrada desde
então em `docs/limitacoes-simulacao-local.md`, tier 4.

O floci (`github.com/floci-io/floci`, MIT, sem conta paga nem feature
gate) implementa os quatro contra Docker real: RDS sobe um container
`postgres`/`mysql`/`mariadb`, ElastiCache sobe `valkey`, MSK sobe
`redpandadata/redpanda` atrás da API MSK, e EKS sobe um `rancher/k3s` com
API server Kubernetes real. A pergunta que este ADR responde é a mesma do
0012, restrita a esses quatro serviços: vale a pena escrever e aplicar
Terraform de verdade contra eles, ou a lacuna deveria continuar só
documentada?

## Decisão

Escrever quatro módulos novos (`infra/terraform/modules/database`,
`cache`, `messaging`, `kubernetes`) e aplicá-los no `aws-lab` contra o
floci, mantendo os módulos `storage` e `secrets` exatamente como estavam,
contra o LocalStack. Não houve motivo para trocar o que já funcionava.

O ambiente `aws-lab` ganhou um segundo provider `aws` com alias `floci`,
apontando `rds`, `elasticache`, `kafka`, `eks`, `iam` e `sts` para
`http://localhost:4577` (porta separada da 4566 do LocalStack, no mesmo
`docker-compose.yml`, serviço `floci`, profile `aws-lab`). Os módulos
`storage` e `secrets` continuam usando o provider `aws` default, sem
alias, contra o LocalStack.

Escopo deliberadamente pequeno em cada módulo, para não fingir prova de
Multi-AZ que o floci não pode dar:

- `database`: um `aws_db_instance` PostgreSQL único, `multi_az = false`
  explícito;
- `cache`: um `aws_elasticache_replication_group` com
  `num_cache_clusters = 1` e `automatic_failover_enabled = false`
  explícitos;
- `messaging`: um `aws_msk_cluster` com `number_of_broker_nodes = 1`
  explícito;
- `kubernetes`: um `aws_eks_cluster` com a `aws_iam_role` mínima que o
  recurso exige.

## O que se prova de verdade

Depois de `terraform apply` limpo contra o floci (evidência abaixo), os
quatro recursos existem como containers Docker reais, não como resposta
fake de API:

```text
$ docker ps --format '{{.Names}}\t{{.Image}}'
floci-rds-25142f                postgres:16-alpine
floci-msk-29fee8                redpandadata/redpanda:latest
floci-eks-lunchrush-aws-lab     rancher/k3s:latest
floci-valkey-lunchrush-aws-lab  valkey/valkey:8
```

E cada um responde ao protocolo real, não só à API de controle:

```text
$ PGPASSWORD=*** psql -h 172.18.0.2 -p 7001 -U lunchrush -d postgres -c "select version();"
 PostgreSQL 16.14 on x86_64-pc-linux-musl, ...

$ docker exec floci-valkey-lunchrush-aws-lab redis-cli ping
PONG

$ docker exec floci-msk-29fee8 rpk topic create lunchrush-probe-topic
TOPIC                  STATUS
lunchrush-probe-topic  OK

$ docker exec floci-eks-lunchrush-aws-lab kubectl get nodes
NAME           STATUS   ROLES           AGE   VERSION
07cb0711146f   Ready    control-plane   11m   v1.34.1+k3s1
```

Isso fecha, para estes quatro serviços, a mesma lacuna que o ADR 0012
fechou para S3 e Secrets Manager: Terraform real, aplicado contra uma API
real, criando um recurso que aceita conexão real. Antes deste ADR, nenhum
módulo Terraform existia para EKS/MSK/RDS/ElastiCache neste laboratório.

## O que continua sem prova (sem mudança em relação à limitação já registrada)

Nada disso passa a provar o que o tier 4 pede de verdade sobre esses
serviços:

- **Multi-AZ real com failover gerenciado** (RDS/Aurora,
  ElastiCache): cada módulo sobe um único container, sem réplica nem
  orquestração de failover. `multi_az`/`automatic_failover_enabled` ficam
  `false` no código, não escondidos atrás de um valor que nunca seria
  testado;
- **distribuição real de brokers/nós entre zonas físicas** (MSK, EKS):
  `number_of_broker_nodes = 1` e um único node do k3s; nenhuma AZ real
  existe fora da AWS;
- **control plane gerenciado do EKS**: o floci sobe k3s, não o control
  plane da AWS; IAM de nó, VPC CNI e os limites reais de escala do EKS não
  são exercitados;
- **custo real**: nenhum valor de cobrança é produzido.

Essas lacunas já estavam documentadas em
`docs/limitacoes-simulacao-local.md` antes deste ADR e continuam lá,
atualizadas para refletir que agora existe módulo Terraform, não texto.

## Limitações encontradas na prática, novas nesta sessão

Assim como o ADR 0012 encontrou o waiter quebrado de
`aws_s3_bucket_lifecycle_configuration` contra o LocalStack, aplicar
Terraform de verdade contra o floci revelou três limitações do provider
`hashicorp/aws` contra a API do floci, não hipotéticas:

1. **MSK: `DescribeCluster` não devolve `broker_node_group_info`,
   `enhanced_monitoring` nem `tags` de volta no refresh.** Sem tratamento,
   isso faz o `terraform plan` pedir destroy/recreate do cluster a cada
   execução (porque o provider vê o bloco como vazio e ele é
   `ForceNew`), e tentar reconciliar `enhanced_monitoring` chama
   `UpdateMonitoring`, que devolve `404` com corpo HTML, não JSON — o
   endpoint simplesmente não existe no floci. Contornado com
   `lifecycle { ignore_changes = all }` no módulo `messaging`: depois do
   apply inicial, qualquer mudança real neste recurso precisa de
   `terraform taint`, não de update in-place.
2. **ElastiCache: `DescribeReplicationGroups` não devolve `engine`,
   `port`, `num_cache_clusters` nem `tags` de volta.** Mesmo sintoma:
   sem tratamento, o plan pede recreate (`engine` é `ForceNew`) e, se o
   provider tentar reconciliar `num_cache_clusters`, a chamada
   `IncreaseReplicaCount` devolve `UnsupportedOperation` (400). Mesmo
   contorno: `ignore_changes = all` no módulo `cache`.
3. **RDS: `DescribeDBInstances` sempre devolve
   `auto_minor_version_upgrade = false` no refresh**, independente do
   valor enviado no create. Sem tratamento, o plan pede um update
   in-place a cada execução (esse felizmente não quebra, só não é
   idempotente). Contornado com `ignore_changes = [auto_minor_version_upgrade]`
   no módulo `database`.
4. **`floci-ecr-registry` fica órfão depois do `terraform destroy`.** O
   floci sobe esse container de suporte (registry OCI genérico) de forma
   incondicional, mesmo sem nenhum `aws_ecr_repository` neste
   laboratório; como não pertence a nenhum recurso Terraform específico,
   o `destroy` não o remove. Confirmado com inventário pós-destroy
   (prática já seguida no ADR 0012): `docker ps -a` mostrou o container
   ainda de pé depois do `Destroy complete! Resources: 13 destroyed.`;
   removido manualmente com `docker rm -f floci-ecr-registry`. Quem
   repetir este laboratório deve checar `docker ps -a` depois do
   `destroy`, não confiar que ele cobre tudo que o floci sobe.

Nenhuma dessas três é um bug deste laboratório: é o provider oficial da
HashiCorp conversando com uma API que ainda não ecoa de volta tudo o que
aceitou no create. `ignore_changes` é a ferramenta certa para não deixar
isso destruir um recurso saudável, mas tem um custo real: depois do apply
inicial, uma mudança de configuração intencional nesses quatro módulos
não se propaga sozinha, precisa de `terraform taint` explícito. Isso é
diferente de RDS/MSK/ElastiCache reais, onde um `terraform apply` normal
reconcilia a mudança.

## Alternativas consideradas

- **Continuar só documentando a lacuna, sem escrever módulo:** era a
  postura correta enquanto a única opção community era o LocalStack, que
  de fato não implementa esses serviços. Deixa de ser a postura correta
  quando existe uma alternativa MIT que implementa os quatro com Docker
  real; manter a lacuna vazia depois de saber disso seria omissão, não
  cautela.
- **Trocar o LocalStack inteiro pelo floci, incluindo S3 e Secrets
  Manager:** rejeitada. S3 e Secrets Manager já funcionam no LocalStack
  community, com evidência publicada no ADR 0012; trocar um emulador que
  funciona por outro não ensina nada novo neste laboratório e arrisca
  reintroduzir bugs já resolvidos (como o da lifecycle configuration).
- **Um único provider `aws`, sem alias, apontando tudo para o floci:**
  rejeitada pelo mesmo motivo. Manter dois providers deixa explícito, no
  próprio `main.tf`, quem prova o quê.

## Consequências

- todo recurso citado como criado pelos módulos `database`, `cache`,
  `messaging` e `kubernetes` foi de fato criado contra uma API real
  (a do floci), com container Docker real por trás, verificado por
  `docker ps` e por uma chamada real ao protocolo de cada serviço;
- a limitação de Multi-AZ, distribuição real entre zonas e custo real
  continua exatamente onde estava, registrada e não escondida;
- três limitações de fidelidade do refresh do floci (MSK, ElastiCache,
  RDS) ficam documentadas aqui e nos comentários dos módulos, junto com o
  contorno aplicado e o custo desse contorno (`ignore_changes` exige
  `taint` manual para mudanças reais);
- `docs/limitacoes-simulacao-local.md` foi atualizado para refletir que
  EKS, MSK, RDS e ElastiCache passam a ter módulo Terraform aplicado, não
  mais "nenhum módulo foi escrito".

## Status

Aceita.
