# s3-notifications-harness

Test harness that reproduces the "single authoritative S3 bucket notification configuration"
problem across independently-deployed stacks, side by side:

| Suite | Tooling | Expected |
|---|---|---|
| `awscdk/` | AWS CDK v2 L2 constructs (`Bucket.addEventNotification` → `BucketNotifications` custom resource) | GREEN |
| `terraform/` | Terraform 1.15 + `hashicorp/awscc` (bucket, lambda, sqs, iam) + `hashicorp/aws` `aws_s3_bucket_notification` for cross-stack attach | RED |

Three stacks per suite:

- **Stack A** — owns the bucket, adds notification target `a` (lambda, prefix `a/`)
- **Stack B** — references A's bucket by name, adds target `b` (prefix `b/`)
- **Stack C** — references A's bucket by name, adds target `c` (prefix `c/`)

`integ/` is a terratest (Go) suite that deploys A → B → C, then re-applies A, and after every step asserts:

1. `GetBucketNotificationConfiguration` still contains every previously-registered target
2. uploading `a/x`, `b/x`, `c/x` delivers exactly one event to the corresponding stack's results SQS queue

Every sample lambda (`lambda/index.mjs`) forwards its S3 events to a per-stack SQS results queue.

## Prereqs

```sh
mise install            # terraform 1.15.9, go, node 24, aws-cli
aws-vault exec tcons-vincent -- make -C integ test
```
