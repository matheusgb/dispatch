output "receipts_bucket_name" {
  value = module.storage.bucket_name
}

output "jwt_secret_arn" {
  value = module.secrets.secret_arn
}

output "jwt_secret_name" {
  value = module.secrets.secret_name
}
