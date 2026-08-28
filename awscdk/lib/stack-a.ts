import { Stack, StackProps, RemovalPolicy } from 'aws-cdk-lib';
import * as s3 from 'aws-cdk-lib/aws-s3';
import { Construct } from 'constructs';
import { NotificationTarget } from './notification-target';

export interface StackAProps extends StackProps {
  readonly suffix: string;
}

/** Owns the shared bucket, adds notification target `a` (prefix `a/`). */
export class StackA extends Stack {
  public readonly bucket: s3.Bucket;

  constructor(scope: Construct, id: string, props: StackAProps) {
    super(scope, id, props);

    const { suffix } = props;

    this.bucket = new s3.Bucket(this, 'Bucket', {
      bucketName: `s3n-harness-${suffix}`,
      removalPolicy: RemovalPolicy.DESTROY,
      autoDeleteObjects: true,
    });

    new NotificationTarget(this, 'TargetA', {
      suffix,
      owner: 'a',
      bucket: this.bucket,
    });
  }
}
