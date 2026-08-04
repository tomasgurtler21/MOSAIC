package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-common/interaction"
	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
)

// ---- scripted Session ----

// scriptedSession implements session.Session with scripted responses.
// It records the RunConfig passed to Start so tests can assert flag-to-config mapping.
type scriptedSession struct {
	outcome domain.RunOutcome
	err     error
	called  bool
	config  domain.RunConfig
}

func (s *scriptedSession) Start(_ context.Context, config domain.RunConfig) (domain.RunOutcome, error) {
	s.called = true
	s.config = config
	return s.outcome, s.err
}

// ---- helper ----

func runCLI(t *testing.T, args []string, sess *scriptedSession) (exitCode int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cli.Run(context.Background(), args, nil, nil, sess, &out, &errOut)
	return code, out.String(), errOut.String()
}

// ---- tests: missing required flags ----

func TestMissingRequiredFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "missing orchestrator-file",
			args:    []string{"run", "--workflow", "w1", "--task", "do work"},
			wantErr: "--orchestrator-file",
		},
		{
			name:    "missing workflow",
			args:    []string{"run", "--orchestrator-file", "orch.md", "--task", "do work"},
			wantErr: "--workflow",
		},
		{
			name:    "missing task",
			args:    []string{"run", "--orchestrator-file", "orch.md", "--workflow", "w1"},
			wantErr: "--task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &scriptedSession{}
			code, _, errOut := runCLI(t, tt.args, sess)
			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
			}
			if !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("stderr %q does not contain %q", errOut, tt.wantErr)
			}
			if sess.called {
				t.Error("session.Start should not be called when required flags are missing")
			}
		})
	}
}

// ---- tests: default flag values ----

func TestDefaultFlagValues(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	// Use runCLIWithStore (not runCLI) so that the test does not panic after I5.4
	// adds a store.SetPhase call on RunCompleted. A nil store would panic at that point.
	code, _, _ := runCLIWithStore(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "my-workflow",
		"--task", "do the work",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}

	cfg := sess.config
	if cfg.OrchestratorFilePath != "orch.md" {
		t.Errorf("OrchestratorFilePath = %q, want %q", cfg.OrchestratorFilePath, "orch.md")
	}
	if string(cfg.WorkflowID) != "my-workflow" {
		t.Errorf("WorkflowID = %q, want %q", cfg.WorkflowID, "my-workflow")
	}
	if cfg.Task != "do the work" {
		t.Errorf("Task = %q, want %q", cfg.Task, "do the work")
	}
	if cfg.OnDeviation != domain.DeviationDelegate {
		t.Errorf("OnDeviation = %q, want %q (default)", cfg.OnDeviation, domain.DeviationDelegate)
	}
	if cfg.AllowVersionDrift {
		t.Error("AllowVersionDrift should default to false")
	}
	if cfg.Checkpoints {
		t.Error("Checkpoints should default to false (--checkpoints=disabled)")
	}
}

// ---- tests: all flags explicitly set ----

func TestAllFlagsExplicitlySet(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, _ := runCLI(t, []string{
		"run",
		"--orchestrator-file", "/path/to/orch.md",
		"--workflow", "greenfield-tdd",
		"--task", "build the feature",
		"--on-deviation", "stop",
		"--allow-version-drift",
		"--checkpoints", "enabled",
		"--new-run",
	}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}

	cfg := sess.config
	if cfg.OrchestratorFilePath != "/path/to/orch.md" {
		t.Errorf("OrchestratorFilePath = %q, want %q", cfg.OrchestratorFilePath, "/path/to/orch.md")
	}
	if string(cfg.WorkflowID) != "greenfield-tdd" {
		t.Errorf("WorkflowID = %q, want %q", cfg.WorkflowID, "greenfield-tdd")
	}
	if cfg.Task != "build the feature" {
		t.Errorf("Task = %q, want %q", cfg.Task, "build the feature")
	}
	if cfg.OnDeviation != domain.DeviationStop {
		t.Errorf("OnDeviation = %q, want %q", cfg.OnDeviation, domain.DeviationStop)
	}
	if !cfg.AllowVersionDrift {
		t.Error("AllowVersionDrift should be true when --allow-version-drift is set")
	}
	if !cfg.Checkpoints {
		t.Error("Checkpoints should be true when --checkpoints=enabled")
	}
}

// ---- tests: exit code mapping ----

func TestExitCodeMapping(t *testing.T) {
	baseArgs := []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "t1",
	}

	tests := []struct {
		status   domain.RunStatus
		wantCode int
	}{
		{domain.RunCompleted, cli.ExitSuccess},
		{domain.RunStopped, cli.ExitStopped},
		{domain.RunDeviationUnresolved, cli.ExitDeviationUnresolved},
		{domain.RunRefused, cli.ExitRefused},
		{domain.RunFailed, cli.ExitFailure},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			sess := &scriptedSession{
				outcome: domain.RunOutcome{Status: tt.status, Message: "test outcome message"},
			}
			// Use runCLIWithStore (not runCLI) so that the RunCompleted subtest does
			// not panic after I5.4 adds a store.SetPhase call on RunCompleted.
			code, _, _ := runCLIWithStore(t, baseArgs, &spyStore{}, sess)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d for status %q", code, tt.wantCode, tt.status)
			}
		})
	}
}

// ---- tests: progress output format ----

