// Package integ contains the s3-notifications-harness terratest (Go) suite:
// it drives the same deploy/validate flow (see CONTRACT.md) against the awscdk/
// suite, each terraform/ scenario, and the cdktn/ app via the Suite interface
// below, so harness_test.go's TestAwsCdk, TestTerraformAwscc, TestTerraformAws,
// TestTerraformCfncompat, and TestCdktn share one implementation.
package integ

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	cli cliSuite
}

func NewCDKSuite(suffix string) *cdkSuite {
	return &cdkSuite{cli: cliSuite{tool: "cdk", workDir: "../awscdk", suffix: suffix}}
}

func (s *cdkSuite) Name() string { return "cdk" }

// stackName returns e.g. "S3nHarnessA-<suffix>" for x == "a".
func (s *cdkSuite) stackName(x string) string {
	return fmt.Sprintf("S3nHarness%s-%s", upper1(x), s.cli.suffix)
}

func (s *cdkSuite) Deploy(t *testing.T, x string) {
	name := s.stackName(x)
	outFile := s.cli.freshOutputsFile(x)

	out, err := s.cli.run(t, "deploy", name, "-c", "suffix="+s.cli.suffix, "--require-approval", "never", "--outputs-file", outFile)
	require.NoError(t, err, "cdk deploy %s failed:\n%s", name, out)
}

func (s *cdkSuite) Plan(t *testing.T, x string) string {
	name := s.stackName(x)
	return s.cli.plan(t, name, "diff", name, "-c", "suffix="+s.cli.suffix)
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
	outFile := s.cli.outputsFile(x)

	data, err := os.ReadFile(outFile)
	require.NoError(t, err, "reading cdk outputs file %s -- did Deploy(%s) run first?", outFile, x)

	out := make(map[string]string)
	for k, v := range s.cli.stackOutputs(t, data, outFile, name) {
		if canonical, ok := cdkOutputKeys[k]; ok {
			k = canonical
		}
		out[k] = v
	}
	return out
}

func (s *cdkSuite) Destroy(t *testing.T, x string) {
	name := s.stackName(x)
	s.cli.destroy(t, name, "destroy", name, "-c", "suffix="+s.cli.suffix, "--force")
}

// ---------------------------------------------------------------------------
// tfSuite
// ---------------------------------------------------------------------------

// tfSuite drives terraform/<provider>/stack-<x> via terratest's terraform
// module, where provider is "awscc" (hashicorp/awscc + hashicorp/aws), "aws"
// (hashicorp/aws only), or "cfncompat" (hashicorp/awscc + cdktn-io/cfncompat +
// hashicorp/aws) -- see CONTRACT.md and terraform/README.md. Each
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
// "../../awscc/modules/notification-target") both resolve unchanged)
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
// cdktnSuite
// ---------------------------------------------------------------------------

// cdktnSuite drives the cdktn/ TypeScript app (Option B, docs/OPTIONS.md) via the
// `cdktn` CLI (through `npx`), run from the cdktn/ directory (relative to integ/,
// where `go test` runs) so the app's `../lambda` handler paths resolve. Unlike
// cdkSuite/tfSuite, every stage needs SUFFIX in the environment: the app reads it
// directly (see cdktn/main.ts) rather than taking it as a CLI var/context flag.
type cdktnSuite struct {
	cli cliSuite
}

func NewCdktnSuite(suffix string) *cdktnSuite {
	return &cdktnSuite{cli: cliSuite{
		tool:    "cdktn",
		workDir: "../cdktn",
		suffix:  suffix,
		env:     map[string]string{"SUFFIX": suffix},
	}}
}

func (s *cdktnSuite) Name() string { return "cdktn" }

// stackName returns e.g. "s3n-harness-a-<suffix>" for x == "a" -- the TerraformStack
// id cdktn/main.ts constructs each stack with, and so also the stack's cdktf.out
// directory name and its key in an --outputs-file's JSON.
func (s *cdktnSuite) stackName(x string) string {
	return fmt.Sprintf("s3n-harness-%s-%s", x, s.cli.suffix)
}

func (s *cdktnSuite) Deploy(t *testing.T, x string) {
	name := s.stackName(x)
	outFile := s.cli.freshOutputsFile(x)

	out, err := s.cli.run(t, "deploy", name, "--auto-approve", "--outputs-file", outFile)
	if err != nil && strings.Contains(out, "ErrorCode: InternalFailure") {
		// Cloud Control occasionally fails a CreateResource with a bare "InternalFailure"
		// (AWS::IAM::Role); a second apply converges, so retry that one signature.
		t.Logf("[cdktn] %s: Cloud Control InternalFailure, retrying deploy once", name)
		out, err = s.cli.run(t, "deploy", name, "--auto-approve", "--outputs-file", outFile)
	}
	require.NoError(t, err, "cdktn deploy %s failed:\n%s", name, out)
}

