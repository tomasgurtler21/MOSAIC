package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Pre-consultation failure keeps the run and is retried on resume =====

// intPreConsultant is a scripted domain.PreConsultant for integration tests.
type intPreConsultant struct {
	err   error
	Calls int
}

func (p *intPreConsultant) PreConsult(_ context.Context, _ domain.ConsultationRequest) (domain.PreConsultationAdvice, error) {
	p.Calls++
	return domain.PreConsultationAdvice{}, p.err
}

// TestIntegration_PreConsultation_Failure_KeepsArtifactThenResumeRetries
// verifies on the real file store that a failed pre-consultation leaves the
// artifact on disk with no Execution Log row and global_sequence 0, dispatches
// nothing, and that a resume retries pre-consultation and then runs the
// workflow with sequences starting at 1.
func TestIntegration_PreConsultation_Failure_KeepsArtifactThenResumeRetries(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	artifactPath := filepath.Join(scopedRunFolder(t, dir), "Orchestration.md")

	pc := &intPreConsultant{err: &domain.ConsultationError{
		Failure: domain.ConsultFailTransport,
		Detail:  "orchestrator agent timed out",
	}}
	newPreConsultSession := func(f *harness.MockAdapter) session.Session {
		return session.New(session.Deps{
			Harness:    f,
			Store:      artifact.NewFileStore(artifactPath),
			Clock:      fixedClock{t: epoch},
			Interact:   &noopInteraction{},
			Approvals:  alwaysApprovedReader{},
			PreConsult: pc,
		})
	}
	cfg := domain.RunConfig{
		RunID:                integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "preconsult task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:            domain.ExecutionModeAuto,
			PreConsultation: true,
		},
	}

	first := harness.NewMockAdapter()
	got, err := newPreConsultSession(first).Start(context.Background(), cfg)

	requireStartFailed(t, got, err)
	data, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("want the artifact kept after a failed pre-consultation, read error: %v", readErr)
	}
	state, parseErr := artifact.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse artifact: %v", parseErr)
	}
	if len(state.ExecutionLog) != 0 || state.GlobalSequence != 0 {
		t.Errorf("want nothing recorded by pre-consultation, got %d rows and global_sequence=%d",
			len(state.ExecutionLog), state.GlobalSequence)
	}
	if n := len(first.Invocations()); n != 0 {
		t.Errorf("want no dispatch after a failed pre-consultation, got %d", n)
	}

	// The failure clears; the run is resumed.
	pc.err = nil
	second := harness.NewMockAdapter()
	second.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	second.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	cfg.IsNewRun = false
	cfg.RunFolder = filepath.Dir(artifactPath)

	got, err = newPreConsultSession(second).Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if pc.Calls != 2 {
		t.Errorf("want pre-consultation attempted on the start and retried on resume (2 calls), got %d", pc.Calls)
	}
	data, readErr = os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("read artifact: %v", readErr)
	}
	state, parseErr = artifact.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse artifact: %v", parseErr)
	}
	if len(state.ExecutionLog) != 2 || state.ExecutionLog[0].Seq != 1 || state.ExecutionLog[1].Seq != 2 {
		t.Errorf("want workflow rows Seq 1 and 2 (pre-consultation consumed none), got %+v", state.ExecutionLog)
	}
}
