package runscan_test

// Tests for the runscan package.
//
// Coverage:
//
//   Scan — empty / non-matching directories:
//   - A root directory with no children yields zero candidates and zero completed.
//   - Folders not matching the Orchestration-{run_id} pattern are ignored.
//   - "Orchestration-" with no run_id suffix is not matched.
//
//   Scan — resumable candidate classification:
//   - Exactly one matching folder with a parseable, non-COMPLETED artifact
//     appears in Candidates.
//   - Multiple matching folders all appear in Candidates when none is COMPLETED.
//   - RunCandidate.RunID is extracted from the folder name.
//   - RunCandidate.FolderPath is the absolute path to the matched folder.
//   - RunCandidate.LastUpdated reflects last_updated from the artifact frontmatter.
//   - RunCandidate.Workflow reflects the workflow ID from the artifact frontmatter.
//   - RunCandidate.Task reflects the task from the artifact frontmatter.
//
//   Scan — COMPLETED exclusion:
//   - A folder whose artifact has current_state.phase == "COMPLETED" (case-insensitive)
//     is excluded from Candidates.
//   - "completed" (all lowercase) is treated as COMPLETED (case-insensitive match).
//   - "Completed" (mixed case) is also excluded.
//   - ScanResult.CompletedCount() is incremented for each excluded completed run.
//   - When some runs are completed and others are not, only resumable runs appear
//     in Candidates.
//
//   Scan — unresumable runs are surfaced, not discarded:
//   - A COMPLETED run appears in Unresumable, not Candidates, with Reason
//     ReasonCompleted.
//   - An UnresumableRun carries the same identifying metadata a candidate
//     carries: RunID, FolderPath, LastUpdated, Workflow, Task.
//   - An UnresumableRun's Phase, Stage, and LastAgent are read straight from
//     current_state, not left zero-valued.
//   - A RunCandidate's Phase, Stage, and LastAgent are read straight from
//     current_state, not left zero-valued.
//   - An empty workspace yields zero Unresumable entries.
//   - Multiple completed runs all appear in Unresumable, ordered by
//     LastUpdated descending, independently of Candidates ordering.
//   - The presence of unresumable runs does not change the resumable
//     candidate set or its ordering.
//
//   UnresumableReason.Description():
//   - Never empty for ReasonCompleted.
//   - Never empty for an unrecognised UnresumableReason value (default branch).
//
//   Scan — graceful degradation for unparseable artifacts:
//   - A folder whose Orchestration.md is missing is treated as resumable;
//     RunCandidate.ParseError is non-nil.
//   - A folder whose Orchestration.md is present but unparseable (corrupt) is
//     treated as resumable; RunCandidate.ParseError is non-nil.
//   - A folder with a missing or unparseable artifact is not classified as
//     unresumable.
//
//   Scan — ordering:
//   - Candidates are returned sorted by LastUpdated descending (most recent first).
//
//   Scan — error propagation:
//   - A filesystem error while listing rootDir propagates as a non-nil error
//     from Scan (the rootDir itself does not exist).

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-run/internal/runscan"
)

// ---- fixture helpers ----

// writeArtifact writes a minimal parseable Orchestration.md with the given
// current_state.phase and last_updated timestamp into dir/Orchestration.md.
func writeArtifact(t *testing.T, dir, phase string, lastUpdated time.Time) {
	t.Helper()
	content := fmt.Sprintf(`---
type: orchestration-artifact
workflow: test-workflow
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: %s
global_sequence: 1
checkpoints: disabled
current_state:
  phase: %s
  stage: ""
  last_status: SUCCESS
  last_agent: "agent#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent   | Phase     | Stage | Status  | Timestamp            | Summary | Checkpoint |
| --- | ------- | --------- | ----- | ------- | -------------------- | ------- | ---------- |
| 1   | agent#1 | EXECUTION | -     | SUCCESS | 2026-01-01T00:00:00Z | done    | -          |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
</Artifacts>
`, lastUpdated.UTC().Format(time.RFC3339), phase)

	if err := os.WriteFile(filepath.Join(dir, "Orchestration.md"), []byte(content), 0600); err != nil {
		t.Fatalf("writeArtifact: %v", err)
	}
}

// writeArtifactWithMeta writes a minimal parseable Orchestration.md with
// workflow and task fields for testing metadata extraction.
func writeArtifactWithMeta(t *testing.T, dir, phase, workflow, task string, lastUpdated time.Time) {
	t.Helper()
	content := fmt.Sprintf(`---
type: orchestration-artifact
workflow: %s
workflow_version: "1.0"
task: %q
started: 2026-01-01T00:00:00Z
last_updated: %s
global_sequence: 1
checkpoints: disabled
current_state:
  phase: %s
  stage: ""
  last_status: SUCCESS
  last_agent: "agent#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent   | Phase     | Stage | Status  | Timestamp            | Summary | Checkpoint |
| --- | ------- | --------- | ----- | ------- | -------------------- | ------- | ---------- |
| 1   | agent#1 | EXECUTION | -     | SUCCESS | 2026-01-01T00:00:00Z | done    | -          |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
</Artifacts>
`, workflow, task, lastUpdated.UTC().Format(time.RFC3339), phase)

	if err := os.WriteFile(filepath.Join(dir, "Orchestration.md"), []byte(content), 0600); err != nil {
		t.Fatalf("writeArtifactWithMeta: %v", err)
	}
}

