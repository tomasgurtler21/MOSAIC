package session_test

// Tests for review-class post-trigger routing consultation (Stage 2).
//
// These tests verify the behavioral contract specified in ContractsDesign.md
// Stage 2 -- Observable Behavioral Contract:
//
//   (a) A successful review-class agent triggers exactly one routing
//       consultation per evaluateTriggers pass, with last_status_message
//       set to the last successful review agent's StatusMessage.
//   (b) A consultant stop instruction terminates the run with
//       RunStoppedByConsultant; the outcome message has a "consultant stop:"
//       prefix and StopReason is non-empty.
//   (c) A consultant dispatch instruction dispatches the named agent, writes
//       an infrastructure log row, and continues the run. Call site 2
//       (consultRoute dispatch-completion tail) fires after the dispatched
//       agent completes.
//   (d) Non-review infrastructure classes (checkpoint) do NOT trigger a
//       follow-up consultation even when Routing is wired.
//   (e) A review agent failure (non-SUCCESS) does NOT trigger consultation;
//       the existing on_failure policy applies.
//   (f) When Routing is nil the consultation is silently skipped and the run
//       proceeds as if no consultation mechanism is present.
//   (g) A consultant error terminates the run with RunStoppedByConsultant and
//       a "consultation failed:" message prefix.
//   (h) Multiple successful review agents in one evaluateTriggers pass produce
//       exactly one consultation using the last successful review's message.
//       (covered together with (a))
//   (i) The dispatch-then-trigger chain is bounded by antiLoopState: repeated
//       consultation dispatch of the same agent eventually triggers the
//       anti-loop guard which produces a deviation consultation, bounding the
//       chain.
//   (j) Consultation ordering: the nested evaluateTriggers call inside
//       consultRoute's dispatch-completion tail (call site 2) fires after
//       prevWorkflowStep is updated, so the second consultation carries the
//       message from the second review pass, not the first.
//   (k) Consultation dispatch increments seq; INVOCATION_INTERVAL trigger
//       evaluation at call site 2 accounts for the updated sequence number.
//
// scriptedRoutingConsultant is shared with the rest of the session test
// package (defined in session_test.go). Fields:
//   consultant.CallCount    -- number of ConsultRouting calls made
//   consultant.Requests     -- the ConsultationRequest for each call
//   consultant.queueStop    -- enqueue a stop instruction
//   consultant.queueDispatch -- enqueue a dispatch instruction
//   consultant.queueError   -- enqueue a consultant error
//
// RED tests (a, b, c, g, h, i, j, k): these tests fail without the Stage-2
// implementation. The queues are set up so that without review consultation:
//   - The run completes normally (RunCompleted) because all normal-path
//     responses are queued (no deviation path fires), OR
//   - The run ends differently than what is asserted.
// With the Stage-2 implementation:
//   - The review consultation stops or redirects the run before the normal
//     path can complete.
// Guard tests (d, e, f): assert no consultation; pass in both RED and GREEN
// as regression protection.

import (
	"context"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ---- session helper for review-consultation tests ----

// newReviewConsultSession builds a session backed by review-class-orch.md
// with the given routing consultant wired into session.Deps.Routing.
// It writes agent definition files for all agents referenced by the fixture.
//
// review-class-orch.md declares two review-class infrastructure agents
// (review-agent-a and review-agent-b), both with INVOCATION_INTERVAL:1
// and on_failure=continue. The linear workflow has agent-a -> agent-b.
func newReviewConsultSession(
	t *testing.T,
	routing domain.RoutingConsultant,
) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "review-class-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "review-agent-a")
	writeAgentFile(t, dir, "review-agent-b")
	f = harness.NewMockAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Routing:  routing,
	})
	return
}

// ---- TDD-RED tests: consultation DOES occur ----

