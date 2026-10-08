package session_test

// Tests for the conditions under which STAGE_END and PHASE_END infrastructure
// triggers fire: only after the last step of the stage (or phase) returned
// SUCCESS and, where HITL applies, passed HITL verification. The same rules must
// hold for auto-routed dispatches and for consultant-routed dispatches.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

const (
	condStageEndAgent = "commit-manager-git"
	condPhaseEndAgent = "review-agent-a"
)

const condOneStagePlan = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`

const condTwoStagePlan = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First stage | - | FALSE |
| 2 | Stage Two | Second stage | 1 | FALSE |
`

// newTriggerConditionSession builds a session over trigger-conditions-orch.md with
// the given plan text written next to the orchestrator file.
func newTriggerConditionSession(t *testing.T, plan string, approvals domain.ApprovalReader, consultant domain.RoutingConsultant) (
	ses session.Session, f *harness.MockAdapter, orchPath string,
) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "trigger-conditions-orch.md")
	for _, a := range []string{"implementation-tdd", "implementation-review", condStageEndAgent, condPhaseEndAgent} {
		writeAgentFile(t, dir, a)
	}
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(plan), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}
	f = harness.NewMockAdapter()
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     &memStore{},
		Routing:   consultant,
		Approvals: approvals,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

func condConfig(orchPath string, mode domain.ExecutionMode) domain.RunConfig {
	return domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: mode},
		RunFolder:            filepath.Dir(orchPath),
	}
}

func queueCond(f *harness.MockAdapter, agent string, status domain.StatusCode) {
	f.Queue(agent, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: agent + "#0",
		StatusCode:      status,
		StatusMessage:   "scripted " + string(status),
	}})
}

// invokedAgents returns the agent identifiers in invocation order.
func invokedAgents(f *harness.MockAdapter) []string {
	var out []string
	for _, inv := range f.Invocations() {
		out = append(out, inv.Agent.Identifier)
	}
	return out
}

func countAgent(agents []string, name string) int {
	n := 0
	for _, a := range agents {
		if a == name {
			n++
		}
	}
	return n
}

func requireAgentSequence(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("want invocation sequence %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want invocation sequence %v, got %v", want, got)
		}
	}
}

func requireNoBoundaryTriggers(t *testing.T, agents []string) {
	t.Helper()
	if n := countAgent(agents, condStageEndAgent); n != 0 {
		t.Errorf("STAGE_END must not fire, but %s was dispatched %d time(s): %v", condStageEndAgent, n, agents)
	}
	if n := countAgent(agents, condPhaseEndAgent); n != 0 {
		t.Errorf("PHASE_END must not fire, but %s was dispatched %d time(s): %v", condPhaseEndAgent, n, agents)
	}
}

var condNonSuccessStatuses = []domain.StatusCode{
	domain.StatusCOMPLETED_NEEDS_ACTION,
	domain.StatusPARTIALLY_DONE,
	domain.StatusBLOCKED,
	domain.StatusNEEDS_CLARIFICATION,
}

// ===== auto-routed dispatch =====

func TestSession_Start_BoundaryTriggers_NonSuccessLastStepFiresNothing(t *testing.T) {
	for _, status := range condNonSuccessStatuses {
		t.Run(string(status), func(t *testing.T) {
			ses, f, orchPath := newTriggerConditionSession(t, condOneStagePlan, &fixedApprovalReader{approval: domain.ApprovalTrue}, nil)
			queueCond(f, "implementation-tdd", domain.StatusSUCCESS)
			queueCond(f, "implementation-review", status)

			_, _ = ses.Start(context.Background(), condConfig(orchPath, domain.ExecutionModeAuto))

			requireNoBoundaryTriggers(t, invokedAgents(f))
		})
	}
}

func TestSession_Start_BoundaryTriggers_HITLRejectedSuccessFiresNothing(t *testing.T) {
	ses, f, orchPath := newTriggerConditionSession(t, condOneStagePlan, &fixedApprovalReader{approval: domain.ApprovalFalse}, nil)
	queueCond(f, "implementation-tdd", domain.StatusSUCCESS)
	// Original attempt and the single HITL redispatch are both rejected.
	queueCond(f, "implementation-review", domain.StatusSUCCESS)
	queueCond(f, "implementation-review", domain.StatusSUCCESS)

	_, _ = ses.Start(context.Background(), condConfig(orchPath, domain.ExecutionModeAuto))

	requireNoBoundaryTriggers(t, invokedAgents(f))
}

