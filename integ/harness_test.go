package integ

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/random"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"

	harnessaws "github.com/cdktn-io/s3-notifications-harness/integ/aws"
)

func awsRegion() string {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r
	}
	return "us-east-1"
}

// bucketNameFor mirrors CONTRACT.md: stacks B and C derive this deterministically too,
// never reading it from stack A's outputs.
func bucketNameFor(suffix string) string {
	return fmt.Sprintf("s3n-harness-%s", suffix)
}

// TestAwsCdk runs the CONTRACT.md flow against awscdk/. Expected: fully GREEN.
func TestAwsCdk(t *testing.T) {
	suffix := strings.ToLower(random.UniqueId())
	region := awsRegion()
	t.Logf("suite=cdk suffix=%s region=%s", suffix, region)
	runHarness(t, NewCDKSuite(suffix), suffix, region)
}

// TestTerraform runs the identical flow against terraform/. Expected: RED starting at
// "deploy B -> assert config includes {a,b}" -- stack B's aws_s3_bucket_notification
// resource is authoritative over the bucket's whole notification config and replaces
// stack A's target instead of merging with it. validate_b (and later validate stages)
// failing is that expected RED, not a bug in this test: validations use testify's
// non-fatal assert precisely so every later stage still runs and logs more evidence
// (including each RED stage's terraform plan) once that happens.
func TestTerraform(t *testing.T) {
	suffix := strings.ToLower(random.UniqueId())
	region := awsRegion()
	t.Logf("suite=terraform suffix=%s region=%s", suffix, region)
	runHarness(t, NewTerraformSuite(suffix, region), suffix, region)
}

