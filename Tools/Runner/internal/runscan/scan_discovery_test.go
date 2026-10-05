package runscan_test

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/runscan"
)

// ---- tests: empty / non-matching directories ----

func TestScan_EmptyDirectory_ZeroCandidates(t *testing.T) {
	// An empty rootDir must yield zero candidates and zero completed.
	rootDir := t.TempDir()
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Errorf("Candidates = %d, want 0", len(result.Candidates))
	}
	if result.CompletedCount() != 0 {
		t.Errorf("CompletedCount() = %d, want 0", result.CompletedCount())
	}
	if len(result.Unresumable) != 0 {
		t.Errorf("Unresumable = %d, want 0", len(result.Unresumable))
	}
}

func TestScan_NonMatchingFolderNames_AreIgnored(t *testing.T) {
	// Folders that do not match Orchestration-{run_id} must be ignored.
	rootDir := t.TempDir()
	for _, name := range []string{"src", "docs", "SomeOtherFolder", "plan.md"} {
		if err := os.MkdirAll(filepath.Join(rootDir, name), 0700); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Errorf("Candidates = %d, want 0 (non-matching folders must be ignored)", len(result.Candidates))
	}
}

func TestScan_OrchestrationPrefixWithNoRunID_IsIgnored(t *testing.T) {
	// "Orchestration-" with no suffix must not be matched.
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "Orchestration-"), 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Errorf("Candidates = %d, want 0 (empty run_id is not valid)", len(result.Candidates))
	}
}

func TestScan_OrchestrationNonFormatFolder_IsIgnored(t *testing.T) {
	// Directories starting with "Orchestration-" but whose suffix does not match
	// the run_id format (\d{8}T\d{6}Z-[0-9a-f]{4}) must be ignored.
	// Examples: "Orchestration-something-else", "Orchestration-not-a-run-id".
	rootDir := t.TempDir()
	for _, name := range []string{
		"Orchestration-something-else",
		"Orchestration-not-a-run-id",
		"Orchestration-20260101",             // incomplete timestamp, no hex suffix
		"Orchestration-XXXXXXXX000000Z-aaaa", // wrong timestamp chars
	} {
		if err := os.MkdirAll(filepath.Join(rootDir, name), 0700); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Errorf("Candidates = %d, want 0 (non-format run_id suffixes must be ignored)", len(result.Candidates))
	}
	if result.CompletedCount() != 0 {
		t.Errorf("CompletedCount() = %d, want 0 (non-format folders must not be counted)", result.CompletedCount())
	}
}

// ---- tests: resumable candidate classification ----

func TestScan_OneResumableCandidate_AppearInCandidates(t *testing.T) {
	// A single folder with a parseable non-COMPLETED artifact yields one candidate.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifact(t, folder, "EXECUTION", t1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Errorf("Candidates = %d, want 1", len(result.Candidates))
	}
}

func TestScan_MultipleResumableCandidates_AllAppearInCandidates(t *testing.T) {
	// Multiple non-COMPLETED folders all appear in Candidates.
	rootDir := t.TempDir()
	folder1 := newTestRunFolder(t, rootDir, runID1)
	folder2 := newTestRunFolder(t, rootDir, runID2)
	writeArtifact(t, folder1, "EXECUTION", t1)
	writeArtifact(t, folder2, "PLANNING", t2)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 2 {
		t.Errorf("Candidates = %d, want 2", len(result.Candidates))
	}
}

func TestScan_Candidate_RunIDExtractedFromFolderName(t *testing.T) {
	// The candidate's RunID must match the run_id in the folder name.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifact(t, folder, "EXECUTION", t1)
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

func TestScan_Candidate_FolderPathIsAbsolute(t *testing.T) {
	// The candidate's FolderPath must be absolute.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifact(t, folder, "EXECUTION", t1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) == 0 {
		t.Fatal("expected 1 candidate, got 0")
	}
	if !filepath.IsAbs(result.Candidates[0].FolderPath) {
		t.Errorf("FolderPath %q is not absolute", result.Candidates[0].FolderPath)
	}
}

func TestScan_Candidate_FolderPathPointsToMatchedFolder(t *testing.T) {
	// The candidate's FolderPath must point to the matched Orchestration-{run_id}/ folder.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifact(t, folder, "EXECUTION", t1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) == 0 {
		t.Fatal("expected 1 candidate, got 0")
	}

	wantPath := filepath.Join(rootDir, "Orchestration-"+runID1)
	gotPath := result.Candidates[0].FolderPath
	if gotPath != wantPath {
		t.Errorf("FolderPath = %q, want %q", gotPath, wantPath)
	}
}

func TestScan_Candidate_LastUpdatedFromArtifactFrontmatter(t *testing.T) {
	// The candidate's LastUpdated must reflect last_updated from the artifact frontmatter.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifact(t, folder, "EXECUTION", t2)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) == 0 {
		t.Fatal("expected 1 candidate, got 0")
	}
	if !result.Candidates[0].LastUpdated.Equal(t2) {
		t.Errorf("LastUpdated = %v, want %v", result.Candidates[0].LastUpdated, t2)
	}
}

func TestScan_Candidate_WorkflowAndTaskFromArtifactFrontmatter(t *testing.T) {
	// The candidate's Workflow and Task must reflect the artifact frontmatter.
	rootDir := t.TempDir()
	folder := newTestRunFolder(t, rootDir, runID1)
	writeArtifactWithMeta(t, folder, "EXECUTION", "greenfield-tdd", "Build the feature", t1)
	scanner := runscan.NewDirScanner()

	result, err := scanner.Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) == 0 {
		t.Fatal("expected 1 candidate, got 0")
	}
	c := result.Candidates[0]
	if c.Workflow != "greenfield-tdd" {
		t.Errorf("Workflow = %q, want %q", c.Workflow, "greenfield-tdd")
	}
	if c.Task != "Build the feature" {
		t.Errorf("Task = %q, want %q", c.Task, "Build the feature")
	}
}
