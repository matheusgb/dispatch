variable "secret_name" {
  description = "Nome do segredo no Secrets Manager."
  type        = string
  default     = "lunchrush/jwt-secret"
}

variable "jwt_secret_value" {
  description = "Valor do segredo do JWT. Em aws-lab é um valor de laboratório, não um segredo de produção."
  type        = string
  sensitive   = true
}

variable "environment" {
  description = "Nome do ambiente (aws-lab, aws-benchmark)."
  type        = string
  default     = "aws-lab"
}
