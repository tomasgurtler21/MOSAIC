package main

// Shared helpers for the buildDeps tests: a single call adapter over buildDeps
// (so a change to the builder's parameter list touches one place), a store
// holding a resumable artifact with recorded settings, a resume RunConfig that
// supplies no settings, and an Interaction double that records questions.

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mosaic-common/interaction"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// resumeTestRunID is the run identity of the resumable artifact fixtures.
const resumeTestRunID = "20260928T103906Z-d408"

// buildDepsFromTransports is the one call site of buildDeps used by the wiring
// tests. Availability of the consultation ports is a function of the transports
// handed in (raw invoker, interaction channel), never of run settings, so the
// tests pass no settings of their own.
func buildDepsFromTransports(
	invoker domain.RawInvoker,
	interact domain.Interaction,
	approvals domain.ApprovalReader,
	dispLogger domain.DispatchLogger,
) session.Deps {
	return buildDeps(invoker, interact, approvals, dispLogger)
}

// recordingInteraction is a mainTestNoopInteraction that counts SelectOne calls,
// the question the manual resolver asks when it is reached.
type recordingInteraction struct {
	mainTestNoopInteraction
	mu         sync.Mutex
	selectOnes int
}

func (r *recordingInteraction) SelectOne(ctx context.Context, q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	r.mu.Lock()
	r.selectOnes++
	r.mu.Unlock()
	return r.mainTestNoopInteraction.SelectOne(ctx, q)
}

func (r *recordingInteraction) selectOneCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.selectOnes
}

// recordedResumeStore returns a store holding the artifact of a linear run that
// completed agent-a and recorded the given runner settings.
func recordedResumeStore(recorded domain.RunSettings) *mainTestMemStore {
	return &mainTestMemStore{
		exists: true,
		state: domain.ArtifactState{
			Type:            "orchestration-artifact",
			RunID:           resumeTestRunID,
			Workflow:        "linear",
			WorkflowVersion: "1.0",
			Task:            "test task",
			GlobalSequence:  1,
			RunSettings:     recorded,
			CurrentState: domain.CurrentState{
				Phase: "PLANNING", LastStatus: domain.StatusSUCCESS, LastAgent: "agent-a#1",
			},
			ExecutionLog: []domain.ExecutionLogEntry{
				{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			},
		},
	}
}

// resumeConfig returns the RunConfig of a resume whose frontend supplied the
// given settings values and marked none of them as supplied, as both frontends
// do when the user passed no setting.
func resumeConfig(orchPath string, defaults domain.RunSettings) domain.RunConfig {
	return domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false,
		RunID:                resumeTestRunID,
		RunFolder:            filepath.Join(filepath.Dir(orchPath), domain.RunScopedFolder(resumeTestRunID)),
		RunSettings:          defaults,
	}
}

// resumeScenario is a resumable linear run assembled around deps from the
// shared builder.
type resumeScenario struct {
	ses      session.Session
	harness  *harness.MockAdapter
	invoker  *fakeRawInvoker
	interact *recordingInteraction
	orchPath string
}

// newResumeScenario builds the session from buildDeps output plus the
// frontend-supplied infrastructure ports, over a store that records the given
// settings. The invoker answers with the scripted raw responses in order.
func newResumeScenario(t *testing.T, recorded domain.RunSettings, rawResponses ...string) *resumeScenario {
	t.Helper()
	dir := t.TempDir()
	orchPath := copyOrchestratorFileForMain(t, dir, "linear-orch.md")
	writeAgentFileForMain(t, dir, "agent-a")
	writeAgentFileForMain(t, dir, "agent-b")

	sc := &resumeScenario{
		harness:  harness.NewMockAdapter(),
		invoker:  &fakeRawInvoker{},
		interact: &recordingInteraction{},
		orchPath: orchPath,
	}
	for _, r := range rawResponses {
		sc.invoker.responses = append(sc.invoker.responses, []byte(r))
	}

	deps := buildDepsFromTransports(sc.invoker, sc.interact, nil, nil)
	deps.Harness = sc.harness
	deps.Store = recordedResumeStore(recorded)
	deps.Clock = mainTestClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	deps.Interact = sc.interact
	sc.ses = session.New(deps)
	return sc
}

// startSessionGuarded runs Start and turns a panic into a test failure, so a
// nil port dereference reports as one failing test instead of aborting the
// package run.
func startSessionGuarded(t *testing.T, ses session.Session, cfg domain.RunConfig) (out domain.RunOutcome, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Start panicked instead of reporting a failure or honoring the recorded settings: %v", r)
		}
	}()
	return ses.Start(context.Background(), cfg)
}

func (sc *resumeScenario) start(t *testing.T, cfg domain.RunConfig) (domain.RunOutcome, error) {
	t.Helper()
	return startSessionGuarded(t, sc.ses, cfg)
}

// queueAgentBSuccess scripts a successful agent-b dispatch.
func (sc *resumeScenario) queueAgentBSuccess() {
	sc.harness.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
}

// failingHarnessEntry scripts a harness invocation that fails, which sends the
// session into a consultation.
func failingHarnessEntry() harness.ScriptedEntry {
	return harness.ScriptedEntry{Err: errors.New("simulated harness failure")}
}