// TestProgressOutputFormat verifies that the CLI Interaction's Notify method writes
// a machine-readable line per completed invocation that contains the agent instance
// identifier, phase, and status (FR-5 / AC9.4).
func TestProgressOutputFormat(t *testing.T) {
	var buf bytes.Buffer
	interact := cli.NewInteraction(&buf)

	// Simulate the Notify call the session makes after each completed invocation.
	interact.Notify(context.Background(), interaction.Notice{
		Level:   interaction.NoticeInfo,
		Title:   "implementation-tdd#3",
		Message: `phase=EXECUTION stage="Stage-1" status=SUCCESS`,
	})

	output := buf.String()

	// Must contain the agent instance identifier.
	if !strings.Contains(output, "implementation-tdd#3") {
		t.Errorf("output %q does not contain agent instance ID %q", output, "implementation-tdd#3")
	}
	// Must contain the phase.
	if !strings.Contains(output, "EXECUTION") {
		t.Errorf("output %q does not contain phase %q", output, "EXECUTION")
	}
	// Must contain the status.
	if !strings.Contains(output, "SUCCESS") {
		t.Errorf("output %q does not contain status %q", output, "SUCCESS")
	}
	// Must be a single line (machine-readable: one line per step).
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 output line per step, got %d lines: %v", len(lines), lines)
	}
}

// TestProgressOutputParseable verifies the format is deterministically parseable.
func TestProgressOutputParseable(t *testing.T) {
	var buf bytes.Buffer
	interact := cli.NewInteraction(&buf)

	interact.Notify(context.Background(), interaction.Notice{
		Level:   interaction.NoticeWarning,
		Title:   "research#1",
		Message: `phase=RESEARCH stage="" status=PARTIALLY_DONE`,
	})

	output := strings.TrimRight(buf.String(), "\n")
	// Format is "[<level>] <title>: <message>" — verify prefix.
	if !strings.HasPrefix(output, "[warning] research#1:") {
		t.Errorf("output %q does not match expected format \"[<level>] <title>: ...\"", output)
	}
}

// ---- tests: invalid flag values ----

func TestInvalidFlagValues(t *testing.T) {
	tests := []struct {
		name     string
		extraArg string
		extraVal string
	}{
		{"invalid --on-deviation", "--on-deviation", "invalid-value"},
		{"invalid --checkpoints", "--checkpoints", "invalid-value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &scriptedSession{}
			code, _, _ := runCLI(t, []string{
				"run",
				"--orchestrator-file", "f",
				"--workflow", "w",
				"--task", "t",
				tt.extraArg, tt.extraVal,
			}, sess)
			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
			}
			if sess.called {
				t.Error("session.Start should not be called for invalid flag values")
			}
		})
	}
}

// ---- tests: no subcommand ----

func TestNoSubcommand(t *testing.T) {
	sess := &scriptedSession{}
	code, _, _ := runCLI(t, []string{}, sess)
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if sess.called {
		t.Error("session.Start should not be called when no subcommand is given")
	}
}

// ---- tests: progress event output format ----

// TestProgressEventFormat verifies that the CLI Interaction's Progress method writes
// a machine-readable line containing the phase, current/total counters, and subject.
func TestProgressEventFormat(t *testing.T) {
	var buf bytes.Buffer
	interact := cli.NewInteraction(&buf)

	interact.Progress(context.Background(), interaction.ProgressEvent{
		Phase:   "dispatching",
		Current: 3,
		Total:   10,
		Subject: "research#1",
	})

	output := strings.TrimRight(buf.String(), "\n")

	if !strings.Contains(output, "dispatching") {
		t.Errorf("output %q does not contain phase %q", output, "dispatching")
	}
	if !strings.Contains(output, "3") {
		t.Errorf("output %q does not contain current count", output)
	}
	if !strings.Contains(output, "10") {
		t.Errorf("output %q does not contain total count", output)
	}
	if !strings.Contains(output, "research#1") {
		t.Errorf("output %q does not contain subject %q", output, "research#1")
	}
	// Must be a single line (machine-readable: one line per event).
	lines := strings.Split(output, "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 output line per progress event, got %d lines: %v", len(lines), lines)
	}
}

// ---- tests: session error path ----

// TestSessionError verifies that when session.Start returns a non-nil error the CLI
// maps that to ExitFailure (unexpected infrastructure error, not a run outcome).
func TestSessionError(t *testing.T) {
	sess := &scriptedSession{err: fmt.Errorf("harness unavailable")}
	code, _, errOut := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "t1",
	}, sess)

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d)", code, cli.ExitFailure)
	}
	if !strings.Contains(errOut, "harness unavailable") {
		t.Errorf("stderr %q does not contain error message", errOut)
	}
}

// ---- tests: exit code constants ----

func TestExitCodeConstants(t *testing.T) {
	// Verify distinct values and FR-4 constraint (0 = completed run).
	if cli.ExitSuccess != 0 {
		t.Errorf("ExitSuccess = %d, want 0 (FR-4)", cli.ExitSuccess)
	}

	codes := map[string]int{
		"ExitSuccess":             cli.ExitSuccess,
		"ExitFailure":             cli.ExitFailure,
		"ExitStopped":             cli.ExitStopped,
		"ExitUsage":               cli.ExitUsage,
		"ExitRefused":             cli.ExitRefused,
		"ExitDeviationUnresolved": cli.ExitDeviationUnresolved,
	}

	seen := make(map[int]string)
	for name, code := range codes {
		if prev, dup := seen[code]; dup {
			t.Errorf("duplicate exit code %d shared by %s and %s", code, prev, name)
		}
		seen[code] = name
	}
}

