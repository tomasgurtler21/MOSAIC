package cli_test

// Tests for how the CLI presents a failed run start: a commit setup or
// pre-consultation failure that left a resumable artifact behind.

import (
	"strings"
	"testing"

	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
)

// TestRunStartFailed_ExitsWithStartFailedCode verifies that a start-failed
// outcome maps to its own non-zero exit code, distinct from refusal, stop and
// generic failure.
func TestRunStartFailed_ExitsWithStartFailedCode(t *testing.T) {
	sess := &scriptedSession{
		outcome: domain.RunOutcome{
			Status:  domain.RunStartFailed,
			Message: "commit setup failed: no branch marker reported",
		},
	}

	code, _, _ := runCLI(t, newStage7BaseArgs(), sess)

	if code == cli.ExitSuccess {
		t.Error("exit code = 0, want non-zero for a start failure")
	}
	if code != cli.ExitStartFailed {
		t.Errorf("exit code = %d, want ExitStartFailed (%d)", code, cli.ExitStartFailed)
	}
	for name, other := range map[string]int{
		"ExitFailure": cli.ExitFailure, "ExitStopped": cli.ExitStopped, "ExitRefused": cli.ExitRefused,
		"ExitDeviationUnresolved": cli.ExitDeviationUnresolved, "ExitStoppedByConsultant": cli.ExitStoppedByConsultant,
	} {
		if cli.ExitStartFailed == other {
			t.Errorf("ExitStartFailed must be distinct from %s (%d)", name, other)
		}
	}
}

// TestRunStartFailed_MessagePrintedToStderrWithResumeHint verifies that the
// failure is visible: stderr carries the outcome message naming the failed
// step, and states that resuming the run retries it.
func TestRunStartFailed_MessagePrintedToStderrWithResumeHint(t *testing.T) {
	const msg = "pre-consultation failed: orchestrator agent timed out"
	sess := &scriptedSession{
		outcome: domain.RunOutcome{Status: domain.RunStartFailed, Message: msg},
	}

	code, out, errOut := runCLI(t, newStage7BaseArgs(), sess)

	if code != cli.ExitStartFailed {
		t.Errorf("exit code = %d, want ExitStartFailed (%d)", code, cli.ExitStartFailed)
	}
	if !strings.Contains(errOut, msg) {
		t.Errorf("stderr %q does not contain the outcome message %q", errOut, msg)
	}
	if !strings.Contains(strings.ToLower(errOut), "resum") {
		t.Errorf("stderr %q does not state that resuming the run retries the failed step", errOut)
	}
	if strings.Contains(out, msg) {
		t.Errorf("stdout %q must stay machine-readable and not carry the failure message", out)
	}
}

// TestRunStartFailed_DoesNotWriteCompletedMarker verifies that a start failure
// never marks the run COMPLETED; it must stay resumable.
func TestRunStartFailed_DoesNotWriteCompletedMarker(t *testing.T) {
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{Status: domain.RunStartFailed, Message: "commit setup failed"},
	}

	runCLIWithStore(t, newStage7BaseArgs(), spy, sess)

	if len(spy.setCalls) != 0 {
		t.Errorf("store.SetPhase called %d time(s), want 0 when status is RunStartFailed", len(spy.setCalls))
	}
}
