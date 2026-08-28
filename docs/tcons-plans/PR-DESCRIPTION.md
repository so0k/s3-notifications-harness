# feat(aws): CloudFormation-compatible custom resources, and bucket notifications on imported / shared buckets

## Summary

Adds a CloudFormation-shaped custom-resource primitive to TerraConstructs, backed by the
`cdktn-io/cfncompat` Terraform provider, and uses it to lift the long-standing restriction
that S3 bucket notifications can only be managed by the stack that owns the bucket.

Two commits, on top of `origin/main`:

1. **`feat(aws): CustomResource and CustomResourceHandler on @cdktn/provider-cfncompat`** —
   `aws.CustomResource` (a port of `aws-cdk-lib/core/lib/custom-resource.ts`, wrapping
   `cfncompat_custom_resource`) and `aws.CustomResourceHandler` (a stack-singleton Lambda
   backing one or more custom resources). `AwsStack` grows a lazy `cfncompatProvider`
   singleton, an optional `cfncompatProviderConfig` prop, and a lazy per-stack
   `customResourceResponseBucket`.
2. **`feat(storage): custom-resource bucket notifications for imported and shared buckets`** —
   `NotificationsResourceHandler` (AWS CDK's Python handler, verbatim) and
   `BucketNotificationsResource` (`Custom::S3BucketNotifications`, `Managed: "false"`).
   `BucketBase.addEventNotification` / `enableEventBridgeNotification` now pick between the
   native `aws_s3_bucket_notification` resource and the custom resource.

Imported buckets (`Bucket.fromBucketName` and friends) always use the custom resource — it is
the only way to attach a notification to a bucket the stack does not own. Owned buckets keep
the native resource unless the `@terraconstructs/aws-s3:keepNotificationInImportedBucket`
context key is set, matching AWS CDK's feature-flag name.

Docs: new `src/aws/storage/README.md` ("Notifications on imported / shared buckets"), and the
new integ target documented in `integ/aws/storage/README.md`.

## Design decisions

- **Merge, never overwrite.** The custom resource always runs the handler unmanaged
  (`Managed: "false"`). On every apply the handler reads the bucket's existing notification
  configuration, keeps everything it does not recognise, and merges in only this stack's own
  entries, identified by an `Id` prefixed with the stack id. That is what lets several stacks
  — the owning stack included — attach notifications to one bucket.
- **`stackId` = `gridUUID`.** The handler distinguishes its own entries by `StackId` prefix, so
  the value must be stable across applies. `gridUUID` is; a construct path is not.
- **Context key, not a `FeatureFlags` port.** Read with `node.tryGetContext`; jsii has no
  exported constants, so the literal key string is the public API and
  `S3_KEEP_NOTIFICATION_IN_IMPORTED_BUCKET` in `cx-api.ts` stays internal (same treatment as
  `TARGET_PARTITIONS`).
- **Migration hazard, documented not automated.** Flipping the key on an already-deployed
  owned bucket destroys the native resource — which wipes the bucket's whole notification
  configuration — unordered against the custom resource's Put. Called out in
  `addEventNotification`'s JSDoc, in `cx-api.ts`, and in the storage README.
- **Response transport.** `cfncompat_custom_resource` delivers handler responses through a
  pre-signed S3 URL. Each stack lazily creates one `CustomResourceResponsesBucket` with
  `force_destroy` (response objects are written at apply time and are not tracked in state, so
  destroy would otherwise fail on a non-empty bucket). Setting
  `cfncompatProviderConfig.customResourceBucket` opts out and defers to the provider's bucket.
- **Lazy provider and bucket.** Stacks that never create a custom resource never synthesize a
  `cfncompat` provider block or a response bucket. `AwsStack` requires `storage/bucket` lazily
  to break the `aws-stack` ↔ `bucket` import cycle.
- **Handler source inlined.** Upstream loads the Python handler from an aws-cdk-lib build
  asset; that pipeline does not exist here, so the source is inlined verbatim (precedent:
  `compute/ecs/drain-hook`). Upstream's comment-stripping is dropped — it only exists to fit
  CloudFormation's 4 KiB inline `ZipFile` limit, and `Code.fromInline` renders a
  `data.archive_file`.
- **No async provider framework.** No `onEvent`/`isComplete` state machine: Terraform applies
  are synchronous, so a plain Lambda invoked by `cfncompat_custom_resource` suffices.

## Testing evidence

Unit (`pnpm exec jest test/aws/storage test/aws/custom-resource`): **18 suites, 375 passed, 2
skipped, 0 failed**. New: `test/aws/custom-resource.test.ts` (13),
`test/aws/custom-resource-handler.test.ts` (6),
`test/aws/storage/notification-custom-resource.test.ts` (15), plus imported-bucket cases in
`test/aws/storage/bucket.test.ts`.

Also green: `pnpm compile`, `pnpm exec eslint` on every touched file, `pnpm exec projen` (no
drift). The repo sets `docgen: false` and ships no `API.md`, so there is no generated
reference to regenerate.

**Live AWS run — `TestTcons` PASS (1195s)**, account 694710432912 / us-east-1, using
`terraconstructs@0.0.0.jsii.tgz` built from this branch. Three stacks share one bucket; A owns
it, B and C import it by name:

| Stage | Result |
|---|---|
| deploy A → `{a}` | PASS |
| deploy B → `{a,b}` | PASS (b live after 7 warm-up probes) |
| deploy C → `{a,b,c}` | PASS |
| re-deploy A → `{a,b,c}` | PASS — `No changes. Your infrastructure matches the configuration.` |
| destroy B → `{a,c}` | PASS |
| cleanup | PASS |

Re-applying the owning stack leaving B and C's entries intact is the property the whole change
exists for.

**OpenTofu registry limitation.** The in-repo integ target
`make bucket-notifications-cross-stack` (three stacks, entry-set assertions after every stage
plus an end-to-end delivery check per owner) could **not** be run. Terratest invokes `tofu`
(hardcoded in `integ/aws/util.go`) and `registry.opentofu.org` does not serve
`cdktn-io/cfncompat`, so init fails with `Failed to query available provider packages`. The
scenario was therefore validated by the equivalent standalone harness run above. Running the
in-repo target today requires a `filesystem_mirror` for `cdktn-io/cfncompat` in the OpenTofu
CLI configuration, or pointing terratest at a `terraform` binary; this is documented in
`integ/aws/storage/README.md`.

## Follow-ups

- Publish `cdktn-io/cfncompat` to the OpenTofu registry (or make the integ helpers' terraform
  binary configurable) so `bucket-notifications-cross-stack` runs in CI.
- The imported-bucket handler-role tests upstream depends on machinery not ported here
  (recorded as `not ported:` lines in `notification-custom-resource.test.ts`).
- Snapshot cases are omitted, matching the sibling `test/aws/storage/notification.test.ts`,
  where they are commented out.
- Other constructs that need out-of-stack mutation (bucket policies on imported buckets, for
  example) can now reuse `aws.CustomResource`.
