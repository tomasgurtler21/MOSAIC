package session_test

// Tests for ConsultRoute stage context preservation: the current_state.stage
// value must survive consultant-routed dispatches (not be wiped to ""), every
// CompletedStep produced by consultRoute must carry Stage=entryStage, recursive
// consultRoute calls must independently read the stage at their own entry, and
// HITL-rejection infrastructure steps must also carry the stage value.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// noticeCapturingInteraction wraps noopInteraction and records every Notify
// call. Tests use it to assert that consultRoute notification messages carry
// the correct stage context rather than the hardcoded "" the pre-fix code
// produces.
type noticeCapturingInteraction struct {
	noopInteraction
	mu      sync.Mutex
	Notices []interaction.Notice
}

func (c *noticeCapturingInteraction) Notify(_ context.Context, n interaction.Notice) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Notices = append(c.Notices, n)
}

func (c *noticeCapturingInteraction) allNotices() []interaction.Notice {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]interaction.Notice, len(c.Notices))
	copy(out, c.Notices)
	return out
}

// writeConsultStagedPlan writes a Plan.md with Stage-1 and Stage-2 into dir.
// consult-staged-orch.md uses the EXECUTION.[StageNumber] template, so the
// session must read Plan.md from RunFolder to expand the routing table to four
// rows (indices 0-3). This helper satisfies that requirement.
func writeConsultStagedPlan(t *testing.T, dir string) {
	t.Helper()
	const planContent = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First stage | - | FALSE |
| 2 | Stage Two | Second stage | 1 | FALSE |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(planContent), 0600); err != nil {
		t.Fatalf("writeConsultStagedPlan: %v", err)
	}
}

// newConsultStagedSession builds a session backed by consult-staged-orch.md
// in orchestrated mode. The fixture uses EXECUTION.[StageNumber] rows for
// agent-a and agent-b and requires Plan.md for template expansion; this helper
// writes Plan.md (Stage-1 and Stage-2) and sets RunFolder = dir so the session
// finds it. After expansion the routing table has four rows (indices 0-3):
//
//	0 = EXECUTION.Stage-1 / agent-a
//	1 = EXECUTION.Stage-1 / agent-b
//	2 = EXECUTION.Stage-2 / agent-a
//	3 = EXECUTION.Stage-2 / agent-b
//
// The session uses a no-op interaction. Callers that need notice capture should
// build the session inline as TestSession_ConsultRoute_PreservesCurrentStateStage
// does. runFolder is returned so callers can pass it to baseConsultStagedConfig.
func newConsultStagedSession(t *testing.T, consultant domain.RoutingConsultant) (
	ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string, runFolder string,
) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "consult-staged-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeConsultStagedPlan(t, dir)
	runFolder = dir

	f = harness.NewMockAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	return
}

// baseConsultStagedConfig returns a RunConfig for the consult-staged workflow
// in orchestrated resume mode (IsNewRun=false, store pre-seeded by caller).
// runFolder must point to the directory containing Plan.md so the session can
// expand the EXECUTION.[StageNumber] template rows.
func baseConsultStagedConfig(orchPath, runFolder string) domain.RunConfig {
	return domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "consult-staged",
		Task:                 "test task",
		IsNewRun:             false, // resume: store is pre-seeded with Stage-2 state
		RunFolder:            runFolder,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeOrchestrated,
		},
	}
}

// consultStagedStage2State returns an ArtifactState that simulates a
// consult-staged-orch.md run having completed Stage-1 (agent-a#1, agent-b#2)
// and Stage-2/agent-a (agent-a#3), with Stage-2/agent-b still pending.
// CurrentState.Stage="Stage-2" is the load-bearing value: tests assert that
// it is preserved (not wiped to "") after a consultant-routed dispatch.
//
// The Phase value "EXECUTION.Stage-2" is the expanded form of the
// EXECUTION.[StageNumber] template row after plan expansion.
func consultStagedStage2State() domain.ArtifactState {
	return domain.ArtifactState{
		Workflow:        "consult-staged",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  3,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION.Stage-2",
			Stage:      "Stage-2",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#3",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "EXECUTION.Stage-1", Stage: "Stage-1", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "agent-b#2", Phase: "EXECUTION.Stage-1", Stage: "Stage-1", Status: domain.StatusSUCCESS},
			{Seq: 3, Agent: "agent-a#3", Phase: "EXECUTION.Stage-2", Stage: "Stage-2", Status: domain.StatusSUCCESS},
		},
	}
}

