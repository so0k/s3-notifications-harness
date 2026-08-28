# Plan — cfncompat bucket notifications (CDK `Custom::S3BucketNotifications` port)

Branch `feat/cfncompat-custom-resource` (worktree `~/tcons/base-cfncompat`), the S3 layer on top of
HEAD's `src/aws/custom-resource.ts`, `custom-resource-handler.ts`, `AwsStack.cfncompatProvider` and
`AwsStack.customResourceResponseBucket`. Spec: `~/cdktn/s3-notifications-harness/docs/OPTION-C-PLAN.md`
("Decisions taken"). Upstream: aws-cdk **v2.233.0** — read it with `git show v2.233.0:<path>` in
`~/cdk/aws-cdk` (the checkout is v2.263.0, whose notifications-resource has diverged: Box lazies,
per-bucket `iam.Policy` child, `lit`-tagged errors). GREEN-on-AWS references:
`~/cdktn/s3-notifications-harness/{terraform/cfncompat/modules/bucket-notifications/main.tf,cdktn/lib/bucket-notifications-polyfill.ts}`.

## Files

New: `src/aws/storage/notifications-resource-handler.ts` (singleton + verbatim Python),
`src/aws/storage/bucket-notifications-resource.ts` (`BucketNotificationsResource`),
`test/aws/storage/notification-custom-resource.test.ts`,
`integ/aws/storage/apps/bucket-notifications-cross-stack.ts`.
Edited: `src/aws/storage/bucket.ts` (selection, un-comment `notificationsHandlerRole`),
`src/aws/cx-api.ts` (context key), `src/aws/storage/index.ts` (exports — see jsii note 3),
`test/aws/storage/bucket.test.ts` (selection cases; `notification.test.ts` native cases unchanged),
`integ/aws/storage/{storage_test.go,Makefile}` + `integ/aws/util.go` (terratest hook + multi-stack synth).

## 1. `notifications-resource-handler.ts`

Provenance header → `.../v2.233.0/packages/aws-cdk-lib/aws-s3/lib/notifications-resource/notifications-resource-handler.ts`. Surface: `NotificationsResourceHandlerProps extends AwsConstructProps { readonly role?: iam.IRole }`;
`class NotificationsResourceHandler extends AwsConstructBase` with `static singleton(context, props?)`,
`readonly handler: CustomResourceHandler`, getters `functionArn`/`role`, `addToRolePolicy(statement)`,
and `get outputs()` → `{ arn: this.functionArn }` (bare keys, conventions.md).

- `singleton()` copies upstream: `AwsStack.ofAwsConstruct(context).node.tryFindChild(id)` else
  `new NotificationsResourceHandler(stack, id, props)` under the verbatim upstream logical id
  `BucketNotificationsHandler050a0587b7544547bf325f094a3db834`; the constructor creates
  `new CustomResourceHandler(this, "Handler", …)` (not `getOrCreate` — singleton-ness lives one level up)
  with `code: compute.Code.fromInline(HANDLER_SOURCE)`, `runtime: compute.Runtime.PYTHON_3_12`,
  `timeout: Duration.seconds(300)`, `handler: "index.handler"`, `role: props.role`, `description: 'AWS
  CloudFormation handler for "Custom::S3BucketNotifications" resources (@aws-cdk/aws-s3)'`.
  `CustomResourceHandler` already adds a role + `AWSLambdaBasicExecutionRole` when none is passed.
  Runtime is pinned (no `determineLatestPythonRuntime` port); PYTHON_3_12 is what the GREEN harness ran.
- **Handler source**: `git show v2.233.0:packages/@aws-cdk/custom-resource-handlers/lib/aws-s3/notifications-resource-handler/index.py`
  copied verbatim into a module-level `const HANDLER_SOURCE = \`…\`` (precedent:
  `src/aws/compute/ecs/drain-hook/instance-drain-hook.ts`); verified safe in a template literal — the
  file has no backtick, no `${`, no backslash. Do **not** apply upstream's comment-stripping regex (it
  exists only for the 4 KiB CFN `ZipFile` limit); byte-identical keeps upstream diffs trivial. Not a
  `.py` asset — projen packages only `lib/**/*.js|d.ts`. `Code.fromInline` + a python runtime renders a
  `data.archive_file` with `source_content_filename = index.py`
  (`src/aws/compute/code.ts:getInlineCodeFileExtension`) — no asset staging.

## 2. `bucket-notifications-resource.ts`

