package session_test

// Tests for stop-request boundaries at infrastructure trigger, consultRoute,
// and HITL-redispatch sites (split from session_stoprequest_test.go).
//
// Coverage:
//   - The evaluateTriggers per-iteration checkpoint honours the between-agents
//     stop-request boundary for declared infrastructure agents.
//   - The consultRoute-reachable invocation site honours the between-steps
//     stop-request boundary, applied between the consultation and the
//     consultant-routed agent's dispatch.
//   - The top-level (primary auto-routed dispatch) HITL-redispatch invocation
//     site honours the same between-steps stop-request boundary.
//   - The consultRoute internal HITL redispatch site also honours the
//     between-steps stop-request boundary (orchestrated mode).

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// TestSession_EvaluateTriggers_StopRequest_BetweenAgents_StopsBeforeSecondDispatch
// covers the evaluateTriggers per-iteration stop checkpoint (session.go:2052):
// review-class-orch.md declares two review-class infrastructure agents
// (review-agent-a, review-agent-b) that both fire after agent-a's step. A stop
// request observed after review-agent-a's Store.Apply -- but before
// review-agent-b's dispatch -- must leave review-agent-a's outcome recorded
// and never dispatch review-agent-b, stopping the run before that call.
func TestSession_EvaluateTriggers_StopRequest_BetweenAgents_StopsBeforeSecondDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "review-class-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "review-agent-a")
	writeAgentFile(t, dir, "review-agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		// Routing: nil -- no post-review consultation expected. This test verifies
		// the graceful-stop checkpoint between infra agent dispatches, independent
		// of the Stage-2 review consultation mechanism.
		// True only once agent-a's step and review-agent-a's trigger dispatch
		// have both been applied -- modelling a stop confirmed strictly
		// between two declared infra agents' dispatches within the same
		// evaluateTriggers pass.
		StopRequested: func() bool { return len(store.Applied) >= 2 },
	})

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

	cfg := baseLinearConfig(orchPath)
	cfg.InfraClassSelections = nil

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped before review-agent-b's dispatch, got %q (message: %q)", got.Status, got.Message)
	}
	if len(store.Applied) != 2 {
		t.Fatalf("want exactly 2 Apply calls (agent-a, review-agent-a; no partial/spurious Apply for review-agent-b), got %d: %+v", len(store.Applied), store.Applied)
	}
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "review-agent-b" {
			t.Errorf("want review-agent-b never dispatched once a stop was requested between infra agents, but it was invoked")
		}
		if inv.Agent.Identifier == "agent-b" {
			t.Errorf("want agent-b never dispatched once a stop was requested during review-class trigger evaluation, but it was invoked")
		}
	}
}

// TestSession_ConsultRoute_StopRequest_BetweenConsultationAndDispatch_StopsBeforeInvocation
// covers the invokeAndLog call site reachable from consultRoute (orchestrated
// mode's routing-consultation dispatch): a stop request observed after the
// consultation has returned -- but before the consultant-routed agent is
// dispatched -- must stop the run before that invocation. The consultation
// itself is not recorded in the artifact, so nothing at all is applied.
func TestSession_ConsultRoute_StopRequest_BetweenConsultationAndDispatch_StopsBeforeInvocation(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do the work", 0)

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		// True only once the consultation has been made -- not before --
		// modelling a stop confirmed strictly between the consultation and the
		// routed agent's dispatch.
		StopRequested: func() bool { return consultant.CallCount >= 1 },
	})

	// No scripted entry for agent-a: if the session incorrectly dispatched it
	// despite the stop request, MockAdapter would return a "no scripted
	// response queued" error instead of the expected RunStopped outcome.

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped before the consultant-routed dispatch, got %q (message: %q)", got.Status, got.Message)
	}
	if len(store.Applied) != 0 || store.state.GlobalSequence != 0 {
		t.Errorf("want nothing recorded for the consultation, got %d applied, global_sequence %d",
			len(store.Applied), store.state.GlobalSequence)
	}
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			t.Errorf("want agent-a never dispatched once a stop was requested after the consultation, but it was invoked")
		}
	}
}

