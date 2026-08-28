// Sample notification target (CommonJS so awscc `code.zip_file` inline and CDK `Code.fromAsset` share it).
// Forwards every S3 event record to an SQS "results" queue so the terratest harness can observe
// which stack's target received the event. nodejs22.x bundles @aws-sdk v3.
const { SQSClient, SendMessageCommand } = require("@aws-sdk/client-sqs");

const sqs = new SQSClient({});
const queueUrl = process.env.RESULTS_QUEUE_URL;
const owner = process.env.STACK_NAME || "unknown";

exports.handler = async (event) => {
  const records = event.Records || [];
  console.log(JSON.stringify({ owner, records: records.length, event }));
  for (const r of records) {
    const body = {
      owner,
      bucket: r.s3 && r.s3.bucket && r.s3.bucket.name,
      key: decodeURIComponent(((r.s3 && r.s3.object && r.s3.object.key) || "").replace(/\+/g, " ")),
      eventName: r.eventName,
    };
    await sqs.send(new SendMessageCommand({ QueueUrl: queueUrl, MessageBody: JSON.stringify(body) }));
  }
  return { forwarded: records.length };
};