// runHarness drives CONTRACT.md's integ/ flow identically for both suites:
//
//	deploy A      -> assert config includes {a};       upload a/1          -> queue a receives
//	deploy B      -> assert config includes {a,b};     upload a/2,b/2      -> queues a,b receive
//	deploy C      -> assert config includes {a,b,c};   upload a/3,b/3,c/3  -> all receive
//	re-deploy A   -> assert config includes {a,b,c};   upload */4          -> all receive
//	destroy B     -> assert config includes {a,c}, excludes b
//	destroy C, A  (cleanup, deferred; always runs; empties the bucket first)
//
// Only Deploy is fatal (require, inside terratest's shell/terraform helpers); every
// validation is non-fatal (assert) so later stages keep running and logging evidence
// even once one fails.
func runHarness(t *testing.T, suite Suite, suffix, region string) {
	bucket := bucketNameFor(suffix)

	// Deferred cleanup: always runs (even after a require-triggered t.Fatal partway
	// through, since that unwinds via runtime.Goexit through this same goroutine),
	// tolerates any stack never having been (successfully) deployed, and empties the
	// bucket -- all object versions and delete markers -- before destroying the owning
	// stack A, since the Terraform suite's awscc_s3_bucket has no auto-delete-on-destroy
	// equivalent.
	defer test_structure.RunTestStage(t, "cleanup", func() {
		t.Logf("[cleanup] emptying bucket %s before destroying owning stack a", bucket)
		if err := harnessaws.EmptyBucket(t, region, bucket); err != nil {
			t.Logf("[cleanup] EmptyBucket(%s) error (tolerated): %v", bucket, err)
		}
		suite.Destroy(t, "b")
		suite.Destroy(t, "c")
		suite.Destroy(t, "a")
	})

	lambdaArn := map[string]string{}
	queueURL := map[string]string{}
	refreshOutputs := func(x string) {
		out := suite.Outputs(t, x)
		lambdaArn[x] = out["lambda_arn"]
		queueURL[x] = out["queue_url"]
		if got := out["bucket_name"]; got != bucket {
			t.Errorf("stack %s reported bucket_name=%q, want %q", x, got, bucket)
		}
		if got := out["owner"]; got != x {
			t.Errorf("stack %s reported owner=%q, want %q", x, got, x)
		}
	}

	// --- deploy A ---
	test_structure.RunTestStage(t, "deploy_a", func() {
		suite.Deploy(t, "a")
		refreshOutputs("a")
		waitForTargetLive(t, region, bucket, ownerUpload{owner: "a", queueURL: queueURL["a"]})
	})
	test_structure.RunTestStage(t, "validate_a", func() {
		assertNotificationTargets(t, region, bucket, []string{lambdaArn["a"]}, nil)
		assertDelivery(t, region, bucket, 1, []ownerUpload{
			{owner: "a", queueURL: queueURL["a"]},
		})
	})

	// --- deploy B ---
	test_structure.RunTestStage(t, "deploy_b", func() {
		plan := suite.Plan(t, "b")
		t.Logf("[%s] plan for stack b (before apply):\n%s", suite.Name(), plan)
		suite.Deploy(t, "b")
		refreshOutputs("b")
		waitForTargetLive(t, region, bucket, ownerUpload{owner: "b", queueURL: queueURL["b"]})
	})
	test_structure.RunTestStage(t, "validate_b", func() {
		// Expected RED for TestTerraform: see runHarness's doc comment.
		assertNotificationTargets(t, region, bucket, []string{lambdaArn["a"], lambdaArn["b"]}, nil)
		assertDelivery(t, region, bucket, 2, []ownerUpload{
			{owner: "a", queueURL: queueURL["a"]},
			{owner: "b", queueURL: queueURL["b"]},
		})
		assertNoCrossDelivery(t, region, queueURL["a"], bucket, "b/")
	})

	// --- deploy C ---
	test_structure.RunTestStage(t, "deploy_c", func() {
		plan := suite.Plan(t, "c")
		t.Logf("[%s] plan for stack c (before apply):\n%s", suite.Name(), plan)
		suite.Deploy(t, "c")
		refreshOutputs("c")
		waitForTargetLive(t, region, bucket, ownerUpload{owner: "c", queueURL: queueURL["c"]})
	})
	test_structure.RunTestStage(t, "validate_c", func() {
		assertNotificationTargets(t, region, bucket, []string{lambdaArn["a"], lambdaArn["b"], lambdaArn["c"]}, nil)
		assertDelivery(t, region, bucket, 3, []ownerUpload{
			{owner: "a", queueURL: queueURL["a"]},
			{owner: "b", queueURL: queueURL["b"]},
			{owner: "c", queueURL: queueURL["c"]},
		})
		assertNoCrossDelivery(t, region, queueURL["a"], bucket, "c/")
	})

	// --- re-deploy A ---
	test_structure.RunTestStage(t, "redeploy_a", func() {
		plan := suite.Plan(t, "a")
		t.Logf("[%s] plan for stack a (re-deploy, after b and c):\n%s", suite.Name(), plan)
		suite.Deploy(t, "a")
		refreshOutputs("a")
		waitForTargetLive(t, region, bucket, ownerUpload{owner: "a", queueURL: queueURL["a"]})
	})
	test_structure.RunTestStage(t, "validate_redeploy_a", func() {
		assertNotificationTargets(t, region, bucket, []string{lambdaArn["a"], lambdaArn["b"], lambdaArn["c"]}, nil)
		assertDelivery(t, region, bucket, 4, []ownerUpload{
			{owner: "a", queueURL: queueURL["a"]},
			{owner: "b", queueURL: queueURL["b"]},
			{owner: "c", queueURL: queueURL["c"]},
		})
	})

	// --- destroy B ---
	test_structure.RunTestStage(t, "destroy_b", func() {
		suite.Destroy(t, "b")
	})
	test_structure.RunTestStage(t, "validate_after_destroy_b", func() {
		assertNotificationTargets(t, region, bucket, []string{lambdaArn["a"], lambdaArn["c"]}, []string{lambdaArn["b"]})
	})

	// destroy C, A: handled by the deferred "cleanup" stage above.
}