func TestSession_Start_BoundaryTriggers_FireOnceAfterRejectedThenAcceptedSuccess(t *testing.T) {
	approvals := &switchingApprovalReader{firstApproval: domain.ApprovalFalse, restApproval: domain.ApprovalTrue}
	ses, f, orchPath := newTriggerConditionSession(t, condOneStagePlan, approvals, nil)
	queueCond(f, "implementation-tdd", domain.StatusSUCCESS)
	queueCond(f, "implementation-review", domain.StatusSUCCESS) // rejected
	queueCond(f, "implementation-review", domain.StatusSUCCESS) // accepted
	queueCond(f, condStageEndAgent, domain.StatusSUCCESS)
	queueCond(f, condPhaseEndAgent, domain.StatusSUCCESS)

	got, err := ses.Start(context.Background(), condConfig(orchPath, domain.ExecutionModeAuto))

	requireRunStatus(t, got, err, domain.RunCompleted)
	requireAgentSequence(t, invokedAgents(f), []string{
		"implementation-tdd", "implementation-review", "implementation-review", condStageEndAgent, condPhaseEndAgent,
	})
}

func TestSession_Start_BoundaryTriggers_PhaseEndOnlyAtLastStage(t *testing.T) {
	ses, f, orchPath := newTriggerConditionSession(t, condTwoStagePlan, &fixedApprovalReader{approval: domain.ApprovalTrue}, nil)
	for i := 0; i < 2; i++ {
		queueCond(f, "implementation-tdd", domain.StatusSUCCESS)
		queueCond(f, "implementation-review", domain.StatusSUCCESS)
		queueCond(f, condStageEndAgent, domain.StatusSUCCESS)
	}
	queueCond(f, condPhaseEndAgent, domain.StatusSUCCESS)

	got, err := ses.Start(context.Background(), condConfig(orchPath, domain.ExecutionModeAuto))

	requireRunStatus(t, got, err, domain.RunCompleted)
	requireAgentSequence(t, invokedAgents(f), []string{
		"implementation-tdd", "implementation-review", condStageEndAgent,
		"implementation-tdd", "implementation-review", condStageEndAgent, condPhaseEndAgent,
	})
}

// A reviewer that reports findings is a deviation from the auto route; the
// consultant sends the run back to the implementer and then to the reviewer.
func TestSession_Start_BoundaryTriggers_ReviewerFindingsThenSuccessFiresOnlyOnSuccess(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("implementation-tdd", "fix findings", 0, 1)
	// After the fix the auto route resumes on its own and re-runs the reviewer;
	// the next consultation happens only after the boundary triggers and stops.
	consultant.queueStop("done")
	ses, f, orchPath := newTriggerConditionSession(t, condOneStagePlan, &fixedApprovalReader{approval: domain.ApprovalTrue}, consultant)
	queueCond(f, "implementation-tdd", domain.StatusSUCCESS)
	queueCond(f, "implementation-review", domain.StatusCOMPLETED_NEEDS_ACTION)
	queueCond(f, "implementation-tdd", domain.StatusSUCCESS)
	queueCond(f, "implementation-review", domain.StatusSUCCESS)
	queueCond(f, condStageEndAgent, domain.StatusSUCCESS)
	queueCond(f, condPhaseEndAgent, domain.StatusSUCCESS)

	_, _ = ses.Start(context.Background(), condConfig(orchPath, domain.ExecutionModeAuto))

	requireAgentSequence(t, invokedAgents(f), []string{
		"implementation-tdd", "implementation-review", // findings: nothing fires
		"implementation-tdd", "implementation-review", // success: both fire
		condStageEndAgent, condPhaseEndAgent,
	})
}

// ===== consultant-routed dispatch =====

// newConsultTriggerSession builds an orchestrated session that resumes inside
// Stage-1 of trigger-conditions-orch.md with the implementer already done, so
// the consultant-routed dispatches carry a stage. Rows (single-stage plan):
// 0 = implementation-tdd, 1 = implementation-review.
func newConsultTriggerSession(t *testing.T, plan string, approvals domain.ApprovalReader, consultant *scriptedRoutingConsultant) (
	ses session.Session, f *harness.MockAdapter, cfg domain.RunConfig,
) {
	t.Helper()
	dir := scopedTempDir(t)
	orchPath := copyOrchestratorFile(t, dir, "trigger-conditions-orch.md")
	for _, a := range []string{"implementation-tdd", "implementation-review", condStageEndAgent, condPhaseEndAgent} {
		writeAgentFile(t, dir, a)
	}
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(plan), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}
	store := &memStore{}
	store.state = domain.ArtifactState{
		Workflow:        "staged",
		WorkflowVersion: "1.0",
		Task:            "task",
		GlobalSequence:  1,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION.Stage-1",
			Stage:      "Stage-1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "implementation-tdd#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "implementation-tdd#1", Phase: "EXECUTION.Stage-1", Stage: "Stage-1", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true
	f = harness.NewMockAdapter()
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   consultant,
		Approvals: approvals,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	cfg = domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             false,
		RunID:                testRunID,
		RunFolder:            dir,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}
	return
}

