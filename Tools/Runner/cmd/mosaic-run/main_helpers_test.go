package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mosaic-common/interaction"

	"mosaic-run/internal/domain"
)

// ---------------------------------------------------------------------------
// Shared test infrastructure for behavioural session tests in cmd/mosaic-run
// ---------------------------------------------------------------------------

// fakeRawInvoker is a scripted domain.RawInvoker for use in session integration
// tests. It returns pre-configured byte slices in FIFO order, enabling tests to
// script orchestrator consultation responses without spawning a real agent.
type fakeRawInvoker struct {
	mu        sync.Mutex
	responses [][]byte
	callCount int
}

func (f *fakeRawInvoker) InvokeRaw(_ context.Context, _ domain.AgentReference, _ []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.callCount >= len(f.responses) {
		return nil, fmt.Errorf("fakeRawInvoker: no more scripted responses (call index %d, have %d)",
			f.callCount, len(f.responses))
	}
	resp := f.responses[f.callCount]
	f.callCount++
	return resp, nil
}

// mainTestMemStore is a minimal in-memory ArtifactStore for use in behavioural
// session tests in the cmd/mosaic-run package. It follows the same contract as
// the memStore in internal/session/session_test.go.
type mainTestMemStore struct {
	state  domain.ArtifactState
	exists bool
}

func (m *mainTestMemStore) Read(_ context.Context) (domain.ArtifactState, error) {
	if !m.exists {
		return domain.ArtifactState{}, os.ErrNotExist
	}
	return m.state, nil
}

func (m *mainTestMemStore) Create(_ context.Context, info domain.WorkflowInfo, task string, settings domain.RunSettings, now time.Time, runID string) (domain.ArtifactState, error) {
	m.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		RunID:           runID,
		Workflow:        info.ID,
		WorkflowVersion: info.Version,
		Task:            task,
		Started:         now,
		LastUpdated:     now,
		GlobalSequence:  0,
		RunSettings:     settings,
	}
	m.exists = true
	return m.state, nil
}

func (m *mainTestMemStore) Apply(_ context.Context, state domain.ArtifactState, step domain.CompletedStep) (domain.ArtifactState, error) {
	state.GlobalSequence = step.Seq
	if !step.IsInfrastructure {
		state.CurrentState = domain.CurrentState{
			Phase:      step.Phase,
			Stage:      step.Stage,
			LastStatus: step.Status,
			LastAgent:  step.AgentInstance,
		}
	}
	entry := domain.ExecutionLogEntry{
		Seq:         step.Seq,
		Agent:       step.AgentInstance,
		Phase:       step.Phase,
		Stage:       step.Stage,
		WorkflowRow: step.WorkflowRow,
		Status:      step.Status,
	}
	// Like the file store, only a BLOCKED row carries its error code in the log.
	if step.Status == domain.StatusBLOCKED {
		entry.ErrorCode = step.ErrorCode
	}
	state.ExecutionLog = append(state.ExecutionLog, entry)
	m.state = state
	return state, nil
}

func (m *mainTestMemStore) SetPhase(_ context.Context, _ domain.ArtifactState, _ string, _ time.Time) (domain.ArtifactState, error) {
	return domain.ArtifactState{}, fmt.Errorf("mainTestMemStore.SetPhase: not implemented in cmd/mosaic-run session tests")
}

func (m *mainTestMemStore) SetCommitBranch(_ context.Context, branch string, _ time.Time) (domain.ArtifactState, error) {
	m.state.CommitBranch = branch
	return m.state, nil
}

func (m *mainTestMemStore) AdoptRunnerSettings(_ context.Context, mode domain.ExecutionMode, pre, manual bool, _ time.Time) (domain.ArtifactState, error) {
	m.state.Mode = mode
	m.state.PreConsultation = pre
	m.state.ManualResolution = manual
	return m.state, nil
}

// mainTestClock is a fixed-time domain.Clock for use in cmd/mosaic-run
// behavioural session tests.
type mainTestClock struct{ t time.Time }

func (c mainTestClock) Now() time.Time { return c.t }

// mainTestNoopInteraction is a no-op implementation of interaction.Interaction
// for use in cmd/mosaic-run behavioural session tests. All question methods
// return Answered status; Notify and Progress are silent.
type mainTestNoopInteraction struct{}

func (n *mainTestNoopInteraction) SelectOne(_ context.Context, _ interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
}
func (n *mainTestNoopInteraction) SelectMany(_ context.Context, _ interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.Answered}, nil
}
func (n *mainTestNoopInteraction) AskText(_ context.Context, _ interaction.TextQuestion) (interaction.TextAnswer, error) {
	return interaction.TextAnswer{Status: interaction.Answered}, nil
}
func (n *mainTestNoopInteraction) Confirm(_ context.Context, _ interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.Answered}, nil
}
func (n *mainTestNoopInteraction) Notify(_ context.Context, _ interaction.Notice)          {}
func (n *mainTestNoopInteraction) Progress(_ context.Context, _ interaction.ProgressEvent) {}

// copyOrchestratorFileForMain copies the named session fixture file into the
// given directory and returns the destination path. Fixtures are loaded from
// the shared testdata/session directory within Tools/Runner.
func copyOrchestratorFileForMain(t *testing.T, dir, fixtureName string) string {
	t.Helper()
	src := filepath.Join(mainTestOrchestratorDir, fixtureName)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("copyOrchestratorFileForMain: read %q: %v", src, err)
	}
	dst := filepath.Join(dir, "orchestrator.md")
	if err := os.WriteFile(dst, data, 0600); err != nil {
		t.Fatalf("copyOrchestratorFileForMain: write %q: %v", dst, err)
	}
	return dst
}

// writeAgentFileForMain creates a minimal agent definition file in dir with the
// given agent identifier as the filename stem, matching the format expected by
// the agentresolve package.
func writeAgentFileForMain(t *testing.T, dir, agentID string) {
	t.Helper()
	path := filepath.Join(dir, agentID+".md")
	if err := os.WriteFile(path, []byte("# Agent: "+agentID+"\n"), 0600); err != nil {
		t.Fatalf("writeAgentFileForMain(%q): %v", agentID, err)
	}
}
