package session_test

// Tests for consultation failure classes, consultations leaving no row for a
// non-default orchestrator identifier, and resume of artifacts written by an
// earlier Runner version that still carry consultation rows (position recovery
// and rewind for both default and non-default orchestrator identifiers).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Consultation failure classes =====

// TestSession_ConsultationFailureClasses_AllTerminal verifies that each
// orchestrator failure class terminates the run with RunStoppedByConsultant.
// This covers ConsultFailMissingField, ConsultFailUnknownAction, and
// ConsultFailUnknownAgent — the three classes not exercised by the existing
// ConsultationFailure tests (which already cover ConsultFailMalformedJSON and
// ConsultFailTransport).
func TestSession_ConsultationFailureClasses_AllTerminal(t *testing.T) {
	classes := []domain.ConsultationFailure{
		domain.ConsultFailMissingField,
		domain.ConsultFailUnknownAction,
		domain.ConsultFailUnknownAgent,
	}
	for _, failClass := range classes {
		failClass := failClass
		t.Run(string(failClass), func(t *testing.T) {
			consultant := &scriptedRoutingConsultant{}
			consultant.queueError(failClass)

			ses, _, _, orchPath := newOrchestratedSession(t, consultant)

			got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

			if err != nil {
				t.Fatalf("want nil error (failure encoded in RunOutcome for %s), got %v", failClass, err)
			}
			if got.Status != domain.RunStoppedByConsultant {
				t.Errorf("want RunStoppedByConsultant for %s failure class, got %q (message: %q)",
					failClass, got.Status, got.Message)
			}
		})
	}
}

// ===== Consultation leaves no artifact trace =====

