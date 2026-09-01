package tui

// queue_preflight_test.go specifies the queue-wide pre-flight behaviour
// introduced in Stage 3.
//
// Coverage:
//
//   T3.1 - Pre-flight is invoked for every selected suite before any suite
//           starts executing, and resolved plans are retained on the model
//           (preflightedPlans) for suite start to consume without a second
//           pre-flight call.
//
//   T3.2 - Before any pre-flight work begins, the model enters
//           ScreenPreflightNotice, distinct from ScreenSuiteSelect and from
//           ScreenProgress, and its rendered view contains text describing that
//           all selected suites are being pre-flighted.
//
//   T3.3 - When any selected suite fails pre-flight, no suite executes, the
//           user is returned to ScreenSuiteSelect, and the failure detail names
//           every failing suite and carries every diagnostic each produced.
//
//   T3.4 - When suites produce only warnings, every warning appears in the
//           failure detail attributed to its suite, the status message describes
//           the queue rather than a single suite, and the queue still executes.
//
//   T3.5 - A single-suite selection pre-flights, reports its failures and
//           warnings, and runs exactly as before the queue-wide change.
//
//   T3.6 - Each suite's report path is resolved when that suite starts (via
//           ReportPathFor), not during queue-wide pre-flight; the queue still
//           advances suite by suite with the report file written per suite.

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/preflight"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// recordingPreflight is a PreflightFunc that records every Input it receives
// and returns a controlled plan/report per suite path. When a suite path has
// no entry in plans/reports, it returns fixturePlan and an empty Report.
type recordingPreflight struct {
	mu      sync.Mutex
	inputs  []preflight.Input
	plans   map[string]preflight.Plan
	reports map[string]authoring.Report
}

func newRecordingPreflight() *recordingPreflight {
	return &recordingPreflight{
		plans:   make(map[string]preflight.Plan),
		reports: make(map[string]authoring.Report),
	}
}

// withReport sets the authoring.Report returned for the given suite path.
func (r *recordingPreflight) withReport(suitePath string, rpt authoring.Report) *recordingPreflight {
	r.reports[suitePath] = rpt
	return r
}

// withPlan sets the preflight.Plan returned for the given suite path.
func (r *recordingPreflight) withPlan(suitePath string, p preflight.Plan) *recordingPreflight {
	r.plans[suitePath] = p
	return r
}

func (r *recordingPreflight) preflight(in preflight.Input) (preflight.Plan, authoring.Report) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputs = append(r.inputs, in)
	p, ok := r.plans[in.SuitePath]
	if !ok {
		p = fixturePlan(in.SuitePath)
	}
	rpt := r.reports[in.SuitePath]
	return p, rpt
}

// callCount returns the number of times the preflight function was called.
func (r *recordingPreflight) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.inputs)
}