// writeArtifactWithState writes a minimal parseable Orchestration.md with an
// explicit stage and last_agent, for testing that RunInfo.Phase, .Stage, and
// .LastAgent are read straight from current_state.
func writeArtifactWithState(t *testing.T, dir, phase, stage, lastAgent string, lastUpdated time.Time) {
	t.Helper()
	content := fmt.Sprintf(`---
type: orchestration-artifact
workflow: test-workflow
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: %s
global_sequence: 1
checkpoints: disabled
current_state:
  phase: %s
  stage: %q
  last_status: SUCCESS
  last_agent: %q
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent   | Phase     | Stage | Status  | Timestamp            | Summary | Checkpoint |
| --- | ------- | --------- | ----- | ------- | -------------------- | ------- | ---------- |
| 1   | agent#1 | EXECUTION | -     | SUCCESS | 2026-01-01T00:00:00Z | done    | -          |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
</Artifacts>
`, lastUpdated.UTC().Format(time.RFC3339), phase, stage, lastAgent)

	if err := os.WriteFile(filepath.Join(dir, "Orchestration.md"), []byte(content), 0600); err != nil {
		t.Fatalf("writeArtifactWithState: %v", err)
	}
}

// makeRunFolder creates an Orchestration-{runID}/ subfolder inside rootDir
// and returns the folder's absolute path.
func makeRunFolder(t *testing.T, rootDir, runID string) string {
	t.Helper()
	folderName := "Orchestration-" + runID
	path := filepath.Join(rootDir, folderName)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatalf("makeRunFolder: %v", err)
	}
	return path
}

const (
	runID1 = "20260101T000000Z-aaaa"
	runID2 = "20260102T000000Z-bbbb"
	runID3 = "20260103T000000Z-cccc"
)

var (
	t1 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 = time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
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
		"Orchestration-20260101",      // incomplete timestamp, no hex suffix
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
	folder := makeRunFolder(t, rootDir, runID1)
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
	folder1 := makeRunFolder(t, rootDir, runID1)
	folder2 := makeRunFolder(t, rootDir, runID2)
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
	folder := makeRunFolder(t, rootDir, runID1)
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
	folder := makeRunFolder(t, rootDir, runID1)
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
	folder := makeRunFolder(t, rootDir, runID1)
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
	folder := makeRunFolder(t, rootDir, runID1)
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
	folder := makeRunFolder(t, rootDir, runID1)
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

// ---- tests: COMPLETED exclusion ----

func TestScan_CompletedRun_ExcludedFromCandidates(t *testing.T) {
	// A folder with current_state.phase == "COMPLETED" must not appear in Candidates.
	rootDir := t.TempDir()
	folder := makeRunFolder(t, rootDir, runID1)
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
	folder := makeRunFolder(t, rootDir, runID1)
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
	folder := makeRunFolder(t, rootDir, runID1)
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
	folder := makeRunFolder(t, rootDir, runID1)
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
	folder := makeRunFolder(t, rootDir, runID1)
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
	folder1 := makeRunFolder(t, rootDir, runID1)
	folder2 := makeRunFolder(t, rootDir, runID2)
	folder3 := makeRunFolder(t, rootDir, runID3)
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
	folder := makeRunFolder(t, rootDir, runID1)
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
	folder := makeRunFolder(t, rootDir, runID1)
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
	folder1 := makeRunFolder(t, rootDir, runID1)
	folder2 := makeRunFolder(t, rootDir, runID2)
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
	folder1 := makeRunFolder(t, rootDir, runID1)
	folder2 := makeRunFolder(t, rootDir, runID2)
	folder3 := makeRunFolder(t, rootDir, runID3)
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
	resumable1 := makeRunFolder(t, rootDir, runID1)
	completed := makeRunFolder(t, rootDir, runID2)
	resumable2 := makeRunFolder(t, rootDir, runID3)
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

// ---- tests: graceful degradation for unparseable artifacts ----

func TestScan_MissingArtifact_TreatedAsResumable(t *testing.T) {
	// A folder with no Orchestration.md must appear as a resumable candidate
	// with a non-nil ParseError.
	rootDir := t.TempDir()
	makeRunFolder(t, rootDir, runID1) // no Orchestration.md written
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
	folder := makeRunFolder(t, rootDir, runID1)
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
	makeRunFolder(t, rootDir, runID1)
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
	folder1 := makeRunFolder(t, rootDir, runID1)
	folder2 := makeRunFolder(t, rootDir, runID2)
	folder3 := makeRunFolder(t, rootDir, runID3)
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
