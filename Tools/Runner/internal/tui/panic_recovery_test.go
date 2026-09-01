package tui

// panic_recovery_test.go verifies that the startSession tea.Cmd closure
// recovers from a panic in session.Start and returns a runErrorMsg with
// diagnostic information (panic value and stack trace), rather than
// propagating the panic and silently terminating the process.

import (
	"context"
	"strings"
	"testing"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
)

// panicSession is a stub session whose Start method panics with a fixed value.
// It is used to trigger the panic-recovery path in startSession.
type panicSession struct {
	panicValue interface{}
}

func (p *panicSession) Start(_ context.Context, _ domain.RunConfig) (domain.RunOutcome, error) {
	panic(p.panicValue)
}

// TestStartSession_PanicRecovery verifies that when session.Start panics,
// the startSession command closure recovers from the panic and returns a
// runErrorMsg whose error contains the panic value and stack trace information.
// The test calls the tea.Cmd synchronously so no goroutine coordination is
// needed — Bubble Tea runs the command in a goroutine in production, but the
// recovery applies equally because the defer/recover lives inside the closure.
func TestStartSession_PanicRecovery(t *testing.T) {
	const panicMsg = "intentional panic for recovery test"

	sess := &panicSession{panicValue: panicMsg}
	m := newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
	})

	cmd := m.startSession()

	// Invoke the command directly. This must not panic.
	msg := cmd()

	errMsg, ok := msg.(runErrorMsg)
	if !ok {
		t.Fatalf("startSession() cmd returned %T, want runErrorMsg", msg)
	}
	if errMsg.err == nil {
		t.Fatal("runErrorMsg.err is nil; want non-nil error with panic diagnostics")
	}

	errStr := errMsg.err.Error()

	// The error must contain the panic value so the operator can identify the cause.
	if !strings.Contains(errStr, panicMsg) {
		t.Errorf("runErrorMsg.err does not contain panic value %q; got: %v", panicMsg, errStr)
	}

	// The error must contain stack trace information. debug.Stack() always
	// begins with a "goroutine" header line.
	if !strings.Contains(errStr, "goroutine") {
		t.Errorf("runErrorMsg.err does not appear to contain a stack trace; got: %v", errStr)
	}
}

// TestStartSession_PanicRecovery_NonStringPanic verifies that the recovery
// also works when the panic value is not a string (e.g., an integer or error).
func TestStartSession_PanicRecovery_NonStringPanic(t *testing.T) {
	sess := &panicSession{panicValue: 42}
	m := newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
	})

	cmd := m.startSession()
	msg := cmd()

	errMsg, ok := msg.(runErrorMsg)
	if !ok {
		t.Fatalf("startSession() cmd returned %T, want runErrorMsg", msg)
	}
	if errMsg.err == nil {
		t.Fatal("runErrorMsg.err is nil; want non-nil error")
	}
	// The formatted error must include the integer value.
	if !strings.Contains(errMsg.err.Error(), "42") {
		t.Errorf("runErrorMsg.err does not contain panic value 42; got: %v", errMsg.err)
	}
}