// TestSession_Consultation_NonDefaultOrchestrator_LeavesNoRow verifies that
// consultations of a script orchestrator with a non-default identifier
// ("custom-orch.md") leave no Execution Log row either: the artifact holds only
// the workflow step the consultation chose, and no entry is attributed to the
// orchestrator.
func TestSession_Consultation_NonDefaultOrchestrator_LeavesNoRow(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do work", 0)
	consultant.queueStop("done")

	dir := t.TempDir()
	// Write the orchestrator under a non-default filename stem so that the
	// resolved Identifier ("custom-orch") differs from every hardcoded string
	// that could exist in the implementation.
	data, err := os.ReadFile(orchFilePath("linear-orch.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	orchPath := filepath.Join(dir, "custom-orch.md")
	if err := os.WriteFile(orchPath, data, 0600); err != nil {
		t.Fatalf("write orchestrator: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// Only agent-a is recorded, at the first sequence slot.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}
	ses.Start(context.Background(), cfg) //nolint:errcheck

	for _, step := range store.Applied {
		if step.IsInfrastructure || strings.HasPrefix(step.AgentInstance, "custom-orch#") {
			t.Errorf("want no row attributed to the orchestrator, got %q (Seq %d)",
				step.AgentInstance, step.Seq)
		}
	}
	if len(store.Applied) != 1 || store.Applied[0].Seq != 1 {
		t.Errorf("want exactly one applied row (agent-a at Seq 1), got %+v", store.Applied)
	}
	if consultant.CallCount != 2 {
		t.Errorf("want 2 consultations observed through the consultant, got %d", consultant.CallCount)
	}
}

// TestSession_Resume_WithTrailingConsultationRow_PositionRecovery verifies that
// when a run is resumed and the execution log ends with a consultation row after
// the last workflow step, the session correctly identifies the last workflow
// step and advances to the step that follows it.
//
// Log layout: [agent-a#1 (workflow), orchestrator#2 (consultation/infra)]
// current_state: agent-a#1 (matching — no interruption)
// Expected: session dispatches agent-b (the step following agent-a in the
// linear workflow), proving it skipped the trailing consultation row.
func TestSession_Resume_WithTrailingConsultationRow_PositionRecovery(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Consulted once to decide the next step (agent-b) after the resume.
	consultant.queueDispatch("agent-b", "step 2", 1)
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	// Pre-populate: agent-a completed (seq=1) then a consultation row (seq=2).
	// The consultation row's Agent is "orchestrator#2" — the stem of
	// "orchestrator.md", which newOrchestratedSession writes for the fixture.
	// current_state records agent-a#1, matching the last workflow log entry
	// (no interruption).
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  2,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "orchestrator#2", Phase: "", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseOrchestratedConfig(orchPath)
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error on resume (consultation row must not cause PositionUnresolvedError), got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after stop instruction, got %q (message: %q)",
			got.Status, got.Message)
	}
	// agent-b must have been dispatched: if the resume had not correctly
	// recognised the consultation row as infrastructure and skipped it, it
	// would have returned RunFailed with PositionUnresolvedError instead.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation (agent-b) on resume, got 0")
	}
	if invs[0].Agent.Identifier != "agent-b" {
		t.Errorf("want resumed session to dispatch agent-b (step following agent-a), got %q",
			invs[0].Agent.Identifier)
	}
}

// TestSession_Resume_Rewind_WithConsultationRowsInLog_CorrectlyIdentifiesLastWorkflowStep
// verifies the re-run rewind path (FR-33): when a run is resumed after a
// mid-invocation interruption and the execution log contains consultation rows,
// the rewind correctly identifies the last completed workflow step by skipping
// the consultation rows, then routes to the step that was interrupted.
//
// Log layout:
//   - seq=1: orchestrator#1 (consultation/infra — first routing decision)
//   - seq=2: agent-a#2      (workflow step — completed)
//   - seq=3: orchestrator#3 (consultation/infra — next routing decision)
//
// current_state.LastAgent = "agent-b#4" (premature: set before agent-b's
// Apply completed, then the session crashed).
//
// The rewind must skip orchestrator#3, find agent-a#2 as the last completed
// workflow step, and route to agent-b (the step that was being dispatched when
// the crash occurred).
func TestSession_Resume_Rewind_WithConsultationRowsInLog_CorrectlyIdentifiesLastWorkflowStep(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// After the rewind the session consults for the next step and dispatches agent-b.
	consultant.queueDispatch("agent-b", "step 2 (retry after interruption)", 1)
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	// Pre-populate: consultation (seq=1), agent-a completed (seq=2), another
	// consultation (seq=3). current_state was advanced to "agent-b#4" before
	// the session crashed without recording agent-b's Apply, so agent-b is absent
	// from the log. This is the mid-invocation interruption scenario.
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  3,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-b#4", // premature: agent-b was never logged
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "orchestrator#1", Phase: "", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "agent-a#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 3, Agent: "orchestrator#3", Phase: "", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseOrchestratedConfig(orchPath)
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error on resume (consultation rows must not prevent rewind), got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after stop instruction, got %q (message: %q)",
			got.Status, got.Message)
	}
	// agent-b must have been dispatched: a correct rewind sets CurrentState to
	// agent-a#2 (the last completed workflow step), which routes the engine to
	// agent-b. If the rewind had failed to skip the consultation rows it would
	// have returned RunFailed with PositionUnresolvedError, or re-dispatched
	// agent-a unnecessarily.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation (agent-b) on resume, got 0")
	}
	if invs[0].Agent.Identifier != "agent-b" {
		t.Errorf("want rewind to route to agent-b (the interrupted step), got %q",
			invs[0].Agent.Identifier)
	}
}

