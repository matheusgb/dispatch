terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }
}

# Módulo cache: ElastiCache (Valkey) via `aws_elasticache_replication_group`.
# Contra AWS real o roadmap do tier 4 pede um replication group Multi-AZ
# com automatic failover exercitado. Contra o floci, o recurso cria de
# verdade um container `valkey/valkey:8` (confirmado via `docker ps`) e o
# protocolo Redis/Valkey funciona de ponta a ponta; não existe réplica
# nem failover automático porque este módulo usa `num_cache_clusters = 1`
# de propósito (floci não orquestra um segundo nó com failover
# gerenciado). Ver docs/adr/0027-floci-para-eks-msk-rds-elasticache.md.

resource "aws_elasticache_replication_group" "this" {
  replication_group_id       = var.replication_group_id
  description                = "lunchrush cache (${var.environment})"
  engine                     = "valkey"
  node_type                  = "cache.t3.micro"
  num_cache_clusters         = 1
  automatic_failover_enabled = false
  multi_az_enabled           = false

  tags = {
    Owner       = "lunchrush"
    Project     = "lunchrush"
    Environment = var.environment
  }

  # O `DescribeReplicationGroups` do floci não devolve boa parte dos
  # atributos computados de volta no refresh (engine, port,
  # num_cache_clusters, tags, entre outros). Isso não só quebra a
  # idempotência do plan como pode disparar uma chamada de update real
  # contra uma operação que o floci não implementa: tentar reconciliar
  # `num_cache_clusters` chama `IncreaseReplicaCount`, que devolve
  # `UnsupportedOperation` (400). `ignore_changes = all` é deliberadamente
  # amplo aqui: depois do apply inicial, qualquer mudança real neste
  # recurso precisa de `terraform taint` (recriar), não de update
  # in-place. Ver docs/adr/0027-floci-para-eks-msk-rds-elasticache.md.
  lifecycle {
    ignore_changes = all
  }
}
