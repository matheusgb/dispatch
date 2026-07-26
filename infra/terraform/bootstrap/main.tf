# Bootstrap: cria o bucket S3 e a tabela DynamoDB que serviriam de backend
# remoto (estado + lock) para os ambientes em infra/terraform/environments.
# Roda contra o mesmo LocalStack usado pelos módulos de aplicação, com o
# mesmo `terraform apply` de sempre — não é um script separado.
#
# O ambiente aws-lab NÃO encadeia este backend (ver
# docs/adr/0012-terraform-contra-localstack.md): LocalStack community sobe
# com PERSISTENCE=0 neste laboratório, então o estado remoto desapareceria
# a cada `docker compose down`, trocando um problema real (perda de
# estado) por uma falsa sensação de backend durável. Este diretório existe
# para provar que o padrão de bootstrap funciona de verdade contra a API
# do LocalStack, não para ser usado como backend ativo neste laboratório.

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
    s3       = var.localstack_endpoint
    dynamodb = var.localstack_endpoint
  }
}

variable "localstack_endpoint" {
  description = "Endpoint do LocalStack (edge, todos os serviços)."
  type        = string
  default     = "http://localhost:4566"
}

resource "aws_s3_bucket" "tfstate" {
  bucket = "dispatch-tfstate-lab"

  tags = {
    Owner   = "dispatch"
    Project = "dispatch"
    Purpose = "bootstrap-state-backend-demo"
  }
}

resource "aws_s3_bucket_versioning" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_dynamodb_table" "tfstate_lock" {
  name         = "dispatch-tfstate-lock"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "LockID"

  attribute {
    name = "LockID"
    type = "S"
  }

  tags = {
    Owner   = "dispatch"
    Project = "dispatch"
    Purpose = "bootstrap-state-backend-demo"
  }
}

output "state_bucket" {
  value = aws_s3_bucket.tfstate.bucket
}

output "lock_table" {
  value = aws_dynamodb_table.tfstate_lock.name
}
