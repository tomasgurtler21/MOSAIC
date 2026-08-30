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

	"github.com/spf13/pflag"

	commonharness "mosaic-common/harness"
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
			name:    "missing workflow",
			args:    []string{"run", "--task", "do work"},
			wantErr: "--workflow",
		},
		{
			name:    "missing task",
			args:    []string{"run", "--workflow", "w1"},
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
		"--workflow", "my-workflow",
		"--task", "do the work",
		"--mode", "auto",
		"--new-run",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}

	cfg := sess.config
	// HarnessID must default to "fake" (the default --harness value).
	if cfg.HarnessID != "fake" {
		t.Errorf("HarnessID = %q, want %q (default harness)", cfg.HarnessID, "fake")
	}
	if string(cfg.WorkflowID) != "my-workflow" {
		t.Errorf("WorkflowID = %q, want %q", cfg.WorkflowID, "my-workflow")
	}
	if cfg.Task != "do the work" {
		t.Errorf("Task = %q, want %q", cfg.Task, "do the work")
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
		"--workflow", "greenfield-tdd",
		"--task", "build the feature",
		"--allow-version-drift",
		"--checkpoints", "enabled",
		"--mode", "auto",
		"--new-run",
	}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}

	cfg := sess.config
	if string(cfg.WorkflowID) != "greenfield-tdd" {
		t.Errorf("WorkflowID = %q, want %q", cfg.WorkflowID, "greenfield-tdd")
	}
	if cfg.Task != "build the feature" {
		t.Errorf("Task = %q, want %q", cfg.Task, "build the feature")
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
		"--workflow", "w1",
		"--task", "t1",
		"--mode", "auto",
		"--new-run",
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
		{domain.RunStoppedByConsultant, cli.ExitStoppedByConsultant},
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
		{"unknown --on-deviation flag", "--on-deviation", "invalid-value"},
		{"invalid --checkpoints", "--checkpoints", "invalid-value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &scriptedSession{}
			code, _, _ := runCLI(t, []string{
				"run",
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

		"--workflow", "w1",
		"--task", "t1",
		"--mode", "auto",
		"--new-run",
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
		"ExitStoppedByConsultant": cli.ExitStoppedByConsultant,
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

func (s *spyStore) Create(_ context.Context, _ domain.WorkflowInfo, _ string, _ domain.RunSettings, _ time.Time, _ string) (domain.ArtifactState, error) {
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

<ExecutionLog type="core">
| Seq | Agent   | Phase     | Stage | Status  | Timestamp            | Summary | Checkpoint |
| --- | ------- | --------- | ----- | ------- | -------------------- | ------- | ---------- |
| 1   | agent#1 | EXECUTION | -     | SUCCESS | 2026-01-01T00:00:00Z | done    | -          |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
</Artifacts>
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

<ExecutionLog type="core">
| Seq | Agent   | Phase     | Stage | Status  | Timestamp            | Summary | Checkpoint |
| --- | ------- | --------- | ----- | ------- | -------------------- | ------- | ---------- |
| 1   | agent#1 | EXECUTION | -     | SUCCESS | 2026-01-01T00:00:00Z | done    | -          |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
</Artifacts>
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

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
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

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
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

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
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

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
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

// TestOrchestratorFileFlag_IsNoLongerRecognized verifies that --orchestrator-file
// is not recognised by the run subcommand after Stage 3 removal. cobra must
// reject it as an unknown flag (ExitUsage), since orchestrator discovery is now
// automatic from the harness's agents directory.
func TestOrchestratorFileFlag_IsNoLongerRecognized(t *testing.T) {
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",
		"--workflow", "w1",
		"--task", "do work",
		"--orchestrator-file", "orch.md",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("--orchestrator-file: exit code = %d, want ExitUsage (%d) (flag must be removed)",
			code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "unknown flag") && !strings.Contains(errOut, "orchestrator-file") {
		t.Errorf("stderr %q does not indicate that --orchestrator-file is unknown", errOut)
	}
	if sess.called {
		t.Error("session.Start must not be called when an unknown flag is supplied")
	}
}

// TestHarnessID_InRunConfig verifies that the --harness flag value is propagated
// to RunConfig.HarnessID so the session layer can use it for discovery and
// snapshot creation.
func TestHarnessID_InRunConfig(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",
		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--harness", "fake",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.HarnessID != "fake" {
		t.Errorf("HarnessID = %q, want %q", sess.config.HarnessID, "fake")
	}
}

func TestArtifactLocationFlag_IsNoLongerRecognized(t *testing.T) {
	// --artifact-location was removed in Stage 5; cobra must reject it as unknown.
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLI(t, []string{
		"run",

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

// TestDefaultPath_ZeroCandidates_Refuses verifies the removal of the
// zero-candidate auto-start defect (AC2.6). With neither --run nor --new-run
// and an empty working directory, the CLI must refuse rather than silently
// minting a new run: the number of runs present never changes whether the
// question is asked, and starting a new run must be stated as an explicit
// choice (--new-run), not inferred from an empty workspace.
//
// Currently fails (RED): the CLI still auto-starts a new run for zero
// candidates (the defect this stage removes).
func TestDefaultPath_ZeroCandidates_Refuses(t *testing.T) {
	rootDir := t.TempDir()
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

		"--workflow", "w1",
		"--task", "do work",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) when no run exists and no selection flag was given", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "--new-run") {
		t.Errorf("stderr %q does not mention --new-run as the way to start a new run", errOut)
	}
	if sess.called {
		t.Error("session.Start must not be called when selection is unresolved (zero candidates, no flags)")
	}
}

// TestDefaultPath_OneCandidateWithNoFlag_Refuses is the CLI-side expression
// of the core defect this stage removes (AC2.2): a workspace with exactly one
// resumable run must no longer be resumed silently. With neither --run nor
// --new-run, the CLI must refuse and name the candidate so the caller can
// pass --run explicitly, or --new-run to start fresh.
//
// Currently fails (RED): the CLI still auto-resumes the single candidate.
func TestDefaultPath_OneCandidateWithNoFlag_Refuses(t *testing.T) {
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

	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) when exactly one resumable run exists and no selection flag was given", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, cliRunID1) {
		t.Errorf("stderr %q does not name the single candidate %q", errOut, cliRunID1)
	}
	if !strings.Contains(errOut, "--new-run") {
		t.Errorf("stderr %q does not mention --new-run as an available choice", errOut)
	}
	if sess.called {
		t.Error("session.Start must not be called when a single candidate exists and no selection flag was given")
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

// TestDefaultPath_MultipleCandidates_RefusalMentionsNewRunOption verifies
// AC2.3: whatever the workspace contains, starting a new run is an available
// outcome, so the multi-candidate refusal must also mention --new-run, not
// just the existing candidates.
func TestDefaultPath_MultipleCandidates_RefusalMentionsNewRunOption(t *testing.T) {
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

		"--workflow", "w1",
		"--task", "do work",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "--new-run") {
		t.Errorf("stderr %q does not mention --new-run; starting a new run must remain an available choice", errOut)
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

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
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

		"--workflow", "w1",
		"--task", "do work",
	}, spy, sess)

	if len(spy.setCalls) != 0 {
		t.Errorf("store.SetPhase called %d time(s), want 0 when status is RunFailed", len(spy.setCalls))
	}
}

func TestCOMPLETEDMarker_NotWrittenWhenRunStoppedByConsultant(t *testing.T) {
	// When session.Start returns RunStoppedByConsultant, the CLI must NOT call
	// SetPhase (the run must remain resumable — AC7.5). This test guards against
	// an implementation that inadvertently writes the COMPLETED marker for the
	// new stop outcome, which would silently break resumability.
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{
			Status:     domain.RunStoppedByConsultant,
			StopReason: "consultant decided to halt the run",
		},
	}
	runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
	}, spy, sess)

	if len(spy.setCalls) != 0 {
		t.Errorf("store.SetPhase called %d time(s), want 0 when status is RunStoppedByConsultant", len(spy.setCalls))
	}
}

