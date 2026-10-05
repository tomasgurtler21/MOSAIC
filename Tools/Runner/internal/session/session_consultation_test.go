package session_test

// Tests for consultations leaving no trace in the artifact (no Execution Log
// row, no global_sequence use, no current_state change; visible in the
// diagnostic log), failure handling (terminal failures, artifact
// left resumable, ManualResolution fallback, StopInstruction, harness errors as
// deviations), and write discipline (Apply-before-consult ordering, re-read
// after consultation returns).

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Consultation recording =====

// newOrchestratedSessionWithDebug is newOrchestratedSession with a diagnostic
// logger wired into Deps.Debug, so a test can prove a consultation happened
// without reading the artifact.
func newOrchestratedSessionWithDebug(t *testing.T, consultant domain.RoutingConsultant, debug domain.DebugLogger) (
	ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string,
) {
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
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    debug,
	})
	return
}

// requireNoConsultationRows fails when any applied row is attributed to the
// orchestrator identity, whether flagged infrastructure or not: a consultation
// must leave no Execution Log row behind. Genuine infrastructure rows for
// non-orchestrator agents (e.g. review agents) are legitimate workflow rows
// and are not consultation artifacts, so they are not flagged here.
func requireNoConsultationRows(t *testing.T, store *memStore) {
	t.Helper()
	for _, step := range store.Applied {
		if !strings.HasPrefix(step.AgentInstance, "orchestrator#") {
			continue
		}
		// HITL-rejected attempts are classified as non-workflow (infrastructure)
		// rows by design; they are not consultation rows, but they are still
		// orchestrator-attributed and so still fail the check below.
		if step.IsInfrastructure && !step.HITLRejected {
			t.Errorf("want no infrastructure row for a consultation, got %q (Seq %d)",
				step.AgentInstance, step.Seq)
			continue
		}
		t.Errorf("want no orchestrator-attributed row, got %q (Seq %d)",
			step.AgentInstance, step.Seq)
	}
	for _, e := range store.state.ExecutionLog {
		if strings.HasPrefix(e.Agent, "orchestrator#") {
			t.Errorf("want no orchestrator-attributed Execution Log entry, got %q", e.Agent)
		}
	}
}

// TestSession_Consultation_LeavesNoExecutionLogRow verifies that a routing
// consultation is not recorded in the Execution Log: only the workflow step the
// consultation chose is recorded. That the consultation happened is proven by
// the consultant's call capture and the diagnostic log.
func TestSession_Consultation_LeavesNoExecutionLogRow(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do planning", 0)
	consultant.queueStop("done")
	debug := &sessionRecordingLogger{}

	ses, f, store, orchPath := newOrchestratedSessionWithDebug(t, consultant, debug)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	requireNoConsultationRows(t, store)
	if len(store.Applied) != 1 || !strings.Contains(store.Applied[0].AgentInstance, "agent-a") {
		t.Errorf("want exactly one applied row (agent-a), got %+v", store.Applied)
	}
	if len(store.state.ExecutionLog) != 1 {
		t.Errorf("want exactly one Execution Log entry, got %d", len(store.state.ExecutionLog))
	}
	// The consultation is still observable outside the artifact.
	if consultant.CallCount != 2 {
		t.Errorf("want 2 consultations observed through the consultant, got %d", consultant.CallCount)
	}
	if !debug.eventLogged(domain.EventSessionConsultStop) {
		t.Error("want the stop consultation visible in the diagnostic log")
	}
}

// TestSession_Consultation_DoesNotConsumeGlobalSequence verifies that a
// consultation does not consume a global_sequence slot: after one consultation
// dispatching one workflow step and one stop consultation, the workflow step
// holds Seq 1 and global_sequence is 1.
func TestSession_Consultation_DoesNotConsumeGlobalSequence(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "step 1", 0)
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	if store.state.GlobalSequence != 1 {
		t.Errorf("want GlobalSequence == 1 (workflow step only), got %d", store.state.GlobalSequence)
	}
	if len(store.Applied) == 0 || store.Applied[0].Seq != 1 {
		t.Errorf("want the dispatched workflow step recorded at Seq 1, got %+v", store.Applied)
	}
}

