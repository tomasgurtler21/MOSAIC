package runscan_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/runscan"
)

// A run folder whose artifact carries an unusable run identity is surfaced as
// refused, with the reason, and is never offered as resumable.
func TestScan_InvalidRunIdentity_SurfacedAsRefusedNotOffered(t *testing.T) {
	tests := []struct {
		name      string
		runIDLine string
		mentions  string // fragment the detail must contain
	}{
		{"run_id key absent", "", "run_id"},
		{"run_id empty", `run_id: ""`, "run_id"},
		{"run_id malformed", "run_id: not-a-run-id", "malformed"},
		{"run_id names a different folder", "run_id: " + runID2, runID2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			rootDir := t.TempDir()
			folder := newTestRunFolder(t, rootDir, runID1)
			writeArtifactWithRunIDLine(t, folder, tc.runIDLine, t1)
			scanner := runscan.NewDirScanner()

			// Act
			result, err := scanner.Scan(rootDir)

			// Assert
			if err != nil {
				t.Fatalf("Scan() unexpected error: %v", err)
			}
			if len(result.Candidates) != 0 {
				t.Errorf("Candidates = %d, want 0 (invalid identity must not be offered)", len(result.Candidates))
			}
			if len(result.Unresumable) != 1 {
				t.Fatalf("Unresumable = %d, want 1 (invalid identity must be surfaced, not hidden)", len(result.Unresumable))
			}
			got := result.Unresumable[0]
			if got.Reason != runscan.ReasonInvalidRunIdentity {
				t.Errorf("Reason = %q, want %q", got.Reason, runscan.ReasonInvalidRunIdentity)
			}
			if got.RunID != runID1 {
				t.Errorf("RunID = %q, want the folder's run_id %q", got.RunID, runID1)
			}
			if got.FolderPath != folder {
				t.Errorf("FolderPath = %q, want %q", got.FolderPath, folder)
			}
			if !strings.Contains(got.Detail, tc.mentions) {
				t.Errorf("Detail = %q, want it to name %q", got.Detail, tc.mentions)
			}
		})
	}
}

func TestScan_InvalidRunIdentity_DoesNotCountAsCompleted(t *testing.T) {
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifactWithRunIDLine(t, folder, "run_id: not-a-run-id", t1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if result.CompletedCount() != 0 {
		t.Errorf("CompletedCount() = %d, want 0 (a refused run is not a completed run)", result.CompletedCount())
	}
}

func TestScan_InvalidRunIdentity_DoesNotHideCurrentRuns(t *testing.T) {
	rootDir := t.TempDir()
	bad := newTestRunFolder(t, rootDir, runID1)
	writeArtifactWithRunIDLine(t, bad, `run_id: ""`, t1)
	good := newTestRunFolder(t, rootDir, runID2)
	writeArtifact(t, good, "EXECUTION", t2)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].RunID != runID2 {
		t.Errorf("Candidates = %+v, want exactly the current run %s", result.Candidates, runID2)
	}
	if len(result.Unresumable) != 1 || result.Unresumable[0].RunID != runID1 {
		t.Errorf("Unresumable = %+v, want exactly the refused run %s", result.Unresumable, runID1)
	}
}

func TestUnresumableReason_InvalidRunIdentity_Description(t *testing.T) {
	got := runscan.ReasonInvalidRunIdentity.Description()

	if got != "invalid run identity" {
		t.Errorf("Description() = %q, want %q", got, "invalid run identity")
	}
}
