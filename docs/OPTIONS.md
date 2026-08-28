# Options: making cross-stack S3 notifications work with cfncompat

Goal: turn the RED Terraform scenarios GREEN by polyfilling CloudFormation's
`Custom::S3BucketNotifications` with `cfncompat_custom_resource` driving the AWS CDK
handler (`index.py`, copied verbatim), then validate it with the same terratest flow.

Evidence behind the choice: cfncompat 0.2.0 implements the full CFN custom-resource
protocol (presigned `ResponseURL`, `StackId`, `OldResourceProperties`, Delete, replacement)
and the CDK handler needs nothing it doesn't provide (`Managed` must be the string
`"false"`); this harness is that protocol engine's only exercise against real AWS.
`@cdktn/provider-cfncompat@1.0.0` (peer `cdktn ^0.24`) exports `CustomResource` + provider
functions and pins provider 0.2.0.
TerraConstructs (cdktn 0.23, `@cdktn/provider-aws`) has a single authoritative
`aws_s3_bucket_notification` and a TODO for the custom-resource approach.

## Status

| Option | State |
|---|---|
| A — `terraform/cfncompat/` | built; `TestTerraformCfncompat` GREEN |
| B — `cdktn/` app | built; `TestCdktn` GREEN |
| C — TerraConstructs L2 | outstanding; the remaining work, now unblocked by A and B |
| D — native Go merge resource | rejected |

## Option A — HCL scenario `terraform/cfncompat/` (spike first)
Fourth sibling next to `awscc/` and `aws/`, built on the same `hashicorp/awscc` resources as
`awscc/`: each stack deploys the CDK python handler (awscc_lambda_function python3.12, source
inlined via `code.zip_file = file(...)`) + role (Get/PutBucketNotification) +
`cfncompat_custom_resource { service_token, resource_properties = { BucketName,
NotificationConfiguration, Managed = "false" }, stack_id = "<stack>" }`, one response bucket
per stack (better for destroy isolation than one shared per run -- see
`terraform/cfncompat/README.md`). Terratest gets a `TestTerraformCfncompat` target; expected GREEN.
- Pros: smallest change; first-ever real-AWS proof of cfncompat's protocol engine;
  isolates provider bugs from construct/tooling bugs; directly reusable as a cfncompat
  integ fixture.
- Cons: no constructs, so it says nothing about the cdktn binding/asset path; HCL boilerplate
  (handler source, role, response bucket) is repeated per stack.

## Option B — cdktn TypeScript app using `@cdktn/provider-cfncompat`
`cdktn/` sibling app (cdktn 0.24, `@cdktn/provider-aws` or `-awscc`, `@cdktn/provider-cfncompat`
`CustomResource`, `TerraformAsset` for the handler zip), three stacks, wrapped in a small
reusable construct (`BucketNotificationsPolyfill`). Terratest drives `cdktn deploy`.
- Pros: exercises the product path users will take (binding, provider functions, assets,
  cdktn CLI); the construct is the seed of the eventual L2.
- Cons: more scaffolding (cdktn.json/cdktf.json, .gen, esbuild); a failure could be in the
  provider, the binding, or the CLI — harder to attribute without Option A first.

## Option C — TerraConstructs L2 (`Bucket.addEventNotification` backed by cfncompat)
Resolve the TODO in `~/tcons/base/src/aws/storage/bucket.ts`: new `BucketNotifications`
implementation using `@cdktn/provider-cfncompat` (new peer dep in `.projenrc.ts`), bundling
the handler via `Code.fromInline` (python) or an asset.
- Pros: real user-facing API parity with AWS CDK; imported buckets get notifications.
- Cons: largest scope (jsii, peer-dep/publish plumbing, tcons is on cdktn 0.23 vs binding peer
  ^0.24, provider-aws only); mixes library work into a validation harness.

## Option D — native Go merge resource in cfncompat (no Lambda)
`cfncompat_s3_bucket_notification_target` implemented in the provider: Get → merge own
entry (Id-prefixed) → Put, with Terraform state CRUD.
- Pros: no Lambda/response bucket; fastest apply; deterministic.
- Cons: diverges from cfncompat's purpose (polyfill the CFN deployment model, not
  re-implement each custom resource); does nothing for the hundreds of other CDK handlers.

## Ordering rationale
A before B: A proves the protocol engine on real AWS as the fourth terratest target, B then
ports the same three stacks to cdktn + the binding as the fifth, so any regression between
them is attributable to a single layer. C builds on both; D is rejected outright.
