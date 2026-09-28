package session_test

// Tests for debug-logging events emitted by the session dispatch loop.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ---- sessionLogEntry / sessionRecordingLogger ----

// sessionLogEntry is one captured call to sessionRecordingLogger.Log.
type sessionLogEntry struct {
	Event   string
	Message string
	Fields  []domain.DebugField
}

// sessionRecordingLogger is a thread-safe domain.DebugLogger that records every
// Log call. Session logging tests inject this to assert which events were emitted.
type sessionRecordingLogger struct {
	mu      sync.Mutex
	entries []sessionLogEntry
}

// Log implements domain.DebugLogger.
func (r *sessionRecordingLogger) Log(event string, message string, fields ...domain.DebugField) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, sessionLogEntry{
		Event:   event,
		Message: message,
		Fields:  append([]domain.DebugField{}, fields...),
	})
}

// eventLogged reports whether at least one entry with the given event name
// was recorded.
func (r *sessionRecordingLogger) eventLogged(event string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event == event {
			return true
		}
	}
	return false
}

// fieldValue returns the value for the given field key in the first entry
// with the given event name. Returns ("", false) when not found.
func (r *sessionRecordingLogger) fieldValue(event, key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event != event {
			continue
		}
		for _, f := range e.Fields {
			if f.Key == key {
				return f.Value, true
			}
		}
	}
	return "", false
}

// allEvents returns the event names of all recorded entries in order.
func (r *sessionRecordingLogger) allEvents() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, len(r.entries))
	for i, e := range r.entries {
		names[i] = e.Event
	}
	return names
}

// ---- newLinearSessionWithDebug helper ----

// newLinearSessionWithDebug builds a session backed by the linear-orch.md
// fixture and wires the supplied debug logger into Deps.Debug. The returned
// MockAdapter and memStore are available for test configuration.
func newLinearSessionWithDebug(t *testing.T, debug domain.DebugLogger) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewMockAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    debug,
	})
	return
}

// ---- newTestDeviationWorkflowSession helper ----

// newTestDeviationWorkflowSession is a helper that constructs a session with the
// deviation-trigger workflow (agent-a has no On Findings column, so any
// non-SUCCESS status triggers a deviation) and wires the supplied logger.
// Agent files are created in a temp dir. The orchestrator file path is returned
// so the caller can build a RunConfig. No Routing consultant is wired, so
// deviations terminate with RunDeviationUnresolved.
func newTestDeviationWorkflowSession(
	t *testing.T,
	logger domain.DebugLogger,
) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	const deviationWorkflow = `<Workflow type="core" name="deviate-log" version="1.0">
## Deviation Log Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | plan.md | result.md |
</Workflow>
`
	orchPath = filepath.Join(dir, "deviate-log-orch.md")
	if err := os.WriteFile(orchPath, []byte(deviationWorkflow), 0600); err != nil {
		t.Fatalf("newTestDeviationWorkflowSession: write %q: %v", orchPath, err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewMockAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    logger,
	})
	return
}

// ---- session logs dispatch start and step completion ----

// TestSession_Start_WithLogger_LogsDispatchStart verifies that the session
// emits EventSessionDispatchStart before each harness invocation, carrying
// the agent instance ID, phase, stage and row index as structured fields.
func TestSession_Start_WithLogger_LogsDispatchStart(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, _, orchPath := newLinearSessionWithDebug(t, logger)

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

	ses.Start(context.Background(), baseLinearConfig(orchPath)) //nolint:errcheck

	if !logger.eventLogged(domain.EventSessionDispatchStart) {
		t.Errorf("want %s logged before each harness invocation, got events: %v",
			domain.EventSessionDispatchStart, logger.allEvents())
	}
	// The agent instance ID of the first dispatch should appear in a field.
	agentVal, ok := logger.fieldValue(domain.EventSessionDispatchStart, "agent")
	if !ok {
		t.Errorf("want 'agent' field on %s entry", domain.EventSessionDispatchStart)
	} else if agentVal != "agent-a#1" {
		t.Errorf("want agent=agent-a#1 on first %s, got %q", domain.EventSessionDispatchStart, agentVal)
	}
}

// TestSession_Start_WithLogger_LogsDispatchStart_PhaseAndStageFields verifies
// that the EventSessionDispatchStart entry carries the workflow phase and stage
// context so log readers can identify where in the workflow the dispatch occurred.
func TestSession_Start_WithLogger_LogsDispatchStart_PhaseAndStageFields(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, _, orchPath := newLinearSessionWithDebug(t, logger)

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

	ses.Start(context.Background(), baseLinearConfig(orchPath)) //nolint:errcheck

	phaseVal, ok := logger.fieldValue(domain.EventSessionDispatchStart, "phase")
	if !ok {
		t.Errorf("want 'phase' field on %s entry", domain.EventSessionDispatchStart)
	} else if phaseVal == "" {
		t.Errorf("want non-empty 'phase' field on %s", domain.EventSessionDispatchStart)
	}
}