// ============================================================
// T3.1: CLI harness flag tests
// ============================================================

// baseHarnessArgs returns the minimum required flags for the run subcommand.
// --mode auto is included because mode is required; tests that exercise mode
// behaviour specifically should not use this helper.
func baseHarnessArgs() []string {
	return []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
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

// setupCLIHarnessWorkDir creates a temp working directory with the orchestrator
// file at the path expected by the given harness, changes the process working
// directory to it, and registers a cleanup to restore the original directory.
// Must be called from a test function (not a goroutine).
func setupCLIHarnessWorkDir(t *testing.T, harnessID string) {
	t.Helper()
	entry, ok := commonharness.LookupCLIHarness(harnessID)
	if !ok {
		t.Skipf("setupCLIHarnessWorkDir: %q is not a CLI harness", harnessID)
		return
	}
	workDir := t.TempDir()
	agentsDirFull := filepath.Join(workDir, entry.AgentsDir)
	if err := os.MkdirAll(agentsDirFull, 0o755); err != nil {
		t.Fatalf("setupCLIHarnessWorkDir: mkdir %q: %v", agentsDirFull, err)
	}
	orchPath := filepath.Join(agentsDirFull, "orchestrator-script.md")
	if err := os.WriteFile(orchPath, []byte("# orchestrator\n"), 0o644); err != nil {
		t.Fatalf("setupCLIHarnessWorkDir: write %q: %v", orchPath, err)
	}
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("setupCLIHarnessWorkDir: getwd: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("setupCLIHarnessWorkDir: chdir %q: %v", workDir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
}

// TestHarnessFlag_ClaudeCodeAccepted verifies that --harness claude-code is accepted
// and the session is started normally.
func TestHarnessFlag_ClaudeCodeAccepted(t *testing.T) {
	setupCLIHarnessWorkDir(t, "claude-code")
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

// TestHarnessFlag_ClaudeCodeRefusedWhenOrchestratorFileAbsent verifies that the
// CLI refuses with a non-zero exit code when --harness claude-code is used but
// the expected orchestrator-script.md is absent from the working directory. The
// error message must name the harness ID or expected file path so the user can
// diagnose the problem (AC3.4, CLI integration side).
func TestHarnessFlag_ClaudeCodeRefusedWhenOrchestratorFileAbsent(t *testing.T) {
	// Use a clean temp directory — no agents directory or orchestrator file created.
	rootDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{}
	args := append(baseHarnessArgs(), "--harness", "claude-code")
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)

	if code != cli.ExitRefused && code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitRefused (%d) or ExitUsage (%d) when orchestrator file is absent",
			code, cli.ExitRefused, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "claude-code") && !strings.Contains(errOut, "orchestrator-script") {
		t.Errorf("stderr %q does not mention the harness ID or expected file; "+
			"error must be actionable", errOut)
	}
	if sess.called {
		t.Error("session.Start must not be called when the orchestrator file is absent")
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

// ---------------------------------------------------------------------------
// T4.4: catalog-driven flag surfaces
//
// These tests verify that --harness's usage string, its validation and its
// usage-error message are all derived from Runner's one accepted set
// (Selections/Accepts/FlagValues/FlagValueList in internal/harness), so a
// shared-catalog addition reaches all three without a restated literal.
// ---------------------------------------------------------------------------

// TestHarnessFlag_OpenCodeAccepted verifies that --harness opencode is
// accepted and the session is started normally, mirroring
// TestHarnessFlag_ClaudeCodeAccepted for the new catalog entry.
func TestHarnessFlag_OpenCodeAccepted(t *testing.T) {
	setupCLIHarnessWorkDir(t, "opencode")
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := append(baseHarnessArgs(), "--harness", "opencode")
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); stderr: %q", code, cli.ExitSuccess, errOut)
	}
	if !sess.called {
		t.Error("session.Start was not called for --harness opencode")
	}
}

// TestHarnessFlag_EveryCatalogEntryAccepted verifies that every harness the
// shared catalog declares passes --harness validation, so a future catalog
// addition is accepted here without an edit to this test or to run.go.
func TestHarnessFlag_EveryCatalogEntryAccepted(t *testing.T) {
	for _, entry := range commonharness.CLIHarnesses() {
		entry := entry
		t.Run(entry.ID, func(t *testing.T) {
			setupCLIHarnessWorkDir(t, entry.ID)
			sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
			args := append(baseHarnessArgs(), "--harness", entry.ID)
			code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
			if code != cli.ExitSuccess {
				t.Errorf("--harness %s: exit code = %d, want ExitSuccess (%d); stderr: %q", entry.ID, code, cli.ExitSuccess, errOut)
			}
			if !sess.called {
				t.Errorf("--harness %s: session.Start was not called", entry.ID)
			}
		})
	}
}

// TestHarnessFlag_UnknownStillRejectsWithUsageError_AfterOpenCodeAdded
// re-verifies AC3.8's negative half now that a second catalog entry exists:
// an unrecognised value must still be refused.
func TestHarnessFlag_UnknownStillRejectsWithUsageError_AfterOpenCodeAdded(t *testing.T) {
	sess := &scriptedSession{}
	args := append(baseHarnessArgs(), "--harness", "still-not-a-harness")
	code, _, errOut := runCLI(t, args, sess)
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for unknown --harness value", code, cli.ExitUsage)
	}
	if sess.called {
		t.Error("session.Start should not be called for invalid --harness value")
	}
	_ = errOut
}

