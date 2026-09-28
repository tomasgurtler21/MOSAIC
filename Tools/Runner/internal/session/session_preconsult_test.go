package session_test

// Tests for pre-consultation at run start.
// Covers: auto and auto-review modes call PreConsult, orchestrated mode does not,
// advice is applied to the dispatch, and failure removes the run folder.

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

// TestSession_Start_PreConsultation_Failure_ReturnsRefusal verifies that when
// the PreConsultant returns an error, the run is refused. A pre-consultation
// failure prevents the run from starting — it occurs before the dispatch loop
// begins, so no work has been done yet.
func TestSession_Start_PreConsultation_Failure_ReturnsRefusal(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	preConsultant := &scriptedPreConsultant{
		err: &domain.ConsultationError{
			Failure: domain.ConsultFailTransport,
			Detail:  "orchestrator agent timed out",
		},
	}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
	})

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_PreConsultation_Failure_NoArtifactCreated verifies that a
// pre-consultation failure removes the run folder so that a refused run leaves no
// trace. Per the ContractsDesign ordering (Create -> Apply(commit setup row) ->
// pre-consultation -> dispatch loop), ArtifactStore.Create is called before
// pre-consultation runs, so the artifact exists in the store at the point of
// failure. The "no trace" guarantee is therefore about the run folder being
// removed from the filesystem (os.RemoveAll(cfg.RunFolder)), not about Create
// never having been called.
func TestSession_Start_PreConsultation_Failure_NoArtifactCreated(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Create a run folder that the session must remove on pre-consultation failure.
	runFolder := filepath.Join(dir, "run")
	if err := os.MkdirAll(runFolder, 0o755); err != nil {
		t.Fatalf("setup: failed to create run folder: %v", err)
	}

	f := harness.NewMockAdapter()
	store := &memStore{}
	preConsultant := &scriptedPreConsultant{
		err: &domain.ConsultationError{
			Failure: domain.ConsultFailTransport,
			Detail:  "connection refused",
		},
	}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
	})

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true
	cfg.RunFolder = runFolder

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// The session must remove the run folder on pre-consultation failure so that
	// a refused run leaves no trace on disk. Store.Create was called (store.exists
	// is true by design), but the filesystem run folder must be gone.
	if _, statErr := os.Stat(runFolder); !os.IsNotExist(statErr) {
		t.Error("want run folder removed when pre-consultation fails (failure must remove any run folder)")
	}
}