// ============================================================
// T5.2: CLI flag change tests
// ============================================================
//
// These tests specify the new --run and --new-run flag behaviour and verify
// that the removed flags (--existing-artifact, --artifact-location) are no
// longer recognised by the CLI.
//
// All tests in this section are in the RED phase: they compile but fail because
// the implementation (I5.3) has not been completed yet.

// ---- spyStore: fake ArtifactStore that records SetPhase calls ----

// spyStore is a test double for domain.ArtifactStore.
// It panics on operations that CLI tests should not trigger (Read, Create, Apply),
// and records every SetPhase call for T5.3 assertions.
type spyStore struct {
	setCalls []spySetPhaseCall
}

type spySetPhaseCall struct {
	state domain.ArtifactState
	phase string
	now   time.Time
}

func (s *spyStore) Read(_ context.Context) (domain.ArtifactState, error) {
	panic("spyStore.Read: unexpected call in CLI tests")
}

func (s *spyStore) Create(_ context.Context, _ domain.WorkflowInfo, _ string, _ bool, _ time.Time, _ string) (domain.ArtifactState, error) {
	panic("spyStore.Create: unexpected call in CLI tests")
}

func (s *spyStore) Apply(_ context.Context, _ domain.ArtifactState, _ domain.CompletedStep) (domain.ArtifactState, error) {
	panic("spyStore.Apply: unexpected call in CLI tests")
}

func (s *spyStore) SetPhase(_ context.Context, state domain.ArtifactState, phase string, now time.Time) (domain.ArtifactState, error) {
	s.setCalls = append(s.setCalls, spySetPhaseCall{state, phase, now})
	state.CurrentState.Phase = phase
	return state, nil
}

// ---- helper: runCLIWithStore ----

// runCLIWithStore is like runCLI but injects a real store (for T5.3 tests).
func runCLIWithStore(t *testing.T, args []string, store domain.ArtifactStore, sess *scriptedSession) (exitCode int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cli.Run(context.Background(), args, store, nil, sess, &out, &errOut)
	return code, out.String(), errOut.String()
}

// ---- helper: writeCompletedRunArtifact ----

// writeCompletedRunArtifact creates a completed run folder and its Orchestration.md
// inside rootDir. Returns the absolute path to the Orchestration.md file.
func writeCompletedRunArtifact(t *testing.T, rootDir, runID string) string {
	t.Helper()
	folderPath := filepath.Join(rootDir, "Orchestration-"+runID)
	if err := os.MkdirAll(folderPath, 0700); err != nil {
		t.Fatalf("writeCompletedRunArtifact: %v", err)
	}
	artifactContent := `---
type: orchestration-artifact
workflow: test-workflow
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
checkpoints: disabled
current_state:
  phase: COMPLETED
  stage: ""
  last_status: SUCCESS
  last_agent: "agent#1"
  error_code: null
---

[[SECTION:ExecutionLog]]
| Seq | Agent   | Phase     | Stage | Status  | Timestamp            | Summary | Checkpoint |
| --- | ------- | --------- | ----- | ------- | -------------------- | ------- | ---------- |
| 1   | agent#1 | EXECUTION | -     | SUCCESS | 2026-01-01T00:00:00Z | done    | -          |
[[/SECTION:ExecutionLog]]

[[SECTION:Artifacts]]
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
[[/SECTION:Artifacts]]
`
	artifactPath := filepath.Join(folderPath, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(artifactContent), 0600); err != nil {
		t.Fatalf("writeCompletedRunArtifact: %v", err)
	}
	return artifactPath
}

const testRunID = "20260727T170000Z-a3f9"

// cliRunID1 and cliRunID2 are run IDs used in default-scan-path tests that need
// more than one resumable candidate in the working directory.
const (
	cliRunID1 = "20260201T000000Z-cccc"
	cliRunID2 = "20260202T000000Z-dddd"
)

// writeResumableRunArtifact creates a resumable (non-COMPLETED) run folder and
// its Orchestration.md inside rootDir. Returns the absolute path of the artifact.
func writeResumableRunArtifact(t *testing.T, rootDir, runID string) string {
	t.Helper()
	folderPath := filepath.Join(rootDir, "Orchestration-"+runID)
	if err := os.MkdirAll(folderPath, 0700); err != nil {
		t.Fatalf("writeResumableRunArtifact: %v", err)
	}
	artifactContent := `---
type: orchestration-artifact
workflow: test-workflow
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
checkpoints: disabled
current_state:
  phase: EXECUTION
  stage: ""
  last_status: SUCCESS
  last_agent: "agent#1"
  error_code: null
---

[[SECTION:ExecutionLog]]
| Seq | Agent   | Phase     | Stage | Status  | Timestamp            | Summary | Checkpoint |
| --- | ------- | --------- | ----- | ------- | -------------------- | ------- | ---------- |
| 1   | agent#1 | EXECUTION | -     | SUCCESS | 2026-01-01T00:00:00Z | done    | -          |
[[/SECTION:ExecutionLog]]

[[SECTION:Artifacts]]
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
[[/SECTION:Artifacts]]
`
	artifactPath := filepath.Join(folderPath, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(artifactContent), 0600); err != nil {
		t.Fatalf("writeResumableRunArtifact: %v", err)
	}
	return artifactPath
}

// ---- tests: --new-run flag ----

func TestNewRunFlag_SetsIsNewRunTrue(t *testing.T) {
	// --new-run must set RunConfig.IsNewRun to true and cause the session to be called.
	// Currently fails (RED) because --new-run is not a recognised flag yet (I5.3).
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--new-run",
	}, sess)

	if !sess.called {
		t.Fatal("session.Start was not called; --new-run must cause the session to run")
	}
	if !sess.config.IsNewRun {
		t.Error("IsNewRun = false, want true when --new-run is set")
	}
}

