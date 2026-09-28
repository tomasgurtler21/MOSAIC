package session_test

// Tests for review-class routing consultation anti-loop and call-site-2
// sequencing (split from session_review_consult_test.go).
//
// Covers scenarios (i), (j), (k):
//   (i) The dispatch-then-trigger chain is bounded by antiLoopState.
//   (j) Call site 2 inside consultRoute's dispatch-completion tail fires after
//       prevWorkflowStep is updated, carrying the second review pass message.
//   (k) Consultation dispatch increments seq so INVOCATION_INTERVAL fires again.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// TestSession_ReviewConsult_AntiLoopBoundsDispatchChain verifies scenario (i):
// when the review consultation repeatedly dispatches the same agent,
// antiLoopState eventually blocks the dispatch and triggers a guard deviation
// consultation, bounding the dispatch-then-trigger chain.
//
// Anti-loop accounting (maxConsecutiveSameAgentDispatches = 4):
//   engine: agent-a -> antiLoop.count = 1
//   consult-1: dispatch agent-a -> count = 2 (OK)
//   consult-2: dispatch agent-a -> count = 3 (OK)
//   consult-3: dispatch agent-a -> count = 4 >= max -> BLOCKED -> guard deviation
//   consult-4 (guard deviation): stop -> run ends
//
// Queue strategy: all responses for 2 consultation dispatches of agent-a plus
// agent-b and its reviews for the RED path.
//
// RED failure: run completes with RunCompleted (no consultation dispatches).
func TestSession_ReviewConsult_AntiLoopBoundsDispatchChain(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Consultations 1 and 2: dispatch agent-a (row 0). These succeed.
	consultant.queueDispatch("agent-a", "re-examine", 0)
	consultant.queueDispatch("agent-a", "re-examine again", 0)
	// Consultation 3 would dispatch agent-a but antiLoopState blocks it (count=4).
	// The guard deviation calls consultRoute again; return stop here.
	consultant.queueDispatch("agent-a", "third re-examine", 0)
	// Consultation 4 (guard deviation): stop terminates the run.
	consultant.queueStop("anti-loop guard triggered, stopping")

	ses, f, _, orchPath := newReviewConsultSession(t, consultant)

	// engine: agent-a (seq=1)
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "engine dispatch",
	}})
	// pass 1 reviews
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a p1",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b p1",
	}})
	// consult-1 dispatches agent-a (seq=5 after consultation record at seq=4)
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "consult dispatch 1",
	}})
	// pass 2 reviews (after consult-1 dispatched agent-a)
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a p2",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#7",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b p2",
	}})
	// consult-2 dispatches agent-a (seq=9 after consultation record at seq=8)
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#9",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "consult dispatch 2",
	}})
	// pass 3 reviews (after consult-2 dispatched agent-a)
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#10",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a p3",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#11",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b p3",
	}})
	// consult-3 attempt: anti-loop blocks dispatch -> guard deviation consultation.
	// No agent-a queue entry needed for the blocked attempt.
	//
	// agent-b and its reviews: consumed only in RED (normal completion path).
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#12",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "agent-b done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#13",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a normal",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#14",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b normal",
	}})

	cfg := baseLinearConfig(orchPath)
	got, err := ses.Start(context.Background(), cfg)

	// In RED: run completes (RunCompleted). This assertion fails.
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	// At least 4 consultation calls must occur:
	// 3 review consultations + 1 guard deviation consultation.
	if consultant.CallCount < 4 {
		t.Errorf("want at least 4 consultation calls (3 dispatch + 1 guard stop), got %d",
			consultant.CallCount)
	}

	// agent-a must be dispatched at least 3 times:
	// once by engine, twice by review consultations.
	agentACount := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACount++
		}
	}
	if agentACount < 3 {
		t.Errorf("want at least 3 agent-a dispatches (engine + 2 consultations), got %d",
			agentACount)
	}
}

