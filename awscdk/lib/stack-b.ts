import { Stack, StackProps } from 'aws-cdk-lib';
import * as s3 from 'aws-cdk-lib/aws-s3';
import { Construct } from 'constructs';
import { NotificationTarget } from './notification-target';

export interface StackBProps extends StackProps {
  readonly suffix: string;
}

/**
 * References Stack A's bucket by name (never reads Stack A's outputs — the
 * bucket name is derived deterministically per CONTRACT.md) and adds
 * notification target `b` (prefix `b/`).
 */
export class StackB extends Stack {
  constructor(scope: Construct, id: string, props: StackBProps) {
    super(scope, id, props);

    const { suffix } = props;
    const bucketName = `s3n-harness-${suffix}`;
    const bucket = s3.Bucket.fromBucketName(this, 'Bucket', bucketName);

    new NotificationTarget(this, 'TargetB', {
      suffix,
      owner: 'b',
      bucket,
    });
  }
}
