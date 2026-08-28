# Shared notification target: results queue + lambda + role + s3->lambda permission.
# Used identically by stack-a, stack-b, and stack-c so every stack's target is built the
# same way regardless of who owns the underlying bucket.

data "aws_caller_identity" "current" {}

resource "awscc_sqs_queue" "results" {
  queue_name = "s3n-harness-${var.suffix}-${var.owner}-results"
  tags = [{
    key   = "s3n-harness:bucket"
    value = var.bucket_name
  }]
}

resource "awscc_iam_role" "lambda" {
  role_name = "s3n-harness-${var.suffix}-${var.owner}-lambda"

  assume_role_policy_document = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })

  managed_policy_arns = [
    "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
  ]

  policies = [{
    policy_name = "sqs-send"
    policy_document = jsonencode({
      Version = "2012-10-17"
      Statement = [{
        Effect   = "Allow"
        Action   = "sqs:SendMessage"
        Resource = awscc_sqs_queue.results.arn
      }]
    })
  }]
}

resource "awscc_lambda_function" "handler" {
  function_name = "s3n-harness-${var.suffix}-${var.owner}"
  role          = awscc_iam_role.lambda.arn
  handler       = "index.handler"
  runtime       = "nodejs22.x"

  code = {
    zip_file = file("${path.module}/../../../lambda/index.js")
  }

  environment = {
    variables = {
      RESULTS_QUEUE_URL = awscc_sqs_queue.results.queue_url
      STACK_NAME        = var.owner
    }
  }
}

resource "awscc_lambda_permission" "allow_s3" {
  action         = "lambda:InvokeFunction"
  principal      = "s3.amazonaws.com"
  function_name  = awscc_lambda_function.handler.function_name
  source_arn     = var.bucket_arn
  source_account = data.aws_caller_identity.current.account_id
}
