package session_test

// Tests for orchestrator resolution and RunContextBinder behaviour in the
// session's run-start sequence.
//
// Coverage:
//
//   Orchestrator refusal before artifact creation:
//   - A run whose orchestrator file has an empty filename stem is refused before
//     any artifact is created. The empty-stem condition can only be caught by
//     ResolveOrchestrator (not orchfile, which reads the file regardless of name),
//     so this test specifically covers the session-level enforcement of the
//     "refuses before any artifact is created" guarantee.
//   - The refusal is returned as RunRefused with a nil error (not infrastructure).
//   - The refusal message is non-empty and surfaceable to the user.
//
//   RunContextBinder on routing consultant:
//   - When the routing consultant implements domain.RunContextBinder, the session
//     calls BindRunContext on it before the first consultation.
//   - The bound RunContext carries a non-empty routing table.
//   - The bound RunContext's Orchestrator carries InvocationOrchestrator kind.
//   - The bound Orchestrator's Identifier matches the orchestrator file's stem.
//
//   RunContextBinder on manual resolver:
//   - When the manual resolver implements domain.RunContextBinder, the session
//     calls BindRunContext on it as well, providing a non-empty routing table.
//
//   RunContextBinder on pre-consultant:
//   - When the pre-consultant implements domain.RunContextBinder and
//     PreConsultation is enabled, the session calls BindRunContext on it.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ---- bindingRecordingConsultant ----

// bindingRecordingConsultant implements domain.RoutingConsultant and
// domain.RunContextBinder. It records whether BindRunContext was called and
// what it was called with. ConsultRouting returns a stop instruction so the
// run ends without requiring a real harness dispatch.
type bindingRecordingConsultant struct {
	BoundContext  *domain.RunContext
	BindCallCount int
}

func (b *bindingRecordingConsultant) BindRunContext(rc domain.RunContext) {
	b.BindCallCount++
	copy := rc
	b.BoundContext = &copy
}

func (b *bindingRecordingConsultant) ConsultRouting(_ context.Context, _ domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	return domain.RoutingInstruction{
		Stop: &domain.StopInstruction{Reason: "bindingRecordingConsultant: stop"},
	}, nil
}

// ---- bindingRecordingPreConsultant ----

// bindingRecordingPreConsultant implements domain.PreConsultant and
// domain.RunContextBinder, recording binding calls for pre-consultation tests.
type bindingRecordingPreConsultant struct {
	BoundContext  *domain.RunContext
	BindCallCount int
}

func (b *bindingRecordingPreConsultant) BindRunContext(rc domain.RunContext) {
	b.BindCallCount++
	copy := rc
	b.BoundContext = &copy
}

func (b *bindingRecordingPreConsultant) PreConsult(_ context.Context, _ domain.ConsultationRequest) (domain.PreConsultationAdvice, error) {
	return domain.PreConsultationAdvice{}, nil
}

// ===== Orchestrator refusal before artifact creation =====