Provenance header → `.../v2.233.0/packages/aws-cdk-lib/aws-s3/lib/notifications-resource/notifications-resource.ts`.
`BucketNotificationsResourceProps extends AwsConstructProps { readonly bucket: IBucket; readonly
handlerRole?: iam.IRole; readonly skipDestinationValidation?: boolean /* default false */ }`.
`class BucketNotificationsResource extends AwsConstructBase` exposes `addNotification(event, target,
...filters)`, `enableEventBridgeNotification()`, `toTerraform()`, `get outputs()` (`{}` until the
resource exists, then `{ physicalResourceId }`) — today's `BucketNotifications` surface, so
`withNotifications` can hold either. Private accumulators hold the upstream **PascalCase** shapes
(`{ Events, Filter?, LambdaFunctionArn | QueueArn | TopicArn }`) plus `eventBridgeEnabled`. Port
`renderFilters()` verbatim **including** the three validations the native impl has commented out
(`must specify prefix and/or suffix`, multiple suffix, multiple prefix) — the CFN-shaped API enforces
them here. Errors: `ValidationError` from `src/errors.ts`.

`createResourceOnce()`:

1. `const handler = NotificationsResourceHandler.singleton(this, { role: this.handlerRole })`.
2. Per-bucket policy, v2.233 shape (`handler.addToRolePolicy`, **not** the v2.263 `iam.Policy` child):
   `s3:PutBucketNotification` and — since `managed` is always false here — `s3:GetBucketNotification`,
   both on `bucket.bucketArn`. Then `this.resource.node.addDependency(handler)` so the role policy
   exists before the custom resource is invoked (harness: `depends_on = [awscc_iam_role.handler]`).
3. `new CustomResource(this, "Resource", { serviceToken: handler.functionArn, resourceType:
   "Custom::S3BucketNotifications", serviceTimeout: Duration.seconds(300), properties: { BucketName:
   this.bucket.bucketName, NotificationConfiguration: Lazy.anyValue({ produce: () =>
   this.renderNotificationConfiguration() }), Managed: "false" } })`.
   - `Managed` is the **string** `"false"`, never a bool: the handler does
     `props.get('Managed','true').lower() == 'true'`. Same for `SkipDestinationValidation`; render it
     only when `true` (handler default `'false'`), keeping the harness-proven shape byte-for-byte.
   - Never pass `ServiceToken`/`ServiceTimeout` inside `properties` — the provider merges the service
     token itself and `CustomResource` warns on the collision.
   - `stackId`/`logicalResourceId`/`responseBucket` come from `CustomResource` (`stack.gridUUID`,
     `stack.uniqueResourceName(this)`, `stack.customResourceResponseBucket`). `gridUUID` is stable per
     stack and is the id prefix `handle_unmanaged` uses to separate our entries from external ones.
   - `serviceTimeout` matches the handler timeout (harness-proven); upstream has no equivalent.
4. `renderNotificationConfiguration()` verbatim: `{ EventBridgeConfiguration: enabled ? {} : undefined,
   LambdaFunctionConfigurations: len>0 ? [...] : undefined, QueueConfigurations, TopicConfigurations }`.
   CDK sends **no** `Id` — the handler assigns `f"{stack_id}-{hash(...)}"`; inventing ids breaks
   merge/delete. Wrap in `Lazy.anyValue` (cdktn spelling) since notifications arrive post-construction.
5. `addNotification`: one `target.bind()` per call (upstream shape — the native impl binds inside the
   filter loop), push `{ ...commonConfig, XxxArn: targetProps.arn }`, and
   `resource.node.addDependency(...targetProps.dependencies)` (e.g. the destination's
   `aws_lambda_permission` / topic policy) so destinations exist before the handler PUTs.
