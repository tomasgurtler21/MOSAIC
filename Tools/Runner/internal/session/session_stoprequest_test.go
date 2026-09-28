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
//
// (Infrastructure-trigger, consultRoute, and HITL stop-request tests are in
// session_stoprequest_infra_test.go.)

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

	f := harness.NewMockAdapter()
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
	// MockAdapter would return a "no scripted response queued" error instead
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

	f := harness.NewMockAdapter()
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

	f := harness.NewMockAdapter()
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

	f := harness.NewMockAdapter()
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