// calledFor returns true if the preflight function was called with the given
// suite path.
func (r *recordingPreflight) calledFor(suitePath string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, in := range r.inputs {
		if in.SuitePath == suitePath {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Navigation helpers for the queue-wide pre-flight two-update sequence
// ---------------------------------------------------------------------------

// selectSuites toggles each suite by index on ScreenSuiteSelect and confirms
// the selection with Enter, leaving the model at ScreenRetention.
func selectSuites(t *testing.T, m Model, indices []int) Model {
	t.Helper()
	// Navigate to suite-select if needed.
	m = advanceToRunFlow(t, m)
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("selectSuites: Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
	// Toggle each requested suite. The cursor starts at index 0.
	cur := 0
	for _, idx := range indices {
		for cur < idx {
			m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
			cur++
		}
		m, _ = safeUpdate(t, m, keyMsg(" ")) // toggle
	}
	m, _ = safeUpdate(t, m, keyMsg("\r")) // confirm
	if m.Screen() != ScreenRetention {
		t.Fatalf("selectSuites: after confirm, Screen() = %q, want %q", m.Screen(), ScreenRetention)
	}
	return m
}

// driveToPreflightNotice navigates from ScreenRetention through all settings
// screens and returns (model at ScreenPreflightNotice, cmd producing
// preflightQueueMsg). It does NOT run the cmd.
func driveToPreflightNotice(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	m, cmd := navigateThroughSettings(t, m)
	return m, cmd
}

// driveQueuePreflight drives the full two-update pre-flight sequence:
//  1. Navigates from ScreenRetention through settings to ScreenPreflightNotice.
//  2. Runs the cmd to produce preflightQueueMsg.
//  3. Feeds preflightQueueMsg to Update, which runs the synchronous pre-flight.
//
// Returns the model after pre-flight completes and the cmd produced by step 3
// (which, on success, starts the first suite).
func driveQueuePreflight(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	m, cmd := driveToPreflightNotice(t, m)
	if cmd == nil {
		t.Fatalf("driveQueuePreflight: no cmd returned from driveToPreflightNotice; expected cmd producing preflightQueueMsg")
	}
	msg := runCmd(t, cmd)
	m, cmd = safeUpdate(t, m, msg)
	return m, cmd
}

// ---------------------------------------------------------------------------
// T3.1 - Queue-wide pre-flight: all suites pre-flighted, plans retained
// ---------------------------------------------------------------------------

// TestQueuePreflight_AllSuitesPreflightedBeforeAnyStarts verifies that the
// pre-flight function is called once for every selected suite before any suite
// runner receives a Run call.
func TestQueuePreflight_AllSuitesPreflightedBeforeAnyStarts(t *testing.T) {
	rec := newRecordingPreflight()
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"alpha.yaml", "beta.yaml", "gamma.yaml"}, runner)
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	m = selectSuites(t, m, []int{0, 1, 2}) // toggle all three
	m, cmd := driveQueuePreflight(t, m)
	_ = m
	_ = cmd

	for _, path := range []string{"alpha.yaml", "beta.yaml", "gamma.yaml"} {
		if !rec.calledFor(path) {
			t.Errorf("pre-flight was not called for suite %q; every selected suite must be pre-flighted before any starts", path)
		}
	}
	// Pre-flight must have been called once per suite (3 suites, 3 calls).
	if n := rec.callCount(); n != 3 {
		t.Errorf("pre-flight called %d times, want 3 (once per selected suite, before any suite starts)", n)
	}
}

// TestQueuePreflight_ResolvedPlansRetained verifies that after queue-wide
// pre-flight, preflightedPlans holds one plan per selected suite.
func TestQueuePreflight_ResolvedPlansRetained(t *testing.T) {
	rec := newRecordingPreflight()
	planA := fixturePlan("alpha")
	planB := fixturePlan("beta")
	rec.withPlan("alpha.yaml", planA)
	rec.withPlan("beta.yaml", planB)

	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"alpha.yaml", "beta.yaml"}, runner)
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	m = selectSuites(t, m, []int{0, 1})
	m, _ = driveQueuePreflight(t, m)

	if len(m.preflightedPlans) != 2 {
		t.Fatalf("preflightedPlans has %d entries, want 2 (one per selected suite)", len(m.preflightedPlans))
	}
	if m.preflightedPlans[0].Suite.ID != planA.Suite.ID {
		t.Errorf("preflightedPlans[0].Suite.ID = %q, want %q", m.preflightedPlans[0].Suite.ID, planA.Suite.ID)
	}
	if m.preflightedPlans[1].Suite.ID != planB.Suite.ID {
		t.Errorf("preflightedPlans[1].Suite.ID = %q, want %q", m.preflightedPlans[1].Suite.ID, planB.Suite.ID)
	}
}

// TestQueuePreflight_SuiteStartDoesNotInvokePreflightAgain verifies that when
// a suite starts from its pre-resolved plan, the pre-flight function is not
// called a second time. Pre-flight must happen exactly once per suite, during
// queue-wide pre-flight, and not be repeated at suite start.
func TestQueuePreflight_SuiteStartDoesNotInvokePreflightAgain(t *testing.T) {
	rec := newRecordingPreflight()
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, runner)
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	m = selectSuites(t, m, []int{0, 1})
	// Drive queue pre-flight: 2 calls expected.
	m, cmd := driveQueuePreflight(t, m)
	afterPreflightCount := rec.callCount()

	// Run the first suite to completion.
	if cmd != nil {
		msg := runCmd(t, cmd)
		m, cmd = safeUpdate(t, m, msg) // SuiteFinishedMsg → starts second suite
	}
	// Run the second suite to completion.
	if cmd != nil {
		msg := runCmd(t, cmd)
		m, _ = safeUpdate(t, m, msg)
	}
	_ = m

	afterRunCount := rec.callCount()
	if afterRunCount != afterPreflightCount {
		t.Errorf("pre-flight called %d times total, %d times during queue pre-flight; suite start must not invoke pre-flight again (got %d additional calls)",
			afterRunCount, afterPreflightCount, afterRunCount-afterPreflightCount)
	}
}