6. Bucket-policy ordering: upstream uses an Aspect; keep the repo-wide prepare-time hook instead —
   today's `toTerraform()` body (`if (this.bucket.policy) this.node.addDependency(this.bucket.policy);
   return {};`). EventBridge: `enableEventBridgeNotification()` creates the resource and sets the flag
   → `EventBridgeConfiguration: {}`; the handler preserves any pre-existing one as external.

## 3. Selection in `BucketBase.withNotifications`

In `src/aws/cx-api.ts`, next to `TARGET_PARTITIONS`: `export const
S3_KEEP_NOTIFICATION_IN_IMPORTED_BUCKET = "@terraconstructs/aws-s3:keepNotificationInImportedBucket";`.
`BucketBase.notifications` widens to `BucketNotifications | BucketNotificationsResource`.

```ts
private withNotifications(cb: (n: BucketNotifications | BucketNotificationsResource) => void) {
  if (!this.notifications) {
    const keep = !!this.node.tryGetContext(cxapi.S3_KEEP_NOTIFICATION_IN_IMPORTED_BUCKET);
    this.notifications = (keep || !(this instanceof Bucket))
      ? new BucketNotificationsResource(this, "Notifications", {
          bucket: this, handlerRole: this.notificationsHandlerRole,
          skipDestinationValidation: this.notificationsSkipDestinationValidation })
      : new BucketNotifications(this, "Notifications", { bucket: this });
  }
  cb(this.notifications);
}
```

- `this instanceof Bucket` mirrors upstream; `Import` extends `BucketBase` only, so imported buckets
  always take the custom resource even with the key off. `tryGetContext` on the bucket node reaches
  app/stack context (tcons idiom, no `FeatureFlags` port); truthy check, so `"true"` from
  `cdk.tf.json` context works as well as `true`.
- Construct id stays `"Notifications"` in both branches — no snapshot churn on the native path.
  Un-comment `notificationsHandlerRole` on `BucketAttributes` + `Import`, drop the three
  `// TODO: Use Custom Resource …` comments in `bucket.ts` (~lines 123, 989, 1002).
- README note: switching an existing **owned** bucket from native to custom resource is a migration
  step — the native destroy wipes the whole configuration, unordered against the custom resource's Put.

## 4. Unit tests

New `test/aws/storage/notification-custom-resource.test.ts`, `describe("notification custom resource")`,
`beforeEach` as in `notification.test.ts` but **no `HttpBackend`** (conventions.md: the local backend
embeds a machine-dependent tfstate path in snapshots) — assert via `Template` helpers. Ported
**verbatim** from `v2.233.0:packages/aws-cdk-lib/aws-s3/test/notification.test.ts`:

- `when notification is added a custom s3 bucket notification resource is provisioned` (key on),
  `can specify prefix and suffix filter rules`, `EventBridge notification custom resource`
- `can specify a custom role for the notifications handler of imported buckets`
- `throws with multiple prefix rules in a filter`, `throws with multiple suffix rules in a filter`
  (the native file has `does not throw …` variants; the custom-resource path *does* throw)
- `the notification lambda handler must depend on the role to prevent executing too early`, and the
  three bucket-policy cases `custom resource must not depend on bucket policy if it bucket policy does
  not exists` / `… must depend on bucket policy to prevent executing too early` / `… must depend on
  bucket policy even if bucket policy is added after notification`
- `Notification custom resource uses always treat bucket as unmanaged` — assert `Managed: "false"`
  (the string) and Put+Get statements scoped to the bucket ARN
- `check notifications handler runtime version` — `python3.12` (tcons pin ≠ upstream `python3.13`);
  tcons-only: `only one handler is created for multiple buckets in the same stack` (singleton)

