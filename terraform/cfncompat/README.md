# terraform/cfncompat/

Same three-root-module shape as `../aws/` and `../awscc/`, but with no
`aws_s3_bucket_notification` (or inline `awscc_s3_bucket` notification config) anywhere.
Instead, every stack — including stack-a, which owns the bucket via a plain
`aws_s3_bucket` (`force_destroy = true`) — attaches its own notification target purely
through `modules/bucket-notifications`' `cfncompat_custom_resource`, which drives AWS
CDK's own bucket-notifications Lambda handler (`../../lambda/notifications-handler/index.py`,
copied verbatim) in its merge ("unmanaged", `Managed = "false"`) mode: each apply GETs the
bucket's current notification configuration, merges in only that stack's own entry, and
PUTs the result back, instead of the whole-bucket-singleton replace `aws_s3_bucket_notification`
does. See `../../docs/OPTIONS.md` (Option A) and `../../CONTRACT.md`.

All three roots share `modules/notification-target` (results SQS queue, lambda role, lambda
function, and the `s3.amazonaws.com` invoke permission) by relative path from
`../aws/modules/notification-target` — the same module `../aws/` uses — plus this scenario's own
`modules/bucket-notifications` (response bucket, handler IAM role/lambda, and the
`cfncompat_custom_resource` itself).

## Apply order (matches the `integ/` terratest flow)

```sh
export AWSV="aws-vault exec --no-session tcons-vincent --"

cd terraform/cfncompat
$AWSV mise x -- terraform -chdir=stack-a init && $AWSV mise x -- terraform -chdir=stack-a apply -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-b init && $AWSV mise x -- terraform -chdir=stack-b apply -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-c init && $AWSV mise x -- terraform -chdir=stack-c apply -var suffix=k3m9x1

# re-apply A: unlike ../aws/ and ../awscc/, this is expected to leave B/C's targets intact
$AWSV mise x -- terraform -chdir=stack-a apply -var suffix=k3m9x1
```

Destroy in reverse (empty the bucket first — `stack-a`'s `force_destroy = true` only empties the
bucket on its own destroy, not on B/C's):

```sh
$AWSV mise x -- terraform -chdir=stack-c destroy -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-b destroy -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-a destroy -var suffix=k3m9x1
```

`suffix` must match across all three roots, the `../aws` and `../awscc` scenarios, and the
awscdk suite for a given test run; `region` defaults to `us-east-1` and can be overridden with
`-var region=...`. See `../../CONTRACT.md` for the full cross-suite contract.

## Verify without AWS credentials

```sh
cd terraform/cfncompat
for x in a b c; do
  mise x -- terraform -chdir=stack-$x init -backend=false && mise x -- terraform -chdir=stack-$x validate
done
mise x -- terraform fmt -check -recursive
```