// ---- tests: --run flag ----

func TestRunFlag_SetsRunID(t *testing.T) {
	// --run <run_id> must set RunConfig.RunID to the given run_id value.
	// Currently fails (RED) because --run is not a recognised flag yet (I5.3).
	// Filesystem setup: create a resumable run folder so the CLI can find it after
	// --run is implemented and reads the artifact to confirm the run is not COMPLETED.
	rootDir := t.TempDir()
	writeResumableRunArtifact(t, rootDir, testRunID)
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--run", testRunID,
	}, sess)

	if !sess.called {
		t.Fatal("session.Start was not called; --run must cause the session to run")
	}
	if sess.config.RunID != testRunID {
		t.Errorf("RunID = %q, want %q", sess.config.RunID, testRunID)
	}
}

func TestRunFlag_SetsIsNewRunFalse(t *testing.T) {
	// --run must set RunConfig.IsNewRun to false (resuming, not creating).
	// Currently fails (RED) because --run is not a recognised flag yet (I5.3).
	// Filesystem setup mirrors TestRunFlag_SetsRunID.
	rootDir := t.TempDir()
	writeResumableRunArtifact(t, rootDir, testRunID)
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--run", testRunID,
	}, sess)

	if !sess.called {
		t.Fatal("session.Start was not called; --run must cause the session to run")
	}
	if sess.config.IsNewRun {
		t.Error("IsNewRun = true, want false when --run selects an existing run")
	}
}

func TestRunFlag_SetsRunFolder(t *testing.T) {
	// --run <run_id> must set RunConfig.RunFolder to the derived folder path.
	// Currently fails (RED) because --run is not a recognised flag yet (I5.3).
	// Filesystem setup mirrors TestRunFlag_SetsRunID.
	rootDir := t.TempDir()
	writeResumableRunArtifact(t, rootDir, testRunID)
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--run", testRunID,
	}, sess)

	if !sess.called {
		t.Fatal("session.Start was not called; --run must cause the session to run")
	}
	if sess.config.RunFolder == "" {
		t.Error("RunFolder is empty, want a path derived from the run_id")
	}
	wantFolderName := "Orchestration-" + testRunID
	if !strings.Contains(sess.config.RunFolder, wantFolderName) {
		t.Errorf("RunFolder = %q, want it to contain %q", sess.config.RunFolder, wantFolderName)
	}
}

// ---- tests: --run and --new-run mutual exclusivity ----

func TestRunAndNewRunFlags_MutuallyExclusive(t *testing.T) {
	// Passing both --run and --new-run must be rejected with a clear error.
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--run", testRunID,
		"--new-run",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for mutually exclusive flags", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Errorf("stderr %q does not contain %q", errOut, "mutually exclusive")
	}
	if sess.called {
		t.Error("session.Start should not be called when flags are mutually exclusive")
	}
}

// ---- tests: removed flags ----

func TestExistingArtifactFlag_IsNoLongerRecognized(t *testing.T) {
	// --existing-artifact was removed in Stage 5; cobra must reject it as unknown.
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--existing-artifact", "resume",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("--existing-artifact: exit code = %d, want ExitUsage (%d) (flag must be removed)",
			code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "unknown flag") && !strings.Contains(errOut, "existing-artifact") {
		t.Errorf("stderr %q does not indicate that --existing-artifact is unknown", errOut)
	}
}

func TestArtifactLocationFlag_IsNoLongerRecognized(t *testing.T) {
	// --artifact-location was removed in Stage 5; cobra must reject it as unknown.
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--artifact-location", "/some/path",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("--artifact-location: exit code = %d, want ExitUsage (%d) (flag must be removed)",
			code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "unknown flag") && !strings.Contains(errOut, "artifact-location") {
		t.Errorf("stderr %q does not indicate that --artifact-location is unknown", errOut)
	}
}

// ---- tests: help text ----

func TestHelpText_ContainsNewRunFlag(t *testing.T) {
	// The help text must mention --new-run after implementation.
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	if !strings.Contains(errOut, "--new-run") {
		t.Errorf("help text does not contain --new-run; got:\n%s", errOut)
	}
}

func TestHelpText_ContainsRunFlag(t *testing.T) {
	// The help text must mention --run after implementation.
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	if !strings.Contains(errOut, "--run") {
		t.Errorf("help text does not contain --run; got:\n%s", errOut)
	}
}

func TestHelpText_DoesNotContainExistingArtifactFlag(t *testing.T) {
	// --existing-artifact was removed in Stage 5; it must not appear in help text.
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	if strings.Contains(errOut, "existing-artifact") {
		t.Errorf("help text still contains 'existing-artifact' (must be removed); got:\n%s", errOut)
	}
}

func TestHelpText_DoesNotContainArtifactLocationFlag(t *testing.T) {
	// --artifact-location was removed in Stage 5; it must not appear in help text.
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	if strings.Contains(errOut, "artifact-location") {
		t.Errorf("help text still contains 'artifact-location' (must be removed); got:\n%s", errOut)
	}
}

// ---- tests: --run targeting a completed run ----

