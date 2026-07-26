package cli_test

// output_test.go verifies the exit code contract and the stdout summary contract,
// including the machine-readable JSON form.
//
// Verified invariants (T19.5, T19.6):
//
// Exit code contract (T19.5):
//   - OutcomeSuccess from the service returns ExitSuccess (0)
//   - OutcomeCompletedWithGaps from the service returns ExitWithSkips (2)
//   - OutcomeFailed from the service returns ExitFailure (1)
//   - An error from the service (not a summary Outcome) returns ExitFailure (1)
//   - A usage error (invalid flag, unknown subcommand) returns ExitUsage (3)
//
// Stdout summary — human form (T19.6):
//   - By default (no --output flag), Run writes a human-readable summary to out
//   - The human summary mentions the workspace path
//   - The human summary mentions the outcome (success, failure, skipped)
//
// Stdout summary — JSON form (T19.6):
//   - --output json causes Run to emit a JSON document to out
//   - The JSON document is valid JSON (parseable)
//   - The JSON document contains the RunSummary fields (Mode, Outcome, WorkspacePath)
//   - The JSON document is written to out, not errOut
//   - The JSON output for OutcomeCompletedWithGaps still uses exit code ExitWithSkips (2)
//   - The JSON document is still emitted when the service returns OutcomeFailed (scripting
//     consumers must be able to parse the failure reason from the JSON, not only from exit code)

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"mosaic-deploy/internal/cli"
)

// ---------------------------------------------------------------------------
// Exit code contract (T19.5)
// ---------------------------------------------------------------------------

// TestRun_OutcomeSuccess_ReturnsExitSuccess verifies that when the service returns a
// RunSummary with OutcomeSuccess, Run returns ExitSuccess (0).
func TestRun_OutcomeSuccess_ReturnsExitSuccess(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want %d (ExitSuccess) for OutcomeSuccess", code, cli.ExitSuccess)
	}
}

// TestRun_OutcomeCompletedWithGaps_ReturnsExitWithSkips verifies that when the service
// returns OutcomeCompletedWithGaps, Run returns ExitWithSkips (2), distinguishing a run
// that completed but has items requiring manual attention from a full success.
func TestRun_OutcomeCompletedWithGaps_ReturnsExitWithSkips(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: skippedSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitWithSkips {
		t.Errorf("exit code = %d, want %d (ExitWithSkips) for OutcomeCompletedWithGaps", code, cli.ExitWithSkips)
	}
}

// TestRun_OutcomeFailed_ReturnsExitFailure verifies that when the service returns
// OutcomeFailed, Run returns ExitFailure (1).
func TestRun_OutcomeFailed_ReturnsExitFailure(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: failedSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want %d (ExitFailure) for OutcomeFailed", code, cli.ExitFailure)
	}
}

// TestRun_ServiceError_ReturnsExitFailure verifies that when the service returns a non-nil
// error (not a RunSummary Outcome), Run returns ExitFailure (1).
func TestRun_ServiceError_ReturnsExitFailure(t *testing.T) {
	// Arrange
	svc := &spyService{
		deployErr: errors.New("planner error: unknown workflow"),
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", t.TempDir(), "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want %d (ExitFailure) when service returns an error", code, cli.ExitFailure)
	}
}

// TestRun_UsageError_ReturnsExitUsage verifies that a usage error (unknown flag) returns
// ExitUsage (3), which is distinct from a runtime failure exit code.
func TestRun_UsageError_ReturnsExitUsage(t *testing.T) {
	// Arrange
	svc := &spyService{}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--no-such-flag"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d (ExitUsage) for unknown flag", code, cli.ExitUsage)
	}
}

// TestRun_ExitCodeConstants_AreDistinct verifies that the four exit code constants are
// distinct values, so a consumer can distinguish them unambiguously in a script.
func TestRun_ExitCodeConstants_AreDistinct(t *testing.T) {
	codes := map[string]int{
		"ExitSuccess":   cli.ExitSuccess,
		"ExitFailure":   cli.ExitFailure,
		"ExitWithSkips": cli.ExitWithSkips,
		"ExitUsage":     cli.ExitUsage,
	}
	seen := make(map[int]string)
	for name, code := range codes {
		if prev, ok := seen[code]; ok {
			t.Errorf("exit code %d is shared by %q and %q; all exit codes must be distinct", code, prev, name)
		}
		seen[code] = name
	}
}

