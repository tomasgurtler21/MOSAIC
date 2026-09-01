package session_test

// Tests for the graceful-stop-request mechanism (Deps.StopRequested), which is
// orthogonal to ctx cancellation.
//
// Coverage:
//   - A stop request set while a step is in flight does not abort that step;
//     the step's outcome is still recorded via Store.Apply before the run
//     returns RunStopped.
//   - A stop request set between steps (after one step's Store.Apply, before
//     the next dispatch) stops the loop before that next invocation, with no
//     extra/partial Store.Apply.
//   - With no stop request set, the loop behaves exactly as today.
//   - Hard cancellation (ctx cancelled directly) still aborts immediately and
//     is unaffected by the presence of a (false-returning) StopRequested.
//   - The HITL-redispatch invocation site honours the same between-steps
//     stop-request boundary as the primary dispatch site.
//   - The evaluateTriggers per-iteration checkpoint honours the same
//     between-agents stop-request boundary for declared infrastructure agents.
//   - The consultRoute-reachable invocation site honours the same
//     between-steps stop-request boundary, applied between the consultation
//     record and the consultant-routed agent's dispatch.
//   - The top-level (primary auto-routed dispatch) HITL-redispatch invocation
//     site honours the same between-steps stop-request boundary as
//     consultRoute's own internal HITL redispatch: both are structurally
//     similar but distinct invokeAndLog call sites (session.go's primary
//     hitlCheckLoop vs. consultRoute's hitlLoop) and must each be checked.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// TestSession_Start_StopRequest_InFlightStepCompletesAndApplies verifies that
// a stop request observed while agent-a's invocation is in flight does not
// abort that invocation: agent-a's outcome is still recorded via Store.Apply,
// and only the *next* dispatch (agent-b) is skipped.
func TestSession_Start_StopRequest_InFlightStepCompletesAndApplies(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	stopRequested := false

	// The stop request is set as soon as agent-a's invocation returns --
	// i.e. while the step is still "in flight" from the dispatch loop's
	// perspective (before Store.Apply for it has happened).
	harnessCb := &callbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			if agentID == "agent-a" {
				stopRequested = true
			}
		},
	}

	ses := session.New(session.Deps{
		Harness:       harnessCb,
		Store:         store,
		Clock:         fixedClock{t: epoch},
		Interact:      &noopInteraction{},
		StopRequested: func() bool { return stopRequested },
	})

	// Only agent-a is queued: if the session incorrectly dispatched agent-b,
	// FakeAdapter would return a "no scripted response queued" error instead
	// of the expected RunStopped outcome.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped after in-flight stop request, got %q (message: %q)", got.Status, got.Message)
	}
	if len(store.Applied) != 1 || store.Applied[0].AgentInstance != "agent-a#1" {
		t.Fatalf("want agent-a's completed step applied before the run stopped, got %+v", store.Applied)
	}
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-b" {
			t.Errorf("want agent-b never dispatched once a stop was requested, but it was invoked")
		}
	}
}

// TestSession_Start_StopRequest_BetweenSteps_StopsBeforeNextDispatch verifies
// that a stop request observed after agent-a's Store.Apply -- but before
// agent-b's dispatch -- stops the run before that next invocation, with no
// extra Store.Apply beyond agent-a's.
func TestSession_Start_StopRequest_BetweenSteps_StopsBeforeNextDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		// True only once agent-a's step has already been applied -- this
		// models a stop confirmed strictly between dispatch cycles.
		StopRequested: func() bool { return len(store.Applied) >= 1 },
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped after between-steps stop request, got %q (message: %q)", got.Status, got.Message)
	}
	if len(store.Applied) != 1 {
		t.Fatalf("want exactly 1 Apply call (agent-a only, no partial/spurious Apply), got %d", len(store.Applied))
	}
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-b" {
			t.Errorf("want agent-b never dispatched once a stop was requested between steps, but it was invoked")
		}
	}
}

