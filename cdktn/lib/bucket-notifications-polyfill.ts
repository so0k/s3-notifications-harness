import { Fn } from "cdktn";
import * as fs from 'fs';
import * as path from 'path';
import { Construct } from 'constructs';
import { S3Bucket } from '@cdktn/provider-aws/lib/s3-bucket';
import { IamRole } from '@cdktn/provider-awscc/lib/iam-role';
import { LambdaFunction } from '@cdktn/provider-awscc/lib/lambda-function';
import { CustomResource } from '@cdktn/provider-cfncompat/lib/custom-resource';
import { ITerraformDependable } from 'cdktn';

export interface BucketNotificationsPolyfillProps {
  /** Unique per-test-run suffix, e.g. `k3m9x1`. */
  readonly suffix: string;
  /** Which stack owns this notification entry: `a` | `b` | `c`. */
  readonly owner: 'a' | 'b' | 'c';
  /** Name of the shared bucket (`s3n-harness-<suffix>`) this custom resource merges a notification into. */
  readonly bucketName: string;
  /** ARN of the target lambda function (this stack's notification-target) to route s3:ObjectCreated:* events to. */
  readonly lambdaArn: string;
  /** S3 key prefix filter for this stack's notification entry, e.g. `a/`. */
  readonly filterPrefix: string;
  /**
   * Resources that must exist before the custom resource's handler puts the merged
   * notification configuration (this stack's own target's lambda permission, plus
   * the bucket itself when this stack owns it).
   */
  readonly dependsOn: ITerraformDependable[];
}

/**
 * Port of terraform/cfncompat/modules/bucket-notifications: polyfills the single
 * authoritative aws_s3_bucket_notification resource with a per-stack
 * cfncompat_custom_resource that drives AWS CDK's own bucket-notifications Lambda
 * handler (lambda/notifications-handler/index.py, copied verbatim) in its
 * "unmanaged" (merge) mode -- Managed = "false" (must be the string, not a bool).
 */
export class BucketNotificationsPolyfill extends Construct {
  public readonly customResource: CustomResource;

  constructor(scope: Construct, id: string, props: BucketNotificationsPolyfillProps) {
    super(scope, id);

    const { suffix, owner, bucketName, lambdaArn, filterPrefix, dependsOn } = props;

    // The one deliberate hashicorp/aws resource in this construct: awscc_s3_bucket has no
    // force_destroy equivalent, and this response bucket cannot be relied on to be empty
    // at destroy time (cfncompat only deletes a response object best-effort). Each stack
    // gets its own response bucket -- better for destroy isolation than one shared bucket.
    const responseBucket = new S3Bucket(this, 'ResponseBucket', {
      bucket: `s3n-harness-${suffix}-${owner}-cfn-responses`,
      forceDestroy: true,
    });

    const handlerRole = new IamRole(this, 'HandlerRole', {
      roleName: `s3n-harness-${suffix}-${owner}-notifications-handler`,
      assumeRolePolicyDocument: Fn.jsonencode({
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
      // GetBucketNotification (read the existing config to merge with) + PutBucketNotification
      // (write the merged config back) on the shared bucket -- matches the permissions AWS
      // CDK's own BucketNotifications construct grants its handler for an "unmanaged"
      // (imported) bucket.
      policies: [
        {
          policyName: 's3-bucket-notifications',
          policyDocument: Fn.jsonencode({
            Version: '2012-10-17',
            Statement: [
              {
                Effect: 'Allow',
                Action: ['s3:GetBucketNotification', 's3:PutBucketNotification'],
                Resource: `arn:aws:s3:::${bucketName}`,
              },
            ],
          }),
        },
      ],
    });

    const handler = new LambdaFunction(this, 'HandlerFunction', {
      functionName: `s3n-harness-${suffix}-${owner}-notifications-handler`,
      role: handlerRole.arn,
      handler: 'index.handler',
      runtime: 'python3.12',
      // Matches AWS CDK's own provisioning of this exact handler (Timeout: 300 --
      // aws-cdk-lib/aws-s3/lib/notifications-resource/notifications-resource-handler.ts)
      // and the custom resource's serviceTimeout below.
      timeout: 300,
      // Inline source, straight from the CDK handler file -- no bundling / zip artifact.
      code: {
        zipFile: fs.readFileSync(
          path.join(__dirname, '..', '..', 'lambda', 'notifications-handler', 'index.py'),
          'utf-8',
        ),
      },
    });

    // Emulates the CloudFormation "Custom::S3BucketNotifications" resource AWS CDK's
    // BucketNotifications construct would declare, driving the same handler with the same
    // request shape it expects.
    this.customResource = new CustomResource(this, 'BucketNotifications', {
      serviceToken: handler.arn,
      resourceType: 'Custom::S3BucketNotifications',
      stackId: `s3n-harness-${suffix}-${owner}`,
      logicalResourceId: 'BucketNotifications',
      responseBucket: responseBucket.bucket,
      serviceTimeout: 300,
      resourceProperties: {
        BucketName: bucketName,
        NotificationConfiguration: {
          LambdaFunctionConfigurations: [
            {
              Events: ['s3:ObjectCreated:*'],
              LambdaFunctionArn: lambdaArn,
              Filter: {
                Key: {
                  FilterRules: [
                    {
                      Name: 'prefix',
                      Value: filterPrefix,
                    },
                  ],
                },
              },
            },
          ],
        },
        // Must be the *string* "false", not a bool: the handler does
        // props.get('Managed', 'true').lower() == 'true'. Unmanaged means merge into the
        // bucket's existing configuration rather than replace it.
        Managed: 'false',
      },
      // The handler must be able to invoke s3:Get/PutBucketNotification before cfncompat
      // invokes it, and the target lambda's own invoke permission (and, for stack A, the
      // bucket itself) must exist before the handler GETs/PUTs the configuration.
      dependsOn: [handlerRole, ...dependsOn],
    });
  }
}
