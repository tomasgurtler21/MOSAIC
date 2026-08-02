package logread_test

// Tests for the tolerant JSONL reader (logread.Reader).
//
// Design invariants under test:
//   - Well-formed lines decode into the correct domain.Event type with all
//     envelope fields and the appropriate field block populated.
//   - Malformed lines are contained per line: surrounding events still arrive,
//     and exactly one finding identifies the file and 1-based line number.
//   - A truncated final line (incomplete JSON, as when a harness is killed
//     mid-write) is treated the same as a malformed line and produces one
//     finding of kind FindingTruncatedLine.
//   - Unknown event types decode as EventOther with RawType preserved; unknown
//     JSON fields are ignored; absent optional fields produce no finding.
//   - harness and schema_version are populated on every event, even for
//     EventOther; a mixed-harness file decodes fully with no findings.
//   - An unrecognised schema_version yields exactly one FindingUnknownSchema
//     per file (not per line) and still returns all events.
//   - An empty file, a file of blank lines produce zero events and zero findings.
//   - A file with a very long line (> bufio default limit) decodes correctly
//     and does not produce a spurious malformed-line finding.
//   - An unopenable file produces FindingUnreadableFile and a nil error (never
//     a hard error).
//   - A visitor error is propagated from Read unchanged; context cancellation
//     is also propagated as an error.
//
// Fixtures in testdata/ are used for the standard scenarios; inline temp-files
// are used where fine-grained control over line numbers or content is needed.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-log-analyzer/internal/domain"
	"mosaic-log-analyzer/internal/logread"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testdataFile returns the path to a named fixture under the project-level
// testdata/ directory. Tests run from the package directory (internal/logread),
// so we navigate up two levels to the module root.
func testdataFile(name string) string {
	return filepath.Join("..", "..", "testdata", name)
}

// writeTempFile creates a file in t.TempDir() containing content and returns
// its absolute path. The test fails immediately if writing fails.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Well-formed decoding — T6.1
// ---------------------------------------------------------------------------

func TestReader_Read_CompleteRun_ReturnsEventsInOrder(t *testing.T) {
	r := logread.NewReader()
	events, findings, err := r.ReadAll(context.Background(), testdataFile("complete.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("complete file must produce no findings, got %d: %v", len(findings), findings)
	}
	wantTypes := []domain.EventType{
		domain.EventRunStart,
		domain.EventSessionStart,
		domain.EventInvocationStart,
		domain.EventTurn, // user
		domain.EventTurn, // assistant
		domain.EventInvocationEnd,
		domain.EventSessionEnd,
		domain.EventRunEnd,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("expected %d events, got %d", len(wantTypes), len(events))
	}
	for i, ev := range events {
		if ev.Type != wantTypes[i] {
			t.Errorf("event[%d].Type = %q, want %q", i, ev.Type, wantTypes[i])
		}
	}
}

func TestReader_Read_InvocationStart_FieldsPopulated(t *testing.T) {
	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), testdataFile("complete.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var ev domain.Event
	for _, e := range events {
		if e.Type == domain.EventInvocationStart {
			ev = e
			break
		}
	}
	if ev.InvocationStart == nil {
		t.Fatal("invocation_start event must have non-nil InvocationStart field block")
	}
	if ev.InvocationStart.AgentInstanceID != "test-writer#1" {
		t.Errorf("AgentInstanceID = %q, want %q", ev.InvocationStart.AgentInstanceID, "test-writer#1")
	}
	if ev.InvocationStart.AgentType != "test-writer" {
		t.Errorf("AgentType = %q, want %q", ev.InvocationStart.AgentType, "test-writer")
	}
	// Other field blocks must be nil
	if ev.InvocationEnd != nil {
		t.Error("InvocationEnd must be nil on an invocation_start event")
	}
	if ev.Turn != nil {
		t.Error("Turn must be nil on an invocation_start event")
	}
}

func TestReader_Read_InvocationEnd_FieldsPopulated(t *testing.T) {
	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), testdataFile("complete.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var ev domain.Event
	for _, e := range events {
		if e.Type == domain.EventInvocationEnd {
			ev = e
			break
		}
	}
	if ev.InvocationEnd == nil {
		t.Fatal("invocation_end event must have non-nil InvocationEnd field block")
	}
	if ev.InvocationEnd.AgentInstanceID != "test-writer#1" {
		t.Errorf("AgentInstanceID = %q, want %q", ev.InvocationEnd.AgentInstanceID, "test-writer#1")
	}
	if ev.InvocationEnd.StatusCode != "SUCCESS" {
		t.Errorf("StatusCode = %q, want %q", ev.InvocationEnd.StatusCode, "SUCCESS")
	}
	if ev.InvocationEnd.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want %q", ev.InvocationEnd.Model, "claude-sonnet-4-6")
	}
	if !ev.InvocationEnd.HasUsage {
		t.Error("HasUsage must be true when token_usage was present in the JSON")
	}
	if v, ok := ev.InvocationEnd.Usage.Input.Value(); !ok || v != 12 {
		t.Errorf("Usage.Input = (%d, %v), want (12, true)", v, ok)
	}
	if v, ok := ev.InvocationEnd.Usage.Output.Value(); !ok || v != 8 {
		t.Errorf("Usage.Output = (%d, %v), want (8, true)", v, ok)
	}
	if v, ok := ev.InvocationEnd.Usage.CacheRead.Value(); !ok || v != 200 {
		t.Errorf("Usage.CacheRead = (%d, %v), want (200, true)", v, ok)
	}
	if v, ok := ev.InvocationEnd.Usage.CacheCreation.Value(); !ok || v != 50 {
		t.Errorf("Usage.CacheCreation = (%d, %v), want (50, true)", v, ok)
	}
}

