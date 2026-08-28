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

## CloudFormation pseudo parameters and intrinsics used by aws-cdk-lib

Surveyed against `aws-cdk-lib` at `~/cdk/aws-cdk` commit `a9e6639d` (2026-07-31). Counts are
non-test, non-`.d.ts` reference sites found with `grep -rn --include='*.ts' '<pattern>' .
| grep -v '\.d\.ts\|/test/' | wc -l` run from `packages/aws-cdk-lib`; every `grep` below is
the exact command used. CFN semantics are cited from the AWS Documentation MCP
(`pseudo-parameter-reference.html`, `intrinsic-function-reference*.html`). "Already in
cfncompat" is read off `docs/functions/` and `docs/resources/` in
`terraform-provider-cfncompat`, which today has: `join`, `split`, `select`, `sub`, `base64`,
`cidr`, `find_in_map`, `length`, `to_json_string`, `condition_and/or/not/equals/if/contains/
each_member_equals/each_member_in`, and the `cfncompat_custom_resource` resource — no
pseudo-parameter surface, no `ref`/`get_att`/`get_azs`/`import_value`/`get_stack_output` at all.

### 1. Pseudo parameters

All eight are minted in one place, `core/lib/cfn-pseudo.ts:5-32` (`Aws.ACCOUNT_ID` etc., each
a `{ Ref: 'AWS::<Name>' }` token) plus a stack-scoped variant, `ScopedAws` (`cfn-pseudo.ts:40-77`),
used when a pseudo parameter must be anchored to a specific stack in a multi-stack app. `Stack`
exposes `account`/`region`/`partition`/`urlSuffix`/`stackName`/`stackId` as resolved-or-token
properties (`core/lib/stack.ts`) that L2s read far more often than `Aws.*` directly. AWS
reference: [pseudo-parameter-reference.html](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/pseudo-parameter-reference.html).