// TestHarnessFlag_UsageErrorMessageListsEveryAcceptedValue verifies that an
// unknown --harness value's usage-error message names every accepted value,
// including the new "opencode" catalog entry — not just the two that
// predate it (AC4.5).
func TestHarnessFlag_UsageErrorMessageListsEveryAcceptedValue(t *testing.T) {
	sess := &scriptedSession{}
	args := append(baseHarnessArgs(), "--harness", "still-not-a-harness")
	_, _, errOut := runCLI(t, args, sess)

	for _, want := range []string{"fake", "claude-code", "opencode"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("usage-error message %q does not mention accepted value %q", errOut, want)
		}
	}
}

// TestHarnessFlag_HelpUsageListsEveryAcceptedValue verifies that the flag's
// own usage string (shown by --help) names every accepted value, so a
// catalog addition reaches the usage text without a restated literal.
func TestHarnessFlag_HelpUsageListsEveryAcceptedValue(t *testing.T) {
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	for _, want := range []string{"fake", "claude-code", "opencode"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("--help usage text %q does not mention accepted value %q", errOut, want)
		}
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

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
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

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
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

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
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

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
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

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
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

		"--workflow", "w1",
		"--task", "do work",
		"--input", "/some/seed.md",
		"--run", testRunID,
	}, sess)

	if !strings.Contains(errOut, "--run") {
		t.Errorf("stderr %q does not name --run in the mutual-exclusion error", errOut)
	}
}

// ============================================================
// T2.3: chosen-run announcement tests
// ============================================================
//
// AC2.7 requires that the tool states which run it is about to perform,
// whether new or resumed, and for a resumed run its recorded position,
// before any dispatch. These tests assert on stdout content, not exact
// wording (per runselect.Announce's contract: content is fixed, phrasing is
// not).
//
// Currently fail (RED): the CLI does not yet call runselect.Announce before
// starting the session.

// TestAnnouncement_NewRun_StatedBeforeDispatch verifies that starting a new
// run via --new-run writes an announcement to stdout, before the session
// outcome, naming the resolved run_id and stating that the run is new.
func TestAnnouncement_NewRun_StatedBeforeDispatch(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, stdout, errOut := runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.RunID == "" {
		t.Fatal("sess.config.RunID is empty; cannot verify announcement without a resolved run_id")
	}
	if !strings.Contains(stdout, sess.config.RunID) {
		t.Errorf("stdout %q does not contain the resolved run_id %q; the chosen run must be announced before dispatch", stdout, sess.config.RunID)
	}
	if !strings.Contains(strings.ToLower(stdout), "new") {
		t.Errorf("stdout %q does not state that the run is new", stdout)
	}
}

// TestAnnouncement_ResumedRun_ContainsPosition verifies that resuming a run
// via --run writes an announcement to stdout naming the run_id, stating that
// it is resumed, and reporting its recorded phase.
func TestAnnouncement_ResumedRun_ContainsPosition(t *testing.T) {
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
	code, stdout, errOut := runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--run", testRunID,
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if !strings.Contains(stdout, testRunID) {
		t.Errorf("stdout %q does not contain the resumed run_id %q", stdout, testRunID)
	}
	if !strings.Contains(strings.ToLower(stdout), "resum") {
		t.Errorf("stdout %q does not state that the run is resumed", stdout)
	}
	// writeResumableRunArtifact records current_state.phase: EXECUTION.
	if !strings.Contains(stdout, "EXECUTION") {
		t.Errorf("stdout %q does not contain the recorded phase %q", stdout, "EXECUTION")
	}
}

// ============================================================
// T7.1: Flag parsing and validation for the Stage 7 CLI surface
// ============================================================
//
// Tests for the new --mode, --commits, --commit-branch, --pre-consult, and
// --manual-resolution flags, and for the removal of --on-deviation.
//
// All tests in this section are in the TDD RED phase: they compile but fail
// because the implementation (I7.1) has not been completed yet.

// newStage7BaseArgs returns the minimum required flags for a successful run
// command invocation, explicitly including --mode. Callers that test mode
// behaviour directly should not use this helper.
func newStage7BaseArgs() []string {
	return []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
	}
}

// --- T7.1: --mode flag ---

