# Stack B references stack-a's bucket by name (never by remote state / outputs) and attaches
# its own notification target (prefix "b/") purely via ../modules/bucket-notifications'
# cfncompat_custom_resource -- no aws_s3_bucket_notification anywhere in this scenario.

data "aws_s3_bucket" "shared" {
  bucket = "s3n-harness-${var.suffix}"
}

module "target" {
  source = "../../aws/modules/notification-target"

  suffix      = var.suffix
  owner       = "b"
  bucket_name = data.aws_s3_bucket.shared.bucket
  bucket_arn  = data.aws_s3_bucket.shared.arn
}

module "bucket_notifications" {
  source = "../modules/bucket-notifications"

  suffix        = var.suffix
  owner         = "b"
  bucket_name   = data.aws_s3_bucket.shared.bucket
  lambda_arn    = module.target.lambda_arn
  filter_prefix = "b/"

  # The s3 permission allowing this lambda to be invoked (built by module.target) must
  # exist before the custom resource's handler puts the notification configuration.
  depends_on = [module.target]
}
