# Plan — cfncompat custom-resource core (`aws-cdk-lib/core` port)

Spec: `~/cdktn/s3-notifications-harness/docs/OPTION-C-PLAN.md` § "Decisions taken".
Worktree: `/Users/vincentsmet/tcons/base-cfncompat`, branch `feat/cfncompat-custom-resource`.
Scope: **core only** — no S3 notifications here. That lands on top of this.

Tracked aws-cdk tag: **v2.233.0** (dominant tag in existing provenance headers, 101 hits).
Every new `src/` file starts with the provenance line as its *first* line, e.g.
`// https://github.com/aws/aws-cdk/blob/v2.233.0/packages/aws-cdk-lib/core/lib/custom-resource.ts`.

## 1. Files

| File | Responsibility |
| --- | --- |
| `src/aws/custom-resource.ts` | `CustomResourceProps` + `CustomResource` L2 wrapping `@cdktn/provider-cfncompat` `customResource.CustomResource`. Verbatim ports of `renderResourceType` / `uppercaseProperties`. |
| `src/aws/custom-resource-handler.ts` | `CustomResourceHandlerProps` + `CustomResourceHandler` — stack-singleton Lambda (tcons `LambdaFunction` + `iam.Role` + `Code`), `getOrCreate` in the `NotificationsResourceHandler.singleton` shape. |
| `src/aws/cfncompat-provider-config.generated.ts` | `jsii-struct-builder` output: `CfncompatProviderConfig` from `@cdktn/provider-cfncompat.provider.CfncompatProviderConfig` `.omit("alias")`. Mirrors `provider-config.generated.ts`. |
| `projenrc/cfncompat-provider-config-struct-builder.ts` | `Component` producing the above; exported from `projenrc/index.ts`, instantiated in `.projenrc.ts` next to `AwsProviderStructBuilder`. |
| `src/aws/aws-stack.ts` (edit) | `cfncompatProviderConfig` prop, `cfncompatProvider` getter, `customResourceResponseBucket` getter. |
| `src/aws/index.ts` (edit) | `export * from "./custom-resource";`, `"./custom-resource-handler"`, `"./cfncompat-provider-config.generated"`. |
| `.projenrc.ts` (edit) | dependency entries — **already applied in this worktree**, see §5. |
| `test/aws/custom-resource.test.ts` | ported `core/test/custom-resource.test.ts`. |
| `test/aws/custom-resource-handler.test.ts` | ported singleton/`getOrCreate` cases. |

## 2. Public API

### `src/aws/custom-resource.ts`

```ts
export interface CustomResourceProps extends AwsConstructProps {
  readonly serviceToken: string;              // ARN of the Lambda/SNS handler
  readonly serviceTimeout?: Duration;         // @default Duration.seconds(3600)
  readonly properties?: { [key: string]: any }; // @default - none
  readonly resourceType?: string;             // @default AWS::CloudFormation::CustomResource
  readonly pascalCaseProperties?: boolean;    // @default false
  readonly responseBucket?: string;           // @default stack.customResourceResponseBucket
  readonly responseKeyPrefix?: string;        // @default - no prefix
}

export interface ICustomResource extends IAwsConstruct {
  readonly ref: string;
}

export class CustomResource extends AwsConstructBase implements ICustomResource {
  public readonly resource: cfncompatCustomResource.CustomResource;
  public get outputs(): Record<string, any>;   // { physicalResourceId: this.ref }
  constructor(scope: Construct, id: string, props: CustomResourceProps);
  public get ref(): string;                    // physical id returned by the handler
  public getAtt(attributeName: string): any;   // `data` lookup = CFN Fn::GetAtt
  public getAttString(attributeName: string): string;
}
```

No `removalPolicy`: Terraform has no `DeletionPolicy` equivalent and the cfncompat resource
always sends `Delete` on destroy.

**Constructor** (order mirrors upstream): (1) `renderResourceType(this, props.resourceType)` —
verbatim port, using `ValidationError(message, scope)` from `src/errors.ts` (drop upstream's
`lit\`…\`` error-code argument); (2) `pascalCaseProperties ? uppercaseProperties(props.properties
?? {}) : (props.properties ?? {})`; (3) `serviceTimeout` range check 1..3600 when
`!isUnresolved()`; (4) `Annotations.of(this).addWarning(...)` when `properties` contains
`ServiceToken`/`ServiceTimeout` (cdktn has no `addWarningV2`); (5) instantiate the binding.

**Prop mapping onto `customResource.CustomResourceConfig`:**

| tcons | cfncompat |
| --- | --- |
| `props.serviceToken` | `serviceToken` (required) |
| `type` from `renderResourceType` | `resourceType` |
| merged `properties` | `resourceProperties` — **`ServiceToken` is merged in by the resource itself**, so do *not* re-inject it the way upstream's `constructPropertiesPassed` does |
| `props.serviceTimeout?.toSeconds()` | `serviceTimeout` (number, not the CFN string) |
| `AwsStack.gridUUID` | `stackId` |
| `stack.uniqueResourceName(this)` | `logicalResourceId` |
| response bucket (§4) | `responseBucket` |
| `props.responseKeyPrefix` | `responseKeyPrefix` |

