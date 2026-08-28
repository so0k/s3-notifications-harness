# terraform/

Three independent root modules that reproduce the "single authoritative S3 bucket notification
configuration" problem: `stack-a` owns the bucket and its target; `stack-b` and `stack-c`
reference the bucket by name (deterministic, never via remote state) and each attach their own
target with `aws_s3_bucket_notification`, which — unlike CDK's `addEventNotification` — replaces
the bucket's entire notification configuration on every apply instead of merging with it.

All three roots share `modules/notification-target` (results SQS queue, lambda role, lambda
function, and the `s3.amazonaws.com` invoke permission).

## Apply order (matches the `integ/` terratest flow)

```sh
cd terraform/stack-a && mise x -- terraform init && mise x -- terraform apply -var suffix=k3m9x1
cd terraform/stack-b && mise x -- terraform init && mise x -- terraform apply -var suffix=k3m9x1
cd terraform/stack-c && mise x -- terraform init && mise x -- terraform apply -var suffix=k3m9x1

# re-apply A: merges nothing new, but demonstrates A's inline config is untouched by B/C
cd terraform/stack-a && mise x -- terraform apply -var suffix=k3m9x1

# expected RED point: `terraform plan` for stack-b (and stack-c) shows
# aws_s3_bucket_notification replacing the bucket's whole notification_configuration,
# dropping stack-a's (and, for C, stack-b's) lambda target instead of merging with it.
```

Destroy in reverse (empty the bucket first — `stack-a` owns it and does not enable
`force_destroy`; the terratest harness does this via the AWS SDK before destroying):

```sh
cd terraform/stack-c && mise x -- terraform destroy -var suffix=k3m9x1
cd terraform/stack-b && mise x -- terraform destroy -var suffix=k3m9x1
cd terraform/stack-a && mise x -- terraform destroy -var suffix=k3m9x1
```

`suffix` must match across all three roots and the awscdk suite for a given test run; `region`
defaults to `us-east-1` on every provider and can be overridden with `-var region=...`.

## Verify without AWS credentials

```sh
cd terraform/stack-a && mise x -- terraform init -backend=false && mise x -- terraform validate
cd terraform/stack-b && mise x -- terraform init -backend=false && mise x -- terraform validate
cd terraform/stack-c && mise x -- terraform init -backend=false && mise x -- terraform validate
cd terraform && mise x -- terraform fmt -check -recursive
```
