package testcheck_test

import (
	"testing"

	"mosaic-run/internal/testcheck"
)

// ============================================================
// Check tests: task_contains
// ============================================================

func TestCheck_TaskContainsEmpty_TaskTextNotChecked(t *testing.T) {
	// When task_contains is empty, the task text is irrelevant.
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("agent#1", "any task description whatsoever"),
		responseLine("agent#1", "SUCCESS"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "agent", ExpectStatus: "SUCCESS", TaskContains: ""},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if !result.Pass {
		t.Errorf("Pass = false, want true when task_contains is empty; mismatch: %+v", result.Mismatch)
	}
}

func TestCheck_TaskContainsMatch_Passes(t *testing.T) {
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("agent#1", "write tests for the authentication module"),
		responseLine("agent#1", "SUCCESS"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "agent", ExpectStatus: "SUCCESS", TaskContains: "authentication"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if !result.Pass {
		t.Errorf("Pass = false, want true when task text contains expected substring; mismatch: %+v", result.Mismatch)
	}
}

func TestCheck_TaskContainsNoMatch_ReportsMismatchTaskContains(t *testing.T) {
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("agent#1", "write tests for the payment module"),
		responseLine("agent#1", "SUCCESS"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "agent", ExpectStatus: "SUCCESS", TaskContains: "authentication"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if result.Pass {
		t.Error("Pass = true, want false when task text does not contain expected substring")
	}
	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if result.Mismatch.Kind != testcheck.MismatchTaskContains {
		t.Errorf("Mismatch.Kind = %v, want MismatchTaskContains", result.Mismatch.Kind)
	}
	if result.Mismatch.Index != 0 {
		t.Errorf("Mismatch.Index = %d, want 0", result.Mismatch.Index)
	}
}

// ============================================================
// Check tests: error entries
// ============================================================

func TestCheck_ExpectErrorAndActualIsError_Passes(t *testing.T) {
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("flaky-agent#1", "risky operation"),
		errorLine("flaky-agent#1", "subprocess exited with code 137"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 1,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "flaky-agent", ExpectError: true},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 1, DispatchLog: logPath}, expected)

	if !result.Pass {
		t.Errorf("Pass = false, want true when expected error matches actual error entry; mismatch: %+v", result.Mismatch)
	}
}

func TestCheck_ExpectErrorButGotResponse_ReportsMismatchErrorExpected(t *testing.T) {
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("agent#1", "task"),
		responseLine("agent#1", "SUCCESS"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "agent", ExpectError: true},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if result.Pass {
		t.Error("Pass = true, want false when expected error but actual was a normal response")
	}
	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if result.Mismatch.Kind != testcheck.MismatchErrorExpected {
		t.Errorf("Mismatch.Kind = %v, want MismatchErrorExpected", result.Mismatch.Kind)
	}
}

func TestCheck_ExpectResponseButGotError_ReportsMismatchErrorUnexpected(t *testing.T) {
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("agent#1", "task"),
		errorLine("agent#1", "harness failed"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "agent", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if result.Pass {
		t.Error("Pass = true, want false when expected response but actual was an error entry")
	}
	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if result.Mismatch.Kind != testcheck.MismatchErrorUnexpected {
		t.Errorf("Mismatch.Kind = %v, want MismatchErrorUnexpected", result.Mismatch.Kind)
	}
	if result.Mismatch.Index != 0 {
		t.Errorf("Mismatch.Index = %d, want 0", result.Mismatch.Index)
	}
}

// ============================================================
// Check tests: edge cases
// ============================================================

func TestCheck_EmptyLogEmptyExpected_Passes(t *testing.T) {
	dir := t.TempDir()
	logPath := emptyDispatchLog(t, dir)
	expected := &testcheck.ExpectedOutcome{
		ExitCode:   0,
		Dispatches: []testcheck.ExpectedDispatch{},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if !result.Pass {
		t.Errorf("Pass = false, want true for empty log with empty expected dispatches; mismatch: %+v", result.Mismatch)
	}
}

func TestCheck_EmptyLogWithExpectedDispatches_ReportsMissing(t *testing.T) {
	dir := t.TempDir()
	logPath := emptyDispatchLog(t, dir)
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "agent", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if result.Pass {
		t.Error("Pass = true, want false when expected dispatches but log is empty")
	}
	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if result.Mismatch.Kind != testcheck.MismatchMissingDispatch {
		t.Errorf("Mismatch.Kind = %v, want MismatchMissingDispatch", result.Mismatch.Kind)
	}
}

func TestCheck_ExitCodeMismatchSkipsDispatchComparison(t *testing.T) {
	// When exit codes differ, the mismatch kind must be MismatchExitCode,
	// not a dispatch-related kind -- confirming dispatch comparison was skipped.
	dir := t.TempDir()
	// Dispatch log has no entries; if dispatch comparison ran it would also fail.
	logPath := emptyDispatchLog(t, dir)
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "agent", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 1, DispatchLog: logPath}, expected)

	if result.Pass {
		t.Error("Pass = true, want false")
	}
	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if result.Mismatch.Kind != testcheck.MismatchExitCode {
		t.Errorf("Mismatch.Kind = %v, want MismatchExitCode (dispatch comparison should be skipped)", result.Mismatch.Kind)
	}
}

func TestCheck_LogWithVersionCorrelationOnlyAndEmptyExpected_Passes(t *testing.T) {
	// A log with only version/correlation entries is equivalent to an empty
	// dispatch sequence when the expected dispatches list is also empty.
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		versionLine(),
		correlationLine("20260914T171535Z-9c2f"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode:   0,
		Dispatches: []testcheck.ExpectedDispatch{},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if !result.Pass {
		t.Errorf("Pass = false, want true for log with only metadata entries and empty expected; mismatch: %+v", result.Mismatch)
	}
}

func TestCheck_MismatchMessage_IsNonEmpty(t *testing.T) {
	// The Message field of a Mismatch must be non-empty to be useful for
	// human-readable output.
	dir := t.TempDir()
	logPath := writeDispatchLog(t, dir, []string{
		requestLine("actual#1", "task"),
		responseLine("actual#1", "BLOCKED"),
	})
	expected := &testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "actual", ExpectStatus: "SUCCESS"},
		},
	}

	result := testcheck.Check(testcheck.CheckInput{ExitCode: 0, DispatchLog: logPath}, expected)

	if result.Mismatch == nil {
		t.Fatal("Mismatch = nil, want non-nil")
	}
	if result.Mismatch.Message == "" {
		t.Error("Mismatch.Message is empty, want a non-empty human-readable description")
	}
}
