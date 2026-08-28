import * as path from 'path';
import { Duration, CfnOutput } from 'aws-cdk-lib';
import * as lambda from 'aws-cdk-lib/aws-lambda';
import * as sqs from 'aws-cdk-lib/aws-sqs';
import * as s3 from 'aws-cdk-lib/aws-s3';
import { LambdaDestination } from 'aws-cdk-lib/aws-s3-notifications';
import { Construct } from 'constructs';

export interface NotificationTargetProps {
  /** Unique per-test-run suffix, e.g. `k3m9x1`. */
  readonly suffix: string;
  /** Which stack owns this target: `a` | `b` | `c`. */
  readonly owner: 'a' | 'b' | 'c';
  /** The (possibly imported) shared bucket to attach a notification to. */
  readonly bucket: s3.IBucket;
}

/**
 * Shared L3 construct: results SQS queue + lambda target + S3 event notification
 * (prefix `<owner>/`) + the four CfnOutputs every stack must expose per CONTRACT.md.
 */
export class NotificationTarget extends Construct {
  public readonly queue: sqs.Queue;
  public readonly fn: lambda.Function;

  constructor(scope: Construct, id: string, props: NotificationTargetProps) {
    super(scope, id);

    const { suffix, owner, bucket } = props;

    this.queue = new sqs.Queue(this, 'ResultsQueue', {
      queueName: `s3n-harness-${suffix}-${owner}-results`,
    });

    this.fn = new lambda.Function(this, 'Function', {
      functionName: `s3n-harness-${suffix}-${owner}`,
      runtime: lambda.Runtime.NODEJS_22_X,
      handler: 'index.handler',
      code: lambda.Code.fromAsset(path.join(__dirname, '..', '..', 'lambda')),
      timeout: Duration.seconds(30),
      environment: {
        RESULTS_QUEUE_URL: this.queue.queueUrl,
        STACK_NAME: owner,
      },
    });

    this.queue.grantSendMessages(this.fn);

    bucket.addEventNotification(
      s3.EventType.OBJECT_CREATED,
      new LambdaDestination(this.fn),
      { prefix: `${owner}/` },
    );

    // CloudFormation Output logical ids must be alphanumeric ([A-Za-z0-9]) — snake_case ids
    // (the canonical key names in CONTRACT.md) are rejected at CreateStack/UpdateStack
    // validation. Use the PascalCase equivalents here; the integ Go `Suite` adapter for the
    // CDK suite translates these back to the canonical snake_case keys. See CONTRACT.md
    // "Outputs" section.
    new CfnOutput(this, 'BucketNameOutput', {
      value: bucket.bucketName,
    }).overrideLogicalId('BucketName');

    new CfnOutput(this, 'LambdaArnOutput', {
      value: this.fn.functionArn,
    }).overrideLogicalId('LambdaArn');

    new CfnOutput(this, 'QueueUrlOutput', {
      value: this.queue.queueUrl,
    }).overrideLogicalId('QueueUrl');

    new CfnOutput(this, 'OwnerOutput', {
      value: owner,
    }).overrideLogicalId('Owner');
  }
}
