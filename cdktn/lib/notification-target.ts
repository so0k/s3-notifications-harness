import * as fs from 'fs';
import * as path from 'path';
import { Construct } from 'constructs';
import { SqsQueue } from '@cdktn/provider-awscc/lib/sqs-queue';
import { IamRole } from '@cdktn/provider-awscc/lib/iam-role';
import { LambdaFunction } from '@cdktn/provider-awscc/lib/lambda-function';
import { LambdaPermission } from '@cdktn/provider-awscc/lib/lambda-permission';
import { DataAwsCallerIdentity } from '@cdktn/provider-aws/lib/data-aws-caller-identity';

export interface NotificationTargetProps {
  /** Unique per-test-run suffix, e.g. `k3m9x1`. */
  readonly suffix: string;
  /** Which stack owns this target: `a` | `b` | `c`. */
  readonly owner: 'a' | 'b' | 'c';
  /** Name of the shared bucket (`s3n-harness-<suffix>`). */
  readonly bucketName: string;
  /** ARN of the shared bucket, used for the lambda permission's `sourceArn`. */
  readonly bucketArn: string;
}

/**
 * Port of terraform/awscc/modules/notification-target: results SQS queue + lambda
 * target + S3->lambda invoke permission. Used identically by stack a/b/c so every
 * stack's target is built the same way regardless of who owns the underlying bucket.
 */
export class NotificationTarget extends Construct {
  public readonly queue: SqsQueue;
  public readonly fn: LambdaFunction;
  public readonly permission: LambdaPermission;

  constructor(scope: Construct, id: string, props: NotificationTargetProps) {
    super(scope, id);

    const { suffix, owner, bucketName, bucketArn } = props;

    const callerIdentity = new DataAwsCallerIdentity(this, 'CallerIdentity', {});

    this.queue = new SqsQueue(this, 'ResultsQueue', {
      queueName: `s3n-harness-${suffix}-${owner}-results`,
      tags: [
        {
          key: 's3n-harness:bucket',
          value: bucketName,
        },
      ],
    });

    const role = new IamRole(this, 'LambdaRole', {
      roleName: `s3n-harness-${suffix}-${owner}-lambda`,
      assumeRolePolicyDocument: JSON.stringify({
        Version: '2012-10-17',
        Statement: [
          {
            Effect: 'Allow',
            Principal: { Service: 'lambda.amazonaws.com' },
            Action: 'sts:AssumeRole',
          },
        ],
      }),
      managedPolicyArns: ['arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole'],
      policies: [
        {
          policyName: 'sqs-send',
          policyDocument: JSON.stringify({
            Version: '2012-10-17',
            Statement: [
              {
                Effect: 'Allow',
                Action: 'sqs:SendMessage',
                Resource: this.queue.arn,
              },
            ],
          }),
        },
      ],
    });

    this.fn = new LambdaFunction(this, 'Function', {
      functionName: `s3n-harness-${suffix}-${owner}`,
      role: role.arn,
      handler: 'index.handler',
      runtime: 'nodejs22.x',
      code: {
        // Relative to this file's location (__dirname), not the process cwd.
        zipFile: fs.readFileSync(path.join(__dirname, '..', '..', 'lambda', 'index.js'), 'utf-8'),
      },
      environment: {
        variables: {
          RESULTS_QUEUE_URL: this.queue.queueUrl,
          STACK_NAME: owner,
        },
      },
    });

    this.permission = new LambdaPermission(this, 'AllowS3', {
      action: 'lambda:InvokeFunction',
      principal: 's3.amazonaws.com',
      functionName: this.fn.functionName,
      sourceArn: bucketArn,
      sourceAccount: callerIdentity.accountId,
    });
  }
}
