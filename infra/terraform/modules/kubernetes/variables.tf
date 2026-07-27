variable "cluster_name" {
  description = "Nome do cluster EKS."
  type        = string
}

variable "environment" {
  description = "Nome do ambiente (aws-lab, aws-benchmark)."
  type        = string
  default     = "aws-lab"
}

variable "subnet_ids" {
  description = "Subnets do cluster EKS (rótulo local, sem VPC real por trás)."
  type        = list(string)
  default     = ["subnet-default-a", "subnet-default-b"]
}
