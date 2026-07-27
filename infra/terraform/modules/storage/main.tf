# Módulo storage: bucket S3 para comprovantes de entrega, arquivos de
# replay e histórico frio (roadmap do tier 4). Contra AWS real seria S3
# com bucket policy, lifecycle e talvez replicação entre regiões. Contra
# LocalStack community, versionamento e SSE-S3 funcionam de verdade; o
# resto (replicação cross-region, Object Lock) não existe na edição
# community e não é fingido aqui.

resource "aws_s3_bucket" "receipts" {
  bucket = var.bucket_name

  tags = {
    Owner       = "lunchrush"
    Project     = "lunchrush"
    Environment = var.environment
  }
}

resource "aws_s3_bucket_versioning" "receipts" {
  bucket = aws_s3_bucket.receipts.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "receipts" {
  bucket = aws_s3_bucket.receipts.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# aws_s3_bucket_lifecycle_configuration foi removido deste módulo: contra o
# LocalStack 3.8.1 usado neste laboratório, o provider AWS aceita o PUT
# (LocalStack responde 200 em GetBucketLifecycleConfiguration), mas o
# waiter de propagação do provider nunca considera a configuração
# convergida e o `terraform apply` fica preso indefinidamente nesse
# recurso (confirmado batendo no log do LocalStack: GetBucketLifecycleConfiguration
# retornando 200 a cada 5s sem o apply nunca terminar). Documentado em
# docs/limitacoes-simulacao-local.md e docs/adr/0012-terraform-contra-localstack.md;
# o módulo continua descrevendo a intenção (expirar versões antigas de
# comprovante em 30 dias) só que fora do Terraform aplicado aqui.

resource "aws_s3_bucket_public_access_block" "receipts" {
  bucket                  = aws_s3_bucket.receipts.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