func TestReader_Read_Turn_AssistantFieldsPopulated(t *testing.T) {
	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), testdataFile("complete.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var assistantTurn domain.Event
	for _, ev := range events {
		if ev.Type == domain.EventTurn && ev.Turn != nil && ev.Turn.Role == domain.TurnAssistant {
			assistantTurn = ev
			break
		}
	}
	if assistantTurn.Turn == nil {
		t.Fatal("no assistant turn event found in complete.jsonl")
	}
	if assistantTurn.Turn.Role != domain.TurnAssistant {
		t.Errorf("Turn.Role = %q, want %q", assistantTurn.Turn.Role, domain.TurnAssistant)
	}
	if assistantTurn.Turn.Model != "claude-sonnet-4-6" {
		t.Errorf("Turn.Model = %q, want %q", assistantTurn.Turn.Model, "claude-sonnet-4-6")
	}
	if !assistantTurn.Turn.HasUsage {
		t.Error("Turn.HasUsage must be true when token_usage was present")
	}
	if v, ok := assistantTurn.Turn.Usage.Input.Value(); !ok || v != 12 {
		t.Errorf("Turn.Usage.Input = (%d, %v), want (12, true)", v, ok)
	}
	if v, ok := assistantTurn.Turn.Usage.CacheRead.Value(); !ok || v != 200 {
		t.Errorf("Turn.Usage.CacheRead = (%d, %v), want (200, true)", v, ok)
	}
	if v, ok := assistantTurn.Turn.Usage.CacheCreation.Value(); !ok || v != 50 {
		t.Errorf("Turn.Usage.CacheCreation = (%d, %v), want (50, true)", v, ok)
	}
	// Other field blocks must be nil
	if assistantTurn.InvocationStart != nil {
		t.Error("InvocationStart must be nil on a turn event")
	}
	if assistantTurn.InvocationEnd != nil {
		t.Error("InvocationEnd must be nil on a turn event")
	}
}

func TestReader_Read_Turn_UserFieldsPopulated(t *testing.T) {
	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), testdataFile("complete.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var userTurn domain.Event
	for _, ev := range events {
		if ev.Type == domain.EventTurn && ev.Turn != nil && ev.Turn.Role == domain.TurnUser {
			userTurn = ev
			break
		}
	}
	if userTurn.Turn == nil {
		t.Fatal("no user turn event found in complete.jsonl")
	}
	if userTurn.Turn.Role != domain.TurnUser {
		t.Errorf("Turn.Role = %q, want %q", userTurn.Turn.Role, domain.TurnUser)
	}
}

func TestReader_Read_RunEnd_FieldBlocksAllNil(t *testing.T) {
	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), testdataFile("complete.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var found bool
	for _, ev := range events {
		if ev.Type != domain.EventRunEnd {
			continue
		}
		found = true
		if ev.InvocationStart != nil {
			t.Error("run_end must have nil InvocationStart")
		}
		if ev.InvocationEnd != nil {
			t.Error("run_end must have nil InvocationEnd")
		}
		if ev.Turn != nil {
			t.Error("run_end must have nil Turn")
		}
	}
	if !found {
		t.Fatal("expected run_end event in complete.jsonl")
	}
}