// TestSession_Resume_WithTrailingConsultationRow_NonDefaultOrchestrator_PositionRecovery
// is the non-default-orchestrator complement to
// TestSession_Resume_WithTrailingConsultationRow_PositionRecovery.
//
// Where that test relies on the default "orchestrator.md" stem (identifier
// "orchestrator"), this test names the orchestrator file "custom-orch.md" so the
// resolved identifier is "custom-orch". The pre-populated log carries
// "custom-orch#N" for the consultation entry. If position recovery hardcodes any
// string other than reading s.orchRef.Identifier, the consultation row will not be
// recognised as infrastructure and the test will fail with a PositionUnresolvedError
// or a wrong first dispatch.
//
// Log layout: [agent-a#1 (workflow), custom-orch#2 (consultation/infra)]
// current_state: agent-a#1 (matching — no interruption)
// Expected: session dispatches agent-b (the step after agent-a), then
// RunStoppedByConsultant when the consultant issues a stop.
func TestSession_Resume_WithTrailingConsultationRow_NonDefaultOrchestrator_PositionRecovery(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-b", "step 2", 1)
	consultant.queueStop("done")

	dir := t.TempDir()
	data, err := os.ReadFile(orchFilePath("linear-orch.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	orchPath := filepath.Join(dir, "custom-orch.md")
	if err := os.WriteFile(orchPath, data, 0600); err != nil {
		t.Fatalf("write orchestrator: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// Pre-populate: agent-a completed (seq=1) then a consultation row (seq=2).
	// The consultation row uses "custom-orch#2" — the stem of "custom-orch.md".
	// current_state records agent-a#1, matching the last workflow log entry
	// (no interruption).
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  2,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "custom-orch#2", Phase: "", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error on resume (consultation row with non-default orchestrator must not cause PositionUnresolvedError), got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after stop instruction, got %q (message: %q)",
			got.Status, got.Message)
	}
	// agent-b must have been dispatched: if position recovery failed to recognise
	// "custom-orch#2" as an infrastructure row it would have returned RunFailed
	// with PositionUnresolvedError instead.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation (agent-b) on resume, got 0")
	}
	if invs[0].Agent.Identifier != "agent-b" {
		t.Errorf("want resumed session to dispatch agent-b (step following agent-a), got %q",
			invs[0].Agent.Identifier)
	}
}

// TestSession_Resume_Rewind_WithConsultationRowsInLog_NonDefaultOrchestrator_CorrectlyIdentifiesLastWorkflowStep
// is the non-default-orchestrator complement to
// TestSession_Resume_Rewind_WithConsultationRowsInLog_CorrectlyIdentifiesLastWorkflowStep.
//
// The orchestrator file is named "custom-orch.md" so the resolved identifier is
// "custom-orch". Pre-populated consultation log entries carry "custom-orch#N".
// If the FR-33 rewind path hardcodes any specific string the consultation rows will
// not be recognised as infrastructure, the rewind will mis-identify the last
// completed workflow step, and the test will fail.
//
// Log layout:
//   - seq=1: custom-orch#1 (consultation/infra — first routing decision)
//   - seq=2: agent-a#2      (workflow step — completed)
//   - seq=3: custom-orch#3  (consultation/infra — next routing decision)
//
// current_state.LastAgent = "agent-b#4" (premature: set before agent-b's Apply
// completed, then the session crashed).
//
// The rewind must skip custom-orch#3, find agent-a#2 as the last completed
// workflow step, and route to agent-b (the step that was being dispatched when
// the crash occurred). Expected result: RunStoppedByConsultant.
func TestSession_Resume_Rewind_WithConsultationRowsInLog_NonDefaultOrchestrator_CorrectlyIdentifiesLastWorkflowStep(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-b", "step 2 (retry after interruption)", 1)
	consultant.queueStop("done")

	dir := t.TempDir()
	data, err := os.ReadFile(orchFilePath("linear-orch.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	orchPath := filepath.Join(dir, "custom-orch.md")
	if err := os.WriteFile(orchPath, data, 0600); err != nil {
		t.Fatalf("write orchestrator: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// Pre-populate: consultation (seq=1), agent-a completed (seq=2), another
	// consultation (seq=3). current_state was advanced to "agent-b#4" before the
	// session crashed without recording agent-b's Apply — the mid-invocation
	// interruption scenario. All consultation entries use "custom-orch#N".
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  3,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-b#4", // premature: agent-b was never logged
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "custom-orch#1", Phase: "", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "agent-a#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 3, Agent: "custom-orch#3", Phase: "", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error on resume (consultation rows with non-default orchestrator must not prevent rewind), got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after stop instruction, got %q (message: %q)",
			got.Status, got.Message)
	}
	// agent-b must have been dispatched: a correct rewind sets CurrentState to
	// agent-a#2 (the last completed workflow step), which routes the engine to
	// agent-b. If the rewind failed to skip "custom-orch#N" entries it would have
	// returned RunFailed with PositionUnresolvedError, or re-dispatched agent-a.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation (agent-b) on resume, got 0")
	}
	if invs[0].Agent.Identifier != "agent-b" {
		t.Errorf("want rewind to route to agent-b (the interrupted step), got %q",
			invs[0].Agent.Identifier)
	}
}
