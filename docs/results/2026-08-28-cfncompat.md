# 2026-08-28 — cfncompat scenario (awscc base) live run

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
