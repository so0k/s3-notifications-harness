#!/usr/bin/env node
import { App, Environment } from 'aws-cdk-lib';
import { StackA } from '../lib/stack-a';
import { StackB } from '../lib/stack-b';
import { StackC } from '../lib/stack-c';

const app = new App();

const suffix = app.node.tryGetContext('suffix');
if (!suffix) {
  throw new Error(
    "Missing required context 'suffix'. Pass it with `-c suffix=<suffix>` (e.g. `cdk synth -c suffix=k3m9x1`).",
  );
}

const env: Environment = {
  region: process.env.CDK_DEFAULT_REGION ?? 'us-east-1',
  account: process.env.CDK_DEFAULT_ACCOUNT,
};

new StackA(app, `S3nHarnessA-${suffix}`, { suffix, env });
new StackB(app, `S3nHarnessB-${suffix}`, { suffix, env });
new StackC(app, `S3nHarnessC-${suffix}`, { suffix, env });
