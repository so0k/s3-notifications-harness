package aws

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	terratestaws "github.com/gruntwork-io/terratest/modules/aws"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/testing"
)

// QueueMessageResponse holds one received SQS message, or the error that stopped the wait
// (including a plain timeout, wrapped as an error) -- mirrors the shape of terratest's own
// modules/aws.QueueMessageResponse so callers can pattern-match against it the same way.
type QueueMessageResponse struct {
	ReceiptHandle string
	MessageBody   string
	Error         error
}

// WaitForQueueMessage long-polls queueURL for up to timeoutSeconds (in <=20s cycles, since
// that's SQS's per-call long-poll max) for a single message. It does NOT delete the message
// it receives -- callers that want to drain the queue (recommended, so a later poll doesn't
// see the same message again after its visibility timeout expires) must call DeleteMessage.
func WaitForQueueMessage(t testing.TestingT, region, queueURL string, timeoutSeconds int) QueueMessageResponse {
	client, err := terratestaws.NewSqsClientE(t, region)
	if err != nil {
		return QueueMessageResponse{Error: err}
	}

	cycleLength := 20
	if timeoutSeconds < cycleLength {
		cycleLength = timeoutSeconds
	}
	if cycleLength < 1 {
		cycleLength = 1
	}
	cycles := timeoutSeconds / cycleLength
	if cycles < 1 {
		cycles = 1
	}

	for i := 0; i < cycles; i++ {
		logger.Log(t, fmt.Sprintf("Waiting for message on %s (%ds elapsed)", queueURL, i*cycleLength))

		result, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
			QueueUrl:            awssdk.String(queueURL),
			MaxNumberOfMessages: 1,
			WaitTimeSeconds:     int32(cycleLength),
		})
		if err != nil {
			return QueueMessageResponse{Error: err}
		}

		if len(result.Messages) > 0 {
			m := result.Messages[0]
			logger.Log(t, fmt.Sprintf("Message %s received on %s", awssdk.ToString(m.MessageId), queueURL))
			return QueueMessageResponse{
				ReceiptHandle: awssdk.ToString(m.ReceiptHandle),
				MessageBody:   awssdk.ToString(m.Body),
			}
		}
	}

	return QueueMessageResponse{Error: fmt.Errorf("no message received on %s after %ds", queueURL, timeoutSeconds)}
}

// DeleteMessage deletes a message (by receipt handle) from queueURL, so a later poll on the
// same queue doesn't see it -- or a redelivery of it -- again.
func DeleteMessage(t testing.TestingT, region, queueURL, receiptHandle string) error {
	client, err := terratestaws.NewSqsClientE(t, region)
	if err != nil {
		return err
	}

	_, err = client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
		QueueUrl:      awssdk.String(queueURL),
		ReceiptHandle: awssdk.String(receiptHandle),
	})
	return err
}
