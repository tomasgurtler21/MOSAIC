package session_test

// Tests for ConsultRoute stage carry-over and Seq monotonicity across mixed
// routing paths: cross-stage deviation dispatches must carry the target row's
// stage, not state.CurrentState.Stage; and all recorded Seq values must be
// strictly increasing, with no collision between a consultation and the
// dispatch it chose, when auto-routed, consultant-routed, HITL-rejected,
// harness-error and resumed dispatches are interleaved. Consultations consume
// no sequence slot.

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
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
	// Row 0 is EXECUTION.[StageNumber]/agent-a (zero-based); stage 2 is named
	// by the instruction.
	consultant.queueStagedDispatch("agent-a", "recover Stage-2 after harness failure", 0, 2)
	consultant.queueStop("Stage-2 step completed after recovery")

	dir := scopedTempDir(t)
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

	// The engine auto-dispatches Stage-2/agent-a (row 2). The harness fails on
	// every attempt until the E501 budget is used up, triggering consultRoute
	// with deviation.CurrentStage = "2".
	for i := 0; i < engine.E501AttemptLimit; i++ {
		f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("simulated harness failure on Stage-2/agent-a")})
	}
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
		RunID:               testRunID,
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

	// The consultation itself writes no row: no infrastructure step may appear.
	for _, step := range store.Applied {
		if step.IsInfrastructure && !step.HITLRejected {
			t.Errorf("want no consultation row in store.Applied, got infrastructure step %q", step.AgentInstance)
		}
	}
}

// TestSession_MixedRoutingPaths_SeqStrictlyMonotonic verifies that a run mixing
// auto-routed dispatches, a harness error, consultant-routed dispatches and
// HITL-rejected rows records a strictly increasing Seq column, in which every
// recorded invocation takes the next slot after global_sequence.
//
// Sequence:
//
//	(a) Stage-1/agent-a auto-dispatched and succeeds (Seq=1).
//	(b) Stage-1/agent-b auto-dispatch: the harness returns an error each time,
//	    recorded as rows (Seq=2..4) until the engine's E501 budget is used up,
//	    and the consultant is asked. The consultation consumes no slot. It
//	    re-dispatches agent-b with HITLOverride=true: first attempt SUCCESS +
//	    ApprovalFalse is a HITL-rejected row (Seq=5), the redispatch is
//	    accepted (Seq=6).
//	(c) Stage-2/agent-a and Stage-2/agent-b are auto-dispatched afterwards and
//	    take Seq 7 and 8, with no collision with the accepted step from (b).
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
	consultant.stageLastDispatch(1)
	// After consultRoute returns, the outer auto loop dispatches Stage-2 rows.
	// No further consultant instructions needed -- the run completes on its own
	// via auto-routing.

	dir := scopedTempDir(t)
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
	// (b) Stage-1/agent-b: every harness error is recorded (Seq=2..4); once the
	// E501 budget is used up the last one triggers consultRoute.
	for i := 0; i < engine.E501AttemptLimit; i++ {
		f.Queue("agent-b", harness.ScriptedEntry{Err: errors.New("simulated Stage-1/agent-b harness error")})
	}
	// Inside consultRoute: first agent-b dispatch (HITLOverride=true).
	// ApprovalFalse -> HITLRedispatch (rlRejStep applied at Seq=5).
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "Stage-1/agent-b first attempt",
	}})
	// HITL redispatch: ApprovalTrue -> HITLAccept (workflowStep applied at Seq=6).
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "Stage-1/agent-b accepted after redispatch",
	}})
	// (c) Stage-2/agent-a: auto-dispatched after consultRoute returns.
	// Its Seq must be global_sequence+1 = 7, not a repeat of the accepted step's 6.
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

	// Primary assertion: the consultation leaves no row, and every recorded
	// invocation takes the next slot after global_sequence (Seq 1..8).
	requireNoConsultationRows(t, store)
	if len(store.Applied) != 8 {
		t.Fatalf("want 8 applied steps (agent-a, three agent-b errors, rejected attempt, accepted "+
			"attempt, Stage-2/agent-a, Stage-2/agent-b), got %d: %+v", len(store.Applied), store.Applied)
	}
	for i, step := range store.Applied {
		if step.Seq != i+1 {
			t.Errorf("store.Applied[%d] (%s) has Seq=%d, want %d: every recorded invocation must take the "+
				"next slot, with no gap from a consultation and no collision after consultRoute returns",
				i, step.AgentInstance, step.Seq, i+1)
		}
	}
	if store.state.GlobalSequence != len(store.Applied) {
		t.Errorf("want global_sequence %d (one per recorded invocation), got %d",
			len(store.Applied), store.state.GlobalSequence)
	}
}

