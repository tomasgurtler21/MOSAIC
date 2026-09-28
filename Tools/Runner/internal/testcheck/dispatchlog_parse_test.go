package testcheck_test

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/testcheck"
)

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
