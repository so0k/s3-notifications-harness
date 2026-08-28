# Cross-suite contract (awscdk/, terraform/, integ/)

Region: `us-east-1`. Credentials: ambient env (`aws-vault exec --no-session tcons-vincent -- ...`). Never hardcode.

## Inputs (identical for both suites)
- `suffix` (string, lowercase, e.g. `k3m9x1`): unique per test run.
  - CDK: `-c suffix=<suffix>` context. Terraform: `-var suffix=<suffix>`.
- Stack B and C derive the bucket name deterministically, they never read Stack A's outputs:
  - bucket name = `s3n-harness-<suffix>`

## Per-stack naming (X in a|b|c, SUFFIX = suffix)
- Stack name: CDK `S3nHarness<A|B|C>-<suffix>`; Terraform root modules `terraform/<provider>/stack-a`, `stack-b`, `stack-c` for each of the two sibling scenarios, `provider` in `awscc` (`hashicorp/awscc` + `hashicorp/aws`), `aws` (`hashicorp/aws` only) — see `terraform/README.md` (each root has its own local state in its working dir, terratest copies each into a temp dir).
- Lambda function name: `s3n-harness-<suffix>-<x>` (env `RESULTS_QUEUE_URL`, `STACK_NAME=<x>`), runtime `nodejs22.x`, handler `index.handler`, source `lambda/index.js` (single file, CommonJS, no deps beyond runtime SDK).
- Lambda role: allows `sqs:SendMessage` on its results queue + `AWSLambdaBasicExecutionRole`.
- Lambda permission: principal `s3.amazonaws.com`, source arn = bucket arn, source account = current account.
- Results SQS queue: `s3n-harness-<suffix>-<x>-results` (standard queue).
- Notification: event `s3:ObjectCreated:*`, filter prefix `<x>/`, target = the stack's lambda.

## Outputs (every stack, both suites; canonical keys used by integ's `Suite` interface)
- `bucket_name`  – the shared bucket name
- `lambda_arn`   – this stack's lambda arn
- `queue_url`    – this stack's results queue URL
- `owner`        – `a` | `b` | `c`
Terraform: `output "bucket_name" {}` etc. — output ids match the canonical keys exactly.
CDK: CloudFormation Output *logical ids* must be alphanumeric (`[A-Za-z0-9]` only) — snake_case
ids are rejected at CreateStack/UpdateStack validation, so `CfnOutput` ids are the PascalCase
equivalents `BucketName`, `LambdaArn`, `QueueUrl`, `Owner` (exported via `cdk deploy
--outputs-file`). The integ Go `Suite` adapter for the CDK suite must translate these back to
the canonical snake_case keys before returning them to the shared assertion helpers:
  `BucketName -> bucket_name`, `LambdaArn -> lambda_arn`, `QueueUrl -> queue_url`, `Owner -> owner`.

## Stack A specifics
- Owns the bucket (`RemovalPolicy.DESTROY` + `autoDeleteObjects` in CDK; the terraform `awscc` scenario needs the terratest empty-bucket-before-destroy step, the terraform `aws` scenario sets `force_destroy = true` instead — harmless no-op empty step there and for CDK, see integ/harness_test.go's cleanup comment).
- CDK: `cdk.json` sets `"@aws-cdk/aws-s3:keepNotificationInImportedBucket": true` (required so the owning stack also merges instead of overwriting).
- Terraform (awscc): `awscc_s3_bucket` with inline `notification_configuration.lambda_configurations`.
- Terraform (aws): `aws_s3_bucket` (`force_destroy = true`) + its own `aws_s3_bucket_notification`, the same resource stack-b/c use.

## Stack B / C specifics
- CDK: `s3.Bucket.fromBucketName(this, 'Bucket', bucketName)` + `addEventNotification(...)`.
- Terraform: `data "aws_s3_bucket"` + `aws_s3_bucket_notification` (hashicorp/aws) with one `lambda_function` block; the target module is `awscc_*` for lambda/iam/sqs/permission in the awscc scenario, `aws_*` (+ `data "archive_file"` for the lambda zip) in the aws scenario.

## Provider/tool versions
- terraform 1.15.9 (mise), hashicorp/awscc `~> 1.98`, hashicorp/aws `~> 6.0`, hashicorp/archive (unpinned, used for the lambda zip in the `terraform/aws` scenario) (pin whatever latest resolves, commit lock files are ignored, use `required_providers`).
- aws-cdk-lib `2.267.0`, aws-cdk `2.1139.0`, node 24, TypeScript app via `npx tsx bin/app.ts` (`cdk.json` app: `npx tsx bin/app.ts`).

## integ/ (Go, terratest) test flow — same for both suites, parameterised by a `Suite` interface
```
deploy A      → assert config ⊇ {a};      upload a/1 → queue a receives
deploy B      → assert config ⊇ {a,b};    upload a/2,b/2 → queues a,b receive
deploy C      → assert config ⊇ {a,b,c};  upload a/3,b/3,c/3 → all receive
re-deploy A   → assert config ⊇ {a,b,c};  upload */4 → all receive
destroy B     → assert config ⊇ {a,c}, ∌ b
destroy C, A  (cleanup, deferred; always runs; empties bucket first)
```
Assertion helper: `GetBucketNotificationConfiguration` → set of LambdaFunctionArn present. Stage skipping via `test_structure.RunTestStage` + `SKIP_*` env.
Expected: `TestAwsCdk` GREEN, `TestTerraformAwscc` RED at "deploy B → config ⊇ {a,b}" (and the B plan output logged shows it replacing A's target); `TestTerraformAws` runs the same flow against the aws-only scenario (outcome is what the comparison is meant to answer — see harness_test.go's TestTerraformAws doc comment). The test must log the terraform plan for B and C before applying.

## Timing caveats
- S3 applies a new notification configuration eventually — AWS documents "about five minutes". `GetBucketNotificationConfiguration` is read-after-write consistent and is the assertion that proves merge semantics; delivery is only asserted after `waitForTargetLive` (≤6 min of warm-up probes) for the target the stage just deployed. A delivery-only failure is a harness timeout, not a merge failure.
- `awscc_*` IAM resources go through Cloud Control, which rejects `GetSessionToken` credentials for IAM; run with `aws-vault exec --no-session`.
