package runselect_test

// workflow_carry_test.go covers the workflow a resumed run already recorded
// travelling out of the selection decision alongside the run's identity.
//
// A run's workflow is part of what the run already is: it was chosen when the
// run was created and written into the run's artifact. Resuming that run means
// resuming it under that workflow, so the selection decision -- the one place
// that turns a scan into a settled run identity -- must hand the recorded
// workflow to its caller rather than leave the caller to ask for it again.
//
// Both routes into a settled identity are covered: Resolve, for a run named
// explicitly ahead of time, and Answer, for a run picked from the question.
// A new run has no recorded workflow and must carry none.

import (
	"testing"
	"time"

	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// carryRunID is a well-formed run_id used by the tests in this file.
const carryRunID = "20260701T120000Z-a3f9"

// carryFolder is the run-scoped folder matching carryRunID.
const carryFolder = "/ws/Orchestration-20260701T120000Z-a3f9"

// recordedWorkflowCandidate returns a scan candidate for a run that recorded
// the given workflow and is otherwise mid-execution.
func recordedWorkflowCandidate(workflow string) runscan.RunCandidate {
	return runscan.RunCandidate{
		RunInfo: runscan.RunInfo{
			RunID:       carryRunID,
			FolderPath:  carryFolder,
			LastUpdated: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			Workflow:    workflow,
			Task:        "test task",
			Phase:       "EXECUTION",
			Stage:       "Stage-1",
			LastAgent:   "impl#1",
		},
	}
}

// resolveByRunID settles the identity of the run named by carryRunID against a
// scan containing the given candidates, failing the test on any error or on a
// question rather than a resolution.
func resolveByRunID(t *testing.T, candidates ...runscan.RunCandidate) runselect.Identity {
	t.Helper()
	dec, err := runselect.Resolve(runselect.Request{
		Scan:      runscan.ScanResult{Candidates: candidates},
		WorkDir:   "/ws",
		RunIDFlag: carryRunID,
	}, failingMinter(t))
	if err != nil {
		t.Fatalf("Resolve(run %s): unexpected error: %v", carryRunID, err)
	}
	if dec.Resolved == nil {
		t.Fatalf("Resolve(run %s): want a resolved identity, got a question", carryRunID)
	}
	return *dec.Resolved
}

// answerByRunID settles the identity of the run named by carryRunID by
// answering the question built from a scan containing the given candidates.
func answerByRunID(t *testing.T, candidates ...runscan.RunCandidate) runselect.Identity {
	t.Helper()
	dec, err := runselect.Resolve(runselect.Request{
		Scan:    runscan.ScanResult{Candidates: candidates},
		WorkDir: "/ws",
	}, failingMinter(t))
	if err != nil {
		t.Fatalf("Resolve(no explicit input): unexpected error: %v", err)
	}
	if dec.Question == nil {
		t.Fatalf("Resolve(no explicit input): want a question, got %+v", dec.Resolved)
	}
	id, err := runselect.Answer(*dec.Question, carryRunID, failingMinter(t))
	if err != nil {
		t.Fatalf("Answer(%s): unexpected error: %v", carryRunID, err)
	}
	return id
}

// failingMinter returns a Minter that fails the test if it is ever called.
// Every test here settles on an existing run, which must never mint.
func failingMinter(t *testing.T) runselect.Minter {
	t.Helper()
	return func() (string, string) {
		t.Fatalf("mint called while settling on an existing run; a resumed run keeps its own identity")
		return "", ""
	}
}

// ---------------------------------------------------------------------------
// Identity carries the recorded workflow
// ---------------------------------------------------------------------------

// TestResolve_NamedRun_IdentityCarriesRecordedWorkflow verifies that naming a
// run explicitly settles an identity that states which workflow that run was
// recorded under.
//
// Without this, the caller holds a run it can resume but no idea what to resume
// it as, and has no choice but to ask a question the run already answered.
func TestResolve_NamedRun_IdentityCarriesRecordedWorkflow(t *testing.T) {
	// Arrange
	scan := recordedWorkflowCandidate("standard-development")

	// Act
	id := resolveByRunID(t, scan)

	// Assert
	if id.Workflow != "standard-development" {
		t.Errorf("Identity.Workflow = %q, want %q; a resumed run must carry the workflow "+
			"recorded in its artifact so the caller never has to ask for it again",
			id.Workflow, "standard-development")
	}
}

// TestAnswer_ChosenRun_IdentityCarriesRecordedWorkflow verifies that choosing a
// run from the question settles an identity carrying that run's recorded
// workflow, exactly as naming it explicitly does.
//
// The two routes into a settled identity must agree; a user picking a run from
// a list is resuming the same run under the same workflow as a user naming it.
func TestAnswer_ChosenRun_IdentityCarriesRecordedWorkflow(t *testing.T) {
	// Arrange
	scan := recordedWorkflowCandidate("standard-development")

	// Act
	id := answerByRunID(t, scan)

	// Assert
	if id.Workflow != "standard-development" {
		t.Errorf("Identity.Workflow = %q, want %q; choosing a run from the question must "+
			"carry its recorded workflow just as naming the run does",
			id.Workflow, "standard-development")
	}
}

// TestAnswer_NewRun_IdentityCarriesNoWorkflow verifies that a newly minted run
// carries no workflow.
//
// A new run has not recorded anything yet: its workflow is the one the user is
// about to choose. Carrying a stale value here would silently pre-answer a
// question the user must still be asked.
func TestAnswer_NewRun_IdentityCarriesNoWorkflow(t *testing.T) {
	// Arrange
	scan := runscan.ScanResult{Candidates: []runscan.RunCandidate{recordedWorkflowCandidate("standard-development")}}
	dec, err := runselect.Resolve(runselect.Request{Scan: scan, WorkDir: "/ws"}, func() (string, string) {
		return "20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"
	})
	if err != nil {
		t.Fatalf("Resolve(no explicit input): unexpected error: %v", err)
	}

	// Act
	id, err := runselect.Answer(*dec.Question, runselect.NewRunChoiceID, func() (string, string) {
		return "20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"
	})
	if err != nil {
		t.Fatalf("Answer(new run): unexpected error: %v", err)
	}

	// Assert
	if id.Workflow != "" {
		t.Errorf("Identity.Workflow = %q for a new run, want empty; a new run has recorded "+
			"no workflow and must not pre-answer the workflow question", id.Workflow)
	}
}

// TestAnswer_ChosenRun_RecordingNoWorkflow_CarriesNoWorkflow verifies that
// resuming a run whose artifact records no workflow settles an identity that
// says so, rather than one that has been filled in from somewhere else.
//
// A run records no workflow when its artifact frontmatter is missing or
// unreadable. The honest answer is "this run does not say", which the caller can
// refuse on; substituting a plausible workflow -- the only other run's, the
// first the orchestrator file declares -- would run the user's work through a
// process nothing ever chose, and the user would have no way to tell.
func TestAnswer_ChosenRun_RecordingNoWorkflow_CarriesNoWorkflow(t *testing.T) {
	// Arrange
	unrecorded := runscan.RunCandidate{
		RunInfo: runscan.RunInfo{
			RunID:       carryRunID,
			FolderPath:  carryFolder,
			LastUpdated: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			Phase:       "EXECUTION",
			LastAgent:   "impl#1",
		},
	}

	// Act
	id := answerByRunID(t, unrecorded)

	// Assert
	if id.Workflow != "" {
		t.Errorf("Identity.Workflow = %q for a run whose artifact records no workflow, want "+
			"empty; an absent recorded workflow must travel as absent so the caller can "+
			"refuse the resume, not be filled in with a workflow the run never named",
			id.Workflow)
	}
}

// ---------------------------------------------------------------------------
// Position carries the recorded workflow
// ---------------------------------------------------------------------------

// TestResolve_NamedRun_PositionCarriesRecordedWorkflow verifies that the
// recorded workflow appears in the resumed run's reported position, alongside
// the phase, stage, and agent it was last at.
//
// The position is what the caller shows the user to confirm the run is the
// intended one; which workflow the run is executing belongs in that statement.
func TestResolve_NamedRun_PositionCarriesRecordedWorkflow(t *testing.T) {
	// Arrange
	scan := recordedWorkflowCandidate("standard-development")

	// Act
	id := resolveByRunID(t, scan)

	// Assert
	if id.Position == nil {
		t.Fatalf("Identity.Position = nil for a run recording phase, stage, agent and workflow")
	}
	if id.Position.Workflow != "standard-development" {
		t.Errorf("Position.Workflow = %q, want %q", id.Position.Workflow, "standard-development")
	}
}

// TestResolve_RunRecordingOnlyWorkflow_HasPosition verifies that a run which
// recorded a workflow but has not yet executed anything still reports a
// position.
//
// A created-but-unstarted run has no phase, no stage, no agent and no update
// time -- but it does know its workflow, and that is the one thing the caller
// needs to resume it. Discarding the whole position because the execution
// fields are blank would throw that away and force the workflow question to be
// asked again.
func TestResolve_RunRecordingOnlyWorkflow_HasPosition(t *testing.T) {
	// Arrange
	unstarted := runscan.RunCandidate{
		RunInfo: runscan.RunInfo{
			RunID:      carryRunID,
			FolderPath: carryFolder,
			Workflow:   "standard-development",
		},
	}

	// Act
	id := resolveByRunID(t, unstarted)

	// Assert
	if id.Position == nil {
		t.Fatalf("Identity.Position = nil for a created-but-unstarted run; the run records " +
			"its workflow, which is not nothing and must survive the selection decision")
	}
	if id.Position.Workflow != "standard-development" {
		t.Errorf("Position.Workflow = %q, want %q", id.Position.Workflow, "standard-development")
	}
}

// TestResolve_RunRecordingNothing_HasNoPosition verifies that a run whose
// artifact yielded no recognisable state at all reports no position.
//
// This is the counterpart to the test above: the position is dropped only when
// there is genuinely nothing recorded, not merely when execution has not begun.
func TestResolve_RunRecordingNothing_HasNoPosition(t *testing.T) {
	// Arrange
	blank := runscan.RunCandidate{
		RunInfo: runscan.RunInfo{
			RunID:      carryRunID,
			FolderPath: carryFolder,
		},
	}

	// Act
	id := resolveByRunID(t, blank)

	// Assert
	if id.Position != nil {
		t.Errorf("Identity.Position = %+v for a run recording nothing, want nil", id.Position)
	}
}
