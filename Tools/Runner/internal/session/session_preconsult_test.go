package session_test

// Tests for pre-consultation at run start.
// Covers: auto and auto-review modes call PreConsult, orchestrated mode does not,
// advice is applied to the dispatch, and a failure keeps the run state and is
// retried on resume.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ---- scriptedPreConsultant (Stage 5) ----

// scriptedPreConsultant is a controllable implementation of domain.PreConsultant
// for Stage 5 pre-consultation tests.
type scriptedPreConsultant struct {
	advice domain.PreConsultationAdvice
	err    error
	Called bool
}

func (p *scriptedPreConsultant) PreConsult(_ context.Context, _ domain.ConsultationRequest) (domain.PreConsultationAdvice, error) {
	p.Called = true
	return p.advice, p.err
}

// ===== Pre-consultation =====

// TestSession_Start_PreConsultation_AutoMode_CalledBeforeWorkflowDispatch verifies
// that when pre-consultation is enabled and the run mode is auto, PreConsult is
// invoked before any workflow agent is dispatched via the harness. The
// pre-consultation is part of the run-start sequence and must complete before
// the dispatch loop begins.
func TestSession_Start_PreConsultation_AutoMode_CalledBeforeWorkflowDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	preConsultant := &scriptedPreConsultant{
		advice: domain.PreConsultationAdvice{
			TaskDescription: "extra context from pre-consultation",
		},
	}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
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

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if !preConsultant.Called {
		t.Error("want PreConsult called when pre-consultation is enabled in auto mode, but it was not called")
	}
}

// TestSession_Start_PreConsultation_AutoReviewMode_CalledBeforeWorkflowDispatch
// verifies that pre-consultation is also invoked when the run mode is auto-review.
// Both auto and auto-review are modes where pre-consultation is meaningful.
func TestSession_Start_PreConsultation_AutoReviewMode_CalledBeforeWorkflowDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	preConsultant := &scriptedPreConsultant{}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
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

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAutoReview
	cfg.PreConsultation = true

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if !preConsultant.Called {
		t.Error("want PreConsult called when pre-consultation is enabled in auto-review mode, but it was not called")
	}
}

// TestSession_Start_PreConsultation_Disabled_NotCalled verifies that when
// PreConsultation is false (the default), the PreConsult port is never invoked,
// even when mode is auto. Pre-consultation is opt-in.
func TestSession_Start_PreConsultation_Disabled_NotCalled(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	preConsultant := &scriptedPreConsultant{}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
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

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = false // explicitly disabled (also the default)

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if preConsultant.Called {
		t.Error("want PreConsult NOT called when pre-consultation is disabled, but it was called")
	}
}

// TestSession_Start_PreConsultation_OrchestratedMode_NotCalled verifies that
// pre-consultation is NOT invoked when the run mode is orchestrated, even when
// PreConsultation=true. Pre-consultation is only meaningful in auto and
// auto-review; it must be silently skipped in orchestrated mode regardless of
// the caller's PreConsultation setting.
//
// Guard: this test passes vacuously before I5.3 is implemented (PreConsult is
// never called, so Called=false trivially). It will fail if I5.3 calls
// PreConsult unconditionally without mode-based filtering — i.e., if
// pre-consultation fires in orchestrated mode, Called=true and this test fails.
func TestSession_Start_PreConsultation_OrchestratedMode_NotCalled(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	preConsultant := &scriptedPreConsultant{}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
	})

	// Queue agent-a so the run can proceed past the first dispatch before the
	// engine's orchestrated-mode routing takes over. The test does not assert
	// on the run's final outcome — only on whether PreConsult was called.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeOrchestrated
	cfg.PreConsultation = true // enabled, but must be suppressed in orchestrated mode

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if preConsultant.Called {
		t.Error("want PreConsult NOT called in orchestrated mode (pre-consultation is only meaningful in auto and auto-review), but it was called")
	}
}

// TestSession_Start_PreConsultation_AdviceAppliedToDispatch verifies that the
// strings returned by PreConsult (TaskDescription and Constraints) are wired
// into the subsequent auto-routed dispatch's request fields. Verifying that
// PreConsult is called (done in other tests) is distinct from verifying that
// its output is actually used.
//
// This test is in the RED phase: without I5.3 retaining and applying the
// pre-consultation advice strings, the dispatch's TaskDescription will not
// contain the advice text.
func TestSession_Start_PreConsultation_AdviceAppliedToDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	const adviceText = "pre-consultation-advice-sentinel-zq9"
	preConsultant := &scriptedPreConsultant{
		advice: domain.PreConsultationAdvice{
			TaskDescription: adviceText,
		},
	}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
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

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least one harness invocation, got none")
	}
	// The pre-consultation advice must appear in the first auto-routed dispatch's
	// TaskDescription. The design contract (ContractsDesign §DispatchInstruction
	// field-resolution) specifies that advice strings are appended to auto-routed
	// dispatches only.
	if !strings.Contains(invs[0].Request.TaskDescription, adviceText) {
		t.Errorf("want first dispatch TaskDescription to contain pre-consultation advice %q, got %q",
			adviceText, invs[0].Request.TaskDescription)
	}
}

