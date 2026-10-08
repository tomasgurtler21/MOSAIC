package tui

// restart_resume_helpers_test.go holds the fixtures shared by the restart and
// history tests: an artifact store double that serves a fixed Execution Log,
// a model builder for a run whose artifact already exists, drivers for each
// restart path that return the command the restart produced, and a runner
// that executes that command far enough to observe the RunConfig the session
// is started with.

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui/screens/runconfig"
	"mosaic-run/internal/tui/screens/runflow"
)

const (
	restartTestRunFolder = "/ws/Orchestration-20260801T120000Z-ab12"
	restartTestRunID     = "20260801T120000Z-ab12"
	restartTestSeed      = "seed-input.md"
)

// readOnlyStore is a domain.ArtifactStore whose Read returns a fixed state and
// error. Every write method panics: restart and history code only reads.
type readOnlyStore struct {
	state domain.ArtifactState
	err   error
}

func (s *readOnlyStore) Read(_ context.Context) (domain.ArtifactState, error) {
	return s.state, s.err
}

func (s *readOnlyStore) Create(context.Context, domain.WorkflowInfo, string, domain.RunSettings, time.Time, string) (domain.ArtifactState, error) {
	panic("readOnlyStore.Create: unexpected call")
}

func (s *readOnlyStore) Apply(context.Context, domain.ArtifactState, domain.CompletedStep) (domain.ArtifactState, error) {
	panic("readOnlyStore.Apply: unexpected call")
}

func (s *readOnlyStore) SetPhase(context.Context, domain.ArtifactState, string, time.Time) (domain.ArtifactState, error) {
	panic("readOnlyStore.SetPhase: unexpected call")
}

func (s *readOnlyStore) SetCommitBranch(context.Context, string, time.Time) (domain.ArtifactState, error) {
	panic("readOnlyStore.SetCommitBranch: unexpected call")
}

func (s *readOnlyStore) AdoptRunnerSettings(context.Context, domain.ExecutionMode, bool, bool, time.Time) (domain.ArtifactState, error) {
	panic("readOnlyStore.AdoptRunnerSettings: unexpected call")
}

// storeWithLog returns a readable store whose artifact holds log.
func storeWithLog(log ...domain.ExecutionLogEntry) *readOnlyStore {
	return &readOnlyStore{state: domain.ArtifactState{RunID: restartTestRunID, ExecutionLog: log}}
}

// sampleLog is an Execution Log with a workflow step, a staged step and an
// infrastructure-style row without a stage.
func sampleLog() []domain.ExecutionLogEntry {
	return []domain.ExecutionLogEntry{
		{Seq: 1, Agent: "planner#1", Phase: "PLANNING", Stage: "", WorkflowRow: 1, Status: domain.StatusSUCCESS},
		{Seq: 2, Agent: "builder#2", Phase: "EXECUTION", Stage: "Implementation.1", WorkflowRow: 2, Status: domain.StatusBLOCKED},
		{Seq: 3, Agent: "checkpoint#3", Phase: "EXECUTION", Stage: "Implementation.1", Status: domain.StatusSUCCESS},
	}
}

// sampleHistory is the progress rows sampleLog must produce.
func sampleHistory() []runflow.ProgressRow {
	return []runflow.ProgressRow{
		{AgentInstance: "planner#1", Phase: "PLANNING", Stage: "", Status: "SUCCESS"},
		{AgentInstance: "builder#2", Phase: "EXECUTION", Stage: "Implementation.1", Status: "BLOCKED"},
		{AgentInstance: "checkpoint#3", Phase: "EXECUTION", Stage: "Implementation.1", Status: "SUCCESS"},
	}
}

// restartFixture is a model for a run that was created in this process
// (isNewRun true, seed input chosen) and has since stopped, with its artifact
// served by store.
type restartFixture struct {
	m            *rootModel
	sess         *capturingSession
	stopSignal   *session.StopSignal
	factoryCalls *[]factoryCall
}

