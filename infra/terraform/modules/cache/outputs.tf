output "configuration_endpoint" {
  value = aws_elasticache_replication_group.this.configuration_endpoint_address != null ? aws_elasticache_replication_group.this.configuration_endpoint_address : aws_elasticache_replication_group.this.primary_endpoint_address
}

output "port" {
  value = aws_elasticache_replication_group.this.port
}