Skipped, each with a `// not ported: <reason>` line: the six `Role`/`IRole` managed-policy-warning
cases (`add service-role permission if no Roles are provided`, `no warnings are shown …`, `service-role
permission are not added if IRole is provided`, `warning is thrown when IRole is provided and not
policies are added`, `If Role is provided, PutBucketNotification, GetBucketNotification will be added
along with service-role/AWSLambdaBasicExecutionRole`, `If Role is provided, No warnings are thrown`) —
machinery tcons' `iam.Role` + `CustomResourceHandler` do not reproduce; the three `skip destination
validation is set to …` cases → one tcons case `skip destination validation is only rendered when
enabled`; `multiple buckets in same stack result in consolidated policy with all bucket ARNs`
(v2.263-only `iam.Policy` child).

In `test/aws/storage/bucket.test.ts`: upstream-named `Event Bridge notification can be enabled after
the bucket is created`, plus tcons-named `addEventNotification on an imported bucket uses the custom
resource` / `… on an owned bucket uses aws_s3_bucket_notification by default` / `… on an owned bucket
uses the custom resource when the keepNotificationInImportedBucket context key is set`.
Loop: `pnpm compile && pnpm exec jest test/aws/storage`, then `pnpm eslint <files>`.

## 5. jsii pitfalls

1. `NotificationsResourceHandlerProps` must be a `readonly` **interface** — upstream is a class with
   mutable fields, which jsii rejects as a props bag. JSDoc on every exported member; no `any` beyond
   the blessed `Record<string, any>` outputs getter.
2. Keep `CommonConfiguration` / `LambdaFunctionConfiguration` / `FilterRule` / `Filter` **non-exported**
   (PascalCase members; jsii only inspects exports).
3. Do **not** export `cx-api.ts` or the new const from `src/aws/index.ts` / `storage/index.ts` — jsii
   has no exported-const support (`TARGET_PARTITIONS` is the precedent). Document the literal key in
   the `addEventNotification` JSDoc.
4. The `BucketNotifications | BucketNotificationsResource` union is legal only because the field and
   the callback are `private` — never widen it into a public member (jsii has no unions).

## 6. Integ — `bucket-notifications-cross-stack`

App `integ/aws/storage/apps/bucket-notifications-cross-stack.ts` (provenance header → the harness
`cdktn/` app + CONTRACT.md). One `App` with context
`{ "@terraconstructs/aws-s3:keepNotificationInImportedBucket": true }`, three `AwsStack`s named
`${STACK_NAME}-a|-b|-c`, each with its own `LocalBackend` (`${stackName}.tfstate`):

- shared bucket name `s3n-<SUFFIX>` from env `SUFFIX`; B/C derive it, never read A's outputs;
- **A**: `new storage.Bucket(this, "Bucket", { bucketName, forceDestroy: true })`; **B**/**C**:
  `storage.Bucket.fromBucketName(this, "Bucket", bucketName)`. All three:
  `addEventNotification(EventType.OBJECT_CREATED, new storage.targets.FunctionDestination(fn), { prefix: "<x>/" })`;
- per stack: `compute.LambdaFunction` `s3n-<suffix>-<x>` (nodejs22.x, inline handler forwarding the S3
  event to SQS, env `RESULTS_QUEUE_URL`/`STACK_NAME`), `notify.Queue` `s3n-<suffix>-<x>-results`,
  `queue.grantSendMessages(fn)`;
- outputs — four flat `new TerraformOutput(stack, <key>, { value, staticId: true })` (not
  `registerOutputs`, which emits one object output), so the terraform output ids are exactly the
  CONTRACT keys: `bucket_name` = bucketName, `lambda_arn` = `fn.functionArn`, `queue_url` =
  `queue.queueUrl`, `owner` = `"a"|"b"|"c"`; read in Go with `terraform.Output`.

Go, `integ/aws/storage/storage_test.go` — `TestBucketNotificationsCrossStack`: `t.Parallel()`, `SUFFIX`
= 6 lowercase chars (reuse the file's `rand` helper), `AWS_REGION=us-east-1`,
`STACK_NAME=bucket-notifications-cross-stack`, working dirs `tf/bucket-notifications-cross-stack/{a,b,c}`.

- **New `util.SynthMultiStackApp(t, testApp string, stacks []string, tfWorkingDirs map[string]string, env)`**
  in `integ/aws/util.go`: go-synth's `App.Eval` copies exactly one `cdktf.out/stacks/<name>` per
  executor, so mirror `SynthApp` but drive `executors.NewBunExecutor` directly — `PreSetupFn`/`Setup`/
  `Exec` once, then one `CopyTo("cdktf.out/stacks/"+testApp+"-"+s, …)` per stack (three `SynthApp`
  calls would re-run `bun install` three times).
- Stages (`test_structure.RunTestStage`, `SKIP_*`-able): `synth_app`, `deploy_a`, `validate_a`,
  `deploy_b`, `validate_ab`, `deploy_c`, `validate_abc`, `redeploy_a`, `validate_abc_again`,
  `destroy_b`, `validate_ac`, deferred `cleanup_terraform` (empty bucket, destroy c/a, plus b if
  `destroy_b` was skipped).
- Assertions: `GetBucketNotificationConfiguration` → set of `LambdaFunctionArn` ⊇ expected, ∌ b after
  `destroy_b` (extend `integ/aws/s3.go`); after `validate_abc`, one delivery check per owner — put
  `<x>/1`, poll that owner's queue (≤6 min; S3 config propagation is eventual).
- `integ/aws/storage/Makefile`: target `bucket-notifications-cross-stack` (`## Test cross-stack S3
  notifications via the cfncompat custom resource`) running
  `go test -v -count 1 -timeout 45m ./... -run ^TestBucketNotificationsCrossStack$`, `.PHONY`; the
  `%-synth-only`/`-no-cleanup`/`-validate-only`/`-cleanup-only` suffixes come from `integ/common.mk`.

## 7. Order of work

(1) handler + its unit tests → `pnpm compile`, jest. (2) `bucket-notifications-resource.ts` (renderer,
policy, dependsOn) → unit tests. (3) selection + `cx-api` key + `bucket.ts` cleanups → full
`pnpm exec jest test/aws/storage`, native snapshots unchanged. (4) `pnpm eslint` on every touched file
(+ projen API docs if the repo gates on it). (5) integ app, Go helper, Makefile target (`pnpm compile`
first — terratest reads `lib/`). No AWS runs from this workflow; live validation is the harness's
sixth target.
