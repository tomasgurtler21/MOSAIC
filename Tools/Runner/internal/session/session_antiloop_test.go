package session_test

// Tests for the anti-loop guard and ConsultStep phase field: the guard prevents
// any single agent from being consecutively dispatched for the same step more
// than four times, escalating to the routing consultant on the fifth would-be
// dispatch. The counter resets on a different agent or a new workflow step.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// TestSession_AntiLoopGuard_OrchestratedMode_EscalatesAfterFourConsecutiveDispatches
// verifies that in orchestrated-mode HITL loops, when the routing consultant
// repeatedly dispatches the same agent for the same step, the anti-loop guard
// fires after four total dispatches and escalates instead of performing a
// fifth dispatch.
//
// Without the guard the test setup produces six agent-a invocations: each of
// the three consultant dispatch instructions triggers one initial dispatch plus
// one automatic HITL redispatch before escalating back to the consultant. With
// the guard in place, only four dispatches occur (the first two pairs of
// initial + HITL-redispatch), and the third consultant instruction sees the
// guard fire so it receives an escalation deviation and terminates the run
// with a stop instruction.
func TestSession_AntiLoopGuard_OrchestratedMode_EscalatesAfterFourConsecutiveDispatches(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Three dispatch instructions produce six agent-a invocations without the
	// guard (each pair: initial + HITL auto-redispatch). The guard must block
	// the fifth invocation so that only four occur before the stop.
	consultant.queueDispatch("agent-a", "attempt", 0)
	consultant.queueDispatch("agent-a", "attempt", 0)
	consultant.queueDispatch("agent-a", "attempt", 0)
	// After the guard fires on the fifth would-be dispatch, the consultant is
	// called with an escalation deviation; it terminates the run here.
	consultant.queueStop("run terminated by anti-loop guard")

	// ApprovalFalse keeps HITL non-compliant on every check, driving the full
	// auto-redispatch -> escalation cycle.
	ses, f, _, orchPath := newHITLLinearSession(t, consultant, &fixedApprovalReader{domain.ApprovalFalse})

	// Queue more responses than the guard allows so the test is not bounded by
	// the harness queue rather than by the guard logic.
	for i := 0; i < 8; i++ {
		f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: fmt.Sprintf("agent-a#%d", i+1),
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "done",
		}})
	}

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	totalA := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			totalA++
		}
	}
	if totalA > 4 {
		t.Errorf("anti-loop guard (orchestrated): want at most 4 consecutive agent-a dispatches for the same step, got %d; "+
			"the guard must prevent the 5th dispatch and escalate to the consultant instead",
			totalA)
	}
	if totalA < 4 {
		t.Errorf("anti-loop guard (orchestrated): must allow at least four dispatches before escalating, got %d; "+
			"the guard is firing too early",
			totalA)
	}
}

// TestSession_AntiLoopGuard_AutoMode_EscalatesAfterFourConsecutiveDispatches
// verifies that in auto-mode HITL loops (hitlCheckLoop), when the routing
// consultant repeatedly dispatches the same agent after HITL escalation, the
// anti-loop guard fires after four total dispatches.
//
// In auto mode the engine dispatches agent-a directly (dispatch 1). The
// HITL check triggers one automatic redispatch (dispatch 2), then escalates
// to the consultant. Without the guard the consultant's two dispatch
// instructions each produce two more invocations (dispatches 3-4 and 5-6).
// With the guard the fifth invocation is blocked: after dispatches 1-4 the
// counter is at four and the second consultant call receives an escalation
// deviation, causing it to stop the run.
func TestSession_AntiLoopGuard_AutoMode_EscalatesAfterFourConsecutiveDispatches(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Two dispatch instructions produce four additional agent-a invocations
	// without the guard (on top of the two from the engine-driven hitlCheckLoop
	// path), totalling six. With the guard, only two additional invocations are
	// allowed before the guard fires on the second consultant call.
	consultant.queueDispatch("agent-a", "attempt", 0)
	consultant.queueDispatch("agent-a", "attempt", 0)
	// After the guard fires, the consultant receives an escalation and stops.
	consultant.queueStop("run terminated by anti-loop guard")

	ses, f, _, orchPath := newHITLLinearSession(t, consultant, &fixedApprovalReader{domain.ApprovalFalse})

	for i := 0; i < 8; i++ {
		f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: fmt.Sprintf("agent-a#%d", i+1),
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "done",
		}})
	}

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeAuto,
		},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	totalA := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			totalA++
		}
	}
	if totalA > 4 {
		t.Errorf("anti-loop guard (auto): want at most 4 consecutive agent-a dispatches for the same step, got %d; "+
			"the guard must fire after the 4th dispatch and escalate rather than performing a 5th",
			totalA)
	}
	if totalA < 4 {
		t.Errorf("anti-loop guard (auto): must allow at least four dispatches before escalating, got %d; "+
			"the guard is firing too early",
			totalA)
	}
}

