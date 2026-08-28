output "lambda_arn" {
  value = aws_lambda_function.handler.arn
}

output "queue_url" {
  value = aws_sqs_queue.results.url
}

output "role_arn" {
  value = aws_iam_role.lambda.arn
}
