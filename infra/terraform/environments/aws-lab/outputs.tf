output "receipts_bucket_name" {
  value = module.storage.bucket_name
}

output "jwt_secret_arn" {
  value = module.secrets.secret_arn
}

output "jwt_secret_name" {
  value = module.secrets.secret_name
}

output "database_endpoint" {
  value = module.database.endpoint
}

output "cache_configuration_endpoint" {
  value = module.cache.configuration_endpoint
}

output "msk_cluster_arn" {
  value = module.messaging.cluster_arn
}

output "eks_cluster_endpoint" {
  value = module.kubernetes.endpoint
}
