package session_test

// Tests for ConsultRoute stage carry-over (D2) and Seq monotonicity across
// mixed routing paths (D3): cross-stage deviation dispatches must carry the
// target row's stage, not state.CurrentState.Stage; and all Applied Seq values
// must be strictly increasing when auto-routed, consultant-routed, and
// HITL-rejected dispatches are interleaved.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// stage1DoneAutoState returns an ArtifactState that simulates an auto-mode run
// through consult-staged-orch.md having completed both Stage-1 rows (agent-a#1,
// agent-b#2). CurrentState.Stage = "1" (FormatStageValue("", 1)) is the
// load-bearing value for cross-stage deviation tests: it represents the last
// applied stage and should NOT appear in CompletedSteps for Stage-2 dispatches
// after the D2 fix.
func stage1DoneAutoState() domain.ArtifactState {
	return domain.ArtifactState{
		Workflow:        "consult-staged",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  2,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION",
			Stage:      "1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-b#2",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "EXECUTION", Stage: "1", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "agent-b#2", Phase: "EXECUTION", Stage: "1", Status: domain.StatusSUCCESS},
		},
	}
}

// alternatingApprovalReader implements domain.ApprovalReader. It returns
// first on the first ReadApproval call and rest on all subsequent calls.
// Used to produce a HITLRedispatch on the first HITL check and a
// HITLAccept on the second, simulating a human approval that arrives after
// one redispatch cycle.
type alternatingApprovalReader struct {
	mu    sync.Mutex
	count int
	first domain.HumanApproval
	rest  domain.HumanApproval
}

func (r *alternatingApprovalReader) ReadApproval(_ context.Context, _ string) domain.HumanApproval {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
	if r.count == 1 {
		return r.first
	}
	return r.rest
}

// TestSession_ConsultRoute_CrossStageDeviation_UsesTargetRowStage verifies that
// when a harness error triggers consultRoute and the target row is in a
// different stage than the last applied row, every CompletedStep written by
// consultRoute carries the stage of the target row, not state.CurrentState.Stage.
//
// Setup: consult-staged-orch.md in auto-mode, Stage-1 pre-seeded as complete
// (CurrentState.Stage = "1"). The engine auto-dispatches Stage-2/agent-a (row 2).
// The harness returns an error, so consultRoute is called with
// deviation.CurrentStage = "2" but state.CurrentState.Stage = "1". The
// consultant re-routes to row 2 (agent-a, Stage-2) and the harness succeeds.
//
// With the current code entryStage = state.CurrentState.Stage = "1", so the
// workflow CompletedStep carries Stage = "1" (wrong). After the fix the stage
// is derived from the target row using the deviation context, producing Stage = "2".
func TestSession_ConsultRoute_CrossStageDeviation_UsesTargetRowStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Row 2 is EXECUTION.Stage-2/agent-a after plan expansion (zero-based).
	consultant.queueDispatch("agent-a", "recover Stage-2 after harness failure", 2)
	consultant.queueStop("Stage-2 step completed after recovery")

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "consult-staged-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeConsultStagedPlan(t, dir)

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// Pre-seed: Stage-1 complete, Stage-2 pending.
	// CurrentState.Stage = "1" is the load-bearing value: it is the stage of the
	// last applied row and must NOT appear in CompletedSteps for Stage-2 dispatches.
	store.state = stage1DoneAutoState()
	store.exists = true

	// The engine auto-dispatches Stage-2/agent-a (row 2). The harness fails,
	// triggering consultRoute with deviation.CurrentStage = "2".
	f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("simulated harness failure on Stage-2/agent-a")})
	// The consultant re-routes to row 2 (agent-a). The harness succeeds this time.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "Stage-2/agent-a recovered",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "consult-staged",
		Task:                 "test task",
		IsNewRun:             false, // resume from pre-seeded Stage-1-done state
		RunFolder:            dir,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	_, err := ses.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("want nil error (harness error is a handled deviation), got %v", err)
	}

	// Find the workflow step applied by consultRoute: non-infrastructure, non-HITLRejected.
	var workflowStep *domain.CompletedStep
	for i := range store.Applied {
		s := &store.Applied[i]
		if !s.IsInfrastructure && !s.HITLRejected {
			workflowStep = s
		}
	}
	if workflowStep == nil {
		t.Fatal("want at least one non-infrastructure workflow step in store.Applied, got none; " +
			"the consultant-routed dispatch must produce a CompletedStep via Store.Apply")
	}

	// Primary assertion: the workflow step must carry Stage = "2" (derived from
	// the target row via deviation.CurrentStage), not Stage = "1" (from
	// state.CurrentState.Stage). With the current code entryStage captures the
	// old Stage-1 value and stamps it on the Stage-2 CompletedStep.
	const wantStage = "2"
	if workflowStep.Stage != wantStage {
		t.Errorf("want workflow CompletedStep.Stage = %q (target row stage), got %q; "+
			"consultRoute must derive the stage from the target row when the target row "+
			"is in a different stage than state.CurrentState.Stage",
			wantStage, workflowStep.Stage)
	}

	// Also verify the consultation record (infrastructure step) carries Stage = "2".
	// Every CompletedStep produced by consultRoute must reflect the target row's stage.
	for _, s := range store.Applied {
		if s.IsInfrastructure && !s.HITLRejected && s.Stage != "" && s.Stage != wantStage {
			t.Errorf("infrastructure ConsultStep.Stage = %q, want %q; "+
				"all CompletedSteps in consultRoute must carry the target row stage, "+
				"not the prior stage from state.CurrentState.Stage",
				s.Stage, wantStage)
		}
	}
}