// TestSession_HITL_StopRequest_BetweenSteps_StopsBeforeRedispatchInvocation
// covers the HITL-redispatch invocation site (session.go's HITL compliance
// loop) with the same between-steps stop-request boundary verified above for
// the primary dispatch site: a stop request observed after the rejected
// attempt has been persisted (HITLRejected=true), but before the same agent
// is redispatched, must stop the run before that redispatch call.
func TestSession_HITL_StopRequest_BetweenSteps_StopsBeforeRedispatchInvocation(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do the work", 0)

	// ApprovalFalse on every read makes agent-a's first result non-compliant,
	// triggering the HITL-redispatch path.
	ses, f, store, orchPath := newHITLLinearSessionWithStopRequest(t, consultant, &fixedApprovalReader{domain.ApprovalFalse})

	// Only one scripted entry for agent-a: if the session incorrectly
	// redispatched it, MockAdapter would return a "no scripted response
	// queued" error instead of the expected RunStopped outcome.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped before the HITL redispatch call, got %q (message: %q)", got.Status, got.Message)
	}
	// The rejected attempt must still be recorded (HITLRejected=true); no
	// second (redispatch) attempt should ever be applied.
	rejectedApplies := 0
	for _, applied := range store.Applied {
		if applied.AgentInstance == "agent-a#1" {
			rejectedApplies++
			if !applied.HITLRejected {
				t.Errorf("want agent-a#1's Apply to have HITLRejected=true, got %+v", applied)
			}
		}
		if applied.AgentInstance == "agent-a#2" {
			t.Errorf("want no redispatch Apply after a stop request, but agent-a#2 was applied")
		}
	}
	if rejectedApplies != 1 {
		t.Fatalf("want exactly 1 Apply call for agent-a's rejected attempt, got %d (all applies: %+v)", rejectedApplies, store.Applied)
	}
	agentACalls := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACalls++
		}
	}
	if agentACalls != 1 {
		t.Errorf("want agent-a invoked exactly once (no HITL redispatch after a stop request), got %d", agentACalls)
	}
}

// TestSession_AutoHITL_StopRequest_BetweenSteps_StopsBeforeRedispatchInvocation
// covers the top-level HITL-redispatch invocation site inside the primary
// auto-routed dispatch loop (session.go's hitlCheckLoop) -- a distinct
// invokeAndLog call site from consultRoute's own internal HITL redispatch
// (which TestSession_HITL_StopRequest_BetweenSteps_StopsBeforeRedispatchInvocation
// exercises: ExecutionModeOrchestrated never produces an engine Dispatch
// decision, so every dispatch and HITL check in that test runs through
// consultRoute, not this top-level loop). This test uses ExecutionModeAuto
// (no RoutingConsultant needed, since the anti-loop guard never trips on a
// single rejection) so the primary hitlCheckLoop's own redispatch call is the
// one under test: a stop request observed after the rejected attempt has been
// persisted (HITLRejected=true), but before the same agent is redispatched,
// must stop the run before that redispatch call.
func TestSession_AutoHITL_StopRequest_BetweenSteps_StopsBeforeRedispatchInvocation(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "hitl-linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}

	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Approvals: &fixedApprovalReader{domain.ApprovalFalse},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		// True only once the rejected HITL attempt has been recorded --
		// modelling a stop confirmed strictly between the rejected-attempt
		// Apply and the top-level redispatch invocation.
		StopRequested: func() bool {
			for _, applied := range store.Applied {
				if applied.HITLRejected {
					return true
				}
			}
			return false
		},
	})

	// Only one scripted entry for agent-a: if the session incorrectly
	// redispatched it, MockAdapter would return a "no scripted response
	// queued" error instead of the expected RunStopped outcome.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped before the top-level HITL redispatch call, got %q (message: %q)", got.Status, got.Message)
	}
	// The rejected attempt must still be recorded (HITLRejected=true); no
	// second (redispatch) attempt should ever be applied.
	rejectedApplies := 0
	for _, applied := range store.Applied {
		if applied.AgentInstance == "agent-a#1" {
			rejectedApplies++
			if !applied.HITLRejected {
				t.Errorf("want agent-a#1's Apply to have HITLRejected=true, got %+v", applied)
			}
		}
		if applied.AgentInstance == "agent-a#2" {
			t.Errorf("want no redispatch Apply after a stop request, but agent-a#2 was applied")
		}
	}
	if rejectedApplies != 1 {
		t.Fatalf("want exactly 1 Apply call for agent-a's rejected attempt, got %d (all applies: %+v)", rejectedApplies, store.Applied)
	}
	agentACalls := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACalls++
		}
	}
	if agentACalls != 1 {
		t.Errorf("want agent-a invoked exactly once (no top-level HITL redispatch after a stop request), got %d", agentACalls)
	}
}

// newHITLLinearSessionWithStopRequest mirrors newHITLLinearSession, additionally
// wiring a StopRequested that reports true once the rejected HITL attempt has
// been recorded (len(store.Applied) >= 1), modelling a stop confirmed strictly
// between the rejected-attempt Apply and the redispatch invocation.
func newHITLLinearSessionWithStopRequest(t *testing.T, consultant domain.RoutingConsultant, approvals domain.ApprovalReader) (
	ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string,
) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "hitl-linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewMockAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   consultant,
		Approvals: approvals,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		// True only once the rejected HITL attempt has been recorded -- not
		// on the routing consultation's own (unrelated) infrastructure Apply
		// that precedes it -- modelling a stop confirmed strictly between the
		// rejected-attempt Apply and the redispatch invocation.
		StopRequested: func() bool {
			for _, applied := range store.Applied {
				if applied.HITLRejected {
					return true
				}
			}
			return false
		},
	})
	return
}