// TestSession_Start_WithLogger_LogsStepDone verifies that the session emits
// EventSessionStepDone after each successful step is applied to the artifact,
// carrying the status code as a structured field.
func TestSession_Start_WithLogger_LogsStepDone(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, _, orchPath := newLinearSessionWithDebug(t, logger)

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

	ses.Start(context.Background(), baseLinearConfig(orchPath)) //nolint:errcheck

	if !logger.eventLogged(domain.EventSessionStepDone) {
		t.Errorf("want %s logged after each completed step, got events: %v",
			domain.EventSessionStepDone, logger.allEvents())
	}
	statusVal, ok := logger.fieldValue(domain.EventSessionStepDone, "status")
	if !ok {
		t.Errorf("want 'status' field on %s entry", domain.EventSessionStepDone)
	} else if statusVal != string(domain.StatusSUCCESS) {
		t.Errorf("want status=SUCCESS on %s, got %q", domain.EventSessionStepDone, statusVal)
	}
}

// ---- harness error, deviation handling, and unresolved deviation logged ----

// TestSession_Start_WithLogger_HarnessError_LogsHarnessError verifies that
// when a harness invocation fails (not a context cancellation), the session
// logs EventSessionHarnessError before terminating.
func TestSession_Start_WithLogger_HarnessError_LogsHarnessError(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, _, orchPath := newTestDeviationWorkflowSession(t, logger)

	// Queue a harness-level error for agent-a.
	f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("simulated harness failure")})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "deviate-log",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}) //nolint:errcheck

	if !logger.eventLogged(domain.EventSessionHarnessError) {
		t.Errorf("want %s logged on harness error, got events: %v",
			domain.EventSessionHarnessError, logger.allEvents())
	}
}

// TestSession_Start_WithLogger_DeviationResolution_LogsDeviation verifies that
// when the engine returns a Deviation decision, the session logs
// EventSessionDeviation before terminating.
func TestSession_Start_WithLogger_DeviationResolution_LogsDeviation(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, _, orchPath := newTestDeviationWorkflowSession(t, logger)

	// agent-a returns PARTIALLY_DONE with no On Findings column -> engine returns Deviation.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "only partly done",
	}})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "deviate-log",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}) //nolint:errcheck

	if !logger.eventLogged(domain.EventSessionDeviation) {
		t.Errorf("want %s logged when engine returns Deviation, got events: %v",
			domain.EventSessionDeviation, logger.allEvents())
	}
}

// TestSession_Start_WithLogger_DeviationUnresolved_LoggedWithoutStoreApply
// verifies that when a harness error occurs and no routing consultant is wired,
// the session logs EventSessionDeviationUnresolved without calling Store.Apply.
// This covers the path that leaves no trace in Orchestration.md.
func TestSession_Start_WithLogger_DeviationUnresolved_LoggedWithoutStoreApply(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, store, orchPath := newTestDeviationWorkflowSession(t, logger)

	// Harness error -> no routing consultant -> EventSessionDeviationUnresolved.
	f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("harness failed")})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "deviate-log",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}) //nolint:errcheck

	if !logger.eventLogged(domain.EventSessionDeviationUnresolved) {
		t.Errorf("want %s logged on unresolved deviation, got events: %v",
			domain.EventSessionDeviationUnresolved, logger.allEvents())
	}
	// Store.Apply must NOT have been called: the unresolved step was never recorded.
	if len(store.Applied) != 0 {
		t.Errorf("want Store.Apply NOT called on unresolved deviation, got %d calls", len(store.Applied))
	}
}

// ---- run-start refusals logged ----

// TestSession_Start_WithLogger_MissingOrchestratorFile_LogsRefusal verifies
// that a run-start refusal occurring before Store.Create is logged as
// EventSessionRefusal. This is the path that currently leaves no trace because
// no artifact exists to record the refusal reason.
func TestSession_Start_WithLogger_MissingOrchestratorFile_LogsRefusal(t *testing.T) {
	logger := &sessionRecordingLogger{}
	dir := t.TempDir()
	ses := session.New(session.Deps{
		Harness:  harness.NewMockAdapter(),
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    logger,
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: filepath.Join(dir, "nonexistent.md"),
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
	if !logger.eventLogged(domain.EventSessionRefusal) {
		t.Errorf("want %s logged on run-start refusal, got events: %v",
			domain.EventSessionRefusal, logger.allEvents())
	}
}

// TestSession_Start_WithLogger_AgentNotFound_LogsRefusal verifies that a
// run-start refusal triggered by a missing agent definition file (pre-dispatch)
// is also logged as EventSessionRefusal.
func TestSession_Start_WithLogger_AgentNotFound_LogsRefusal(t *testing.T) {
	logger := &sessionRecordingLogger{}
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	// Deliberately omit agent-a.md and agent-b.md so agent resolution fails.

	ses := session.New(session.Deps{
		Harness:  harness.NewMockAdapter(),
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    logger,
	})

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	requireRefused(t, got, err)
	if !logger.eventLogged(domain.EventSessionRefusal) {
		t.Errorf("want %s logged on agent-not-found refusal, got events: %v",
			domain.EventSessionRefusal, logger.allEvents())
	}
}

// ---- nil Debug field defaults to no-op; behaviour unchanged ----

// TestSession_Start_NilDebugField_NoopIsDefault verifies that a session created
// without the Debug field set (nil) behaves identically to one with no logger:
// the run completes normally and no panic occurs. This is the regression guard
// that ensures all existing Deps{...} literals (which omit Debug) keep working.
func TestSession_Start_NilDebugField_NoopIsDefault(t *testing.T) {
	// newLinearSession creates Deps without Debug -- Debug is nil.
	ses, f, _, orchPath := newLinearSession(t)

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

	// Behaviour must be identical to a run with a real logger: completes normally.
	requireRunStatus(t, got, err, domain.RunCompleted)
}