func TestReader_Read_SessionEnd_FieldBlocksAllNil(t *testing.T) {
	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), testdataFile("complete.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var found bool
	for _, ev := range events {
		if ev.Type != domain.EventSessionEnd {
			continue
		}
		found = true
		if ev.InvocationStart != nil {
			t.Error("session_end must have nil InvocationStart")
		}
		if ev.InvocationEnd != nil {
			t.Error("session_end must have nil InvocationEnd")
		}
		if ev.Turn != nil {
			t.Error("session_end must have nil Turn")
		}
	}
	if !found {
		t.Fatal("expected session_end event in complete.jsonl")
	}
}

func TestReader_Read_EnvelopePopulated(t *testing.T) {
	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), testdataFile("complete.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events from complete.jsonl, got none")
	}
	for i, ev := range events {
		if ev.SchemaVersion == "" {
			t.Errorf("event[%d].SchemaVersion is empty, must be populated", i)
		}
		if ev.Harness == "" {
			t.Errorf("event[%d].Harness is empty, must be populated", i)
		}
		if ev.SessionID == "" {
			t.Errorf("event[%d].SessionID is empty, must be populated", i)
		}
		if ev.RunID == "" {
			t.Errorf("event[%d].RunID is empty, must be populated", i)
		}
		if ev.Timestamp.IsZero() {
			t.Errorf("event[%d].Timestamp is zero, must be set", i)
		}
	}
}

func TestReader_Read_EnvelopeValues_MatchFixture(t *testing.T) {
	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), testdataFile("complete.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events")
	}
	ev := events[0]
	if ev.SchemaVersion != "1.0.0" {
		t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, "1.0.0")
	}
	if ev.Harness != domain.HarnessClaudeCode {
		t.Errorf("Harness = %q, want %q", ev.Harness, domain.HarnessClaudeCode)
	}
	if ev.SessionID != "sess-comp-01" {
		t.Errorf("SessionID = %q, want %q", ev.SessionID, "sess-comp-01")
	}
	if ev.RunID != "20260801T100000Z-ab01" {
		t.Errorf("RunID = %q, want %q", ev.RunID, "20260801T100000Z-ab01")
	}
}

func TestReader_Read_PartialRun_NoRunEnd_AllEventsReturned(t *testing.T) {
	// A file with no run_end or session_end should still decode all events
	// without any findings from the reader (provisional detection is the
	// analysis layer's job, not the reader's).
	r := logread.NewReader()
	events, findings, err := r.ReadAll(context.Background(), testdataFile("partial.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("partial run must produce no reader findings, got: %v", findings)
	}
	wantTypes := []domain.EventType{
		domain.EventInvocationStart,
		domain.EventTurn,
		domain.EventTurn,
		domain.EventInvocationEnd,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("expected %d events from partial.jsonl, got %d", len(wantTypes), len(events))
	}
	for i, ev := range events {
		if ev.Type != wantTypes[i] {
			t.Errorf("event[%d].Type = %q, want %q", i, ev.Type, wantTypes[i])
		}
	}
}

func TestReader_Read_InvocationEnd_HasUsage_True_WhenKeyPresent(t *testing.T) {
	content := `{"schema_version":"1.0.0","event":"invocation_end","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01","agent_instance_id":"a#1","token_usage":{"input_tokens":5,"output_tokens":3}}`
	path := writeTempFile(t, content)

	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) != 1 || events[0].InvocationEnd == nil {
		t.Fatal("expected 1 invocation_end event")
	}
	if !events[0].InvocationEnd.HasUsage {
		t.Error("HasUsage must be true when token_usage key is present")
	}
}

func TestReader_Read_InvocationEnd_HasUsage_False_WhenKeyAbsent(t *testing.T) {
	// No token_usage key at all.
	content := `{"schema_version":"1.0.0","event":"invocation_end","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01","agent_instance_id":"a#1"}`
	path := writeTempFile(t, content)

	r := logread.NewReader()
	events, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("absent token_usage must produce no finding from reader, got: %v", findings)
	}
	if len(events) != 1 || events[0].InvocationEnd == nil {
		t.Fatal("expected 1 invocation_end event")
	}
	if events[0].InvocationEnd.HasUsage {
		t.Error("HasUsage must be false when token_usage key is absent entirely")
	}
	if !events[0].InvocationEnd.Usage.IsEmpty() {
		t.Error("Usage must be all-absent when token_usage key is absent")
	}
}