func TestRunFlag_TargetingCompletedRun_IsRejected(t *testing.T) {
	// --run with a run_id whose artifact has phase==COMPLETED must be rejected.
	// The CLI must not call session.Start for a completed run.
	rootDir := t.TempDir()
	writeCompletedRunArtifact(t, rootDir, testRunID)

	// Change to rootDir so the CLI can find the run folder.
	// Note: this changes the process working directory for this test.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--run", testRunID,
	}, sess)

	if code != cli.ExitUsage && code != cli.ExitRefused {
		t.Errorf("exit code = %d, want ExitUsage or ExitRefused for completed run", code)
	}
	if !strings.Contains(errOut, "completed") && !strings.Contains(errOut, testRunID) {
		t.Errorf("stderr %q does not mention 'completed' or the run_id %q", errOut, testRunID)
	}
	if sess.called {
		t.Error("session.Start must not be called for a completed run")
	}
}

func TestRunFlag_InvalidFormat_IsRejected(t *testing.T) {
	// --run with a run_id that does not match the expected format must be rejected.
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--run", "not-a-valid-run-id",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage for invalid run_id format", code)
	}
	_ = errOut // error message content not asserted (allow flexibility in wording)
	if sess.called {
		t.Error("session.Start must not be called for an invalid run_id")
	}
}

func TestRunFlag_NonExistentRun_IsRejected(t *testing.T) {
	// --run <run_id> whose folder does not exist on disk must be rejected.
	// The CLI must not call session.Start when the specified run folder is absent.
	rootDir := t.TempDir()
	// No run folder created — the folder for testRunID is absent.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--run", testRunID,
	}, sess)

	if code != cli.ExitUsage && code != cli.ExitRefused {
		t.Errorf("exit code = %d, want ExitUsage or ExitRefused for non-existent run_id", code)
	}
	if !strings.Contains(errOut, testRunID) {
		t.Errorf("stderr %q does not contain the run_id %q", errOut, testRunID)
	}
	if sess.called {
		t.Error("session.Start must not be called when the run folder is absent")
	}
}

// ---- tests: default scan delegation (neither --run nor --new-run) ----

func TestDefaultPath_ZeroCandidates_StartsNewRun(t *testing.T) {
	// With no --run or --new-run and an empty working directory, the scanner
	// finds zero candidates. The CLI must mint a new run and call session.Start
	// with IsNewRun=true. Currently fails (RED): the implementation (I5.3) has
	// not added the default scan path yet, so IsNewRun defaults to false.
	rootDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, _ := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
	}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess for zero-candidate default path", code)
	}
	if !sess.called {
		t.Fatal("session.Start was not called; CLI must start a new run when no candidates exist")
	}
	if !sess.config.IsNewRun {
		t.Error("IsNewRun = false, want true when no resumable candidates exist (new run minted)")
	}
}

func TestDefaultPath_OneCandidateWithNoFlag_AutoResumes(t *testing.T) {
	// With no --run or --new-run and exactly one resumable candidate in the
	// working directory, the CLI must auto-resume that candidate without user
	// interaction. Currently fails (RED): I5.3 has not added the default scan
	// path yet, so the CLI does not populate RunID from the scanned candidate.
	rootDir := t.TempDir()
	writeResumableRunArtifact(t, rootDir, cliRunID1)
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, _ := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
	}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess for single-candidate auto-resume", code)
	}
	if !sess.called {
		t.Fatal("session.Start was not called; CLI must auto-resume the single candidate")
	}
	if sess.config.IsNewRun {
		t.Error("IsNewRun = true, want false when auto-resuming the single candidate")
	}
	if sess.config.RunID != cliRunID1 {
		t.Errorf("RunID = %q, want %q (the auto-resumed candidate's run_id)", sess.config.RunID, cliRunID1)
	}
}

func TestDefaultPath_MultipleCandidates_CLIRejectsWithRunIDList(t *testing.T) {
	// With no --run or --new-run and multiple resumable candidates in the
	// working directory, the CLI cannot resolve ambiguity in non-interactive
	// mode. It must reject with ExitUsage and list the candidate run_ids in
	// stderr. Currently fails (RED): I5.3 has not added the default scan path.
	rootDir := t.TempDir()
	writeResumableRunArtifact(t, rootDir, cliRunID1)
	writeResumableRunArtifact(t, rootDir, cliRunID2)
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage when multiple candidates exist and no --run flag", code)
	}
	// stderr must identify the candidate run_ids so the user knows which to pass to --run.
	if !strings.Contains(errOut, cliRunID1) {
		t.Errorf("stderr %q does not contain candidate run_id %q", errOut, cliRunID1)
	}
	if !strings.Contains(errOut, cliRunID2) {
		t.Errorf("stderr %q does not contain candidate run_id %q", errOut, cliRunID2)
	}
	if sess.called {
		t.Error("session.Start must not be called when multiple candidates exist and no --run flag")
	}
}

// ============================================================
// T5.3: COMPLETED marker persistence tests
// ============================================================
//
// These tests verify that the CLI writes "COMPLETED" to current_state.phase
// via store.SetPhase when the session returns RunCompleted, and does NOT call
// SetPhase for other terminal statuses.
//
// The positive test (RunCompleted → SetPhase called) is RED: the current CLI
// does not call SetPhase.
//
// The negative tests (other statuses → SetPhase not called) are trivially GREEN
// with the current implementation (CLI never calls SetPhase) and serve as
// regression guards once the feature is implemented.

func TestCOMPLETEDMarker_WrittenWhenRunCompleted(t *testing.T) {
	// When session.Start returns RunCompleted, the CLI must call
	// store.SetPhase with phase="COMPLETED".
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{
			Status: domain.RunCompleted,
			Message: "run finished",
		},
	}
	code, _, _ := runCLIWithStore(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
	}, spy, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess", code)
	}
	if len(spy.setCalls) == 0 {
		t.Error("store.SetPhase was not called; want it called with phase=COMPLETED after RunCompleted")
	} else if spy.setCalls[0].phase != "COMPLETED" {
		t.Errorf("SetPhase called with phase=%q, want %q", spy.setCalls[0].phase, "COMPLETED")
	}
}