// TestModeFlag_Absent_ProducesRefusal verifies that omitting --mode produces an
// error that names the flag and lists the valid values (AC7.3). The session must
// not be started.
func TestModeFlag_Absent_ProducesRefusal(t *testing.T) {
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--new-run",
	}, sess)
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage when --mode is absent", code)
	}
	if sess.called {
		t.Error("session.Start must not be called when --mode is absent")
	}
	if !strings.Contains(errOut, "mode") {
		t.Errorf("stderr %q does not mention \"mode\"", errOut)
	}
	// Valid values must be listed so the error is actionable.
	for _, m := range domain.ExecutionModes() {
		if !strings.Contains(errOut, string(m)) {
			t.Errorf("stderr %q does not list valid mode value %q", errOut, m)
		}
	}
}

// TestModeFlag_ValidValues_AreAccepted verifies that each of the three valid
// mode strings is accepted without error and causes the session to start.
func TestModeFlag_ValidValues_AreAccepted(t *testing.T) {
	validModes := []string{"orchestrated", "auto", "auto-review"}
	for _, mode := range validModes {
		t.Run(mode, func(t *testing.T) {
			sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
			args := []string{
				"run",
		
				"--workflow", "w1",
				"--task", "do work",
				"--mode", mode,
				"--new-run",
			}
			code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
			if code != cli.ExitSuccess {
				t.Errorf("mode=%q: exit code = %d, want ExitSuccess; stderr: %q", mode, code, errOut)
			}
			if !sess.called {
				t.Errorf("mode=%q: session.Start was not called", mode)
			}
		})
	}
}

// TestModeFlag_UnrecognisedValue_ProducesRefusal verifies that an unrecognised
// --mode value produces an error that names both the offending value and the
// valid alternatives. The session must not be started.
func TestModeFlag_UnrecognisedValue_ProducesRefusal(t *testing.T) {
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "quick",
		"--new-run",
	}, sess)
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage for unrecognised --mode value", code)
	}
	if sess.called {
		t.Error("session.Start must not be called for an unrecognised --mode value")
	}
	if !strings.Contains(errOut, "quick") {
		t.Errorf("stderr %q does not name the offending value %q", errOut, "quick")
	}
	// All valid values must be listed.
	for _, m := range domain.ExecutionModes() {
		if !strings.Contains(errOut, string(m)) {
			t.Errorf("stderr %q does not list valid mode value %q", errOut, m)
		}
	}
}

// --- T7.1: --commits flag ---

// TestCommitsFlag_ValidValues_AreAccepted verifies that "disabled" and "enabled"
// are both accepted without error and cause the session to start.
func TestCommitsFlag_ValidValues_AreAccepted(t *testing.T) {
	for _, v := range []string{"disabled", "enabled"} {
		t.Run(v, func(t *testing.T) {
			sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
			args := append(newStage7BaseArgs(), "--commits", v)
			code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
			if code != cli.ExitSuccess {
				t.Errorf("--commits=%q: exit code = %d, want ExitSuccess; stderr: %q", v, code, errOut)
			}
			if !sess.called {
				t.Errorf("--commits=%q: session.Start was not called", v)
			}
		})
	}
}

// TestCommitsFlag_InvalidValue_ProducesRefusal verifies that an unrecognised
// --commits value produces an error naming both the offending value and the
// valid alternatives. The session must not be started.
func TestCommitsFlag_InvalidValue_ProducesRefusal(t *testing.T) {
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, append(newStage7BaseArgs(), "--commits", "yes"), sess)
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage for invalid --commits value", code)
	}
	if sess.called {
		t.Error("session.Start must not be called for invalid --commits value")
	}
	if !strings.Contains(errOut, "commits") {
		t.Errorf("stderr %q does not name the --commits flag", errOut)
	}
	// The offending value must be named in the error.
	if !strings.Contains(errOut, "yes") {
		t.Errorf("stderr %q does not name the offending value %q", errOut, "yes")
	}
	// All valid values must be listed so the user knows what to pass.
	for _, v := range []string{"enabled", "disabled"} {
		if !strings.Contains(errOut, v) {
			t.Errorf("stderr %q does not list valid --commits value %q", errOut, v)
		}
	}
}

// --- T7.1: --commit-branch flag ---

// TestCommitBranchFlag_ValidValues_AreAccepted verifies that "mosaic-owned" and
// "user-own" are both accepted without error and cause the session to start.
func TestCommitBranchFlag_ValidValues_AreAccepted(t *testing.T) {
	for _, v := range []string{"mosaic-owned", "user-own"} {
		t.Run(v, func(t *testing.T) {
			sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
			args := append(newStage7BaseArgs(), "--commit-branch", v)
			code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
			if code != cli.ExitSuccess {
				t.Errorf("--commit-branch=%q: exit code = %d, want ExitSuccess; stderr: %q", v, code, errOut)
			}
			if !sess.called {
				t.Errorf("--commit-branch=%q: session.Start was not called", v)
			}
		})
	}
}

// TestCommitBranchFlag_InvalidValue_ProducesRefusal verifies that an unrecognised
// --commit-branch value produces an error naming the flag, the offending value,
// and all valid alternatives. The session must not be started.
func TestCommitBranchFlag_InvalidValue_ProducesRefusal(t *testing.T) {
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, append(newStage7BaseArgs(), "--commit-branch", "main"), sess)
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage for invalid --commit-branch value", code)
	}
	if sess.called {
		t.Error("session.Start must not be called for invalid --commit-branch value")
	}
	if !strings.Contains(errOut, "commit-branch") {
		t.Errorf("stderr %q does not name the --commit-branch flag", errOut)
	}
	if !strings.Contains(errOut, "main") {
		t.Errorf("stderr %q does not name the offending value %q", errOut, "main")
	}
	// All valid values must be listed, matching the --mode error message shape.
	for _, v := range domain.CommitBranchVariants() {
		if !strings.Contains(errOut, string(v)) {
			t.Errorf("stderr %q does not list valid --commit-branch value %q", errOut, v)
		}
	}
}

// --- T7.1: --pre-consult flag ---