func TestReader_Read_InvocationEnd_HasUsage_True_WhenKeyPresentButEmpty(t *testing.T) {
	// token_usage key is present but the object has no recognised sub-fields.
	content := `{"schema_version":"1.0.0","event":"invocation_end","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01","agent_instance_id":"a#1","token_usage":{}}`
	path := writeTempFile(t, content)

	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) != 1 || events[0].InvocationEnd == nil {
		t.Fatal("expected 1 invocation_end event")
	}
	if !events[0].InvocationEnd.HasUsage {
		t.Error("HasUsage must be true when token_usage key is present (even if the object has no sub-fields)")
	}
	if !events[0].InvocationEnd.Usage.IsEmpty() {
		t.Error("Usage must be all-absent when token_usage has no recognised sub-fields")
	}
}

// ---------------------------------------------------------------------------
// Per-line malformed containment — T6.2
// ---------------------------------------------------------------------------

func TestReader_Read_MalformedMiddleLine_SurroundingEventsReturned(t *testing.T) {
	// malformed.jsonl: line 1 = valid invocation_start, line 2 = bad JSON,
	// line 3 = valid invocation_end, line 4 = truncated tail.
	// Lines 1 and 3 must still be returned.
	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), testdataFile("malformed.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var startCount, endCount int
	for _, ev := range events {
		switch ev.Type {
		case domain.EventInvocationStart:
			startCount++
		case domain.EventInvocationEnd:
			endCount++
		}
	}
	if startCount == 0 {
		t.Error("expected invocation_start (before the malformed line) to be returned")
	}
	if endCount == 0 {
		t.Error("expected invocation_end (after the malformed line) to be returned")
	}
}

func TestReader_Read_MalformedLine_ExactlyOneFindingMalformedLine(t *testing.T) {
	path := testdataFile("malformed.jsonl")
	r := logread.NewReader()
	_, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var count int
	for _, f := range findings {
		if f.Kind == domain.FindingMalformedLine {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 FindingMalformedLine, got %d", count)
	}
}

func TestReader_Read_MalformedLine_FindingNamesFileAndLine(t *testing.T) {
	path := testdataFile("malformed.jsonl")
	r := logread.NewReader()
	_, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var f *domain.Finding
	for i := range findings {
		if findings[i].Kind == domain.FindingMalformedLine {
			cp := findings[i]
			f = &cp
			break
		}
	}
	if f == nil {
		t.Fatal("expected a FindingMalformedLine finding")
	}
	if f.Path != path {
		t.Errorf("finding.Path = %q, want %q", f.Path, path)
	}
	if f.Line != 2 {
		t.Errorf("finding.Line = %d, want 2 (the bad JSON is on line 2)", f.Line)
	}
}

func TestReader_Read_MalformedLine_NilError(t *testing.T) {
	r := logread.NewReader()
	_, _, err := r.ReadAll(context.Background(), testdataFile("malformed.jsonl"))
	if err != nil {
		t.Errorf("malformed line must not produce a hard error, got: %v", err)
	}
}

func TestReader_Read_MalformedLine_Inline_SurroundingEventsReturned(t *testing.T) {
	// Inline version: three consecutive valid-bad-valid lines.
	content := strings.Join([]string{
		`{"schema_version":"1.0.0","event":"run_start","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01"}`,
		`not json {{{`,
		`{"schema_version":"1.0.0","event":"run_end","timestamp":"2026-08-01T10:00:01.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01"}`,
	}, "\n")
	path := writeTempFile(t, content)

	r := logread.NewReader()
	events, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 events (run_start + run_end), got %d", len(events))
	}
	if len(findings) != 1 {
		t.Errorf("expected exactly 1 finding, got %d", len(findings))
	}
	if len(findings) > 0 && findings[0].Line != 2 {
		t.Errorf("finding.Line = %d, want 2", findings[0].Line)
	}
}

// ---------------------------------------------------------------------------
// Truncated final line — T6.3
// ---------------------------------------------------------------------------

