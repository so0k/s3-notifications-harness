# Observed results

Append-only log of harness runs, one document per run. Add a new document under
`results/` and a new row here; never rewrite an earlier entry to reflect what a later
run found. Raw `go test` output for each run is under `../test-reports/`.

- [2026-08-28 — account 694710432912, us-east-1](results/2026-08-28-awscdk-vs-awscc.md) — awscdk GREEN vs terraform + awscc RED — the baseline side-by-side run.
- [2026-08-28 (14:12–14:41 +07) — account 694710432912, us-east-1 — awscc re-run + aws scenario](results/2026-08-28-awscc-rerun-and-aws.md) — awscc re-run alongside the new `aws`-only scenario, with the B/C/re-A plan comparison.
- [2026-08-28 — cfncompat scenario (awscc base) live run](results/2026-08-28-cfncompat.md) — `TestTerraformCfncompat` PASS — first run of cfncompat's custom-resource engine on real AWS, plus the awscc drift answer.
- [2026-08-28 — cdktn scenario live run](results/2026-08-28-cdktn.md) — `TestCdktn` PASS — the cdktn port of the cfncompat scenario, including the `Fn.jsonencode` follow-up run.