func TestCOMPLETEDMarker_NotWrittenWhenRunStopped(t *testing.T) {
	// When session.Start returns RunStopped, the CLI must NOT call SetPhase
	// (the run must remain resumable).
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{Status: domain.RunStopped},
	}
	runCLIWithStore(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
	}, spy, sess)

	if len(spy.setCalls) != 0 {
		t.Errorf("store.SetPhase called %d time(s), want 0 when status is RunStopped", len(spy.setCalls))
	}
}

func TestCOMPLETEDMarker_NotWrittenWhenRunDeviationUnresolved(t *testing.T) {
	// When session.Start returns RunDeviationUnresolved, the CLI must NOT call SetPhase.
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{Status: domain.RunDeviationUnresolved},
	}
	runCLIWithStore(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
	}, spy, sess)

	if len(spy.setCalls) != 0 {
		t.Errorf("store.SetPhase called %d time(s), want 0 when status is RunDeviationUnresolved", len(spy.setCalls))
	}
}

func TestCOMPLETEDMarker_NotWrittenWhenRunRefused(t *testing.T) {
	// When session.Start returns RunRefused, the CLI must NOT call SetPhase.
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{Status: domain.RunRefused},
	}
	runCLIWithStore(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
	}, spy, sess)

	if len(spy.setCalls) != 0 {
		t.Errorf("store.SetPhase called %d time(s), want 0 when status is RunRefused", len(spy.setCalls))
	}
}

func TestCOMPLETEDMarker_NotWrittenWhenRunFailed(t *testing.T) {
	// When session.Start returns RunFailed, the CLI must NOT call SetPhase.
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{Status: domain.RunFailed},
	}
	runCLIWithStore(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
	}, spy, sess)

	if len(spy.setCalls) != 0 {
		t.Errorf("store.SetPhase called %d time(s), want 0 when status is RunFailed", len(spy.setCalls))
	}
}

// ============================================================
// T3.1: CLI harness flag tests
// ============================================================

// baseHarnessArgs returns the minimum required flags for the run subcommand.
func baseHarnessArgs() []string {
	return []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--new-run",
	}
}

// TestHarnessFlag_FakeAccepted verifies that --harness fake is accepted and the
// session is started normally.
func TestHarnessFlag_FakeAccepted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := append(baseHarnessArgs(), "--harness", "fake")
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); stderr: %q", code, cli.ExitSuccess, errOut)
	}
	if !sess.called {
		t.Error("session.Start was not called for --harness fake")
	}
}

// TestHarnessFlag_ClaudeCodeAccepted verifies that --harness claude-code is accepted
// and the session is started normally.
func TestHarnessFlag_ClaudeCodeAccepted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := append(baseHarnessArgs(), "--harness", "claude-code")
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); stderr: %q", code, cli.ExitSuccess, errOut)
	}
	if !sess.called {
		t.Error("session.Start was not called for --harness claude-code")
	}
}

// TestHarnessFlag_UnknownRejectsWithUsageError verifies that --harness with an
// unknown value produces ExitUsage and an actionable error message that names
// both the invalid value and the valid alternatives, satisfying AC3.8.
func TestHarnessFlag_UnknownRejectsWithUsageError(t *testing.T) {
	sess := &scriptedSession{}
	args := append(baseHarnessArgs(), "--harness", "invalid-harness")
	code, _, errOut := runCLI(t, args, sess)
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for unknown --harness value", code, cli.ExitUsage)
	}
	// Error must name the invalid value so the user can see what they typed.
	if !strings.Contains(errOut, "invalid-harness") {
		t.Errorf("stderr %q should mention the invalid value %q", errOut, "invalid-harness")
	}
	// Error must also name the valid alternatives so the user knows how to fix it.
	if !strings.Contains(errOut, "fake") {
		t.Errorf("stderr %q should mention valid value %q so the error is actionable", errOut, "fake")
	}
	if !strings.Contains(errOut, "claude-code") {
		t.Errorf("stderr %q should mention valid value %q so the error is actionable", errOut, "claude-code")
	}
	if sess.called {
		t.Error("session.Start should not be called for invalid --harness value")
	}
}

// TestTimeoutFlag_ValidDurationAccepted verifies that --timeout with a parseable
// duration string is accepted and the session is started normally.
func TestTimeoutFlag_ValidDurationAccepted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := append(baseHarnessArgs(), "--timeout", "45m")
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); stderr: %q", code, cli.ExitSuccess, errOut)
	}
	if !sess.called {
		t.Error("session.Start was not called for valid --timeout value")
	}
}

// TestTimeoutFlag_InvalidDurationRejectsWithUsageError verifies that --timeout with
// an unparseable duration string produces ExitUsage.
func TestTimeoutFlag_InvalidDurationRejectsWithUsageError(t *testing.T) {
	sess := &scriptedSession{}
	args := append(baseHarnessArgs(), "--timeout", "not-a-duration")
	code, _, errOut := runCLI(t, args, sess)
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for invalid --timeout value", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "not-a-duration") {
		t.Errorf("stderr %q should mention the invalid value", errOut)
	}
	if sess.called {
		t.Error("session.Start should not be called for invalid --timeout value")
	}
}

