variable "localstack_endpoint" {
  description = "Endpoint do LocalStack do stack cloud-a (docker-compose.yml, profile aws-lab)."
  type        = string
  default     = "http://localhost:4566"
}

variable "receipts_bucket_name" {
  description = "Nome do bucket S3 de comprovantes de entrega em cloud-a."
  type        = string
  default     = "dispatch-cloud-a-receipts"
}

variable "jwt_secret_name" {
  description = "Nome do segredo do JWT no Secrets Manager de cloud-a."
  type        = string
  default     = "dispatch/cloud-a/jwt-secret"
}

variable "jwt_secret_value" {
  description = "Valor do segredo do JWT usado neste ambiente de laboratório."
  type        = string
  sensitive   = true
  default     = "cloud-a-jwt-secret-nao-usar-em-producao"
}