// TestSession_ReviewConsult_CallSite2_SecondPassMessageAndSeqCounting verifies
// scenarios (j) and (k):
//
//   (j) Consultation ordering: the second consultation (at call site 2 inside
//       consultRoute's dispatch-completion tail) carries the message from the
//       SECOND review pass, not the first. This proves that prevWorkflowStep
//       is correctly set before the consultation, enabling the nested
//       evaluateTriggers to fire with the right position context.
//   (k) INVOCATION_INTERVAL counting: the consultation dispatch (consultation
//       record + dispatched agent) increments seq, so the INVOCATION_INTERVAL
//       trigger check at call site 2 correctly fires the reviews again.
//
// Queue strategy: all responses for one consultation pass plus agent-b and its
// reviews for the RED path.
//
// RED failure: run completes with RunCompleted (CallCount stays 0).
func TestSession_ReviewConsult_CallSite2_SecondPassMessageAndSeqCounting(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Call site 1: dispatch agent-a (row 0).
	consultant.queueDispatch("agent-a", "first dispatch", 0)
	// Call site 2: stop after the second review pass.
	consultant.queueStop("second pass reviewed")

	ses, f, _, orchPath := newReviewConsultSession(t, consultant)

	// engine: agent-a (seq=1)
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "first run",
	}})
	// pass 1 reviews (after engine's agent-a)
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a first-pass",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b first-pass",
	}})
	// consult-1 dispatches agent-a (consultation record seq=4, agent-a seq=5)
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "second run",
	}})
	// pass 2 reviews (call site 2): fire because INVOCATION_INTERVAL:1 fires on
	// the incremented seq after the consultation dispatch (k).
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a second-pass",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#7",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b second-pass",
	}})
	// agent-b and its reviews: consumed only in RED.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#8",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "agent-b done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#9",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a normal",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#10",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b normal",
	}})

	cfg := baseLinearConfig(orchPath)
	got, err := ses.Start(context.Background(), cfg)

	// In RED: run completes (RunCompleted). This assertion fails.
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	// Two consultations: call site 1 and call site 2.
	if consultant.CallCount != 2 {
		t.Fatalf("want 2 consultation calls (call site 1 + call site 2), got %d",
			consultant.CallCount)
	}

	// (j) The first consultation carries the FIRST-pass last review's message.
	call1 := consultant.Requests[0]
	if call1.LastStatusMessage == nil {
		t.Fatal("want first consultation LastStatusMessage set, got nil")
	}
	const wantFirstMsg = "review-b first-pass"
	if *call1.LastStatusMessage != wantFirstMsg {
		t.Errorf("want first consultation LastStatusMessage %q, got %q",
			wantFirstMsg, *call1.LastStatusMessage)
	}

	// (j) The second consultation carries the SECOND-pass last review's message,
	// proving call site 2 fired with the correct prevWorkflowStep context and
	// that seq incremented correctly from the consultation dispatch (k).
	call2 := consultant.Requests[1]
	if call2.LastStatusMessage == nil {
		t.Fatal("want second consultation LastStatusMessage set, got nil")
	}
	const wantSecondMsg = "review-b second-pass"
	if *call2.LastStatusMessage != wantSecondMsg {
		t.Errorf("want second consultation LastStatusMessage %q, got %q",
			wantSecondMsg, *call2.LastStatusMessage)
	}
}