// requireInstanceIDsMatchRecordedSeq fails unless every recorded row is named
// "{agent}#{Seq}" and the rows' Seq values run 1..n without a gap or repeat.
func requireInstanceIDsMatchRecordedSeq(t *testing.T, store *memStore) {
	t.Helper()
	for i, step := range store.Applied {
		if step.Seq != i+1 {
			t.Errorf("applied row %d (%s): want Seq %d, got %d", i, step.AgentInstance, i+1, step.Seq)
		}
		if !strings.HasSuffix(step.AgentInstance, fmt.Sprintf("#%d", step.Seq)) {
			t.Errorf("applied row %d: want instance id to end with #%d, got %q", i, step.Seq, step.AgentInstance)
		}
	}
}

// TestSession_ConsultRoute_HITLRedispatch_SeqHasNoConsultationGap verifies that
// consultant-routed dispatches with a HITL rejection and redispatch record
// consecutive Seq values from 1, and that the dispatch after the redispatch
// takes the next slot.
//
// Expected rows: agent-a#1 (rejected), agent-a#2 (accepted), agent-b#3.
func TestSession_ConsultRoute_HITLRedispatch_SeqHasNoConsultationGap(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "step 1", 0)
	consultant.queueDispatch("agent-b", "step 2", 1)
	consultant.queueStop("done")

	ses, f, store, orchPath := newHITLLinearSession(t, consultant,
		&switchingApprovalReader{firstApproval: domain.ApprovalFalse, restApproval: domain.ApprovalTrue})
	for _, id := range []string{"agent-a", "agent-a", "agent-b"} {
		f.Queue(id, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: id + "#0",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "done",
		}})
	}

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	requireNoConsultationRows(t, store)
	if len(store.Applied) != 3 {
		t.Fatalf("want 3 applied rows (rejected, accepted, agent-b), got %d: %+v", len(store.Applied), store.Applied)
	}
	requireInstanceIDsMatchRecordedSeq(t, store)
	var dispatched []string
	for _, inv := range f.Invocations() {
		dispatched = append(dispatched, inv.Request.AgentInstanceID)
	}
	if !reflect.DeepEqual(dispatched, []string{"agent-a#1", "agent-a#2", "agent-b#3"}) {
		t.Errorf("want dispatched instance ids agent-a#1, agent-a#2, agent-b#3, got %v", dispatched)
	}
}

// TestSession_ConsultRoute_HarnessErrorThenRedispatch_SeqConsecutive verifies
// that a harness error recorded as a row, followed by a consultation that
// re-dispatches the same agent, gives the retry the next slot: the consultation
// between the two consumes none.
func TestSession_ConsultRoute_HarnessErrorThenRedispatch_SeqConsecutive(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "try the work", 0)
	consultant.queueDispatch("agent-a", "retry after harness error", 0)
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)
	f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("harness: subprocess timed out")})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done on retry",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	requireNoConsultationRows(t, store)
	if len(store.Applied) != 2 {
		t.Fatalf("want 2 applied rows (harness error, retry), got %d: %+v", len(store.Applied), store.Applied)
	}
	requireInstanceIDsMatchRecordedSeq(t, store)
	if got := store.Applied[0]; got.Status != domain.StatusBLOCKED {
		t.Errorf("want the harness-error row recorded as BLOCKED, got %q", got.Status)
	}
}

// TestSession_ConsultRoute_ResumeContinuesFromGlobalSequence verifies that a
// resumed orchestrated run takes its next Seq from the artifact's
// global_sequence: the consultation that chooses the next dispatch takes no
// slot, so the dispatch is recorded at global_sequence+1.
func TestSession_ConsultRoute_ResumeContinuesFromGlobalSequence(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-b", "step 2", 1)
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  1,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseOrchestratedConfig(orchPath)
	markResume(&cfg)
	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) != 1 || invs[0].Request.AgentInstanceID != "agent-b#2" {
		t.Fatalf("want a single dispatch agent-b#2 after resuming at global_sequence 1, got %+v", invs)
	}
	if len(store.Applied) != 1 || store.Applied[0].Seq != 2 {
		t.Errorf("want the dispatch recorded at Seq 2, got %+v", store.Applied)
	}
	if store.state.GlobalSequence != 2 {
		t.Errorf("want global_sequence 2, got %d", store.state.GlobalSequence)
	}
}
