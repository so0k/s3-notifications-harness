# cdktn/ (Option B)

TypeScript [cdktn](https://cdktn.io) app porting `terraform/cfncompat/` 1:1: three
`TerraformStack`s (`s3n-harness-<a|b|c>-<suffix>`) using `@cdktn/provider-awscc`,
`@cdktn/provider-aws`, and `@cdktn/provider-cfncompat`'s `CustomResource` to drive
AWS CDK's own bucket-notifications Lambda handler in its "unmanaged" (merge) mode.
See `../CONTRACT.md` and `../docs/OPTIONS.md` (Option B).

## Setup

```sh
mise x -- npm install
```

## Synth (no AWS credentials required)

```sh
SUFFIX=k3m9x1 mise x -- npx cdktn synth
```

Output lands in `cdktf.out/stacks/s3n-harness-<a|b|c>-k3m9x1/cdk.tf.json`.

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

Stack A's `awscc_s3_bucket` has no `force_destroy` equivalent -- empty the shared
bucket before destroying stack A (same caveat as `terraform/cfncompat/`, see
CONTRACT.md's "Stack A specifics").

## Layout

- `main.ts` -- app entrypoint; reads `SUFFIX` from the environment (throws if unset),
  region from `AWS_REGION` (default `us-east-1`), wires the three stacks.
- `lib/notification-target.ts` -- port of `terraform/awscc/modules/notification-target`:
  results SQS queue + lambda target (`../lambda/index.js`) + role + S3-invoke permission.
- `lib/bucket-notifications-polyfill.ts` -- port of
  `terraform/cfncompat/modules/bucket-notifications`: response bucket + handler role +
  handler lambda (`../lambda/notifications-handler/index.py`, inlined verbatim) +
  `cfncompat_custom_resource` (`Managed = "false"`, merge semantics).
- `cdktf.json` -- cdktn app config (`app: "npx tsx main.ts"`).

## Versions used (this synth)

- `cdktn` / `cdktn-cli` `^0.24.0` (resolved `0.24.0`)
- `@cdktn/provider-awscc` `^1.2.0` -> `hashicorp/awscc 1.98.0`
- `@cdktn/provider-aws` `^25.3.0` -> `hashicorp/aws 6.62.0`
- `@cdktn/provider-cfncompat` `^1.0.0` -> `cdktn-io/cfncompat 0.2.0`
- `constructs` `~10.7.2` (pinned within the bindings' shared peer range `>=10.6.0 <10.8.0`)
- node `24.18.0`, typescript `^5.9.0`, tsx `^4.19.2`

All three prebuilt provider packages peer on `cdktn ^0.24.0`, so `cdktn get` /
`terraformProviders` in `cdktf.json` was not needed.
