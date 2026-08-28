# integ

Terratest (Go) suite that drives the CONTRACT.md deploy/validate flow against both
`../awscdk/` and `../terraform/`, side by side, through one shared `Suite` interface
(`suite.go`): `cdkSuite` shells out to `npx cdk` in `../awscdk`; `tfSuite` copies
`../terraform/stack-<x>` (plus `../terraform/modules` and `../lambda`, via the whole-repo
copy `test_structure.CopyTerraformFolderToTemp` does) into a temp dir and drives it with
terratest's `terraform` module.

`harness_test.go` runs the identical flow for both:

```
deploy A      -> assert config includes {a};       upload a/1         -> queue a receives
deploy B      -> assert config includes {a,b};     upload a/2,b/2     -> queues a,b receive
deploy C      -> assert config includes {a,b,c};   upload a/3,b/3,c/3 -> all receive
re-deploy A   -> assert config includes {a,b,c};   upload */4         -> all receive
destroy B     -> assert config includes {a,c}, excludes b
destroy C, A  (cleanup, deferred; always runs; empties the bucket first)
```

**Expected:** `TestAwsCdk` GREEN. `TestTerraform` RED starting at "deploy B -> assert
config includes {a,b}" -- stack B's `aws_s3_bucket_notification` resource is authoritative
over the bucket's whole notification config, so it replaces stack A's target rather than
merging with it (the `awscdk` suite's `Bucket.addEventNotification` merges correctly, via
the `BucketNotifications` custom resource, hence GREEN there). That RED is the expected,
documented outcome, not a bug in the test -- deploy calls are fatal (`require`), but every
validation is non-fatal (`assert`), so every later stage still runs and logs more evidence
(including each of stage B and C's terraform plan, showing the replacement) once it starts
failing.

## Prereqs

```sh
mise install                       # terraform 1.15.9, go, node 24, aws-cli (repo root .mise.toml)
cd awscdk && npm install && cd ..  # aws-cdk-lib / aws-cdk CLI for the cdk suite
```

## Running

```sh
aws-vault exec tcons-vincent -- make test      # both suites
aws-vault exec tcons-vincent -- make test-cdk  # just TestAwsCdk
aws-vault exec tcons-vincent -- make test-tf   # just TestTerraform
```

Or drive `go test` directly:

```sh
aws-vault exec tcons-vincent -- go test -v -count 1 -timeout 60m -run '^TestAwsCdk$' ./...
aws-vault exec tcons-vincent -- go test -v -count 1 -timeout 60m -run '^TestTerraform$' ./...
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
  suite.go            # Suite interface + cdkSuite / tfSuite implementations
  harness_test.go      # TestAwsCdk, TestTerraform -- the shared CONTRACT.md flow
  assert.go            # assertNotificationTargets, assertDelivery, assertNoCrossDelivery
  aws/
    s3.go              # GetS3BucketNotificationE, UploadS3File, EmptyBucket (all object versions)
    sqs.go             # WaitForQueueMessage, DeleteMessage
```