func TestSession_Consult_BoundaryTriggers_NonSuccessLastStepFiresNothing(t *testing.T) {
	for _, status := range condNonSuccessStatuses {
		t.Run(string(status), func(t *testing.T) {
			consultant := &scriptedRoutingConsultant{}
			consultant.queueStagedDispatch("implementation-review", "review", 1, 1)
			consultant.queueStop("done")
			ses, f, cfg := newConsultTriggerSession(t, condOneStagePlan, &fixedApprovalReader{approval: domain.ApprovalTrue}, consultant)
			queueCond(f, "implementation-review", status)

			_, _ = ses.Start(context.Background(), cfg)

			requireNoBoundaryTriggers(t, invokedAgents(f))
		})
	}
}

func TestSession_Consult_BoundaryTriggers_HITLRejectedSuccessFiresNothing(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("implementation-review", "review", 1, 1)
	consultant.queueStop("done")
	ses, f, cfg := newConsultTriggerSession(t, condOneStagePlan, &fixedApprovalReader{approval: domain.ApprovalFalse}, consultant)
	queueCond(f, "implementation-review", domain.StatusSUCCESS)
	queueCond(f, "implementation-review", domain.StatusSUCCESS)

	_, _ = ses.Start(context.Background(), cfg)

	requireNoBoundaryTriggers(t, invokedAgents(f))
}

func TestSession_Consult_BoundaryTriggers_FireOnceAfterAcceptedSuccess(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("implementation-review", "review", 1, 1)
	consultant.queueStop("done")
	ses, f, cfg := newConsultTriggerSession(t, condOneStagePlan, &fixedApprovalReader{approval: domain.ApprovalTrue}, consultant)
	queueCond(f, "implementation-review", domain.StatusSUCCESS)
	queueCond(f, condStageEndAgent, domain.StatusSUCCESS)
	queueCond(f, condPhaseEndAgent, domain.StatusSUCCESS)

	_, _ = ses.Start(context.Background(), cfg)

	requireAgentSequence(t, invokedAgents(f), []string{
		"implementation-review", condStageEndAgent, condPhaseEndAgent,
	})
}

func TestSession_Consult_BoundaryTriggers_PhaseEndNotFiredBeforeLastStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("implementation-review", "review", 1, 1)
	consultant.queueStop("done")
	ses, f, cfg := newConsultTriggerSession(t, condTwoStagePlan, &fixedApprovalReader{approval: domain.ApprovalTrue}, consultant)
	queueCond(f, "implementation-review", domain.StatusSUCCESS)
	queueCond(f, condStageEndAgent, domain.StatusSUCCESS)

	_, _ = ses.Start(context.Background(), cfg)

	requireAgentSequence(t, invokedAgents(f), []string{"implementation-review", condStageEndAgent})
}

func TestSession_Consult_BoundaryTriggers_ReviewerFindingsThenSuccessFiresOnlyOnSuccess(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("implementation-review", "review", 1, 1)
	consultant.queueStagedDispatch("implementation-tdd", "fix findings", 0, 1)
	consultant.queueStagedDispatch("implementation-review", "re-review", 1, 1)
	consultant.queueStop("done")
	ses, f, cfg := newConsultTriggerSession(t, condOneStagePlan, &fixedApprovalReader{approval: domain.ApprovalTrue}, consultant)
	queueCond(f, "implementation-review", domain.StatusCOMPLETED_NEEDS_ACTION)
	queueCond(f, "implementation-tdd", domain.StatusSUCCESS)
	queueCond(f, "implementation-review", domain.StatusSUCCESS)
	queueCond(f, condStageEndAgent, domain.StatusSUCCESS)
	queueCond(f, condPhaseEndAgent, domain.StatusSUCCESS)

	_, _ = ses.Start(context.Background(), cfg)

	requireAgentSequence(t, invokedAgents(f), []string{
		"implementation-review",
		"implementation-tdd", "implementation-review",
		condStageEndAgent, condPhaseEndAgent,
	})
}
