package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Seeding: resume does not re-seed =====

// TestIntegration_Seeding_Resume_ExistingFilesPreservedByteIdentical verifies
// that resuming a run (IsNewRun false) does not re-copy seed inputs into the
// run folder. Any file already in the run folder retains its exact content
// regardless of what SeedInputs contains.
func TestIntegration_Seeding_Resume_ExistingFilesPreservedByteIdentical(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Pre-create the run folder with a valid Orchestration.md that encodes a
	// partially completed run: agent-a done (seq=1), agent-b pending.
	runDir := filepath.Join(dir, "Orchestration-"+integrationRunID)
	if err := os.MkdirAll(runDir, 0700); err != nil {
		t.Fatalf("mkdir runDir: %v", err)
	}
	const seedingResumeArtifact = `---
type: orchestration-artifact
run_id: 20260727T170000Z-a3f9
workflow: linear
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
runner_mode: auto
runner_pre_consultation: disabled
runner_manual_resolution: disabled
checkpoints: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "agent-a#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent     | Phase    | Stage | Status  | Timestamp            | Summary | Checkpoint |
| --- | --------- | -------- | ----- | ------- | -------------------- | ------- | ---------- |
| 1   | agent-a#1 | PLANNING | -     | SUCCESS | 2026-01-01T00:00:00Z | done    | -          |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
</Artifacts>

<WorkflowNotes type="core">
| Seq | Note |
| --- | ---- |
</WorkflowNotes>
`
	artifactPath := filepath.Join(runDir, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(seedingResumeArtifact), 0600); err != nil {
		t.Fatalf("write resume artifact: %v", err)
	}

	// A file already in the run folder simulating a seeded artifact from the
	// original run that may have been partially modified by the agents.
	preExistingContent := "# pre-existing: this content must survive the resume\n"
	if err := os.WriteFile(filepath.Join(runDir, "seeded.md"), []byte(preExistingContent), 0600); err != nil {
		t.Fatalf("write pre-existing seeded file: %v", err)
	}

	// Seed source: a file named "seeded.md" with different content.
	// If seeding ran on resume, runDir/seeded.md would be overwritten.
	srcFile := filepath.Join(dir, "seeded.md")
	newSeedContent := "# new seed: must NOT overwrite on resume\n"
	if err := os.WriteFile(srcFile, []byte(newSeedContent), 0600); err != nil {
		t.Fatalf("write seed source: %v", err)
	}

	f := harness.NewMockAdapter()
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false,

		RunFolder:  runDir,
		SeedInputs: []string{srcFile}, // ignored on resume
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// seeded.md must retain its pre-resume content — the session must not have
	// re-copied it from the seed source.
	gotContent, readErr := os.ReadFile(filepath.Join(runDir, "seeded.md"))
	if readErr != nil {
		t.Fatalf("read seeded.md after resume: %v", readErr)
	}
	if string(gotContent) != preExistingContent {
		t.Errorf("seeded.md after resume: got %q, want pre-existing %q (seeding must be skipped on resume)",
			string(gotContent), preExistingContent)
	}
}

// ===== Seeding: mid-copy failure removes the entire run folder =====

