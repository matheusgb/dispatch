variable "bucket_name" {
  description = "Nome do bucket S3 usado para comprovantes de entrega."
  type        = string
}

variable "environment" {
  description = "Nome do ambiente (aws-lab, aws-benchmark)."
  type        = string
  default     = "aws-lab"
}
