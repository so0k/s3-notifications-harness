output "bucket_name" {
  value = local.bucket_name
}

output "lambda_arn" {
  value = module.target.lambda_arn
}

output "queue_url" {
  value = module.target.queue_url
}

output "owner" {
  value = "c"
}