// TestSession_ReviewConsult_Stop_RunEndsWithStoppedByConsultant verifies
// scenarios (a), (b), and (h).
//
//   (a) After both review-class agents fire successfully, the routing
//       consultant receives exactly one call with LastStatusMessage matching
//       the LAST successful review agent's StatusMessage.
//   (b) A consultant stop instruction terminates the run with
//       RunStoppedByConsultant; the outcome message has a "consultant stop:"
//       prefix and StopReason is non-empty.
//   (h) Multiple successful reviews in one pass produce exactly one
//       consultation using the last successful review's message.
//
// Queue strategy: all normal-run responses are queued so that without the
// Stage-2 implementation, the run completes (RunCompleted) without ever
// calling the routing consultant. With the implementation, the consultant is
// called after the first review pass and stops the run before agent-b.
//
// RED failure: run completes with RunCompleted; consultant is never called
// because evaluateTriggers returns no review signal.
func TestSession_ReviewConsult_Stop_RunEndsWithStoppedByConsultant(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStop("review observed, stopping run")

	ses, f, _, orchPath := newReviewConsultSession(t, consultant)

	// agent-a (seq=1): the triggering workflow step.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "workflow step done",
	}})
	// review-agent-a fires first (seq=2).
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a complete",
	}})
	// review-agent-b fires second (seq=3) -- its message is the "last" review message.
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b complete",
	}})
	// agent-b and pass2 reviews: consumed only in RED (when no consultation stops the run).
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "agent-b done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a pass2",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b pass2",
	}})

	cfg := baseLinearConfig(orchPath)
	got, err := ses.Start(context.Background(), cfg)

	// (b) Consultant stop must terminate with RunStoppedByConsultant.
	// In RED, the run completes normally (RunCompleted) -- this assertion fails.
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	// (b) Outcome message must carry "consultant stop:" prefix.
	if !strings.HasPrefix(got.Message, "consultant stop:") {
		t.Errorf("want outcome message prefix %q, got %q", "consultant stop:", got.Message)
	}
	// (b) StopReason must be non-empty.
	if got.StopReason == "" {
		t.Error("want non-empty StopReason on RunStoppedByConsultant outcome, got empty string")
	}

	// (a)+(h) Exactly one consultation must be issued for the full trigger pass
	// (not one per review agent).
	if consultant.CallCount != 1 {
		t.Fatalf("want exactly 1 consultation call (one per trigger pass), got %d",
			consultant.CallCount)
	}

	// (a) The consultation must carry the LAST successful review's message.
	// review-agent-b fired after review-agent-a; "review-b complete" is the last.
	call := consultant.Requests[0]
	if call.LastStatusMessage == nil {
		t.Fatal("want LastStatusMessage set to the last review agent's StatusMessage, got nil")
	}
	const wantMsg = "review-b complete"
	if *call.LastStatusMessage != wantMsg {
		t.Errorf("want LastStatusMessage %q (last review's StatusMessage), got %q",
			wantMsg, *call.LastStatusMessage)
	}
}

