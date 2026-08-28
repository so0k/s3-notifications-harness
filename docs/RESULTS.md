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