// TestPreConsultFlag_IsAccepted verifies that --pre-consult is recognised as a
// boolean flag and does not cause a usage error.
func TestPreConsultFlag_IsAccepted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := append(newStage7BaseArgs(), "--pre-consult")
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess when --pre-consult is present; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Error("session.Start was not called when --pre-consult is present")
	}
}

// --- T7.1: --manual-resolution flag ---

// TestManualResolutionFlag_IsAccepted verifies that --manual-resolution is
// recognised as a boolean flag and does not cause a usage error.
func TestManualResolutionFlag_IsAccepted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := append(newStage7BaseArgs(), "--manual-resolution")
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess when --manual-resolution is present; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Error("session.Start was not called when --manual-resolution is present")
	}
}

// --- T7.1: help text ---

// TestHelpText_ContainsModeFlag verifies that --mode appears in the run
// subcommand's help text after implementation.
func TestHelpText_ContainsModeFlag(t *testing.T) {
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)
	if !strings.Contains(errOut, "--mode") {
		t.Errorf("help text does not mention --mode; got:\n%s", errOut)
	}
}

// TestHelpText_DoesNotContainOnDeviationFlag verifies that --on-deviation does
// not appear in the help text (AC7.4 — it is removed).
func TestHelpText_DoesNotContainOnDeviationFlag(t *testing.T) {
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)
	if strings.Contains(errOut, "on-deviation") {
		t.Errorf("help text still contains 'on-deviation' (must be removed); got:\n%s", errOut)
	}
}

// ============================================================
// T7.2: Parsed flags reach the session run configuration
// ============================================================

// TestModeFlag_Orchestrated_ReachesRunSettings verifies that --mode orchestrated
// sets RunSettings.Mode to ExecutionModeOrchestrated.
func TestModeFlag_Orchestrated_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "orchestrated",
		"--new-run",
	}
	_, _, _ = runCLI(t, args, sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.Mode != domain.ExecutionModeOrchestrated {
		t.Errorf("Mode = %q, want %q", sess.config.Mode, domain.ExecutionModeOrchestrated)
	}
}

// TestModeFlag_Auto_ReachesRunSettings verifies that --mode auto sets
// RunSettings.Mode to ExecutionModeAuto.
func TestModeFlag_Auto_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
	}
	_, _, _ = runCLI(t, args, sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.Mode != domain.ExecutionModeAuto {
		t.Errorf("Mode = %q, want %q", sess.config.Mode, domain.ExecutionModeAuto)
	}
}

// TestModeFlag_AutoReview_ReachesRunSettings verifies that --mode auto-review
// sets RunSettings.Mode to ExecutionModeAutoReview.
func TestModeFlag_AutoReview_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto-review",
		"--new-run",
	}
	_, _, _ = runCLI(t, args, sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.Mode != domain.ExecutionModeAutoReview {
		t.Errorf("Mode = %q, want %q", sess.config.Mode, domain.ExecutionModeAutoReview)
	}
}

// TestCommitsFlag_Enabled_ReachesRunSettings verifies that --commits enabled
// sets RunSettings.Commits to true.
func TestCommitsFlag_Enabled_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--commits", "enabled"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if !sess.config.Commits {
		t.Error("Commits = false, want true when --commits=enabled")
	}
}

// TestCommitsFlag_Disabled_ReachesRunSettings verifies that --commits disabled
// (or omitted, since disabled is the default) sets RunSettings.Commits to false.
func TestCommitsFlag_Disabled_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--commits", "disabled"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.Commits {
		t.Error("Commits = true, want false when --commits=disabled")
	}
}

// TestCommitBranchFlag_MOSAICOwned_ReachesRunSettings verifies that
// --commit-branch mosaic-owned sets RunSettings.CommitBranchVariant to
// CommitBranchMOSAICOwned.
func TestCommitBranchFlag_MOSAICOwned_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--commit-branch", "mosaic-owned"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.CommitBranchVariant != domain.CommitBranchMOSAICOwned {
		t.Errorf("CommitBranchVariant = %q, want %q",
			sess.config.CommitBranchVariant, domain.CommitBranchMOSAICOwned)
	}
}

// TestCommitBranchFlag_UserOwn_ReachesRunSettings verifies that
// --commit-branch user-own sets RunSettings.CommitBranchVariant to
// CommitBranchUserOwn.
func TestCommitBranchFlag_UserOwn_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--commit-branch", "user-own"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.CommitBranchVariant != domain.CommitBranchUserOwn {
		t.Errorf("CommitBranchVariant = %q, want %q",
			sess.config.CommitBranchVariant, domain.CommitBranchUserOwn)
	}
}

// TestCommitBranchFlag_DefaultIsMOSAICOwned_WhenOmittedAndCommitsEnabled verifies
// that omitting --commit-branch when --commits enabled leaves CommitBranchVariant
// at CommitBranchMOSAICOwned, the documented default for commits-enabled runs.
func TestCommitBranchFlag_DefaultIsMOSAICOwned_WhenOmittedAndCommitsEnabled(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--commits", "enabled"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.CommitBranchVariant != domain.CommitBranchMOSAICOwned {
		t.Errorf("CommitBranchVariant = %q, want default %q when --commits enabled and --commit-branch omitted",
			sess.config.CommitBranchVariant, domain.CommitBranchMOSAICOwned)
	}
}

// TestCommitBranchVariant_IsEmpty_WhenCommitsDisabledAndBranchOmitted verifies
// that when --commits is disabled (or omitted, since disabled is the default)
// and --commit-branch is not specified, RunSettings.CommitBranchVariant is
// the zero value (empty string), not mosaic-owned.
// RED: current implementation defaults to mosaic-owned regardless of --commits.
func TestCommitBranchVariant_IsEmpty_WhenCommitsDisabledAndBranchOmitted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--commits", "disabled"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.CommitBranchVariant != "" {
		t.Errorf("CommitBranchVariant = %q, want %q (zero value) when --commits disabled and --commit-branch omitted",
			sess.config.CommitBranchVariant, "")
	}
}