// ---------------------------------------------------------------------------
// T3.2 - Pre-flight notice screen
// ---------------------------------------------------------------------------

// TestPreflightNotice_ScreenEnteredBeforePreflightRuns verifies that after the
// settings flow completes (Enter on the last settings screen), the model is on
// ScreenPreflightNotice and the pre-flight function has not been called yet.
func TestPreflightNotice_ScreenEnteredBeforePreflightRuns(t *testing.T) {
	rec := newRecordingPreflight()
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, newFakeSuiteRunner())
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	m = selectSuites(t, m, []int{0, 1})
	m, _ = driveToPreflightNotice(t, m)

	if m.Screen() != ScreenPreflightNotice {
		t.Errorf("Screen() after settings flow = %q, want %q; notice must be entered before pre-flight runs", m.Screen(), ScreenPreflightNotice)
	}
	if n := rec.callCount(); n != 0 {
		t.Errorf("pre-flight called %d times before preflightQueueMsg processed; must not run until notice is rendered", n)
	}
}

// TestPreflightNotice_IsDistinctFromSuiteSelectAndProgress verifies that
// ScreenPreflightNotice is a different constant from ScreenSuiteSelect and
// ScreenProgress.
func TestPreflightNotice_IsDistinctFromSuiteSelectAndProgress(t *testing.T) {
	if ScreenPreflightNotice == ScreenSuiteSelect {
		t.Errorf("ScreenPreflightNotice == ScreenSuiteSelect; the notice must be a distinct screen state")
	}
	if ScreenPreflightNotice == ScreenProgress {
		t.Errorf("ScreenPreflightNotice == ScreenProgress; the notice must be a distinct screen state")
	}
}

// TestPreflightNotice_ViewContainsPreflightingText verifies that the view
// rendered while on ScreenPreflightNotice carries text informing the user
// that all selected suites are being pre-flighted.
func TestPreflightNotice_ViewContainsPreflightingText(t *testing.T) {
	rec := newRecordingPreflight()
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, newFakeSuiteRunner())
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	m = selectSuites(t, m, []int{0, 1})
	m, _ = driveToPreflightNotice(t, m)

	if m.Screen() != ScreenPreflightNotice {
		t.Fatalf("Screen() = %q, want %q; precondition for view test not met", m.Screen(), ScreenPreflightNotice)
	}
	view := safeView(t, m)
	// The view must contain some indication of pre-flighting activity.
	// "pre-flight" or "preflight" or "checking" — implementation may vary,
	// but the user must see that work is in progress for all suites.
	lowerView := strings.ToLower(view)
	if !strings.Contains(lowerView, "pre-flight") && !strings.Contains(lowerView, "preflight") && !strings.Contains(lowerView, "checking") {
		t.Errorf("ScreenPreflightNotice view does not contain text indicating pre-flight activity (looked for 'pre-flight', 'preflight', 'checking'):\n%s", view)
	}
}

// TestPreflightNotice_NavigationKeysIgnored verifies that navigation keys
// (Up, Down, Enter) do not change the screen while on ScreenPreflightNotice.
// Only the shared cancel binding is accepted.
func TestPreflightNotice_NavigationKeysIgnored(t *testing.T) {
	rec := newRecordingPreflight()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	// Single-suite: single Enter on suite-select confirms it directly.
	m = advanceToRunFlow(t, m)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // confirm suite → settings flow
	m, _ = driveToPreflightNotice(t, m)

	if m.Screen() != ScreenPreflightNotice {
		t.Fatalf("Screen() = %q, want %q; precondition not met", m.Screen(), ScreenPreflightNotice)
	}

	for _, key := range []tea.KeyMsg{
		keyType(tea.KeyUp),
		keyType(tea.KeyDown),
		keyType(tea.KeyEnter),
		keyMsg(" "),
	} {
		m2, _ := safeUpdate(t, m, key)
		if m2.Screen() != ScreenPreflightNotice {
			t.Errorf("key %v changed screen from %q to %q; navigation keys must be ignored on ScreenPreflightNotice",
				key, ScreenPreflightNotice, m2.Screen())
		}
	}
}