func TestReader_Read_TruncatedFinalLine_PrecedingEventsReturned(t *testing.T) {
	content := strings.Join([]string{
		`{"schema_version":"1.0.0","event":"invocation_start","timestamp":"2026-08-01T10:00:01.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01","agent_instance_id":"a#1","agent_type":"a"}`,
		`{"schema_version":"1.0.0","event":"invocation_end","timestamp":"2026-08-01T10:00:02.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01","agent_instance_id":"a#1","status_code":"SUCCESS","model":"m"}`,
		`{"schema_version":"1.0.0","event":"turn","timestamp":"2026-08-`,
	}, "\n")
	path := writeTempFile(t, content)

	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) < 2 {
		t.Errorf("expected at least 2 events before the truncated tail, got %d", len(events))
	}
}

func TestReader_Read_TruncatedFinalLine_ExactlyOneFindingTruncatedLine(t *testing.T) {
	content := strings.Join([]string{
		`{"schema_version":"1.0.0","event":"invocation_start","timestamp":"2026-08-01T10:00:01.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01","agent_instance_id":"a#1","agent_type":"a"}`,
		`{"schema_version":"1.0.0","event":"turn","timestamp":"2026-08-`,
	}, "\n")
	path := writeTempFile(t, content)

	r := logread.NewReader()
	_, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var count int
	for _, f := range findings {
		if f.Kind == domain.FindingTruncatedLine {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 FindingTruncatedLine, got %d", count)
	}
}

func TestReader_Read_TruncatedFinalLine_FindingNamesFileAndLine(t *testing.T) {
	content := strings.Join([]string{
		`{"schema_version":"1.0.0","event":"run_start","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01"}`,
		`{"schema_version":"1.0.0","event":"turn","timestamp":"2026-08-`,
	}, "\n")
	path := writeTempFile(t, content)

	r := logread.NewReader()
	_, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var f *domain.Finding
	for i := range findings {
		if findings[i].Kind == domain.FindingTruncatedLine {
			cp := findings[i]
			f = &cp
			break
		}
	}
	if f == nil {
		t.Fatal("expected FindingTruncatedLine")
	}
	if f.Path != path {
		t.Errorf("finding.Path = %q, want %q", f.Path, path)
	}
	if f.Line != 2 {
		t.Errorf("finding.Line = %d, want 2 (truncated line is line 2)", f.Line)
	}
}

func TestReader_Read_TruncatedFinalLine_NilError(t *testing.T) {
	content := strings.Join([]string{
		`{"schema_version":"1.0.0","event":"run_start","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01"}`,
		`{"schema_version":"1.0.0","event":"turn","timestamp":"2026-08-`,
	}, "\n")
	path := writeTempFile(t, content)

	r := logread.NewReader()
	_, _, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Errorf("truncated final line must not produce a hard error, got: %v", err)
	}
}

func TestReader_Read_MalformedFixture_TruncatedTail_FindingForLine4(t *testing.T) {
	// malformed.jsonl has a truncated final line (line 4). Verify the finding
	// for the truncated tail specifically names line 4.
	path := testdataFile("malformed.jsonl")
	r := logread.NewReader()
	_, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var found bool
	for _, f := range findings {
		if f.Kind == domain.FindingTruncatedLine && f.Line == 4 {
			found = true
			if f.Path != path {
				t.Errorf("truncated finding.Path = %q, want %q", f.Path, path)
			}
		}
	}
	if !found {
		t.Errorf("expected FindingTruncatedLine at line 4 in malformed.jsonl, findings: %v", findings)
	}
}

// ---------------------------------------------------------------------------
// Unknown event types, unknown fields, absent optional fields — T6.4
// ---------------------------------------------------------------------------

func TestReader_Read_UnknownEventType_DecodesAsEventOther_NoFinding(t *testing.T) {
	content := `{"schema_version":"1.0.0","event":"future_event_type_v9","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01"}`
	path := writeTempFile(t, content)

	r := logread.NewReader()
	events, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("unknown event type must produce no finding, got: %v", findings)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != domain.EventOther {
		t.Errorf("Type = %q, want EventOther (empty string)", events[0].Type)
	}
}

func TestReader_Read_UnknownEventType_RawTypePreserved(t *testing.T) {
	const rawType = "future_event_type_v9"
	content := `{"schema_version":"1.0.0","event":"` + rawType + `","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01"}`
	path := writeTempFile(t, content)

	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].RawType != rawType {
		t.Errorf("RawType = %q, want %q", events[0].RawType, rawType)
	}
}