// TestPreConsultFlag_ReachesRunSettings verifies that --pre-consult sets
// RunSettings.PreConsultation to true.
func TestPreConsultFlag_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--pre-consult"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if !sess.config.PreConsultation {
		t.Error("PreConsultation = false, want true when --pre-consult is present")
	}
}

// TestPreConsultFlag_DefaultIsTrue_WhenOmitted verifies that omitting --pre-consult
// leaves RunSettings.PreConsultation enabled — pre-consultation is on by default
// so that automated runs are safe without explicit opt-in.
func TestPreConsultFlag_DefaultIsTrue_WhenOmitted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, newStage7BaseArgs(), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if !sess.config.PreConsultation {
		t.Error("PreConsultation = false, want true when --pre-consult is omitted; " +
			"pre-consultation must be enabled by default")
	}
}

// TestPreConsultFlag_ExplicitFalse_DisablesPreConsultation verifies that passing
// --pre-consult=false explicitly disables pre-consultation even though the flag
// defaults to enabled. The =false form is the only way users can opt out of the
// default, so it must be honoured by the cobra flag machinery.
//
// NOTE — conditional RED phase: with the current cobra default of false for
// --pre-consult, passing --pre-consult=false already produces PreConsultation =
// false, so this test passes before implementation. It only enters RED once I2.1
// flips the cobra default to true. The RED phase for this test must be
// re-verified after I2.1 lands, not before.
func TestPreConsultFlag_ExplicitFalse_DisablesPreConsultation(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--pre-consult=false"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.PreConsultation {
		t.Error("PreConsultation = true, want false when --pre-consult=false is passed; " +
			"the explicit-false form must override the default")
	}
}

// TestPreConsultFlag_ExplicitTrue_EnablesPreConsultation verifies that passing
// --pre-consult=true explicitly enables pre-consultation. The design behavioral
// contract table lists this invocation form as required to produce
// PreConsultation == true. Cobra handles this correctly for bool flags; this
// test closes the gap against the contract table for the =true form through the
// full CLI path into RunSettings.PreConsultation.
//
// REGRESSION GUARD — this test passes before I2.1 lands because --pre-consult=true
// produces PreConsultation = true regardless of the cobra default. It is not a
// TDD RED-phase driver; it pins the =true form's behavior so a future refactor
// cannot silently break explicit enabling.
func TestPreConsultFlag_ExplicitTrue_EnablesPreConsultation(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--pre-consult=true"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if !sess.config.PreConsultation {
		t.Error("PreConsultation = false, want true when --pre-consult=true is passed; " +
			"the explicit-true form must set PreConsultation enabled")
	}
}

// TestManualResolutionFlag_ReachesRunSettings verifies that --manual-resolution
// sets RunSettings.ManualResolution to true.
func TestManualResolutionFlag_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--manual-resolution"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if !sess.config.ManualResolution {
		t.Error("ManualResolution = false, want true when --manual-resolution is present")
	}
}

// TestManualResolutionFlag_DefaultIsFalse_WhenOmitted verifies that omitting
// --manual-resolution leaves RunSettings.ManualResolution as false.
func TestManualResolutionFlag_DefaultIsFalse_WhenOmitted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, newStage7BaseArgs(), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.ManualResolution {
		t.Error("ManualResolution = true, want false when --manual-resolution is omitted")
	}
}

// TestOnDeviationFlag_IsRejectedAsUnknown verifies that --on-deviation is
// rejected as an unknown flag (AC7.4). The flag was removed in Stage 7; cobra
// must surface it as an error and the session must not start.
func TestOnDeviationFlag_IsRejectedAsUnknown(t *testing.T) {
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--on-deviation", "stop",
	}, sess)
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage for removed --on-deviation flag", code)
	}
	if sess.called {
		t.Error("session.Start must not be called when --on-deviation is present")
	}
	// The error must indicate the flag is unknown or not recognised.
	if !strings.Contains(errOut, "on-deviation") {
		t.Errorf("stderr %q does not mention \"on-deviation\"", errOut)
	}
}

// ============================================================
// T7.3: Stop outcome exits non-zero with reason printed
// ============================================================

// TestRunStoppedByConsultant_ExitsWithDistinctNonZeroCode verifies that a
// RunStoppedByConsultant outcome maps to ExitStoppedByConsultant, which is a
// distinct non-zero exit code (AC7.5).
func TestRunStoppedByConsultant_ExitsWithDistinctNonZeroCode(t *testing.T) {
	const stopReason = "orchestrator decided to stop the run"
	sess := &scriptedSession{
		outcome: domain.RunOutcome{
			Status:     domain.RunStoppedByConsultant,
			Message:    "run stopped by consultant",
			StopReason: stopReason,
		},
	}
	code, _, _ := runCLI(t, newStage7BaseArgs(), sess)
	if code == cli.ExitSuccess {
		t.Error("exit code = 0 (ExitSuccess), want non-zero for RunStoppedByConsultant")
	}
	if code != cli.ExitStoppedByConsultant {
		t.Errorf("exit code = %d, want ExitStoppedByConsultant (%d)",
			code, cli.ExitStoppedByConsultant)
	}
}

// TestRunStoppedByConsultant_StopReasonPrintedToStderr verifies that the
// consultant's stop reason is printed to stderr when the run is stopped
// (AC7.5). The artifact is left resumable; stderr is the operator's signal.
func TestRunStoppedByConsultant_StopReasonPrintedToStderr(t *testing.T) {
	const stopReason = "workflow prerequisites not met: missing artifact X"
	sess := &scriptedSession{
		outcome: domain.RunOutcome{
			Status:     domain.RunStoppedByConsultant,
			Message:    stopReason,
			StopReason: stopReason,
		},
	}
	_, _, errOut := runCLI(t, newStage7BaseArgs(), sess)
	if !strings.Contains(errOut, stopReason) {
		t.Errorf("stderr %q does not contain stop reason %q", errOut, stopReason)
	}
}

