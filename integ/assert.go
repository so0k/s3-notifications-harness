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
// outcome for TestTerraformAwscc and TestTerraformAws (see their doc comments) starting at
// "deploy B" -- must not stop later stages from running and logging further evidence.
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

// probeUntilDelivered uploads throwaway probe objects named "<keyPrefix><attempt>.txt" --
// keyPrefix starts with "<owner>/", so every probe lands under that owner's notification
// filter prefix and all of an owner's probes share one queue-side prefix. Returns true as
// soon as the owner's results queue echoes back any of them, so a probe that arrived late
// still counts on a later attempt's poll.
func probeUntilDelivered(t *testing.T, region, bucket string, o ownerUpload, keyPrefix string, attempts, waitSeconds int) bool {
	t.Helper()

	for attempt := 1; attempt <= attempts; attempt++ {
		key := fmt.Sprintf("%s%d.txt", keyPrefix, attempt)
		harnessaws.UploadS3File(t, region, bucket, key, "s3-notifications-harness probe "+key)
		if waitForKey(t, region, o.queueURL, bucket, keyPrefix, waitSeconds) {
			t.Logf("owner %s: %s* delivered on attempt %d/%d", o.owner, keyPrefix, attempt, attempts)
			return true
		}
		t.Logf("owner %s: probe %s not delivered within %ds (attempt %d/%d)", o.owner, key, waitSeconds, attempt, attempts)
	}
	return false
}

// waitForTargetLive absorbs S3's propagation window for a freshly written notification
// configuration -- AWS documents "about five minutes" before changes take effect, and
// objects put before then are silently not delivered. Call it right after deploying the
// stack that adds/keeps that target; assertDelivery can then stay short and strict.
// Non-fatal: see assertNotificationTargets.
func waitForTargetLive(t *testing.T, region, bucket string, o ownerUpload) {
	t.Helper()

	const attempts, waitSeconds = 18, 20
	if probeUntilDelivered(t, region, bucket, o, o.owner+"/warmup-", attempts, waitSeconds) {
		return
	}
	assert.Fail(t, "target never went live", "owner %s's results queue (%s) received no warmup probe within %ds", o.owner, o.queueURL, attempts*waitSeconds)
}

// assertDelivery proves each owner's target is live now, uploading round n's probes under
// "<owner>/<n>-". Targets are expected to have been warmed by waitForTargetLive after their
// deploy, so the window here is short; the second attempt only covers a probe lost at the
// tail of that window. Non-fatal: see assertNotificationTargets.
func assertDelivery(t *testing.T, region, bucket string, n int, owners []ownerUpload) {
	t.Helper()

	const attempts, waitSeconds = 2, 45
	for _, o := range owners {
		keyPrefix := fmt.Sprintf("%s/%d-", o.owner, n)
		assert.True(t, probeUntilDelivered(t, region, bucket, o, keyPrefix, attempts, waitSeconds),
			"owner %s's results queue (%s) never received any %s* probe across %d attempts", o.owner, o.queueURL, keyPrefix, attempts)
	}
}

// waitForKey polls queueURL, draining (deleting) every message it reads along the way, until
// it sees one whose body matches bucket and a key starting with wantKey, or timeoutSeconds
// elapses. Returns false on timeout or a queue/receive error (logged, not asserted -- the
// caller decides whether that's fatal).
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
// receive any key under unexpectedPrefix -- e.g. that stack B's target doesn't also receive
// a/-prefixed events meant only for stack A. Deliberately cheap (one queue, one short poll,
// at most once per validate stage): checking every owner pair at every stage would multiply
// suite runtime for marginal signal beyond the structural assertNotificationTargets check.
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