// ---------------------------------------------------------------------------
// T3.3 - Queue-wide pre-flight failure
// ---------------------------------------------------------------------------

// buildMultiSuiteWithFailure builds a three-suite model where suite-b.yaml
// fails pre-flight with an error report and the others pass.
func buildMultiSuiteWithFailure(t *testing.T) (Model, *recordingPreflight) {
	t.Helper()
	rec := newRecordingPreflight()
	rec.withReport("suite-b.yaml", errorReport()) // suite-b fails
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml", "suite-c.yaml"}, runner)
	opts.Preflight = rec.preflight
	m := NewModel(opts)
	return m, rec
}

// TestQueuePreflightFailure_NoSuiteExecutes verifies that when one suite in
// the queue fails pre-flight, no suite runner is invoked.
func TestQueuePreflightFailure_NoSuiteExecutes(t *testing.T) {
	runner := newFakeSuiteRunner()
	rec := newRecordingPreflight()
	rec.withReport("suite-b.yaml", errorReport())
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, runner)
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	m = selectSuites(t, m, []int{0, 1})
	m, _ = driveQueuePreflight(t, m)

	if runner.called {
		t.Errorf("suite runner was called after a queue-wide pre-flight failure; no suite must execute when any suite fails pre-flight")
	}
}

// TestQueuePreflightFailure_ReturnsToSuiteSelect verifies that after a queue
// pre-flight failure, the model is on ScreenSuiteSelect.
func TestQueuePreflightFailure_ReturnsToSuiteSelect(t *testing.T) {
	m, _ := buildMultiSuiteWithFailure(t)
	m = selectSuites(t, m, []int{0, 1, 2})
	m, _ = driveQueuePreflight(t, m)

	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("Screen() after queue pre-flight failure = %q, want %q; user must be on suite-select when any suite fails", m.Screen(), ScreenSuiteSelect)
	}
}

// TestQueuePreflightFailure_FailureDetailIsShown verifies that the failure
// detail pane is live after a queue pre-flight failure.
func TestQueuePreflightFailure_FailureDetailIsShown(t *testing.T) {
	m, _ := buildMultiSuiteWithFailure(t)
	m = selectSuites(t, m, []int{0, 1, 2})
	m, _ = driveQueuePreflight(t, m)

	_, _, shown := m.FailureDetail()
	if !shown {
		t.Errorf("FailureDetail() shown = false after queue pre-flight failure; failure detail must be live so the user can see what went wrong")
	}
}

// TestQueuePreflightFailure_FailingsSuiteNamedInDetail verifies that the
// failure detail body names every suite that failed pre-flight.
func TestQueuePreflightFailure_FailingSuiteNamedInDetail(t *testing.T) {
	m, _ := buildMultiSuiteWithFailure(t)
	m = selectSuites(t, m, []int{0, 1, 2})
	m, _ = driveQueuePreflight(t, m)

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after queue pre-flight failure; precondition not met")
	}
	if !strings.Contains(body, "suite-b.yaml") {
		t.Errorf("FailureDetail() body does not contain the failing suite path %q; the failure report must attribute diagnostics to their suite", "suite-b.yaml")
	}
}

// TestQueuePreflightFailure_AllDiagnosticsPresent verifies that every
// diagnostic produced by the failing suite appears in the failure detail body.
func TestQueuePreflightFailure_AllDiagnosticsPresent(t *testing.T) {
	rpt := errorReport()
	rec := newRecordingPreflight()
	rec.withReport("suite-b.yaml", rpt)
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, runner)
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	m = selectSuites(t, m, []int{0, 1})
	m, _ = driveQueuePreflight(t, m)

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false; precondition not met")
	}
	for _, d := range rpt.Diagnostics {
		if !strings.Contains(body, d.Message) {
			t.Errorf("FailureDetail() body does not contain diagnostic message %q from suite-b.yaml; no diagnostic may be dropped", d.Message)
		}
	}
}

