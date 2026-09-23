// Package testcheck_test provides black-box tests for the testcheck package.
// Tests cover dispatch log parsing, expected-outcome sidecar loading, and the
// two-layer exit-code + dispatch-sequence checker.
//
// All filesystem access uses t.TempDir() so tests never create files in the
// repository root.
package testcheck_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/testcheck"
)

// ============================================================
// JSONL line helpers
//
// These helpers produce minimal JSONL lines that match the entry shapes
// written by internal/dispatchlog. Only the fields the parser needs are
// included; extra fields are ignored by the parser and omitted here for
// readability.
// ============================================================

// requestLine returns a JSONL request entry for the given agent and task.
func requestLine(agentID, taskDesc string) string {
	agentJSON, _ := json.Marshal(agentID)
	taskJSON, _ := json.Marshal(taskDesc)
	return `{"type":"request","timestamp":"2026-01-01T00:00:00Z","request":{"agent_instance_id":` +
		string(agentJSON) + `,"task_description":` + string(taskJSON) +
		`,"input_artifacts":null,"output_artifacts":null,"include_result_summary":false,"human_in_the_loop":false}}`
}

// responseLine returns a JSONL response entry for the given agent and status.
func responseLine(agentID, statusCode string) string {
	agentJSON, _ := json.Marshal(agentID)
	statusJSON, _ := json.Marshal(statusCode)
	return `{"type":"response","timestamp":"2026-01-01T00:00:00Z","agent_instance_id":` +
		string(agentJSON) + `,"response":{"agent_instance_id":` + string(agentJSON) +
		`,"status_code":` + string(statusJSON) + `,"status_message":"done"}}`
}

// errorLine returns a JSONL error entry for the given agent and error text.
func errorLine(agentID, errText string) string {
	agentJSON, _ := json.Marshal(agentID)
	errJSON, _ := json.Marshal(errText)
	return `{"type":"error","timestamp":"2026-01-01T00:00:00Z","agent_instance_id":` +
		string(agentJSON) + `,"error":` + string(errJSON) + `}`
}

// versionLine returns a JSONL version entry.
func versionLine() string {
	return `{"type":"version","timestamp":"2026-01-01T00:00:00Z","tool_version":"1.0.0"}`
}

// correlationLine returns a JSONL correlation entry.
func correlationLine(runID string) string {
	idJSON, _ := json.Marshal(runID)
	return `{"type":"correlation","timestamp":"2026-01-01T00:00:00Z","run_id":` + string(idJSON) + `}`
}

// ============================================================
// File writing helpers
// ============================================================

// writeDispatchLog writes the given JSONL lines (one per element) to a file
// in the temp directory and returns the file path.
func writeDispatchLog(t *testing.T, dir string, lines []string) string {
	t.Helper()
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	path := filepath.Join(dir, "dispatch.log")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeDispatchLog: WriteFile: %v", err)
	}
	return path
}

// writeSidecar serializes the given ExpectedOutcome to JSON and writes it to a
// file in the temp directory, returning the file path.
func writeSidecar(t *testing.T, dir string, outcome testcheck.ExpectedOutcome) string {
	t.Helper()
	data, err := json.Marshal(outcome)
	if err != nil {
		t.Fatalf("writeSidecar: json.Marshal: %v", err)
	}
	path := filepath.Join(dir, "outcome.expected.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("writeSidecar: WriteFile: %v", err)
	}
	return path
}

// emptyDispatchLog writes an empty dispatch log file and returns its path.
func emptyDispatchLog(t *testing.T, dir string) string {
	t.Helper()
	return writeDispatchLog(t, dir, nil)
}

// ============================================================
// ParseDispatchLog tests
// ============================================================

func TestParseDispatchLog_BasicRequestResponsePair(t *testing.T) {
	dir := t.TempDir()
	path := writeDispatchLog(t, dir, []string{
		requestLine("foo#1", "do stuff"),
		responseLine("foo#1", "SUCCESS"),
	})

	got, err := testcheck.ParseDispatchLog(path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d dispatches, want 1", len(got))
	}
	d := got[0]
	if d.Agent != "foo#1" {
		t.Errorf("Agent = %q, want %q", d.Agent, "foo#1")
	}
	if d.Status != "SUCCESS" {
		t.Errorf("Status = %q, want %q", d.Status, "SUCCESS")
	}
	if d.IsError {
		t.Error("IsError = true, want false")
	}
}

