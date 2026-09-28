package testcheck_test

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/testcheck"
)

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
