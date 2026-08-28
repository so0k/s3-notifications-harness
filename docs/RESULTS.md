# Observed results

Append-only log of harness runs. Add new entries at the end; never rewrite an earlier
entry to reflect what a later run found.

## 2026-08-28 — account 694710432912, us-east-1

terraform 1.15.9 + awscc 1.98 vs aws-cdk-lib 2.267.0.

| Stage | awscdk | terraform + awscc |
|---|---|---|
| deploy A → config ⊇ {a} | ✅ | ✅ |
| deploy B → config ⊇ {a,b} | ✅ | ❌ config = {b} — `aws_s3_bucket_notification` plan only says "will be created", A's target is dropped silently |
| deploy C → config ⊇ {a,b,c} | ✅ | ❌ config = {c} |
| re-deploy A → config ⊇ {a,b,c} | ✅ (no-op) | ❌ plan: `awscc_s3_bucket.bucket will be updated in-place` → config = {a} |
| destroy B → config = {a,c} | ✅ | ❌ config = {} (destroying B's authoritative resource wipes everything) |

Raw `go test` output for this run is under `../test-reports/`.

## 2026-08-28 (14:12–14:41 +07) — account 694710432912, us-east-1 — awscc re-run + aws scenario

terraform 1.15.9, hashicorp/aws 6.62.0, hashicorp/awscc 1.98.0. Ran `make test-awscc` and
`make test-aws` in parallel (`suffix=jeofnh` for awscc, `suffix=xti2eq` for aws). Both
`go test` processes exited 2 (FAIL), as expected — deploy calls are `require` (fatal),
every validation is `assert` (non-fatal), so both suites ran every stage through cleanup.

| Stage | terraform + awscc | terraform + aws (own scenario) |
|---|---|---|
| deploy_a / validate_a → config ⊇ {a} | ✅ config = {a} | ✅ config = {a} |
| deploy_b / validate_b → config ⊇ {a,b} | ❌ config = {b} only; a/2 delivery assert also fails (target a dropped) | ❌ config = {b} only; a/2 delivery assert also fails |
| deploy_c / validate_c → config ⊇ {a,b,c} | ❌ config = {c} only | ❌ config = {c} only |
| redeploy_a / validate_redeploy_a → config ⊇ {a,b,c} | ❌ config = {a} only (b, c assertions fail) | ❌ config = {a} only (b, c assertions fail) |
| destroy_b / validate_after_destroy_b → config = {a,c} | ❌ config = {} (a and c both missing) | ❌ config = {} (a and c both missing) |
| cleanup (destroy c, a; empty bucket first) | ✅ clean, 5 resources destroyed on final apply | ✅ clean, 8 resources destroyed on final apply |

Bottom line: **the `aws`-only scenario is exactly as RED as the `awscc` scenario**, and by
the identical mechanism — `aws_s3_bucket_notification` is a whole-bucket singleton no
matter which provider owns stack A's own target, so each stack's independent state has no
visibility into another stack's config and each apply clobbers the others.

### Plan comparison (the question this run was meant to answer)

**B's first plan (`deploy_b`), both scenarios — pure "create", no replace/remove of A's target visible:**

awscc (`aws_s3_bucket_notification.this`, unchanged across scenarios since B/C always use
`hashicorp/aws` for the cross-stack attach):
```
Terraform used the selected providers to generate the following execution
plan. Resource actions are indicated with the following symbols:
  + create

  # aws_s3_bucket_notification.this will be created
  + resource "aws_s3_bucket_notification" "this" {
      + bucket      = "s3n-harness-jeofnh"
      ...
          + filter_prefix       = "b/"
          + lambda_function_arn = (known after apply)
        }
    }
Plan: 5 to add, 0 to change, 0 to destroy.
```
aws scenario, same stage, same shape (only the target module resource types differ —
`aws_iam_role`/`aws_lambda_function`/`aws_sqs_queue` instead of `awscc_*`; bucket is
resolved via the `data "aws_s3_bucket"` lookup, so its name is already known at plan
time, unlike A's own first-ever plan where it's `(known after apply)`):
```
  # aws_s3_bucket_notification.this will be created
  + resource "aws_s3_bucket_notification" "this" {
      + bucket      = "s3n-harness-xti2eq"
      + eventbridge = false
      + id          = (known after apply)
      + region      = "us-east-1"

      + lambda_function {
          + events              = [
              + "s3:ObjectCreated:*",
            ]
          + filter_prefix       = "b/"
          + id                  = (known after apply)
          + lambda_function_arn = (known after apply)
        }
    }
Plan: 7 to add, 0 to change, 0 to destroy.
```
Neither plan shows any destroy/replace of stack A's target — B's terraform state has no
resource tracking A's config at all (bucket is a `data` source), so the plan is blind to
the clobbering its apply is about to cause. No "Objects have changed outside of Terraform"
drift section appears anywhere in either log (grepped both full logs, zero matches) —
because from each stack's own state, the singleton notification resource is brand new,
not drifted.

**C's first plan (`deploy_c`)**: identical shape to B's in both scenarios — `will be
created`, `filter_prefix = "c/"`, `Plan: 5 to add, 0 to change, 0 to destroy` (awscc) /
`Plan: 7 to add, 0 to change, 0 to destroy` (aws, same count as B's plan since the target
module is the same shape) — no replace signal either.

**A's re-plan (`redeploy_a`) — this is where the swap becomes visible, in both scenarios,
but on different resources:**

awscc — `awscc_s3_bucket.bucket` (A's own inline notification) updates in place, function
ARN swapped c → a:
```
  # awscc_s3_bucket.bucket will be updated in-place
  ~ resource "awscc_s3_bucket" "bucket" {
        id                                = "s3n-harness-jeofnh"
      ~ notification_configuration        = {
          ~ lambda_configurations = [
              ~ {
                  ~ filter   = {
                      ~ s3_key = {
                          ~ rules = [
                              - { - name  = "Prefix" -> null
                                  - value = "c/" -> null },
                              + { + name  = "Prefix"
                                  + value = "a/" },
                            ]
                        }
                    }
                  ~ function = "...:function:s3n-harness-jeofnh-c" -> "...:function:s3n-harness-jeofnh-a"
                },
            ]
        }
    }
Plan: 0 to add, 1 to change, 0 to destroy.
```
Casing check: both the removed and added filter rule show `name = "Prefix"` (CloudFormation
casing) — no perpetual casing diff remains; the only delta is the `value`/`function` swap.
This confirms the `486cc36` casing fix holds under re-plan.

aws scenario — `aws_s3_bucket_notification.this` (A's own target, same resource type B/C
use) updates in place, same swap, expressed as a flat `filter_prefix`/`lambda_function_arn`
diff instead of a nested CFN-shaped block:
```
  # aws_s3_bucket_notification.this will be updated in-place
  ~ resource "aws_s3_bucket_notification" "this" {
        id          = "s3n-harness-xti2eq"
        # (3 unchanged attributes hidden)
      ~ lambda_function {
          ~ filter_prefix       = "c/" -> "a/"
            id                  = "tf-s3-lambda-20260828072248570000000001"
          ~ lambda_function_arn = "...:function:s3n-harness-xti2eq-c" -> "...:function:s3n-harness-xti2eq-a"
        }
    }
Plan: 0 to add, 1 to change, 0 to destroy.
```

So: **B's/C's first plans never show the removal explicitly** in either scenario — the
clobbering is invisible at plan time because it happens through another stack's
independent apply, not a dependency Terraform can see. **A's re-plan is where the swap
finally surfaces**, in both scenarios, as an in-place update of whichever resource holds
A's target (`awscc_s3_bucket.bucket` for awscc, `aws_s3_bucket_notification.this` for aws)
with the lambda ARN (and filter value) swapped from the last stack that overwrote the
config (`c`) back to `a`. The aws scenario adds no new failure mode and no new visibility
into the problem — it just relocates the same singleton-resource hazard from an
inline `awscc_s3_bucket` block onto a standalone `aws_s3_bucket_notification` resource.

Raw `go test` output for this run: `../test-reports/awscc-20260828.log`,
`../test-reports/aws-20260828.log`.

## 2026-08-28 — cfncompat scenario (awscc base) live run

`TestTerraformCfncompat` **PASS** (1406s). Stack A: `awscc_s3_bucket` with no `notification_configuration`;
every stack: `awscc_lambda_function` running the unmodified AWS CDK `index.py` handler, driven by
`cfncompat_custom_resource` (cdktn-io/cfncompat 0.2.0, `Managed = "false"`). First execution of the
provider's custom-resource engine against real AWS.

| Stage | result |
|---|---|
| deploy A → {a} | ✅ |
| deploy B → {a,b} | ✅ (b live after 4 warm-up probes) |
| deploy C → {a,b,c} | ✅ |
| re-deploy A → {a,b,c} | ✅ — plan: `No changes. Your infrastructure matches the configuration.` |
| destroy B → {a,c} | ✅ |
| cleanup | ✅ account clean |

Drift question answered: with the attribute unset in config, awscc's refresh keeps the out-of-band
value in state and plans nothing. `lifecycle { ignore_changes = [notification_configuration] }` is not
required for this case; it remains the recommended guard against future updates of other bucket
attributes sending `NotificationConfiguration: null`.
