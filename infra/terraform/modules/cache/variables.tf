variable "replication_group_id" {
  description = "Identificador do replication group ElastiCache."
  type        = string
}

variable "environment" {
  description = "Nome do ambiente (aws-lab, aws-benchmark)."
  type        = string
  default     = "aws-lab"
}
