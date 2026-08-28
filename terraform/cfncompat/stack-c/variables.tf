variable "suffix" {
  type        = string
  description = "Unique per-test-run suffix (lowercase, e.g. k3m9x1). Bucket name = s3n-harness-<suffix>, owned by stack-a."
}

variable "region" {
  type        = string
  default     = "us-east-1"
  description = "AWS region for all providers (awscc, aws, cfncompat)."
}
