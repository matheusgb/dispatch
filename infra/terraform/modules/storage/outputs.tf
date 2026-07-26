output "bucket_name" {
  value = aws_s3_bucket.receipts.bucket
}

output "bucket_arn" {
  value = aws_s3_bucket.receipts.arn
}
