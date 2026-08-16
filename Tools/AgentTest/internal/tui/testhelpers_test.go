package tui

// Shared test doubles and scripted event builders for this package's tests.
//
// Update, Fold, View, Init and Run are implemented in app.go/progress.go/
// screens.go. The safe* drivers below (safeUpdate, safeFold, safeView) still
// recover a panic from any of them and turn it into a clean test failure
// rather than crashing the whole test binary — a defensive property worth
// keeping regardless of implementation state, not a RED-phase artifact.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
)

// ---------------------------------------------------------------------------
// Panic-safe drivers
// ---------------------------------------------------------------------------

// safeUpdate drives m.Update and converts a panic into a test failure
// instead of crashing the whole test binary.
func safeUpdate(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	var next Model
	var cmd tea.Cmd
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Model.Update panicked: %v", r)
			}
		}()
		tm, c := m.Update(msg)
		next = tm.(Model)
		cmd = c
	}()
	return next, cmd
}

// safeFold drives m.Fold and converts a panic into a test failure.
func safeFold(t *testing.T, m Model, ev domain.ProgressEvent) Model {
	t.Helper()
	var next Model
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Model.Fold panicked on %s: %v", ev.Kind, r)
			}
		}()
		next = m.Fold(ev)
	}()
	return next
}

// foldAll folds every event in seq, in order, returning the final Model.
func foldAll(t *testing.T, m Model, seq []domain.ProgressEvent) Model {
	t.Helper()
	for _, ev := range seq {
		m = safeFold(t, m, ev)
	}
	return m
}

// safeView drives m.View and converts a panic into a test failure.
func safeView(t *testing.T, m Model) string {
	t.Helper()
	var out string
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Model.View panicked: %v", r)
			}
		}()
		out = m.View()
	}()
	return out
}

// runCmd invokes a tea.Cmd synchronously and returns the tea.Msg it
// produces, or nil when cmd is nil. Used to advance a Model past a
// background operation (starting a suite, waiting on its completion)
// without a real Bubble Tea program loop.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

// ---------------------------------------------------------------------------
// Fixture Options / Model construction
// ---------------------------------------------------------------------------

// fixturePreflight returns a PreflightFunc that always resolves the given
// plan with no diagnostics, regardless of input.
func fixturePreflight(p preflight.Plan) PreflightFunc {
	return func(preflight.Input) (preflight.Plan, authoring.Report) {
		return p, authoring.Report{}
	}
}

// scriptedPreflight returns a PreflightFunc that ignores its input and yields
// the given report, so a test can drive the error-bearing and warning-only
// pre-flight paths without depending on real authored files.
//
// It returns a minimal but usable preflight.Plan alongside the report, so
// the warning-only case (where HasErrors() is false) can proceed to start the
// suite rather than failing at plan construction.
func scriptedPreflight(rpt authoring.Report) PreflightFunc {
	return func(preflight.Input) (preflight.Plan, authoring.Report) {
		return fixturePlan("suite-under-test"), rpt
	}
}

// fixturePlan builds a minimal preflight.Plan naming one suite and no
// tests, sufficient for driving suite selection without needing a real
// authored suite on disk.
func fixturePlan(suiteID string) preflight.Plan {
	return preflight.Plan{Suite: domain.TestSuite{ID: suiteID}}
}

// newFixtureOptions builds Options wired to a fakeSuiteRunner and a
// preflight stub, offering the given suite paths for selection.
func newFixtureOptions(suites []string, runner *fakeSuiteRunner) Options {
	return Options{
		Preflight: fixturePreflight(fixturePlan("suite-under-test")),
		Suite:     runner,
		Suites:    suites,
		Harness:   "fake",
	}
}

// ---------------------------------------------------------------------------
// fakeSuiteRunner: a SuiteRunner double
// ---------------------------------------------------------------------------

// fakeSuiteRunner is a SuiteRunner double whose Run either completes
// immediately with a scripted result, or blocks until release is closed —
// so a test can observe the model while a run is "in flight" and prove that
// quitting cancels the context the suite was given.
type fakeSuiteRunner struct {
	mu sync.Mutex

	// result/err are returned once Run is allowed to complete.
	result domain.CostReport // unused placeholder to keep zero-value construction simple
	report report.Result
	err    error

	// events, when non-empty, are emitted through sink (in order) before Run
	// returns, so folding tests can exercise a scripted sequence through a
	// real suite call.
	events []domain.ProgressEvent

	// block, when true, makes Run wait on release before returning — so a
	// test can assert on mid-run state and on cancellation.
	block   bool
	release chan struct{}

	called       bool
	gotCtx       context.Context
	gotRetention domain.RetentionPolicy
	cancelled    bool
	cancelDone   chan struct{}
}