func TestReader_Read_UnknownEventType_FieldBlocksAllNil(t *testing.T) {
	content := `{"schema_version":"1.0.0","event":"totally_unknown","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01"}`
	path := writeTempFile(t, content)

	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev := events[0]
	if ev.InvocationStart != nil {
		t.Error("InvocationStart must be nil for EventOther")
	}
	if ev.InvocationEnd != nil {
		t.Error("InvocationEnd must be nil for EventOther")
	}
	if ev.Turn != nil {
		t.Error("Turn must be nil for EventOther")
	}
}

func TestReader_Read_UnknownFields_Ignored_NoFinding(t *testing.T) {
	content := `{"schema_version":"1.0.0","event":"invocation_start","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01","agent_instance_id":"a#1","agent_type":"a","unknown_future_field":"some_value","another_unknown":42}`
	path := writeTempFile(t, content)

	r := logread.NewReader()
	events, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("unknown JSON fields must produce no finding, got: %v", findings)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != domain.EventInvocationStart {
		t.Errorf("Type = %q, want EventInvocationStart", events[0].Type)
	}
}

func TestReader_Read_AbsentOptionalFields_NoFinding(t *testing.T) {
	// invocation_end with no token_usage, no status_code, no model (all optional).
	content := `{"schema_version":"1.0.0","event":"invocation_end","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01","agent_instance_id":"a#1"}`
	path := writeTempFile(t, content)

	r := logread.NewReader()
	events, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("absent optional fields must produce no finding, got: %v", findings)
	}
	if len(events) != 1 || events[0].InvocationEnd == nil {
		t.Fatalf("expected 1 invocation_end event")
	}
	if events[0].InvocationEnd.HasUsage {
		t.Error("HasUsage must be false when token_usage is absent")
	}
	if events[0].InvocationEnd.Model != "" {
		t.Errorf("Model should be empty when absent, got %q", events[0].InvocationEnd.Model)
	}
}

func TestReader_Read_UnknownEventType_EnvelopePopulated(t *testing.T) {
	content := `{"schema_version":"1.0.0","event":"future_event","timestamp":"2026-08-01T10:00:00.000Z","harness":"opencode","session_id":"sess-oc","run_id":"20260801T100000Z-ab01"}`
	path := writeTempFile(t, content)

	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev := events[0]
	if ev.Harness != domain.HarnessOpenCode {
		t.Errorf("Harness = %q, want %q", ev.Harness, domain.HarnessOpenCode)
	}
	if ev.SchemaVersion != "1.0.0" {
		t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, "1.0.0")
	}
	if ev.SessionID != "sess-oc" {
		t.Errorf("SessionID = %q, want %q", ev.SessionID, "sess-oc")
	}
}

// ---------------------------------------------------------------------------
// Envelope extraction, mixed harness, schema version — T6.5
// ---------------------------------------------------------------------------

func TestReader_Read_Envelope_HarnessAndSchemaVersionOnEveryEvent(t *testing.T) {
	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), testdataFile("complete.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	for i, ev := range events {
		if ev.SchemaVersion == "" {
			t.Errorf("event[%d] (%s): SchemaVersion is empty, must be populated", i, ev.Type)
		}
		if ev.Harness == "" {
			t.Errorf("event[%d] (%s): Harness is empty, must be populated", i, ev.Type)
		}
	}
}

func TestReader_Read_MixedHarness_DecodesAllEvents_NoFindings(t *testing.T) {
	r := logread.NewReader()
	events, findings, err := r.ReadAll(context.Background(), testdataFile("multiharness.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("mixed-harness file must produce no findings, got: %v", findings)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events from multiharness.jsonl, got %d", len(events))
	}
}

func TestReader_Read_MixedHarness_BothHarnessesPresent(t *testing.T) {
	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), testdataFile("multiharness.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var seenCC, seenOC bool
	for _, ev := range events {
		if ev.Harness == domain.HarnessClaudeCode {
			seenCC = true
		}
		if ev.Harness == domain.HarnessOpenCode {
			seenOC = true
		}
	}
	if !seenCC {
		t.Error("expected at least one event with Harness=claude-code in multiharness.jsonl")
	}
	if !seenOC {
		t.Error("expected at least one event with Harness=opencode in multiharness.jsonl")
	}
}

func TestReader_Read_MixedHarness_HarnessOnEveryEvent(t *testing.T) {
	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), testdataFile("multiharness.jsonl"))
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	for i, ev := range events {
		if ev.Harness == "" {
			t.Errorf("event[%d]: Harness is empty in mixed-harness file", i)
		}
	}
}

