output "bucket_name" {
  value = aws_s3_bucket.bucket.bucket
}

output "lambda_arn" {
  value = module.target.lambda_arn
}

output "queue_url" {
  value = module.target.queue_url
}

output "owner" {
  value = "a"
}
