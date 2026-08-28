# awscdk suite

AWS CDK v2 (TypeScript) implementation of the S3 notification harness. See
`../CONTRACT.md` for the full cross-suite contract.

Three stacks, deployed in order A → B → C (Stack A owns the bucket; B and C
attach to it by name):

- `S3nHarnessA-<suffix>` — owns bucket `s3n-harness-<suffix>`, target `a` (prefix `a/`)
- `S3nHarnessB-<suffix>` — imports the bucket by name, target `b` (prefix `b/`)
- `S3nHarnessC-<suffix>` — imports the bucket by name, target `c` (prefix `c/`)

`cdk.json` sets `"@aws-cdk/aws-s3:keepNotificationInImportedBucket": true`, which
is required so Stack A's own `BucketNotifications` custom resource merges with
(rather than clobbers) the notifications added by imported-bucket stacks B/C —
this is the flag that makes the awscdk suite GREEN where the terraform suite is RED.

## Install

```sh
cd awscdk
mise x -- npm install
```

## Synth (no AWS credentials required when account is unset)

```sh
mise x -- npx cdk synth -c suffix=<suffix> --no-lookups
```

## Deploy (requires AWS credentials)

```sh
aws-vault exec --no-session tcons-vincent -- mise x -- npx cdk deploy S3nHarnessA-<suffix> -c suffix=<suffix> \
  --require-approval never --outputs-file outputs-a.json

aws-vault exec --no-session tcons-vincent -- mise x -- npx cdk deploy S3nHarnessB-<suffix> -c suffix=<suffix> \
  --require-approval never --outputs-file outputs-b.json

aws-vault exec --no-session tcons-vincent -- mise x -- npx cdk deploy S3nHarnessC-<suffix> -c suffix=<suffix> \
  --require-approval never --outputs-file outputs-c.json
```

Re-deploying A (to prove it still merges B/C's targets instead of overwriting them):

```sh
aws-vault exec --no-session tcons-vincent -- mise x -- npx cdk deploy S3nHarnessA-<suffix> -c suffix=<suffix> \
  --require-approval never --outputs-file outputs-a.json
```

## Destroy

Destroy in reverse dependency order (B, C before A) since A owns the bucket:

```sh
aws-vault exec --no-session tcons-vincent -- mise x -- npx cdk destroy S3nHarnessB-<suffix> S3nHarnessC-<suffix> -c suffix=<suffix> -f
aws-vault exec --no-session tcons-vincent -- mise x -- npx cdk destroy S3nHarnessA-<suffix> -c suffix=<suffix> -f
```

Stack A's bucket has `RemovalPolicy.DESTROY` + `autoDeleteObjects: true`, so
`cdk destroy` empties and removes it without a manual empty step.
