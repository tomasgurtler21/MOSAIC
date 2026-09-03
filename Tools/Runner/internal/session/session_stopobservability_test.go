package session_test

// Tests for the session-side observability of the graceful-stop signal.
//
// The dispatch loop polls Deps.StopRequested at five checkpoints, each sitting
// immediately before a harness invocation. When a checkpoint observes the
// signal as set, it must leave a trace in the debug log that names which of the
// five dispatch paths saw it -- otherwise a stopped run is indistinguishable in
// the log from one that simply ended.
//
// Coverage:
//   - Each of the five checkpoints, exercised through the scenario that reaches
//     it, emits one session.stop.observed entry carrying its own distinguishing
//     checkpoint identifier.
//   - A run whose stop predicate is always false emits no stop-observed entry
//     at all: negative polls are silent, so a run in which no stop is ever
//     requested gains nothing in the log.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// stopObservedCheckpoints returns the value of the "checkpoint" field of every
// recorded session.stop.observed entry, in order. An entry without a
// "checkpoint" field contributes an empty string, so a missing field is visible
// rather than silently skipped.
func (r *sessionRecordingLogger) stopObservedCheckpoints() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.entries {
		if e.Event != domain.EventSessionStopObserved {
			continue
		}
		value := ""
		for _, f := range e.Fields {
			if f.Key == "checkpoint" {
				value = f.Value
				break
			}
		}
		out = append(out, value)
	}
	return out
}

// requireSingleStopObserved asserts that exactly one session.stop.observed
// entry was recorded and that it identifies the expected checkpoint. Asserting
// the exact set -- rather than mere presence -- is what makes the five
// checkpoint tests discriminating: an emitter that hardcodes one identifier
// everywhere passes a presence check in one test and fails the other four.
func requireSingleStopObserved(t *testing.T, logger *sessionRecordingLogger, want string) {
	t.Helper()
	got := logger.stopObservedCheckpoints()
	if len(got) != 1 {
		t.Fatalf("want exactly 1 %s entry, got %d (checkpoints: %v; all events: %v)",
			domain.EventSessionStopObserved, len(got), got, logger.allEvents())
	}
	if got[0] != want {
		t.Errorf("want %s entry to identify checkpoint %q, got %q",
			domain.EventSessionStopObserved, want, got[0])
	}
}

// TestSession_StopObserved_EngineStep_LogsCheckpointIdentifier drives the
// primary engine-dispatched checkpoint: a stop confirmed after agent-a's
// Store.Apply but before agent-b's dispatch. The checkpoint that skips that
// dispatch must say so in the log, naming itself.
func TestSession_StopObserved_EngineStep_LogsCheckpointIdentifier(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	logger := &sessionRecordingLogger{}

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    logger,
		// True only once agent-a's step has already been applied -- a stop
		// confirmed strictly between dispatch cycles.
		StopRequested: func() bool { return len(store.Applied) >= 1 },
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	// Act
	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	// Assert
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Fatalf("want RunStopped (the scenario must actually reach the checkpoint), got %q (message: %q)", got.Status, got.Message)
	}
	requireSingleStopObserved(t, logger, session.StopCheckpointEngineStep)
}

// TestSession_StopObserved_EngineHITLRedispatch_LogsCheckpointIdentifier drives
// the HITL-redispatch checkpoint inside the primary auto-routed dispatch loop:
// a stop confirmed after the rejected attempt is persisted but before the same
// agent is redispatched.
func TestSession_StopObserved_EngineHITLRedispatch_LogsCheckpointIdentifier(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "hitl-linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	logger := &sessionRecordingLogger{}

	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Approvals: &fixedApprovalReader{domain.ApprovalFalse},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		Debug:     logger,
		// True only once the rejected HITL attempt has been recorded.
		StopRequested: func() bool {
			for _, applied := range store.Applied {
				if applied.HITLRejected {
					return true
				}
			}
			return false
		},
	})

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

	// Act
	got, err := ses.Start(context.Background(), cfg)

	// Assert
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Fatalf("want RunStopped (the scenario must actually reach the checkpoint), got %q (message: %q)", got.Status, got.Message)
	}
	requireSingleStopObserved(t, logger, session.StopCheckpointEngineHITLRedispatch)
}