func TestParseDispatchLog_SkipsVersionAndCorrelationEntries(t *testing.T) {
	dir := t.TempDir()
	path := writeDispatchLog(t, dir, []string{
		versionLine(),
		correlationLine("20260914T171535Z-9c2f"),
		requestLine("agent#1", "task"),
		responseLine("agent#1", "SUCCESS"),
	})

	got, err := testcheck.ParseDispatchLog(path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d dispatches, want 1 (version and correlation lines must be skipped)", len(got))
	}
}

func TestParseDispatchLog_ErrorEntryProducesIsErrorDispatch(t *testing.T) {
	dir := t.TempDir()
	path := writeDispatchLog(t, dir, []string{
		requestLine("bad-agent#1", "risky task"),
		errorLine("bad-agent#1", "harness failed: exit status 2"),
	})

	got, err := testcheck.ParseDispatchLog(path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d dispatches, want 1", len(got))
	}
	d := got[0]
	if d.Agent != "bad-agent#1" {
		t.Errorf("Agent = %q, want %q", d.Agent, "bad-agent#1")
	}
	if !d.IsError {
		t.Error("IsError = false, want true for error entry")
	}
	if d.Status != "" {
		t.Errorf("Status = %q, want empty string for error dispatch", d.Status)
	}
}

func TestParseDispatchLog_PreservesTaskDescription(t *testing.T) {
	dir := t.TempDir()
	const wantTask = "write tests for the login feature"
	path := writeDispatchLog(t, dir, []string{
		requestLine("writer#1", wantTask),
		responseLine("writer#1", "SUCCESS"),
	})

	got, err := testcheck.ParseDispatchLog(path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d dispatches, want 1", len(got))
	}
	if got[0].TaskDescription != wantTask {
		t.Errorf("TaskDescription = %q, want %q", got[0].TaskDescription, wantTask)
	}
}

func TestParseDispatchLog_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := emptyDispatchLog(t, dir)

	got, err := testcheck.ParseDispatchLog(path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d dispatches, want 0 for empty log", len(got))
	}
}

func TestParseDispatchLog_OnlyVersionAndCorrelationEntries(t *testing.T) {
	dir := t.TempDir()
	path := writeDispatchLog(t, dir, []string{
		versionLine(),
		correlationLine("20260914T171535Z-9c2f"),
	})

	got, err := testcheck.ParseDispatchLog(path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d dispatches, want 0 when log contains only version/correlation entries", len(got))
	}
}

func TestParseDispatchLog_MultipleAgentsPairedByID(t *testing.T) {
	// Two agents dispatched sequentially; each response pairs with its own request.
	dir := t.TempDir()
	path := writeDispatchLog(t, dir, []string{
		requestLine("alpha#1", "task A"),
		responseLine("alpha#1", "SUCCESS"),
		requestLine("beta#1", "task B"),
		responseLine("beta#1", "COMPLETED_NEEDS_ACTION"),
	})

	got, err := testcheck.ParseDispatchLog(path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d dispatches, want 2", len(got))
	}
	// Verify agents are captured (order follows request order).
	if got[0].Agent != "alpha#1" || got[0].Status != "SUCCESS" {
		t.Errorf("dispatch[0] = {Agent:%q, Status:%q}, want {alpha#1, SUCCESS}",
			got[0].Agent, got[0].Status)
	}
	if got[1].Agent != "beta#1" || got[1].Status != "COMPLETED_NEEDS_ACTION" {
		t.Errorf("dispatch[1] = {Agent:%q, Status:%q}, want {beta#1, COMPLETED_NEEDS_ACTION}",
			got[1].Agent, got[1].Status)
	}
}

