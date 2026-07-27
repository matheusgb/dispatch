# Módulo secrets: chave KMS e segredo no Secrets Manager para o
# LUNCHRUSH_JWT_SECRET. Contra AWS real, KMS teria rotação automática de
# chave e política de acesso via IAM. Contra LocalStack community, a chave
# é criada e usada de verdade para cifrar o segredo (KMS local funciona:
# CreateKey, Encrypt/Decrypt e a integração nativa do Secrets Manager com
# kms_key_id), mas não há garantia de HSM real nem de rotação automática
# suportada pelo emulador.

resource "aws_kms_key" "jwt" {
  description             = "chave usada para cifrar o segredo do JWT do lunchrush"
  deletion_window_in_days = 7

  tags = {
    Owner       = "lunchrush"
    Project     = "lunchrush"
    Environment = var.environment
  }
}

resource "aws_kms_alias" "jwt" {
  name          = "alias/lunchrush-${var.environment}-jwt"
  target_key_id = aws_kms_key.jwt.key_id
}

resource "aws_secretsmanager_secret" "jwt" {
  name       = var.secret_name
  kms_key_id = aws_kms_key.jwt.key_id

  tags = {
    Owner       = "lunchrush"
    Project     = "lunchrush"
    Environment = var.environment
  }
}

resource "aws_secretsmanager_secret_version" "jwt" {
  secret_id     = aws_secretsmanager_secret.jwt.id
  secret_string = var.jwt_secret_value
}