// TestClaudePathFlag_Accepted verifies that --claude-path is accepted with any
// non-empty string value and the session is started normally.
func TestClaudePathFlag_Accepted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := append(baseHarnessArgs(), "--claude-path", "/usr/local/bin/claude")
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); stderr: %q", code, cli.ExitSuccess, errOut)
	}
	if !sess.called {
		t.Error("session.Start was not called when --claude-path is provided")
	}
}

// ============================================================
// T7.1 (CLI): --infra-class flag tests
// ============================================================
//
// These tests specify the --infra-class flag behaviour for non-interactive
// agent-per-class selection (AC7.3). They compile and FAIL in the RED phase
// because the flag is not yet recognised by the CLI (I7.3 not implemented):
// cobra rejects --infra-class as an unknown flag, returning ExitUsage.
//
// In GREEN (after I7.3): the flag is accepted, parsed into
// RunConfig.InfraClassSelections, and session.Start is called with the
// populated map.

// TestInfraClassFlag_SingleMapping_ParsedIntoRunConfig verifies that a single
// key=value pair supplied via --infra-class is parsed into
// RunConfig.InfraClassSelections with the correct class name and agent name.
func TestInfraClassFlag_SingleMapping_ParsedIntoRunConfig(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := append(baseHarnessArgs(),
		"--infra-class", "checkpoint=checkpoint-manager-git",
	)
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}

	got := sess.config.InfraClassSelections
	if got == nil {
		t.Fatal("InfraClassSelections is nil; want a non-nil map populated from --infra-class")
	}
	if got["checkpoint"] != "checkpoint-manager-git" {
		t.Errorf("InfraClassSelections[checkpoint] = %q, want %q",
			got["checkpoint"], "checkpoint-manager-git")
	}
}

// TestInfraClassFlag_MultipleClassMappings_AllParsed verifies that when multiple
// class=agent pairs are provided (either comma-separated in one flag or via multiple
// --infra-class flags), all pairs are present in RunConfig.InfraClassSelections.
func TestInfraClassFlag_MultipleClassMappings_AllParsed(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	// Comma-separated format: checkpoint=name1,commit=name2
	args := append(baseHarnessArgs(),
		"--infra-class", "checkpoint=checkpoint-manager-git,commit=commit-manager-git",
	)
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}

	got := sess.config.InfraClassSelections
	if got == nil {
		t.Fatal("InfraClassSelections is nil; want non-nil map with both class mappings")
	}
	if got["checkpoint"] != "checkpoint-manager-git" {
		t.Errorf("InfraClassSelections[checkpoint] = %q, want %q",
			got["checkpoint"], "checkpoint-manager-git")
	}
	if got["commit"] != "commit-manager-git" {
		t.Errorf("InfraClassSelections[commit] = %q, want %q",
			got["commit"], "commit-manager-git")
	}
}

// TestInfraClassFlag_MalformedValue_RejectsWithUsageError verifies that an
// --infra-class value that is not in "class=agent" format (no "=" separator)
// causes the CLI to reject with ExitUsage and not call session.Start.
func TestInfraClassFlag_MalformedValue_RejectsWithUsageError(t *testing.T) {
	sess := &scriptedSession{}
	args := append(baseHarnessArgs(),
		"--infra-class", "not-a-valid-pair",
	)
	code, _, _ := runCLI(t, args, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for malformed --infra-class value",
			code, cli.ExitUsage)
	}
	if sess.called {
		t.Error("session.Start must not be called when --infra-class value is malformed")
	}
}

// TestInfraClassFlag_HelpTextMentionsFlag verifies that the --infra-class flag
// appears in the run subcommand's help text after implementation.
func TestInfraClassFlag_HelpTextMentionsFlag(t *testing.T) {
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	if !strings.Contains(errOut, "--infra-class") {
		t.Errorf("help text does not mention --infra-class; got:\n%s", errOut)
	}
}

// ============================================================
// T2.1: --input flag collection tests
// ============================================================
//
// These tests specify the repeatable --input flag behaviour: each occurrence
// appends one verbatim path element to RunConfig.SeedInputs, preserving order
// and exact string content (including paths containing spaces and commas).
//
// Tests that assert on SeedInputs values depend on domain.RunConfig.SeedInputs
// existing (I2.1) and the --input flag being registered (I2.2). Until those
// tasks are complete these tests fail: at compile time if SeedInputs is absent,
// and at runtime (unknown-flag ExitUsage) if the flag is not registered.

// TestInputFlag_ZeroOccurrences_SeedInputsIsNil verifies that omitting --input
// entirely leaves RunConfig.SeedInputs nil. This is a baseline / regression guard:
// runs without any seed input must behave exactly as before.
func TestInputFlag_ZeroOccurrences_SeedInputsIsNil(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, _ := runCLIWithStore(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--new-run",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess", code)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.SeedInputs != nil {
		t.Errorf("SeedInputs = %v, want nil when --input is not supplied", sess.config.SeedInputs)
	}
}

// TestInputFlag_OneOccurrence_SingleValueReachesSeedInputs verifies that a single
// --input occurrence produces a one-element SeedInputs slice containing the
// supplied path verbatim.
func TestInputFlag_OneOccurrence_SingleValueReachesSeedInputs(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--new-run",
		"--input", "/path/to/seed.md",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if len(sess.config.SeedInputs) != 1 {
		t.Fatalf("len(SeedInputs) = %d, want 1; got %v", len(sess.config.SeedInputs), sess.config.SeedInputs)
	}
	if sess.config.SeedInputs[0] != "/path/to/seed.md" {
		t.Errorf("SeedInputs[0] = %q, want %q", sess.config.SeedInputs[0], "/path/to/seed.md")
	}
}

