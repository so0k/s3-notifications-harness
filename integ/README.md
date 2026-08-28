# integ

Terratest (Go) suite that drives the CONTRACT.md deploy/validate flow against
`../awscdk/`, all three terraform scenarios (`../terraform/awscc/`, `../terraform/aws/`,
`../terraform/cfncompat/`), and `../cdktn/` (Option B, `../docs/OPTIONS.md`), side by
side, through one shared `Suite` interface (`suite.go`): `cdkSuite` shells out to `npx cdk`
in `../awscdk`; `tfSuite` copies `../terraform/<provider>/stack-<x>` (plus
`../terraform/<provider>/modules` and `../lambda`, via the whole-repo copy
`test_structure.CopyTerraformFolderToTemp` does) into a temp dir and drives it with
terratest's `terraform` module, where `provider` is `"awscc"`, `"aws"`, or `"cfncompat"`;
`cdktnSuite` shells out to `npx cdktn` in `../cdktn` (with `SUFFIX` in the environment,
since the app reads it directly rather than as a CLI var/context flag).

`harness_test.go` runs the identical flow for all five:

```
deploy A      -> assert config includes {a};       upload a/1         -> queue a receives
deploy B      -> assert config includes {a,b};     upload a/2,b/2     -> queues a,b receive
deploy C      -> assert config includes {a,b,c};   upload a/3,b/3,c/3 -> all receive
re-deploy A   -> assert config includes {a,b,c};   upload */4         -> all receive
destroy B     -> assert config includes {a,c}, excludes b
destroy C, A  (cleanup, deferred; always runs; empties the bucket first)
```

**Expected:** `TestAwsCdk` GREEN. `TestTerraformAwscc` RED starting at "deploy B -> assert
config includes {a,b}" -- stack B's `aws_s3_bucket_notification` resource is authoritative
over the bucket's whole notification config, so it replaces stack A's inline
`awscc_s3_bucket` target rather than merging with it (the `awscdk` suite's
`Bucket.addEventNotification` merges correctly, via the `BucketNotifications` custom
resource, hence GREEN there). That RED is the expected, documented outcome, not a bug in
the test -- deploy calls are fatal (`require`), but every validation is non-fatal
(`assert`), so every later stage still runs and logs more evidence (including each of
stage B and C's terraform plan, showing the replacement) once it starts failing.

`TestTerraformAws` runs the same flow against a scenario where stack-a's target also goes
through `aws_s3_bucket_notification` (the same resource type stack-b/c use), instead of
awscc's inline config. **Expected: RED at the same stage as `TestTerraformAwscc`**, since
that resource is authoritative over the whole bucket regardless of which provider owns
stack-a's target -- comparing the two is what establishes that equivalence; see
`../terraform/README.md` and `TestTerraformAws`'s doc comment in `harness_test.go`.

`TestTerraformCfncompat` runs the same flow against `../terraform/cfncompat/`, which
replaces `aws_s3_bucket_notification` everywhere (including on stack-a's own target) with
a `cfncompat_custom_resource` driving AWS CDK's own bucket-notifications Lambda handler in
its "unmanaged" (merge) mode -- see `../docs/OPTIONS.md` (Option A) and
`../terraform/cfncompat/README.md`. **Expected: fully GREEN**, unlike `TestTerraformAwscc`
and `TestTerraformAws` -- each stack's custom resource GETs the bucket's existing
notification configuration, merges in only its own entry, and PUTs the merged result back,
so no stack's apply clobbers another's target.

`TestCdktn` runs the same flow against `../cdktn/` (Option B, `../docs/OPTIONS.md`), a
cdktn TypeScript app porting `../terraform/cfncompat/` 1:1 -- same three stacks, same
`cfncompat_custom_resource`-driven merge semantics, but built with
`@cdktn/provider-cfncompat`'s `CustomResource` construct instead of HCL. **Expected: fully
GREEN**, for the same reason `TestTerraformCfncompat` is; a regression here with
`TestTerraformCfncompat` still GREEN would point at the construct/binding/CLI layer
rather than the cfncompat protocol engine itself.

## Prereqs

```sh
mise install                       # terraform 1.15.9, go, node 24, aws-cli (repo root .mise.toml)
cd awscdk && npm install && cd ..  # aws-cdk-lib / aws-cdk CLI for the cdk suite
cd cdktn && npm install && cd ..   # cdktn CLI + prebuilt providers for the cdktn suite
```

## Running

```sh
aws-vault exec --no-session tcons-vincent -- make test             # all five suites
aws-vault exec --no-session tcons-vincent -- make test-cdk         # just TestAwsCdk
aws-vault exec --no-session tcons-vincent -- make test-awscc       # just TestTerraformAwscc
aws-vault exec --no-session tcons-vincent -- make test-aws         # just TestTerraformAws
aws-vault exec --no-session tcons-vincent -- make test-cfncompat   # just TestTerraformCfncompat
aws-vault exec --no-session tcons-vincent -- make test-cdktn       # just TestCdktn
```

Or drive `go test` directly:

```sh
aws-vault exec --no-session tcons-vincent -- go test -v -count 1 -timeout 60m -run '^TestAwsCdk$' ./...
aws-vault exec --no-session tcons-vincent -- go test -v -count 1 -timeout 60m -run '^TestTerraformAwscc$' ./...
aws-vault exec --no-session tcons-vincent -- go test -v -count 1 -timeout 60m -run '^TestTerraformAws$' ./...
aws-vault exec --no-session tcons-vincent -- go test -v -count 1 -timeout 60m -run '^TestTerraformCfncompat$' ./...
aws-vault exec --no-session tcons-vincent -- go test -v -count 1 -timeout 60m -run '^TestCdktn$' ./...
```

Region defaults to `us-east-1` (`AWS_REGION`, set by `.mise.toml`); to point at a
different `terraform`/`tofu` binary, set `TERRAFORM_BINARY` (defaults to `terraform`).

### Stages and stage-skipping

Each test runs through named stages via terratest's `test_structure.RunTestStage`:
`deploy_a`, `validate_a`, `deploy_b`, `validate_b`, `deploy_c`, `validate_c`,
`redeploy_a`, `validate_redeploy_a`, `destroy_b`, `validate_after_destroy_b`, and a
deferred `cleanup` (destroys C then A, always runs, empties the bucket first). Setting a
`SKIP_<stage>=true` environment variable skips that stage -- e.g.
`SKIP_cleanup=true make test-cdk` (also `make test-cdk-no-cleanup`) leaves the stacks up
for inspection after a run.

## Layout

```
integ/
  suite.go             # Suite interface + cdkSuite / tfSuite / cdktnSuite implementations (cdkSuite and cdktnSuite share cliSuite)
  harness_test.go      # TestAwsCdk, TestTerraformAwscc, TestTerraformAws, TestTerraformCfncompat, TestCdktn -- the shared CONTRACT.md flow
  assert.go            # assertNotificationTargets, assertDelivery, assertNoCrossDelivery
  aws/
    s3.go              # GetS3BucketNotificationE, UploadS3File, EmptyBucket (all object versions)
    sqs.go             # WaitForQueueMessage, DeleteMessage
```