// TestSession_AntiLoopGuard_PartiallyDoneAppliesGuard verifies that the
// anti-loop guard has no status-based exceptions: an agent that repeatedly
// returns PARTIALLY_DONE is subject to the same four-dispatch cap as an agent
// that fails HITL compliance checks.
//
// In orchestrated mode with HITL disabled, a PARTIALLY_DONE result causes the
// outer dispatch loop to call the consultant again for the next routing
// decision. Without the guard, the consultant's five dispatch instructions
// each produce one agent-a invocation (no HITL redispatch occurs for
// non-SUCCESS status), totalling five. With the guard, the fifth would-be
// dispatch is blocked after four successful invocations and the consultant
// receives an escalation that terminates the run.
func TestSession_AntiLoopGuard_PartiallyDoneAppliesGuard(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Five dispatch instructions each produce one agent-a invocation (no HITL
	// redispatch for PARTIALLY_DONE). Without the guard: five dispatches total.
	// With the guard: four dispatches, then the fifth is blocked.
	for i := 0; i < 5; i++ {
		consultant.queueDispatch("agent-a", "attempt", 0)
	}
	// After the guard fires on the fifth would-be dispatch, the consultant
	// receives an escalation deviation and terminates the run.
	consultant.queueStop("run terminated by anti-loop guard")

	// Use linear-orch.md (HITL=false) so no HITL redispatch occurs; each
	// consultant dispatch results in exactly one agent-a invocation.
	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	for i := 0; i < 6; i++ {
		f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: fmt.Sprintf("agent-a#%d", i+1),
			StatusCode:      domain.StatusPARTIALLY_DONE,
			StatusMessage:   "partially done",
		}})
	}

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	totalA := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			totalA++
		}
	}
	if totalA > 4 {
		t.Errorf("anti-loop guard (PARTIALLY_DONE): want at most 4 consecutive agent-a dispatches for the same step, got %d; "+
			"PARTIALLY_DONE must not bypass the guard -- no status-based exceptions",
			totalA)
	}
	if totalA < 4 {
		t.Errorf("anti-loop guard (PARTIALLY_DONE): must allow at least four dispatches before escalating, got %d; "+
			"the guard is firing too early",
			totalA)
	}
}

// TestSession_ConsultDispatch_HITLStep_LeavesNoConsultationRow verifies that a
// consultant-routed dispatch of a HITL step records only the workflow step, in
// the phase declared by the workflow, and no consultation row.
func TestSession_ConsultDispatch_HITLStep_LeavesNoConsultationRow(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do the work", 0)
	consultant.queueStop("done")

	// ApprovalTrue so HITL passes without cycling; the test focuses on what is
	// recorded, not on HITL redispatch behavior.
	ses, f, store, orchPath := newHITLLinearSession(t, consultant, &fixedApprovalReader{domain.ApprovalTrue})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	requireNoConsultationRows(t, store)
	if len(store.Applied) != 1 {
		t.Fatalf("want exactly one applied row (the workflow step), got %d: %+v", len(store.Applied), store.Applied)
	}
	// The hitl-linear fixture declares the dispatched row in phase "PLANNING".
	if got := store.Applied[0]; got.Phase != "PLANNING" || got.Seq != 1 {
		t.Errorf("want workflow step in phase PLANNING at Seq 1, got phase %q Seq %d", got.Phase, got.Seq)
	}
}

