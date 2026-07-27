variable "localstack_endpoint" {
  description = "Endpoint do LocalStack do stack cloud-b (docker-compose.cloud-b.yml, profile aws-lab)."
  type        = string
  default     = "http://localhost:14566"
}

variable "receipts_bucket_name" {
  description = "Nome do bucket S3 de comprovantes de entrega em cloud-b."
  type        = string
  default     = "lunchrush-cloud-b-receipts"
}

variable "jwt_secret_name" {
  description = "Nome do segredo do JWT no Secrets Manager de cloud-b."
  type        = string
  default     = "lunchrush/cloud-b/jwt-secret"
}

variable "jwt_secret_value" {
  description = "Valor do segredo do JWT usado neste ambiente de laboratório."
  type        = string
  sensitive   = true
  default     = "cloud-b-jwt-secret-nao-usar-em-producao"
}