| Pseudo param | CDK source | Usage (`Aws.X` / `stack.x` non-test refs) | Representative call sites | TF/awscc equivalent | cfncompat needs | Priority | In cfncompat? |
|---|---|---|---|---|---|---|---|
| `AWS::AccountId` | `cfn-pseudo.ts:22`, `Stack.account` | `Aws.ACCOUNT_ID`: 21; `.account`: 243 | `Arn.format` (`arn.ts:140`); `stack-synthesizers/default-synthesizer.ts:427` deploy-role ARNs; `aws-codebuild/lib/linux-gpu-build-image.ts:131` (`regionalFact(DLC_REPOSITORY_ACCOUNT)`) | `aws_caller_identity.current.account_id` | data source (`cfncompat_caller_identity` or reuse hashicorp/aws) | P0 | No |
| `AWS::Region` | `cfn-pseudo.ts:26`, `Stack.region` | `Aws.REGION`: 33; `.region`: 68 | `Arn.format`; `private/region-lookup.ts:97` (`RegionInfo.get(region).domainSuffix`); `stack.ts:919` `availabilityZones` fallback | `aws_region.current.name` / provider config | provider-level value (already resolvable from TF provider block) | P0 | No |
| `AWS::Partition` | `cfn-pseudo.ts:25`, `Stack.partition` | `Aws.PARTITION`: 49; `.partition`: 43 | `Arn.format` (`arn.ts:140`); `aws-iam/lib/managed-policy.ts:191`; `stack.ts:787` `RegionInfo.get(this.region).partition` | `aws_partition.current.partition` | data source or static per-region table (partition is never a live API value; `hashicorp/aws`'s `aws_partition` derives it from region) | P0 | No |
| `AWS::URLSuffix` | `cfn-pseudo.ts:23`, `Stack.urlSuffix` | `Aws.URL_SUFFIX`: 7; `urlSuffix`: 39 | `core/lib/nested-stack.ts:254` (asset S3 URL); `aws-s3/lib/bucket.ts:2176-2201` (website endpoint domain); `aws-cognito/lib/user-pool.ts:1116` (SAML provider name); `core/lib/private/region-lookup.ts` (`RegionInfo` domain-suffix table) | no live TF equivalent | static lookup table keyed by region/partition (mirrors `region-info`'s `FactName.URL_SUFFIX` table) | P1 | No |
| `AWS::StackName` | `cfn-pseudo.ts:28`, `Stack.stackName` | `Aws.STACK_NAME`: 7; `.stackName`: 56 | `stack-synthesizers/stack-synthesizer.ts` templateUrl construction; every default `physicalName` fallback (`Names.uniqueResourceName`); `exportValue()` (`stack.ts:1279` export name = `${stackName}:...`) | no CFN stack exists in the awscc/TF world | needs a modelled value — see design question below | P0 | No |
| `AWS::StackId` | `cfn-pseudo.ts:27`, `Stack.stackId` | `Aws.STACK_ID`: 4 | rarely read directly by L2s; mostly available for custom-resource physical IDs and `Fn::Sub` templates that want a globally-unique ARN-shaped value | none | needs a modelled value — see design question below | P2 | No |
| `AWS::NotificationARNs` | `cfn-pseudo.ts:24`, `ScopedAws.notificationArns` | `Aws.NOTIFICATION_ARNS`: 1 | essentially unused by L2s (stack-level CLI `--notification-arns`, no resource references it) | none (no TF/awscc concept of stack notification subscribers) | P2 (no equivalent, safe to stub empty list) | No |
| `AWS::NoValue` | `cfn-pseudo.ts:29` | `Aws.NO_VALUE`: 13 | `aws-dynamodb/lib/table.ts:1869-1964` (optional GSI ARN / replica `Fn::If`); `aws-codebuild/lib/cache.ts:82` (`Fn.join('/', [...,  options.prefix ?? Aws.NO_VALUE])`); always paired with `Fn.conditionIf` | HCL's own `null` / omitted-attribute semantics inside the bridge | (a) resolvable at synth-time — when `Fn::If` bottoms out on a literal condition, the bridge should just omit the property/list element like Terraform's `null` does | P1 | No (see `condition_if.md`, which documents the function but not the `NoValue` sentinel) |

### 2. Intrinsic functions

`core/lib/cfn-fn.ts:1` — the class docblock cites [intrinsic-function-reference.html](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/intrinsic-function-reference.html)
directly. Token-level rendering (auto-generated `Fn::Join`/`Fn::Sub` for string
interpolation, not explicit `Fn.*` calls) happens in `core/lib/private/cloudformation-lang.ts`
(`fnJoinConcat`, `tokenAwareStringify`, `minimalCloudFormationJoin`) — this is why raw
`Fn.join`/`Fn.getAtt` counts undercount real template output: most `${resource.attr}`
string templating in L2s never calls `Fn.*` explicitly, it goes through `Token`/`CfnReference`
resolution and CDK's own minimal-join optimizer instead.

| Intrinsic | CDK source | Usage (`Fn.x(` non-test refs) | TF/awscc equivalent | cfncompat needs | Priority | In cfncompat? |
|---|---|---|---|---|---|---|
| `Ref` | `cfn-fn.ts:21` `Fn.ref`; mainly emitted implicitly via `CfnReference`/`CfnElement.ref` for every L1/L2 resource reference | `Fn.ref(`: 2 (explicit); implicit refs: pervasive (`cfnOptions.` sites: 82, `.getAtt(`: 34 suggest the scale) | Terraform's own resource-attribute references (`aws_x.y.id`) | (a) resolvable at synth-time in the bridge — a `Ref` to a resource logical id becomes a direct TF resource-attribute reference | P0 | No (not a function-shaped intrinsic; this is graph-wiring the bridge must already do) |
| `Fn::GetAtt` | `cfn-fn.ts:35` `Fn.getAtt`; L2s almost always use generated `resource.attrXxx` properties instead (`.getAtt(`: 34 explicit call sites) | (a) resolvable at synth-time — maps to the corresponding `awscc_*`/`aws_*` computed attribute | P0 | No (same as `Ref`: graph wiring, not a provider function) |
| `Fn::Join` | `cfn-fn.ts:49`; auto-emitted by `cloudformation-lang.ts:85` `fnJoinConcat` for token concatenation | `Fn.join(`: 17 explicit + pervasive implicit | (a) HCL string interpolation / `join()` | function | P0 | **Yes** (`function_join.go`, `docs/functions/join.md`) |
| `Fn::Sub` | `cfn-fn.ts:153` `Fn.sub` | 5 | (a) resolvable at bridge synth-time when all `${Var}` are known; otherwise (b) `provider::cfncompat::sub(template, variables)` — explicit-variables form only, `${Resource.Attr}` must be pre-resolved by the bridge | function (exists) | P1 | **Yes** (`docs/functions/sub.md`, `function_sub.go`) |
| `Fn::Select` | `cfn-fn.ts:129`; used constantly with `Fn::GetAZs`/`Fn::Split` | 71 | (a) TF `element()`/index | function | P0 | **Yes** (`docs/functions/select.md`) |
| `Fn::Split` | `cfn-fn.ts:100` | 57 | (a) TF `split()` | function | P0 | **Yes** (`docs/functions/split.md`) |
| `Fn::Base64` | `cfn-fn.ts:164` | 5 | (a) TF `base64encode()` | function | P2 | **Yes** (`docs/functions/base64.md`) |
| `Fn::Cidr` | `cfn-fn.ts:175` | 5 | (b) TF `cidrsubnets()` has different signature/order — needs the CFN-shaped wrapper | function | P1 | **Yes** (`docs/functions/cidr.md`, already CFN-shaped) |
| `Fn::FindInMap` | `cfn-fn.ts:255/265`; also auto-generated by `regionalFact` → `private/region-lookup.ts:38-45` (`deployTimeLookup` builds a `CfnMapping` keyed by region, `mapping.findInMap(Aws.REGION, factKey)`) | `Fn.findInMap`+`_findInMap`: 4 explicit, but every `regionalFact` call (7 sites: `custom-resource-provider.ts:166`, `bucket.ts:2187`, `linux-gpu-build-image.ts:131`, `adot-layers.ts:82`, `runtime.ts:487`, `params-and-secrets-layers.ts:257`, `lambda-insights.ts:183`) round-trips through it when region is unresolved | (a) resolvable at synth-time whenever the map + key are both known at bridge-synth time (region is usually a literal in the TF provider config) | function, for the rare case region truly isn't known until apply | P1 | **Yes** (`docs/functions/find_in_map.md`) |
| `Fn::GetAZs` | `cfn-fn.ts:201` `FnGetAZs`; the fallback path in `Stack.availabilityZones` (`stack.ts:919-928`) emits `Fn.select(0/1, Fn.getAzs())` whenever account/region are unresolved tokens — exactly the awscc/cdktn shape when account/region come from data sources | 3 explicit + every environment-agnostic stack's AZ lookup | `data.aws_availability_zones.available.names` | **data source** | P0 | No (flagged as the headline gap in this file's original "Confirmed gaps" table) |
| `Fn::ImportValue` | `cfn-fn.ts:213`; `Stack.exportValue`/`exportStringListValue` (`stack.ts:1304-1360`) wrap it for cross-stack refs | 17 | no TF/awscc concept of a CFN Export | **no clean equivalent** — cross-stack refs must become TF remote-state data sources or cdktn cross-stack outputs at the construct-graph level, not a runtime lookup | P1 (blocks any multi-stack app with cross-stack references, which is common) | No |
| `Fn::GetStackOutput` | `cfn-fn.ts:229` `Fn.getStackOutput`; used internally by `core/lib/private/refs.ts:605` as the cross-account/cross-region alternative to `Export`+`Fn::ImportValue` (see below) | 1 (framework-internal, but triggered by every cross-account/region stack reference) | same category as `Fn::ImportValue` | **no clean equivalent** — needs the exporting stack's real CFN stack name/region, which doesn't exist in an awscc/TF deployment | P2 (only hit by explicit cross-account/region references) | No |
| `Fn::If`/`And`/`Or`/`Not`/`Equals`/`Contains`/`EachMemberEquals`/`EachMemberIn` (Conditions) | `cfn-fn.ts:288-407`; `addBootstrapVersionRule` in `core/lib/stack-synthesizers/stack-synthesizer.ts:333-357` builds a real `CfnRule` + `Fn::Contains`/`Fn::Not` assertion against the bootstrap-version SSM parameter | `conditionIf`: 6, `conditionEquals`: 7, `conditionOr`: 5, `conditionAnd`: 3, `conditionNot`: 3, `conditionContains`: 1 (rest 0) | (a) HCL `condition ? a : b` when the condition is resolvable at synth time (the common case: conditions usually depend on context/feature flags CDK already resolves) | function, for the residual case where a condition depends on a genuinely deploy-time-unknown value | P1 | **Yes** (`condition_if/and/or/not/equals/contains/each_member_equals/each_member_in.md` — already the most complete category) |
| `Fn::Length` | `cfn-fn.ts:459` `Fn.len`; only invoked when the array argument is an unresolved `Token` (short-circuits to plain `.length` otherwise, `cfn-fn.ts:461-463`) — triggers `stackOf(scope).addTransform('AWS::LanguageExtensions')` (`cfn-fn.ts:949`) | 0 direct L2 calls found (framework/user-template use only) | (a) TF `length()` | function | P2 | **Yes** (`docs/functions/length.md`) |
| `Fn::ToJsonString` | `cfn-fn.ts:445` `Fn.toJsonString`; same `Token`-only short-circuit, same `AWS::LanguageExtensions` transform (`cfn-fn.ts:938`) | 0 direct L2 calls found | (a) TF `jsonencode()` | function | P2 | **Yes** (`docs/functions/to_json_string.md`) |
| `Fn::Transform` / macros (`AWS::LanguageExtensions`, `AWS::Serverless-2016-10-31`, `AWS::SecretsManager-2024-09-16`) | `cfn-fn.ts:276` `Fn.transform`; `Stack.addTransform` (`stack.ts:993-996`); call sites at `stack.ts:1462`, `cfn-fn.ts:538/949/978`, `aws-secretsmanager/lib/rotation-schedule.ts:319` | 1 explicit `Fn.transform` (used by `helpers-internal/cfn-parse.ts:685` for round-tripping `Fn::Transform` found in an *included* template) | **no CFN macro-processing engine exists outside CloudFormation** | (c) no equivalent for third-party macros (SAM, custom account-level macros); the `AWS::LanguageExtensions` cases specifically are moot in the bridge because cfncompat implements `Fn::Length`/`Fn::ToJsonString` as native functions, bypassing the need for the macro entirely | P2 (unsupported) / N/A for LanguageExtensions | No, and `AWS::Serverless`/custom macros should stay explicitly unsupported |
| `Fn::Cidr`, `Fn::GetAZs` composed with `Stack.availabilityZones` | see above | — | — | — | — | — |
| `cloudformation-include` (`Fn::Transform` "AWS::Include", raw template embedding) | `cloudformation-include/lib/cfn-include.ts`, `core/lib/cfn-include.ts` | out of scope for typical L2 usage — opt-in escape hatch | no TF/awscc equivalent | (c) unsupported; `CfnInclude` users are already opting out of typed L2s | P2 | No |

### 3. Stack-level constructs that behave like intrinsics

- **`CfnParameter`**, incl. SSM-backed types (`AWS::SSM::Parameter::Value<String>`): the
  framework itself uses one, `BootstrapVersion` in
  `core/lib/stack-synthesizers/stack-synthesizer.ts:340-357`, to encode a `CfnRule` assertion
  (`Fn::Not(Fn::Contains(oldVersions, param.valueAsString))`) checking the deployed bootstrap
  stack version — this entire mechanism is CLI-bootstrap-specific and **should simply not fire**
  in an awscc/cdktn synthesis (no CFN bootstrap stack exists); the synthesizer used should be a
  bootstrapless one so `generateBootstrapVersionRule` never triggers. User-authored
  `CfnParameter`s (9 `new CfnParameter(` sites in aws-cdk-lib itself, effectively unlimited in
  user code) have no runtime "prompt for a value" concept in TF — they need to become TF
  variables at bridge-synth time.
- **`CfnMapping`/`Fn::FindInMap`** for `RegionInfo` fact tables: `Stack.regionalFact`
  (`stack.ts:1238-1258`) is the single chokepoint region-dependent facts (partition, URL
  suffix, ELB account IDs, Lambda Insights layer ARNs, …) flow through; see the `Fn::FindInMap`
  row above. 6 explicit `new CfnMapping(` sites plus every `regionalFact` call.
- **`CfnCondition`**: 5 explicit `new CfnCondition(` sites in aws-cdk-lib (mostly
  `aws-dynamodb`/replica logic and `aws-eks(-v2)` kubectl-provider ECR-partition branching);
  resolvable at synth time whenever the condition only depends on context CDK already knows.
- **`CfnOutput`/exports** (`Stack.exportValue`, `stack.ts:1304-1341`): 32 explicit
  `new CfnOutput(` sites plus every automatic cross-stack reference. Requires `AWS::StackName`
  (export name defaults to `${stackName}:ExportsOutputRef...`) — another reason `AWS::StackName`
  is P0, not just cosmetic.
- **`CfnRule`**: 2 explicit sites, both framework-internal (`stack-synthesizer.ts`,
  bootstrap-version check) — moot once bootstrapless synthesis is used (see above).
- **`Stack.addTransform`** (SAM/macros): see `Fn::Transform` row.
- **`CfnResource.cfnOptions`** (`DeletionPolicy`/`UpdateReplacePolicy`/`Condition`/`Metadata`/
  `CreationPolicy`/`UpdatePolicy`): 49 non-test references to `cfnOptions.deletionPolicy` et
  al. `DeletionPolicy`/`UpdateReplacePolicy` map to Terraform's `lifecycle { prevent_destroy }`
  /`create_before_destroy` at bridge-synth time (a) — no runtime equivalent needed.
  `CreationPolicy`/`UpdatePolicy` (CFN signal-count waiting, mostly ASG/EC2) have **no TF/awscc
  equivalent** (c) — TF has no "wait for N `cfn-signal` calls" primitive.
- **`Stack.availabilityZones`**: see `Fn::GetAZs` row — the environment-agnostic branch is
  exactly the awscc/cdktn case (account/region often only resolvable from a TF data source at
  apply time, so unresolved at CDK-synth time).
- **`Stack.templateOptions`** (`description`, `transforms`, `metadata`, `templateFormatVersion`):
  pure CFN-template-envelope metadata with no TF/HCL equivalent target — (c) drop silently, or
  round-trip into provider-level metadata/tags for parity in generated docs only.
- **`Fn.importValue`/`Stack.exportValue`**: see `Fn::ImportValue` row.
- **`Stack.toJsonString`/`CfnJson`** (2 sites, both `aws-eks(-v2)/lib/service-account.ts:165-166`,
  building an OIDC-trust-policy `Condition` block from token-derived keys): resolvable at
  synth-time in the bridge once the underlying tokens resolve to known TF expressions;
  otherwise needs the `to_json_string` function already in cfncompat.
- **`Stack.splitArn`/`formatArn`** (`arn.ts:139-154`, 306 `.formatArn(`/87 `.splitArn(` call
  sites — the single most common "intrinsic-shaped" operation in aws-cdk-lib): `Arn.format`
  needs `partition`/`region`/`account` all non-null (`arn.ts:145-147` throws otherwise) — this
  is why `AWS::Partition`/`AccountId`/`Region` are P0: nearly a third of a millionth-scale
  aws-cdk-lib's ARN construction goes through this one function.
- **Bootstrapless synthesis assumptions**: `helpers-internal/string-specializer.ts:40-47`
  (`StringSpecializer.specialize`) shows the framework's own admission that `${AWS::Partition}`
  can **never** be resolved at synth time (only `${Qualifier}` always is, `${AWS::Region}`/
  `${AWS::AccountId}` only if concrete) — asset bucket/role names keep a literal
  `${AWS::Partition}` placeholder even in the DefaultStackSynthesizer's own output. A bridge
  targeting awscc must either force a concrete environment (`env: { account, region }`) so
  these specialize away, or supply a synthesizer (bootstrapless / a cdktn-specific one) that
  never emits SSM-bootstrap-version lookups (`DEFAULT_BOOTSTRAP_STACK_VERSION_SSM_PARAMETER`,
  `stack-synthesizers/default-synthesizer.ts:215-219,343-345`) or asset publishing roles at all.

### cfncompat backlog (prioritized)

1. **P0 — pseudo-parameter data source**, e.g. `data "cfncompat_pseudo_parameters" {}` (or
   split: `cfncompat_account_id`, `cfncompat_partition`, `cfncompat_region`) exposing
   `account_id`/`partition`/`region`/`url_suffix` in one call so the bridge doesn't need three
   separate `hashicorp/aws` data sources when targeting awscc-only. Partition and URL suffix in
   particular have zero awscc/hashicorp equivalent (`aws_partition` derives from region;
   nothing derives URL suffix — needs cfncompat's own static table, mirrored from
   `region-info`'s `FactName.URL_SUFFIX`/`region-lookup.ts:97`).
2. **P0 — `data "cfncompat_availability_zones"`** (`Fn::GetAZs` polyfill), matching
   `aws_availability_zones`'s shape but usable when `hashicorp/aws` isn't configured — the
   single most-flagged gap, hit by every environment-agnostic `Stack.availabilityZones` call.
3. **P0 — a modelled stack identity.** `AWS::StackName`/`AWS::StackId` are read by 56+4 sites
   (export names, physical-name fallbacks, custom-resource identifiers) but there is no CFN
   stack in an awscc/TF deployment. Open design question: a `cfncompat_stack` resource that
   mints a stable, deterministic pseudo stack id/ARN (e.g.
   `arn:<partition>:cloudformation:<region>:<account>:stack/<name>/<uuid-from-tf-state>`) and a
   `stack_name` value taken from the cdktn app's stack id, so `Stack.exportValue`-style naming
   and any code keying off `AWS::StackName`/`AWS::StackId` stays stable across `terraform
   plan`/`apply` cycles? Or should the bridge instead special-case these two and substitute
   literal values at synth time, since (unlike account/region) the "stack name" is always known
   statically from the cdktn construct tree? The latter seems simpler and avoids a resource with
   no real backing API — flag for a design decision before implementation.
4. **P1 — cross-stack reference story for `Fn::ImportValue`/`Fn::GetStackOutput`.** 17+1 sites,
   blocks any multi-stack cdktn app that uses `stack.exportValue()` or resource sharing across
   stacks (the common CDK pattern). Two candidate shapes: (a) a `cfncompat_stack_output` data
   source that reads a real CloudFormation stack's `Outputs` (useful for a *mixed* CFN+TF
   estate) or (b) resolve entirely at the cdktn/TF-graph level via cross-stack `remote_state`
   / output references, bypassing cfncompat altogether — needs a decision on whether cdktn
   stacks-of-stacks are modelled as separate TF states (favors (b)) or one flat state (needs
   neither). Recommend documenting (b) as the primary path and treating a cfncompat data source
   as a fallback only for interop with real CFN stacks.
5. **P1 — `Fn::Sub` bridge contract.** `provider::cfncompat::sub` already exists (explicit `variables` map only,
   no recursion, `${!Var}` escape). The remaining work is on the bridge side: rewrite `${LogicalId}` /
   `${LogicalId.Attr}` / `${AWS::Region}` references into explicit `variables` entries before emitting the call.
6. **P1 — document the synth-time-resolvable set explicitly.** `Ref`, `Fn::GetAtt`, and most
   `Fn::If`/`Fn::FindInMap` cases never need a cfncompat function at all if the bridge does
   graph-level substitution during synthesis (this is implicit today but not written down
   anywhere in `docs/`); worth a short "what cfncompat does NOT need to implement" note next to
   `docs/index.md` so contributors don't reflexively add a function for something the bridge
   already resolves.
7. **P2 — `CreationPolicy`/`UpdatePolicy` (signal-count waits).** No TF/awscc primitive exists;
   should be explicitly documented as unsupported (affects ASG rolling-update CDK patterns) so
   consumers get a clear synth-time error instead of silently dropped semantics.
8. **P2 — explicitly unsupported list:** `Fn::Transform`/`AWS::Include`, third-party macros
   (`AWS::Serverless-*`, `AWS::SecretsManager-2024-09-16`), `AWS::NotificationARNs`. None have a
   TF/awscc shape to polyfill; cfncompat should fail synthesis loudly rather than attempt a
   partial emulation.

### Open design questions

- Does a cdktn-targeted synthesizer need to be a genuinely new `IStackSynthesizer`
  implementation (bootstrapless, no `BootstrapVersion` SSM parameter/`CfnRule`, no asset
  publishing roles), or can `BootstraplessSynthesizer` (already shipped in aws-cdk-lib) be
  reused as-is? If reused, does its own `${AWS::Partition}` un-specialized placeholder
  (`string-specializer.ts:38`) leak into any resource property the bridge can't post-process?
- Should `AWS::StackName`/`AWS::StackId` be a real `cfncompat_stack` resource (stateful,
  survives `apply`) or a pure synth-time substitution with no TF-side representation at all?
  The former supports import/interop with a real CFN stack; the latter is simpler and matches
  how account/region/partition are already handled as synth-time-or-data-source values, not
  managed resources.
- For `Fn::ImportValue`/cross-stack references: is a cdktn app-of-stacks always one flat TF
  state (making this a non-issue, since it's just a construct-graph reference) or can stacks be
  split into independent TF states/workspaces (making it a real "read someone else's state"
  problem cfncompat would need to solve with a data source)? This determines whether backlog
  item 4 is needed at all.
- `AWS::NotificationARNs` and CFN stack-level `Metadata`/`TemplateFormatVersion`
  (`templateOptions`) have no deployment-model equivalent in Terraform at all — worth an
  explicit "these pseudo-parameters/template fields are silently dropped" note in
  `docs/index.md` rather than leaving it implicit.
