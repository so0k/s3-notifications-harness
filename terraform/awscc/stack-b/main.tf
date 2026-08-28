# Stack B references stack-a's bucket by name (never by remote state / outputs) and attaches
# its own notification target (prefix "b/") via aws_s3_bucket_notification. That resource
# replaces the bucket's *entire* notification configuration on apply -- it does not merge with
# stack-a's inline awscc lambda_configurations -- which is the "RED" behavior this harness
# demonstrates.
#
# This root is otherwise hashicorp/awscc only: the bucket lookup uses data "awscc_s3_bucket"
# (not data "aws_s3_bucket"), and modules/notification-target is pure awscc_*. hashicorp/aws is
# kept for exactly one resource -- aws_s3_bucket_notification below -- because awscc has no
# equivalent resource; that is the whole point of this scenario, so it stays.

data "awscc_s3_bucket" "shared" {
  id = "s3n-harness-${var.suffix}"
}

module "target" {
  source = "../modules/notification-target"

  suffix      = var.suffix
  owner       = "b"
  bucket_name = data.awscc_s3_bucket.shared.bucket_name
  bucket_arn  = data.awscc_s3_bucket.shared.arn
}

resource "aws_s3_bucket_notification" "this" {
  bucket = data.awscc_s3_bucket.shared.bucket_name

  lambda_function {
    lambda_function_arn = module.target.lambda_arn
    events              = ["s3:ObjectCreated:*"]
    filter_prefix       = "b/"
  }

  # The s3 permission allowing this lambda to be invoked must exist before S3 is told about it.
  depends_on = [module.target]
}
