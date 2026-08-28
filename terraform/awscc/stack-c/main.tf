# Stack C references stack-a's bucket by name (never by remote state / outputs) and attaches
# its own notification target (prefix "c/") via aws_s3_bucket_notification. Same RED behavior
# as stack-b: this resource overwrites the bucket's whole notification configuration on apply.

data "aws_s3_bucket" "shared" {
  bucket = "s3n-harness-${var.suffix}"
}

module "target" {
  source = "../modules/notification-target"

  suffix      = var.suffix
  owner       = "c"
  bucket_name = data.aws_s3_bucket.shared.bucket
  bucket_arn  = data.aws_s3_bucket.shared.arn
}

resource "aws_s3_bucket_notification" "this" {
  bucket = data.aws_s3_bucket.shared.bucket

  lambda_function {
    lambda_function_arn = module.target.lambda_arn
    events              = ["s3:ObjectCreated:*"]
    filter_prefix       = "c/"
  }

  # The s3 permission allowing this lambda to be invoked must exist before S3 is told about it.
  depends_on = [module.target]
}