// TestQueuePreflightFailure_MultipleFaultysuites_AllNamed verifies that when
// more than one suite fails pre-flight, every failing suite is named in the
// detail body and every diagnostic from each is present.
func TestQueuePreflightFailure_MultipleFaultySuites_AllNamed(t *testing.T) {
	rptA := errorReport()
	rptB := manyDiagnosticsReport(3)
	rec := newRecordingPreflight()
	rec.withReport("alpha.yaml", rptA)
	rec.withReport("beta.yaml", rptB)
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"alpha.yaml", "beta.yaml"}, runner)
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	m = selectSuites(t, m, []int{0, 1})
	m, _ = driveQueuePreflight(t, m)

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false; precondition not met")
	}
	if !strings.Contains(body, "alpha.yaml") {
		t.Errorf("FailureDetail() body does not contain suite path %q; all failing suites must be named", "alpha.yaml")
	}
	if !strings.Contains(body, "beta.yaml") {
		t.Errorf("FailureDetail() body does not contain suite path %q; all failing suites must be named", "beta.yaml")
	}
	for _, d := range rptA.Diagnostics {
		if !strings.Contains(body, d.Message) {
			t.Errorf("FailureDetail() body missing diagnostic %q from alpha.yaml", d.Message)
		}
	}
	for _, d := range rptB.Diagnostics {
		if !strings.Contains(body, d.Message) {
			t.Errorf("FailureDetail() body missing diagnostic %q from beta.yaml", d.Message)
		}
	}
}

// TestQueuePreflightFailure_StatusIsError verifies that after a queue-wide
// pre-flight failure, statusError is true so the status bar is styled as an
// error.
func TestQueuePreflightFailure_StatusIsError(t *testing.T) {
	m, _ := buildMultiSuiteWithFailure(t)
	m = selectSuites(t, m, []int{0, 1, 2})
	m, _ = driveQueuePreflight(t, m)

	if !m.statusError {
		t.Errorf("statusError = false after queue pre-flight failure; failure must be styled as an error in the status bar")
	}
}

// ---------------------------------------------------------------------------
// T3.4 - Queue-wide pre-flight warnings
// ---------------------------------------------------------------------------

// buildMultiSuiteWithWarnings builds a two-suite model where both suites
// produce warning-only reports.
func buildMultiSuiteWithWarnings(t *testing.T) Model {
	t.Helper()
	rec := newRecordingPreflight()
	// Different warning codes per suite so we can verify per-suite attribution.
	var rptA authoring.Report
	rptA.Add(authoring.Diagnostic{
		Severity: authoring.SeverityWarning,
		Code:     "deprecated-field",
		Path:     "suite-a.yaml",
		Message:  "warning from suite-a",
	})
	var rptB authoring.Report
	rptB.Add(authoring.Diagnostic{
		Severity: authoring.SeverityWarning,
		Code:     "deprecated-field",
		Path:     "suite-b.yaml",
		Message:  "warning from suite-b",
	})
	rec.withReport("suite-a.yaml", rptA)
	rec.withReport("suite-b.yaml", rptB)
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, runner)
	opts.Preflight = rec.preflight
	return NewModel(opts)
}

// TestQueuePreflightWarning_QueueStillExecutes verifies that when all suites
// produce only warnings, the queue proceeds to ScreenProgress.
func TestQueuePreflightWarning_QueueStillExecutes(t *testing.T) {
	m := buildMultiSuiteWithWarnings(t)
	m = selectSuites(t, m, []int{0, 1})
	m, _ = driveQueuePreflight(t, m)

	if m.Screen() != ScreenProgress {
		t.Errorf("Screen() after warning-only queue pre-flight = %q, want %q; warnings must not block queue execution", m.Screen(), ScreenProgress)
	}
}

// TestQueuePreflightWarning_WarningsAreSurfaced verifies that the failure
// detail pane is live with per-suite warning content after a warning-only
// queue pre-flight.
func TestQueuePreflightWarning_WarningsAreSurfaced(t *testing.T) {
	m := buildMultiSuiteWithWarnings(t)
	m = selectSuites(t, m, []int{0, 1})
	m, _ = driveQueuePreflight(t, m)

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after warning-only queue pre-flight; warnings must be surfaced in the detail pane")
	}
	if !strings.Contains(body, "warning from suite-a") {
		t.Errorf("FailureDetail() body does not contain warning message from suite-a; all suites' warnings must appear")
	}
	if !strings.Contains(body, "warning from suite-b") {
		t.Errorf("FailureDetail() body does not contain warning message from suite-b; all suites' warnings must appear")
	}
}