// ---------------------------------------------------------------------------
// Stdout summary — human form (T19.6)
// ---------------------------------------------------------------------------

// TestRun_DefaultOutput_ContainsWorkspacePath verifies that the default (human-readable)
// summary output includes the workspace path so the user can correlate the output to the
// workspace that was processed.
func TestRun_DefaultOutput_ContainsWorkspacePath(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	var out bytes.Buffer

	// Act
	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &out, &bytes.Buffer{})

	// Assert
	if !strings.Contains(out.String(), workspace) {
		t.Errorf("stdout does not contain workspace path %q; the human summary must name the workspace", workspace)
	}
}

// TestRun_DefaultOutput_ContainsOutcome verifies that the default summary output mentions
// the outcome so the user can tell at a glance whether the run succeeded.
func TestRun_DefaultOutput_ContainsOutcome(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	var out bytes.Buffer

	// Act
	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &out, &bytes.Buffer{})

	// Assert
	output := out.String()
	if output == "" {
		t.Error("stdout is empty; the human summary must contain at least the outcome")
	}
	// The outcome string "success" should appear somewhere in the output.
	if !strings.Contains(strings.ToLower(output), "success") {
		t.Errorf("stdout %q does not contain outcome word 'success'", output)
	}
}

// TestRun_DefaultOutput_FailedOutcome_ContainsFailureWord verifies that when the service
// returns OutcomeFailed, the human-readable summary mentions "fail" or "error" so the user
// can tell at a glance that the run did not succeed. A broken formatter that always prints
// "success" would pass the ContainsOutcome test but fail this one.
func TestRun_DefaultOutput_FailedOutcome_ContainsFailureWord(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: failedSummary(workspace)}
	var out bytes.Buffer

	// Act
	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &out, &bytes.Buffer{})

	// Assert
	output := strings.ToLower(out.String())
	if output == "" {
		t.Error("stdout is empty; the human summary must be written even on failure")
	}
	if !strings.Contains(output, "fail") && !strings.Contains(output, "error") {
		t.Errorf("stdout %q does not contain 'fail' or 'error'; the human summary must indicate a failed outcome", out.String())
	}
}

// TestRun_DefaultOutput_CompletedWithSkips_ContainsSkipWord verifies that when the service
// returns OutcomeCompletedWithGaps, the human-readable summary mentions "skip" or "gap" so
// the user knows items were skipped and can find them in the TODO list.
func TestRun_DefaultOutput_CompletedWithSkips_ContainsSkipWord(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: skippedSummary(workspace)}
	var out bytes.Buffer

	// Act
	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &out, &bytes.Buffer{})

	// Assert
	output := strings.ToLower(out.String())
	if output == "" {
		t.Error("stdout is empty; the human summary must be written even when items were skipped")
	}
	if !strings.Contains(output, "skip") && !strings.Contains(output, "gap") {
		t.Errorf("stdout %q does not contain 'skip' or 'gap'; the human summary must indicate that some items were skipped", out.String())
	}
}

// ---------------------------------------------------------------------------
// Stdout summary — JSON form (T19.6)
// ---------------------------------------------------------------------------

// TestRun_JsonOutput_EmitsValidJSON verifies that --output json causes Run to write a
// JSON document that can be parsed without error.
func TestRun_JsonOutput_EmitsValidJSON(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	var out bytes.Buffer

	// Act
	cli.Run(context.Background(),
		[]string{"deploy", "--output", "json", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &out, &bytes.Buffer{})

	// Assert
	if out.Len() == 0 {
		t.Fatal("stdout is empty; --output json must emit a JSON document")
	}
	var parsed any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Errorf("stdout is not valid JSON: %v\nraw output: %s", err, out.String())
	}
}

