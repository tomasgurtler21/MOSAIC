package cli

// exitcode_whitebox_test.go provides white-box coverage of the printTestSummary
// function and the exit-code mapping in runTestSubcmd. These tests live in
// package cli (not cli_test) so they can reach the unexported printTestSummary
// function directly.
//
// Why a separate file: the public-API tests in test_test.go (package cli_test)
// cannot reach unexported symbols. The ExitSuccess path in runTestSubcmd
// (`if summary.AllPass { return ExitSuccess }`) has no unit coverage because
// RunTestCommand always fails at the deploy step in tests (no real binary).
// Testing printTestSummary directly with a crafted TestSummary covers the
// observable output for the AllPass=true case and documents the behavior
// independently of the deployment infrastructure.

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-run/internal/testrun"
)

// TestPrintTestSummary_AllPassTrue_OutputsResultPass verifies that when
// summary.AllPass is true, printTestSummary writes "Result: PASS" to out.
// This is the observable output counterpart to the ExitSuccess exit-code
// branch in runTestSubcmd: both branch on the same AllPass field.
func TestPrintTestSummary_AllPassTrue_OutputsResultPass(t *testing.T) {
	var out bytes.Buffer
	summary := &testrun.TestSummary{
		AllPass:    true,
		TotalPass:  1,
		TotalFail:  0,
		TotalError: 0,
	}
	printTestSummary(&out, summary)
	got := out.String()
	if !strings.Contains(got, "Result: PASS") {
		t.Errorf("printTestSummary(AllPass=true) output does not contain \"Result: PASS\";\ngot: %q", got)
	}
	if strings.Contains(got, "Result: FAIL") {
		t.Errorf("printTestSummary(AllPass=true) output contains \"Result: FAIL\"; want only \"Result: PASS\";\ngot: %q", got)
	}
}

// TestPrintTestSummary_AllPassFalse_OutputsResultFail verifies that when
// summary.AllPass is false, printTestSummary writes "Result: FAIL" to out.
// This ensures the pass/fail output branches are both exercised.
func TestPrintTestSummary_AllPassFalse_OutputsResultFail(t *testing.T) {
	var out bytes.Buffer
	summary := &testrun.TestSummary{
		AllPass:    false,
		TotalPass:  0,
		TotalFail:  1,
		TotalError: 0,
	}
	printTestSummary(&out, summary)
	got := out.String()
	if !strings.Contains(got, "Result: FAIL") {
		t.Errorf("printTestSummary(AllPass=false) output does not contain \"Result: FAIL\";\ngot: %q", got)
	}
	if strings.Contains(got, "Result: PASS") {
		t.Errorf("printTestSummary(AllPass=false) output contains \"Result: PASS\"; want only \"Result: FAIL\";\ngot: %q", got)
	}
}

// TestPrintTestSummary_DeployError_OutputsDeployFailed verifies that when
// summary.DeployError is non-nil, printTestSummary reports the deploy failure
// and returns early (no per-harness or totals output). This covers the
// short-circuit path in printTestSummary.
func TestPrintTestSummary_DeployError_OutputsDeployFailed(t *testing.T) {
	var out bytes.Buffer
	deployErr := &fakeError{msg: "binary not found: mosaic-run"}
	summary := &testrun.TestSummary{
		AllPass:     false,
		DeployError: deployErr,
	}
	printTestSummary(&out, summary)
	got := out.String()
	if !strings.Contains(got, "DEPLOY FAILED") {
		t.Errorf("printTestSummary(DeployError set) output does not contain \"DEPLOY FAILED\";\ngot: %q", got)
	}
	// The error message from DeployError must appear in the output.
	if !strings.Contains(got, deployErr.msg) {
		t.Errorf("printTestSummary(DeployError set) output does not contain error message %q;\ngot: %q",
			deployErr.msg, got)
	}
	// With a deploy error, the summary must NOT print per-harness results or totals.
	if strings.Contains(got, "Result:") {
		t.Errorf("printTestSummary(DeployError set) output contains \"Result:\"; "+
			"deploy-failed path must return early before the result line;\ngot: %q", got)
	}
}

// TestExitCodeMapping_AllPassTrue_MapsToExitSuccess documents the exit-code
// contract from the runTestSubcmd body:
//
//	if summary.AllPass { return ExitSuccess }
//	return ExitFailure
//
// Since runTestSubcmd is unexported and requires a fully wired cobra.Command,
// this test encodes the mapping as a pure boolean assertion. It serves as
// executable documentation: if ExitSuccess or ExitFailure are ever reassigned
// to unexpected values, the TestRunTestCommand_* tests in test_test.go will
// catch the observable effect on exit codes.
//
// The intent here is to pin the CONTRACT: AllPass=true => ExitSuccess (0),
// AllPass=false => ExitFailure (1). This test fails if the constants diverge
// from their expected values, which would invalidate the mapping's semantics.
func TestExitCodeMapping_AllPassTrue_MapsToExitSuccess(t *testing.T) {
	// ExitSuccess must be 0 per CLI conventions.
	if ExitSuccess != 0 {
		t.Errorf("ExitSuccess = %d, want 0; the AllPass=true branch must return 0 for shell consumers", ExitSuccess)
	}
	// ExitFailure must be non-zero.
	if ExitFailure == 0 {
		t.Errorf("ExitFailure = %d, must not be 0; the AllPass=false branch must return non-zero", ExitFailure)
	}
	// ExitFailure and ExitSuccess must be distinct.
	if ExitSuccess == ExitFailure {
		t.Errorf("ExitSuccess (%d) == ExitFailure (%d); these must be distinct exit codes", ExitSuccess, ExitFailure)
	}
	// ExitUsage must be distinct from both.
	if ExitUsage == ExitSuccess || ExitUsage == ExitFailure {
		t.Errorf("ExitUsage (%d) collides with ExitSuccess (%d) or ExitFailure (%d); all three must be distinct",
			ExitUsage, ExitSuccess, ExitFailure)
	}
}

// fakeError is a minimal error implementation for test fixtures.
type fakeError struct{ msg string }

func (e *fakeError) Error() string { return e.msg }
