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

// newTestRunFolder creates an Orchestration-{runID}/ subfolder inside rootDir
// and returns the folder's absolute path.
func newTestRunFolder(t *testing.T, rootDir, runID string) string {
	t.Helper()
	folderName := "Orchestration-" + runID
	path := filepath.Join(rootDir, folderName)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatalf("newTestRunFolder: %v", err)
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
