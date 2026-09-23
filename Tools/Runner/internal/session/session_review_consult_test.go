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
	"os"
	"path/filepath"
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
) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "review-class-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "review-agent-a")
	writeAgentFile(t, dir, "review-agent-b")
	f = harness.NewFakeAdapter()
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

	// Build the session directly so we control the exact FakeAdapter entries
	// (instead of using the helper, which would use a different adapter).
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "review-class-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "review-agent-a")
	writeAgentFile(t, dir, "review-agent-b")
	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Routing:  consultant,
	})
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

	// AC2.3: verify the consultation dispatch writes an infrastructure log row.
	// In the GREEN path, consultRoute records the consultation at seq=4 as an
	// infrastructure-flagged CompletedStep before dispatching the named agent.
	// The AgentInstance is "{orchStem}#{consultSeq}" -- here "review-class-orch#4".
	// An implementation that dispatches the agent but omits Store.Apply would
	// leave this entry absent from store.Applied.
	var consultRecord *domain.CompletedStep
	for i := range store.Applied {
		step := &store.Applied[i]
		if step.IsInfrastructure && strings.HasPrefix(step.AgentInstance, "orchestrator#") {
			consultRecord = step
			break
		}
	}
	if consultRecord == nil {
		t.Error("want consultation dispatch to write an infrastructure log row (AC2.3), " +
			"but no IsInfrastructure=true entry with AgentInstance prefix 'orchestrator#' found in store.Applied")
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
// GREEN, the review consultation triggers the consultant error → stop.
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
//   1. PLANNING/agent-a (seq=1): row 0 is the last PLANNING row → PHASE_END fires.
//      review-agent-a dispatched (seq=2).
//      evaluateTriggers returns review signal "first-phase-end-review".
//   2. Consultation (call site 1): dispatches agent-a (row 0, PLANNING).
//   3. PLANNING/agent-a (seq=4): row 0 is still the last PLANNING row → PHASE_END fires.
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
	f := harness.NewFakeAdapter()
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

// TestSession_ReviewConsult_ExecutionPhaseEnd_FiresOnlyAfterLastStage verifies
// the FR-3 prospective semantics for EXECUTION PHASE_END: the look-ahead spans
// the entire EXECUTION phase across all stages, so PHASE_END fires only after
// the last step of the *last* stage -- not after the last step of each
// individual stage.
//
// Fixture: phase-end-execution-staged-orch.md
//   Stage-1: agent-a -> agent-b
//   Stage-2: agent-a -> agent-b
//   InfraAgent: review-agent-a, PHASE_END, continue
//
// Under prospective EXECUTION PHASE_END semantics (GREEN):
//   - agent-b completes Stage-1 (row 1): NOT the last EXECUTION step -- no PHASE_END
//   - agent-b completes Stage-2 (row 3): last step of last stage = last EXECUTION step
//     PHASE_END fires; review-agent-a is dispatched; consultant stops the run.
//   Total dispatches: 5 (agent-a/S1, agent-b/S1, agent-a/S2, agent-b/S2, review-agent-a)
//
// Under retrospective semantics (RED -- current code):
//   - PHASE_END fires when phase changes from EXECUTION.Stage-1 to EXECUTION.Stage-2,
//     i.e., after agent-b/Stage-1 completes. review-agent-a is dispatched too early.
//   - The consultant stops the run after Stage-1, before Stage-2 is dispatched.
//   Total dispatches: 3 (agent-a/S1, agent-b/S1, review-agent-a)
//
// RED failure: total invocations == 3, not 5; review-agent-a is at index 2, not 4;
//   Stage-2 agents (invs[2]=agent-a, invs[3]=agent-b) are never dispatched.
func TestSession_ReviewConsult_ExecutionPhaseEnd_FiresOnlyAfterLastStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Consultant stops the run when called. In GREEN this is called after Stage-2's
	// last step. In RED this is called (incorrectly) after Stage-1's last step.
	consultant.queueStop("execution phase-end review seen, stopping")

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "phase-end-execution-staged-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "review-agent-a")

	const planContent = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First stage | - | FALSE |
| 2 | Stage Two | Second stage | 1 | FALSE |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(planContent), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Routing:  consultant,
	})

	// Stage-1, step 1: not the last EXECUTION step -- PHASE_END must not fire.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 1 agent-a done",
	}})
	// Stage-1, step 2 (last Stage-1 step): NOT the last EXECUTION step.
	// PHASE_END must NOT fire here; the look-ahead still sees Stage-2 steps ahead.
	// In RED: PHASE_END fires here (phase changes to EXECUTION.Stage-2 next).
	// In GREEN: no PHASE_END; Stage-2 proceeds.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 1 agent-b done",
	}})
	// Stage-2, step 1: not the last EXECUTION step -- PHASE_END must not fire.
	// Only consumed in GREEN.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 2 agent-a done",
	}})
	// Stage-2, step 2: last step of last stage = last EXECUTION step.
	// PHASE_END fires after this completes (GREEN). Only consumed in GREEN.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 2 agent-b done",
	}})
	// review-agent-a dispatched by PHASE_END after Stage-2's last step.
	// Only consumed in GREEN.
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "execution-phase-end-review",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir, // Plan.md was written into dir
	}

	got, err := ses.Start(context.Background(), cfg)

	// In RED: run stops after Stage-1 (consultant called too early).
	// This assertion still passes in RED (consultant does stop the run) -- the
	// failing assertions below distinguish RED from GREEN.
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	invs := f.Invocations()

	// GREEN: 5 dispatches (a/S1, b/S1, a/S2, b/S2, review-agent-a).
	// RED:   3 dispatches (a/S1, b/S1, review-agent-a) -- PHASE_END fired too early.
	if len(invs) != 5 {
		t.Fatalf("want 5 invocations (agent-a/S1, agent-b/S1, agent-a/S2, agent-b/S2, review-agent-a), got %d: EXECUTION PHASE_END must fire only after the last stage, not after Stage-1", len(invs))
	}

	// Stage-2 must have been dispatched before review-agent-a.
	// In RED, Stage-2 is never reached; invs[2] would be review-agent-a.
	if invs[2].Agent.Identifier != "agent-a" {
		t.Errorf("want invocation[2] = agent-a (Stage-2 first step), got %q: PHASE_END must not fire before Stage-2 is dispatched", invs[2].Agent.Identifier)
	}
	if invs[3].Agent.Identifier != "agent-b" {
		t.Errorf("want invocation[3] = agent-b (Stage-2 last step), got %q: PHASE_END must not fire before Stage-2 last step", invs[3].Agent.Identifier)
	}

	// review-agent-a must be the fifth dispatch (index 4), after both stages.
	// In RED it is the third dispatch (index 2), after only Stage-1.
	if invs[4].Agent.Identifier != "review-agent-a" {
		t.Errorf("want invocation[4] = review-agent-a (EXECUTION PHASE_END fires after last stage), got %q", invs[4].Agent.Identifier)
	}

	// Consultant must be called exactly once (after Stage-2's last step in GREEN).
	if consultant.CallCount != 1 {
		t.Fatalf("want 1 consultation call (after EXECUTION PHASE_END at end of Stage-2), got %d", consultant.CallCount)
	}
}