// TestSession_ConsultRoute_PreservesCurrentStateStage verifies that after a
// consultant-routed dispatch completes in a multi-stage workflow, the
// artifact's current_state.stage retains the stage value that was in force
// when the consultation was triggered.
//
// The test uses consult-staged-orch.md (explicit Stage-1 and Stage-2 rows)
// and pre-seeds the store to represent a run that completed Stage-1 and
// Stage-2/agent-a, leaving Stage-2/agent-b as the next step. Stage="Stage-2"
// is load-bearing: if consultRoute omits Stage from the CompletedStep,
// Store.Apply overwrites current_state.stage with "", causing the next
// engine.Next() call to fail with "stage 0 has no entry in stage set".
//
// Secondary assertions verify that the CompletedStep produced by consultRoute
// carries Stage="Stage-2" and that at least one notification message emitted
// during the dispatch contains the stage value rather than the hardcoded "".
func TestSession_ConsultRoute_PreservesCurrentStateStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Row 3 is EXECUTION.Stage-2/agent-b after plan expansion (zero-based):
	// the template rows for agent-a and agent-b expand to indices 0-1 (Stage-1)
	// and 2-3 (Stage-2) given a Plan.md with Stage-1 and Stage-2.
	consultant.queueDispatch("agent-b", "complete Stage-2", 3)
	consultant.queueStop("Stage-2 complete")

	notices := &noticeCapturingInteraction{}

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "consult-staged-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeConsultStagedPlan(t, dir) // provides Stage-1 and Stage-2 for template expansion

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: notices,
	})

	// Pre-seed the store to simulate Stage-1 done and Stage-2/agent-a done,
	// so Stage="Stage-2" is the value consultRoute must not wipe to "".
	store.state = consultStagedStage2State()
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "Stage-2 complete",
	}})

	_, err := ses.Start(context.Background(), baseConsultStagedConfig(orchPath, dir))
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}

	// Primary assertion: current_state.stage must remain "Stage-2" after the
	// consultant-routed dispatch. Without the fix, consultRoute omits Stage
	// from CompletedStep, and Store.Apply overwrites current_state.stage with
	// "" because the step is not infrastructure.
	if store.state.CurrentState.Stage != "Stage-2" {
		t.Errorf("want current_state.stage=%q after consultant-routed dispatch in Stage-2, got %q; "+
			"consultRoute must capture state.CurrentState.Stage at entry (entryStage) and "+
			"populate every CompletedStep with Stage: entryStage so Store.Apply does not "+
			"wipe the stage context",
			"Stage-2", store.state.CurrentState.Stage)
	}

	// Secondary assertion: the non-infrastructure CompletedStep applied by
	// consultRoute must carry Stage="Stage-2". Find the last workflow step
	// (non-infrastructure, non-HITLRejected) in store.Applied.
	var workflowStep *domain.CompletedStep
	for i := range store.Applied {
		s := &store.Applied[i]
		if !s.IsInfrastructure && !s.HITLRejected {
			workflowStep = s
		}
	}
	if workflowStep == nil {
		t.Fatal("want at least one non-infrastructure workflow step in store.Applied, got none; "+
			"the consultant-routed dispatch must produce a CompletedStep recorded via Store.Apply")
	}
	if workflowStep.Stage != "Stage-2" {
		t.Errorf("want CompletedStep.Stage=%q for consultant-routed workflow step, got %q; "+
			"all CompletedStep literals in consultRoute must include Stage: entryStage",
			"Stage-2", workflowStep.Stage)
	}

	// Tertiary assertion: at least one notification emitted by consultRoute
	// must contain "Stage-2". The dispatch-start and step-done format strings
	// currently hardcode stage="" and must be updated to use entryStage.
	found := false
	for _, n := range notices.allNotices() {
		if strings.Contains(n.Message, "Stage-2") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want at least one notification message containing %q from consultRoute, "+
			"got none in %d total notifications; "+
			"consultRoute notification format strings must use entryStage, not hardcoded \"\"",
			"Stage-2", len(notices.allNotices()))
	}
}

