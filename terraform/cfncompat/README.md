# terraform/cfncompat/

Same three-root-module shape as `../aws/` and `../awscc/`, and built on the same
`hashicorp/awscc` resources as `../awscc/`, but with no `aws_s3_bucket_notification` (or
inline `awscc_s3_bucket` notification config) anywhere. Instead, every stack — including
stack-a, which owns the bucket via an `awscc_s3_bucket` carrying **no**
`notification_configuration` block at all — attaches its own notification target purely
through `modules/bucket-notifications`' `cfncompat_custom_resource`, which drives AWS
CDK's own bucket-notifications Lambda handler (`../../lambda/notifications-handler/index.py`,
copied verbatim) in its merge ("unmanaged", `Managed = "false"`) mode: each apply GETs the
bucket's current notification configuration, merges in only that stack's own entry, and
PUTs the result back, instead of the whole-bucket-singleton replace `aws_s3_bucket_notification`
does. See `../../docs/OPTIONS.md` (Option A) and `../../CONTRACT.md`.

Because `awscc_s3_bucket.notification_configuration` is Optional+Computed, leaving it out of
stack-a's bucket entirely also makes this scenario the check that the configuration the custom
resource's handler writes **out of band** does not resurface as drift: a `plan` after any apply
must report no changes for the bucket.

Stack-b and stack-c never look the bucket up — awscc has no S3 bucket data source and none is
needed, since `../../CONTRACT.md`'s deterministic naming gives both the name
(`s3n-harness-<suffix>`) and the arn (`arn:aws:s3:::s3n-harness-<suffix>`) as plain strings.

All three roots share `modules/notification-target` (results SQS queue, lambda role, lambda
function, and the `s3.amazonaws.com` invoke permission, all `awscc_*`) by relative path from
`../awscc/modules/notification-target` — the same module `../awscc/` uses — plus this scenario's
own `modules/bucket-notifications` (response bucket, handler IAM role/lambda, and the
`cfncompat_custom_resource` itself). The handler lambda is an `awscc_lambda_function` with the
CDK handler inlined via `code.zip_file = file(...)` — no `archive_file`, no zip artifact.

### Where `hashicorp/aws` is still used

One place, deliberate:

- `aws_s3_bucket` **for the per-stack cfncompat response bucket only** (`modules/bucket-notifications`),
  because it needs `force_destroy = true` and `awscc_s3_bucket` has no equivalent. cfncompat
  deletes a response object only *best effort*, after it has successfully read and parsed it, so a
  failed or timed-out handler invocation can leave one behind; the terratest cleanup empties only
  the *shared* bucket, so an `awscc_s3_bucket` here could strand the root with `BucketNotEmpty` on
  destroy. This is orthogonal to what the scenario tests.

`../awscc/modules/notification-target` (reused here) needs no `hashicorp/aws`: the lambda
permission's `source_account` is derived from the lambda's own arn instead of a
`data "aws_caller_identity"` lookup. Since a module can't declare its own provider
configuration, each root here still carries a minimal `provider "aws"` block (see `versions.tf`)
purely so `modules/bucket-notifications`' response bucket has one to use.

## Apply order (matches the `integ/` terratest flow)

`--no-session` is required: the `awscc_*` IAM resources go through Cloud Control, which
rejects `GetSessionToken` credentials.

```sh
export AWSV="aws-vault exec --no-session tcons-vincent --"

cd terraform/cfncompat
$AWSV mise x -- terraform -chdir=stack-a init && $AWSV mise x -- terraform -chdir=stack-a apply -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-b init && $AWSV mise x -- terraform -chdir=stack-b apply -var suffix=k3m9x1
$AWSV mise x -- terraform -chdir=stack-c init && $AWSV mise x -- terraform -chdir=stack-c apply -var suffix=k3m9x1

# re-apply A: unlike ../aws/ and ../awscc/, this is expected to leave B/C's targets intact
$AWSV mise x -- terraform -chdir=stack-a apply -var suffix=k3m9x1
```

Destroy in reverse (empty the shared bucket first — `awscc_s3_bucket` has no `force_destroy`,
so, exactly as in `../awscc/`, the terratest harness empties it via the AWS SDK before
destroying stack-a; the per-stack cfncompat response buckets do set `force_destroy` and need
no such step):

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
