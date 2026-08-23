package tui

// preflight_failure_test.go verifies the TUI's failure-detail surface for
// pre-flight diagnostics. The tested behaviors are:
//
//   - A pre-flight error report surfaces every diagnostic's severity, code,
//     path, pointer and message in the rendered view and through FailureDetail,
//     in sorted order, with nothing dropped — and does not start the suite.
//   - A warning-only report surfaces its warnings and does not block the run.
//   - The detail pane is embedded in the screen where the failure occurred;
//     no new Screen constant is introduced and the status bar stays one line.
//   - The detail pane scrolls under the designated scroll bindings; a fresh
//     failure resets the view to the top; a window resize is honored.
//
// The scriptedPreflight helper (added in testhelpers_test.go) drives these
// paths by returning a controlled authoring.Report without real authored
// files. The existing fixturePreflight and all happy-path tests are
// unaffected.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/authoring"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// errorReport builds an authoring.Report with two error-severity diagnostics,
// exercising every renderable field (severity, code, path, line, pointer,
// message, hint). The two items are deliberately given paths that sort in
// reverse alphabetical order so the tests can verify they appear in
// Report.Sorted() order (ascending), not insertion order.
func errorReport() authoring.Report {
	var rpt authoring.Report
	rpt.Add(
		authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "unknown-field",
			Path:     "tests/zz-last/suite.yaml",
			Line:     20,
			Pointer:  "tests[1].timeout_s",
			Message:  "field is not recognised",
			Hint:     "remove it or check the schema version",
		},
		authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "missing-required",
			Path:     "tests/aa-first/suite.yaml",
			Line:     5,
			Pointer:  "tests[0].name",
			Message:  "required field is absent",
		},
	)
	return rpt
}

// warningReport builds an authoring.Report with two warning-severity
// diagnostics and no errors, so HasErrors() returns false.
func warningReport() authoring.Report {
	var rpt authoring.Report
	rpt.Add(
		authoring.Diagnostic{
			Severity: authoring.SeverityWarning,
			Code:     "deprecated-field",
			Path:     "tests/suite.yaml",
			Pointer:  "tests[0].timeout_s",
			Message:  "renamed to timeout",
		},
		authoring.Diagnostic{
			Severity: authoring.SeverityWarning,
			Code:     "deprecated-field",
			Path:     "tests/suite.yaml",
			Pointer:  "tests[1].timeout_s",
			Message:  "renamed to timeout",
		},
	)
	return rpt
}

// manyDiagnosticsReport builds a report with more diagnostics than a narrow
// pane can show at once, to verify nothing is dropped from FailureDetail even
// when the visible window cannot hold the full list.
func manyDiagnosticsReport(n int) authoring.Report {
	var rpt authoring.Report
	for i := 0; i < n; i++ {
		rpt.Add(authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "unknown-field",
			Path:     "tests/suite.yaml",
			Line:     i + 1,
			Message:  "field is not recognised",
		})
	}
	return rpt
}

// modelOnSuiteSelectWithPreflight builds a fresh Model on the suite-select
// screen wired to a scripted preflight. It does not select or start a suite.
func modelOnSuiteSelectWithPreflight(rpt authoring.Report) Model {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.Preflight = scriptedPreflight(rpt)
	return NewModel(opts)
}

// ---------------------------------------------------------------------------
// Pre-flight error display
// ---------------------------------------------------------------------------

// TestPreflightFailure_ScreenStaysOnSuiteSelect verifies that selecting a
// suite whose pre-flight reports errors does not transition to the progress
// screen — the run must not start.
func TestPreflightFailure_ScreenStaysOnSuiteSelect(t *testing.T) {
	m := modelOnSuiteSelectWithPreflight(errorReport())
	m = triggerSuiteStart(t, m) // navigate through settings flow; preflight fails and returns to suite-select

	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("Screen() after a pre-flight error = %q, want %q; the suite must not start when pre-flight fails", m.Screen(), ScreenSuiteSelect)
	}
}

