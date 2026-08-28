// Package integ contains the s3-notifications-harness terratest (Go) suite:
// it drives the same deploy/validate flow (see CONTRACT.md) against both the
// awscdk/ and terraform/ suites via the Suite interface below, so
// harness_test.go's TestAwsCdk, TestTerraformAwscc, TestTerraformAws, and
// TestTerraformCfncompat share one implementation.
package integ

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gruntwork-io/terratest/modules/shell"
	"github.com/gruntwork-io/terratest/modules/terraform"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"
	"github.com/stretchr/testify/require"
)

// Suite drives one full deploy/plan/outputs/destroy lifecycle for a single
// stack letter ("a", "b", or "c") for one of the tool suites, so
// harness_test.go can run the identical CONTRACT.md flow against all of them.
type Suite interface {
	// Name identifies the suite for logging (e.g. "cdk", "terraform").
	Name() string
	// Deploy applies stack x ("a", "b", or "c"). Failures are fatal
	// (require semantics), per CONTRACT.md.
	Deploy(t *testing.T, x string)
	// Plan returns the plan/diff output for stack x without applying it.
	// The caller is expected to t.Log the result.
	Plan(t *testing.T, x string) string
	// Outputs returns CONTRACT.md's output keys (bucket_name, lambda_arn,
	// queue_url, owner) for stack x. Requires Deploy(x) to have run first.
	Outputs(t *testing.T, x string) map[string]string
	// Destroy tears down stack x. Must tolerate the stack never having
	// been (successfully) deployed in this run.
	Destroy(t *testing.T, x string)
}

// ---------------------------------------------------------------------------
// cdkSuite
// ---------------------------------------------------------------------------

// cdkSuite drives the awscdk/ app via the `cdk` CLI (through `npx`), run from
// the awscdk/ directory (relative to integ/, where `go test` runs).
type cdkSuite struct {
	suffix string
}

func NewCDKSuite(suffix string) *cdkSuite {
	return &cdkSuite{suffix: suffix}
}

func (s *cdkSuite) Name() string { return "cdk" }

const cdkWorkDir = "../awscdk"

// stackName returns e.g. "S3nHarnessA-<suffix>" for x == "a".
func (s *cdkSuite) stackName(x string) string {
	return fmt.Sprintf("S3nHarness%s-%s", upper1(x), s.suffix)
}

// outputsFile is a deterministic (not randomly-named) path so a later stage
// -- or a later `go test` invocation with SKIP_deploy_<x>=true against the
// same suffix -- can still find the outputs from a prior Deploy.
func (s *cdkSuite) outputsFile(x string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("s3n-harness-%s-cdk-%s.outputs.json", s.suffix, x))
}

func (s *cdkSuite) Deploy(t *testing.T, x string) {
	name := s.stackName(x)
	outFile := s.outputsFile(x)
	_ = os.Remove(outFile) // start clean so a failed deploy can't leave a stale outputs file behind

	shell.RunCommandAndGetOutput(t, shell.Command{
		Command:    "npx",
		Args:       []string{"cdk", "deploy", name, "-c", "suffix=" + s.suffix, "--require-approval", "never", "--outputs-file", outFile},
		WorkingDir: cdkWorkDir,
	})
}

func (s *cdkSuite) Plan(t *testing.T, x string) string {
	name := s.stackName(x)
	out, err := shell.RunCommandAndGetOutputE(t, shell.Command{
		Command:    "npx",
		Args:       []string{"cdk", "diff", name, "-c", "suffix=" + s.suffix},
		WorkingDir: cdkWorkDir,
	})
	if err != nil {
		// `cdk diff` exits non-zero whenever there are pending changes to apply --
		// that's the normal case here (first deploy of a stack), not a Plan failure.
		t.Logf("cdk diff %s exited non-zero (expected when there are pending changes): %v", name, err)
	}
	return out
}

// cdkOutputKeys maps the CDK app's CfnOutput logical ids to CONTRACT.md's canonical output
// keys. CloudFormation logical ids must be alphanumeric ([A-Za-z0-9]) -- snake_case ids are
// rejected at CreateStack/UpdateStack validation -- so the app emits PascalCase and this
// adapter translates back for the shared assertion helpers.
var cdkOutputKeys = map[string]string{
	"BucketName": "bucket_name",
	"LambdaArn":  "lambda_arn",
	"QueueUrl":   "queue_url",
	"Owner":      "owner",
}

func (s *cdkSuite) Outputs(t *testing.T, x string) map[string]string {
	name := s.stackName(x)
	outFile := s.outputsFile(x)

	data, err := os.ReadFile(outFile)
	require.NoError(t, err, "reading cdk outputs file %s -- did Deploy(%s) run first?", outFile, x)

	var all map[string]map[string]string
	require.NoError(t, json.Unmarshal(data, &all), "parsing cdk outputs file %s", outFile)

	stackOutputs, ok := all[name]
	require.True(t, ok, "no outputs recorded for stack %s in %s (keys present: %v)", name, outFile, mapKeys(all))

	out := make(map[string]string, len(stackOutputs))
	for k, v := range stackOutputs {
		if canonical, ok := cdkOutputKeys[k]; ok {
			k = canonical
		}
		out[k] = v
	}
	return out
}

