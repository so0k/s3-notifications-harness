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
# Each stack gets its own response bucket, IAM role/handler lambda, and custom resource;
# the target lambda (this stack's own s3n-harness-<suffix>-<owner> notification
# destination, built by ../notification-target) is passed in as `lambda_arn` and is a
# separate concern from the handler lambda built here.

resource "aws_s3_bucket" "responses" {
  bucket        = "s3n-harness-${var.suffix}-${var.owner}-cfn-responses"
  force_destroy = true
}

data "aws_iam_policy_document" "handler_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "handler" {
  name               = "s3n-harness-${var.suffix}-${var.owner}-notifications-handler"
  assume_role_policy = data.aws_iam_policy_document.handler_assume_role.json
}

resource "aws_iam_role_policy_attachment" "handler_basic_execution" {
  role       = aws_iam_role.handler.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# GetBucketNotification (read the existing config to merge with) + PutBucketNotification
# (write the merged config back) on the shared bucket -- matches the permissions AWS CDK's
# own BucketNotifications construct grants its handler for an "unmanaged" (imported) bucket.
resource "aws_iam_role_policy" "handler_s3_notifications" {
  name = "s3-bucket-notifications"
  role = aws_iam_role.handler.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["s3:GetBucketNotification", "s3:PutBucketNotification"]
      Resource = "arn:aws:s3:::${var.bucket_name}"
    }]
  })
}

data "archive_file" "handler" {
  type        = "zip"
  source_file = "${path.module}/../../../../lambda/notifications-handler/index.py"
  output_path = "${path.module}/dist/notifications-handler-${var.suffix}-${var.owner}.zip"
}

resource "aws_lambda_function" "handler" {
  function_name = "s3n-harness-${var.suffix}-${var.owner}-notifications-handler"
  role          = aws_iam_role.handler.arn
  handler       = "index.handler"
  runtime       = "python3.12"
  # Matches AWS CDK's own provisioning of this exact handler (Timeout: 300 --
  # aws-cdk-lib/aws-s3/lib/notifications-resource/notifications-resource-handler.ts) and the
  # custom resource's service_timeout above: GetBucketNotificationConfiguration +
  # PutBucketNotificationConfiguration (destination validation on, since SkipDestinationValidation
  # is unset) right after a fresh aws_lambda_permission can be slow, and 60s was tight enough that
  # a timeout here would look like a cfncompat protocol-engine bug rather than a Lambda timeout.
  timeout = 300

  filename         = data.archive_file.handler.output_path
  source_code_hash = data.archive_file.handler.output_base64sha256

  depends_on = [
    aws_iam_role_policy_attachment.handler_basic_execution,
    aws_iam_role_policy.handler_s3_notifications,
  ]
}

# Emulates the CloudFormation "Custom::S3BucketNotifications" resource AWS CDK's
# BucketNotifications construct would declare, driving the same handler with the same
# request shape it expects (see the handler's `handler()`/`handle_unmanaged()` and CDK's
# notifications-resource.ts renderNotificationConfiguration()).
resource "cfncompat_custom_resource" "bucket_notifications" {
  service_token       = aws_lambda_function.handler.arn
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
  # invokes it; the target lambda's own invoke permission (a separate concern, built by
  # ../notification-target) is depended on at the stack level via the module block's
  # own depends_on = [module.target], not here.
  depends_on = [
    aws_iam_role_policy_attachment.handler_basic_execution,
    aws_iam_role_policy.handler_s3_notifications,
  ]
}