// TestSession_ConsultRoute_RecursiveCall_UsesOwnEntryStage verifies that when
// consultRoute calls itself recursively (here triggered by a harness error that
// becomes a deviation re-route), the recursive call independently reads
// state.CurrentState.Stage at its own entry point and propagates that value to
// the CompletedStep it produces.
//
// The test pre-seeds Stage="Stage-2" and causes the first consultant-dispatched
// agent (agent-a, row 2) to fail at the harness level. consultRoute calls
// itself with a deviation; the recursive call dispatches agent-b (row 3), which
// succeeds. The CompletedStep applied by the recursive call must carry
// Stage="Stage-2" because state.CurrentState.Stage was "Stage-2" at the
// recursive entry (the parent's consultStep Apply is infrastructure and does
// not modify CurrentState).
//
// This guards against an implementation where the recursive call might receive
// a stale or zero stage value instead of reading from the current state at its
// own entry.
func TestSession_ConsultRoute_RecursiveCall_UsesOwnEntryStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// First dispatch: agent-a (row 2, Stage-2). Harness returns an error.
	consultant.queueDispatch("agent-a", "attempt Stage-2 step", 2)
	// The harness error triggers recursive consultRoute with a deviation.
	// Recursive dispatch: agent-b (row 3, Stage-2). Harness returns SUCCESS.
	consultant.queueDispatch("agent-b", "recover from deviation in Stage-2", 3)
	// After the recursive consultRoute returns, the outer loop resumes and
	// calls the consultant once more; stop to end the run cleanly.
	consultant.queueStop("Stage-2 deviation resolved")

	ses, f, store, orchPath, runFolder := newConsultStagedSession(t, consultant)

	// Pre-seed the store: Stage-1 done, Stage-2/agent-a done, Stage="Stage-2".
	store.state = consultStagedStage2State()
	store.exists = true

	// agent-a: harness error -> consultRoute recurses with a deviation.
	f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("simulated harness failure in Stage-2")})
	// agent-b: dispatched by the recursive consultRoute, returns SUCCESS.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "Stage-2 recovered",
	}})

	_, err := ses.Start(context.Background(), baseConsultStagedConfig(orchPath, runFolder))
	if err != nil {
		t.Fatalf("want nil error (harness error is a deviation, not a crash), got %v", err)
	}

	// The recursive consultRoute applied a CompletedStep for agent-b#4.
	// Its Stage field must be "Stage-2" because state.CurrentState.Stage was
	// "Stage-2" when the recursive call entered consultRoute (the parent's
	// consultStep is IsInfrastructure=true and does not update CurrentState).
	// Without the fix, the recursive call also omits Stage, setting it to "".
	var workflowStep *domain.CompletedStep
	for i := range store.Applied {
		s := &store.Applied[i]
		if !s.IsInfrastructure && !s.HITLRejected && s.AgentInstance == "agent-b#4" {
			workflowStep = s
		}
	}
	if workflowStep == nil {
		t.Fatal("want agent-b#4 CompletedStep in store.Applied (applied by recursive consultRoute), " +
			"got none; the recursive consultRoute must apply its accepted step via Store.Apply")
	}
	if workflowStep.Stage != "Stage-2" {
		t.Errorf("want CompletedStep.Stage=%q for the recursive consultRoute's accepted step, got %q; "+
			"the recursive call must read state.CurrentState.Stage at its own entry and "+
			"populate Stage: entryStage on its CompletedStep",
			"Stage-2", workflowStep.Stage)
	}

	// Also verify the artifact's current_state.stage after the full sequence.
	if store.state.CurrentState.Stage != "Stage-2" {
		t.Errorf("want current_state.stage=%q after recursive consultRoute completes, got %q",
			"Stage-2", store.state.CurrentState.Stage)
	}
}