// TestSession_ReviewConsult_PrevWorkflowStep_PhaseEndTrigger verifies
// scenario (j): PHASE_END trigger fires correctly across a consultation-dispatched
// step re-entry.
//
// A PHASE_END trigger on a review-class agent fires when the completed step is the
// last row of its phase in the routing table (prospective semantics). The nested
// evaluateTriggers call inside consultRoute's dispatch-completion tail (call site 2)
// must fire again after the consultation re-dispatches a step whose row is still the
// last row of its phase.
//
// Observable: if PHASE_END fires correctly both times, two consultations occur. If
// PHASE_END fails to fire at call site 2 (e.g., implementation omits the look-ahead
// for re-dispatched steps), only one consultation occurs and CallCount == 1 fails.
//
// Fixture: review-consult-phase-end-orch.md
//   Row 0: PLANNING / agent-a -> agent-b (OnSuccess)
//   Row 1: REVIEW   / agent-b -> COMPLETE (OnSuccess)
//   InfraAgent: review-agent-a, PHASE_END, continue
//
// Note: REVIEW is used rather than EXECUTION because EXECUTION triggers
// staged-workflow handling in the engine which requires a Plan.md file.
// REVIEW is a plain non-staged phase, the same as PLANNING.
//
// Under prospective semantics row 0 (PLANNING/agent-a) is the last row of the
// PLANNING phase, so PHASE_END fires when agent-a completes. The workflow never
// advances to agent-b (row 1) because consultation intercepts after the first
// PHASE_END and re-dispatches row 0.
//
// GREEN path trace:
//   1. PLANNING/agent-a (seq=1): row 0 is the last PLANNING row -> PHASE_END fires.
//      review-agent-a dispatched (seq=2).
//      evaluateTriggers returns review signal "first-phase-end-review".
//   2. Consultation (call site 1): dispatches agent-a (row 0, PLANNING).
//   3. PLANNING/agent-a (seq=4): row 0 is still the last PLANNING row -> PHASE_END fires.
//      review-agent-a dispatched (seq=5).
//      evaluateTriggers returns review signal "second-phase-end-review".
//   4. Consultation (call site 2): stops the run.
//
// RED failure: no consultation at all (implementation absent); run completes
// normally with RunCompleted and CallCount == 0.
func TestSession_ReviewConsult_PrevWorkflowStep_PhaseEndTrigger(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Call site 1: re-dispatch agent-a (row 0, PLANNING phase) after first PHASE_END.
	consultant.queueDispatch("agent-a", "re-examine after phase end", 0)
	// Call site 2: stop after the second PHASE_END review fires.
	consultant.queueStop("second phase-end review seen, stopping")

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "review-consult-phase-end-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b") // declared in workflow; never dispatched in this test
	writeAgentFile(t, dir, "review-agent-a")
	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Routing:  consultant,
	})

	// PLANNING/agent-a (seq=1): row 0 is the last PLANNING row.
	// PHASE_END fires after this step completes.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning step done",
	}})
	// review-agent-a (seq=2): fired by PHASE_END after agent-a completes.
	// Its StatusMessage becomes the call site 1 consultation's LastStatusMessage.
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "first-phase-end-review",
	}})
	// PLANNING/agent-a (seq=4, after consultation record at seq=3):
	// dispatched by the call site 1 consultation (re-entry at row 0).
	// Row 0 is still the last PLANNING row, so PHASE_END fires again.
	// Consumed only in GREEN; never reached in RED.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "consultation-dispatched planning step",
	}})
	// review-agent-a (seq=5): fired at call site 2 by PHASE_END after the
	// consultation-dispatched agent-a completes (row 0 is still last PLANNING row).
	// This is the observable proof that PHASE_END fires again on re-entry.
	// Consumed only in GREEN; never reached in RED.
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "second-phase-end-review",
	}})

	cfg := baseLinearConfig(orchPath)
	got, err := ses.Start(context.Background(), cfg)

	// In RED: run completes normally (RunCompleted); this assertion fails.
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	// Two consultations must occur: call site 1 (after first PHASE_END) and
	// call site 2 (after second PHASE_END on consultation re-dispatch).
	// If PHASE_END does not fire at call site 2, CallCount stays at 1.
	if consultant.CallCount != 2 {
		t.Fatalf("want 2 consultation calls (call site 1 + call site 2 via PHASE_END), got %d",
			consultant.CallCount)
	}

	// The first consultation must carry the first-pass review message.
	call1 := consultant.Requests[0]
	if call1.LastStatusMessage == nil {
		t.Fatal("want first consultation LastStatusMessage set, got nil")
	}
	const wantFirstMsg = "first-phase-end-review"
	if *call1.LastStatusMessage != wantFirstMsg {
		t.Errorf("want first consultation LastStatusMessage %q, got %q",
			wantFirstMsg, *call1.LastStatusMessage)
	}

	// The second consultation must carry the second-pass review message,
	// proving PHASE_END fired again at call site 2 on the re-dispatched step.
	call2 := consultant.Requests[1]
	if call2.LastStatusMessage == nil {
		t.Fatal("want second consultation LastStatusMessage set, got nil")
	}
	const wantSecondMsg = "second-phase-end-review"
	if *call2.LastStatusMessage != wantSecondMsg {
		t.Errorf("want second consultation LastStatusMessage %q, got %q",
			wantSecondMsg, *call2.LastStatusMessage)
	}
}
