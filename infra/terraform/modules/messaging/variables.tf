variable "cluster_name" {
  description = "Nome do cluster MSK."
  type        = string
}

variable "environment" {
  description = "Nome do ambiente (aws-lab, aws-benchmark)."
  type        = string
  default     = "aws-lab"
}

variable "client_subnet_ids" {
  description = "Subnets do cluster MSK (rótulo local, sem VPC real por trás)."
  type        = list(string)
  default     = ["subnet-default-a"]
}