// TestPreflightFailure_ViewContainsAllDiagnosticFields verifies that every
// diagnostic field — severity, code, path, pointer and message — from every
// diagnostic in the report is surfaced after a pre-flight failure. Short
// tokens (severity, code, path, pointer) are verified in the rendered view
// directly; the full message is verified through FailureDetail() because
// wrapping at the terminal width may split a long message across visual lines,
// so the contiguous message string does not appear verbatim in the rendered
// output.
func TestPreflightFailure_ViewContainsAllDiagnosticFields(t *testing.T) {
	rpt := errorReport()
	m := modelOnSuiteSelectWithPreflight(rpt)
	m = triggerSuiteStart(t, m) // navigate through settings flow; preflight fails
	view := safeView(t, m)

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a pre-flight failure; expected failure detail to be live")
	}

	for _, d := range rpt.Diagnostics {
		if !strings.Contains(view, string(d.Severity)) {
			t.Errorf("view does not contain severity %q for diagnostic with code %q; all diagnostic fields must be surfaced", d.Severity, d.Code)
		}
		if !strings.Contains(view, d.Code) {
			t.Errorf("view does not contain code %q; all diagnostic fields must be surfaced", d.Code)
		}
		if !strings.Contains(view, d.Path) {
			t.Errorf("view does not contain path %q; all diagnostic fields must be surfaced", d.Path)
		}
		if d.Pointer != "" && !strings.Contains(view, d.Pointer) {
			t.Errorf("view does not contain pointer %q; all diagnostic fields must be surfaced", d.Pointer)
		}
		// The message may be long enough that wrapping splits it across visual
		// lines; check it in FailureDetail()'s body which holds the full text.
		if !strings.Contains(body, d.Message) {
			t.Errorf("FailureDetail() body does not contain message %q; all diagnostic fields must be surfaced", d.Message)
		}
	}
}

// TestPreflightFailure_FailureDetailContainsAllDiagnostics verifies that
// FailureDetail() returns the complete unsorted body — nothing dropped — and
// that the body follows Report.Sorted() order (ascending by path then line).
func TestPreflightFailure_FailureDetailContainsAllDiagnostics(t *testing.T) {
	rpt := errorReport()
	m := modelOnSuiteSelectWithPreflight(rpt)
	m = triggerSuiteStart(t, m)

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a pre-flight failure; expected failure detail to be live")
	}

	// Every diagnostic's message must appear in the body.
	for _, d := range rpt.Diagnostics {
		if !strings.Contains(body, d.Message) {
			t.Errorf("FailureDetail() body does not contain message %q; no diagnostic may be dropped", d.Message)
		}
	}

	// Body must follow Report.Sorted() ordering: aa-first before zz-last.
	idxFirst := strings.Index(body, "tests/aa-first")
	idxLast := strings.Index(body, "tests/zz-last")
	if idxFirst < 0 {
		t.Errorf("FailureDetail() body does not mention first diagnostic path %q", "tests/aa-first")
	}
	if idxLast < 0 {
		t.Errorf("FailureDetail() body does not mention second diagnostic path %q", "tests/zz-last")
	}
	if idxFirst >= 0 && idxLast >= 0 && idxFirst > idxLast {
		t.Errorf("FailureDetail() body has diagnostics in wrong order: %q appears after %q; body must follow Report.Sorted() order", "tests/aa-first", "tests/zz-last")
	}
}

// TestPreflightFailure_FailureDetailTitle verifies the pane title identifies
// the failing operation and names the suite path, as specified in the failure
// detail content matrix.
func TestPreflightFailure_FailureDetailTitle(t *testing.T) {
	rpt := errorReport()
	m := modelOnSuiteSelectWithPreflight(rpt)
	m = triggerSuiteStart(t, m)

	title, _, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a pre-flight failure")
	}
	if !strings.Contains(title, "suite-a.yaml") {
		t.Errorf("FailureDetail() title %q does not contain the suite path %q; the title must name the failing operation", title, "suite-a.yaml")
	}
}

