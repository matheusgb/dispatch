# Ambiente cloud-a do tier 6: reusa exatamente os mesmos módulos do
# ambiente aws-lab (tier 4, ver docs/adr/0012-terraform-contra-localstack.md),
# só que com nomes de recurso próprios de "cloud-a" e apontado para o
# LocalStack do stack cloud-a (docker-compose.yml, porta 4566). Não é um
# módulo novo: é o padrão do tier 4 reaplicado como o roadmap pede
# ("stacks Terraform separados por provedor... reaproveitando o padrão já
# estabelecido").
#
# Igual ao aws-lab, isto só prova S3 e Secrets Manager/KMS: LocalStack
# community não implementa EKS/MSK/RDS/ElastiCache (ver
# docs/limitacoes-simulacao-local.md).

terraform {
  required_version = ">= 1.9"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }
}

provider "aws" {
  region                      = "us-east-1"
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  s3_use_path_style           = true

  endpoints {
    s3             = var.localstack_endpoint
    secretsmanager = var.localstack_endpoint
    kms            = var.localstack_endpoint
    sts            = var.localstack_endpoint
    iam            = var.localstack_endpoint
  }
}

module "storage" {
  source      = "../../modules/storage"
  bucket_name = var.receipts_bucket_name
  environment = "cloud-a"
}

module "secrets" {
  source           = "../../modules/secrets"
  secret_name      = var.jwt_secret_name
  jwt_secret_value = var.jwt_secret_value
  environment      = "cloud-a"
}