var _ SuiteRunner = (*fakeSuiteRunner)(nil)

// newFakeSuiteRunner constructs a runner that completes immediately with an
// empty, passing report.
func newFakeSuiteRunner() *fakeSuiteRunner {
	return &fakeSuiteRunner{release: make(chan struct{}), cancelDone: make(chan struct{})}
}

// blocking configures the runner to block until release() is called.
func (f *fakeSuiteRunner) blocking() *fakeSuiteRunner {
	f.block = true
	return f
}

// withEvents configures events to be emitted through sink before Run
// returns.
func (f *fakeSuiteRunner) withEvents(events ...domain.ProgressEvent) *fakeSuiteRunner {
	f.events = events
	return f
}

// withResult configures the report.Result Run returns.
func (f *fakeSuiteRunner) withResult(r report.Result) *fakeSuiteRunner {
	f.report = r
	return f
}

// release unblocks a blocking Run call.
func (f *fakeSuiteRunner) releaseRun() {
	select {
	case <-f.release:
	default:
		close(f.release)
	}
}

// wasCancelled reports whether the context Run received was cancelled
// before Run returned control, waiting briefly for the cancellation to
// propagate.
func (f *fakeSuiteRunner) wasCancelled(t *testing.T) bool {
	t.Helper()
	f.mu.Lock()
	ctx := f.gotCtx
	f.mu.Unlock()
	if ctx == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	case <-time.After(500 * time.Millisecond):
		return false
	}
}

func (f *fakeSuiteRunner) Run(ctx context.Context, p preflight.Plan, sink domain.ProgressSink, retention domain.RetentionPolicy) (report.Result, error) {
	f.mu.Lock()
	f.called = true
	f.gotCtx = ctx
	f.gotRetention = retention
	f.mu.Unlock()

	for _, ev := range f.events {
		if sink != nil {
			sink.Emit(ev)
		}
	}

	if f.block {
		select {
		case <-f.release:
		case <-ctx.Done():
			f.mu.Lock()
			f.cancelled = true
			f.mu.Unlock()
			return f.report, ctx.Err()
		}
	}

	return f.report, f.err
}

// ---------------------------------------------------------------------------
// Scripted event sequences
// ---------------------------------------------------------------------------

// scriptedSuite describes one suite's worth of scripted progress events, for
// building both a full ordered event sequence and the report.Result it
// should fold to.
type scriptedSuite struct {
	suiteID string
	tests   []scriptedTest
}

// scriptedTest describes one test's single repetition, for folding tests
// that do not need to exercise the repetition loop itself.
type scriptedTest struct {
	testID      string
	invocations int // ProgressInvocation events emitted before the test finishes
	verdict     domain.Verdict
	duration    time.Duration
	cost        domain.CostReport
	failed      []string
}

// events renders the scripted suite into the exact ordered ProgressEvent
// sequence suite.Suite emits for it.
func (s scriptedSuite) events() []domain.ProgressEvent {
	var out []domain.ProgressEvent
	out = append(out, domain.ProgressEvent{
		Kind:       domain.ProgressSuiteStarted,
		SuiteID:    s.suiteID,
		TotalTests: len(s.tests),
	})
	counts := map[domain.Verdict]int{}
	total := domain.CostReport{Attribution: domain.AttributionAttributed}
	for _, tc := range s.tests {
		out = append(out, domain.ProgressEvent{
			Kind:        domain.ProgressTestStarted,
			TestID:      tc.testID,
			Repetition:  1,
			Repetitions: 1,
		})
		for i := 0; i < tc.invocations; i++ {
			out = append(out, domain.ProgressEvent{
				Kind:     domain.ProgressInvocation,
				Seq:      i + 1,
				Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "worker"},
				Outcome:  domain.OutcomePassthrough,
			})
		}
		out = append(out, domain.ProgressEvent{
			Kind:             domain.ProgressTestFinished,
			TestID:           tc.testID,
			Repetition:       1,
			Repetitions:      1,
			Verdict:          tc.verdict,
			Duration:         tc.duration,
			Cost:             tc.cost,
			FailedAssertions: tc.failed,
		})
		counts[tc.verdict]++
		total = total.Add(tc.cost)
	}
	out = append(out, domain.ProgressEvent{
		Kind:      domain.ProgressSuiteFinished,
		Counts:    counts,
		TotalCost: total,
	})
	return out
}

// fmtID names test i for a burst fixture, so a burst test's failure message
// can name which test in a large scripted sequence went missing.
func fmtID(i int) string {
	return fmt.Sprintf("test-%03d", i)
}
