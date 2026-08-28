# terraform/

Two sibling scenarios, both reproducing the "single authoritative S3 bucket notification
configuration" problem across independently-deployed root modules, but wired with different
providers so the RED behavior can be attributed to a specific resource rather than to Terraform
in general:

| Scenario | Provider(s) | Bucket owned by (stack-a) | Cross-stack attach (stack-b/c) |
|---|---|---|---|
| [`awscc/`](awscc/README.md) | `hashicorp/awscc` (+ `hashicorp/aws` for the caller-identity data source) | `awscc_s3_bucket` with inline `notification_configuration` | `aws_s3_bucket_notification` |
| [`aws/`](aws/README.md) | `hashicorp/aws` only | `aws_s3_bucket` + a separate `aws_s3_bucket_notification` | `aws_s3_bucket_notification` |

In `awscc/`, stack-a's target is inline on the bucket resource itself; in `aws/`, stack-a's
target uses the very same `aws_s3_bucket_notification` resource that stack-b and stack-c use.
That difference is deliberate: it isolates whether the RED behavior comes from mixing
`awscc_s3_bucket`'s inline config with `aws_s3_bucket_notification`, or from
`aws_s3_bucket_notification` itself always being authoritative regardless of what owns the
bucket.

Each scenario has three independent root modules (`stack-a`, `stack-b`, `stack-c`) sharing a
`modules/notification-target` module (results SQS queue, lambda role, lambda function, and the
`s3.amazonaws.com` invoke permission) — see each scenario's own README for apply order and
verify-without-credentials steps. Naming, inputs, and outputs are identical across both
scenarios and across the `awscdk/` suite; see [`../CONTRACT.md`](../CONTRACT.md).

## Verify without AWS credentials (both scenarios)

```sh
cd terraform
for scenario in awscc aws; do
  for x in a b c; do
    mise x -- terraform -chdir=$scenario/stack-$x init -backend=false && \
      mise x -- terraform -chdir=$scenario/stack-$x validate
  done
done
mise x -- terraform fmt -check -recursive
```
