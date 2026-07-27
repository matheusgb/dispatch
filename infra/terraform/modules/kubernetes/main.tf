terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }
}

# Módulo kubernetes: cluster EKS via `aws_eks_cluster`. O tier 3 já usa
# Kubernetes real desde `kind` (ver docs/limitacoes-simulacao-local.md); o
# que faltava era o Terraform que provisiona `aws_eks_cluster`, que o
# LocalStack community não implementa. Contra o floci, o recurso sobe de
# verdade um container `rancher/k3s:latest` (confirmado via `docker ps`)
# com um API server Kubernetes real por trás, atingível com `kubectl`; o
# que não existe é o control plane gerenciado da AWS, node groups
# distribuídos entre AZs reais, nem IAM de nó. Ver
# docs/adr/0027-floci-para-eks-msk-rds-elasticache.md.

resource "aws_iam_role" "eks" {
  name = "${var.cluster_name}-eks-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "eks.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })

  tags = {
    Owner       = "lunchrush"
    Project     = "lunchrush"
    Environment = var.environment
  }

  # mesma limitação de tags dos módulos messaging e cache: o `GetRole` do
  # floci não devolve `tags`/`tags_all` de volta no refresh.
  lifecycle {
    ignore_changes = [tags, tags_all]
  }
}

resource "aws_eks_cluster" "this" {
  name     = var.cluster_name
  role_arn = aws_iam_role.eks.arn

  vpc_config {
    subnet_ids = var.subnet_ids
  }

  tags = {
    Owner       = "lunchrush"
    Project     = "lunchrush"
    Environment = var.environment
  }
}
