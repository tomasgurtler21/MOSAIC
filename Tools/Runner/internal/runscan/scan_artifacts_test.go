package runscan_test

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/runscan"
)

// ---- tests: graceful degradation for unparseable artifacts ----

func TestScan_MissingArtifact_TreatedAsResumable(t *testing.T) {
	// A folder with no Orchestration.md must appear as a resumable candidate
	// with a non-nil ParseError.
	rootDir := t.TempDir()
	newTestRunFolder(t, rootDir, runID1) // no Orchestration.md written
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Errorf("Candidates = %d, want 1 (missing artifact treated as resumable)", len(result.Candidates))
		return
	}
	if result.Candidates[0].ParseError == nil {
		t.Error("ParseError is nil, want non-nil (missing Orchestration.md must set ParseError)")
	}
	if len(result.Unresumable) != 0 {
		t.Errorf("Unresumable = %d, want 0 (missing artifact must remain resumable, not unresumable)", len(result.Unresumable))
	}
}

func TestScan_UnparseableArtifact_TreatedAsResumable(t *testing.T) {
	// A folder with a corrupt / unparseable Orchestration.md must appear as
	// a resumable candidate with a non-nil ParseError.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	// Write garbage content that artifact.Parse will reject.
	if err := os.WriteFile(filepath.Join(folder, "Orchestration.md"), []byte("not valid yaml or artifact format\n"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Errorf("Candidates = %d, want 1 (unparseable artifact treated as resumable)", len(result.Candidates))
		return
	}
	if result.Candidates[0].ParseError == nil {
		t.Error("ParseError is nil, want non-nil (unparseable Orchestration.md must set ParseError)")
	}
	if len(result.Unresumable) != 0 {
		t.Errorf("Unresumable = %d, want 0 (unparseable artifact must remain resumable, not unresumable)", len(result.Unresumable))
	}
}

func TestScan_MissingArtifact_RunIDIsCorrect(t *testing.T) {
	// Even when the artifact is missing, RunID must be extracted from the folder name.
	rootDir := t.TempDir()
	newTestRunFolder(t, rootDir, runID1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) == 0 {
		t.Fatal("expected 1 candidate, got 0")
	}
	if result.Candidates[0].RunID != runID1 {
		t.Errorf("RunID = %q, want %q", result.Candidates[0].RunID, runID1)
	}
}

// ---- tests: ordering ----

func TestScan_CandidatesOrderedByLastUpdatedDescending(t *testing.T) {
	// Candidates must be sorted by LastUpdated descending (most recent first).
	rootDir := t.TempDir()
	folder1 := newTestRunFolder(t, rootDir, runID1)
	folder2 := newTestRunFolder(t, rootDir, runID2)
	folder3 := newTestRunFolder(t, rootDir, runID3)
	writeArtifact(t, folder1, "EXECUTION", t1) // oldest
	writeArtifact(t, folder2, "EXECUTION", t3) // newest
	writeArtifact(t, folder3, "EXECUTION", t2) // middle
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 3 {
		t.Fatalf("Candidates = %d, want 3", len(result.Candidates))
	}
	// Most recent (t3) must be first.
	if !result.Candidates[0].LastUpdated.Equal(t3) {
		t.Errorf("Candidates[0].LastUpdated = %v, want %v (most recent first)", result.Candidates[0].LastUpdated, t3)
	}
	// Oldest (t1) must be last.
	if !result.Candidates[2].LastUpdated.Equal(t1) {
		t.Errorf("Candidates[2].LastUpdated = %v, want %v (oldest last)", result.Candidates[2].LastUpdated, t1)
	}
}

// ---- tests: UnresumableReason.Description() ----

func TestUnresumableReason_Description_NeverEmpty(t *testing.T) {
	// Description() must return a non-empty phrase for every reason,
	// including an unrecognised value (the default branch).
	cases := []struct {
		name   string
		reason runscan.UnresumableReason
	}{
		{"ReasonCompleted", runscan.ReasonCompleted},
		{"unrecognised value", runscan.UnresumableReason("some-future-reason")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.reason.Description()
			if got == "" {
				t.Errorf("Description() = %q, want non-empty", got)
			}
		})
	}
}

// ---- tests: error propagation ----

func TestScan_NonExistentRootDir_ReturnsError(t *testing.T) {
	// Scanning a rootDir that does not exist must return a non-nil error.
	nonExistentDir := filepath.Join(t.TempDir(), "does-not-exist")
	scanner := runscan.NewDirScanner()

	_, err := scanner.Scan(nonExistentDir)

	if err == nil {
		t.Error("Scan() error = nil, want non-nil for non-existent rootDir")
	}
}
