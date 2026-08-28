# terraform-provider-awscc constraints observed in this harness

Everything below was hit while running the five suites against a real account
(2026-08-28, awscc 1.98.0, Terraform 1.15.9). Each entry: what happened, the evidence, and
what the harness does about it. Items marked **polyfill** matter for building CFN-style
custom resources on top of awscc-owned resources.

## 1. Cloud Control rejects `GetSessionToken` credentials for IAM

`awscc_iam_role` create failed with
`waiter state transitioned to FAILED. StatusMessage: The security token included in the
request is invalid (Service: Iam, Status Code: 403)` — `ErrorCode: AccessDenied`.
`aws-vault exec <profile>` defaults to `sts:GetSessionToken`; IAM (and Cloud Control's
IAM handler) refuse those temporary credentials. Refresh then fails the same way
(`GetResource ... AccessDeniedException: AWS::IAM::Role Handler returned status FAILED`),
so a half-applied root cannot even be destroyed with the same credentials.
Harness: every command runs under `aws-vault exec --no-session`.

## 2. Cloud Control returns bare `InternalFailure` on IAM role creation

`awscc_iam_role` (stack B target role) failed once with
`waiter state transitioned to FAILED. StatusMessage: Internal Failure. ErrorCode:
InternalFailure` while the identical resource in stack A had just succeeded; the re-run
converged. Harness: the cdktn driver retries a deploy once when the output contains
`ErrorCode: InternalFailure`; the plain-terraform driver relies on terratest's default
retryable-error list (does not cover this string — add it if it recurs).

## 3. No `force_destroy` on `awscc_s3_bucket`

`AWS::S3::Bucket` has no force-delete property, so awscc cannot empty a bucket on destroy.
Harness: terratest empties the shared bucket (all versions + delete markers) before
destroying stack A; the cfncompat response bucket stays `aws_s3_bucket { force_destroy }`
because cfncompat deletes response objects only best-effort. **polyfill**: any construct that
lets a custom resource write objects into an awscc-owned bucket must own the emptying.

## 4. No data source to look up an existing bucket

awscc has no `data "awscc_s3_bucket"`. Stacks B/C either use `data "aws_s3_bucket"`
(hashicorp/aws) or, as the cfncompat/cdktn scenarios do, compute the name/ARN from the
deterministic naming contract and pass plain strings.

## 5. Inline `notification_configuration` is authoritative and cycles with the target

Declaring `notification_configuration` on `awscc_s3_bucket` makes the bucket resource the
single writer: any out-of-band `PutBucketNotificationConfiguration` (stack B/C's
`aws_s3_bucket_notification`) is reverted on A's next apply. Evidence, A's re-plan:
```
~ function = "...:function:s3n-harness-quuipx-c" -> "...:function:s3n-harness-quuipx-a"
Plan: 0 to add, 1 to change, 0 to destroy.
```
The inline block also creates a dependency cycle (bucket → lambda ARN, lambda permission →
bucket ARN); the awscc scenario breaks it by giving the permission a literal
`arn:aws:s3:::<name>` and making the bucket `depends_on` the permission.

## 6. Unset Optional+Computed sub-resource shows no drift  **polyfill**

With `notification_configuration` absent from config, Cloud Control reads the live
(custom-resource-written) configuration into state on refresh and Terraform plans nothing:
A's re-plan after B and C mutated the bucket was `No changes. Your infrastructure matches
the configuration.` (cfncompat and cdktn runs). This is the property that lets a CFN-owned
parent coexist with a custom resource mutating one of its sub-resources.
`lifecycle { ignore_changes = [notification_configuration] }` is not required for this case
and remains the guard against a future update of another attribute sending
`NotificationConfiguration: null`.

## 7. Cloud Control canonicalises casing — write CFN casing

`rules = [{ name = "prefix" }]` read back as `name = "Prefix"`, producing a perpetual
in-place diff (`- name = "Prefix" -> null` / `+ name = "prefix"`). Write the CloudFormation
schema casing (`Prefix`/`Suffix`). Contrast: the CDK custom-resource handler passes
`Name: "prefix"` straight to the S3 API, which accepts either — the canonicalisation is a
read-back property of awscc, not of S3.

## 8. IAM documents must be rendered with `jsonencode`  **polyfill (cdktn)**

`assume_role_policy_document` / `policies[].policy_document` given as `JSON.stringify`
strings from cdktn produced two `awscc_iam_role ... will be updated in-place` entries
flagged `jsonencode( # whitespace changes` on every re-plan. Rendering them with
`Fn.jsonencode` (Terraform `jsonencode`) round-trips cleanly; HCL scenarios using
`jsonencode` never showed the diff.

## 9. `zip_file` inline code

`awscc_lambda_function.code.zip_file` accepts inline Node.js/Python source with handler
`index.handler`; both the sample target (`lambda/index.js`) and the CDK notifications
handler (`lambda/notifications-handler/index.py`, python3.12) ship this way, no archive
provider needed. Limit is 4 MB and single file.

## 10. Slow applies

Every awscc resource waits on a Cloud Control operation; the awscc/cfncompat roots take
~25 min for the full flow versus ~14 min for CloudFormation and ~18 min for the aws-only
scenario.

## 11. Still needs hashicorp/aws for `aws_caller_identity`

There is no awscc data source for the account id (used for the Lambda permission's
`source_account`), so every awscc root also configures hashicorp/aws.