func TestReader_Read_UnrecognisedSchemaVersion_EventsStillReturned(t *testing.T) {
	content := strings.Join([]string{
		`{"schema_version":"2.0.0","event":"invocation_start","timestamp":"2026-08-01T10:00:01.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01","agent_instance_id":"a#1","agent_type":"a"}`,
		`{"schema_version":"2.0.0","event":"invocation_end","timestamp":"2026-08-01T10:00:02.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01","agent_instance_id":"a#1","status_code":"SUCCESS","model":"m"}`,
	}, "\n")
	path := writeTempFile(t, content)

	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("events must still be returned for unrecognised schema version, got %d", len(events))
	}
}

func TestReader_Read_UnrecognisedSchemaVersion_ExactlyOneFindingPerFile(t *testing.T) {
	// The schema version finding is per file, not per line. A file with 5 lines
	// all carrying schema_version "2.0.0" must produce exactly 1 finding.
	lines := make([]string, 5)
	for i := range lines {
		lines[i] = `{"schema_version":"2.0.0","event":"run_start","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01"}`
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	r := logread.NewReader()
	_, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var schemaFindings int
	for _, f := range findings {
		if f.Kind == domain.FindingUnknownSchema {
			schemaFindings++
		}
	}
	if schemaFindings != 1 {
		t.Errorf("expected exactly 1 FindingUnknownSchema per file (not per line), got %d", schemaFindings)
	}
}

func TestReader_Read_UnrecognisedSchemaVersion_FindingNamesFile(t *testing.T) {
	content := `{"schema_version":"99.0.0","event":"run_start","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01"}`
	path := writeTempFile(t, content)

	r := logread.NewReader()
	_, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	var f *domain.Finding
	for i := range findings {
		if findings[i].Kind == domain.FindingUnknownSchema {
			cp := findings[i]
			f = &cp
			break
		}
	}
	if f == nil {
		t.Fatal("expected FindingUnknownSchema for unrecognised schema version")
	}
	if f.Path != path {
		t.Errorf("finding.Path = %q, want %q", f.Path, path)
	}
}

func TestReader_Read_SupportedSchemaVersion_NoSchemaFinding(t *testing.T) {
	content := `{"schema_version":"1.0.0","event":"run_start","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01"}`
	path := writeTempFile(t, content)

	r := logread.NewReader()
	_, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	for _, f := range findings {
		if f.Kind == domain.FindingUnknownSchema {
			t.Error("supported schema version 1.0.0 must not produce FindingUnknownSchema")
		}
	}
}

// ---------------------------------------------------------------------------
// Edge inputs — T6.6
// ---------------------------------------------------------------------------

func TestReader_Read_EmptyFile_NoEventsNoFindings(t *testing.T) {
	path := writeTempFile(t, "")

	r := logread.NewReader()
	events, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("empty file must yield 0 events, got %d", len(events))
	}
	if len(findings) != 0 {
		t.Errorf("empty file must yield 0 findings, got %d: %v", len(findings), findings)
	}
}

func TestReader_Read_BlankLinesOnly_NoEventsNoFindings(t *testing.T) {
	path := writeTempFile(t, "\n\n\n   \n\t\n\n")

	r := logread.NewReader()
	events, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("blank-lines-only file must yield 0 events, got %d", len(events))
	}
	if len(findings) != 0 {
		t.Errorf("blank-lines-only file must yield 0 findings, got %d: %v", len(findings), findings)
	}
}

func TestReader_Read_VeryLongLine_DecodesCorrectly_NoFinding(t *testing.T) {
	// A turn event whose content field is 1 MiB. This is well below MaxLineBytes
	// (64 MiB) and must not be misidentified as a malformed line.
	longContent := strings.Repeat("a", 1<<20) // 1 MiB of ASCII
	line := `{"schema_version":"1.0.0","event":"turn","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01","role":"assistant","model":"claude-sonnet-4-6","content":"` + longContent + `","token_usage":{"input_tokens":1,"output_tokens":1}}`
	path := writeTempFile(t, line)

	r := logread.NewReader()
	events, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("very long valid line must produce no finding, got: %v", findings)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event for the long line, got %d", len(events))
	}
	if events[0].Type != domain.EventTurn {
		t.Errorf("Type = %q, want EventTurn", events[0].Type)
	}
}

