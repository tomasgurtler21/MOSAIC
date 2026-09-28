package runscan_test

import (
	"path/filepath"
	"testing"

	"mosaic-run/internal/runscan"
)

// ---- tests: COMPLETED exclusion ----

func TestScan_CompletedRun_ExcludedFromCandidates(t *testing.T) {
	// A folder with current_state.phase == "COMPLETED" must not appear in Candidates.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifact(t, folder, "COMPLETED", t1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Errorf("Candidates = %d, want 0 (COMPLETED run must be excluded)", len(result.Candidates))
	}
	if result.CompletedCount() != 1 {
		t.Errorf("CompletedCount() = %d, want 1 (scanner must count excluded COMPLETED run)", result.CompletedCount())
	}
}

func TestScan_CompletedRun_AppearsInUnresumableWithReason(t *testing.T) {
	// A COMPLETED run must be surfaced in Unresumable, not discarded, with
	// Reason ReasonCompleted.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifact(t, folder, "COMPLETED", t1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Unresumable) != 1 {
		t.Fatalf("Unresumable = %d, want 1 (COMPLETED run must be surfaced)", len(result.Unresumable))
	}
	if result.Unresumable[0].Reason != runscan.ReasonCompleted {
		t.Errorf("Reason = %q, want %q", result.Unresumable[0].Reason, runscan.ReasonCompleted)
	}
}

func TestScan_UnresumableRun_CarriesIdentifyingMetadata(t *testing.T) {
	// An UnresumableRun must carry the same identifying metadata a resumable
	// candidate carries: RunID, FolderPath, LastUpdated, Workflow, Task.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifactWithMeta(t, folder, "COMPLETED", "greenfield-tdd", "Build the feature", t1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Unresumable) != 1 {
		t.Fatalf("Unresumable = %d, want 1", len(result.Unresumable))
	}
	u := result.Unresumable[0]
	if u.RunID != runID1 {
		t.Errorf("RunID = %q, want %q", u.RunID, runID1)
	}
	wantPath := filepath.Join(rootDir, "Orchestration-"+runID1)
	if u.FolderPath != wantPath {
		t.Errorf("FolderPath = %q, want %q", u.FolderPath, wantPath)
	}
	if !u.LastUpdated.Equal(t1) {
		t.Errorf("LastUpdated = %v, want %v", u.LastUpdated, t1)
	}
	if u.Workflow != "greenfield-tdd" {
		t.Errorf("Workflow = %q, want %q", u.Workflow, "greenfield-tdd")
	}
	if u.Task != "Build the feature" {
		t.Errorf("Task = %q, want %q", u.Task, "Build the feature")
	}
}

func TestScan_UnresumableRun_CarriesPhaseStageLastAgent(t *testing.T) {
	// An UnresumableRun's Phase, Stage, and LastAgent must be read straight
	// from current_state, not left zero-valued.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifactWithState(t, folder, "COMPLETED", "Stage-2", "agent#3", t1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Unresumable) != 1 {
		t.Fatalf("Unresumable = %d, want 1", len(result.Unresumable))
	}
	u := result.Unresumable[0]
	if u.Phase != "COMPLETED" {
		t.Errorf("Phase = %q, want %q", u.Phase, "COMPLETED")
	}
	if u.Stage != "Stage-2" {
		t.Errorf("Stage = %q, want %q", u.Stage, "Stage-2")
	}
	if u.LastAgent != "agent#3" {
		t.Errorf("LastAgent = %q, want %q", u.LastAgent, "agent#3")
	}
}

func TestScan_ResumableCandidate_CarriesPhaseStageLastAgent(t *testing.T) {
	// A RunCandidate's Phase, Stage, and LastAgent must be read straight from
	// current_state, not left zero-valued.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifactWithState(t, folder, "EXECUTION", "Stage-1", "agent#2", t1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("Candidates = %d, want 1", len(result.Candidates))
	}
	c := result.Candidates[0]
	if c.Phase != "EXECUTION" {
		t.Errorf("Phase = %q, want %q", c.Phase, "EXECUTION")
	}
	if c.Stage != "Stage-1" {
		t.Errorf("Stage = %q, want %q", c.Stage, "Stage-1")
	}
	if c.LastAgent != "agent#2" {
		t.Errorf("LastAgent = %q, want %q", c.LastAgent, "agent#2")
	}
}

