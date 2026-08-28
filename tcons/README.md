# tcons/ (Option C)

TypeScript [cdktn](https://cdktn.io) app built directly on
[TerraConstructs](https://github.com/TerraConstructs/base)' own
`Bucket.addEventNotification`, rather than on a polyfill construct local to this
harness (that's `../cdktn/`, Option B). Same three `TerraformStack`s
(`s3n-harness-<a|b|c>-<suffix>`) as every other suite: stack A owns the shared
bucket, stacks B and C only ever import it by name. The
`@terraconstructs/aws-s3:keepNotificationInImportedBucket` context key (set in
`cdktf.json`) forces every stack -- including A -- through TerraConstructs'
`Custom::S3BucketNotifications` custom resource (a `cfncompat_custom_resource`
driving AWS CDK's own bucket-notifications Lambda handler in its "unmanaged"
(merge) mode) instead of the native `aws_s3_bucket_notification` resource, so A
can share its bucket with B/C without any of their applies clobbering another's
target. See `../CONTRACT.md` and `../docs/OPTION-C-PLAN.md`.

## Setup

`terraconstructs` is consumed from a local tarball, not npm -- build it first from
the `feat/cfncompat-custom-resource` branch of `terraconstructs/base`:

```sh
cd ~/tcons/base-cfncompat && pnpm package:js   # -> dist/js/terraconstructs@0.0.0.jsii.tgz
cd ~/cdktn/s3-notifications-harness/tcons && npm install
```

`package.json`'s `terraconstructs` dependency is a relative `file:` path to that
tarball (`../../../tcons/base-cfncompat/dist/js/terraconstructs@0.0.0.jsii.tgz`);
re-run `npm install` here after rebuilding the tarball to pick up changes.

## Synth (no AWS credentials required)

```sh
SUFFIX=k3m9x1 mise x -- npx cdktn synth
```

Output lands in `cdktf.out/stacks/s3n-harness-<a|b|c>-k3m9x1/cdk.tf.json`. Each
stack's `cfncompat_custom_resource` (`Managed: "false"`) is the only mechanism
attaching that stack's notification target -- there is no
`aws_s3_bucket_notification` resource anywhere in this app's synthesized output,
matching `../terraform/cfncompat/` and `../cdktn/`.

## Deploy / destroy (requires AWS credentials)

```sh
SUFFIX=k3m9x1 aws-vault exec --no-session tcons-vincent -- npx cdktn deploy s3n-harness-a-k3m9x1 --auto-approve
SUFFIX=k3m9x1 aws-vault exec --no-session tcons-vincent -- npx cdktn deploy s3n-harness-b-k3m9x1 --auto-approve
SUFFIX=k3m9x1 aws-vault exec --no-session tcons-vincent -- npx cdktn deploy s3n-harness-c-k3m9x1 --auto-approve
```

```sh
SUFFIX=k3m9x1 aws-vault exec --no-session tcons-vincent -- npx cdktn destroy s3n-harness-c-k3m9x1 --auto-approve
SUFFIX=k3m9x1 aws-vault exec --no-session tcons-vincent -- npx cdktn destroy s3n-harness-b-k3m9x1 --auto-approve
SUFFIX=k3m9x1 aws-vault exec --no-session tcons-vincent -- npx cdktn destroy s3n-harness-a-k3m9x1 --auto-approve
```

Stack A's bucket has `forceDestroy: true`, so no empty-bucket-before-destroy step
is required for it specifically -- the harness's shared cleanup step still empties
the bucket first regardless, since it is unconditional across every suite (see
`../integ/harness_test.go`'s cleanup comment).

## Layout

- `main.ts` -- app entrypoint; reads `SUFFIX` from the environment (throws if
  unset), region from `AWS_REGION` (default `us-east-1`), wires the three stacks
  directly against `terraconstructs`' `aws.AwsStack`, `aws.storage.Bucket`,
  `aws.compute.LambdaFunction`, `aws.notify.Queue`, and
  `aws.storage.targets.FunctionDestination` -- no local polyfill construct (unlike
  `../cdktn/lib/`).
- `cdktf.json` -- cdktn app config (`app: "npx tsx main.ts"`) plus the
  `@terraconstructs/aws-s3:keepNotificationInImportedBucket` context key.

## TerraConstructs API notes (gaps hit while porting `../cdktn/main.ts`)

- No `storage.notificationTargets.LambdaDestination` -- the actual export is
  `aws.storage.targets.FunctionDestination` (`export * as targets from
  "./notification-targets"` in `storage/index.ts`). It also handles the S3
  invoke permission itself (`fn.addPermission(...)` scoped onto the bucket), so
  unlike `../cdktn/lib/notification-target.ts` this app never constructs a
  `LambdaPermission` by hand.
- `compute.Code.fromAsset(path)` takes a directory (or a `.zip`), not a single
  file -- it cannot point straight at `../lambda/index.js` the way
  `fs.readFileSync` + `Code.zipFile` can in `../cdktn/lib/`. This app instead
  points `fromAsset` at the `../lambda` directory, which stages the whole
  directory as the function's code (via a generated `archive_file` + an
  auto-created per-stack S3 "AssetBucket", the same asset-staging path AWS CDK
  itself uses) -- one directory content deviation from every other suite's
  single-file lambda packaging, but no functional difference (`index.js` is
  still the only file that matters, `handler: "index.handler"`).
- `AwsStack`'s `gridUUID` (used as the physical-name prefix for anything not
  given an explicit name, and as the `cfncompat_custom_resource`'s `stack_id`)
  must start with a letter and be <=36 chars; this app derives it as
  `g-<owner>-<suffix>` per stack.
- Every stack gets its own auto-created `CustomResourceResponsesBucket` (a real
  `aws_s3_bucket { force_destroy: true }`, one per stack, lazily created by the
  first `CustomResource`) as the cfncompat custom resource's response-transport
  bucket -- this shows up as an extra `aws_s3_bucket` resource per stack beyond
  the shared notification bucket itself (which, like `../terraform/cfncompat/`,
  has no data source or resource at all in stacks B/C -- its name and ARN are
  computed from the deterministic `s3n-harness-<suffix>` naming).

## Versions

- `cdktn` / `cdktn-cli` `^0.24.0`
- `terraconstructs` `0.0.0` from a local `pnpm package:js` tarball
  (`terraconstructs/base`, `feat/cfncompat-custom-resource` branch)
- `@cdktn/provider-aws` `^25.0.0`, `@cdktn/provider-cfncompat` `^1.0.0` (matching
  `terraconstructs`' own peer ranges)
- `constructs` `~10.7.2`
- typescript `^5.9.0`, tsx `^4.19.2`, `@types/node` `^22`