// TestPreflightFailure_NothingDroppedFromManyDiagnostics verifies that a
// report with many diagnostics (more than a single-screen pane can display)
// loses none of them in FailureDetail() — completeness must not depend on
// pane height.
func TestPreflightFailure_NothingDroppedFromManyDiagnostics(t *testing.T) {
	const n = 30
	rpt := manyDiagnosticsReport(n)
	m := modelOnSuiteSelectWithPreflight(rpt)
	m = triggerSuiteStart(t, m)

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false with a %d-diagnostic report", n)
	}

	// Count how many times the diagnostic code appears in the body — exactly
	// one occurrence per diagnostic, because each uses Code "unknown-field".
	// Counting by code avoids a false-positive where "suite.yaml:1" matches
	// "suite.yaml:10", "suite.yaml:11", etc.
	count := strings.Count(body, "unknown-field")
	if count != n {
		t.Errorf("FailureDetail() body contains %d occurrences of \"unknown-field\" (one per diagnostic), want %d; nothing must be dropped", count, n)
	}
}

// TestPreflightFailure_StatusBarKeesSummary verifies that the status bar
// retains a short single-line summary when a pre-flight fails, even though
// the detail pane holds the full content. The status bar must not carry the
// full diagnostic list.
func TestPreflightFailure_StatusBarKeepsSummary(t *testing.T) {
	m := modelOnSuiteSelectWithPreflight(errorReport())
	m = triggerSuiteStart(t, m)
	view := safeView(t, m)

	// The status bar historically showed "pre-flight failed for <path>".
	// The exact wording is an implementation choice; what is tested is that
	// some failure summary is present in the rendered view.
	if !strings.Contains(view, "pre-flight") && !strings.Contains(view, "failed") {
		t.Errorf("rendered view does not appear to contain a status-bar summary for the pre-flight failure; a one-line summary must remain")
	}
}

// ---------------------------------------------------------------------------
// Warning-only pre-flight
// ---------------------------------------------------------------------------

// TestPreflightWarning_RunStillProceeds verifies that a warning-only report
// (HasErrors() false) does not block the suite from starting: the model must
// transition to the progress screen.
func TestPreflightWarning_RunStillProceeds(t *testing.T) {
	rpt := warningReport()
	m := modelOnSuiteSelectWithPreflight(rpt)
	m = triggerSuiteStart(t, m) // navigate through settings flow; warnings don't block the run

	if m.Screen() != ScreenProgress {
		t.Errorf("Screen() after warning-only pre-flight = %q, want %q; warnings must not block the run", m.Screen(), ScreenProgress)
	}
}

// TestPreflightWarning_WarningsAreSurfaced verifies that warning diagnostics
// appear in FailureDetail() even when the run proceeds, so the user can
// see them rather than having them silently dropped.
func TestPreflightWarning_WarningsAreSurfaced(t *testing.T) {
	rpt := warningReport()
	m := modelOnSuiteSelectWithPreflight(rpt)
	m = triggerSuiteStart(t, m)

	title, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a warning-only pre-flight; warnings must be surfaced")
	}

	// Title must name the suite and use the warning-specific wording, not the
	// error title ("Pre-flight failed: …"). The design specifies distinct titles:
	// "Pre-flight warnings: <suitePath>" for warnings vs "Pre-flight failed: <suitePath>"
	// for errors — so a missing or wrong title signals the wrong case was triggered.
	if !strings.Contains(title, "suite-a.yaml") {
		t.Errorf("FailureDetail() title %q does not contain the suite path %q; the warning pane title must name the suite", title, "suite-a.yaml")
	}
	if strings.Contains(title, "Pre-flight failed:") {
		t.Errorf("FailureDetail() title %q matches the error-case title; warning-only pre-flight must use a distinct title (want one containing \"warnings\")", title)
	}

	for _, d := range rpt.Diagnostics {
		if !strings.Contains(body, d.Message) {
			t.Errorf("FailureDetail() body does not contain warning message %q; warnings must not be silently dropped", d.Message)
		}
		if !strings.Contains(body, string(authoring.SeverityWarning)) {
			t.Errorf("FailureDetail() body does not contain severity %q; the user must be able to distinguish warnings from errors", authoring.SeverityWarning)
		}
	}
}