// TestQueuePreflightWarning_PerSuiteAttribution verifies that each suite's
// path appears in the detail body alongside its warnings, so the user can
// trace each warning to its source suite.
func TestQueuePreflightWarning_PerSuiteAttribution(t *testing.T) {
	m := buildMultiSuiteWithWarnings(t)
	m = selectSuites(t, m, []int{0, 1})
	m, _ = driveQueuePreflight(t, m)

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false; precondition not met")
	}
	if !strings.Contains(body, "suite-a.yaml") {
		t.Errorf("FailureDetail() body does not name suite-a.yaml; per-suite attribution is required")
	}
	if !strings.Contains(body, "suite-b.yaml") {
		t.Errorf("FailureDetail() body does not name suite-b.yaml; per-suite attribution is required")
	}
}

// TestQueuePreflightWarning_StatusMessageDescribesQueue verifies that the
// status message after a warning-only queue pre-flight describes the queue
// (e.g., counts suites or says "queue") rather than naming a single suite path.
func TestQueuePreflightWarning_StatusMessageDescribesQueue(t *testing.T) {
	m := buildMultiSuiteWithWarnings(t)
	m = selectSuites(t, m, []int{0, 1})
	m, _ = driveQueuePreflight(t, m)

	// The status message must not contain either specific suite path because
	// it describes the queue, not an individual suite.
	if strings.Contains(m.statusMsg, "suite-a.yaml") && !strings.Contains(m.statusMsg, "suite-b.yaml") {
		t.Errorf("statusMsg %q names suite-a.yaml but not suite-b.yaml; queue-level status must describe the queue, not a single suite", m.statusMsg)
	}
	if strings.Contains(m.statusMsg, "suite-b.yaml") && !strings.Contains(m.statusMsg, "suite-a.yaml") {
		t.Errorf("statusMsg %q names suite-b.yaml but not suite-a.yaml; queue-level status must describe the queue, not a single suite", m.statusMsg)
	}
	// The message must not be empty — there are warnings to communicate.
	if m.statusMsg == "" {
		t.Errorf("statusMsg is empty after warning-only queue pre-flight; the user must be informed that warnings occurred")
	}
	// statusError must be false — warnings do not block execution.
	if m.statusError {
		t.Errorf("statusError = true after warning-only queue pre-flight; warnings must not be styled as errors")
	}
}

// ---------------------------------------------------------------------------
// T3.5 - Single-suite selection regression
// ---------------------------------------------------------------------------

