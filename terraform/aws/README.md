# terraform/aws/

Same three-root-module shape as `../awscc/`, but built entirely from `hashicorp/aws` (~> 6.0)
resources — no `awscc` provider anywhere. `stack-a` owns the bucket (`aws_s3_bucket`,
`force_destroy = true`) and attaches its own target with `aws_s3_bucket_notification`; `stack-b`
and `stack-c` reference the bucket by name (deterministic, never via remote state) and attach
their own targets with the very same `aws_s3_bucket_notification` resource. Because stack-a's
target uses that resource too here (unlike `../awscc/`, where stack-a's target is inline on the
bucket), this scenario isolates whether the RED behavior is inherent to
`aws_s3_bucket_notification` being independently authoritative, regardless of what owns the
bucket.

All three roots share `modules/notification-target` (results SQS queue, lambda role, lambda
function built from a `data "archive_file"` zip of `../../../../lambda/index.js`, and the
`s3.amazonaws.com` invoke permission), built entirely from `aws_*` resources.

## Apply order (matches the `integ/` terratest flow)

```sh
export AWSV="aws-vault exec --no-session tcons-vincent --"

cd terraform/aws
$AWSV mise x -- terraform -chdir=stack-a init && $AWSV mise x -- terraform -chdir=stack-a apply -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-b init && $AWSV mise x -- terraform -chdir=stack-b apply -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-c init && $AWSV mise x -- terraform -chdir=stack-c apply -var suffix=k3m9x1

# re-apply A: watch whether this drops B/C's targets too
$AWSV mise x -- terraform -chdir=stack-a apply -var suffix=k3m9x1
```

Destroy in reverse (empty the bucket first — `stack-a`'s `force_destroy = true` only empties the
bucket on its own destroy, not on B/C's):

```sh
$AWSV mise x -- terraform -chdir=stack-c destroy -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-b destroy -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-a destroy -var suffix=k3m9x1
```

`suffix` must match across all three roots, the `../awscc` and `../cfncompat` scenarios, and the
awscdk suite for a given test run; `region` defaults to `us-east-1` and can be overridden with `-var region=...`.
See `../../CONTRACT.md` for the full cross-suite contract.

## Verify without AWS credentials

```sh
cd terraform/aws
for x in a b c; do
  mise x -- terraform -chdir=stack-$x init -backend=false && mise x -- terraform -chdir=stack-$x validate
done
mise x -- terraform fmt -check -recursive
```