// TestSession_Consultation_DoesNotMoveCurrentState verifies that a consultation
// leaves current_state alone: a run whose only consultation stops immediately
// records no position at all, and after a dispatched step current_state names
// that workflow step.
func TestSession_Consultation_DoesNotMoveCurrentState(t *testing.T) {
	t.Run("stop before any dispatch leaves state untouched", func(t *testing.T) {
		consultant := &scriptedRoutingConsultant{}
		consultant.queueStop("nothing to do")

		ses, _, store, orchPath := newOrchestratedSession(t, consultant)

		ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

		if len(store.Applied) != 0 {
			t.Errorf("want no rows recorded, got %d", len(store.Applied))
		}
		if store.state.GlobalSequence != 0 {
			t.Errorf("want global_sequence untouched (0), got %d", store.state.GlobalSequence)
		}
		if !reflect.DeepEqual(store.state.CurrentState, domain.CurrentState{}) {
			t.Errorf("want current_state untouched, got %+v", store.state.CurrentState)
		}
	})

	t.Run("after a dispatched step current_state names the workflow step", func(t *testing.T) {
		consultant := &scriptedRoutingConsultant{}
		consultant.queueDispatch("agent-a", "step 1", 0)
		consultant.queueStop("done")

		ses, f, store, orchPath := newOrchestratedSession(t, consultant)
		f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#1",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "done",
		}})

		ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

		if store.state.CurrentState.LastAgent != "agent-a#1" {
			t.Errorf("want current_state.LastAgent agent-a#1, got %q", store.state.CurrentState.LastAgent)
		}
	})
}

// ===== Failure handling =====

// TestSession_ConsultationFailure_ReturnsStoppedByConsultant verifies that a
// consultation error (any failure class) is terminal by default and returns
// RunStoppedByConsultant.
func TestSession_ConsultationFailure_ReturnsStoppedByConsultant(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueError(domain.ConsultFailMalformedJSON) // first consultation fails

	ses, _, _, orchPath := newOrchestratedSession(t, consultant)

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error (consultation failure encoded in RunOutcome), got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant for consultation failure, got %q (message: %q)",
			got.Status, got.Message)
	}
}

// TestSession_ConsultationFailure_RecordsNothingInArtifact verifies that a
// failed consultation leaves no row, does not advance global_sequence and does
// not change current_state. The failure stays visible in the diagnostic log.
func TestSession_ConsultationFailure_RecordsNothingInArtifact(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueError(domain.ConsultFailMalformedJSON)
	debug := &sessionRecordingLogger{}

	ses, _, store, orchPath := newOrchestratedSessionWithDebug(t, consultant, debug)

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	if len(store.Applied) != 0 || len(store.state.ExecutionLog) != 0 {
		t.Errorf("want no Execution Log row for a failed consultation, got %d applied, %d logged",
			len(store.Applied), len(store.state.ExecutionLog))
	}
	if store.state.GlobalSequence != 0 {
		t.Errorf("want global_sequence untouched (0), got %d", store.state.GlobalSequence)
	}
	if !reflect.DeepEqual(store.state.CurrentState, domain.CurrentState{}) {
		t.Errorf("want current_state untouched, got %+v", store.state.CurrentState)
	}
	if !debug.eventLogged(domain.EventSessionConsultFailed) {
		t.Error("want the failed consultation visible in the diagnostic log")
	}
}

// TestSession_ConsultationFailure_ArtifactLeftResumable verifies that after a
// consultation failure the artifact state is left intact and the run is
// resumable. The artifact must still exist (not deleted) and have a valid state.
func TestSession_ConsultationFailure_ArtifactLeftResumable(t *testing.T) {
	// Dispatch agent-a successfully, then fail on the second consultation.
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "step 1", 0)
	consultant.queueError(domain.ConsultFailTransport) // second consultation fails

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant, got %q", got.Status)
	}
	// The artifact must still be present (store.exists=true) and have the
	// agent-a step recorded. A deleted or reset artifact is not resumable.
	if !store.exists {
		t.Error("want artifact to exist (run is resumable after consultation failure)")
	}
	agentARecorded := false
	for _, step := range store.Applied {
		if strings.Contains(step.AgentInstance, "agent-a") {
			agentARecorded = true
			break
		}
	}
	if !agentARecorded {
		t.Error("want agent-a's completed step recorded in the artifact (run is resumable)")
	}
	requireNoConsultationRows(t, store)
	if store.state.GlobalSequence != 1 {
		t.Errorf("want global_sequence 1 (agent-a only) after the failed consultation, got %d",
			store.state.GlobalSequence)
	}
}

