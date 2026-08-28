# TerraConstructs port — implementation plans

The two file-by-file plans Option C ([../OPTION-C-PLAN.md](../OPTION-C-PLAN.md)) was executed
from, on the `feat/cfncompat-custom-resource` branch of `terraconstructs/base`. Both are kept
as written: they name the exact upstream `aws-cdk-lib` sources each file was ported from and
the jsii constraints that shaped the API, which the shipped code does not restate. Read
OPTION-C-PLAN.md first for the design and its current status; these for the detail.

| Plan | Covers |
|---|---|
| [cfncompat-custom-resource-core.md](cfncompat-custom-resource-core.md) | the `aws-cdk-lib/core` layer that had to land first: `CustomResource`, the handler singleton, `AwsStack`'s cfncompat provider + response bucket, export wiring, projen deps, ported tests |
| [cfncompat-bucket-notifications.md](cfncompat-bucket-notifications.md) | the S3 layer built on it: `NotificationsResourceHandler`, `BucketNotificationsResource`, the selection rule in `BucketBase.withNotifications`, unit tests, and the `bucket-notifications-cross-stack` integ app |

Both plans are longer than this repo's usual 200-line ceiling. They are deliberately not split
further: each is a single unit of work whose sections are only meaningful in order, and neither
is a reference anyone navigates into — this index is the entry point.