`ref` → `this.resource.physicalResourceId`.
`getAtt(name)` → `this.resource.data.lookup(name)` (`cdktn.AnyMap.lookup(key): any`).
`getAttString(name)` → `Token.asString(this.getAtt(name))`.

### `src/aws/custom-resource-handler.ts`

```ts
export interface CustomResourceHandlerProps extends AwsConstructProps {
  readonly code: compute.Code;
  readonly runtime: compute.Runtime;
  readonly handler?: string;    // @default "index.handler"
  readonly timeout?: Duration;  // @default Duration.minutes(5)
  readonly role?: iam.IRole;    // @default - new role + basic Lambda execution policy
  readonly description?: string;
  readonly environment?: { [key: string]: string };
}

export class CustomResourceHandler extends AwsConstructBase {
  /** Stack-singleton handler under a well-known id. */
  public static getOrCreate(
    scope: Construct,
    uniqueId: string,
    props: CustomResourceHandlerProps,
  ): CustomResourceHandler;

  public readonly role: iam.IRole;
  public readonly lambdaFunction: compute.LambdaFunction;
  public readonly functionArn: string; // use as CustomResourceProps.serviceToken
  public get outputs(): Record<string, any>;

  constructor(scope: Construct, id: string, props: CustomResourceHandlerProps);
  public addToRolePolicy(statement: iam.PolicyStatement): void;
}
```

`getOrCreate` = `const stack = AwsStack.ofAwsConstruct(scope);` then
`stack.node.tryFindChild(uniqueId) as CustomResourceHandler` ?? `new CustomResourceHandler(stack,
uniqueId, props)`. The caller supplies the well-known id (S3 layer will pass
`BucketNotificationsHandler050a0587b7544547bf325f094a3db834`). Upstream's
`CustomResourceProviderBase` raw-`CfnResource` machinery is **not** ported — `compute.LambdaFunction`
already gives role, code and log group.

### `src/aws/aws-stack.ts`

```ts
export interface AwsStackProps extends StackBaseProps {
  // ...existing...
  /** @default - provider configured with the stack's region */
  readonly cfncompatProviderConfig?: CfncompatProviderConfig;
}

export class AwsStack extends StackBase implements IAwsStack {
  public get cfncompatProvider(): cfncompatProvider.CfncompatProvider;
  public get customResourceResponseBucket(): storage.Bucket | undefined;
}
```

- `cfncompatProvider`: `private cfncompatProviderSingleton?`; on first access
  `new cfncompatProvider.CfncompatProvider(this, "CfncompatProvider", { region: this.region,
  ...(this._cfncompatProviderConfig ?? {}) })`. Store `props.cfncompatProviderConfig` in a
  `private readonly _cfncompatProviderConfig` in the constructor, like `_providerConfig`.
  It lives on `AwsStack` (not `StackBase`) because it needs `this.region`.
- `customResourceResponseBucket`: `undefined` when
  `this._cfncompatProviderConfig?.customResourceBucket` is set; otherwise lazily creates
  `new storage.Bucket(this, "CustomResourceResponsesBucket", { forceDestroy: true })`. The
  `Bucket` L2 already derives `bucketPrefix` via `uniqueResourceNamePrefix` (gridUUID prefix,
  lowercase, `.-`, max 63) when `bucketName` is omitted — do not hand-roll a name. No IAM:
  Terraform does the presigned PUT/GET.
- `CustomResource` resolves its bucket as
  `props.responseBucket ?? stack.customResourceResponseBucket?.bucketName` and passes the
  provider explicitly (`provider: stack.cfncompatProvider`) so the singleton is always
  instantiated.

## 3. Export wiring

`src/aws/index.ts`, in the "core" block next to `./log-retention`:

```ts
export * from "./custom-resource";
export * from "./custom-resource-handler";
export * from "./cfncompat-provider-config.generated";
```

Top-level `src/index.ts` needs no change (it re-exports `./aws`).

## 4. `.projenrc.ts` changes

Already applied in this worktree — verify with `grep -n cfncompat .projenrc.ts`:

```ts
peerDeps: [ ..., "@cdktn/provider-cfncompat@^1.0.0", ],
devDeps:  [ ..., "@cdktn/provider-cfncompat@1.0.0", ],
```

Still to do: add `new CfncompatProviderStructBuilder(project);` in `.projenrc.ts` beside
`new AwsProviderStructBuilder(project)`, then `pnpm exec projen`. Peer ranges of the binding
(`cdktn ^0.24`, `constructs <10.8`) are already satisfied.

## 5. Tests to port

