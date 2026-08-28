# Stack A owns the shared bucket. Unlike ../../aws/ and ../../awscc/, there is no
# aws_s3_bucket_notification (or inline awscc notification_configuration) anywhere in this
# scenario -- every stack's notification target, including A's own, is attached purely via
# ../modules/bucket-notifications' cfncompat_custom_resource, which drives AWS CDK's own
# bucket-notifications Lambda handler in its merge ("unmanaged") mode. See CONTRACT.md and
# ../../../docs/OPTIONS.md (Option A).

resource "aws_s3_bucket" "bucket" {
  bucket        = "s3n-harness-${var.suffix}"
  force_destroy = true
}

module "target" {
  source = "../../aws/modules/notification-target"

  suffix      = var.suffix
  owner       = "a"
  bucket_name = aws_s3_bucket.bucket.bucket
  bucket_arn  = aws_s3_bucket.bucket.arn
}

module "bucket_notifications" {
  source = "../modules/bucket-notifications"

  suffix        = var.suffix
  owner         = "a"
  bucket_name   = aws_s3_bucket.bucket.bucket
  lambda_arn    = module.target.lambda_arn
  filter_prefix = "a/"

  # The s3 permission allowing this lambda to be invoked (built by module.target) must
  # exist before the custom resource's handler puts the notification configuration.
  depends_on = [module.target]
}