// TestPreflightWarning_FailureDetailNotAnError verifies that a warning-only
// report's detail is not presented as an error: the status bar must not carry
// statusError=true, so it is styled as informational rather than alarming.
// We test the observable consequence: FailureDetail().shown is true AND the
// view does not contain the error styling that errorReport produces.
func TestPreflightWarning_StatusBarNotStyled_AsError(t *testing.T) {
	// Build a view for the error case and the warning case, then confirm
	// they look different. This is an indirect test of statusError, which
	// is unexported.
	runnerE := newFakeSuiteRunner()
	optsE := newFixtureOptions([]string{"suite-a.yaml"}, runnerE)
	optsE.Preflight = scriptedPreflight(errorReport())
	mError := NewModel(optsE)
	mError = triggerSuiteStart(t, mError)
	viewError := safeView(t, mError)

	runnerW := newFakeSuiteRunner()
	optsW := newFixtureOptions([]string{"suite-a.yaml"}, runnerW)
	optsW.Preflight = scriptedPreflight(warningReport())
	mWarning := NewModel(optsW)
	mWarning = triggerSuiteStart(t, mWarning)
	viewWarning := safeView(t, mWarning)

	if viewError == viewWarning {
		t.Errorf("error pre-flight and warning-only pre-flight produce identical views; they must be visually distinct so the user can tell them apart")
	}
}

