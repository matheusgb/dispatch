variable "localstack_endpoint" {
  description = "Endpoint do LocalStack (edge, todos os serviços)."
  type        = string
  default     = "http://localhost:4566"
}

variable "receipts_bucket_name" {
  description = "Nome do bucket S3 usado para comprovantes de entrega."
  type        = string
  default     = "dispatch-delivery-receipts"
}

variable "jwt_secret_name" {
  description = "Nome do segredo do JWT no Secrets Manager."
  type        = string
  default     = "dispatch/jwt-secret"
}

variable "jwt_secret_value" {
  description = "Valor do segredo do JWT usado neste ambiente de laboratório."
  type        = string
  sensitive   = true
  default     = "aws-lab-jwt-secret-nao-usar-em-producao"
}