// TestIntegration_Seeding_MidCopyFailure_RunFolderCompletelyRemoved verifies
// that when seed.Apply fails partway through — after at least one file has
// been written — the session removes the entire run folder, including
// Orchestration.md and the already-copied files, and returns a RunRefused
// naming the failing file. No trace of the failed attempt remains on disk.
//
// The copy failure is induced by pre-populating the run folder with a regular
// file at a path that Apply's os.MkdirAll needs to turn into a directory.
// This technique is cross-platform: os.MkdirAll returns an error whenever a
// non-directory occupies any component of the target path.
func TestIntegration_Seeding_MidCopyFailure_RunFolderCompletelyRemoved(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Source directory:
	//   sources/
	//     first.md          → dest "first.md"          (copies successfully)
	//     Requirements.md   → dest "Requirements.md"    (sole candidate, no-op rename; copies successfully)
	//     sub/
	//       second.md       → dest "sub/second.md"      (fails — see sabotage below)
	srcDir := filepath.Join(dir, "sources")
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0700); err != nil {
		t.Fatalf("mkdir sources/sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "first.md"), []byte("first\n"), 0600); err != nil {
		t.Fatalf("write first.md: %v", err)
	}
	// Top-level Requirement* candidate, so the seed set has exactly one match
	// and seeding is not refused before Apply is reached.
	if err := os.WriteFile(filepath.Join(srcDir, "Requirements.md"), []byte("# Requirements\n"), 0600); err != nil {
		t.Fatalf("write Requirements.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sub", "second.md"), []byte("second\n"), 0600); err != nil {
		t.Fatalf("write sub/second.md: %v", err)
	}

	runDir := filepath.Join(dir, "run")

	// Pre-create the run folder so we can plant the sabotage file inside it.
	// fileStore.Create will call os.MkdirAll(runDir), which succeeds even if the
	// directory already exists, then write Orchestration.md.
	if err := os.MkdirAll(runDir, 0700); err != nil {
		t.Fatalf("pre-create runDir: %v", err)
	}
	// Sabotage: put a regular file at runDir/sub so that Apply's
	// os.MkdirAll(runDir/sub) fails. The walk visits first.md before sub/second.md
	// (lexical order), so first.md is successfully copied before the failure.
	if err := os.WriteFile(filepath.Join(runDir, "sub"), []byte("not a directory"), 0600); err != nil {
		t.Fatalf("write sabotage file: %v", err)
	}

	artifactPath := filepath.Join(runDir, "Orchestration.md")
	f := harness.NewMockAdapter()
	// No responses queued: Apply fails before the dispatch loop begins.

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runDir,
		SeedInputs:           []string{srcDir},
	}

	got, err := sess.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)

	// The entire run folder must be removed — including Orchestration.md and the
	// successfully-copied first.md. A mid-copy failure leaves no trace on disk.
	if _, statErr := os.Stat(runDir); !os.IsNotExist(statErr) {
		t.Errorf("want run folder removed after mid-copy failure, but it still exists at %s", runDir)
	}

	// The refusal message must reference the seed component and name the
	// specific entry that failed to copy. The failing entry is sub/second.md
	// (Apply visits the top-level files before descending into sub/; both
	// Requirements.md and first.md copy successfully, then
	// os.MkdirAll("runDir/sub") fails because the sabotage file occupies that
	// path). Both the component tag and the failing source path must appear
	// so the test distinguishes "named the right file" from "named some seed
	// problem."
	if !strings.Contains(msg, "seed") {
		t.Errorf("want refusal message to reference the seed component; got %q", msg)
	}
	if !strings.Contains(msg, "second.md") {
		t.Errorf("want refusal message to name the failing entry (second.md); got %q", msg)
	}

	// No agents must have been dispatched: the failure occurred before the dispatch loop.
	if len(f.Invocations()) != 0 {
		t.Errorf("want no harness invocations on seed copy failure, got %d", len(f.Invocations()))
	}
}

// ===== Seeding: invalid seed set leaves no run folder behind =====

// TestIntegration_Seeding_InvalidSeedSet_NoRunFolderLeftBehind verifies that
// when the seed set fails NewPlan's validation (here: a source path that
// does not exist), the run is refused and no run folder is created on disk
// at all -- seed planning happens, and is refused, before Store.Create ever
// runs. This is the ordering guarantee the Stage 5 reorder must preserve:
// only seed.Apply moves to after Store.Create, never seed.NewPlan.
func TestIntegration_Seeding_InvalidSeedSet_NoRunFolderLeftBehind(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	nonExistent := filepath.Join(dir, "does-not-exist.md")

	runDir := filepath.Join(dir, "run") // deliberately never created by this test
	artifactPath := filepath.Join(runDir, "Orchestration.md")

	f := harness.NewMockAdapter()
	// No responses queued: NewPlan must refuse before any dispatch.

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runDir,
		SeedInputs:           []string{nonExistent},
	}

	got, err := sess.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)
	if !strings.Contains(msg, "does-not-exist.md") {
		t.Errorf("want refusal message to name the missing source path; got %q", msg)
	}

	// No run folder must exist at all -- not even Orchestration.md.
	if _, statErr := os.Stat(runDir); !os.IsNotExist(statErr) {
		t.Errorf("want no run folder created for an invalid seed set, but one exists at %s", runDir)
	}

	if len(f.Invocations()) != 0 {
		t.Errorf("want no harness invocations for an invalid seed set, got %d", len(f.Invocations()))
	}
}
