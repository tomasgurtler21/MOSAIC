package main

// tui_identity_workflow_test.go covers the workflow a resumed run recorded, on
// the entry point that settles run identity before the TUI launches.
//
// `mosaic-run --run <run_id>` names its run outright, so it never reaches the
// run-select screen -- and the run-select screen is where a chosen run picks up
// the workflow it recorded. Since the setup sequence no longer asks a resumed
// run which workflow to run, this branch is the only remaining place a resumed
// run's workflow can enter, and if it enters nowhere the run is refused for
// recording no workflow when its artifact records one.
//
// The tests constrain the outcome of resolveRunIdentityForTUI, not the route by
// which it gets there: reading the named run's artifact directly and routing the
// branch through runselect.Resolve both satisfy them.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
)

// writeRunArtifactRecordingWorkflow creates Orchestration-{runID}/ under workDir
// with a parseable in-progress artifact whose frontmatter records the given
// workflow, and returns the run folder path.
func writeRunArtifactRecordingWorkflow(t *testing.T, workDir, runID, workflow string) string {
	t.Helper()
	folder := filepath.Join(workDir, domain.RunScopedFolder(runID))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", folder, err)
	}
	content := fmt.Sprintf(`---
type: orchestration-artifact
workflow: %s
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
checkpoints: disabled
current_state:
  phase: EXECUTION
  stage: "1"
  last_status: SUCCESS
  last_agent: "agent#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent   | Phase     | Stage | Status  | Timestamp            | Summary | Checkpoint |
| --- | ------- | --------- | ----- | ------- | -------------------- | ------- | ---------- |
| 1   | agent#1 | EXECUTION | 1     | SUCCESS | 2026-01-01T00:00:00Z | done    | -          |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
</Artifacts>
`, workflow)
	if err := os.WriteFile(filepath.Join(folder, "Orchestration.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write run artifact: %v", err)
	}
	return folder
}

// TestResolveRunIdentityForTUI_NamedRun_CarriesTheRunsRecordedWorkflow verifies
// that resolving `--run <run_id>` reads the named run's recorded workflow and
// carries it out with the identity.
//
// The run-select screen is the only other place a resumed run's workflow is
// settled, and naming a run on the command line bypasses it entirely. Without
// this, the run reaches the session with no workflow and is refused for
// recording none -- while its artifact plainly records one, which sends the user
// looking for a problem that is not there.
func TestResolveRunIdentityForTUI_NamedRun_CarriesTheRunsRecordedWorkflow(t *testing.T) {
	// Arrange
	workDir := t.TempDir()
	const runID = "20260727T170000Z-a3f9"
	const recorded = "greenfield-tdd"
	writeRunArtifactRecordingWorkflow(t, workDir, runID, recorded)

	// Act
	identity, err := resolveRunIdentityForTUI([]string{"--run", runID}, workDir)

	// Assert
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--run %s) error = %v, want nil", runID, err)
	}
	if identity.Workflow != recorded {
		t.Errorf("Workflow = %q for a run whose artifact records %q, want %q; a resumed run is "+
			"no longer asked which workflow to run, so a workflow that is not carried here is "+
			"never supplied at all and the run is refused for recording none",
			identity.Workflow, recorded, recorded)
	}
}

// TestResolveRunIdentityForTUI_NamedRun_ArtifactRecordsNoWorkflow_CarriesNone
// verifies that a named run whose artifact records no workflow carries an empty
// one rather than a substitute.
//
// Guard: passes today because nothing populates Workflow at all. It becomes
// load-bearing once the field is populated, against a fallback that hands the
// run some other workflow. Resuming a run as something it never was is worse
// than the refusal, which at least says what is wrong.
func TestResolveRunIdentityForTUI_NamedRun_ArtifactRecordsNoWorkflow_CarriesNone(t *testing.T) {
	// Arrange: a run folder with no artifact to read a workflow from.
	workDir := t.TempDir()
	const runID = "20260727T170000Z-a3f9"
	folder := filepath.Join(workDir, domain.RunScopedFolder(runID))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", folder, err)
	}

	// Act
	identity, err := resolveRunIdentityForTUI([]string{"--run", runID}, workDir)

	// Assert
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--run %s) error = %v, want nil; an unreadable "+
			"artifact is the session's refusal to make, not an argument error", runID, err)
	}
	if identity.Workflow != "" {
		t.Errorf("Workflow = %q for a run recording none, want empty; a substituted workflow "+
			"resumes the run as something it never was", identity.Workflow)
	}
}

// TestResolveRunIdentityForTUI_NewRunFlag_CarriesNoWorkflow verifies that a
// freshly minted run carries no recorded workflow.
//
// Guard: a new run has recorded nothing, and is still asked which workflow to
// run. Carrying a value here would settle that question behind the user's back.
func TestResolveRunIdentityForTUI_NewRunFlag_CarriesNoWorkflow(t *testing.T) {
	// Arrange
	workDir := t.TempDir()

	// Act
	identity, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)

	// Assert
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--new-run) error = %v, want nil", err)
	}
	if identity.Workflow != "" {
		t.Errorf("Workflow = %q for a new run, want empty; a new run has recorded nothing and "+
			"must still be asked which workflow to run", identity.Workflow)
	}
}

// TestResolveRunIdentityForTUI_Deferred_CarriesNoWorkflow verifies that the
// deferred shape -- identity left to the run-select screen -- carries no
// workflow either.
//
// Guard: which run is being resumed is not yet known, so there is no recorded
// workflow to read. The chosen run's workflow is settled on the screen.
func TestResolveRunIdentityForTUI_Deferred_CarriesNoWorkflow(t *testing.T) {
	// Arrange: two resumable runs recording different workflows.
	workDir := t.TempDir()
	writeRunArtifactRecordingWorkflow(t, workDir, "20260727T170000Z-a3f9", "greenfield-tdd")
	writeRunArtifactRecordingWorkflow(t, workDir, "20260727T180000Z-b1c2", "brownfield-tdd")

	// Act
	identity, err := resolveRunIdentityForTUI([]string{}, workDir)

	// Assert
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(deferred) error = %v, want nil", err)
	}
	if identity.Workflow != "" {
		t.Errorf("Workflow = %q while the run is still unchosen, want empty; picking one of "+
			"the candidates' workflows before the user picks the run decides the question for "+
			"them", identity.Workflow)
	}
}
