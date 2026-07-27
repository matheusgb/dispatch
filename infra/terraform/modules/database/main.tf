terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }
}

# Módulo database: instância RDS PostgreSQL. Contra AWS real seria Aurora
# ou RDS Multi-AZ com failover gerenciado, o que o roadmap do tier 4 pede.
# Contra o floci, o `aws_db_instance` cria de verdade um container
# `postgres:16.3-alpine` (confirmado via `docker ps`) e o motor PostgreSQL
# real aceita conexão; o que não existe é o Multi-AZ gerenciado, a réplica
# automática nem o failover que a AWS orquestraria. Ver
# docs/adr/0027-floci-para-eks-msk-rds-elasticache.md e
# docs/limitacoes-simulacao-local.md.

resource "aws_db_instance" "this" {
  identifier                 = var.identifier
  engine                     = "postgres"
  instance_class             = "db.t3.micro"
  allocated_storage          = 20
  username                   = var.master_username
  password                   = var.master_password
  skip_final_snapshot        = true
  publicly_accessible        = false
  multi_az                   = false
  backup_retention_period    = 0
  auto_minor_version_upgrade = true

  # o `DescribeDBInstances` do floci sempre devolve
  # `auto_minor_version_upgrade = false` no refresh, independente do
  # valor enviado no create ou de qualquer update aplicado depois;
  # declarar `true` acima não resolve sozinho, então o campo entra em
  # `ignore_changes` para o plan ficar idempotente. Ver
  # docs/adr/0027-floci-para-eks-msk-rds-elasticache.md.
  lifecycle {
    ignore_changes = [auto_minor_version_upgrade]
  }

  tags = {
    Owner       = "lunchrush"
    Project     = "lunchrush"
    Environment = var.environment
  }
}
