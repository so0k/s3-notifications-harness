# terraform/

Three sibling scenarios, all reproducing (or, for `cfncompat/`, polyfilling) the "single
authoritative S3 bucket notification configuration" problem across independently-deployed root
modules, wired with different providers so the RED behavior — and `cfncompat/`'s fix — can be
attributed to a specific resource rather than to Terraform in general:

| Scenario | Provider(s) | Bucket owned by (stack-a) | Cross-stack attach (stack-b/c) |
|---|---|---|---|
| [`awscc/`](awscc/README.md) | `hashicorp/awscc` (+ `hashicorp/aws` for the caller-identity data source) | `awscc_s3_bucket` with inline `notification_configuration` | `aws_s3_bucket_notification` |
| [`aws/`](aws/README.md) | `hashicorp/aws` only | `aws_s3_bucket` + a separate `aws_s3_bucket_notification` | `aws_s3_bucket_notification` |
| [`cfncompat/`](cfncompat/README.md) | `hashicorp/awscc` + `cdktn-io/cfncompat` (+ `hashicorp/aws` for the caller-identity data source and the response bucket's `force_destroy`) | `awscc_s3_bucket` with **no** `notification_configuration` at all + a per-stack `cfncompat_custom_resource` (no `aws_s3_bucket_notification` anywhere) | same `cfncompat_custom_resource` polyfill |

In `awscc/`, stack-a's target is inline on the bucket resource itself; in `aws/`, stack-a's
target uses the very same `aws_s3_bucket_notification` resource that stack-b and stack-c use.
That difference is deliberate: it isolates whether the RED behavior comes from mixing
`awscc_s3_bucket`'s inline config with `aws_s3_bucket_notification`, or from
`aws_s3_bucket_notification` itself always being authoritative regardless of what owns the
bucket. `cfncompat/` is built on the same `hashicorp/awscc` resources as `awscc/`, but replaces that
singleton resource everywhere with a `cfncompat_custom_resource` driving AWS CDK's own
bucket-notifications Lambda handler in its merge ("unmanaged") mode — see `../docs/OPTIONS.md`
(Option A) and `../CONTRACT.md`. Keeping it on `awscc` also makes it a test of
`awscc_s3_bucket.notification_configuration` being Optional+Computed: stack-a declares no
notification config at all, so the configuration the custom resource's handler writes out of
band must not resurface as drift on later plans.

Each scenario has three independent root modules (`stack-a`, `stack-b`, `stack-c`) sharing a
`modules/notification-target` module (results SQS queue, lambda role, lambda function, and the
`s3.amazonaws.com` invoke permission; `cfncompat/` reuses `awscc/`'s copy of this module by
relative path) — see each scenario's own README for apply order and verify-without-credentials steps.
Naming, inputs, and outputs are identical across all three scenarios and across the `awscdk/`
suite; see [`../CONTRACT.md`](../CONTRACT.md).

## Verify without AWS credentials (all scenarios)

```sh
cd terraform
for scenario in awscc aws cfncompat; do
  for x in a b c; do
    mise x -- terraform -chdir=$scenario/stack-$x init -backend=false && \
      mise x -- terraform -chdir=$scenario/stack-$x validate
  done
done
mise x -- terraform fmt -check -recursive
```