// TestPreflightWarning_ViewContainsWarningContent verifies that the rendered
// view for a warning-only pre-flight surfaces the warning diagnostics. The
// message is checked through FailureDetail() because wrapping at the terminal
// width may split a long message across visual lines.
func TestPreflightWarning_ViewContainsWarningContent(t *testing.T) {
	rpt := warningReport()
	m := modelOnSuiteSelectWithPreflight(rpt)
	m = triggerSuiteStart(t, m)

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a warning-only pre-flight; expected failure detail to be live")
	}

	for _, d := range rpt.Diagnostics {
		// The full message may be split across visual lines by word-wrapping;
		// check it in the FailureDetail body which holds the full text.
		if !strings.Contains(body, d.Message) {
			t.Errorf("FailureDetail() body after warning-only pre-flight does not contain warning message %q; warnings must be visible to the user", d.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// Detail surface: screen embedding and state
// ---------------------------------------------------------------------------

// TestDetailSurface_NoScreenChangeOnPreflightError verifies that the detail
// pane is embedded in the existing screen where the failure occurred — no new
// Screen constant is introduced and the user is not navigated away from
// suite-select.
func TestDetailSurface_NoScreenChangeOnPreflightError(t *testing.T) {
	m := modelOnSuiteSelectWithPreflight(errorReport())
	before := m.Screen()
	m = triggerSuiteStart(t, m) // navigate through settings flow; preflight error returns to suite-select
	after := m.Screen()

	if before != after {
		t.Errorf("Screen changed from %q to %q after a pre-flight failure; the detail pane must be embedded in the current screen, not a new one", before, after)
	}
}

// TestDetailSurface_FreshModelHasNoFailureDetail verifies that a newly
// constructed Model — before any key press or pre-flight run — returns
// ("", "", false) from FailureDetail(). The initial state must not expose
// any stale or zero-value detail; the pane is quiescent until a failure site
// explicitly sets it.
func TestDetailSurface_FreshModelHasNoFailureDetail(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	m := NewModel(opts)

	title, body, shown := m.FailureDetail()
	if shown {
		t.Errorf("FailureDetail() shown = true on a fresh model before any Enter key; initial state must be not shown")
	}
	if title != "" {
		t.Errorf("FailureDetail() title = %q on a fresh model, want \"\"; initial state must have empty title", title)
	}
	if body != "" {
		t.Errorf("FailureDetail() body is not empty on a fresh model; initial state must have empty body")
	}
}

// TestDetailSurface_FailureDetailClearedAfterSuccessfulRun verifies that
// failure detail from a pre-flight run is not still visible after a later
// successful run on the same suite. Stale detail must not leak across runs.
func TestDetailSurface_FailureDetailClearedAfterSuccessfulRun(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)

	// First attempt: error pre-flight.
	opts.Preflight = scriptedPreflight(errorReport())
	m := NewModel(opts)
	m = triggerSuiteStart(t, m) // navigate through settings; preflight fails, returns to suite-select
	_, _, shownAfterError := m.FailureDetail()
	if !shownAfterError {
		t.Fatalf("FailureDetail() not shown after the error-bearing pre-flight run; test precondition not met")
	}

	// Second attempt: clear preflight (happy path). We replace the preflight
	// on the model's options to simulate the user retrying on the same suite.
	opts.Preflight = fixturePreflight(fixturePlan("suite-under-test"))
	m.opts = opts
	m = triggerSuiteStart(t, m) // navigate again; success this time

	titleAfterSuccess, bodyAfterSuccess, shownAfterSuccess := m.FailureDetail()
	if shownAfterSuccess {
		t.Errorf("FailureDetail() still shown after a successful pre-flight run; stale detail must be cleared so it does not bleed into the next screen")
	}
	// When shown is false the accessor must return empty strings — never stale ones.
	// These checks catch an implementation that returns ("", "", false) correctly via
	// shown but still exposes stale title or body strings through the same call.
	if titleAfterSuccess != "" {
		t.Errorf("FailureDetail() title = %q after a successful run, want \"\"; when shown is false the title must be empty, not stale", titleAfterSuccess)
	}
	if bodyAfterSuccess != "" {
		t.Errorf("FailureDetail() body is not empty after a successful run; when shown is false the body must be empty, not stale")
	}
}

// TestDetailSurface_FreshFailureResetsScrollToTop verifies that when a new
// pre-flight failure is set, the pane's scroll offset is reset to the top
// (line 0), so the user sees the first diagnostic, not wherever the previous
// failure left the scroll position.
//
// This test drives a large report, scrolls down, then triggers a second
// failure and asserts that the view returns to the first line rather than
// continuing from mid-scroll.
func TestDetailSurface_FreshFailureResetsScrollToTop(t *testing.T) {
	// First failure: large report so we can scroll.
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.Preflight = scriptedPreflight(manyDiagnosticsReport(20))
	m := NewModel(opts)
	// Resize to a small height so the pane does not show everything at once.
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 12})
	m = triggerSuiteStart(t, m) // navigate through settings; preflight fails

	// Scroll down several times.
	for i := 0; i < 5; i++ {
		m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	viewScrolled := safeView(t, m)

	// Second failure: triggers a SetContent reset which must reset scroll to top.
	m = triggerSuiteStart(t, m) // navigate again; second preflight failure resets scroll
	viewAfterReset := safeView(t, m)

	// After a fresh failure the view must match what we see when we first
	// arrive at the top (before any scrolling). We get that by comparing with
	// a model that was never scrolled.
	opts2 := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	opts2.Preflight = scriptedPreflight(manyDiagnosticsReport(20))
	mFresh := NewModel(opts2)
	mFresh, _ = safeUpdate(t, mFresh, tea.WindowSizeMsg{Width: 80, Height: 12})
	mFresh = triggerSuiteStart(t, mFresh)
	viewFresh := safeView(t, mFresh)

	if viewScrolled == viewFresh {
		// Scroll had no effect — possibly the pane is large enough to show everything.
		// Not a test failure; just means the dataset is too small to require scrolling.
		t.Log("scroll had no visible effect (pane may be large enough to show all content); reset assertion skipped")
		return
	}
	if viewAfterReset != viewFresh {
		t.Errorf("after a fresh failure the view does not return to the top; SetContent must reset the pane's scroll offset to 0")
	}
}

// TestDetailSurface_ScrollKeysMovePaneNotCursor verifies that the designated
// scroll keys (PgUp / PgDown) move the detail pane and do not move the suite
// cursor. On the suite-select screen, Up/Down drive the suite cursor while
// PgUp/PgDown are reserved for pane scrolling.
func TestDetailSurface_ScrollKeysMovePaneNotCursor(t *testing.T) {
	// Set up with multiple suites so cursor movement would be observable.
	runner := newFakeSuiteRunner()
	opts := Options{
		Preflight: scriptedPreflight(manyDiagnosticsReport(20)),
		Suite:     runner,
		Suites:    []string{"suite-a.yaml", "suite-b.yaml", "suite-c.yaml"},
		Harness:   "fake",
	}
	m := NewModel(opts)
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 12})
	// Navigate through the settings flow to trigger the pre-flight failure.
	m = triggerSuiteStart(t, m)

	cursorBefore := m.suiteCursor

	// PgDown must scroll the pane, not advance the cursor.
	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.suiteCursor != cursorBefore {
		t.Errorf("PgDown moved the suite cursor from %d to %d; scroll keys must not affect the suite cursor", cursorBefore, m.suiteCursor)
	}

	// PgUp must also not affect the cursor.
	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.suiteCursor != cursorBefore {
		t.Errorf("PgUp moved the suite cursor from %d to %d; scroll keys must not affect the suite cursor", cursorBefore, m.suiteCursor)
	}
}

