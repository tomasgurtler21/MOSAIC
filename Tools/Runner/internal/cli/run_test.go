package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

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
	code := cli.Run(context.Background(), args, sess, &out, &errOut)
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
	code, _, _ := runCLI(t, []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "my-workflow",
		"--task", "do the work",
	}, sess)

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
	if cfg.ExistingArtifact != domain.ExistingResume {
		t.Errorf("ExistingArtifact = %q, want %q (default)", cfg.ExistingArtifact, domain.ExistingResume)
	}
	if cfg.AllowVersionDrift {
		t.Error("AllowVersionDrift should default to false")
	}
	if cfg.ArtifactLocation != "" {
		t.Errorf("ArtifactLocation = %q, want empty string (default = canonical path)", cfg.ArtifactLocation)
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
		"--existing-artifact", "fresh",
		"--allow-version-drift",
		"--artifact-location", "/path/to/Orchestration.md",
		"--checkpoints", "enabled",
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
	if cfg.ExistingArtifact != domain.ExistingFresh {
		t.Errorf("ExistingArtifact = %q, want %q", cfg.ExistingArtifact, domain.ExistingFresh)
	}
	if !cfg.AllowVersionDrift {
		t.Error("AllowVersionDrift should be true when --allow-version-drift is set")
	}
	if cfg.ArtifactLocation != "/path/to/Orchestration.md" {
		t.Errorf("ArtifactLocation = %q, want %q", cfg.ArtifactLocation, "/path/to/Orchestration.md")
	}
	if !cfg.Checkpoints {
		t.Error("Checkpoints should be true when --checkpoints=enabled")
	}
}

// ---- tests: existing-artifact flag variants ----

func TestExistingArtifactFlag(t *testing.T) {
	tests := []struct {
		value string
		want  domain.ExistingArtifactMode
	}{
		{"resume", domain.ExistingResume},
		{"fresh", domain.ExistingFresh},
		{"fail", domain.ExistingFail},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
			runCLI(t, []string{
				"run",
				"--orchestrator-file", "f", "--workflow", "w", "--task", "t",
				"--existing-artifact", tt.value,
			}, sess)
			if sess.config.ExistingArtifact != tt.want {
				t.Errorf("ExistingArtifact = %q, want %q", sess.config.ExistingArtifact, tt.want)
			}
		})
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
			code, _, _ := runCLI(t, baseArgs, sess)
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
		{"invalid --existing-artifact", "--existing-artifact", "invalid-value"},
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