func TestScan_UnresumableRuns_OrderedByLastUpdatedDescending(t *testing.T) {
	// Unresumable entries must be ordered by LastUpdated descending,
	// independently of Candidates ordering.
	rootDir := t.TempDir()
	folder1 := newTestRunFolder(t, rootDir, runID1)
	folder2 := newTestRunFolder(t, rootDir, runID2)
	folder3 := newTestRunFolder(t, rootDir, runID3)
	writeArtifact(t, folder1, "COMPLETED", t1) // oldest
	writeArtifact(t, folder2, "COMPLETED", t3) // newest
	writeArtifact(t, folder3, "COMPLETED", t2) // middle
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Unresumable) != 3 {
		t.Fatalf("Unresumable = %d, want 3", len(result.Unresumable))
	}
	if !result.Unresumable[0].LastUpdated.Equal(t3) {
		t.Errorf("Unresumable[0].LastUpdated = %v, want %v (most recent first)", result.Unresumable[0].LastUpdated, t3)
	}
	if !result.Unresumable[2].LastUpdated.Equal(t1) {
		t.Errorf("Unresumable[2].LastUpdated = %v, want %v (oldest last)", result.Unresumable[2].LastUpdated, t1)
	}
}

func TestScan_CompletedRun_CaseInsensitive_Lowercase(t *testing.T) {
	// "completed" (all lowercase) must also be excluded from Candidates.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifact(t, folder, "completed", t1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Errorf("Candidates = %d, want 0 (case-insensitive COMPLETED exclusion)", len(result.Candidates))
	}
	if result.CompletedCount() != 1 {
		t.Errorf("CompletedCount() = %d, want 1 (lowercase 'completed' must be counted as COMPLETED)", result.CompletedCount())
	}
}

func TestScan_CompletedRun_CaseInsensitive_MixedCase(t *testing.T) {
	// "Completed" (mixed case) must also be excluded from Candidates.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifact(t, folder, "Completed", t1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Errorf("Candidates = %d, want 0 (case-insensitive COMPLETED exclusion)", len(result.Candidates))
	}
	if result.CompletedCount() != 1 {
		t.Errorf("CompletedCount() = %d, want 1 (mixed-case 'Completed' must be counted as COMPLETED)", result.CompletedCount())
	}
}

func TestScan_CompletedRun_IncreasesCompletedCount(t *testing.T) {
	// Each excluded COMPLETED run must increment CompletedCount.
	rootDir := t.TempDir()
	folder1 := newTestRunFolder(t, rootDir, runID1)
	folder2 := newTestRunFolder(t, rootDir, runID2)
	writeArtifact(t, folder1, "COMPLETED", t1)
	writeArtifact(t, folder2, "COMPLETED", t2)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if result.CompletedCount() != 2 {
		t.Errorf("CompletedCount() = %d, want 2", result.CompletedCount())
	}
}

func TestScan_MixedRuns_OnlyResumableInCandidates(t *testing.T) {
	// When some runs are COMPLETED and others are not, only resumable runs
	// must appear in Candidates. CompletedCount() must reflect excluded runs,
	// and the excluded runs must be surfaced in Unresumable rather than
	// discarded.
	rootDir := t.TempDir()
	folder1 := newTestRunFolder(t, rootDir, runID1)
	folder2 := newTestRunFolder(t, rootDir, runID2)
	folder3 := newTestRunFolder(t, rootDir, runID3)
	writeArtifact(t, folder1, "COMPLETED", t1)
	writeArtifact(t, folder2, "EXECUTION", t2)
	writeArtifact(t, folder3, "COMPLETED", t3)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Errorf("Candidates = %d, want 1 (only resumable run)", len(result.Candidates))
	}
	if result.CompletedCount() != 2 {
		t.Errorf("CompletedCount() = %d, want 2", result.CompletedCount())
	}
	if len(result.Unresumable) != 2 {
		t.Errorf("Unresumable = %d, want 2 (excluded runs must be surfaced)", len(result.Unresumable))
	}
	for _, u := range result.Unresumable {
		if u.RunID == runID1 {
			continue
		}
		if u.RunID == runID3 {
			continue
		}
		t.Errorf("Unresumable contains unexpected RunID %q", u.RunID)
	}
}

func TestScan_UnresumableRunsPresent_DoesNotChangeCandidateSetOrOrder(t *testing.T) {
	// The presence of unresumable (completed) runs must not change the
	// resumable candidate set or its recency ordering.
	rootDir := t.TempDir()
	resumable1 := newTestRunFolder(t, rootDir, runID1)
	completed := newTestRunFolder(t, rootDir, runID2)
	resumable2 := newTestRunFolder(t, rootDir, runID3)
	writeArtifact(t, resumable1, "EXECUTION", t1) // oldest resumable
	writeArtifact(t, completed, "COMPLETED", t2)
	writeArtifact(t, resumable2, "PLANNING", t3) // newest resumable
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("Candidates = %d, want 2 (completed run must not be a candidate)", len(result.Candidates))
	}
	if result.Candidates[0].RunID != runID3 {
		t.Errorf("Candidates[0].RunID = %q, want %q (most recent resumable first)", result.Candidates[0].RunID, runID3)
	}
	if result.Candidates[1].RunID != runID1 {
		t.Errorf("Candidates[1].RunID = %q, want %q (oldest resumable last)", result.Candidates[1].RunID, runID1)
	}
}
