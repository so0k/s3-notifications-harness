variable "suffix" {
  type        = string
  description = "Shared harness run suffix, e.g. k3m9x1."
}

variable "owner" {
  type        = string
  description = "Which stack owns this notification target: a, b, or c."

  validation {
    condition     = contains(["a", "b", "c"], var.owner)
    error_message = "owner must be one of: a, b, c."
  }
}

variable "bucket_name" {
  type        = string
  description = "Name of the shared bucket (s3n-harness-<suffix>) this custom resource merges a notification into."
}

variable "lambda_arn" {
  type        = string
  description = "ARN of the target lambda function (this stack's notification-target) to route s3:ObjectCreated:* events to."
}

variable "filter_prefix" {
  type        = string
  description = "S3 key prefix filter for this stack's notification entry, e.g. \"a/\"."
}