// TestSession_ConsultRoute_HITLRejection_InfrastructureStepsCarryStage verifies
// that the HITL-rejection CompletedStep records produced inside consultRoute --
// rlRejStep (HITLRedispatch path) and elRejStep (HITLEscalate path) -- carry
// Stage="Stage-2" rather than the empty string the pre-fix code emits.
//
// The test pre-seeds a Stage-2 run and arranges for the consultant's first
// dispatch to trigger both a HITL redispatch and a HITL escalation:
//
//  1. Consultant dispatches agent-b (row 3) with HITLOverride=true and one
//     output artifact. fixedApprovalReader always returns ApprovalFalse.
//  2. First harness response: SUCCESS -> ApprovalFalse -> HITLRedispatch
//     rlRejStep (IsInfrastructure=true, HITLRejected=true) is applied;
//     its Stage field must be "Stage-2".
//  3. HITL redispatch response: SUCCESS -> ApprovalFalse, RedispatchUsed=true
//     -> HITLEscalate. elRejStep (IsInfrastructure=true, HITLRejected=true)
//     is applied; its Stage field must also be "Stage-2". consultRoute is
//     then called recursively with the escalation deviation.
//  4. Recursive consultRoute: consultant dispatches agent-b again without HITL;
//     third harness response -> SUCCESS, HITLAccept -> completedStep applied.
//  5. Outer loop resumes; consultant issues stop to end the run cleanly.
func TestSession_ConsultRoute_HITLRejection_InfrastructureStepsCarryStage(t *testing.T) {
	hitlTrue := true
	// A non-empty output list is required so the HITL check has at least one
	// artifact to evaluate. fixedApprovalReader ignores the path value and
	// returns ApprovalFalse for every call.
	outputs := []string{"hitl-output.md"}

	consultant := &scriptedRoutingConsultant{}
	// Call 1: dispatch agent-b with HITL=true and one output artifact.
	// Both the initial dispatch and the HITL redispatch return SUCCESS with
	// ApprovalFalse, causing redispatch then escalation.
	consultant.queueDispatchWithHITLAndOutputs("agent-b", "complete Stage-2 step with HITL", 3, &hitlTrue, &outputs)
	// Call 2: dispatched by the recursive consultRoute after HITL escalation.
	// No HITL override -> row.HITL=false -> HITLAccept on first attempt.
	consultant.queueDispatch("agent-b", "recover after HITL escalation", 3)
	// Call 3: outer dispatch loop resumes after the recursive call returns.
	consultant.queueStop("Stage-2 HITL recovery complete")

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "consult-staged-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeConsultStagedPlan(t, dir)

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   consultant,
		Approvals: &fixedApprovalReader{domain.ApprovalFalse}, // every artifact is non-compliant
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// Pre-seed the store: Stage-1 done, Stage-2/agent-a done, Stage-2/agent-b
	// pending. Stage="Stage-2" is the value rlRejStep and elRejStep must carry.
	store.state = consultStagedStage2State()
	store.exists = true

	// Three agent-b responses consumed in order by the HITL flow:
	//   1. First dispatch  -> SUCCESS, ApprovalFalse -> HITLRedispatch -> rlRejStep
	//   2. HITL redispatch -> SUCCESS, ApprovalFalse, RedispatchUsed -> HITLEscalate -> elRejStep
	//   3. Recursive dispatch (HITL=false) -> SUCCESS -> HITLAccept -> completedStep
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done (first attempt, HITL will reject)",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done (HITL redispatch, HITL will escalate)",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#7",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done (recursive dispatch after escalation, accepted)",
	}})

	ses.Start(context.Background(), baseConsultStagedConfig(orchPath, dir)) //nolint:errcheck

	// Primary assertion: every HITLRejected step applied by consultRoute must
	// carry Stage="Stage-2". Before the fix, rlRejStep and elRejStep omit the
	// Stage field, leaving it as "". An implementation that only sets Stage on
	// the final completedStep will fail this assertion.
	var hitlRejectedSteps []domain.CompletedStep
	for _, s := range store.Applied {
		if s.HITLRejected {
			hitlRejectedSteps = append(hitlRejectedSteps, s)
		}
	}
	if len(hitlRejectedSteps) == 0 {
		t.Fatal("want at least one HITLRejected=true step in store.Applied, got none; " +
			"the HITL redispatch and escalate paths must apply rejection steps before " +
			"redispatching or escalating, so the execution log has a record of every attempt")
	}
	for _, s := range hitlRejectedSteps {
		if s.Stage != "Stage-2" {
			t.Errorf("HITLRejected step %q: want Stage=%q, got %q; "+
				"all CompletedStep literals in consultRoute -- including rlRejStep (HITLRedispatch) "+
				"and elRejStep (HITLEscalate) -- must include Stage: entryStage so the "+
				"execution log reflects the stage context in force when the step was attempted",
				s.AgentInstance, "Stage-2", s.Stage)
		}
		if !s.IsInfrastructure {
			t.Errorf("HITLRejected step %q: want IsInfrastructure=true, got false; "+
				"HITL-rejected steps must not update current_state",
				s.AgentInstance)
		}
	}
}
