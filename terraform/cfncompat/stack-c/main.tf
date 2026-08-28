# Stack C references stack-a's bucket by name (never by remote state / outputs) and attaches
# its own notification target (prefix "c/") purely via ../modules/bucket-notifications'
# cfncompat_custom_resource -- no aws_s3_bucket_notification anywhere in this scenario.
#
# awscc has no data source for an existing S3 bucket, and none is needed: CONTRACT.md's
# deterministic naming makes both the bucket name and its arn plain strings, so this root
# never has to look the bucket up (and stays free of any hashicorp/aws data source).

locals {
  bucket_name = "s3n-harness-${var.suffix}"
  bucket_arn  = "arn:aws:s3:::s3n-harness-${var.suffix}"
}

module "target" {
  source = "../../awscc/modules/notification-target"

  suffix      = var.suffix
  owner       = "c"
  bucket_name = local.bucket_name
  bucket_arn  = local.bucket_arn
}

module "bucket_notifications" {
  source = "../modules/bucket-notifications"

  suffix        = var.suffix
  owner         = "c"
  bucket_name   = local.bucket_name
  lambda_arn    = module.target.lambda_arn
  filter_prefix = "c/"

  # The s3 permission allowing this lambda to be invoked (built by module.target) must
  # exist before the custom resource's handler puts the notification configuration.
  depends_on = [module.target]
}
