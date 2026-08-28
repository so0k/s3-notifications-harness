# Stack A owns the shared bucket. Unlike ../../aws/ and ../../awscc/, there is no
# aws_s3_bucket_notification (or inline awscc notification_configuration) anywhere in this
# scenario -- every stack's notification target, including A's own, is attached purely via
# ../modules/bucket-notifications' cfncompat_custom_resource, which drives AWS CDK's own
# bucket-notifications Lambda handler in its merge ("unmanaged") mode. See CONTRACT.md and
# ../../../docs/OPTIONS.md (Option A).
#
# The bucket is deliberately declared with *no* notification_configuration block at all:
# awscc_s3_bucket.notification_configuration is Optional+Computed, so this scenario also
# proves that the configuration the custom resource's handler writes out of band does not
# show up as drift ("plan: no changes") on subsequent plans/applies of this root.

resource "awscc_s3_bucket" "bucket" {
  bucket_name = "s3n-harness-${var.suffix}"
}

module "target" {
  source = "../../awscc/modules/notification-target"

  suffix      = var.suffix
  owner       = "a"
  bucket_name = awscc_s3_bucket.bucket.bucket_name
  bucket_arn  = awscc_s3_bucket.bucket.arn
}

module "bucket_notifications" {
  source = "../modules/bucket-notifications"

  suffix        = var.suffix
  owner         = "a"
  bucket_name   = awscc_s3_bucket.bucket.bucket_name
  lambda_arn    = module.target.lambda_arn
  filter_prefix = "a/"

  # The s3 permission allowing this lambda to be invoked (built by module.target) must
  # exist before the custom resource's handler puts the notification configuration, and
  # the bucket itself must exist before the handler GETs/PUTs its configuration.
  depends_on = [module.target, awscc_s3_bucket.bucket]
}
