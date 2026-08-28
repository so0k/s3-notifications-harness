output "lambda_arn" {
  value = awscc_lambda_function.handler.arn
}

output "queue_url" {
  value = awscc_sqs_queue.results.queue_url
}

output "role_arn" {
  value = awscc_iam_role.lambda.arn
}
