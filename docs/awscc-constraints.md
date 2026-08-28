# terraform-provider-awscc behaviours observed in this harness

Observed with awscc 1.98.0 / Terraform 1.15.9 against a real account. Each entry: what
happened, evidence, what the harness does. Entries tagged **polyfill** matter when building
CloudFormation-style custom resources on top of awscc-owned resources. Structural gaps
(things awscc simply does not have) live in [awscc-gaps.md](awscc-gaps.md).

## 1. Applies are slow

Every awscc resource waits on a Cloud Control operation. Same stack A (bucket, role,
lambda, permission, SQS queue), measured from the `deploy_a` stage start to `validate_a`
start in the logs:

| suite | stack A apply | full flow |
|---|---|---|
| terraform + hashicorp/aws | 1m40s | 23m44s |
| CloudFormation (awscdk) | 2m02s | 17m58s |
| terraform + awscc | 3m58s | 27m27s |
| terraform + awscc + cfncompat | 4m06s | 23m26s |
| cdktn + awscc + cfncompat | 3m40s | 21m33s |

Measure/reproduce: run any suite (`aws-vault exec --no-session tcons-vincent -- make -C integ
test-<target>`), then
`grep -E "executing stage '(deploy|validate)_a'" test-reports/<target>.log` and diff the two
timestamps; the total is on the `--- PASS/FAIL` line. Full-flow numbers also include the S3
propagation warm-up (up to ~3 min per newly attached target), so the stack A apply is the
cleaner provider comparison.

## 2. Cloud Control canonicalises casing — write CFN casing

`rules = [{ name = "prefix" }]` was read back as `name = "Prefix"`, producing a perpetual
in-place diff on every plan (`- name = "Prefix" -> null` / `+ name = "prefix"`). Use the
CloudFormation schema casing (`Prefix`/`Suffix`). S3's own API accepts either (the CDK
handler sends `Name: "prefix"`); the canonicalisation is awscc's read-back.

## 3. IAM documents must be rendered with `jsonencode`  **polyfill (cdktn)**

`assume_role_policy_document` / `policies[].policy_document` passed as `JSON.stringify`
strings from cdktn produced two `awscc_iam_role ... will be updated in-place` entries
flagged `jsonencode( # whitespace changes` on every re-plan. Rendering with `Fn.jsonencode`
round-trips cleanly; HCL roots using `jsonencode` never showed the diff.

## 4. Unset Optional+Computed sub-resource shows no drift  **polyfill**

With `notification_configuration` absent from `awscc_s3_bucket`, Cloud Control reads the
live, custom-resource-written configuration into state on refresh and Terraform plans
nothing — stack A's re-plan after B and C mutated the bucket:
`No changes. Your infrastructure matches the configuration.` (cfncompat and cdktn runs).
This is the property that lets a CFN-owned parent coexist with a custom resource mutating
one of its sub-resources, the same way CloudFormation never reads `NotificationConfiguration`
back. `lifecycle { ignore_changes = [notification_configuration] }` was not needed; it
remains the guard against a later update of another attribute sending
`NotificationConfiguration: null`.

## 5. Inline `notification_configuration` behaves exactly like `AWS::S3::Bucket`

Side note, expected: declaring the block makes the bucket resource the single writer, so an
out-of-band `PutBucketNotificationConfiguration` (stack B/C's `aws_s3_bucket_notification`)
is reverted on A's next apply:
```
~ function = "...:function:s3n-harness-quuipx-c" -> "...:function:s3n-harness-quuipx-a"
Plan: 0 to add, 1 to change, 0 to destroy.
```
The inline block also creates a bucket → lambda ARN → permission → bucket ARN cycle; the
awscc scenario breaks it with a literal `arn:aws:s3:::<name>` on the permission and
`depends_on` from the bucket to the permission.

## 6. Cloud Control returns bare `InternalFailure` on IAM role creation

`awscc_iam_role` failed once with `waiter state transitioned to FAILED. StatusMessage:
Internal Failure. ErrorCode: InternalFailure` while the identical resource had just succeeded
in another stack; the re-run converged (provider issues #338, #701 report the same). The
cdktn driver retries a deploy once on that string; the terraform driver relies on terratest's
default retry list, which does not include it.

## 7. `zip_file` inline code

`awscc_lambda_function.code.zip_file` accepts inline Node.js or Python source with handler
`index.handler` (single file, 4 MB). Both the sample target (`lambda/index.js`) and the CDK
notifications handler (`lambda/notifications-handler/index.py`, python3.12) ship this way —
no archive provider needed.

## 8. Account id without hashicorp/aws

`awscc_lambda_permission.source_account` needs the account id; awscc has no caller-identity
data source (see gaps). The harness derives it from an ARN it already owns:
`split(":", awscc_lambda_function.handler.arn)[4]`.