// TestSession_Start_NoStopRequest_LinearWorkflow_CompletesAsBefore is a
// regression test: with StopRequested explicitly false throughout the run,
// the dispatch loop behaves exactly as it did before the stop-request signal
// was introduced -- both agents dispatch and the run completes.
func TestSession_Start_NoStopRequest_LinearWorkflow_CompletesAsBefore(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}

	ses := session.New(session.Deps{
		Harness:       f,
		Store:         store,
		Clock:         fixedClock{t: epoch},
		Interact:      &noopInteraction{},
		StopRequested: func() bool { return false },
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunCompleted)
	if len(store.Applied) != 2 {
		t.Errorf("want 2 Apply calls (unaffected by an always-false StopRequested), got %d", len(store.Applied))
	}
}

// TestSession_Start_StopRequest_HardCancelStillAbortsImmediately verifies that
// direct ctx cancellation (ctrl+c) still produces its existing abandon-
// immediately behaviour, unaffected by an always-false StopRequested: the
// signal and ctx cancellation are independent paths.
func TestSession_Start_StopRequest_HardCancelStillAbortsImmediately(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel ctx directly after agent-a's invocation completes, mirroring the
	// existing hard-cancellation regression test -- but with an explicit,
	// always-false StopRequested to prove the two mechanisms are independent.
	harnessCb := &callbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			if agentID == "agent-a" {
				cancel()
			}
		},
	}

	ses := session.New(session.Deps{
		Harness:       harnessCb,
		Store:         store,
		Clock:         fixedClock{t: epoch},
		Interact:      &noopInteraction{},
		StopRequested: func() bool { return false },
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses.Start(ctx, baseLinearConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error for hard cancellation, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped from ctx cancellation, got %q", got.Status)
	}
	// agent-a completed and was applied before ctx was cancelled; agent-b's
	// invocation then observes ctx.Done() and aborts without ever consuming a
	// scripted entry or being applied -- unchanged from pre-existing behaviour.
	if len(store.Applied) != 1 || store.Applied[0].AgentInstance != "agent-a#1" {
		t.Fatalf("want only agent-a's step applied, got %+v", store.Applied)
	}
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-b" {
			t.Errorf("want agent-b's invocation aborted by ctx cancellation before consuming a scripted entry")
		}
	}
}

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

	f := harness.NewFakeAdapter()
	store := &memStore{}

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
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

// TestSession_ConsultRoute_StopRequest_BetweenConsultationRecordAndDispatch_StopsBeforeInvocation
// covers the invokeAndLog call site reachable from consultRoute (session.go's
// orchestrated-mode routing-consultation dispatch): a stop request observed
// after the consultation itself has been recorded as an infrastructure
// Execution Log row (Store.Apply for the consultation record) -- but before
// the consultant-routed agent is dispatched -- must stop the run before that
// invocation, with no Apply for the routed agent.
func TestSession_ConsultRoute_StopRequest_BetweenConsultationRecordAndDispatch_StopsBeforeInvocation(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do the work", 0)

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		// True only once the consultation's own infrastructure-flagged Apply
		// has happened -- not before -- modelling a stop confirmed strictly
		// between the consultation record and the routed agent's dispatch.
		StopRequested: func() bool { return len(store.Applied) >= 1 },
	})

	// No scripted entry for agent-a: if the session incorrectly dispatched it
	// despite the stop request, FakeAdapter would return a "no scripted
	// response queued" error instead of the expected RunStopped outcome.

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped before the consultant-routed dispatch, got %q (message: %q)", got.Status, got.Message)
	}
	if len(store.Applied) != 1 || !store.Applied[0].IsInfrastructure {
		t.Fatalf("want exactly 1 Apply call for the consultation record (IsInfrastructure=true), got %+v", store.Applied)
	}
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			t.Errorf("want agent-a never dispatched once a stop was requested after the consultation record, but it was invoked")
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
	// redispatched it, FakeAdapter would return a "no scripted response
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

	f := harness.NewFakeAdapter()
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
	// redispatched it, FakeAdapter would return a "no scripted response
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
	ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string,
) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "hitl-linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewFakeAdapter()
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
