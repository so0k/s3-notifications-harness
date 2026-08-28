# Shared notification target: results queue + lambda + role + s3->lambda permission.
# Used identically by stack-a, stack-b, and stack-c so every stack's target is built the
# same way regardless of who owns the underlying bucket. Plain hashicorp/aws resources only
# (no awscc) -- this is the "aws" scenario's counterpart of ../../awscc/modules/notification-target.

data "aws_caller_identity" "current" {}

resource "aws_sqs_queue" "results" {
  name = "s3n-harness-${var.suffix}-${var.owner}-results"
  tags = {
    "s3n-harness:bucket" = var.bucket_name
  }
}

resource "aws_iam_role" "lambda" {
  name = "s3n-harness-${var.suffix}-${var.owner}-lambda"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "sqs_send" {
  name = "sqs-send"
  role = aws_iam_role.lambda.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "sqs:SendMessage"
      Resource = aws_sqs_queue.results.arn
    }]
  })
}

resource "aws_iam_role_policy_attachment" "basic_execution" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "archive_file" "lambda" {
  type        = "zip"
  source_file = "${path.module}/../../../../lambda/index.js"
  # path.root (the calling root module's own directory), not path.module: stack-a, stack-b
  # and stack-c all share this one module directory, so path.module would collect every
  # root's zip inside the shared module instead of under the root that built it.
  output_path = "${path.root}/dist/index-${var.suffix}-${var.owner}.zip"
}

resource "aws_lambda_function" "handler" {
  function_name = "s3n-harness-${var.suffix}-${var.owner}"
  role          = aws_iam_role.lambda.arn
  handler       = "index.handler"
  runtime       = "nodejs22.x"

  filename         = data.archive_file.lambda.output_path
  source_code_hash = data.archive_file.lambda.output_base64sha256

  environment {
    variables = {
      RESULTS_QUEUE_URL = aws_sqs_queue.results.url
      STACK_NAME        = var.owner
    }
  }

  depends_on = [aws_iam_role_policy_attachment.basic_execution, aws_iam_role_policy.sqs_send]
}

resource "aws_lambda_permission" "allow_s3" {
  statement_id   = "AllowS3Invoke"
  action         = "lambda:InvokeFunction"
  principal      = "s3.amazonaws.com"
  function_name  = aws_lambda_function.handler.function_name
  source_arn     = var.bucket_arn
  source_account = data.aws_caller_identity.current.account_id
}
