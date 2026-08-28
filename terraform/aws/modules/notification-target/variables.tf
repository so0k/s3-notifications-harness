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

variable "bucket_arn" {
  type        = string
  description = "ARN of the shared bucket, used for the lambda permission source_arn."
}

variable "bucket_name" {
  type        = string
  description = "Name of the shared bucket (s3n-harness-<suffix>)."
}