// TestSession_ManualResolutionEnabled_ConsultationFailure_UsesManualResolver
// verifies that in orchestrated mode with ManualResolution=true, a consultation
// failure falls back to the Manual resolver instead of terminating.
func TestSession_ManualResolutionEnabled_ConsultationFailure_UsesManualResolver(t *testing.T) {
	// Primary consultant: fails on first call.
	primary := &scriptedRoutingConsultant{}
	primary.queueError(domain.ConsultFailTransport)

	// Manual resolver: dispatches agent-a after the primary fails.
	manual := &scriptedRoutingConsultant{}
	manual.queueDispatch("agent-a", "manual resolution fallback", 0)
	manual.queueStop("done")

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  primary,
		Manual:   manual,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done via manual",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:             domain.ExecutionModeOrchestrated,
			ManualResolution: true,
		},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// The manual resolver must have been invoked after the primary consultant failed.
	if manual.CallCount == 0 {
		t.Error("want ManualResolver called after primary consultation failure with ManualResolution=true, got 0 calls")
	}
}

// TestSession_StopInstruction_ReturnsStoppedByConsultant verifies that a
// StopInstruction from the RoutingConsultant causes the session to return
// RunStoppedByConsultant.
func TestSession_StopInstruction_ReturnsStoppedByConsultant(t *testing.T) {
	const stopReason = "orchestrator decided to pause here"

	consultant := &scriptedRoutingConsultant{}
	consultant.queueStop(stopReason)

	ses, _, _, orchPath := newOrchestratedSession(t, consultant)

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after stop instruction, got %q (message: %q)",
			got.Status, got.Message)
	}
	if got.StopReason != stopReason {
		t.Errorf("want StopReason=%q, got %q", stopReason, got.StopReason)
	}
}

// TestSession_StopInstruction_RecordsNothingInArtifact verifies that a stop
// consultation leaves no row and does not advance global_sequence; the stop is
// visible in the diagnostic log.
func TestSession_StopInstruction_RecordsNothingInArtifact(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStop("pause here")
	debug := &sessionRecordingLogger{}

	ses, _, store, orchPath := newOrchestratedSessionWithDebug(t, consultant, debug)

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	if len(store.Applied) != 0 || store.state.GlobalSequence != 0 {
		t.Errorf("want no row and global_sequence 0, got %d applied, global_sequence %d",
			len(store.Applied), store.state.GlobalSequence)
	}
	if !debug.eventLogged(domain.EventSessionConsultStop) {
		t.Error("want the stop consultation visible in the diagnostic log")
	}
}

// TestSession_SubagentHarnessError_IsDeviation_NotCrash verifies that a
// harness-level error while invoking a subagent is treated as a deviation
// (the consultant is invoked to decide what to do next) and does NOT crash
// the session or return RunFailed.
func TestSession_SubagentHarnessError_IsDeviation_NotCrash(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// In orchestrated mode: dispatch agent-a; when it fails at harness level,
	// the session must consult again (deviation). The consultant then dispatches
	// agent-a again for a retry.
	consultant.queueDispatch("agent-a", "try the work", 0)
	// After the harness error, consultation is triggered again:
	consultant.queueDispatch("agent-a", "retry after harness error", 0)
	consultant.queueStop("done after retry")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	// First agent-a invocation: harness error.
	f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("harness: subprocess timed out")})
	// Second agent-a invocation (after retry): SUCCESS.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done on retry",
	}})

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error (harness error is a deviation, not a crash), got %v", err)
	}
	// The session must NOT return RunFailed for a harness error.
	if got.Status == domain.RunFailed {
		t.Errorf("want harness error treated as deviation (not RunFailed), got RunFailed (message: %q)",
			got.Message)
	}
}

// ===== Write discipline =====

// TestSession_WriteBeforeConsult_PrecedingResultWrittenBeforeConsultation
// verifies that the preceding agent's result is written to the artifact store
// (via Apply) before the RoutingConsultant is invoked. This guarantees the
// orchestrator reads an up-to-date artifact.
//
// Note: this ordering invariant holds both with and without a consultation
// row, so the test may pass before the consultation-row removal. It guards the
// agent's row being written before the second consultation regardless.
func TestSession_WriteBeforeConsult_PrecedingResultWrittenBeforeConsultation(t *testing.T) {
	inner := &scriptedRoutingConsultant{}
	inner.queueDispatch("agent-a", "step 1", 0)
	inner.queueStop("done") // second consultation, after agent-a

	store := &memStore{}
	tracking := &applyBeforeConsultConsultant{inner: inner, store: store}

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  tracking,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}) //nolint:errcheck

	if len(tracking.ApplyAtCall) < 2 {
		t.Fatalf("want at least 2 ConsultRouting calls to observe ordering, got %d", len(tracking.ApplyAtCall))
	}
	// First consultation: no workflow step has been completed yet (Apply=0).
	if tracking.ApplyAtCall[0] != 0 {
		t.Errorf("want 0 workflow Apply calls at first consultation, got %d", tracking.ApplyAtCall[0])
	}
	// Second consultation (after agent-a): agent-a's result must already be written (Apply >= 1).
	if tracking.ApplyAtCall[1] < 1 {
		t.Errorf("want >= 1 Apply calls at second consultation (agent-a result written first), got %d",
			tracking.ApplyAtCall[1])
	}
}

