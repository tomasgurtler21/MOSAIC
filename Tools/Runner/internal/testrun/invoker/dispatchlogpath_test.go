package invoker_test

import (
	"path/filepath"
	"testing"

	"mosaic-run/internal/testrun/invoker"
)

// =============================================================================
// DispatchLogPath utility function
// =============================================================================

// TestDispatchLogPath_Convention verifies that DispatchLogPath follows the
// convention: {workspace}/RunnerLogs/{run_id}/{run_id}-dispatch.log.
func TestDispatchLogPath_Convention(t *testing.T) {
	const workspace = "/workspace"
	const runID = "run-20260914T120000Z-abc1"
	got := invoker.DispatchLogPath(workspace, runID)
	want := filepath.Join(workspace, "RunnerLogs", runID, runID+"-dispatch.log")
	if got != want {
		t.Errorf("DispatchLogPath = %q, want %q", got, want)
	}
}

// TestDispatchLogPath_RunIDAppearsInFilename verifies the run ID appears in
// the final filename segment.
func TestDispatchLogPath_RunIDAppearsInFilename(t *testing.T) {
	runID := "my-run-42"
	got := invoker.DispatchLogPath("/ws", runID)
	if len(got) == 0 {
		t.Fatal("DispatchLogPath returned empty string")
	}
	// The filename segment must be "{runID}-dispatch.log".
	wantFilename := runID + "-dispatch.log"
	// Extract last path component.
	lastSlash := 0
	for i, ch := range got {
		if ch == '/' || ch == '\\' {
			lastSlash = i
		}
	}
	filename := got[lastSlash+1:]
	if filename != wantFilename {
		t.Errorf("DispatchLogPath filename = %q, want %q", filename, wantFilename)
	}
}
