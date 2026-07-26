# Ambiente aws-lab: usa apenas os módulos que fazem sentido contra
# LocalStack community (storage, secrets). EKS, MSK, RDS e ElastiCache não
# têm módulo aqui porque LocalStack community não os implementa de forma
# honesta — ver docs/limitacoes-simulacao-local.md e
# docs/adr/0012-terraform-contra-localstack.md.
#
# Estado local (não remoto): ver justificativa em
# infra/terraform/bootstrap/main.tf.

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
  environment = "aws-lab"
}

module "secrets" {
  source           = "../../modules/secrets"
  secret_name      = var.jwt_secret_name
  jwt_secret_value = var.jwt_secret_value
  environment      = "aws-lab"
}
