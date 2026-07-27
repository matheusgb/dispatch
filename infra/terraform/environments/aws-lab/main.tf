# Ambiente aws-lab: storage e secrets seguem contra o LocalStack community
# (ver docs/adr/0012-terraform-contra-localstack.md, nada mudou aqui).
# EKS, MSK, RDS e ElastiCache, que o LocalStack community não implementa,
# passam a ter módulo contra o floci (MIT, cobre os quatro com Docker
# real) — ver docs/adr/0027-floci-para-eks-msk-rds-elasticache.md e
# docs/limitacoes-simulacao-local.md.
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

# Segundo provider, mesma conta de teste, apontando para o floci em vez do
# LocalStack. Alias porque EKS/MSK/RDS/ElastiCache continuam sem módulo
# no provider "aws" default: mudar de emulador nos serviços que já
# funcionavam no LocalStack não tinha por que acontecer.
provider "aws" {
  alias = "floci"

  region                      = "us-east-1"
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  s3_use_path_style           = true

  endpoints {
    rds         = var.floci_endpoint
    elasticache = var.floci_endpoint
    kafka       = var.floci_endpoint
    eks         = var.floci_endpoint
    iam         = var.floci_endpoint
    sts         = var.floci_endpoint
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

module "database" {
  source = "../../modules/database"
  providers = {
    aws = aws.floci
  }
  identifier      = var.database_identifier
  master_password = var.database_master_password
  environment     = "aws-lab"
}

module "cache" {
  source = "../../modules/cache"
  providers = {
    aws = aws.floci
  }
  replication_group_id = var.cache_replication_group_id
  environment          = "aws-lab"
}

module "messaging" {
  source = "../../modules/messaging"
  providers = {
    aws = aws.floci
  }
  cluster_name = var.msk_cluster_name
  environment  = "aws-lab"
}

module "kubernetes" {
  source = "../../modules/kubernetes"
  providers = {
    aws = aws.floci
  }
  cluster_name = var.eks_cluster_name
  environment  = "aws-lab"
}
