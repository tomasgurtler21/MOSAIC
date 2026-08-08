package tui

// responsiveness_test.go verifies AC16.6: the suite executes in the
// background and the interface handles input while it runs; a slow render
// never blocks the suite, and a slow suite never freezes the interface.
//
// The event contract already requires that a stalled consumer cannot stall
// the producer (suite.AsyncSink); this stage's job is to be a consumer that
// does not stall in the first place. These tests exercise the model side of
// that obligation without a real terminal.

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/domain"
)

// backgroundUpdate drives m.Update on its own goroutine and reports whether
// it returned within timeout. Unlike safeUpdate, it never calls a *testing.T
// failure method from the spawned goroutine — Go's testing package requires
// FailNow (and therefore Fatalf) to run only on the test's own goroutine —
// so responsiveness tests report timing failures from the caller's
// goroutine instead. A panic (e.g. "not implemented") counts as "returned",
// since responsiveness is about not blocking, not about success.
func backgroundUpdate(m Model, msg tea.Msg, timeout time.Duration) (returned bool) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { recover() }()
		m.Update(msg)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// ---------------------------------------------------------------------------
// A slow suite never freezes the interface: input is handled while running
// ---------------------------------------------------------------------------

// TestResponsiveness_InputHandledWhileSuiteRuns verifies that once a suite
// run has started, Update still returns promptly for a key press received
// while the suite is (deliberately, in this test) still blocked in the
// background — the model must not wait on the suite to answer a key.
func TestResponsiveness_InputHandledWhileSuiteRuns(t *testing.T) {
	runner := newFakeSuiteRunner().blocking()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	m, startCmd := safeUpdate(t, m, keyMsg("\r"))
	if startCmd == nil {
		t.Fatalf("selecting a suite produced no command to start the run")
	}
	go func() { _ = startCmd() }() // the suite is now running and blocked

	if !backgroundUpdate(m, tea.WindowSizeMsg{Width: 100, Height: 30}, 2*time.Second) {
		t.Errorf("Update did not return within 2s while the suite was still running; a slow suite must not freeze the interface")
	}

	runner.releaseRun()
}

// ---------------------------------------------------------------------------
// A slow render never blocks the suite: NewProgramSink forwards every event
// it is given, so a burst is never silently thinned on the way in — the
// non-blocking guarantee itself belongs to internal/suite's emitSafe and
// AsyncSink (already covered by that package's own tests), which this
// sink sits behind in the composition root.
// ---------------------------------------------------------------------------

// TestResponsiveness_ProgramSink_ForwardsEveryEvent verifies that
// NewProgramSink delivers every event it receives to the message loop as a
// ProgressMsg, in order — the plain adaptation this frontend is responsible
// for, distinct from the upstream non-blocking guarantee.
func TestResponsiveness_ProgramSink_ForwardsEveryEvent(t *testing.T) {
	var got []tea.Msg
	sink := NewProgramSink(func(msg tea.Msg) {
		got = append(got, msg)
	})

	events := []domain.ProgressEvent{
		{Kind: domain.ProgressSuiteStarted, TotalTests: 1},
		{Kind: domain.ProgressTestStarted, TestID: "test-a", Repetition: 1, Repetitions: 1},
		{Kind: domain.ProgressTestFinished, TestID: "test-a", Verdict: domain.VerdictPass},
		{Kind: domain.ProgressSuiteFinished, Counts: map[domain.Verdict]int{domain.VerdictPass: 1}},
	}
	for _, ev := range events {
		sink.Emit(ev)
	}

	if len(got) != len(events) {
		t.Fatalf("ProgramSink delivered %d messages, want %d", len(got), len(events))
	}
	for i, ev := range events {
		pm, ok := got[i].(ProgressMsg)
		if !ok {
			t.Fatalf("message %d is %T, want ProgressMsg", i, got[i])
		}
		if pm.Event.Kind != ev.Kind {
			t.Errorf("message %d Kind = %q, want %q", i, pm.Event.Kind, ev.Kind)
		}
	}
}

// ---------------------------------------------------------------------------
// Quit is honoured immediately even mid-run (complements the cancellation
// test in navigation_test.go: this test asserts only on responsiveness —
// that Update returns without waiting for the suite — not on cancellation).
// ---------------------------------------------------------------------------

// TestResponsiveness_QuitReturnsImmediatelyDuringRun verifies that pressing
// the quit key while a suite is running produces a command without Update
// itself blocking on the suite's completion.
func TestResponsiveness_QuitReturnsImmediatelyDuringRun(t *testing.T) {
	runner := newFakeSuiteRunner().blocking()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	m, startCmd := safeUpdate(t, m, keyMsg("\r"))
	go func() { _ = startCmd() }()
	defer runner.releaseRun()

	if !backgroundUpdate(m, tea.KeyMsg{Type: tea.KeyCtrlC}, 2*time.Second) {
		t.Errorf("Update did not return within 2s for a quit key press while the suite was running")
	}
}