// TestSession_Start_EmptyStemOrchestrator_RefusesBeforeArtifact verifies that
// when the orchestrator file's filename has an empty stem (e.g. ".md"), the
// session refuses the run before creating any artifact.
//
// The empty-stem condition survives orchfile.GetWorkflow (which reads the file's
// content successfully) but must be caught by ResolveOrchestrator (which checks
// the filename). This test is therefore sensitive to whether the session calls
// ResolveOrchestrator before step 8 (artifact creation).
func TestSession_Start_EmptyStemOrchestrator_RefusesBeforeArtifact(t *testing.T) {
	dir := t.TempDir()

	// Name the orchestrator file ".md" — it has an empty filename stem because
	// filepath.Ext(".md") == ".md" and TrimSuffix leaves an empty string.
	// The file content is the same as the linear-orch.md fixture, so orchfile
	// can parse the workflow region successfully.
	orchContent, err := os.ReadFile(orchFilePath("linear-orch.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	orchPath := filepath.Join(dir, ".md")
	if err := os.WriteFile(orchPath, orchContent, 0600); err != nil {
		t.Fatalf("write empty-stem orchestrator file: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  harness.NewFakeAdapter(),
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeAuto,
		},
	}

	got, runErr := ses.Start(context.Background(), cfg)

	// Refusal must be returned; no infrastructure error.
	requireRefused(t, got, runErr)
	// The artifact must not have been created before the refusal.
	if store.exists {
		t.Error("want no artifact created before orchestrator refusal, but artifact was created in store")
	}
}

// TestSession_Start_EmptyStemOrchestrator_RefusalMessageNonEmpty verifies that
// the refusal message produced by an empty-stem orchestrator path is non-empty
// and therefore surfaceable to the user.
func TestSession_Start_EmptyStemOrchestrator_RefusalMessageNonEmpty(t *testing.T) {
	dir := t.TempDir()
	orchContent, err := os.ReadFile(orchFilePath("linear-orch.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	orchPath := filepath.Join(dir, ".md")
	if err := os.WriteFile(orchPath, orchContent, 0600); err != nil {
		t.Fatalf("write empty-stem orchestrator file: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  harness.NewFakeAdapter(),
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeAuto,
		},
	}

	got, runErr := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, runErr)
	if msg == "" {
		t.Error("want non-empty refusal message, got empty string")
	}
}

// ===== RunContextBinder: routing consultant =====

// TestSession_Start_BindsRunContextToRoutingConsultant verifies that when the
// routing consultant implements domain.RunContextBinder, the session calls
// BindRunContext on it before the first consultation.
func TestSession_Start_BindsRunContextToRoutingConsultant(t *testing.T) {
	recorder := &bindingRecordingConsultant{}
	// Use the existing newOrchestratedSession helper (from session_test.go).
	ses, _, store, orchPath := newOrchestratedSession(t, recorder)
	_ = store

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	if recorder.BoundContext == nil {
		t.Fatal("want session to call BindRunContext on routing consultant; got no binding")
	}
}

// TestSession_Start_BoundRoutingTable_IsNonEmpty verifies that the RunContext
// handed to the routing consultant carries a non-empty routing table — the
// parsed workflow's rows.
func TestSession_Start_BoundRoutingTable_IsNonEmpty(t *testing.T) {
	recorder := &bindingRecordingConsultant{}
	ses, _, store, orchPath := newOrchestratedSession(t, recorder)
	_ = store

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	if recorder.BoundContext == nil {
		t.Fatal("want BindRunContext to be called; got no binding")
	}
	if len(recorder.BoundContext.Table.Rows) == 0 {
		t.Error("want non-empty routing table in BoundContext (workflow has rows); got empty")
	}
}

// TestSession_Start_BoundOrchestrator_HasOrchestratorInvocationKind verifies
// that the RunContext handed to the routing consultant carries an orchestrator
// reference with InvocationKind == InvocationOrchestrator.
func TestSession_Start_BoundOrchestrator_HasOrchestratorInvocationKind(t *testing.T) {
	recorder := &bindingRecordingConsultant{}
	ses, _, store, orchPath := newOrchestratedSession(t, recorder)
	_ = store

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	if recorder.BoundContext == nil {
		t.Fatal("want BindRunContext to be called; got no binding")
	}
	if recorder.BoundContext.Orchestrator.InvocationKind != domain.InvocationOrchestrator {
		t.Errorf("want Orchestrator.InvocationKind %q, got %q",
			domain.InvocationOrchestrator,
			recorder.BoundContext.Orchestrator.InvocationKind)
	}
}

// TestSession_Start_BoundOrchestrator_IdentifierMatchesFileStem verifies that
// the bound orchestrator reference's Identifier equals the stem of the
// orchestrator file path supplied to the run. copyOrchestratorFile names the
// file "orchestrator.md", so the expected stem is "orchestrator".
func TestSession_Start_BoundOrchestrator_IdentifierMatchesFileStem(t *testing.T) {
	recorder := &bindingRecordingConsultant{}
	ses, _, store, orchPath := newOrchestratedSession(t, recorder)
	_ = store

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	if recorder.BoundContext == nil {
		t.Fatal("want BindRunContext to be called; got no binding")
	}
	// copyOrchestratorFile names the file "orchestrator.md", stem is "orchestrator".
	const wantIdentifier = "orchestrator"
	if recorder.BoundContext.Orchestrator.Identifier != wantIdentifier {
		t.Errorf("want Orchestrator.Identifier %q (file stem), got %q",
			wantIdentifier, recorder.BoundContext.Orchestrator.Identifier)
	}
}

// ===== RunContextBinder: manual resolver =====

// TestSession_Start_BindsRunContextToManualResolver verifies that when the
// manual resolver implements domain.RunContextBinder, the session calls
// BindRunContext on it as well as on the routing consultant.
func TestSession_Start_BindsRunContextToManualResolver(t *testing.T) {
	routingRecorder := &bindingRecordingConsultant{}
	manualRecorder := &bindingRecordingConsultant{}

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	ses := session.New(session.Deps{
		Harness:  harness.NewFakeAdapter(),
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Routing:  routingRecorder,
		Manual:   manualRecorder,
	})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	if manualRecorder.BoundContext == nil {
		t.Fatal("want session to call BindRunContext on manual resolver; got no binding")
	}
	if len(manualRecorder.BoundContext.Table.Rows) == 0 {
		t.Error("want non-empty routing table in manual resolver's BoundContext; got empty")
	}
}

// ===== RunContextBinder: pre-consultant =====

// TestSession_Start_BindsRunContextToPreConsultant verifies that when the
// pre-consultant implements domain.RunContextBinder and PreConsultation is
// enabled, the session calls BindRunContext on it.
func TestSession_Start_BindsRunContextToPreConsultant(t *testing.T) {
	routingRecorder := &bindingRecordingConsultant{}
	preConsultRecorder := &bindingRecordingPreConsultant{}

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	ses := session.New(session.Deps{
		Harness:    harness.NewFakeAdapter(),
		Store:      &memStore{},
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		Routing:    routingRecorder,
		PreConsult: preConsultRecorder,
	})
	cfg := baseOrchestratedConfig(orchPath)
	cfg.RunSettings.PreConsultation = true

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if preConsultRecorder.BoundContext == nil {
		t.Fatal("want session to call BindRunContext on pre-consultant; got no binding")
	}
	if len(preConsultRecorder.BoundContext.Table.Rows) == 0 {
		t.Error("want non-empty routing table in pre-consultant's BoundContext; got empty")
	}
}