func newRestartFixture(t *testing.T, store domain.ArtifactStore) *restartFixture {
	t.Helper()
	capSess := newCapturingSession()
	stopSignal := session.NewStopSignal()
	calls := &[]factoryCall{}
	m := newRootModel(context.Background(), capSess, Options{
		Theme:      tuicommon.DefaultTheme(),
		StopSignal: stopSignal,
		SessionFactory: func(runFolder string, isNewRun bool, _ string, _ runconfig.ConfigSelection) session.Session {
			*calls = append(*calls, factoryCall{runFolder: runFolder, isNewRun: isNewRun})
			return capSess
		},
		ArtifactStoreFactory: func(string) domain.ArtifactStore { return store },
	})
	m.selections.isNewRun = true
	m.selections.runID = restartTestRunID
	m.selections.runFolder = restartTestRunFolder
	m.selections.orchestratorFile = "/ws/orchestrator.md"
	m.selections.workflowID = "wf"
	m.selections.task = "build it"
	m.selections.seedInput = restartTestSeed
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	return &restartFixture{m: m, sess: capSess, stopSignal: stopSignal, factoryCalls: calls}
}

// restartDriver drives one restart path to completion and returns the command
// the restart produced. rebuilds reports whether the path rebuilds the session
// through the factory (false: the existing session is reused).
type restartDriver struct {
	name     string
	rebuilds bool
	drive    func(t *testing.T, m *rootModel) tea.Cmd
}

var restartDrivers = []restartDriver{
	{"done-screen continue", true, driveDoneContinue},
	{"exec-override retry", true, driveExecOverrideRetry},
	{"stop-recovery retry", false, driveStopRecoveryRetry},
	{"stop-recovery manual dispatch", false, driveStopRecoveryManual},
}

func driveDoneContinue(t *testing.T, m *rootModel) tea.Cmd {
	t.Helper()
	sendRunStoppedDone(m)
	if m.screen != screenDone {
		t.Fatalf("precondition: screen = %v after a RunStopped outcome, want screenDone", m.screen)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	requireOnProgress(t, m)
	return cmd
}

func driveExecOverrideRetry(t *testing.T, m *rootModel) tea.Cmd {
	t.Helper()
	m.Update(runErrorMsg{err: launchFailureErr("claude-code", "/usr/bin/claude")})
	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v after a launch failure, want screenExecOverride", m.screen)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/opt/claude")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	requireOnProgress(t, m)
	return cmd
}

func driveStopRecoveryRetry(t *testing.T, m *rootModel) tea.Cmd {
	t.Helper()
	enterStopRecoveryScreen(t, m)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	requireOnProgress(t, m)
	return cmd
}

func driveStopRecoveryManual(t *testing.T, m *rootModel) tea.Cmd {
	t.Helper()
	enterStopRecoveryScreen(t, m)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	requireOnProgress(t, m)
	return cmd
}

func requireOnProgress(t *testing.T, m *rootModel) {
	t.Helper()
	if m.screen != screenProgress {
		t.Fatalf("precondition: screen = %v after the restart action, want screenProgress; "+
			"the restart did not happen, so nothing about the resumed run can be asserted", m.screen)
	}
}

// startedConfig executes cmd (a batch that includes the session start) and
// returns the RunConfig the session was started with. Sub-commands run in
// goroutines so the progress screen's tick, which sleeps, cannot block the test.
func startedConfig(t *testing.T, cmd tea.Cmd, sess *capturingSession) domain.RunConfig {
	t.Helper()
	if cmd == nil {
		t.Fatal("the restart returned a nil command; it must return a command that starts the session")
	}
	var run func(c tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			if batch, ok := c().(tea.BatchMsg); ok {
				for _, sub := range batch {
					run(sub)
				}
			}
		}()
	}
	run(cmd)
	select {
	case cfg := <-sess.configs:
		return cfg
	case <-time.After(3 * time.Second):
		t.Fatal("the session was not started by the command the restart returned")
		return domain.RunConfig{}
	}
}