// TestSession_ReviewConsult_Dispatch_AgentRunsAndCallSite2Fires verifies
// scenario (c): a consultant dispatch instruction dispatches the named agent
// and the run continues. This test also exercises call site 2 (the
// consultRoute dispatch-completion tail): after the consultation-dispatched
// agent completes, reviews fire again and a second consultation stops the run.
//
// Queue strategy: both agent-a and agent-b responses are queued. In RED, the
// run follows the normal path (no consultation) and completes. In GREEN, the
// first review consultation dispatches agent-a (consuming the second agent-a
// response), reviews fire at call site 2, and the second consultation stops.
//
// RED failure: run completes with RunCompleted.
func TestSession_ReviewConsult_Dispatch_AgentRunsAndCallSite2Fires(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Call site 1: consultation dispatches agent-a (row index 0).
	consultant.queueDispatch("agent-a", "re-examine agent-a output", 0)
	// Call site 2: second consultation stops the run after second review pass.
	consultant.queueStop("second review observed, stopping")

	ses, f, store, orchPath := newReviewConsultSession(t, consultant)

	// agent-a responses: one for the engine dispatch, one for the consultation dispatch.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "engine dispatch done",
	}})
	// review-agent-a and review-agent-b: pass 1 (after engine's agent-a).
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a pass1",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b pass1",
	}})
	// Second agent-a response: consumed by the consultation dispatch in GREEN;
	// consumed by the normal engine path as a deviation-rerun or never in RED
	// (engine dispatches agent-b next in RED).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "consultation dispatch done",
	}})
	// review-agent-a and review-agent-b: pass 2. In GREEN these fire at call site 2
	// after the consultation-dispatched agent-a. In RED they fire after agent-b.
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a pass2",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#7",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b pass2",
	}})
	// agent-b: consumed only in RED (engine dispatches agent-b after agent-a pass1).
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#8",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "agent-b done",
	}})
	// Pass3 reviews: consumed only in RED (after agent-b, INVOCATION_INTERVAL fires).
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#9",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a pass3",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#10",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b pass3",
	}})

	cfg := baseLinearConfig(orchPath)
	got, err := ses.Start(context.Background(), cfg)

	// In RED: run completes normally (RunCompleted). This assertion fails.
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	// Two consultations: call site 1 (after engine's agent-a pass) and call site 2
	// (after consultation-dispatched agent-a pass).
	if consultant.CallCount != 2 {
		t.Fatalf("want 2 consultation calls (call site 1 + call site 2), got %d",
			consultant.CallCount)
	}

	// The first consultation carries the review-b pass1 message, proving it was
	// triggered by the review pass (not by a deviation with agent-a's message).
	call1 := consultant.Requests[0]
	if call1.LastStatusMessage == nil {
		t.Fatal("want first consultation LastStatusMessage set, got nil")
	}
	if *call1.LastStatusMessage != "review-b pass1" {
		t.Errorf("want first consultation LastStatusMessage %q, got %q",
			"review-b pass1", *call1.LastStatusMessage)
	}

	// agent-a must have been dispatched twice: once by the engine, once by the
	// consultation.
	agentACount := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACount++
		}
	}
	if agentACount != 2 {
		t.Errorf("want agent-a dispatched 2 times (engine + consultation), got %d", agentACount)
	}

	// The consultation dispatch leaves no Execution Log row: no
	// infrastructure row and no row attributed to the orchestrator.
	requireNoConsultationRows(t, store)
	// Every recorded invocation (workflow and infrastructure) takes the next
	// sequence slot: the consultation dispatch collides with nothing.
	for i, step := range store.Applied {
		if step.Seq != i+1 {
			t.Errorf("want applied row %d (%s) at Seq %d, got %d", i, step.AgentInstance, i+1, step.Seq)
		}
	}

	// The second consultation (call site 2) carries the last review's message
	// from the second pass. This confirms call site 2 fired with the correct
	// prevWorkflowStep context and that the reviews re-fired after the
	// consultation-dispatched agent completed.
	call2 := consultant.Requests[1]
	if call2.LastStatusMessage == nil {
		t.Fatal("want second consultation LastStatusMessage set, got nil")
	}
	if *call2.LastStatusMessage != "review-b pass2" {
		t.Errorf("want second consultation LastStatusMessage %q, got %q",
			"review-b pass2", *call2.LastStatusMessage)
	}
}

// TestSession_ReviewConsult_ConsultantError_TerminatesWithFailedPrefix verifies
// scenario (g): a consultant error terminates the run with RunStoppedByConsultant
// and a "consultation failed:" message prefix.
//
// Queue strategy: full normal-run responses queued. In RED, run completes. In
// GREEN, the review consultation triggers the consultant error -> stop.
//
// RED failure: run completes with RunCompleted.
func TestSession_ReviewConsult_ConsultantError_TerminatesWithFailedPrefix(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Queue a transport-level error for the review consultation call.
	consultant.queueError(domain.ConsultFailTransport)

	ses, f, _, orchPath := newReviewConsultSession(t, consultant)

	// Full normal-run queue: no deviation fires in RED.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a complete",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b complete",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-a pass2",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review-b pass2",
	}})

	cfg := baseLinearConfig(orchPath)
	got, err := ses.Start(context.Background(), cfg)

	// In RED: run completes (RunCompleted). This assertion fails.
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	if !strings.HasPrefix(got.Message, "consultation failed:") {
		t.Errorf("want outcome message prefix %q, got %q", "consultation failed:", got.Message)
	}
}
