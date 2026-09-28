package testcheck_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/testcheck"
)

// ============================================================
// Check tests: exit code comparison
// ============================================================

func TestCheck_ExitCodeMatch_EmptySequences_Passes(t *testing.T) {
	dir := t.TempDir()
	logPath := emptyDispatchLog(t, dir)
	expected := &testcheck.ExpectedOutcome{
		ExitCode:   0,
		Dispatches: []testcheck.ExpectedDispatch{},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if !result.Pass {
		t.Errorf("Pass = false, want true; mismatch: %+v", result.Mismatch)
	}
	if result.Mismatch != nil {
		t.Errorf("Mismatch = %+v, want nil", result.Mismatch)
	}
}

func TestCheck_ExitCodeMismatch_ReportsMismatchExitCodeKind(t *testing.T) {
	dir := t.TempDir()
	logPath := emptyDispatchLog(t, dir)
	expected := &testcheck.ExpectedOutcome{
		ExitCode:   0,
		Dispatches: []testcheck.ExpectedDispatch{},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 1, DispatchLog: logPath}, expected)

	if result.Pass {
		t.Error("Pass = true, want false when exit codes differ")
	}
	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil when exit codes differ")
	}
	if result.Mismatch.Kind != testcheck.MismatchExitCode {
		t.Errorf("Mismatch.Kind = %v, want MismatchExitCode", result.Mismatch.Kind)
	}
	if result.Mismatch.Index != -1 {
		t.Errorf("Mismatch.Index = %d, want -1 for exit-code mismatch", result.Mismatch.Index)
	}
}

func TestCheck_ExitCodeMismatch_ReportsExpectedAndActualValues(t *testing.T) {
	dir := t.TempDir()
	logPath := emptyDispatchLog(t, dir)
	expected := &testcheck.ExpectedOutcome{ExitCode: 0}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 5, DispatchLog: logPath}, expected)

	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	// ExpectedValue should contain "0", ActualValue should contain "5".
	if !strings.Contains(result.Mismatch.ExpectedValue, "0") {
		t.Errorf("Mismatch.ExpectedValue = %q, want it to contain %q", result.Mismatch.ExpectedValue, "0")
	}
	if !strings.Contains(result.Mismatch.ActualValue, "5") {
		t.Errorf("Mismatch.ActualValue = %q, want it to contain %q", result.Mismatch.ActualValue, "5")
	}
}

func TestCheck_NonZeroExitCodeExpectedAndMatches_Passes(t *testing.T) {
	// Deviation workflows expect non-zero exit codes; those should pass.
	dir := t.TempDir()
	logPath := emptyDispatchLog(t, dir)
	expected := &testcheck.ExpectedOutcome{
		ExitCode:   5,
		Dispatches: []testcheck.ExpectedDispatch{},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 5, DispatchLog: logPath}, expected)

	if !result.Pass {
		t.Errorf("Pass = false, want true when non-zero exit codes match; mismatch: %+v", result.Mismatch)
	}
}

// ============================================================
// Check tests: dispatch sequence comparison
// ============================================================

