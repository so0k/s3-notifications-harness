import { Construct } from 'constructs';
import { App, LocalBackend, TerraformOutput, TerraformStack } from 'cdktn';
import { AwsccProvider } from '@cdktn/provider-awscc/lib/provider';
import { AwsProvider } from '@cdktn/provider-aws/lib/provider';
import { CfncompatProvider } from '@cdktn/provider-cfncompat/lib/provider';
import { S3Bucket } from '@cdktn/provider-awscc/lib/s3-bucket';
import { NotificationTarget } from './lib/notification-target';
import { BucketNotificationsPolyfill } from './lib/bucket-notifications-polyfill';

// Deliberately a 1:1 port of terraform/cfncompat/ (same resource names and shapes),
// so a divergence between TestCdktn and TestTerraformCfncompat isolates the
// construct/binding/CLI layer from the cfncompat provider itself.
// See CONTRACT.md and docs/OPTIONS.md (Option B).

const suffixEnv = process.env.SUFFIX;
if (!suffixEnv) {
  throw new Error(
    "Missing required env var 'SUFFIX'. Set it before synth/deploy (e.g. `SUFFIX=k3m9x1 npx cdktn synth`).",
  );
}
const suffix: string = suffixEnv;

const region = process.env.AWS_REGION || 'us-east-1';

type Owner = 'a' | 'b' | 'c';

class S3nHarnessStack extends TerraformStack {
  constructor(scope: Construct, id: string, owner: Owner) {
    super(scope, id);

    new LocalBackend(this, { path: `terraform.${id}.tfstate` });

    new AwsccProvider(this, 'awscc', { region });
    new AwsProvider(this, 'aws', { region });
    new CfncompatProvider(this, 'cfncompat', { region });

    const bucketName = `s3n-harness-${suffix}`;
    const bucketArn = `arn:aws:s3:::${bucketName}`;

    // Stack A owns the shared bucket, with *no* notification_configuration of its
    // own -- every stack's target, including A's own, is attached purely via
    // BucketNotificationsPolyfill's cfncompat_custom_resource. See CONTRACT.md's
    // "Stack A specifics" (terraform/cfncompat) and
    // terraform/cfncompat/stack-a/main.tf.
    let ownedBucket: S3Bucket | undefined;
    let effectiveBucketName = bucketName;
    let effectiveBucketArn = bucketArn;

    if (owner === 'a') {
      ownedBucket = new S3Bucket(this, 'Bucket', {
        bucketName: bucketName,
      });
      effectiveBucketName = ownedBucket.bucketName;
      effectiveBucketArn = ownedBucket.arn;
    }

    const target = new NotificationTarget(this, `Target${owner.toUpperCase()}`, {
      suffix,
      owner,
      bucketName: effectiveBucketName,
      bucketArn: effectiveBucketArn,
    });

    new BucketNotificationsPolyfill(this, 'BucketNotifications', {
      suffix,
      owner,
      bucketName: effectiveBucketName,
      lambdaArn: target.fn.arn,
      filterPrefix: `${owner}/`,
      // The s3 permission allowing this lambda to be invoked must exist before the
      // custom resource's handler puts the notification configuration, and (for
      // stack A) the bucket itself must exist before the handler GETs/PUTs it.
      dependsOn: ownedBucket ? [target.permission, ownedBucket] : [target.permission],
    });

    new TerraformOutput(this, 'bucket_name', { value: effectiveBucketName });
    new TerraformOutput(this, 'lambda_arn', { value: target.fn.arn });
    new TerraformOutput(this, 'queue_url', { value: target.queue.queueUrl });
    new TerraformOutput(this, 'owner', { value: owner });
  }
}

const app = new App();

new S3nHarnessStack(app, `s3n-harness-a-${suffix}`, 'a');
new S3nHarnessStack(app, `s3n-harness-b-${suffix}`, 'b');
new S3nHarnessStack(app, `s3n-harness-c-${suffix}`, 'c');

app.synth();