// TestInputFlag_MultipleOccurrences_AllValuesReachSeedInputsInOrder verifies that
// repeated --input occurrences append all paths to SeedInputs in the order given.
func TestInputFlag_MultipleOccurrences_AllValuesReachSeedInputsInOrder(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--new-run",
		"--input", "/alpha/first.md",
		"--input", "/beta/second.md",
		"--input", "/gamma/third.md",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	want := []string{"/alpha/first.md", "/beta/second.md", "/gamma/third.md"}
	if len(sess.config.SeedInputs) != len(want) {
		t.Fatalf("len(SeedInputs) = %d, want %d; got %v",
			len(sess.config.SeedInputs), len(want), sess.config.SeedInputs)
	}
	for i, w := range want {
		if sess.config.SeedInputs[i] != w {
			t.Errorf("SeedInputs[%d] = %q, want %q", i, sess.config.SeedInputs[i], w)
		}
	}
}

// TestInputFlag_PathWithSpaces_PreservedVerbatim verifies that a path containing
// spaces is stored as a single element with the exact original content.
func TestInputFlag_PathWithSpaces_PreservedVerbatim(t *testing.T) {
	const pathWithSpaces = "/path/with spaces/seed file.md"
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--new-run",
		"--input", pathWithSpaces,
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if len(sess.config.SeedInputs) != 1 || sess.config.SeedInputs[0] != pathWithSpaces {
		t.Errorf("SeedInputs = %v, want [%q]; path with spaces must be preserved verbatim",
			sess.config.SeedInputs, pathWithSpaces)
	}
}

// TestInputFlag_PathWithComma_PreservedVerbatim verifies that a path containing a
// comma is stored as a single element. A delimiter-split flag shape (like the
// --infra-class flag) would break such a path into two elements; StringArrayVar
// is required to prevent this.
func TestInputFlag_PathWithComma_PreservedVerbatim(t *testing.T) {
	const pathWithComma = "/path/with,comma/seed.md"
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--new-run",
		"--input", pathWithComma,
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if len(sess.config.SeedInputs) != 1 || sess.config.SeedInputs[0] != pathWithComma {
		t.Errorf("SeedInputs = %v, want [%q]; comma in path must not split the element",
			sess.config.SeedInputs, pathWithComma)
	}
}

// TestInputFlag_HelpTextMentionsFlag verifies that the run subcommand's help text
// names --input after the flag is registered (I2.2).
func TestInputFlag_HelpTextMentionsFlag(t *testing.T) {
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	if !strings.Contains(errOut, "--input") {
		t.Errorf("help text does not mention --input; got:\n%s", errOut)
	}
}

// ============================================================
// T2.2: --input and --run mutual-exclusion tests (CLI layer)
// ============================================================
//
// These tests verify that combining --input with --run is refused at ExitUsage
// with a message naming both flags. The check must fire before any run-folder
// access, so no run folder setup is required.
//
// In RED phase --input is not yet a recognised flag (I2.2 not done), so cobra
// returns ExitUsage with "unknown flag: --input" — the "mutually exclusive"
// substring check then fails, keeping the tests red. Both I2.2 (flag
// registration) and I2.3 (mutual-exclusion check) must be complete for these
// tests to pass.

// TestInputAndRunFlags_MutuallyExclusive_RejectsWithUsageError verifies that
// supplying both --input and --run yields ExitUsage with a message naming the
// mutual exclusion, and that session.Start is never called.
func TestInputAndRunFlags_MutuallyExclusive_RejectsWithUsageError(t *testing.T) {
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--input", "/some/seed.md",
		"--run", testRunID,
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) when --input and --run are both supplied",
			code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Errorf("stderr %q does not contain \"mutually exclusive\"; the error must name the conflict",
			errOut)
	}
	if sess.called {
		t.Error("session.Start must not be called when --input and --run are both supplied")
	}
}

// TestInputAndRunFlags_MutuallyExclusive_ErrorNamesInputFlag verifies that the
// mutual-exclusion error explicitly names --input so the user can identify which
// flags are in conflict.
func TestInputAndRunFlags_MutuallyExclusive_ErrorNamesInputFlag(t *testing.T) {
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--input", "/some/seed.md",
		"--run", testRunID,
	}, sess)

	// Both conditions must hold: the error must be a mutual-exclusion message AND
	// it must name --input. Checking "mutually exclusive" prevents this test from
	// passing coincidentally when --input is not yet registered and cobra emits
	// "unknown flag: --input" (which happens to contain the --input substring but
	// is not the intended mutual-exclusion error).
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Errorf("stderr %q does not contain \"mutually exclusive\"; the error must be the mutual-exclusion message, not cobra's unknown-flag rejection", errOut)
	}
	if !strings.Contains(errOut, "--input") {
		t.Errorf("stderr %q does not name --input in the mutual-exclusion error", errOut)
	}
}

// TestInputAndRunFlags_MutuallyExclusive_ErrorNamesRunFlag verifies that the
// mutual-exclusion error explicitly names --run so the user can identify which
// flags are in conflict.
func TestInputAndRunFlags_MutuallyExclusive_ErrorNamesRunFlag(t *testing.T) {
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--input", "/some/seed.md",
		"--run", testRunID,
	}, sess)

	if !strings.Contains(errOut, "--run") {
		t.Errorf("stderr %q does not name --run in the mutual-exclusion error", errOut)
	}
}
