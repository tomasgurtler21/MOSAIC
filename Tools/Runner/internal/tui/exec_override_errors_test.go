package tui

// exec_override_errors_test.go verifies error identity at the TUI boundary:
// *domain.HarnessLaunchError survives wrapping and is distinguishable via
// errors.As/errors.Is from every other failure shape the session can produce.
//
// T6.2 (error identity): error-identity assertions on HarnessLaunchError are
//   GREEN (the type is correctly constructed); routing assertions live in
//   exec_override_routing_test.go because the runErrorMsg/runDoneMsg handlers
//   do not yet check errors.As.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"mosaic-run/internal/domain"
)

// TestHarnessLaunchError_ErrorsIs_ErrHarnessLaunchFailed verifies that a
// *domain.HarnessLaunchError reports true for errors.Is against
// domain.ErrHarnessLaunchFailed. This pins the class-sentinel identity.
func TestHarnessLaunchError_ErrorsIs_ErrHarnessLaunchFailed(t *testing.T) {
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	if !errors.Is(le, domain.ErrHarnessLaunchFailed) {
		t.Errorf("errors.Is(launchErr, domain.ErrHarnessLaunchFailed) = false; want true — "+
			"the class sentinel must survive wrapping so the TUI can detect a launch failure "+
			"without knowing which harness produced it")
	}
}

// TestHarnessLaunchError_ErrorsAs_ExtractsFields verifies that errors.As on a
// *domain.HarnessLaunchError extracts the Harness and Executable fields. The
// TUI uses these fields to populate the override screen without re-deriving
// them from an error message.
func TestHarnessLaunchError_ErrorsAs_ExtractsFields(t *testing.T) {
	const harnessID = "ghcp-cli"
	const exe = "/usr/bin/copilot"
	le := launchFailureErr(harnessID, exe)

	var found *domain.HarnessLaunchError
	if !errors.As(le, &found) {
		t.Fatal("errors.As did not find *domain.HarnessLaunchError; " +
			"the TUI relies on errors.As to extract the harness ID and executable path " +
			"for display on the override screen")
	}
	if found.Harness != harnessID {
		t.Errorf("HarnessLaunchError.Harness = %q, want %q", found.Harness, harnessID)
	}
	if found.Executable != exe {
		t.Errorf("HarnessLaunchError.Executable = %q, want %q", found.Executable, exe)
	}
}

// TestHarnessLaunchError_SurvivesWrapping_DetectableViaErrorsAs verifies that a
// *domain.HarnessLaunchError remains detectable via errors.As after wrapping
// with fmt.Errorf("%w", ...), simulating session-boundary wrapping.
func TestHarnessLaunchError_SurvivesWrapping_DetectableViaErrorsAs(t *testing.T) {
	inner := launchFailureErr("claude-code", "/usr/local/bin/claude")
	outer := fmt.Errorf("session failed: %w", inner)

	var found *domain.HarnessLaunchError
	if !errors.As(outer, &found) {
		t.Error("errors.As did not find *domain.HarnessLaunchError through %w wrapping; " +
			"the sentinel must survive cross-layer wrapping so the TUI can detect it")
	}
}

// TestNonLaunchFailure_ErrNonZeroExit_NotDetectedAsLaunchFailure verifies that
// a plain non-zero exit error is not classified as a *domain.HarnessLaunchError.
// Regression guard (GREEN today; must stay GREEN after routing is wired).
func TestNonLaunchFailure_ErrNonZeroExit_NotDetectedAsLaunchFailure(t *testing.T) {
	exitErr := fmt.Errorf("process exited: exit status 1")
	var le *domain.HarnessLaunchError
	if errors.As(exitErr, &le) {
		t.Error("errors.As found *domain.HarnessLaunchError in a non-zero exit error; " +
			"want false — non-zero exit must not trigger the override screen")
	}
}

// TestNonLaunchFailure_Timeout_NotDetectedAsLaunchFailure verifies that a
// timeout error is not classified as a launch failure.
func TestNonLaunchFailure_Timeout_NotDetectedAsLaunchFailure(t *testing.T) {
	timeoutErr := fmt.Errorf("invocation timed out after 30m")
	var le *domain.HarnessLaunchError
	if errors.As(timeoutErr, &le) {
		t.Error("errors.As found *domain.HarnessLaunchError in a timeout error; " +
			"want false — timeout must not trigger the override screen")
	}
}

// TestNonLaunchFailure_ContextCancelled_NotDetectedAsLaunchFailure verifies
// that a context-cancellation error is not classified as a launch failure.
func TestNonLaunchFailure_ContextCancelled_NotDetectedAsLaunchFailure(t *testing.T) {
	var le *domain.HarnessLaunchError
	if errors.As(context.Canceled, &le) {
		t.Error("errors.As found *domain.HarnessLaunchError in context.Canceled; " +
			"want false — cancellation must not trigger the override screen")
	}
}

// TestLaunchFailureDetection_TextOnlyError_NotDetectedAsLaunchFailure verifies
// that an error with matching text but no *domain.HarnessLaunchError type in its
// chain is not classified as a launch failure. This pins that detection uses
// type identity (errors.As), not string matching.
func TestLaunchFailureDetection_TextOnlyError_NotDetectedAsLaunchFailure(t *testing.T) {
	textErr := errors.New("harness: launch failed — could not start process")
	var le *domain.HarnessLaunchError
	if errors.As(textErr, &le) {
		t.Error("errors.As found *domain.HarnessLaunchError in a plain text error; " +
			"want false — launch-failure detection must use type identity, not string matching")
	}
}

// TestRunOutcome_Cause_CarriesLaunchError verifies that RunOutcome.Cause can
// hold a *domain.HarnessLaunchError and that errors.As finds it, covering the
// runDoneMsg path where session.refusal preserves the error in Cause.
func TestRunOutcome_Cause_CarriesLaunchError(t *testing.T) {
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	outcome := domain.RunOutcome{
		Status:  domain.RunRefused,
		Message: "pre-consultation failed: " + le.Error(),
		Cause:   le,
	}

	var found *domain.HarnessLaunchError
	if !errors.As(outcome.Cause, &found) {
		t.Error("errors.As did not find *domain.HarnessLaunchError in RunOutcome.Cause; " +
			"the Cause field must carry the error with its type intact across the refusal boundary")
	}
}
