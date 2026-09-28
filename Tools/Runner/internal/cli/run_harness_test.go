package cli_test

import (
	"os"
	"strings"
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
)

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
	newTestCLIHarnessWorkDir(t, "claude-code")
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

// TestHarnessFlag_OpenCodeAccepted verifies that --harness opencode is
// accepted and the session is started normally, mirroring
// TestHarnessFlag_ClaudeCodeAccepted for the new catalog entry.
func TestHarnessFlag_OpenCodeAccepted(t *testing.T) {
	newTestCLIHarnessWorkDir(t, "opencode")
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
			newTestCLIHarnessWorkDir(t, entry.ID)
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

// TestExecutablePathFlag_Accepted verifies that --executable-path is accepted with any
// non-empty string value and the session is started normally.
func TestExecutablePathFlag_Accepted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := append(baseHarnessArgs(), "--executable-path", "/usr/local/bin/claude")
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); stderr: %q", code, cli.ExitSuccess, errOut)
	}
	if !sess.called {
		t.Error("session.Start was not called when --executable-path is provided")
	}
}