func TestParseDispatchLog_MixedSequenceWithErrorAndResponse(t *testing.T) {
	// One successful dispatch followed by one that produced a harness error.
	dir := t.TempDir()
	path := writeDispatchLog(t, dir, []string{
		requestLine("ok-agent#1", "ok task"),
		responseLine("ok-agent#1", "SUCCESS"),
		requestLine("bad-agent#1", "failing task"),
		errorLine("bad-agent#1", "Claude Code crashed"),
	})

	got, err := testcheck.ParseDispatchLog(path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d dispatches, want 2", len(got))
	}
	if got[0].IsError {
		t.Error("dispatch[0].IsError = true, want false (was a response)")
	}
	if !got[1].IsError {
		t.Error("dispatch[1].IsError = false, want true (was an error entry)")
	}
}

func TestParseDispatchLog_FileNotFoundReturnsError(t *testing.T) {
	_, err := testcheck.ParseDispatchLog("/nonexistent/path/dispatch.log")

	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestParseDispatchLog_MalformedJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.log")
	if err := os.WriteFile(path, []byte("this is not json\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := testcheck.ParseDispatchLog(path)

	if err == nil {
		t.Error("expected error for malformed JSON log, got nil")
	}
}

// ============================================================
// LoadExpected tests
// ============================================================

func TestLoadExpected_ValidSidecar(t *testing.T) {
	dir := t.TempDir()
	want := testcheck.ExpectedOutcome{
		ExitCode: 0,
		Dispatches: []testcheck.ExpectedDispatch{
			{Agent: "mosaictest-scripted", ExpectStatus: "SUCCESS"},
		},
	}
	path := writeSidecar(t, dir, want)

	got, err := testcheck.LoadExpected(path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("got nil, want non-nil ExpectedOutcome")
	}
	if got.ExitCode != want.ExitCode {
		t.Errorf("ExitCode = %d, want %d", got.ExitCode, want.ExitCode)
	}
	if len(got.Dispatches) != 1 {
		t.Fatalf("len(Dispatches) = %d, want 1", len(got.Dispatches))
	}
	if got.Dispatches[0].Agent != "mosaictest-scripted" {
		t.Errorf("Dispatches[0].Agent = %q, want %q", got.Dispatches[0].Agent, "mosaictest-scripted")
	}
	if got.Dispatches[0].ExpectStatus != "SUCCESS" {
		t.Errorf("Dispatches[0].ExpectStatus = %q, want %q", got.Dispatches[0].ExpectStatus, "SUCCESS")
	}
}

func TestLoadExpected_FileNotFoundReturnsError(t *testing.T) {
	_, err := testcheck.LoadExpected("/nonexistent/path/outcome.expected.json")

	if err == nil {
		t.Error("expected error for nonexistent sidecar file, got nil")
	}
}

func TestLoadExpected_MalformedJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.expected.json")
	if err := os.WriteFile(path, []byte("not json at all{{{"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := testcheck.LoadExpected(path)

	if err == nil {
		t.Error("expected error for malformed JSON sidecar, got nil")
	}
}

func TestLoadExpected_EmptyDispatchesArray(t *testing.T) {
	dir := t.TempDir()
	want := testcheck.ExpectedOutcome{
		ExitCode:   0,
		Dispatches: []testcheck.ExpectedDispatch{},
	}
	path := writeSidecar(t, dir, want)

	got, err := testcheck.LoadExpected(path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("got nil, want non-nil ExpectedOutcome")
	}
	if len(got.Dispatches) != 0 {
		t.Errorf("len(Dispatches) = %d, want 0", len(got.Dispatches))
	}
}

func TestLoadExpected_AllExpectedDispatchFields(t *testing.T) {
	// Verify all ExpectedDispatch fields round-trip correctly through JSON.
	dir := t.TempDir()
	want := testcheck.ExpectedOutcome{
		ExitCode: 1,
		Dispatches: []testcheck.ExpectedDispatch{
			{
				Agent:        "some-agent",
				ExpectError:  true,
				TaskContains: "required keyword",
			},
		},
	}
	path := writeSidecar(t, dir, want)

	got, err := testcheck.LoadExpected(path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", got.ExitCode)
	}
	d := got.Dispatches[0]
	if !d.ExpectError {
		t.Error("ExpectError = false, want true")
	}
	if d.TaskContains != "required keyword" {
		t.Errorf("TaskContains = %q, want %q", d.TaskContains, "required keyword")
	}
}

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
