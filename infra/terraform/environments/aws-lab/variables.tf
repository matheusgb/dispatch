variable "localstack_endpoint" {
  description = "Endpoint do LocalStack (edge, todos os serviços)."
  type        = string
  default     = "http://localhost:4566"
}

variable "floci_endpoint" {
  description = "Endpoint do floci (edge, todos os serviços), porta separada do LocalStack no mesmo compose."
  type        = string
  default     = "http://localhost:4577"
}

variable "database_identifier" {
  description = "Identificador da instância RDS PostgreSQL."
  type        = string
  default     = "lunchrush-aws-lab"
}

variable "database_master_password" {
  description = "Senha master do RDS, só usada neste laboratório local."
  type        = string
  sensitive   = true
  default     = "aws-lab-rds-password-nao-usar-em-producao"
}

variable "cache_replication_group_id" {
  description = "Identificador do replication group ElastiCache."
  type        = string
  default     = "lunchrush-aws-lab"
}

variable "msk_cluster_name" {
  description = "Nome do cluster MSK."
  type        = string
  default     = "lunchrush-aws-lab"
}

variable "eks_cluster_name" {
  description = "Nome do cluster EKS."
  type        = string
  default     = "lunchrush-aws-lab"
}

variable "receipts_bucket_name" {
  description = "Nome do bucket S3 usado para comprovantes de entrega."
  type        = string
  default     = "lunchrush-delivery-receipts"
}

variable "jwt_secret_name" {
  description = "Nome do segredo do JWT no Secrets Manager."
  type        = string
  default     = "lunchrush/jwt-secret"
}

variable "jwt_secret_value" {
  description = "Valor do segredo do JWT usado neste ambiente de laboratório."
  type        = string
  sensitive   = true
  default     = "aws-lab-jwt-secret-nao-usar-em-producao"
}