// TestSession_MixedRoutingPaths_SeqStrictlyMonotonic verifies that a run mixing
// auto-routed dispatches, consultant-routed dispatches, and HITL-rejected rows
// produces a strictly increasing Seq column across all persisted CompletedSteps.
//
// Sequence:
//
//	(a) Stage-1/agent-a auto-dispatched and succeeds (Seq=1). After Apply:
//	    seq=1, GlobalSequence=1.
//	(b) Stage-1/agent-b auto-dispatch: harness returns an error, triggering
//	    consultRoute. Inside consultRoute:
//	    - consultStep consumed Seq=2; dispSeq = *seq+1 = 1+1 = 2 (same slot as
//	      consultSeq, because *seq was not updated by the consultStep Apply).
//	    - Agent-b is dispatched with HITLOverride=true. First attempt: SUCCESS +
//	      ApprovalFalse -> HITLRedispatch (rlRejStep, Seq=3). After rlRejStep
//	      Apply: *seq=3, currentAttemptSeq++ = 3.
//	    - Second attempt (HITL redispatch): SUCCESS + ApprovalTrue -> HITLAccept
//	      (workflowSeq=4). After workflowStep Apply: GlobalSequence=4.
//	    - consultRoute sets *seq = currentAttemptSeq = 3 (the agent suffix
//	      counter, NOT GlobalSequence). After return: seq=3, GlobalSequence=4.
//	(c) Stage-2/agent-a auto-dispatched. hitlAttemptSeq = seq+1 = 4.
//	    GlobalSequence is already 4 -> Seq=4 collision with workflowSeq from (b).
//
// The test asserts that all Applied[i].Seq values are strictly increasing.
// With the current code step (c) produces a duplicate Seq=4.
// After the fix *seq is reconciled to state.GlobalSequence after consultRoute
// returns, so the next auto-dispatch gets Seq=5.
func TestSession_MixedRoutingPaths_SeqStrictlyMonotonic(t *testing.T) {
	hitlOverride := true
	outputs := []string{"stage-1-b-output.md"}

	consultant := &scriptedRoutingConsultant{}
	// consultRoute is invoked when Stage-1/agent-b harness fails.
	// Dispatch row 1 (agent-b) with HITLOverride=true and one output artifact so
	// the HITL check fires inside consultRoute.
	// AlternatingApprovalReader: first ReadApproval returns False (HITLRedispatch),
	// second returns True (HITLAccept). This exercises both the rlRejStep path
	// and the final workflowStep path within a single consultRoute call.
	consultant.queueDispatchWithHITLAndOutputs("agent-b", "re-route Stage-1/agent-b", 1, &hitlOverride, &outputs)
	// After consultRoute returns, the outer auto loop dispatches Stage-2 rows.
	// No further consultant instructions needed -- the run completes on its own
	// via auto-routing.

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
		Approvals: &alternatingApprovalReader{first: domain.ApprovalFalse, rest: domain.ApprovalTrue},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// (a) Stage-1/agent-a: auto-routed, succeeds normally (Seq=1).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "Stage-1/agent-a done",
	}})
	// (b) Stage-1/agent-b: harness error triggers consultRoute.
	f.Queue("agent-b", harness.ScriptedEntry{Err: errors.New("simulated Stage-1/agent-b harness error")})
	// Inside consultRoute: first agent-b dispatch (HITLOverride=true).
	// ApprovalFalse -> HITLRedispatch (rlRejStep applied at Seq=3).
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "Stage-1/agent-b first attempt",
	}})
	// HITL redispatch: ApprovalTrue -> HITLAccept (workflowStep applied at Seq=4).
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "Stage-1/agent-b accepted after redispatch",
	}})
	// (c) Stage-2/agent-a: auto-dispatched after consultRoute returns.
	// With the bug: hitlAttemptSeq = seq+1 = 4 == GlobalSequence (already used).
	// With the fix: hitlAttemptSeq = GlobalSequence+1 = 5 (reconciled seq).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "Stage-2/agent-a done",
	}})
	// Stage-2/agent-b: auto-dispatched to complete the run.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "Stage-2/agent-b done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "consult-staged",
		Task:                 "test task",
		IsNewRun:             true,
		RunFolder:            dir,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// Primary assertion: every Applied[i].Seq must be strictly greater than
	// Applied[i-1].Seq. With the current code, the auto-dispatched Stage-2/agent-a
	// step (c) computes Seq = seq+1 = 4, which duplicates the workflowSeq = 4
	// that consultRoute applied for Stage-1/agent-b.
	if len(store.Applied) < 4 {
		t.Fatalf("want at least 4 applied steps (consult, rlRejStep, workflowStep, "+
			"Stage-2/agent-a), got %d; the run may have terminated earlier than expected",
			len(store.Applied))
	}
	for i := 1; i < len(store.Applied); i++ {
		if store.Applied[i].Seq <= store.Applied[i-1].Seq {
			t.Errorf("store.Applied[%d].Seq=%d is not greater than Applied[%d].Seq=%d; "+
				"Seq values must be strictly increasing across auto-routed, consultant-routed, "+
				"and HITL-rejected dispatches (D3 Seq reconciliation bug)",
				i, store.Applied[i].Seq, i-1, store.Applied[i-1].Seq)
		}
	}
}