func TestCheck_DispatchSequenceExactMatch_Passes(t *testing.T) {
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("writer#1", "write tests"),
		responseLine("writer#1", "SUCCESS"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "writer", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if !result.Pass {
		t.Errorf("Pass = false, want true for exact match; mismatch: %+v", result.Mismatch)
	}
}

func TestCheck_AgentMatchedByPrefix(t *testing.T) {
	// Expected "mosaictest-scripted" must match actual "mosaictest-scripted#1".
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("mosaictest-scripted#1", "run workflow"),
		responseLine("mosaictest-scripted#1", "SUCCESS"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "mosaictest-scripted", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if !result.Pass {
		t.Errorf("Pass = false, want true; prefix match should work; mismatch: %+v", result.Mismatch)
	}
}

func TestCheck_MissingDispatch_ReportsMismatchMissingDispatch(t *testing.T) {
	// Expected two dispatches, actual log has only one.
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("agent-a#1", "task A"),
		responseLine("agent-a#1", "SUCCESS"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "agent-a", ExpectStatus: "SUCCESS"},
			{Agent: "agent-b", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if result.Pass {
		t.Error("Pass = true, want false when expected has more dispatches than actual")
	}
	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if result.Mismatch.Kind != testcheck.MismatchMissingDispatch {
		t.Errorf("Mismatch.Kind = %v, want MismatchMissingDispatch", result.Mismatch.Kind)
	}
	if result.Mismatch.Index != 1 {
		t.Errorf("Mismatch.Index = %d, want 1 (first missing entry)", result.Mismatch.Index)
	}
}

func TestCheck_ExtraDispatch_ReportsMismatchExtraDispatch(t *testing.T) {
	// Expected one dispatch, actual log has two.
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("agent-a#1", "task A"),
		responseLine("agent-a#1", "SUCCESS"),
		requestLine("agent-b#1", "task B"),
		responseLine("agent-b#1", "SUCCESS"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "agent-a", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if result.Pass {
		t.Error("Pass = true, want false when actual has more dispatches than expected")
	}
	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if result.Mismatch.Kind != testcheck.MismatchExtraDispatch {
		t.Errorf("Mismatch.Kind = %v, want MismatchExtraDispatch", result.Mismatch.Kind)
	}
	if result.Mismatch.Index != 1 {
		t.Errorf("Mismatch.Index = %d, want 1 (index of first extra dispatch)", result.Mismatch.Index)
	}
}

func TestCheck_WrongAgent_ReportsMismatchAgent(t *testing.T) {
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("actual-agent#1", "task"),
		responseLine("actual-agent#1", "SUCCESS"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "expected-agent", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if result.Pass {
		t.Error("Pass = true, want false when agent does not match")
	}
	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if result.Mismatch.Kind != testcheck.MismatchAgent {
		t.Errorf("Mismatch.Kind = %v, want MismatchAgent", result.Mismatch.Kind)
	}
	if result.Mismatch.Index != 0 {
		t.Errorf("Mismatch.Index = %d, want 0", result.Mismatch.Index)
	}
}

func TestCheck_WrongAgent_ReportsExpectedAndActualAgentValues(t *testing.T) {
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("wrong-agent#1", "task"),
		responseLine("wrong-agent#1", "SUCCESS"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "right-agent", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if !strings.Contains(result.Mismatch.ExpectedValue, "right-agent") {
		t.Errorf("Mismatch.ExpectedValue = %q, want it to contain %q", result.Mismatch.ExpectedValue, "right-agent")
	}
	if !strings.Contains(result.Mismatch.ActualValue, "wrong-agent") {
		t.Errorf("Mismatch.ActualValue = %q, want it to contain %q", result.Mismatch.ActualValue, "wrong-agent")
	}
}

func TestCheck_WrongStatus_ReportsMismatchStatus(t *testing.T) {
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("writer#1", "write code"),
		responseLine("writer#1", "BLOCKED"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "writer", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if result.Pass {
		t.Error("Pass = true, want false when status does not match")
	}
	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if result.Mismatch.Kind != testcheck.MismatchStatus {
		t.Errorf("Mismatch.Kind = %v, want MismatchStatus", result.Mismatch.Kind)
	}
	if result.Mismatch.Index != 0 {
		t.Errorf("Mismatch.Index = %d, want 0", result.Mismatch.Index)
	}
}

func TestCheck_WrongStatus_ReportsExpectedAndActualStatusValues(t *testing.T) {
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("agent#1", "task"),
		responseLine("agent#1", "PARTIALLY_DONE"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "agent", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if !strings.Contains(result.Mismatch.ExpectedValue, "SUCCESS") {
		t.Errorf("Mismatch.ExpectedValue = %q, want it to contain %q", result.Mismatch.ExpectedValue, "SUCCESS")
	}
	if !strings.Contains(result.Mismatch.ActualValue, "PARTIALLY_DONE") {
		t.Errorf("Mismatch.ActualValue = %q, want it to contain %q", result.Mismatch.ActualValue, "PARTIALLY_DONE")
	}
}

func TestCheck_FirstMismatchIsReported(t *testing.T) {
	// Both dispatches have wrong statuses; only index 0 should be reported.
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("agent-a#1", "task A"),
		responseLine("agent-a#1", "BLOCKED"),
		requestLine("agent-b#1", "task B"),
		responseLine("agent-b#1", "BLOCKED"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "agent-a", ExpectStatus: "SUCCESS"},
			{Agent: "agent-b", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if result.Pass {
		t.Error("Pass = true, want false")
	}
	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if result.Mismatch.Index != 0 {
		t.Errorf("Mismatch.Index = %d, want 0 (first mismatch should be reported)", result.Mismatch.Index)
	}
}
