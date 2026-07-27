terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }
}

# Módulo messaging: cluster MSK via `aws_msk_cluster`. O roadmap do tier 4
# já usa Kafka de verdade desde o tier 3 (Redpanda, ver
# docs/limitacoes-simulacao-local.md); o que faltava era o Terraform que
# provisiona o recurso `aws_msk_cluster` em si, que o LocalStack community
# não implementa. Contra o floci, o recurso cria de verdade um container
# `redpandadata/redpanda:latest` (confirmado via `docker ps`) atrás da API
# MSK; o que não existe é a distribuição real de brokers entre AZs nem o
# replication factor/in-sync-replicas que a AWS gerenciaria entre zonas
# físicas. `number_of_broker_nodes = 1` é deliberado: um único broker
# local não prova nada sobre distribuição multi-AZ, então este módulo não
# finge que prova. Ver docs/adr/0027-floci-para-eks-msk-rds-elasticache.md.

resource "aws_msk_cluster" "this" {
  cluster_name           = var.cluster_name
  kafka_version          = "3.5.1"
  number_of_broker_nodes = 1
  # o floci sempre devolve "DEFAULT" no refresh e não implementa o
  # endpoint `UpdateMonitoring` da API MSK (bate 404 com corpo HTML, não
  # JSON, se o Terraform tentar mudar este campo); declarar o mesmo valor
  # aqui evita a chamada de update contra um endpoint que não existe.
  enhanced_monitoring = "DEFAULT"

  broker_node_group_info {
    instance_type   = "kafka.t3.small"
    client_subnets  = var.client_subnet_ids
    security_groups = ["sg-default"]
    storage_info {
      ebs_storage_info {
        volume_size = 20
      }
    }
  }

  tags = {
    Owner       = "lunchrush"
    Project     = "lunchrush"
    Environment = var.environment
  }

  # O `DescribeCluster` do floci não devolve `broker_node_group_info`,
  # `enhanced_monitoring` nem `tags` de volta no refresh. Sem
  # `ignore_changes`, isso não só força um destroy/recreate a cada plan
  # como pode disparar uma chamada de update real contra uma operação que
  # o floci não implementa: reconciliar `enhanced_monitoring` chama
  # `UpdateMonitoring`, que devolve 404 com corpo HTML (endpoint
  # inexistente, não um erro JSON da API). `ignore_changes = all` é
  # deliberadamente amplo: depois do apply inicial, qualquer mudança real
  # neste recurso precisa de `terraform taint` (recriar), não de update
  # in-place. Ver docs/adr/0027-floci-para-eks-msk-rds-elasticache.md.
  lifecycle {
    ignore_changes = all
  }
}