func (s *cdkSuite) Destroy(t *testing.T, x string) {
	name := s.stackName(x)
	out, err := shell.RunCommandAndGetOutputE(t, shell.Command{
		Command:    "npx",
		Args:       []string{"cdk", "destroy", name, "-c", "suffix=" + s.suffix, "--force"},
		WorkingDir: cdkWorkDir,
	})
	t.Logf("cdk destroy %s:\n%s", name, out)
	if err != nil {
		// Cleanup must tolerate the stack not existing (e.g. its Deploy never ran).
		t.Logf("cdk destroy %s returned an error (tolerated during cleanup): %v", name, err)
	}
}

// ---------------------------------------------------------------------------
// tfSuite
// ---------------------------------------------------------------------------

// tfSuite drives terraform/<provider>/stack-<x> via terratest's terraform
// module, where provider is "awscc" (hashicorp/awscc + hashicorp/aws), "aws"
// (hashicorp/aws only), or "cfncompat" (hashicorp/aws + hashicorp/archive +
// cdktn-io/cfncompat) -- see CONTRACT.md and terraform/README.md. Each
// stack's *terraform.Options (and the temp dir terratest copies its module
// into) is memoized on first use so later stages -- Plan, Outputs, Destroy,
// or a redeploy -- reuse the same working directory and state, instead of
// terratest handing back a fresh, empty-state copy every call.
type tfSuite struct {
	suffix   string
	region   string
	provider string // "awscc", "aws", or "cfncompat"

	mu      sync.Mutex
	options map[string]*terraform.Options // stack letter -> options, memoized on first use
}

// NewTerraformSuite drives terraform/<provider>/stack-{a,b,c}, provider being
// "awscc", "aws", or "cfncompat" (CONTRACT.md's three sibling terraform scenarios).
func NewTerraformSuite(suffix, region, provider string) *tfSuite {
	return &tfSuite{suffix: suffix, region: region, provider: provider, options: map[string]*terraform.Options{}}
}

func (s *tfSuite) Name() string { return "terraform-" + s.provider }

func terraformBinary() string {
	if bin := os.Getenv("TERRAFORM_BINARY"); bin != "" {
		return bin
	}
	return "terraform"
}

// optionsFor returns the memoized *terraform.Options for stack x, copying
// terraform/<provider>/stack-<x> (plus terraform/<provider>/modules and
// lambda/, via the whole-repo copy test_structure.CopyTerraformFolderToTemp
// does -- it preserves the full repo's directory structure under the temp
// dir, so the module's "${path.module}/../../../../lambda/index.js" (or, for
// cfncompat's handler, "lambda/notifications-handler/index.py") and the
// stack's "../modules/notification-target" source (or, for cfncompat,
// "../../aws/modules/notification-target") both resolve unchanged)
// into a temp working dir the first time it's called for that stack.
func (s *tfSuite) optionsFor(t *testing.T, x string) *terraform.Options {
	s.mu.Lock()
	defer s.mu.Unlock()

	if opt, ok := s.options[x]; ok {
		return opt
	}

	stackDir := fmt.Sprintf("terraform/%s/stack-%s", s.provider, x)
	workingDir := test_structure.CopyTerraformFolderToTemp(t, "..", stackDir)
	t.Logf("[terraform-%s] stack %s working dir: %s", s.provider, x, workingDir)

	opt := terraform.WithDefaultRetryableErrors(t, &terraform.Options{
		TerraformDir:    workingDir,
		TerraformBinary: terraformBinary(),
		Vars: map[string]interface{}{
			"suffix": s.suffix,
			"region": s.region,
		},
		NoColor: true,
	})
	s.options[x] = opt
	return opt
}

func (s *tfSuite) Deploy(t *testing.T, x string) {
	terraform.InitAndApply(t, s.optionsFor(t, x))
}

func (s *tfSuite) Plan(t *testing.T, x string) string {
	return terraform.InitAndPlan(t, s.optionsFor(t, x))
}

func (s *tfSuite) Outputs(t *testing.T, x string) map[string]string {
	raw := terraform.OutputAll(t, s.optionsFor(t, x))
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}

func (s *tfSuite) Destroy(t *testing.T, x string) {
	s.mu.Lock()
	opt, ok := s.options[x]
	s.mu.Unlock()
	if !ok {
		t.Logf("[terraform-%s] stack %s was never deployed in this run, nothing to destroy", s.provider, x)
		return
	}

	out, err := terraform.DestroyE(t, opt)
	t.Logf("terraform-%s destroy stack %s:\n%s", s.provider, x, out)
	if err != nil {
		// Cleanup must tolerate the stack not existing / apply having failed partway.
		t.Logf("terraform-%s destroy stack %s returned an error (tolerated during cleanup): %v", s.provider, x, err)
	}
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

func upper1(x string) string {
	if x == "" {
		return x
	}
	return string(x[0]-'a'+'A') + x[1:]
}

func mapKeys(m map[string]map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
