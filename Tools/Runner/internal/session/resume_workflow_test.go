package session_test

// resume_workflow_test.go covers what happens when a resumed run's recorded
// workflow no longer exists in the orchestrator file.
//
// A resumed run brings its workflow with it: the run recorded a workflow ID
// when it was created and resumes under that ID without asking again. That
// makes the orchestrator file the one thing that can have changed underneath
// it -- a workflow renamed or removed since the run was created leaves the run
// naming something that is no longer there.
//
// The run must be refused, and the refusal must name both halves of the
// problem: which workflow could not be found, and which run was asking for it.
// Naming only one leaves the user unable to tell whether to fix the
// orchestrator file or abandon the run.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// resumedRunID is the run being resumed in the tests below. It is a well-formed
// run_id so that nothing refuses it for its shape.
const resumedRunID = "20260701T120000Z-a3f9"

// missingWorkflowID names a workflow that the linear-orch.md fixture does not
// declare -- standing in for one renamed or removed since the run was created.
const missingWorkflowID = "renamed-away-workflow"

// resolvableWorkflowID names the workflow the linear-orch.md fixture declares.
const resolvableWorkflowID = "linear"

// startResume starts a run being resumed under the given recorded workflow ID
// against the linear-orch.md fixture, and returns the outcome.
//
// store carries the run's recorded state; pass a memStore with exists set for a
// run that is genuinely mid-execution, or a zero-value one for a resume that is
// expected to be refused before the artifact is ever read.
func startResume(t *testing.T, recordedWorkflow string, store *memStore) domain.RunOutcome {
	t.Helper()

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	ses := session.New(session.Deps{
		Harness:  harness.NewFakeAdapter(),
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           domain.WorkflowID(recordedWorkflow),
		RunID:                resumedRunID,
		RunFolder:            filepath.Join(dir, "Orchestration-"+resumedRunID),
		IsNewRun:             false,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := ses.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Start: want nil error for a refusal (refusals are encoded in the outcome), got %v", err)
	}
	return got
}

// startResumeWithMissingWorkflow starts a resumed run whose recorded workflow
// is absent from the orchestrator file, and returns the outcome.
func startResumeWithMissingWorkflow(t *testing.T) domain.RunOutcome {
	t.Helper()
	return startResume(t, missingWorkflowID, &memStore{})
}

// TestSession_Start_Resume_RecordedWorkflowGone_RefusesRun verifies that a
// resumed run naming a workflow the orchestrator file no longer declares is
// refused rather than started.
//
// There is nothing to fall back to: the run cannot be executed under a
// different workflow than the one it recorded, and guessing at a replacement
// would run the user's work through a process they never chose.
func TestSession_Start_Resume_RecordedWorkflowGone_RefusesRun(t *testing.T) {
	// Arrange / Act
	got := startResumeWithMissingWorkflow(t)

	// Assert
	if got.Status != domain.RunRefused {
		t.Errorf("Status = %q, want %q; a resumed run whose recorded workflow no longer "+
			"exists cannot be executed and must be refused (message: %q)",
			got.Status, domain.RunRefused, got.Message)
	}
}

// TestSession_Start_Resume_RecordedWorkflowGone_RefusalNamesTheWorkflow
// verifies that the refusal states which workflow could not be found.
//
// The user's next action is to look for that workflow in the orchestrator file,
// which they can only do if the refusal says which one it was.
func TestSession_Start_Resume_RecordedWorkflowGone_RefusalNamesTheWorkflow(t *testing.T) {
	// Arrange / Act
	got := startResumeWithMissingWorkflow(t)

	// Assert
	if !strings.Contains(got.Message, missingWorkflowID) {
		t.Errorf("refusal message does not name the workflow that could not be found (%q); "+
			"the user cannot act on the refusal without it. Message: %q",
			missingWorkflowID, got.Message)
	}
}

// TestSession_Start_Resume_RecordedWorkflowGone_RefusalNamesTheRun verifies
// that the refusal states which run was being resumed.
//
// A workspace holds many runs. Without the run_id the user knows a workflow is
// missing but not which run is stranded by its absence.
func TestSession_Start_Resume_RecordedWorkflowGone_RefusalNamesTheRun(t *testing.T) {
	// Arrange / Act
	got := startResumeWithMissingWorkflow(t)

	// Assert
	if !strings.Contains(got.Message, resumedRunID) {
		t.Errorf("refusal message does not name the run being resumed (%q); a workspace holds "+
			"many runs and the message must say which one is affected. Message: %q",
			resumedRunID, got.Message)
	}
}

// TestSession_Start_Resume_RecordedWorkflowGone_RefusalIsStructured verifies
// that the refusal carries a structured cause a frontend can inspect, rather
// than only a sentence to print.
//
// The cause attributes the refusal to workflow resolution and to the run it
// concerns, so a frontend can react to this specific condition -- offering to
// pick a different run, say -- instead of pattern-matching on wording.
func TestSession_Start_Resume_RecordedWorkflowGone_RefusalIsStructured(t *testing.T) {
	// Arrange / Act
	got := startResumeWithMissingWorkflow(t)

	// Assert
	var refusal *domain.RefusalError
	if !errors.As(got.Cause, &refusal) {
		t.Fatalf("outcome Cause = %v (%T), want a *domain.RefusalError so a frontend can "+
			"recognise the condition without reading the message", got.Cause, got.Cause)
	}
	if refusal.Component != "workflow" {
		t.Errorf("RefusalError.Component = %q, want %q; the refusal is attributable to "+
			"resolving the run's workflow", refusal.Component, "workflow")
	}
	if refusal.Resource != resumedRunID {
		t.Errorf("RefusalError.Resource = %q, want the run being resumed (%q)",
			refusal.Resource, resumedRunID)
	}
}

// TestSession_Start_Resume_RecordedWorkflowResolves_IsNotRefused verifies that a
// resumed run whose recorded workflow the orchestrator file still declares is
// allowed to proceed.
//
// This is the positive counterpart to the refusals above, and it is what makes
// them mean anything: a session that refused every resume outright would satisfy
// all four of them and strand every user whose workspace is perfectly fine.
func TestSession_Start_Resume_RecordedWorkflowResolves_IsNotRefused(t *testing.T) {
	// Arrange: a run that has completed its first step and is waiting on agent-b.
	store := &memStore{
		exists: true,
		state: domain.ArtifactState{
			Workflow:        resolvableWorkflowID,
			WorkflowVersion: "1.0", // matches the version linear-orch.md declares: no drift
			Task:            "test task",
			GlobalSequence:  1,
			RunSettings:    domain.RunSettings{Mode: domain.ExecutionModeAuto},
			CurrentState: domain.CurrentState{
				Phase:      "PLANNING",
				LastStatus: domain.StatusSUCCESS,
				LastAgent:  "agent-a#1",
			},
			ExecutionLog: []domain.ExecutionLogEntry{
				{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			},
		},
	}

	// Act
	got := startResume(t, resolvableWorkflowID, store)

	// Assert
	if got.Status == domain.RunRefused {
		t.Errorf("Status = %q for a resumed run whose recorded workflow the orchestrator file "+
			"still declares; nothing is wrong with this run and it must be allowed to "+
			"continue (message: %q)", got.Status, got.Message)
	}
}

// startResumeWithNoRecordedWorkflow starts a resumed run that carries no
// recorded workflow at all, and returns the outcome.
func startResumeWithNoRecordedWorkflow(t *testing.T) domain.RunOutcome {
	t.Helper()
	return startResume(t, "", &memStore{})
}

// TestSession_Start_Resume_NoRecordedWorkflow_IsRefused verifies that a run
// resumed with no recorded workflow at all is refused.
//
// A run records no workflow when its artifact frontmatter is missing or
// unreadable. There is nothing to resume it as, and no safe way to guess: the
// run must be refused rather than started under a workflow it never named.
func TestSession_Start_Resume_NoRecordedWorkflow_IsRefused(t *testing.T) {
	// Arrange / Act
	got := startResumeWithNoRecordedWorkflow(t)

	// Assert
	if got.Status != domain.RunRefused {
		t.Errorf("Status = %q, want %q; a run being resumed with no recorded workflow has "+
			"nothing to execute and must be refused rather than started under a guess "+
			"(message: %q)", got.Status, domain.RunRefused, got.Message)
	}
}

// TestSession_Start_Resume_NoRecordedWorkflow_RefusalNamesTheRun verifies that
// the refusal states which run recorded no workflow.
//
// This is the same requirement the missing-workflow refusal is held to, for the
// same reason: a workspace holds many runs, and a refusal that does not say
// which one is affected leaves the user without a next step. It is stated
// separately here because this refusal has no workflow identifier to fall back
// on -- the run ID is the only thing that can identify the problem at all.
func TestSession_Start_Resume_NoRecordedWorkflow_RefusalNamesTheRun(t *testing.T) {
	// Arrange / Act
	got := startResumeWithNoRecordedWorkflow(t)

	// Assert
	if !strings.Contains(got.Message, resumedRunID) {
		t.Errorf("refusal message does not name the run that recorded no workflow (%q); it is "+
			"the only identifier this refusal has, and without it the user is told that "+
			"something recorded no workflow but not what. Message: %q",
			resumedRunID, got.Message)
	}
}

// TestSession_Start_Resume_NoRecordedWorkflow_RefusalSaysNoWorkflowWasRecorded
// verifies that the refusal states the actual problem -- that the run recorded
// no workflow -- rather than reporting a lookup that failed for an identifier
// the user never sees.
//
// The distinction matters because the two conditions call for different actions.
// A recorded workflow that has gone missing sends the user to the orchestrator
// file to look for it. A run that recorded nothing sends them to the run's own
// artifact, which is missing or unreadable; there is no workflow to go looking
// for, and a message that implies there is sends them somewhere with nothing to
// find.
//
// Several phrasings say this, so the assertion accepts any of them rather than
// pinning one. What it does not accept is a message that renders the absent
// identifier as an empty quoted string -- "workflow '' ..." reads as a bug in
// the tool, not as a description of the run's state. The new-run path is held to
// the same standard by TestSession_Start_NewRun_WorkflowNotFound_RefusalNamesNoRun.
func TestSession_Start_Resume_NoRecordedWorkflow_RefusalSaysNoWorkflowWasRecorded(t *testing.T) {
	// Arrange / Act
	got := startResumeWithNoRecordedWorkflow(t)

	// Assert
	if strings.Contains(got.Message, `""`) || strings.Contains(got.Message, "''") {
		t.Errorf("the refusal renders the absent workflow as an empty quoted identifier, which "+
			"reads as a bug rather than as a statement that the run recorded no workflow: %q",
			got.Message)
	}

	lower := strings.ToLower(got.Message)
	phrasings := []string{"no workflow", "no recorded workflow", "records no workflow",
		"did not record a workflow", "without a workflow"}
	for _, p := range phrasings {
		if strings.Contains(lower, p) {
			return
		}
	}
	t.Errorf("refusal message does not state that the run recorded no workflow; it must say so "+
		"rather than report a failed lookup, because the two conditions send the user to "+
		"different places. Wanted any of %v, got: %q", phrasings, got.Message)
}

// TestSession_Start_Resume_NoRecordedWorkflow_RefusalIsStructured verifies that
// this refusal, like its sibling, carries a structured cause.
//
// The two conditions call for different actions, and the file's own rationale is
// that a frontend should be able to tell them apart without pattern-matching on
// wording. It cannot do that if only one of them carries a cause: a refusal with
// no cause is indistinguishable from any other failure, so the frontend is back
// to reading the sentence for exactly the case where the sentence is the thing
// most likely to be reworded.
func TestSession_Start_Resume_NoRecordedWorkflow_RefusalIsStructured(t *testing.T) {
	// Arrange / Act
	got := startResumeWithNoRecordedWorkflow(t)

	// Assert
	var refusal *domain.RefusalError
	if !errors.As(got.Cause, &refusal) {
		t.Fatalf("outcome Cause = %v (%T), want a *domain.RefusalError; the missing-workflow "+
			"refusal carries one and a frontend cannot distinguish the two conditions if this "+
			"one does not", got.Cause, got.Cause)
	}
	if refusal.Component != "workflow" {
		t.Errorf("RefusalError.Component = %q, want %q; the refusal is attributable to "+
			"resolving the run's workflow", refusal.Component, "workflow")
	}
	if refusal.Resource != resumedRunID {
		t.Errorf("RefusalError.Resource = %q, want the run being resumed (%q); the run is the "+
			"only thing this refusal can name", refusal.Resource, resumedRunID)
	}
}

// TestSession_Start_Resume_RecordedTaskSurvivesAResumeWithNoConfiguredTask
// verifies that resuming a run without supplying a task leaves the task the run
// recorded when it was created intact.
//
// A resumed run is not asked for its task -- that is the whole point of the
// skip, and the setup flow deliberately carries an empty task through the resume
// path on the understanding that the recorded one is read from the artifact and
// never overwritten. Nothing else states that understanding, so an
// implementation that threads the configured task into the resume path would
// blank the run's task on the next save with every other test still passing, and
// the run would lose the description of the work it exists to do.
func TestSession_Start_Resume_RecordedTaskSurvivesAResumeWithNoConfiguredTask(t *testing.T) {
	// Arrange: a run mid-execution -- agent-a is done, agent-b is next -- that
	// recorded a task when it was created.
	const recordedTask = "migrate the billing schema"
	store := &memStore{
		exists: true,
		state: domain.ArtifactState{
			Type:            "orchestration-artifact",
			RunID:           resumedRunID,
			Workflow:        resolvableWorkflowID,
			WorkflowVersion: "1.0", // matches the version linear-orch.md declares: no drift
			Task:            recordedTask,
			GlobalSequence:  1,
			RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
			CurrentState: domain.CurrentState{
				Phase:      "PLANNING",
				LastStatus: domain.StatusSUCCESS,
				LastAgent:  "agent-a#1",
			},
			ExecutionLog: []domain.ExecutionLogEntry{
				{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			},
		},
	}

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// Act: resume with Task left at its zero value, as the setup flow does for a
	// resumed run -- it never asks the question, so it has no answer to pass on.
	got, err := ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           domain.WorkflowID(resolvableWorkflowID),
		RunID:                resumedRunID,
		RunFolder:            filepath.Join(dir, "Orchestration-"+resumedRunID),
		IsNewRun:             false,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	})

	// Assert
	if err != nil {
		t.Fatalf("Start: want nil error, got %v", err)
	}
	if got.Status == domain.RunRefused {
		t.Fatalf("Status = %q; this run resumes cleanly and the test says nothing about task "+
			"preservation unless it actually runs (message: %q)", got.Status, got.Message)
	}
	if len(store.Applied) == 0 {
		t.Fatalf("the run recorded no step, so nothing was ever saved and this test would pass " +
			"without exercising the path it exists to guard")
	}
	if store.state.Task != recordedTask {
		t.Errorf("the run's recorded task is %q after resuming with no configured task, want %q; "+
			"a resume must not overwrite the task the run was created with", store.state.Task, recordedTask)
	}
}

// TestSession_Start_NewRun_WorkflowNotFound_RefusalNamesNoRun verifies that the
// refusal a NEW run gets for an unresolvable workflow does not try to name a run
// that does not exist yet.
//
// The resume refusal names the run being resumed because a workspace holds many
// runs and the user needs to know which one is stranded. A new run has no such
// history, and an implementation that reaches for the run ID unconditionally
// renders an empty one -- "run """ -- which reads as a bug rather than as
// guidance. The naming has to be specific to the resume path, and this is the
// only test that says so.
func TestSession_Start_NewRun_WorkflowNotFound_RefusalNamesNoRun(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	ses := session.New(session.Deps{
		Harness:  harness.NewFakeAdapter(),
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// Act: a new run, so no run has been recorded under any workflow yet.
	got, err := ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           missingWorkflowID,
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	})
	if err != nil {
		t.Fatalf("Start: want nil error for a refusal (refusals are encoded in the outcome), got %v", err)
	}

	// Assert
	if got.Status != domain.RunRefused {
		t.Fatalf("Status = %q, want %q (message: %q)", got.Status, domain.RunRefused, got.Message)
	}
	if strings.Contains(got.Message, `""`) || strings.Contains(got.Message, "''") {
		t.Errorf("the refusal a new run got renders an empty quoted identifier, which means it "+
			"named a run that does not exist: %q. Naming the run belongs to the resume path "+
			"only", got.Message)
	}
}
