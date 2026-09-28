package cli_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
)

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