// TestSession_AntiLoopGuard_CounterResets_OnDifferentAgentDispatch verifies
// that the anti-loop guard counter resets when a different agent is dispatched.
// Three agent-a dispatches build the counter to three (one below the cap).
// A single agent-b dispatch resets the counter so subsequent agent-a dispatches
// start fresh and are not blocked prematurely.
func TestSession_AntiLoopGuard_CounterResets_OnDifferentAgentDispatch(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Three agent-a dispatches build the consecutive count to three (one below
	// the max-before-guard-fires threshold of four). The agent-b dispatch must
	// reset the counter so the two subsequent agent-a dispatches start fresh.
	consultant.queueDispatch("agent-a", "attempt", 0)
	consultant.queueDispatch("agent-a", "attempt", 0)
	consultant.queueDispatch("agent-a", "attempt", 0)
	// Switching to agent-b (the agent of row 1) must reset the agent-a counter.
	consultant.queueDispatch("agent-b", "interlude", 1)
	// Two more agent-a dispatches after the reset; neither should be blocked.
	consultant.queueDispatch("agent-a", "post-reset attempt 1", 0)
	consultant.queueDispatch("agent-a", "post-reset attempt 2", 0)
	consultant.queueStop("done")

	// No HITL on linear-orch.md so each consultant instruction produces exactly
	// one agent dispatch; PARTIALLY_DONE drives the loop back to the consultant.
	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	for i := 0; i < 6; i++ {
		f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: fmt.Sprintf("agent-a#%d", i+1),
			StatusCode:      domain.StatusPARTIALLY_DONE,
			StatusMessage:   "still working",
		}})
	}
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#1",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "interlude",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	totalA := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			totalA++
		}
	}
	// After the agent-b break, agent-a's counter must have reset. The guard must
	// allow the two post-reset agent-a dispatches (totalA should reach five:
	// three before agent-b plus two after). A totalA of four or fewer means the
	// guard fired prematurely after the agent-b break.
	if totalA < 5 {
		t.Errorf("anti-loop guard (different-agent reset): want at least 5 agent-a dispatches "+
			"(3 before agent-b + 2 after counter reset), got %d; "+
			"the guard must reset the consecutive-dispatch counter when a different agent is dispatched",
			totalA)
	}
}

// TestSession_AntiLoopGuard_CounterResets_OnNewStep verifies that the anti-loop
// guard counter resets when a new workflow step begins. Agent-a is dispatched
// three times (PARTIALLY_DONE) then once (SUCCESS) for step 0, reaching the
// guard's cap of four without triggering it. When the workflow advances to
// step 1, agent-a must be dispatchable again from a fresh counter; a premature
// guard fire on step 1 would indicate the counter was not scoped to the step.
//
// The test uses a custom two-row workflow where agent-a appears in both rows.
// The consultant dispatches agent-a three times at row 0 (all PARTIALLY_DONE),
// then instructs a SUCCESS response at row 0 to advance to step 1, then
// dispatches agent-a at row 1. An implementation that scopes the counter
// globally (not per-step) would fire the guard on the first agent-a dispatch at
// step 1 because the accumulated count from step 0 carries over.
func TestSession_AntiLoopGuard_CounterResets_OnNewStep(t *testing.T) {
	dir := t.TempDir()

	// Two-row workflow with agent-a in both rows (HITL=false so PARTIALLY_DONE
	// drives consultant re-routing without triggering HITL redispatch).
	const twoAgentAWorkflow = `<Workflow type="core" name="two-agent-a" version="1.0">
## Two-Step Agent-A Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-a | - | - | plan.md |
| PLANNING | agent-a | FALSE | COMPLETE | - | plan.md | result.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "two-agent-a-orch.md")
	if err := os.WriteFile(orchPath, []byte(twoAgentAWorkflow), 0600); err != nil {
		t.Fatalf("write two-agent-a-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")

	consultant := &scriptedRoutingConsultant{}
	// Step 0: dispatch agent-a three times (PARTIALLY_DONE each) then once
	// (SUCCESS) to reach the guard cap of four without triggering it.
	for i := 0; i < 3; i++ {
		consultant.queueDispatch("agent-a", "step-0 attempt", 0)
	}
	// Advance to step 1 by dispatching agent-a at row 0 with a SUCCESS response.
	consultant.queueDispatch("agent-a", "step-0 final", 0)
	// Step 1: agent-a must be dispatchable from a fresh counter; the guard must
	// not fire on this first dispatch at the new step.
	consultant.queueDispatch("agent-a", "step-1 attempt", 1)
	consultant.queueStop("workflow done")

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// Three PARTIALLY_DONE responses for the step-0 loop, one SUCCESS to advance,
	// one SUCCESS for step 1.
	for i := 0; i < 3; i++ {
		f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: fmt.Sprintf("agent-a#%d", i+1),
			StatusCode:      domain.StatusPARTIALLY_DONE,
			StatusMessage:   "still working",
		}})
	}
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "step 0 done",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "step 1 done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "two-agent-a",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeOrchestrated,
		},
	}
	ses.Start(context.Background(), cfg) //nolint:errcheck

	totalA := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			totalA++
		}
	}
	// Step 0 contributes four agent-a dispatches (three PARTIALLY_DONE + one
	// SUCCESS), reaching the guard cap without triggering it. Step 1 contributes
	// one more. The guard must not fire on the step-1 dispatch. A totalA of four
	// or fewer indicates the guard fired prematurely on step 1 because the counter
	// was not reset when the step changed.
	if totalA < 5 {
		t.Errorf("anti-loop guard (new-step reset): want 5 agent-a dispatches "+
			"(4 at step 0 + 1 at step 1 after counter reset), got %d; "+
			"the counter must reset when a new workflow step begins",
			totalA)
	}
}