// ---------------------------------------------------------------------------
// Stage 5: RunFlagSpecs and ValueBearingFlagNames (T5.1 — arity introspection)
//
// cli.RunFlagSpecs() is the authoritative arity declaration for every flag
// mosaic-run accepts. It must derive from the same single declaration that
// cli.Run() uses to register flags onto the run subcommand, so that adding a
// flag requires editing exactly one place rather than maintaining two lists.
//
// cli.ValueBearingFlagNames() is a convenience derived from RunFlagSpecs that
// returns only the names of value-consuming flags; this is the set that
// hasPositionalArg must skip when scanning os.Args.
//
// All tests in this section are RED until Stage 5's I5.1 refactor implements
// these functions. The stubs in flagspecs.go panic to keep every caller RED.
//
// The drift test (TestRunFlagSpecs_DriftFromActualRegistration) uses
// cli.RegisterRunFlags to populate a fresh FlagSet and then walks it with
// VisitAll, comparing the registration against RunFlagSpecs(). This structural
// test is what prevents a regression to two hand-maintained lists: it cannot
// be satisfied by updating a literal list in the test, only by keeping
// RunFlagSpecs and the registration in sync through a shared declaration.
// ---------------------------------------------------------------------------

// TestRunFlagSpecs_IsNonEmpty verifies that RunFlagSpecs returns at least one
// entry. An empty slice means the arity map is empty, which would cause
// hasPositionalArg to treat every flag value as positional.
//
// RED: RunFlagSpecs currently panics.
func TestRunFlagSpecs_IsNonEmpty(t *testing.T) {
	specs := cli.RunFlagSpecs()
	if len(specs) == 0 {
		t.Fatal("RunFlagSpecs() returned empty slice; want at least one FlagSpec entry")
	}
}

// TestRunFlagSpecs_ContainsTUIFlag verifies that RunFlagSpecs includes an entry
// for "--tui" with TakesValue: false. The --tui flag is the one entry-point-only
// flag (not registered on the run subcommand) that RunFlagSpecs must append
// explicitly.
//
// RED: RunFlagSpecs currently panics.
func TestRunFlagSpecs_ContainsTUIFlag(t *testing.T) {
	specs := cli.RunFlagSpecs()
	for _, s := range specs {
		if s.Name == "--tui" {
			if s.TakesValue {
				t.Error("RunFlagSpecs()[--tui].TakesValue = true, want false; --tui is a boolean flag")
			}
			return
		}
	}
	t.Error("RunFlagSpecs() does not contain an entry for \"--tui\"; " +
		"it is the entry-point-only flag that RunFlagSpecs must append explicitly")
}

// TestRunFlagSpecs_ClaudePathIsValueBearing verifies that "--claude-path" appears
// in RunFlagSpecs with TakesValue: true. This flag is the one directly involved
// in the mode-detection bug and must be in the value-bearing set so hasPositionalArg
// skips its value token.
//
// RED: RunFlagSpecs currently panics.
func TestRunFlagSpecs_ClaudePathIsValueBearing(t *testing.T) {
	specs := cli.RunFlagSpecs()
	for _, s := range specs {
		if s.Name == "--claude-path" {
			if !s.TakesValue {
				t.Error("RunFlagSpecs()[--claude-path].TakesValue = false, want true; " +
					"--claude-path consumes a following argument and must be value-bearing")
			}
			return
		}
	}
	t.Error("RunFlagSpecs() does not contain an entry for \"--claude-path\"; " +
		"it is a value-bearing flag and must be declared in the arity map")
}

// TestRunFlagSpecs_PreConsultIsBoolean verifies that "--pre-consult" appears in
// RunFlagSpecs with TakesValue: false. If it were mistakenly marked as
// value-bearing, the token following --pre-consult would be skipped, and a
// genuine positional argument after it would be invisible to hasPositionalArg.
//
// RED: RunFlagSpecs currently panics.
func TestRunFlagSpecs_PreConsultIsBoolean(t *testing.T) {
	specs := cli.RunFlagSpecs()
	for _, s := range specs {
		if s.Name == "--pre-consult" {
			if s.TakesValue {
				t.Error("RunFlagSpecs()[--pre-consult].TakesValue = true, want false; " +
					"--pre-consult is a boolean flag and must not be value-bearing (AC5.5)")
			}
			return
		}
	}
	t.Error("RunFlagSpecs() does not contain an entry for \"--pre-consult\"")
}

// TestRunFlagSpecs_KnownArities verifies that a representative set of flags
// from both the value-bearing and boolean categories carry the correct arity.
// This is a table-driven spot-check that catches systematic errors
// (e.g. all flags marked value-bearing by mistake).
//
// RED: RunFlagSpecs currently panics.
func TestRunFlagSpecs_KnownArities(t *testing.T) {
	wantValueBearing := []string{
		"--workflow",
		"--task",
		"--mode",
		"--checkpoints",
		"--commits",
		"--commit-branch",
		"--run",
		"--harness",
		"--timeout",
		"--claude-path",
		"--infra-class",
		"--input",
	}
	wantBoolean := []string{
		"--allow-version-drift",
		"--pre-consult",
		"--manual-resolution",
		"--new-run",
		"--tui",
	}

	specs := cli.RunFlagSpecs()
	specsMap := make(map[string]cli.FlagSpec, len(specs))
	for _, s := range specs {
		specsMap[s.Name] = s
	}

	for _, name := range wantValueBearing {
		spec, ok := specsMap[name]
		if !ok {
			t.Errorf("flag %q is absent from RunFlagSpecs(); it is value-bearing and must be declared", name)
			continue
		}
		if !spec.TakesValue {
			t.Errorf("RunFlagSpecs()[%q].TakesValue = false, want true", name)
		}
	}

	for _, name := range wantBoolean {
		spec, ok := specsMap[name]
		if !ok {
			t.Errorf("flag %q is absent from RunFlagSpecs(); it is boolean and must be declared", name)
			continue
		}
		if spec.TakesValue {
			t.Errorf("RunFlagSpecs()[%q].TakesValue = true, want false; boolean flags must not be value-bearing", name)
		}
	}
}

