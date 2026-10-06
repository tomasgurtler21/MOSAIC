package tui

// panic_boundary_test.go verifies that a panic in a tea.Cmd closure running
// session or test-flow work becomes a message the model handles, and that the
// panic value and stack are recorded in the Runner debug log.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/testrun"
)

// panicLogEntry is one recorded debug-log call.
type panicLogEntry struct {
	event   string
	message string
	fields  map[string]string
}

// panicLogRecorder is a domain.DebugLogger that records every entry.
type panicLogRecorder struct {
	mu      sync.Mutex
	entries []panicLogEntry
}

func (r *panicLogRecorder) Log(event string, message string, fields ...domain.DebugField) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := map[string]string{}
	for _, fld := range fields {
		f[fld.Key] = fld.Value
	}
	r.entries = append(r.entries, panicLogEntry{event: event, message: message, fields: f})
}

func (r *panicLogRecorder) snapshot() []panicLogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]panicLogEntry(nil), r.entries...)
}

// runCmdNoPanic runs cmd and fails the test (instead of crashing the test
// binary) when the command lets a panic escape.
func runCmdNoPanic(t *testing.T, cmd tea.Cmd) (msg tea.Msg) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("panic escaped the command: %v", p)
		}
	}()
	return cmd()
}

// assertPanicLogged requires exactly one runner.error entry for site that
// carries the panic value and a full stack (including the panicking frames).
func assertPanicLogged(t *testing.T, rec *panicLogRecorder, site, value string) {
	t.Helper()
	var found []panicLogEntry
	for _, e := range rec.snapshot() {
		if e.event == domain.EventRunnerError {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("runner.error entries = %d, want 1: %+v", len(found), rec.snapshot())
	}
	e := found[0]
	if !strings.Contains(e.message, value) {
		t.Errorf("debug message lacks panic value %q: %q", value, e.message)
	}
	if !strings.Contains(e.message, "goroutine") {
		t.Errorf("debug message lacks a stack trace: %q", e.message)
	}
	if e.fields["site"] != site {
		t.Errorf("site field = %q, want %q", e.fields["site"], site)
	}
}

func TestGuardCmd_PanicBecomesOnPanicMessageAndIsLogged(t *testing.T) {
	rec := &panicLogRecorder{}
	type sentinel struct{ err error }
	cmd := guardCmd(rec, "widget", func(err error) tea.Msg { return sentinel{err: err} },
		func() tea.Msg { panic("boom-value") })

	msg := runCmdNoPanic(t, cmd)

	got, ok := msg.(sentinel)
	if !ok {
		t.Fatalf("message = %T, want the onPanic message", msg)
	}
	if got.err == nil || !strings.Contains(got.err.Error(), "panic in widget: boom-value") {
		t.Errorf("error = %v, want it to name the site and panic value", got.err)
	}
	assertPanicLogged(t, rec, "widget", "boom-value")
}

func TestGuardCmd_ReturnedErrorStackIsTruncatedButLogKeepsFullStack(t *testing.T) {
	rec := &panicLogRecorder{}
	cmd := guardCmd(rec, "widget", func(err error) tea.Msg { return err },
		func() tea.Msg { panic("deep") })

	msg := runCmdNoPanic(t, cmd)

	err, _ := msg.(error)
	if err == nil {
		t.Fatalf("message = %T, want an error", msg)
	}
	// "panic in widget: deep\n" + at most 4096 bytes of stack.
	if max := len("panic in widget: deep\n") + 4096; len(err.Error()) > max {
		t.Errorf("returned error is %d bytes, want at most %d", len(err.Error()), max)
	}
}

func TestGuardCmd_NoPanicReturnsMessageUnchangedAndLogsNothing(t *testing.T) {
	rec := &panicLogRecorder{}
	type done struct{}
	cmd := guardCmd(rec, "widget", func(error) tea.Msg { return errors.New("must not be used") },
		func() tea.Msg { return done{} })

	msg := runCmdNoPanic(t, cmd)

	if _, ok := msg.(done); !ok {
		t.Fatalf("message = %T, want the command's own message", msg)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Errorf("debug entries = %d, want 0", n)
	}
}

func TestStartSession_PanicIsLoggedWithValueAndStack(t *testing.T) {
	rec := &panicLogRecorder{}
	m := newRootModel(context.Background(), &panicSession{panicValue: "session-exploded"}, Options{
		Theme: tuicommon.DefaultTheme(),
		Debug: rec,
	})

	msg := runCmdNoPanic(t, m.startSession())

	errMsg, ok := msg.(runErrorMsg)
	if !ok {
		t.Fatalf("message = %T, want runErrorMsg", msg)
	}
	if errMsg.err == nil || !strings.Contains(errMsg.err.Error(), "panic in session: session-exploded") {
		t.Errorf("error = %v, want the prefix and panic value", errMsg.err)
	}
	assertPanicLogged(t, rec, "session", "session-exploded")
}

func TestLaunchTestRun_PanickingFactoryYieldsTestDoneWithErrorAndIsLogged(t *testing.T) {
	rec := &panicLogRecorder{}
	m := newRootModel(context.Background(), &panicSession{}, Options{
		Theme: tuicommon.DefaultTheme(),
		Debug: rec,
		TestRunnerFactory: func(context.Context, testrun.TestConfig, testrun.ProgressReporter) (*testrun.TestSummary, error) {
			panic("factory-exploded")
		},
	})

	msg := runCmdNoPanic(t, m.launchTestRun())

	done, ok := msg.(testAllDoneMsg)
	if !ok {
		t.Fatalf("message = %T, want testAllDoneMsg", msg)
	}
	if done.Summary == nil || done.Summary.DeployError == nil {
		t.Fatalf("summary = %+v, want a summary carrying an error", done.Summary)
	}
	if !strings.Contains(done.Summary.DeployError.Error(), "factory-exploded") {
		t.Errorf("summary error = %v, want the panic value", done.Summary.DeployError)
	}
	assertPanicLogged(t, rec, "test run", "factory-exploded")
}

func TestLaunchTestRun_NonPanickingFactoryIsUnaffected(t *testing.T) {
	rec := &panicLogRecorder{}
	want := &testrun.TestSummary{}
	m := newRootModel(context.Background(), &panicSession{}, Options{
		Theme: tuicommon.DefaultTheme(),
		Debug: rec,
		TestRunnerFactory: func(context.Context, testrun.TestConfig, testrun.ProgressReporter) (*testrun.TestSummary, error) {
			return want, nil
		},
	})

	msg := runCmdNoPanic(t, m.launchTestRun())

	done, ok := msg.(testAllDoneMsg)
	if !ok || done.Summary != want {
		t.Fatalf("message = %#v, want testAllDoneMsg with the factory's summary", msg)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Errorf("debug entries = %d, want 0", n)
	}
}
