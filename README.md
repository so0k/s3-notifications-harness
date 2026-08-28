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

## Observed results (2026-08-28, account 694710432912, us-east-1)

| Stage | awscdk | terraform 1.15.9 + awscc 1.98 |
|---|---|---|
| deploy A → config ⊇ {a} | ✅ | ✅ |
| deploy B → config ⊇ {a,b} | ✅ | ❌ config = {b} — `aws_s3_bucket_notification` plan only says "will be created", A's target is dropped silently |
| deploy C → config ⊇ {a,b,c} | ✅ | ❌ config = {c} |
| re-deploy A → config ⊇ {a,b,c} | ✅ (no-op) | ❌ plan: `awscc_s3_bucket.bucket will be updated in-place` → config = {a} |
| destroy B → config = {a,c} | ✅ | ❌ config = {} (destroying B's authoritative resource wipes everything) |

CDK only passes because `cdk.json` enables `@aws-cdk/aws-s3:keepNotificationInImportedBucket`; without it the owning stack's handler takes the `Managed` path and overwrites the whole configuration on every deploy, exactly like Terraform.

## Prereqs

```sh
mise install            # terraform 1.15.9, go, node 24, aws-cli
aws-vault exec --no-session tcons-vincent -- make -C integ test
```