// TestSession_StopObserved_ConsultDispatch_LogsCheckpointIdentifier drives the
// consultant-routed dispatch checkpoint: a stop confirmed after the
// consultation record is persisted but before the routed agent is dispatched.
func TestSession_StopObserved_ConsultDispatch_LogsCheckpointIdentifier(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	logger := &sessionRecordingLogger{}
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do the work", 0)

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    logger,
		// True only once the consultation's own infrastructure-flagged Apply
		// has happened -- strictly between the consultation record and the
		// routed agent's dispatch.
		StopRequested: func() bool { return len(store.Applied) >= 1 },
	})

	// No scripted entry for agent-a: a dispatch despite the stop would surface
	// as a FakeAdapter "no scripted response queued" error, not RunStopped.

	// Act
	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	// Assert
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Fatalf("want RunStopped (the scenario must actually reach the checkpoint), got %q (message: %q)", got.Status, got.Message)
	}
	requireSingleStopObserved(t, logger, session.StopCheckpointConsultDispatch)
}

// TestSession_StopObserved_ConsultHITLRedispatch_LogsCheckpointIdentifier
// drives consultRoute's own HITL-redispatch checkpoint, a distinct call site
// from the primary loop's: in orchestrated mode every dispatch and HITL check
// runs through consultRoute.
func TestSession_StopObserved_ConsultHITLRedispatch_LogsCheckpointIdentifier(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "hitl-linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	logger := &sessionRecordingLogger{}
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do the work", 0)

	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   consultant,
		Approvals: &fixedApprovalReader{domain.ApprovalFalse},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		Debug:     logger,
		// True only once the rejected HITL attempt has been recorded -- not on
		// the routing consultation's own (unrelated) infrastructure Apply that
		// precedes it, which would land on the consult-dispatch checkpoint
		// instead.
		StopRequested: func() bool {
			for _, applied := range store.Applied {
				if applied.HITLRejected {
					return true
				}
			}
			return false
		},
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	// Act
	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	// Assert
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Fatalf("want RunStopped (the scenario must actually reach the checkpoint), got %q (message: %q)", got.Status, got.Message)
	}
	requireSingleStopObserved(t, logger, session.StopCheckpointConsultHITLRedispatch)
}

// TestSession_StopObserved_InfraDispatch_LogsCheckpointIdentifier drives the
// per-agent checkpoint inside trigger evaluation: review-class-orch.md declares
// two review-class infrastructure agents, and the stop is confirmed between the
// first one's Store.Apply and the second one's dispatch.
func TestSession_StopObserved_InfraDispatch_LogsCheckpointIdentifier(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "review-class-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	logger := &sessionRecordingLogger{}

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    logger,
		// True only once agent-a's step and review-agent-a's trigger dispatch
		// have both been applied -- strictly between two declared infra agents'
		// dispatches within the same trigger-evaluation pass.
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

	// Act
	got, err := ses.Start(context.Background(), cfg)

	// Assert
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Fatalf("want RunStopped (the scenario must actually reach the checkpoint), got %q (message: %q)", got.Status, got.Message)
	}
	requireSingleStopObserved(t, logger, session.StopCheckpointInfraDispatch)
}

// TestSession_StopObserved_AlwaysFalsePredicate_LogsNothing pins the silence
// half of the contract: every checkpoint in a completed two-agent run polls the
// signal and sees it unset, and none of those negative polls may produce an
// entry. A run in which no stop is ever requested must gain nothing in the log.
func TestSession_StopObserved_AlwaysFalsePredicate_LogsNothing(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	logger := &sessionRecordingLogger{}

	ses := session.New(session.Deps{
		Harness:       f,
		Store:         store,
		Clock:         fixedClock{t: epoch},
		Interact:      &noopInteraction{},
		Debug:         logger,
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

	// Act
	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	// Assert
	requireRunStatus(t, got, err, domain.RunCompleted)
	if entries := logger.stopObservedCheckpoints(); len(entries) != 0 {
		t.Errorf("want no %s entries when the stop predicate is always false, got %d: %v",
			domain.EventSessionStopObserved, len(entries), entries)
	}
}
