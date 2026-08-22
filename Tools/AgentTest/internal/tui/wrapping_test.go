package tui

// wrapping_test.go verifies the TUI's wrapping behaviour for the failure-
// detail pane and the test-detail screen. The tests below are the RED-phase
// specification; they compile against the current implementation and fail at
// runtime, proving the behaviours that the implementation tasks must produce.
//
// Covered:
//   - With failure detail live, no rendered line exceeds the terminal width.
//   - At different terminal widths the pane produces differently-wrapped output.
//   - The test-detail screen renders a long failed-assertion detail in full
//     across multiple lines with no ellipsis and no characters dropped.
//   - At a narrow terminal width, no rendered line on any screen state exceeds
//     the reported width, including when failure detail is live.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// longErrorReport builds an authoring.Report with an error whose message is
// significantly longer than termWidth, so the detail pane must wrap it.
func longErrorReport(termWidth int) authoring.Report {
	var rpt authoring.Report
	rpt.Add(authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "wrap-test-error",
		Path:     "tests/suite.yaml",
		Message:  strings.Repeat("diagnostic-text ", termWidth/len("diagnostic-text ")+3),
	})
	return rpt
}

// modelAtWidthWithFailureDetail builds a Model at the given terminal width
// with a pre-flight failure so the detail pane is live and holds content
// that is wider than the terminal.
func modelAtWidthWithFailureDetail(t *testing.T, termWidth int) Model {
	t.Helper()
	rpt := longErrorReport(termWidth)
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.Preflight = scriptedPreflight(rpt)
	m := NewModel(opts)
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: termWidth, Height: 30})
	m, _ = safeUpdate(t, m, keyMsg("\r")) // trigger the pre-flight failure
	return m
}

// assertAllLinesFit checks that every line in view is at most maxWidth cells
// wide, labelling failures with the supplied screen description.
func assertAllLinesFit(t *testing.T, screenDesc, view string, maxWidth int) {
	t.Helper()
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > maxWidth {
			t.Errorf("[%s] line %d is %d cells wide, want <= %d: %q",
				screenDesc, i, w, maxWidth, line)
		}
	}
}

// ---------------------------------------------------------------------------
// Failure-detail pane: lines do not exceed the terminal width
// ---------------------------------------------------------------------------

// TestWrapping_FailureDetailPane_NoLineExceedsTerminalWidth verifies that
// when failure detail is live, every line of the rendered view fits within
// the reported terminal width.
//
// RED-phase failure: paneGeometry() returns width=0 so the detail pane does
// not constrain its lines; long diagnostic text overflows the terminal.
func TestWrapping_FailureDetailPane_NoLineExceedsTerminalWidth(t *testing.T) {
	const termWidth = 40
	m := modelAtWidthWithFailureDetail(t, termWidth)
	view := safeView(t, m)
	assertAllLinesFit(t, "suite-select with failure detail", view, termWidth)
}

// ---------------------------------------------------------------------------
// Failure-detail pane: different terminal widths produce different output
// ---------------------------------------------------------------------------

// TestWrapping_FailureDetailPane_DifferentWidthsDifferentOutput verifies
// that the failure-detail pane re-wraps its content to the current terminal
// width: a view rendered at width=80 must differ from one rendered at
// width=40 in the portion that contains the pane's content, proving that the
// pane's render width tracks the terminal rather than being fixed at zero.
//
// RED-phase failure: paneGeometry() always returns width=0, so the pane
// renders identically regardless of the terminal width.
func TestWrapping_FailureDetailPane_DifferentWidthsDifferentOutput(t *testing.T) {
	var rpt authoring.Report
	rpt.Add(authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "rewrap-test",
		Path:     "tests/suite.yaml",
		Message:  strings.Repeat("word ", 40), // 200-char message
	})

	build := func(termWidth int) Model {
		runner := newFakeSuiteRunner()
		opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
		opts.Preflight = scriptedPreflight(rpt)
		m := NewModel(opts)
		m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: termWidth, Height: 30})
		m, _ = safeUpdate(t, m, keyMsg("\r"))
		return m
	}

	viewWide := safeView(t, build(80))
	viewNarrow := safeView(t, build(40))

	if viewWide == viewNarrow {
		t.Errorf(
			"view at width=80 and view at width=40 are identical while failure detail is live;\n" +
				"the detail pane must re-wrap its content to the current terminal width",
		)
	}

	// Both views must also individually fit their terminal widths.
	assertAllLinesFit(t, "suite-select at width=80", viewWide, 80)
	assertAllLinesFit(t, "suite-select at width=40", viewNarrow, 40)
}

// ---------------------------------------------------------------------------
// Failure-detail pane: live terminal resize re-wraps while failure is active
// ---------------------------------------------------------------------------

// TestWrapping_FailureDetailPane_LiveResize_ReWrapsContent verifies that
// sending a tea.WindowSizeMsg to a model that already has failure detail live
// causes the failure-detail pane to re-wrap to the new terminal width.
// This covers AC1.2 (resize re-wraps without re-triggering the failure) and
// the tea.WindowSizeMsg branch of the implementation.
//
// RED-phase failure: if the WindowSizeMsg handler does not call Resize() on
// the detail pane, the pane keeps its original wrap width and the view is
// unchanged after the resize message.
func TestWrapping_FailureDetailPane_LiveResize_ReWrapsContent(t *testing.T) {
	// Build the model at width=80 and trigger the pre-flight failure.
	m := modelAtWidthWithFailureDetail(t, 80)
	viewBefore := safeView(t, m)

	// Capture the pre-resize view and assert it fits the original width.
	assertAllLinesFit(t, "suite-select at width=80 (before resize)", viewBefore, 80)

	// Send a window-size message while the failure detail is already live.
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: 40, Height: 30})
	viewAfter := safeView(t, m)

	// Every line of the post-resize view must fit the new, narrower width.
	assertAllLinesFit(t, "suite-select at width=40 (after live resize)", viewAfter, 40)

	// The view must differ from the pre-resize view: identical output would
	// mean the pane was not re-wrapped when the resize message was handled.
	if viewBefore == viewAfter {
		t.Errorf(
			"view after tea.WindowSizeMsg{Width:40, Height:30} is identical to the view at width=80;\n"+
				"the failure-detail pane must re-wrap its content when the terminal is resized "+
				"while failure detail is live (AC1.2)",
		)
	}
}