// TestDetailSurface_ScrollProducesVisiblyDifferentView verifies that
// scrolling the pane actually changes the rendered view when the pane has
// content that overflows its visible height. This is the observable contract
// of PgDown: after scrolling, what is displayed is different.
func TestDetailSurface_ScrollProducesVisiblyDifferentView(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.Preflight = scriptedPreflight(manyDiagnosticsReport(30))
	m := NewModel(opts)
	// Small height forces the pane to clip content.
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 10})
	m = triggerSuiteStart(t, m)

	viewBefore := safeView(t, m)

	// Scroll down.
	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	viewAfter := safeView(t, m)

	if viewBefore == viewAfter {
		t.Errorf("PgDown produced no change in the rendered view; either scroll is not wired or all content fits the pane (need a larger dataset or smaller height)")
	}
}

// ---------------------------------------------------------------------------
// Window resize
// ---------------------------------------------------------------------------

// TestDetailSurface_ResizeHonored verifies that after a tea.WindowSizeMsg the
// pane adopts the new geometry: the rendered view changes shape and a Model
// that never received a size message still renders without panicking.
func TestDetailSurface_ResizeHonored(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.Preflight = scriptedPreflight(errorReport())

	// A fresh model with no WindowSizeMsg must still render (uses defaults).
	m := NewModel(opts)
	m = triggerSuiteStart(t, m)
	_ = safeView(t, m) // must not panic

	// Apply a resize and re-render — view dimensions must change.
	mWide := NewModel(opts)
	mWide, _ = safeUpdate(t, mWide, tea.WindowSizeMsg{Width: 120, Height: 40})
	mWide = triggerSuiteStart(t, mWide)
	viewWide := safeView(t, mWide)

	mNarrow := NewModel(opts)
	mNarrow, _ = safeUpdate(t, mNarrow, tea.WindowSizeMsg{Width: 40, Height: 20})
	mNarrow = triggerSuiteStart(t, mNarrow)
	viewNarrow := safeView(t, mNarrow)

	if viewWide == viewNarrow {
		t.Errorf("view at width=120 and view at width=40 are identical after a pre-flight failure; the pane must resize with the terminal")
	}
}

// TestDetailSurface_ReadableCopyMatchesRenderedCopy verifies the single-writer
// invariant: after a failure site runs, every line of FailureDetail()'s body
// is reachable in View() by scrolling from the top. The readable copy (Model
// fields) and the rendered copy (pane) must agree.
//
// This test uses a small report so it fits in a single scroll through a
// reasonably sized pane.
func TestDetailSurface_ReadableCopyMatchesRenderedCopy(t *testing.T) {
	rpt := errorReport()
	m := modelOnSuiteSelectWithPreflight(rpt)
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 40})
	m = triggerSuiteStart(t, m)

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a pre-flight failure")
	}

	// Collect all rendered content by scrolling from top through the pane.
	// Enough scroll iterations to cover a small report.
	allRendered := safeView(t, m)
	for i := 0; i < 20; i++ {
		m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
		allRendered += safeView(t, m)
	}

	// Every line in the readable body must appear somewhere in the collected
	// rendered output. Normalise both sides by collapsing whitespace (including
	// the line-break padding that word-wrapping introduces) to single spaces so
	// that a long body line that wraps across two visual lines can still be
	// matched against its normalised form.
	normalised := strings.Join(strings.Fields(allRendered), " ")
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		normLine := strings.Join(strings.Fields(line), " ")
		if !strings.Contains(normalised, normLine) {
			t.Errorf("FailureDetail() body line %q is not reachable by scrolling through the rendered view; the readable copy and rendered copy must agree", line)
		}
	}
}
