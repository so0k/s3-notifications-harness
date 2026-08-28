# Stack A owns the shared bucket and its own notification target (prefix "a/"). Plain
# hashicorp/aws resources only (no awscc): aws_s3_bucket + a separate aws_s3_bucket_notification,
# same resource stack-b/c use to attach their own targets -- so this scenario demonstrates
# whether plain aws_s3_bucket_notification merges or replaces when applied by independent roots.

resource "aws_s3_bucket" "bucket" {
  bucket        = "s3n-harness-${var.suffix}"
  force_destroy = true
}

module "target" {
  source = "../modules/notification-target"

  suffix      = var.suffix
  owner       = "a"
  bucket_name = aws_s3_bucket.bucket.bucket
  bucket_arn  = aws_s3_bucket.bucket.arn
}

resource "aws_s3_bucket_notification" "this" {
  bucket = aws_s3_bucket.bucket.id

  lambda_function {
    lambda_function_arn = module.target.lambda_arn
    events              = ["s3:ObjectCreated:*"]
    filter_prefix       = "a/"
  }

  # The s3 permission allowing this lambda to be invoked must exist before S3 is told about it.
  depends_on = [module.target]
}