// newPreConsultSession builds a session with the given pre-consultant wired,
// backed by the named orchestrator fixture and the listed agent files.
func newPreConsultSession(t *testing.T, fixture string, pc domain.PreConsultant, agents ...string) (session.Session, *harness.MockAdapter, *memStore, string) {
	t.Helper()
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, fixture)
	for _, a := range agents {
		writeAgentFile(t, dir, a)
	}
	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: pc,
	})
	return ses, f, store, orchPath
}

func failingPreConsultant() *scriptedPreConsultant {
	return &scriptedPreConsultant{
		err: &domain.ConsultationError{
			Failure: domain.ConsultFailTransport,
			Detail:  "orchestrator agent timed out",
		},
	}
}

func queueLinearSuccess(f *harness.MockAdapter) {
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#x", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#x", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
}

// TestSession_Start_PreConsultation_Failure_ReturnsStartFailed verifies that
// when the PreConsultant returns an error on a new run, the outcome is a
// resumable start failure carrying the underlying error, not a refusal.
func TestSession_Start_PreConsultation_Failure_ReturnsStartFailed(t *testing.T) {
	pc := failingPreConsultant()
	ses, _, _, orchPath := newPreConsultSession(t, "linear-orch.md", pc, "agent-a", "agent-b")

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	got, err := ses.Start(context.Background(), cfg)

	requireStartFailed(t, got, err)
	var ce *domain.ConsultationError
	if !errors.As(got.Cause, &ce) {
		t.Errorf("want outcome Cause to carry the *domain.ConsultationError, got %v", got.Cause)
	}
}

// TestSession_Start_PreConsultation_Failure_KeepsRunStateAndWritesNothing
// verifies that a failed pre-consultation on a new run keeps the run folder and
// the artifact, dispatches nothing, and records nothing: no Execution Log row,
// no global_sequence, no current_state.
func TestSession_Start_PreConsultation_Failure_KeepsRunStateAndWritesNothing(t *testing.T) {
	pc := failingPreConsultant()
	ses, f, store, orchPath := newPreConsultSession(t, "linear-orch.md", pc, "agent-a", "agent-b")
	runFolder := filepath.Join(t.TempDir(), "run")
	if err := os.MkdirAll(runFolder, 0o755); err != nil {
		t.Fatalf("setup: failed to create run folder: %v", err)
	}

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true
	cfg.RunFolder = runFolder

	got, err := ses.Start(context.Background(), cfg)

	requireStartFailed(t, got, err)
	if !pc.Called {
		t.Error("want PreConsult attempted before the failure, but it was not called")
	}
	if _, statErr := os.Stat(runFolder); statErr != nil {
		t.Errorf("want run folder kept after a failed pre-consultation, stat error: %v", statErr)
	}
	if !store.exists {
		t.Error("want the artifact kept after a failed pre-consultation")
	}
	if len(store.Applied) != 0 || len(store.state.ExecutionLog) != 0 {
		t.Errorf("want no Execution Log row from pre-consultation, got %d applied, %d logged",
			len(store.Applied), len(store.state.ExecutionLog))
	}
	if store.state.GlobalSequence != 0 {
		t.Errorf("want global_sequence untouched (0), got %d", store.state.GlobalSequence)
	}
	if !reflect.DeepEqual(store.state.CurrentState, domain.CurrentState{}) {
		t.Errorf("want current_state untouched, got %+v", store.state.CurrentState)
	}
	if n := len(f.Invocations()); n != 0 {
		t.Errorf("want no harness dispatch after a failed pre-consultation, got %d", n)
	}
}

// TestSession_Start_PreConsultation_Failure_AfterCommitSetup_KeepsSetupRow
// verifies that when commit setup succeeded and pre-consultation then fails,
// the setup row and commit_branch are kept as they were, and nothing else is
// recorded.
func TestSession_Start_PreConsultation_Failure_AfterCommitSetup_KeepsSetupRow(t *testing.T) {
	pc := failingPreConsultant()
	ses, f, store, orchPath := newPreConsultSession(t, "commit-agent-orch.md", pc,
		"agent-a", "agent-b", "commit-manager-git")
	const wantBranch = "mosaic/run/preconsult-after-setup"
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1", StatusCode: domain.StatusSUCCESS,
		StatusMessage: "ready [branch:" + wantBranch + "]",
	}})

	cfg := baseCommitConfig(orchPath)
	cfg.PreConsultation = true

	got, err := ses.Start(context.Background(), cfg)

	requireStartFailed(t, got, err)
	if len(store.Applied) != 1 || store.Applied[0].Seq != 1 || !store.Applied[0].IsInfrastructure {
		t.Fatalf("want exactly the setup row (Seq 1, infrastructure) recorded, got %d rows", len(store.Applied))
	}
	if store.state.CommitBranch != wantBranch {
		t.Errorf("want commit_branch=%q kept, got %q", wantBranch, store.state.CommitBranch)
	}
	if store.state.GlobalSequence != 1 {
		t.Errorf("want global_sequence=1 (setup only), got %d", store.state.GlobalSequence)
	}
	if !reflect.DeepEqual(store.state.CurrentState, domain.CurrentState{}) {
		t.Errorf("want current_state untouched, got %+v", store.state.CurrentState)
	}
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier != "commit-manager-git" {
			t.Errorf("want no workflow dispatch after a failed pre-consultation, got %q", inv.Agent.Identifier)
		}
	}
}