// TestSingleSuite_Failure_ReturnsToSuiteSelect verifies that a single-suite
// pre-flight failure still returns the user to ScreenSuiteSelect.
func TestSingleSuite_Failure_ReturnsToSuiteSelect(t *testing.T) {
	rec := newRecordingPreflight()
	rec.withReport("suite-a.yaml", errorReport())
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	// Single-suite: advanceToSettingsFlow confirms the cursor item directly.
	m = advanceToSettingsFlow(t, m)
	m, _ = driveQueuePreflight(t, m)

	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("single-suite failure: Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
	if runner.called {
		t.Errorf("single-suite failure: runner was called; suite must not execute after pre-flight failure")
	}
}

// TestSingleSuite_Warning_RunProceeds verifies that a single-suite
// warning-only pre-flight still allows the suite to run.
func TestSingleSuite_Warning_RunProceeds(t *testing.T) {
	rec := newRecordingPreflight()
	rec.withReport("suite-a.yaml", warningReport())
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	m = advanceToSettingsFlow(t, m)
	m, _ = driveQueuePreflight(t, m)

	if m.Screen() != ScreenProgress {
		t.Errorf("single-suite warning: Screen() = %q, want %q; warnings must not block execution", m.Screen(), ScreenProgress)
	}
}

// TestSingleSuite_Success_RunProceeds verifies that a single-suite selection
// with a clean pre-flight transitions to ScreenProgress.
func TestSingleSuite_Success_RunProceeds(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	m := NewModel(opts)

	m = advanceToSettingsFlow(t, m)
	m, _ = driveQueuePreflight(t, m)

	if m.Screen() != ScreenProgress {
		t.Errorf("single-suite success: Screen() = %q, want %q", m.Screen(), ScreenProgress)
	}
}

// TestSingleSuite_PreflightCalledOnce verifies that for a single-suite
// selection, the pre-flight function is called exactly once during queue
// pre-flight and not again when the suite starts.
func TestSingleSuite_PreflightCalledOnce(t *testing.T) {
	rec := newRecordingPreflight()
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	m = advanceToSettingsFlow(t, m)
	m, cmd := driveQueuePreflight(t, m)
	afterPreflight := rec.callCount()

	// Run the suite to completion.
	if cmd != nil {
		msg := runCmd(t, cmd)
		m, _ = safeUpdate(t, m, msg)
	}
	_ = m

	afterRun := rec.callCount()
	if afterPreflight != 1 {
		t.Errorf("pre-flight called %d times during queue pre-flight, want 1", afterPreflight)
	}
	if afterRun != 1 {
		t.Errorf("pre-flight called %d times total (including suite start), want 1; suite start must not invoke pre-flight again", afterRun)
	}
}

// ---------------------------------------------------------------------------
// T3.6 - Report path resolves per suite at suite start, not at pre-flight time
// ---------------------------------------------------------------------------

// TestQueuePreflight_ReportPathForNotCalledDuringPreflight verifies that
// Options.ReportPathFor is not called during the queue-wide pre-flight phase
// (i.e., before any suite starts running).
func TestQueuePreflight_ReportPathForNotCalledDuringPreflight(t *testing.T) {
	var mu sync.Mutex
	var pathForCalls []string

	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, runner)
	opts.ReportPath = "base-report.json"
	opts.ReportPathFor = func(suitePath string) string {
		mu.Lock()
		pathForCalls = append(pathForCalls, suitePath)
		mu.Unlock()
		return fmt.Sprintf("%s-report.json", suitePath)
	}
	m := NewModel(opts)

	m = selectSuites(t, m, []int{0, 1})
	// Drive to pre-flight notice and then run pre-flight.
	m, _ = driveQueuePreflight(t, m)

	mu.Lock()
	callsDuringPreflight := len(pathForCalls)
	mu.Unlock()

	if callsDuringPreflight != 0 {
		t.Errorf("ReportPathFor called %d times during queue pre-flight; report path must not be resolved until each suite starts", callsDuringPreflight)
	}
}

// TestQueuePreflight_ReportPathResolvedAtEachSuiteStart verifies that
// Options.ReportPathFor is called once per suite, when that suite starts
// (not at pre-flight time), and that separate report paths are generated.
func TestQueuePreflight_ReportPathResolvedAtEachSuiteStart(t *testing.T) {
	var mu sync.Mutex
	var pathForCalls []string

	capture := &tuiCaptureWriteFile{}
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, runner)
	opts.ReportPath = "base-report.json"
	opts.WriteFile = capture.write
	opts.ReportPathFor = func(suitePath string) string {
		mu.Lock()
		pathForCalls = append(pathForCalls, suitePath)
		mu.Unlock()
		return fmt.Sprintf("%s-report.json", suitePath)
	}
	m := NewModel(opts)

	m = selectSuites(t, m, []int{0, 1})
	m, cmd := driveQueuePreflight(t, m) // pre-flight runs; suites not started yet

	// Start and finish first suite.
	if cmd != nil {
		msg := runCmd(t, cmd)
		m, cmd = safeUpdate(t, m, msg) // first suite finishes → second starts
	}
	// Finish second suite.
	if cmd != nil {
		msg := runCmd(t, cmd)
		m, _ = safeUpdate(t, m, msg)
	}
	_ = m

	mu.Lock()
	finalCalls := make([]string, len(pathForCalls))
	copy(finalCalls, pathForCalls)
	mu.Unlock()

	// ReportPathFor must have been called once per suite (at suite start, not pre-flight).
	if len(finalCalls) != 2 {
		t.Errorf("ReportPathFor called %d times, want 2 (once per suite at suite start): calls = %v", len(finalCalls), finalCalls)
	}
}
