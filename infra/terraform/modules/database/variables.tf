variable "identifier" {
  description = "Identificador da instância RDS."
  type        = string
}

variable "environment" {
  description = "Nome do ambiente (aws-lab, aws-benchmark)."
  type        = string
  default     = "aws-lab"
}

variable "master_username" {
  description = "Usuário master do banco."
  type        = string
  default     = "lunchrush"
}

variable "master_password" {
  description = "Senha master do banco, só usada neste laboratório local."
  type        = string
  sensitive   = true
}