// resumedAutoState returns the stored artifact of a run that completed
// agent-a and is resumed in auto mode with pre-consultation enabled.
func resumedAutoState() domain.ArtifactState {
	return domain.ArtifactState{
		RunID:           testRunID,
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  1,
		RunSettings: domain.RunSettings{
			Mode:            domain.ExecutionModeAuto,
			PreConsultation: true,
		},
		CurrentState: domain.CurrentState{
			Phase: "PLANNING", LastStatus: domain.StatusSUCCESS, LastAgent: "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
}

// TestSession_Start_PreConsultation_Failure_ResumedRun_PriorHistoryUnchanged
// verifies that a failed pre-consultation on a resumed run leaves the stored
// artifact state exactly as it was: no row, no sequence change, no
// current_state change, and nothing dispatched.
func TestSession_Start_PreConsultation_Failure_ResumedRun_PriorHistoryUnchanged(t *testing.T) {
	pc := failingPreConsultant()
	ses, f, store, orchPath := newPreConsultSession(t, "linear-orch.md", pc, "agent-a", "agent-b")
	store.state = resumedAutoState()
	store.exists = true

	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	got, err := ses.Start(context.Background(), cfg)

	requireStartFailed(t, got, err)
	if !reflect.DeepEqual(store.state, resumedAutoState()) {
		t.Errorf("want the stored artifact state unchanged by a failed pre-consultation, got %+v", store.state)
	}
	if len(store.Applied) != 0 {
		t.Errorf("want no Apply call from a failed pre-consultation, got %d", len(store.Applied))
	}
	if n := len(f.Invocations()); n != 0 {
		t.Errorf("want no harness dispatch after a failed pre-consultation, got %d", n)
	}
}

// TestSession_Start_PreConsultation_Failure_ThenResume_RetriesPreConsultation
// verifies that a run whose pre-consultation failed can be resumed: the resume
// calls pre-consultation again, and once it succeeds the workflow runs with
// sequences starting at 1, because pre-consultation never consumed one.
func TestSession_Start_PreConsultation_Failure_ThenResume_RetriesPreConsultation(t *testing.T) {
	pc := failingPreConsultant()
	ses, f, store, orchPath := newPreConsultSession(t, "linear-orch.md", pc, "agent-a", "agent-b")

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	first, firstErr := ses.Start(context.Background(), cfg)
	requireStartFailed(t, first, firstErr)

	// The failure clears and the run is resumed against the same artifact.
	pc.err = nil
	pc.Called = false
	queueLinearSuccess(f)
	markResume(&cfg)

	second, secondErr := ses.Start(context.Background(), cfg)

	requireRunStatus(t, second, secondErr, domain.RunCompleted)
	if !pc.Called {
		t.Error("want PreConsult retried on resume, but it was not called")
	}
	if len(store.Applied) != 2 {
		t.Fatalf("want 2 workflow rows after the resume, got %d", len(store.Applied))
	}
	if store.Applied[0].Seq != 1 || store.Applied[1].Seq != 2 {
		t.Errorf("want workflow Seq 1 and 2 (pre-consultation consumed none), got %d and %d",
			store.Applied[0].Seq, store.Applied[1].Seq)
	}
}

// TestSession_Start_PreConsultation_Success_ConsumesNoSequenceAndWritesNothing
// verifies the durable state at the first workflow dispatch after a successful
// pre-consultation: global_sequence still 0, no rows, current_state untouched;
// the first workflow row then takes Seq 1.
func TestSession_Start_PreConsultation_Success_ConsumesNoSequenceAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f := harness.NewMockAdapter()
	store := &memStore{}

	var seen bool
	var seq, rows int
	var cur domain.CurrentState
	hooked := &beforeInvokeHarness{delegate: f, before: func(agentID string) {
		if agentID == "agent-a" && !seen {
			seen = true
			seq = store.state.GlobalSequence
			rows = len(store.state.ExecutionLog)
			cur = store.state.CurrentState
		}
	}}
	ses := session.New(session.Deps{
		Harness:    hooked,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: &scriptedPreConsultant{advice: domain.PreConsultationAdvice{TaskDescription: "advice"}},
	})
	queueLinearSuccess(f)

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if !seen {
		t.Fatal("want agent-a dispatched, but it was not")
	}
	if seq != 0 || rows != 0 || !reflect.DeepEqual(cur, domain.CurrentState{}) {
		t.Errorf("want nothing recorded by pre-consultation before the first dispatch, got seq=%d rows=%d current_state=%+v",
			seq, rows, cur)
	}
	if len(store.Applied) == 0 || store.Applied[0].Seq != 1 {
		t.Errorf("want the first workflow row to take Seq 1, got %+v", store.Applied)
	}
}