// TestRun_JsonOutput_ContainsModeField verifies that the JSON output includes the Mode
// field from domain.RunSummary.
func TestRun_JsonOutput_ContainsModeField(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	var out bytes.Buffer

	// Act
	cli.Run(context.Background(),
		[]string{"deploy", "--output", "json", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &out, &bytes.Buffer{})

	// Assert
	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if _, ok := parsed["Mode"]; !ok {
		t.Error("JSON output does not contain 'Mode' field; the JSON form must be domain.RunSummary field-for-field")
	}
}

// TestRun_JsonOutput_ContainsOutcomeField verifies that the JSON output includes the
// Outcome field from domain.RunSummary.
func TestRun_JsonOutput_ContainsOutcomeField(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	var out bytes.Buffer

	// Act
	cli.Run(context.Background(),
		[]string{"deploy", "--output", "json", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &out, &bytes.Buffer{})

	// Assert
	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if _, ok := parsed["Outcome"]; !ok {
		t.Error("JSON output does not contain 'Outcome' field")
	}
}

// TestRun_JsonOutput_ContainsWorkspacePathField verifies that the JSON output includes the
// WorkspacePath field from domain.RunSummary.
func TestRun_JsonOutput_ContainsWorkspacePathField(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	var out bytes.Buffer

	// Act
	cli.Run(context.Background(),
		[]string{"deploy", "--output", "json", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &out, &bytes.Buffer{})

	// Assert
	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if _, ok := parsed["WorkspacePath"]; !ok {
		t.Error("JSON output does not contain 'WorkspacePath' field")
	}
}

// TestRun_JsonOutput_WrittenToOut_NotErrOut verifies that the JSON document is written to
// the out writer (stdout), not errOut (stderr), so that pipeline consumers can capture it.
func TestRun_JsonOutput_WrittenToOut_NotErrOut(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	var out, errOut bytes.Buffer

	// Act
	cli.Run(context.Background(),
		[]string{"deploy", "--output", "json", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &out, &errOut)

	// Assert
	if out.Len() == 0 {
		t.Error("stdout is empty; JSON must be written to out, not errOut")
	}
	// errOut may contain diagnostic messages but not the JSON summary itself.
	if errOut.Len() > 0 {
		// If errOut is non-empty, verify it is not valid JSON (diagnostic messages are fine).
		var parsed any
		if json.Unmarshal(errOut.Bytes(), &parsed) == nil {
			t.Error("errOut contains valid JSON; the machine-readable JSON summary must go to out (stdout)")
		}
	}
}

// TestRun_JsonOutput_CompletedWithSkips_ExitCodeIsExitWithSkips verifies that --output json
// does not change the exit code mapping: OutcomeCompletedWithGaps still returns ExitWithSkips.
func TestRun_JsonOutput_CompletedWithSkips_ExitCodeIsExitWithSkips(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: skippedSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--output", "json", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitWithSkips {
		t.Errorf("exit code = %d, want %d (ExitWithSkips); --output json must not change exit code mapping", code, cli.ExitWithSkips)
	}
}

// TestRun_JsonOutput_FailedOutcome_EmitsJSON verifies that --output json still emits a
// parseable JSON document when the service returns OutcomeFailed. Scripting consumers need
// to be able to capture the JSON from stdout to inspect the failure reason; if the formatter
// falls back to plain-text error output on failure, pipeline consumers cannot parse the result.
func TestRun_JsonOutput_FailedOutcome_EmitsJSON(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: failedSummary(workspace)}
	var out bytes.Buffer

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--output", "json", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &out, &bytes.Buffer{})

	// Assert: exit code must reflect failure.
	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want %d (ExitFailure) for OutcomeFailed with --output json", code, cli.ExitFailure)
	}
	// A JSON document must still be written to out so scripting consumers can parse it.
	if out.Len() == 0 {
		t.Fatal("stdout is empty; --output json must emit a JSON document even on failure")
	}
	var parsed any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Errorf("stdout is not valid JSON on failure: %v\nraw output: %s", err, out.String())
	}
}