func (s *cdktnSuite) Plan(t *testing.T, x string) string {
	name := s.stackName(x)
	return s.cli.plan(t, name, "diff", name)
}

// Outputs reads the outputs file Deploy(x) wrote (cdktn/main.ts's TerraformOutput ids
// already are the canonical snake_case keys -- bucket_name, lambda_arn, queue_url,
// owner -- so, unlike cdkSuite, no key translation is needed). If the file isn't there
// (e.g. Outputs is called in a run that skipped deploy_<x> via SKIP_deploy_<x>=true,
// reusing state from an earlier run), falls back to asking cdktn directly.
func (s *cdktnSuite) Outputs(t *testing.T, x string) map[string]string {
	name := s.stackName(x)
	outFile := s.cli.outputsFile(x)

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Logf("[cdktn] outputs file %s not found (%v); falling back to `cdktn output --outputs-file`", outFile, err)
		out, runErr := s.cli.run(t, "output", name, "--outputs-file", outFile)
		require.NoError(t, runErr, "cdktn output %s failed:\n%s", name, out)
		data, err = os.ReadFile(outFile)
		require.NoError(t, err, "reading cdktn outputs file %s after fallback `cdktn output` -- did Deploy(%s) run first?", outFile, x)
	}

	return s.cli.stackOutputs(t, data, outFile, name)
}

func (s *cdktnSuite) Destroy(t *testing.T, x string) {
	name := s.stackName(x)
	s.cli.destroy(t, name, "destroy", name, "--auto-approve")
}

// ---------------------------------------------------------------------------
// cliSuite -- shared by the two CLI-driven suites (cdkSuite, cdktnSuite)
// ---------------------------------------------------------------------------

// cliSuite is what the cdk and cdktn suites have in common: one `npx <tool>` CLI
// invoked from a sibling directory of integ/ (where `go test` runs), and a JSON
// outputs file per stack. tfSuite shares none of it -- it goes through terratest's
// terraform module rather than a CLI.
type cliSuite struct {
	tool    string
	workDir string
	suffix  string
	env     map[string]string // extra environment for every invocation, nil if none
}

func (c cliSuite) run(t *testing.T, args ...string) (string, error) {
	return shell.RunCommandAndGetOutputE(t, shell.Command{
		Command:    "npx",
		Args:       append([]string{c.tool}, args...),
		WorkingDir: c.workDir,
		Env:        c.env,
	})
}

// outputsFile is a deterministic (not randomly-named) path so a later stage -- or a
// later `go test` invocation with SKIP_deploy_<x>=true against the same suffix -- can
// still find the outputs from a prior Deploy.
func (c cliSuite) outputsFile(x string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("s3n-harness-%s-%s-%s.outputs.json", c.suffix, c.tool, x))
}

func (c cliSuite) freshOutputsFile(x string) string {
	f := c.outputsFile(x)
	_ = os.Remove(f) // start clean so a failed deploy can't leave a stale outputs file behind
	return f
}

// plan runs the tool's diff subcommand for logging. Both CLIs exit non-zero whenever
// there are pending changes to apply -- the normal case here (first deploy of a
// stack), not a Plan failure.
func (c cliSuite) plan(t *testing.T, name string, args ...string) string {
	out, err := c.run(t, args...)
	if err != nil {
		t.Logf("%s diff %s exited non-zero (expected when there are pending changes): %v", c.tool, name, err)
	}
	return out
}

func (c cliSuite) destroy(t *testing.T, name string, args ...string) {
	out, err := c.run(t, args...)
	t.Logf("%s destroy %s:\n%s", c.tool, name, out)
	if err != nil {
		// Cleanup must tolerate the stack not existing (e.g. its Deploy never ran).
		t.Logf("%s destroy %s returned an error (tolerated during cleanup): %v", c.tool, name, err)
	}
}

// stackOutputs picks stack name's entry out of a `--outputs-file` JSON document,
// which is keyed by stack name.
func (c cliSuite) stackOutputs(t *testing.T, data []byte, file, name string) map[string]string {
	var all map[string]map[string]string
	require.NoError(t, json.Unmarshal(data, &all), "parsing %s outputs file %s", c.tool, file)

	stackOutputs, ok := all[name]
	require.True(t, ok, "no outputs recorded for stack %s in %s (keys present: %v)", name, file, mapKeys(all))
	return stackOutputs
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