// ---- Guard-condition tests: consultation does NOT occur ----

// TestSession_ReviewConsult_NonReviewClass_NoConsultation verifies scenario (d):
// non-review infrastructure classes (checkpoint) do NOT trigger a routing
// consultation even when Routing is wired.
//
// Uses review-consult-checkpoint-orch.md: checkpoint-class agent with
// INVOCATION_INTERVAL:1 and on_failure=continue -- no review-class agents.
//
// Guard-condition test: passes in both RED and GREEN.
func TestSession_ReviewConsult_NonReviewClass_NoConsultation(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// No instructions queued. Any unexpected ConsultRouting call exhausts the
	// queue and returns a transport error, making the unwanted call visible as
	// RunStoppedByConsultant rather than a silent no-op.

	dir := t.TempDir()
	// review-consult-checkpoint-orch.md: checkpoint-class agent (INVOCATION_INTERVAL:1,
	// continue) -- no review-class agents declared.
	orchPath := copyOrchestratorFile(t, dir, "review-consult-checkpoint-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")
	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Routing:  consultant,
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	if consultant.CallCount != 0 {
		t.Errorf("want no consultation calls for checkpoint-class infrastructure agents, got %d",
			consultant.CallCount)
	}
}

// TestSession_ReviewConsult_ReviewFailure_NoConsultation verifies scenario (e):
// a review agent failure (non-SUCCESS StatusCode) does NOT trigger a routing
// consultation; the existing on_failure=continue policy applies and the run
// proceeds normally.
//
// Guard-condition test: passes in both RED and GREEN.
func TestSession_ReviewConsult_ReviewFailure_NoConsultation(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// No instructions queued. Unexpected calls surface as errors.

	ses, f, _, orchPath := newReviewConsultSession(t, consultant)

	// Both review agents return non-SUCCESS (BLOCKED). on_failure=continue, so
	// the run proceeds and completes normally.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#2",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "review-a blocked",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#3",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "review-b blocked",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#5",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "review-a blocked again",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#6",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "review-b blocked again",
	}})

	cfg := baseLinearConfig(orchPath)
	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	if consultant.CallCount != 0 {
		t.Errorf("want no consultation calls when review agents return non-SUCCESS, got %d",
			consultant.CallCount)
	}
}

// TestSession_ReviewConsult_NilRouting_NoConsultation verifies scenario (f):
// when session.Deps.Routing is nil, the consultation is silently skipped and
// the run proceeds as if no review consultation mechanism exists.
//
// Call-site coverage: this test exercises call site 1 only (no consultation
// dispatch occurs, so the consultRoute dispatch-completion tail -- call site 2
// -- is never reached). The nil-Routing guard at call site 2 is structurally
// identical to call site 1: both check `s.deps.Routing != nil` using the same
// shared session field before invoking consultRoute. A single test is therefore
// sufficient to validate the guard for both sites.
//
// Guard-condition test: passes in both RED and GREEN.
func TestSession_ReviewConsult_NilRouting_NoConsultation(t *testing.T) {
	// newReviewClassSession creates a session with Routing: nil.
	// No post-review consultation expected; run proceeds normally.
	ses, f, _, orchPath := newReviewClassSession(t)

	// Full linear run with reviews: 2 workflow steps x 2 review agents = 4 reviews.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})

	cfg := baseLinearConfig(orchPath)
	got, err := ses.Start(context.Background(), cfg)

	// Run completes normally: Routing=nil means no consultation.
	requireRunStatus(t, got, err, domain.RunCompleted)
}
