package integ

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"

	harnessaws "github.com/cdktn-io/s3-notifications-harness/integ/aws"
)

// resultMessage mirrors the JSON body every lambda/index.js forwards to its results queue.
type resultMessage struct {
	Owner     string `json:"owner"`
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
	EventName string `json:"eventName"`
}

// assertNotificationTargets fetches bucket's live LambdaFunctionConfigurations (via
// GetBucketNotificationConfiguration) and asserts every ARN in wantArns is present and
// every ARN in unwantArns is absent.
//
// Deliberately non-fatal (testify's assert, not require): a failure here -- the expected
// outcome for TestTerraform starting at "deploy B" -- must not stop later stages from
// running and logging further evidence.
func assertNotificationTargets(t *testing.T, region, bucket string, wantArns, unwantArns []string) {
	t.Helper()

	cfg, err := harnessaws.GetS3BucketNotificationE(t, region, bucket)
	if !assert.NoError(t, err, "GetBucketNotificationConfiguration(%s)", bucket) {
		return
	}

	present := make(map[string]bool, len(cfg.LambdaFunctionConfigurations))
	for _, lc := range cfg.LambdaFunctionConfigurations {
		present[awssdk.ToString(lc.LambdaFunctionArn)] = true
	}
	t.Logf("bucket %s notification targets present: %v", bucket, mapBoolKeys(present))

	for _, arn := range wantArns {
		assert.True(t, present[arn], "expected lambda target %s to be present in %s's notification config", arn, bucket)
	}
	for _, arn := range unwantArns {
		assert.False(t, present[arn], "expected lambda target %s to be ABSENT from %s's notification config (a later stack's deploy likely clobbered it)", arn, bucket)
	}
}

// ownerUpload names one owner's upload/verify pair for assertDelivery: which prefix to
// upload under, and which results queue should receive it.
type ownerUpload struct {
	owner    string // "a" | "b" | "c"
	queueURL string
}

// waitForTargetLive absorbs S3's propagation window for a freshly written notification
// configuration -- AWS documents "about five minutes" before changes take effect, and
// objects put before then are silently not delivered. It uploads throwaway
// "<owner>/warmup-<n>.txt" probes every 20s for up to 6 minutes until the owner's results
// queue echoes one. Call it right after deploying the stack that adds/keeps that target;
// assertDelivery can then stay short and strict. Non-fatal: see assertNotificationTargets.
func waitForTargetLive(t *testing.T, region, bucket string, o ownerUpload) {
	t.Helper()

	const attempts, waitSeconds = 18, 20
	for attempt := 1; attempt <= attempts; attempt++ {
		key := fmt.Sprintf("%s/warmup-%d.txt", o.owner, attempt)
		harnessaws.UploadS3File(t, region, bucket, key, "s3-notifications-harness warmup probe")
		if waitForKey(t, region, o.queueURL, bucket, o.owner+"/warmup-", waitSeconds) {
			t.Logf("owner %s: target live after %d warmup probe(s)", o.owner, attempt)
			return
		}
	}
	assert.Fail(t, "target never went live", "owner %s's results queue (%s) received no warmup probe within %ds", o.owner, o.queueURL, attempts*waitSeconds)
}

// assertDelivery proves each owner's target is live now: it uploads "<owner>/<n>.txt" and
// expects the owner's results queue to echo it back within a short window. Targets are
// expected to have been warmed by waitForTargetLive after their deploy; a single retry
// covers a probe lost at the tail of that window. Non-fatal: see assertNotificationTargets.
func assertDelivery(t *testing.T, region, bucket string, n int, owners []ownerUpload) {
	t.Helper()

	const attempts, waitSeconds = 2, 45
	for _, o := range owners {
		delivered := false
		for attempt := 1; attempt <= attempts && !delivered; attempt++ {
			key := fmt.Sprintf("%s/%d.txt", o.owner, n)
			if attempt > 1 {
				key = fmt.Sprintf("%s/%d-r%d.txt", o.owner, n, attempt)
			}
			harnessaws.UploadS3File(t, region, bucket, key, fmt.Sprintf("s3-notifications-harness payload: %s", key))
			delivered = waitForKey(t, region, o.queueURL, bucket, fmt.Sprintf("%s/%d", o.owner, n), waitSeconds)
			if !delivered {
				t.Logf("owner %s: probe %s not delivered within %ds (attempt %d/%d)", o.owner, key, waitSeconds, attempt, attempts)
			}
		}
		assert.True(t, delivered, "owner %s's results queue (%s) never received any %s/%d* probe across %d attempts", o.owner, o.queueURL, o.owner, n, attempts)
	}
}

// waitForKey polls queueURL, draining (deleting) every message it reads along the way,
// until it sees one whose body matches bucket and a key starting with wantKey (probe
// retries share that prefix) or timeoutSeconds elapses. Returns
// false on timeout or a queue/receive error (logged, not asserted -- the caller decides
// whether that's fatal).
func waitForKey(t *testing.T, region, queueURL, bucket, wantKey string, timeoutSeconds int) bool {
	t.Helper()

	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	for {
		remaining := int(time.Until(deadline).Seconds())
		if remaining <= 0 {
			return false
		}

		msg := harnessaws.WaitForQueueMessage(t, region, queueURL, remaining)
		if msg.Error != nil {
			t.Logf("no (more) messages on %s while waiting for %s: %v", queueURL, wantKey, msg.Error)
			return false
		}

		var body resultMessage
		if err := json.Unmarshal([]byte(msg.MessageBody), &body); err != nil {
			t.Logf("unparseable message body on %s: %v (%q)", queueURL, err, msg.MessageBody)
		}
		if derr := harnessaws.DeleteMessage(t, region, queueURL, msg.ReceiptHandle); derr != nil {
			t.Logf("failed to delete drained message from %s: %v", queueURL, derr)
		}

		if body.Bucket == bucket && strings.HasPrefix(body.Key, wantKey) {
			return true
		}
		t.Logf("drained unrelated message from %s (bucket=%s key=%s), still waiting for %s", queueURL, body.Bucket, body.Key, wantKey)
	}
}

// assertNoCrossDelivery does a single short (15s) poll on queueURL, checking it does NOT
// receive any key under unexpectedPrefix -- e.g. that stack B's target doesn't also receive a/-prefixed
// events meant only for stack A. Kept deliberately cheap (one queue, one short poll, called
// at most once per validate stage) rather than checking every owner pair at every stage:
// that would multiply total suite runtime for marginal extra signal on top of the
// structural assertNotificationTargets check above.
func assertNoCrossDelivery(t *testing.T, region, queueURL, bucket, unexpectedPrefix string) {
	t.Helper()

	msg := harnessaws.WaitForQueueMessage(t, region, queueURL, 15)
	if msg.Error != nil {
		t.Logf("confirmed no message under %s on %s within 15s (expected)", unexpectedPrefix, queueURL)
		return
	}

	if derr := harnessaws.DeleteMessage(t, region, queueURL, msg.ReceiptHandle); derr != nil {
		t.Logf("failed to delete drained message from %s: %v", queueURL, derr)
	}

	var body resultMessage
	_ = json.Unmarshal([]byte(msg.MessageBody), &body)
	assert.False(t, strings.HasPrefix(body.Key, unexpectedPrefix), "queue %s unexpectedly received bucket=%s key=%s -- notification prefix scoping is broken", queueURL, body.Bucket, body.Key)
	t.Logf("queue %s received an unrelated message (bucket=%s key=%s) while polling for absence of %s* -- drained, continuing", queueURL, body.Bucket, body.Key, unexpectedPrefix)
}

func mapBoolKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
