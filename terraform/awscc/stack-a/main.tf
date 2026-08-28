# Stack A owns the shared bucket and its own notification target (prefix "a/").
#
# awscc_s3_bucket's inline notification_configuration must reference the lambda's arn, and the
# lambda permission must reference the bucket's arn -> a naive wiring creates a resource cycle
# (bucket -> lambda -> permission -> bucket). Break it by computing the bucket arn as a plain
# string (not `awscc_s3_bucket.bucket.arn`) for the permission's source_arn, and make the bucket
# depend on the permission explicitly so the permission still exists before S3 is told to invoke
# the function.

module "target" {
  source = "../modules/notification-target"

  suffix      = var.suffix
  owner       = "a"
  bucket_name = "s3n-harness-${var.suffix}"
  # Literal string, not `awscc_s3_bucket.bucket.arn`: breaks the cycle described above.
  bucket_arn = "arn:aws:s3:::s3n-harness-${var.suffix}"
}

resource "awscc_s3_bucket" "bucket" {
  bucket_name = "s3n-harness-${var.suffix}"

  notification_configuration = {
    lambda_configurations = [{
      event    = "s3:ObjectCreated:*"
      function = module.target.lambda_arn
      filter = {
        s3_key = {
          rules = [{
            name  = "Prefix"
            value = "a/"
          }]
        }
      }
    }]
  }

  # Ensure the permission allowing S3 to invoke the lambda exists before the bucket's
  # notification configuration is applied.
  depends_on = [module.target]
}
