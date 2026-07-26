# Ambiente cloud-b do tier 6: mesma composição de módulos que cloud-a,
# apontado para o LocalStack independente do segundo stack
# (docker-compose.cloud-b.yml, porta 14566, rede Docker própria). Dois
# roots Terraform separados, cada um com seu próprio estado local
# (terraform.tfstate neste diretório, não compartilhado com cloud-a) —
# "cada provedor terá bootstrap, estado Terraform, módulos e ambiente
# efêmero próprios" (dispatch.md, tier 6).

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
  environment = "cloud-b"
}

module "secrets" {
  source           = "../../modules/secrets"
  secret_name      = var.jwt_secret_name
  jwt_secret_value = var.jwt_secret_value
  environment      = "cloud-b"
}
