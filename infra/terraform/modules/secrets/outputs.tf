output "secret_arn" {
  value = aws_secretsmanager_secret.jwt.arn
}

output "secret_name" {
  value = aws_secretsmanager_secret.jwt.name
}

output "kms_key_id" {
  value = aws_kms_key.jwt.key_id
}
