# terraform-provider-awscc gaps a construct library must fill

awscc is generated 1:1 from the CloudFormation registry: every resource and every data
source mirrors a Cloud Control type, and "CloudFormation does not support the concept of a
read-only resource" ([issue #729](https://github.com/hashicorp/terraform-provider-awscc/issues/729)).
Anything that is not a CFN resource therefore does not exist in awscc. Confirmed against the
1.98.0 schema dump (2,621 data sources, all resource mirrors; no `functions` block) and the
runs in this harness. Relevant if TerraConstructs-style L2s were to target awscc + cfncompat
without hashicorp/aws.

## Confirmed gaps

| Need | hashicorp/aws | awscc | Fill with |
|---|---|---|---|
| Caller account id / partition / region | `aws_caller_identity`, `aws_partition`, `aws_region` | none | parse an owned ARN (`split(":", arn)[4]`), or a cfncompat data source |
| Availability zones | `aws_availability_zones` | none | cfncompat data source (`Fn::GetAZs` polyfill) |
| Empty bucket on destroy | `aws_s3_bucket.force_destroy` | none — `AWS::S3::Bucket` has no such property; delete fails on a non-empty bucket | out-of-band emptying (CDK uses a custom resource: `autoDeleteObjects`) → cfncompat custom resource |
| Bucket notifications from multiple stacks | `aws_s3_bucket_notification` (single writer) | inline `notification_configuration` (single writer) | `cfncompat_custom_resource` + CDK handler (proven GREEN here) |
| IAM policy document assembly | `aws_iam_policy_document` | none | `jsonencode` / construct-side PolicyDocument |
| Zip archive for Lambda code | `archive_file` (hashicorp/archive) | inline `zip_file` only (single file, 4 MB) | keep hashicorp/archive or S3-asset upload |
| Provider-defined functions (`arn_parse`, …) | yes | none | cfncompat provider functions |
| Bucket lookup argument | `data.aws_s3_bucket.bucket` | `data.awscc_s3_bucket.id` (name passed as `id`) | naming only |
| Resource coverage | full | types the generator suppresses ([#156](https://github.com/hashicorp/terraform-provider-awscc/issues/156), e.g. [#2311 cloudfront_distribution](https://github.com/hashicorp/terraform-provider-awscc/issues/2311)) | hashicorp/aws for those |

## Reported in the provider's issue tracker (not re-verified here)

- Optional+Computed "shadow drift": real drift on optional attributes is invisible
  ([docs/index.md](https://github.com/hashicorp/terraform-provider-awscc/blob/main/docs/index.md),
  [#2726](https://github.com/hashicorp/terraform-provider-awscc/issues/2726),
  [#191](https://github.com/hashicorp/terraform-provider-awscc/issues/191)). The same property
  is what makes the custom-resource coexistence work (constraints §4).
- IAM propagation / bare `InternalFailure` waiter failures
  ([#221](https://github.com/hashicorp/terraform-provider-awscc/issues/221),
  [#338](https://github.com/hashicorp/terraform-provider-awscc/issues/338),
  [#701](https://github.com/hashicorp/terraform-provider-awscc/issues/701)); observed once here.
- Import limitations for composite identifiers
  ([#1259](https://github.com/hashicorp/terraform-provider-awscc/issues/1259),
  [#1560](https://github.com/hashicorp/terraform-provider-awscc/issues/1560)).
- Weekly regeneration from the us-east-1 registry: new CFN types lag.

## What this means for cfncompat

Beyond the custom-resource engine, a library that wanted to be awscc-only would need
cfncompat to carry the CloudFormation pseudo-parameters/intrinsics that have no resource
backing: `AWS::AccountId`, `AWS::Partition`, `AWS::Region`, `AWS::URLSuffix`, `Fn::GetAZs`.
That is out of scope for the current goal (porting the CDK bucket-notifications L2 onto
TerraConstructs, which keeps hashicorp/aws) but is the natural next item on the roadmap.

## Where the rest of this lives

The survey of every CloudFormation pseudo parameter and intrinsic `aws-cdk-lib` actually uses
— with the TF/awscc equivalent, whether cfncompat has it, and the prioritized cfncompat
backlog and open design questions that fall out of it — is in
[cfn-intrinsics-survey.md](cfn-intrinsics-survey.md).
