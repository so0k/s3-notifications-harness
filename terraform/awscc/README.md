# terraform/awscc/

Three independent root modules that reproduce the "single authoritative S3 bucket notification
configuration" problem: `stack-a` owns the bucket (`awscc_s3_bucket` with inline
`notification_configuration.lambda_configurations`) and its own target; `stack-b` and `stack-c`
reference the bucket by name (deterministic, never via remote state) and each attach their own
target with `aws_s3_bucket_notification` (`hashicorp/aws`), which — unlike CDK's
`addEventNotification` — replaces the bucket's entire notification configuration on every apply
instead of merging with it.

All three roots share `modules/notification-target` (results SQS queue, lambda role, lambda
function, and the `s3.amazonaws.com` invoke permission), built entirely from `awscc_*` resources.
`../cfncompat/`'s roots use this same module by relative path, so changes here affect that
scenario too.

## Apply order (matches the `integ/` terratest flow)

`--no-session` is required: the `awscc_*` IAM resources go through Cloud Control, which
rejects `GetSessionToken` credentials.

```sh
export AWSV="aws-vault exec --no-session tcons-vincent --"

cd terraform/awscc
$AWSV mise x -- terraform -chdir=stack-a init && $AWSV mise x -- terraform -chdir=stack-a apply -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-b init && $AWSV mise x -- terraform -chdir=stack-b apply -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-c init && $AWSV mise x -- terraform -chdir=stack-c apply -var suffix=k3m9x1

# re-apply A: merges nothing new, but demonstrates A's inline config is untouched by B/C
$AWSV mise x -- terraform -chdir=stack-a apply -var suffix=k3m9x1

# expected RED point: `terraform plan` for stack-b (and stack-c) shows
# aws_s3_bucket_notification replacing the bucket's whole notification_configuration,
# dropping stack-a's (and, for C, stack-b's) lambda target instead of merging with it.
```

Destroy in reverse (empty the bucket first — `stack-a` owns it and does not enable
`force_destroy`; the terratest harness does this via the AWS SDK before destroying):

```sh
$AWSV mise x -- terraform -chdir=stack-c destroy -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-b destroy -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-a destroy -var suffix=k3m9x1
```

`suffix` must match across all three roots, the `../aws` and `../cfncompat` scenarios, and the
awscdk suite for a given test run; `region` defaults to `us-east-1` on every provider and can be overridden with
`-var region=...`. See `../../CONTRACT.md` for the full cross-suite contract.

## Verify without AWS credentials

```sh
cd terraform/awscc
for x in a b c; do
  mise x -- terraform -chdir=stack-$x init -backend=false && mise x -- terraform -chdir=stack-$x validate
done
mise x -- terraform fmt -check -recursive
```
