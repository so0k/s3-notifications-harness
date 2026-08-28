# s3-notifications-harness

Test harness that reproduces the "single authoritative S3 bucket notification configuration"
problem across independently-deployed stacks, side by side:

| Suite | Tooling | Expected |
|---|---|---|
| `awscdk/` | AWS CDK v2 L2 constructs (`Bucket.addEventNotification` → `BucketNotifications` custom resource) | GREEN |
| `terraform/awscc/` | Terraform 1.15 + `hashicorp/awscc` (bucket, lambda, sqs, iam) + `hashicorp/aws` `aws_s3_bucket_notification` for cross-stack attach | RED |
| `terraform/aws/` | Terraform 1.15 + `hashicorp/aws` only — stack-a's own target also via `aws_s3_bucket_notification` | RED |
| `terraform/cfncompat/` | Terraform 1.15 + `hashicorp/awscc` + `cdktn-io/cfncompat` — every target attached by a `cfncompat_custom_resource` driving AWS CDK's own notifications handler in merge mode | GREEN |
| `cdktn/` | cdktn 0.24 TypeScript + `@cdktn/provider-cfncompat`'s `CustomResource` — `terraform/cfncompat/` ported 1:1 (Option B, docs/OPTIONS.md) | GREEN |

Three stacks per suite (five suites total):

- **Stack A** — owns the bucket, adds notification target `a` (lambda, prefix `a/`)
- **Stack B** — references A's bucket by name, adds target `b` (prefix `b/`)
- **Stack C** — references A's bucket by name, adds target `c` (prefix `c/`)

`integ/` is a terratest (Go) suite that deploys A → B → C, then re-applies A, and after every step asserts:

1. `GetBucketNotificationConfiguration` still contains every previously-registered target
2. uploading `a/x`, `b/x`, `c/x` delivers exactly one event to the corresponding stack's results SQS queue

Every sample lambda (`lambda/index.js`) forwards its S3 events to a per-stack SQS results queue.

CDK only passes because `cdk.json` enables `@aws-cdk/aws-s3:keepNotificationInImportedBucket`; without it the owning stack's handler takes the `Managed` path and overwrites the whole configuration on every deploy, exactly like Terraform.

## Prereqs

```sh
mise install            # terraform 1.15.9, go, node 24, aws-cli
aws-vault exec --no-session tcons-vincent -- make -C integ test
```

## Docs

- [CONTRACT.md](CONTRACT.md) — cross-suite contract: names, inputs, outputs, test flow (single source of truth)
- [docs/OPTIONS.md](docs/OPTIONS.md) — the options considered for the cfncompat polyfill, which of them are built (`terraform/cfncompat/`, `cdktn/`), and what is left
- [docs/RESULTS.md](docs/RESULTS.md) — index of the append-only per-run result documents under `docs/results/`
- [awscdk/README.md](awscdk/README.md), [terraform/README.md](terraform/README.md), [cdktn/README.md](cdktn/README.md), [integ/README.md](integ/README.md) — per-suite usage
- [docs/awscc-constraints.md](docs/awscc-constraints.md) — awscc behaviours hit during the runs