// TestSession_RereadAfterConsult_ArtifactRereadAfterConsultation verifies that
// the artifact is re-read from the store after a consultation returns, so any
// Workflow Notes the orchestrator appended during its deliberation are visible
// in the session's next iteration.
//
// Note: this may pass before the consultation-row removal because the current
// code incidentally re-reads after writing the consultation row. Its value is
// ensuring the re-read survives the refactor.
func TestSession_RereadAfterConsult_ArtifactRereadAfterConsultation(t *testing.T) {
	inner := &scriptedRoutingConsultant{}
	inner.queueDispatch("agent-a", "step 1", 0)
	inner.queueStop("done")

	store := &memStore{}
	tracking := &applyBeforeConsultConsultant{inner: inner, store: store}

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  tracking,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}) //nolint:errcheck

	if len(tracking.ReadAtCall) < 2 {
		t.Fatalf("want at least 2 ConsultRouting calls, got %d", len(tracking.ReadAtCall))
	}
	// After the first consultation returns (which dispatched agent-a), the session
	// must re-read the artifact before the second consultation. So the Read count
	// at the second consultation must be higher than at the first.
	if tracking.ReadAtCall[1] <= tracking.ReadAtCall[0] {
		t.Errorf("want ReadCount to increase between consultations (re-read after each), "+
			"got ReadAtCall[0]=%d ReadAtCall[1]=%d", tracking.ReadAtCall[0], tracking.ReadAtCall[1])
	}
}

// ===== Workflow Notes preservation =====

// noteAppendingConsultant wraps a scripted consultant and, on the listed call
// numbers (1-based), appends a Workflow Note directly to the stored artifact the
// way a script orchestrator does during its deliberation. The note's Seq is the
// artifact's global_sequence at that moment.
type noteAppendingConsultant struct {
	inner   *scriptedRoutingConsultant
	store   *memStore
	onCalls map[int]string // call number -> note text
	calls   int
}

func (c *noteAppendingConsultant) ConsultRouting(ctx context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	c.calls++
	if text, ok := c.onCalls[c.calls]; ok {
		c.store.state.WorkflowNotes = append(c.store.state.WorkflowNotes, domain.WorkflowNote{
			Seq:  c.store.state.GlobalSequence,
			Note: text,
		})
	}
	return c.inner.ConsultRouting(ctx, req)
}

// TestSession_Consultation_WorkflowNotesAppendedDuringConsultation_SurviveNextWrite
// verifies that Workflow Notes appended by the script orchestrator during a
// routing consultation are preserved by the next state write. The first note is
// written before any invocation is recorded (Seq 0); the second after one
// recorded invocation (Seq 1).
func TestSession_Consultation_WorkflowNotesAppendedDuringConsultation_SurviveNextWrite(t *testing.T) {
	inner := &scriptedRoutingConsultant{}
	inner.queueDispatch("agent-a", "step 1", 0)
	inner.queueDispatch("agent-b", "step 2", 1)
	inner.queueStop("done")
	store := &memStore{}
	consultant := &noteAppendingConsultant{
		inner: inner, store: store,
		onCalls: map[int]string{1: "note before any invocation", 2: "note after agent-a"},
	}

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f := harness.NewMockAdapter()
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	for _, id := range []string{"agent-a", "agent-b"} {
		f.Queue(id, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: id + "#1",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "done",
		}})
	}

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	want := []domain.WorkflowNote{
		{Seq: 0, Note: "note before any invocation"},
		{Seq: 1, Note: "note after agent-a"},
	}
	if !reflect.DeepEqual(store.state.WorkflowNotes, want) {
		t.Errorf("want consultant-appended Workflow Notes preserved unchanged, got %+v", store.state.WorkflowNotes)
	}
}