func TestReader_Read_VeryLongLine_CorrectFieldsDecoded(t *testing.T) {
	// The model and role fields must decode correctly even when the line is very large.
	longContent := strings.Repeat("b", 1<<20)
	line := `{"schema_version":"1.0.0","event":"turn","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01","role":"assistant","model":"claude-sonnet-4-6","content":"` + longContent + `","token_usage":{"input_tokens":5,"output_tokens":10}}`
	path := writeTempFile(t, line)

	r := logread.NewReader()
	events, _, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(events) != 1 || events[0].Turn == nil {
		t.Fatal("expected 1 turn event")
	}
	if events[0].Turn.Model != "claude-sonnet-4-6" {
		t.Errorf("Turn.Model = %q, want %q", events[0].Turn.Model, "claude-sonnet-4-6")
	}
	if events[0].Turn.Role != domain.TurnAssistant {
		t.Errorf("Turn.Role = %q, want assistant", events[0].Turn.Role)
	}
}

func TestReader_Read_UnopenableFile_NoError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.jsonl")

	r := logread.NewReader()
	events, findings, err := r.ReadAll(context.Background(), path)
	if err != nil {
		t.Errorf("unopenable file must not return a hard error, got: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("unopenable file must return 0 events, got %d", len(events))
	}
	var found bool
	for _, f := range findings {
		if f.Kind == domain.FindingUnreadableFile {
			found = true
		}
	}
	if !found {
		t.Error("unopenable file must produce exactly one FindingUnreadableFile finding")
	}
}

func TestReader_Read_UnopenableFile_FindingNamesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.jsonl")

	r := logread.NewReader()
	_, findings, _ := r.ReadAll(context.Background(), path)
	for _, f := range findings {
		if f.Kind == domain.FindingUnreadableFile {
			if f.Path != path {
				t.Errorf("finding.Path = %q, want %q", f.Path, path)
			}
			return
		}
	}
	t.Fatal("expected FindingUnreadableFile finding")
}

// ---------------------------------------------------------------------------
// MaxLineBytes constant
// ---------------------------------------------------------------------------

func TestReader_MaxLineBytes_IsLargeEnoughForTurnContent(t *testing.T) {
	const want = 64 << 20 // 64 MiB
	if logread.MaxLineBytes != want {
		t.Errorf("MaxLineBytes = %d, want %d (64 MiB)", logread.MaxLineBytes, want)
	}
}

// ---------------------------------------------------------------------------
// Streaming — Read method and visitor contract
// ---------------------------------------------------------------------------

func TestReader_Read_StreamingCallback_CalledInFileOrder(t *testing.T) {
	r := logread.NewReader()
	var gotTypes []domain.EventType
	_, err := r.Read(context.Background(), testdataFile("complete.jsonl"), func(ev domain.Event) error {
		gotTypes = append(gotTypes, ev.Type)
		return nil
	})
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	wantTypes := []domain.EventType{
		domain.EventRunStart,
		domain.EventSessionStart,
		domain.EventInvocationStart,
		domain.EventTurn,
		domain.EventTurn,
		domain.EventInvocationEnd,
		domain.EventSessionEnd,
		domain.EventRunEnd,
	}
	if len(gotTypes) != len(wantTypes) {
		t.Fatalf("got %d events via Read, want %d", len(gotTypes), len(wantTypes))
	}
	for i, got := range gotTypes {
		if got != wantTypes[i] {
			t.Errorf("callback event[%d].Type = %q, want %q", i, got, wantTypes[i])
		}
	}
}

func TestReader_Read_VisitorError_PropagatedUnchanged(t *testing.T) {
	sentinelErr := errors.New("visitor abort")
	r := logread.NewReader()
	content := `{"schema_version":"1.0.0","event":"run_start","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01"}`
	path := writeTempFile(t, content)

	_, err := r.Read(context.Background(), path, func(_ domain.Event) error {
		return sentinelErr
	})
	if !errors.Is(err, sentinelErr) {
		t.Errorf("visitor error must be propagated unchanged; got %v, want %v", err, sentinelErr)
	}
}

func TestReader_Read_CancelledContext_PropagatesError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting

	// Enough lines to ensure the context check is exercised even if checked lazily.
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = `{"schema_version":"1.0.0","event":"run_start","timestamp":"2026-08-01T10:00:00.000Z","harness":"claude-code","session_id":"s1","run_id":"20260801T100000Z-ab01"}`
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	reader := logread.NewReader()
	_, err := reader.Read(ctx, path, func(_ domain.Event) error { return nil })
	if err == nil {
		t.Error("expected an error from cancelled context, got nil")
	}
}