// TestValueBearingFlagNames_ContainsExpectedFlags verifies that
// ValueBearingFlagNames returns every flag known to be value-bearing. A missing
// entry means hasPositionalArg would not skip that flag's value and would
// falsely classify the value token as a positional argument.
//
// RED: ValueBearingFlagNames currently panics.
func TestValueBearingFlagNames_ContainsExpectedFlags(t *testing.T) {
	wantPresent := []string{
		"--workflow",
		"--task",
		"--mode",
		"--checkpoints",
		"--commits",
		"--commit-branch",
		"--run",
		"--harness",
		"--timeout",
		"--claude-path",
		"--infra-class",
		"--input",
		"--ghcp-permission-mode",
	}

	names := cli.ValueBearingFlagNames()
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, want := range wantPresent {
		if !nameSet[want] {
			t.Errorf("ValueBearingFlagNames() does not contain %q; "+
				"hasPositionalArg will treat its value token as a positional argument", want)
		}
	}
}

// TestValueBearingFlagNames_DoesNotContainBooleanFlags verifies that
// ValueBearingFlagNames excludes every boolean flag. Including a boolean flag
// would cause hasPositionalArg to skip the token following it, hiding genuine
// positional arguments and inverting the bug (AC5.5).
//
// RED: ValueBearingFlagNames currently panics.
func TestValueBearingFlagNames_DoesNotContainBooleanFlags(t *testing.T) {
	boolFlags := []string{
		"--allow-version-drift",
		"--pre-consult",
		"--manual-resolution",
		"--new-run",
		"--tui",
	}

	names := cli.ValueBearingFlagNames()
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, flag := range boolFlags {
		if nameSet[flag] {
			t.Errorf("ValueBearingFlagNames() contains %q; "+
				"boolean flags must not be in the value-bearing set — "+
				"including one would cause hasPositionalArg to skip the following token (AC5.5)", flag)
		}
	}
}

// TestValueBearingFlagNames_ConsistentWithRunFlagSpecs verifies that
// ValueBearingFlagNames() returns exactly the names from RunFlagSpecs() where
// TakesValue is true. If the two functions disagree, a pre-scan consumer using
// ValueBearingFlagNames and a consumer iterating RunFlagSpecs directly would
// make different decisions for the same args.
//
// RED: both functions currently panic.
func TestValueBearingFlagNames_ConsistentWithRunFlagSpecs(t *testing.T) {
	specs := cli.RunFlagSpecs()
	names := cli.ValueBearingFlagNames()

	wantNames := make(map[string]bool)
	for _, s := range specs {
		if s.TakesValue {
			wantNames[s.Name] = true
		}
	}

	gotNames := make(map[string]bool, len(names))
	for _, n := range names {
		gotNames[n] = true
	}

	for name := range wantNames {
		if !gotNames[name] {
			t.Errorf("ValueBearingFlagNames() is missing %q, which RunFlagSpecs() declares as TakesValue=true", name)
		}
	}
	for name := range gotNames {
		if !wantNames[name] {
			t.Errorf("ValueBearingFlagNames() contains %q, which RunFlagSpecs() does not declare as TakesValue=true", name)
		}
	}
}

// TestRunFlagSpecs_DriftFromActualRegistration is the structural drift test.
// It uses cli.RegisterRunFlags to populate a fresh pflag.FlagSet — the same
// function that cli.Run() calls internally — and then walks the registered flags
// via VisitAll, comparing each flag's actual arity (bool vs non-bool) against
// what RunFlagSpecs() declares.
//
// This test cannot be satisfied by updating a literal list in the test file:
// it passes only when RunFlagSpecs derives from the same declaration as the
// registration, ensuring both are automatically in sync when a flag is added
// or removed. That is the single-source-of-truth property Stage 5 requires.
//
// Expected asymmetries (not drift):
//   - "--tui" appears in RunFlagSpecs but not in the run subcommand registration;
//     it is the one entry-point-only flag, documented in the design.
//
// RED: cli.RegisterRunFlags and cli.RunFlagSpecs both currently panic.
func TestRunFlagSpecs_DriftFromActualRegistration(t *testing.T) {
	// Populate a fresh FlagSet using the same registration function that
	// cli.Run() calls. After the Stage 5 refactor, RegisterRunFlags is the
	// single place all run flags are declared.
	fs := pflag.NewFlagSet("run", pflag.ContinueOnError)
	cli.RegisterRunFlags(fs)

	specs := cli.RunFlagSpecs()
	specsMap := make(map[string]cli.FlagSpec, len(specs))
	for _, s := range specs {
		specsMap[s.Name] = s
	}

	// Every flag registered on the run subcommand must appear in RunFlagSpecs
	// with the correct arity (bool vs non-bool).
	fs.VisitAll(func(f *pflag.Flag) {
		name := "--" + f.Name
		spec, ok := specsMap[name]
		if !ok {
			t.Errorf("flag %q is registered on the run subcommand but absent from RunFlagSpecs(); "+
				"adding a flag without updating RunFlagSpecs restores the two-list drift this stage eliminates", name)
			return
		}
		takesValue := f.Value.Type() != "bool"
		if spec.TakesValue != takesValue {
			t.Errorf("flag %q: RunFlagSpecs().TakesValue = %v, registration says %v (type=%q); "+
				"arity mismatch between the declared spec and the actual cobra registration",
				name, spec.TakesValue, takesValue, f.Value.Type())
		}
	})

	// "--tui" is the one expected specs-only flag. Verify it is present in specs
	// with the correct arity and is genuinely absent from the run subcommand registration.
	if _, ok := specsMap["--tui"]; !ok {
		t.Error("RunFlagSpecs() does not contain \"--tui\"; " +
			"it is the entry-point-only boolean flag that RunFlagSpecs must append explicitly")
	}
	if fs.Lookup("tui") != nil {
		t.Error("\"--tui\" is registered on the run subcommand FlagSet; " +
			"it must be entry-point-only and absent from the run subcommand registration")
	}
}
