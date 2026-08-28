import * as path from 'path';
import { App, LocalBackend, TerraformOutput } from 'cdktn';
import { aws } from 'terraconstructs';

// Option C: a cdktn app built directly on TerraConstructs' own `Bucket.addEventNotification`
// (src/aws/storage/bucket-notifications-resource.ts on the feat/cfncompat-custom-resource
// branch of terraconstructs/base), rather than a polyfill construct local to this harness
// (that's cdktn/, Option B). Same three-stack contract as cdktn/main.ts and
// terraform/cfncompat/ -- see CONTRACT.md and docs/OPTION-C-PLAN.md.
//
// The `@terraconstructs/aws-s3:keepNotificationInImportedBucket` context key (set in
// cdktf.json) forces every stack -- including stack A, which owns the bucket -- through
// the `Custom::S3BucketNotifications` custom resource (cfncompat_custom_resource) instead
// of the native `aws_s3_bucket_notification` resource, so stack A can share its bucket
// with stacks B/C without any of their applies clobbering another's notification entry.
// Imported buckets (B/C's `Bucket.fromBucketName`) always use the custom resource
// regardless of the context key -- it's the only way to add notifications to a bucket the
// stack does not own.

const suffixEnv = process.env.SUFFIX;
if (!suffixEnv) {
  throw new Error(
    "Missing required env var 'SUFFIX'. Set it before synth/deploy (e.g. `SUFFIX=k3m9x1 npx cdktn synth`).",
  );
}
const suffix: string = suffixEnv;

const region = process.env.AWS_REGION || 'us-east-1';

type Owner = 'a' | 'b' | 'c';

function buildStack(app: App, owner: Owner): void {
  const id = `s3n-harness-${owner}-${suffix}`;

  const stack = new aws.AwsStack(app, id, {
    // Must start with a letter and be <=36 chars -- see stack-base.ts's
    // VALID_GRID_UUID_REGEX. Unique per stack so physical-name prefixes derived from it
    // (for constructs that don't get an explicit name below) never collide across the
    // three stacks or across concurrent harness runs.
    gridUUID: `g-${owner}-${suffix}`,
    environmentName: suffix,
    providerConfig: { region },
  });

  new LocalBackend(stack, { path: `terraform.${id}.tfstate` });

  const bucketName = `s3n-harness-${suffix}`;

  // Stack A owns the shared bucket; B and C only ever import it by name, deriving it the
  // same way A does -- neither reads A's outputs (CONTRACT.md).
  const bucket: aws.storage.IBucket =
    owner === 'a'
      ? new aws.storage.Bucket(stack, 'Bucket', {
          bucketName,
          forceDestroy: true,
        })
      : aws.storage.Bucket.fromBucketName(stack, 'Bucket', bucketName);

  const queue = new aws.notify.Queue(stack, 'ResultsQueue', {
    queueName: `s3n-harness-${suffix}-${owner}-results`,
  });

  const fn = new aws.compute.LambdaFunction(stack, 'Function', {
    functionName: `s3n-harness-${suffix}-${owner}`,
    runtime: aws.compute.Runtime.NODEJS_22_X,
    handler: 'index.handler',
    // Shared with every other suite in this harness (awscdk/, terraform/, cdktn/):
    // CommonJS, no bundling beyond what nodejs22.x provides (@aws-sdk/client-sqs is in
    // the managed runtime). fromAsset takes a directory (or .zip); ../lambda is the
    // directory holding index.js.
    code: aws.compute.Code.fromAsset(path.join(__dirname, '..', 'lambda')),
    environment: {
      RESULTS_QUEUE_URL: queue.queueUrl,
      STACK_NAME: owner,
    },
  });
  // Grants the lambda's role sqs:SendMessage on this stack's results queue -- the S3
  // invoke permission itself is granted automatically by
  // `storage.targets.FunctionDestination.bind()` below.
  queue.grantSendMessages(fn);

  bucket.addEventNotification(
    aws.storage.EventType.OBJECT_CREATED,
    new aws.storage.targets.FunctionDestination(fn),
    { prefix: `${owner}/` },
  );

  // Flat outputs with fixed (non-hashed) ids matching CONTRACT.md's canonical keys
  // exactly, for the integ Go Suite adapter.
  new TerraformOutput(stack, 'bucket_name', { value: bucketName, staticId: true });
  new TerraformOutput(stack, 'lambda_arn', { value: fn.functionArn, staticId: true });
  new TerraformOutput(stack, 'queue_url', { value: queue.queueUrl, staticId: true });
  new TerraformOutput(stack, 'owner', { value: owner, staticId: true });
}

const app = new App();

buildStack(app, 'a');
buildStack(app, 'b');
buildStack(app, 'c');

app.synth();
