# Polyfills the single authoritative aws_s3_bucket_notification resource (see
# ../../../aws/ and ../../../awscc/, both RED) with a per-stack cfncompat_custom_resource
# that drives the AWS CDK's own bucket-notifications Lambda handler
# (../../../../lambda/notifications-handler/index.py, copied verbatim) in its "unmanaged"
# mode (Managed = "false"): the handler GETs the bucket's existing notification
# configuration, merges in only this stack's own entries (tracked by a stack_id-prefixed
# Id -- see the handler's handle_unmanaged()), and PUTs the merged result back. That
# merge -- not overwrite -- semantics is exactly what CONTRACT.md's cross-stack flow
# needs and what aws_s3_bucket_notification cannot do.
#
# Like the rest of this scenario, the handler's role and lambda are awscc_* resources
# (hashicorp/awscc), matching ../../../awscc/modules/notification-target.
#
# Each stack gets its own response bucket, IAM role/handler lambda, and custom resource;
# the target lambda (this stack's own s3n-harness-<suffix>-<owner> notification
# destination, built by ../notification-target) is passed in as `lambda_arn` and is a
# separate concern from the handler lambda built here.

# The one deliberate hashicorp/aws resource left in this scenario: awscc_s3_bucket has no
# force_destroy equivalent, and this bucket cannot be relied on to be empty at destroy
# time. cfncompat only deletes a response object *best effort*, after it has successfully
# read and parsed it (see the provider's customResourceEngine.parseResponse) -- so any run
# where the handler fails, times out, or the delete call itself errors leaves an object
# behind, and an awscc_s3_bucket would then fail to destroy with BucketNotEmpty, stranding
# the whole root. The terratest cleanup only empties the *shared* bucket, not this one.
# force_destroy = true keeps destroy unconditional. This is orthogonal to what the scenario
# tests (drift on the shared awscc_s3_bucket in stack-a, which carries no
# notification_configuration block at all).
resource "aws_s3_bucket" "responses" {
  bucket        = "s3n-harness-${var.suffix}-${var.owner}-cfn-responses"
  force_destroy = true
}

resource "awscc_iam_role" "handler" {
  role_name = "s3n-harness-${var.suffix}-${var.owner}-notifications-handler"

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

  # GetBucketNotification (read the existing config to merge with) + PutBucketNotification
  # (write the merged config back) on the shared bucket -- matches the permissions AWS CDK's
  # own BucketNotifications construct grants its handler for an "unmanaged" (imported) bucket.
  policies = [{
    policy_name = "s3-bucket-notifications"
    policy_document = jsonencode({
      Version = "2012-10-17"
      Statement = [{
        Effect   = "Allow"
        Action   = ["s3:GetBucketNotification", "s3:PutBucketNotification"]
        Resource = "arn:aws:s3:::${var.bucket_name}"
      }]
    })
  }]
}

resource "awscc_lambda_function" "handler" {
  function_name = "s3n-harness-${var.suffix}-${var.owner}-notifications-handler"
  role          = awscc_iam_role.handler.arn
  handler       = "index.handler"
  runtime       = "python3.12"

  # Matches AWS CDK's own provisioning of this exact handler (Timeout: 300 --
  # aws-cdk-lib/aws-s3/lib/notifications-resource/notifications-resource-handler.ts) and the
  # custom resource's service_timeout below: GetBucketNotificationConfiguration +
  # PutBucketNotificationConfiguration (destination validation on, since SkipDestinationValidation
  # is unset) right after a fresh lambda permission can be slow, and a short timeout here would
  # look like a cfncompat protocol-engine bug rather than a Lambda timeout.
  timeout = 300

  # Inline source, straight from the CDK handler file -- no archive_file / zip artifact.
  code = {
    zip_file = file("${path.module}/../../../../lambda/notifications-handler/index.py")
  }
}

# Emulates the CloudFormation "Custom::S3BucketNotifications" resource AWS CDK's
# BucketNotifications construct would declare, driving the same handler with the same
# request shape it expects (see the handler's `handler()`/`handle_unmanaged()` and CDK's
# notifications-resource.ts renderNotificationConfiguration()).
resource "cfncompat_custom_resource" "bucket_notifications" {
  service_token       = awscc_lambda_function.handler.arn
  resource_type       = "Custom::S3BucketNotifications"
  stack_id            = "s3n-harness-${var.suffix}-${var.owner}"
  logical_resource_id = "BucketNotifications"
  response_bucket     = aws_s3_bucket.responses.bucket
  service_timeout     = 300

  resource_properties = {
    BucketName = var.bucket_name
    NotificationConfiguration = {
      LambdaFunctionConfigurations = [
        {
          Events            = ["s3:ObjectCreated:*"]
          LambdaFunctionArn = var.lambda_arn
          Filter = {
            Key = {
              FilterRules = [
                {
                  Name  = "prefix"
                  Value = var.filter_prefix
                }
              ]
            }
          }
        }
      ]
    }
    # Must be the *string* "false" (not a bool) -- the handler does
    # props.get('Managed', 'true').lower() == 'true', so only a string survives the
    # round trip through the request event's JSON the way CDK's own synthesis emits it.
    # "false" (unmanaged/merge mode) is what makes each stack's apply merge with the
    # others instead of clobbering them, which is the whole point of this scenario.
    Managed = "false"
  }

  # The handler must be able to invoke s3:Get/PutBucketNotification before cfncompat
  # invokes it -- awscc_iam_role carries both the managed policy and the inline policy,
  # so depending on the role covers both. The target lambda's own invoke permission (a
  # separate concern, built by ../notification-target) is depended on at the stack level
  # via the module block's own depends_on = [module.target], not here.
  depends_on = [awscc_iam_role.handler]
}