// ---------------------------------------------------------------------------
// Test-detail screen: long assertion detail shown in full, no truncation
// ---------------------------------------------------------------------------

// TestWrapping_TestDetail_LongAssertionShownInFull verifies that a
// failed-assertion detail that exceeds the terminal width is shown in full
// across multiple wrapped lines rather than being cut with an ellipsis.
//
// RED-phase failure: viewDetail applies tuicommon.Truncate() to each
// "Failed assertion: …" line, so a long detail is cut at width-1 runes
// and the remainder is replaced with "…".
func TestWrapping_TestDetail_LongAssertionShownInFull(t *testing.T) {
	const termWidth = 40
	// A detail string that is significantly longer than termWidth and contains
	// a unique suffix that would be truncated away if Truncate is applied.
	const longDetail = "this is a very long failed-assertion detail string that exceeds forty characters: SENTINEL"

	run := report.RunReport{
		Key:      domain.RunKey{TestID: "test-wrap-detail", RunNumber: 1},
		Verdict:  domain.VerdictFail,
		Duration: time.Second,
		Cost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		Assertions: []domain.AssertionResult{{
			Outcome: domain.AssertionFail,
			Detail:  longDetail,
		}},
	}
	testReport := report.TestReport{
		TestID: "test-wrap-detail",
		Runs:   []report.RunReport{run},
	}
	result := report.Build("suite-under-test", time.Now(), time.Now(), []report.TestReport{testReport}, "")

	runner := newFakeSuiteRunner().withResult(result)
	m := NewModel(newFixtureOptions([]string{"suite.yaml"}, runner))
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: termWidth, Height: 30})
	m = runSuiteToCompletion(t, m, runner)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // drill into the test detail screen
	if m.Screen() != ScreenDetail {
		t.Fatalf("Screen() = %q, want %q after drilling into test detail", m.Screen(), ScreenDetail)
	}

	view := safeView(t, m)

	// The unique sentinel word must be present — truncation at termWidth would
	// cut the string before "SENTINEL" is reached.
	if !strings.Contains(view, "SENTINEL") {
		if strings.Contains(view, "…") {
			t.Errorf(
				"test-detail view truncated the failed-assertion detail with \"…\";\n" +
					"long details must be wrapped across multiple lines, not truncated",
			)
		} else {
			t.Errorf(
				"test-detail view does not contain %q from the failed-assertion detail;\n"+
					"the full detail must appear in the view, not be cut",
				"SENTINEL",
			)
		}
	}
}

// ---------------------------------------------------------------------------
// Narrow-width guard: all screen states produce lines within the width
// ---------------------------------------------------------------------------

// TestWrapping_NarrowWidth_AllScreenStates_LinesDoNotExceedWidth verifies
// that at a narrow terminal width, no rendered line on any AgentTest screen
// state exceeds the reported width — including when failure detail is live.
// This extends the existing narrow-width test which covers only suite-select
// without failure detail.
//
// RED-phase failure: the suite-select screen with failure detail live fails
// this check because the detail pane ignores the terminal width.
func TestWrapping_NarrowWidth_AllScreenStates_LinesDoNotExceedWidth(t *testing.T) {
	const narrowWidth = 30
	const termHeight = 20

	// Suite-select without failure detail (existing behaviour, should pass).
	{
		m := NewModel(newFixtureOptions([]string{"a-very-long-suite-name-that-could-overflow.yaml"}, newFakeSuiteRunner()))
		m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: narrowWidth, Height: termHeight})
		assertAllLinesFit(t, "suite-select (no failure detail)", safeView(t, m), narrowWidth)
	}

	// Suite-select with failure detail live.
	{
		var rpt authoring.Report
		rpt.Add(authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "narrow-guard",
			Path:     fmt.Sprintf("tests/%s.yaml", strings.Repeat("a-very-long-path-name", 3)),
			Message:  strings.Repeat("diagnostic content ", 6),
		})
		runner := newFakeSuiteRunner()
		opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
		opts.Preflight = scriptedPreflight(rpt)
		m := NewModel(opts)
		m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: narrowWidth, Height: termHeight})
		m, _ = safeUpdate(t, m, keyMsg("\r"))
		assertAllLinesFit(t, "suite-select (with failure detail)", safeView(t, m), narrowWidth)
	}

	// Results screen.
	{
		scriptedRunner := newFakeSuiteRunner().withEvents(
			scriptedSuite{
				suiteID: "narrow-suite",
				tests:   []scriptedTest{{testID: "narrow-test-id", verdict: domain.VerdictPass}},
			}.events()...,
		)
		m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, scriptedRunner))
		m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: narrowWidth, Height: termHeight})
		m = runSuiteToCompletion(t, m, scriptedRunner)
		assertAllLinesFit(t, "results", safeView(t, m), narrowWidth)

		// Test-detail screen.
		mDetail, _ := safeUpdate(t, m, keyMsg("\r"))
		if mDetail.Screen() == ScreenDetail {
			assertAllLinesFit(t, "test-detail", safeView(t, mDetail), narrowWidth)
		}
	}
}