`test/aws/custom-resource.test.ts` — from `core/test/custom-resource.test.ts`, keeping the
`describe('custom resource', ...)` wrapper and **verbatim** test names. Assertions become
`Template.synth(stack)` / `template.expectResources("cfncompat_custom_resource")` instead of
`Template.fromStack`.

| Upstream name | Action |
| --- | --- |
| `simple case provider identified by service token` | port |
| `resource type can be specified` | port |
| `removal policy` | **skip** — no Terraform `DeletionPolicy`; `removalPolicy` is not in the API |
| `resource type must begin with "Custom::"` | port |
| `Custom resource type length must be less than 60 characters` | port |
| `properties can be pascal-cased` | port |
| `pascal-casing of props is disabled by default` | port |
| `set serviceTimeout` | port — assert `service_timeout: 60` (number) |
| `set serviceTimeout with token as seconds` | port — `cdktn.TerraformVariable` in place of `CfnParameter` |
| `throws error when serviceTimeout is set with token as units other than seconds` | port |
| `send warning if customResource construct property key is added to properties` | port — assert the cdktn `Annotations.addWarning` annotation; drop the `@aws-cdk/core:` code |

Plus two tcons-only cases (marked as additions): `stackId is the stack gridUUID`, and
`response bucket is created once per stack and skipped when the provider configures one`.

`test/aws/custom-resource-handler.test.ts` — from
`core/test/custom-resource-provider/custom-resource-provider.test.ts`; only these survive:

| Upstream name | Action |
| --- | --- |
| `minimal configuration` | port (adapted: asserts `aws_lambda_function` + `aws_iam_role`) |
| `addToRolePolicy() can be used to add statements to the inline policy` | port |
| `memorySize, timeout and description` | port (drop `memorySize` if not in `FunctionOptions`) |
| `environment variables` | port |
| `roleArn` | port as the `props.role` case |
| `customize roles` / `role is (not) created if preventSynthesis…` | **skip** — no `PolicySynthesizer`/customizeRoles in tcons |
| `asset metadata added…` / `custom resource provided creates asset in new-style synthesis…` | **skip** — CFN asset metadata has no Terraform analogue |
| `policyStatements can be used to add statements to the inline policy` | **skip** — not in the ported props; use `addToRolePolicy` |
| `describe('latest Lambda node runtime')` block (6 tests) | **skip** — `determineLatestNodeRuntime` region table not ported; runtime is an explicit prop |

Add a tcons-only `getOrCreate returns the same handler for the same unique id`.

## 6. jsii pitfalls

- **No generics, no unions** in exported signatures. `properties` is `{ [key: string]: any }` —
  the binding's own shape — which jsii renders as `Map<string, any>`; legal.
- **Props are `interface`s, never classes.** Upstream's `NotificationsResourceHandlerProps` is a
  `class`; `CustomResourceHandlerProps` must be an `interface` of `readonly` members.
- `AnyMap.lookup(key)` returns **`any`**, so `getAtt` must be declared `: any` (jsii maps it to
  `object`); never leak `cdktn.AnyMap` or `IResolvable | string` out of the public API.
  `getAttString` is the typed convenience wrapper.
- `Duration` is tcons `src/duration.ts`, not `cdktn`. Convert to seconds at the boundary.
- Enums/consts: keep `resourceType` a plain `string` (upstream does), validated at runtime.
- Provider config: do **not** re-export `@cdktn/provider-cfncompat`'s `CfncompatProviderConfig`
  directly — generate the omit-`alias` struct through `jsii-struct-builder` exactly like
  `AwsProviderConfig`, so the type is owned by this assembly.
- `static getOrCreate` returning the class itself is fine in jsii; the `Construct` scope
  parameter must be `constructs.Construct`.
- Every exported symbol needs a doc comment; `jsii` lints `@default` on optional struct members.

## 7. Derivations, recap

- **`stackId`** = `AwsStack.gridUUID`. Stable across synths and unique per stack; the S3 python
  handler derives notification ids as `f"{stack_id}-{hash(...)}"` and filters foreign
  notifications with `n['Id'].startswith(f"{stack_id}-")`, so it must not change between applies.
- **`logicalResourceId`** = `stack.uniqueResourceName(this)` — path-derived, stable, and what the
  handler echoes back as `PhysicalResourceId` when the handler returns none.
- **`responseBucket`** = `props.responseBucket` → else `stack.customResourceResponseBucket?.bucketName`
  → else unset, letting the provider's `customResourceBucket` apply. Apply-time error if all
  three are absent, so `CustomResource` creates the stack bucket unless the provider names one.

## 8. Command loop

```
cd /Users/vincentsmet/tcons/base-cfncompat
pnpm exec projen                      # after .projenrc.ts / projenrc/ edits
pnpm compile
pnpm exec jest test/aws/custom-resource.test.ts
pnpm exec jest test/aws/custom-resource-handler.test.ts
pnpm eslint src/aws/custom-resource*.ts
```
