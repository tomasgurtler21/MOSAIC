package tui

// detail_scroll_test.go verifies scrolling behaviour for the test-detail
// screen. The tests below are the RED-phase specification; they compile
// against the current implementation and fail at runtime — proving the
// behaviours the implementation tasks must produce.
//
// Covered:
//   - On a short terminal, the detail screen's rendered output fits the available height.
//   - Scroll keys (PgDown / PgUp) on the detail screen change the visible content.
//   - Scrolling past the top or bottom is a no-op, not a panic or wrap-around.
//   - The scroll position resets when the user leaves and re-enters the screen.
//   - The scroll position resets when the user selects a different test.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// manyAssertionRun builds a RunReport with n failed assertions so the
// test-detail view has many lines — enough to exceed a short terminal height.
func manyAssertionRun(testID string, n int) report.RunReport {
	assertions := make([]domain.AssertionResult, n)
	for i := range assertions {
		assertions[i] = domain.AssertionResult{
			Outcome: domain.AssertionFail,
			Detail:  fmt.Sprintf("assertion %d: expected the subject to pass but it failed with reason %d", i+1, i+1),
		}
	}
	return report.RunReport{
		Key:        domain.RunKey{TestName: testID, RunNumber: 1},
		Verdict:    domain.VerdictFail,
		Duration:   time.Second,
		Cost:       domain.CostReport{Attribution: domain.AttributionAttributed},
		Assertions: assertions,
	}
}

// modelAtDetailScreenWithManyAssertions builds a Model already on the
// test-detail screen where the selected test has enough failed assertions
// to overflow a short terminal.
func modelAtDetailScreenWithManyAssertions(t *testing.T, termWidth, termHeight int) Model {
	t.Helper()
	const testID = "test-scroll-target"
	run := manyAssertionRun(testID, 15) // 15 failed assertions → ~20 lines in the detail view
	testReport := report.TestReport{
		TestName: testID,
		Runs:   []report.RunReport{run},
	}
	result := report.Build("suite-under-test", time.Now(), time.Now(), []report.TestReport{testReport}, "")
	runner := newFakeSuiteRunner().withResult(result)
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: termWidth, Height: termHeight})
	m = runSuiteToCompletion(t, m, runner)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // drill into detail
	if m.Screen() != ScreenDetail {
		t.Fatalf("expected ScreenDetail after Enter on results, got %q", m.Screen())
	}
	return m
}

// ---------------------------------------------------------------------------
// Rendered output fits the terminal height
// ---------------------------------------------------------------------------

// TestDetailScroll_RenderedHeightFitsAvailableHeight verifies that on a
// short terminal the detail screen's rendered output never exceeds the total
// terminal height. Currently viewDetail renders all lines without clamping,
// so this test fails when there are more content lines than the terminal height.
func TestDetailScroll_RenderedHeightFitsAvailableHeight(t *testing.T) {
	const termWidth = 80
	const termHeight = 8 // very short terminal
	m := modelAtDetailScreenWithManyAssertions(t, termWidth, termHeight)
	view := safeView(t, m)
	lines := strings.Split(view, "\n")
	if len(lines) > termHeight {
		t.Errorf(
			"detail screen rendered %d lines for a terminal with height=%d;\n"+
				"the rendered output must never exceed the available terminal height",
			len(lines), termHeight,
		)
	}
}

// ---------------------------------------------------------------------------
// Scroll keys move the visible window
// ---------------------------------------------------------------------------

// TestDetailScroll_PgDownChangesVisibleContent verifies that pressing PgDown
// on the test-detail screen advances the visible window to show content that
// was previously below the fold. Currently updateDetail handles only the Back
// key, so PgDown is a no-op and this test fails.
func TestDetailScroll_PgDownChangesVisibleContent(t *testing.T) {
	const termWidth = 80
	const termHeight = 8
	m := modelAtDetailScreenWithManyAssertions(t, termWidth, termHeight)
	viewBefore := safeView(t, m)

	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	viewAfter := safeView(t, m)

	if viewBefore == viewAfter {
		t.Errorf(
			"PgDown on the test-detail screen produced no change in the rendered view;\n" +
				"scroll must advance the visible window through the content",
		)
	}
}

// TestDetailScroll_PgUpAfterPgDown_RestoresPreviousView verifies that after
// scrolling down, pressing PgUp returns the view to its previous state.
func TestDetailScroll_PgUpAfterPgDown_RestoresPreviousView(t *testing.T) {
	const termWidth = 80
	const termHeight = 8
	m := modelAtDetailScreenWithManyAssertions(t, termWidth, termHeight)
	viewAtTop := safeView(t, m)

	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	viewScrolled := safeView(t, m)

	if viewScrolled == viewAtTop {
		t.Log("PgDown had no visible effect; PgUp round-trip assertion will be vacuously true")
		return
	}

	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	viewAfterUp := safeView(t, m)

	if viewAfterUp != viewAtTop {
		t.Errorf(
			"PgUp after PgDown did not restore the previous view;\n"+
				"scroll must be reversible\nexpected:\n%s\ngot:\n%s",
			viewAtTop, viewAfterUp,
		)
	}
}

// TestDetailScroll_CanReachLastLine verifies that by pressing PgDown enough
// times, the user can bring the last line of the content into view. This
// guards against a scroll-bound calculation that stops too early.
func TestDetailScroll_CanReachLastLine(t *testing.T) {
	const termWidth = 80
	const termHeight = 8
	const testID = "test-reach-end"
	// Build a run whose last assertion has a unique sentinel string.
	assertions := make([]domain.AssertionResult, 14)
	for i := range assertions[:13] {
		assertions[i] = domain.AssertionResult{
			Outcome: domain.AssertionFail,
			Detail:  fmt.Sprintf("assertion %d failed", i+1),
		}
	}
	assertions[13] = domain.AssertionResult{
		Outcome: domain.AssertionFail,
		Detail:  "LAST-LINE-SENTINEL",
	}
	run := report.RunReport{
		Key:        domain.RunKey{TestName: testID, RunNumber: 1},
		Verdict:    domain.VerdictFail,
		Duration:   time.Second,
		Cost:       domain.CostReport{Attribution: domain.AttributionAttributed},
		Assertions: assertions,
	}
	result := report.Build("suite-under-test", time.Now(), time.Now(),
		[]report.TestReport{{TestName: testID, Runs: []report.RunReport{run}}}, "")
	runner := newFakeSuiteRunner().withResult(result)
	m := NewModel(newFixtureOptions([]string{"suite.yaml"}, runner))
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: termWidth, Height: termHeight})
	m = runSuiteToCompletion(t, m, runner)
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenDetail {
		t.Fatalf("expected ScreenDetail, got %q", m.Screen())
	}

	// Scroll down many times to reach the end.
	for i := 0; i < 30; i++ {
		m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	view := safeView(t, m)

	if !strings.Contains(view, "LAST-LINE-SENTINEL") {
		t.Errorf(
			"after scrolling to the end of the detail screen, \"LAST-LINE-SENTINEL\" is not visible;\n" +
				"scroll must be able to bring every line of the content into view",
		)
	}
}

// ---------------------------------------------------------------------------
// Scrolling past either end is a no-op
// ---------------------------------------------------------------------------

// TestDetailScroll_PgUpAtTop_IsNoOp verifies that pressing PgUp when already
// at the top of the detail screen does not change the view and does not panic.
func TestDetailScroll_PgUpAtTop_IsNoOp(t *testing.T) {
	const termWidth = 80
	const termHeight = 8
	m := modelAtDetailScreenWithManyAssertions(t, termWidth, termHeight)
	viewBefore := safeView(t, m)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PgUp at the top of the detail screen panicked: %v", r)
		}
	}()
	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	viewAfter := safeView(t, m)

	if viewBefore != viewAfter {
		t.Errorf(
			"PgUp at the top of the detail screen changed the view; scrolling past the top must be a no-op\n"+
				"before:\n%s\nafter:\n%s",
			viewBefore, viewAfter,
		)
	}
}

// TestDetailScroll_PgDownAtBottom_IsNoOp verifies that pressing PgDown many
// times past the bottom of the content does not panic and the view stabilises.
func TestDetailScroll_PgDownAtBottom_IsNoOp(t *testing.T) {
	const termWidth = 80
	const termHeight = 8
	m := modelAtDetailScreenWithManyAssertions(t, termWidth, termHeight)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PgDown past the bottom of the detail screen panicked: %v", r)
		}
	}()

	// Scroll far past the bottom.
	for i := 0; i < 50; i++ {
		m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	viewAtBottom := safeView(t, m)

	// One more scroll must not change the view.
	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	viewAfterExtra := safeView(t, m)

	if viewAtBottom != viewAfterExtra {
		t.Errorf(
			"PgDown past the bottom of the detail screen changed the view;\n" +
				"scrolling past the bottom must be a no-op",
		)
	}
}

// ---------------------------------------------------------------------------
// Scroll position resets on re-entry
// ---------------------------------------------------------------------------

// TestDetailScroll_PositionResetsWhenReenteringScreen verifies that after
// scrolling down on the detail screen and navigating back to results,
// drilling in again shows the detail from the top.
func TestDetailScroll_PositionResetsWhenReenteringScreen(t *testing.T) {
	const termWidth = 80
	const termHeight = 8
	m := modelAtDetailScreenWithManyAssertions(t, termWidth, termHeight)
	viewAtTop := safeView(t, m)

	// Scroll down several times.
	for i := 0; i < 5; i++ {
		m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	viewScrolled := safeView(t, m)

	if viewScrolled == viewAtTop {
		t.Log("PgDown had no visible effect (content may fit or scroll not yet implemented); reset assertion is vacuously true")
		return
	}

	// Navigate back to results.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenResults {
		t.Fatalf("Esc from detail screen should return to results; got %q", m.Screen())
	}

	// Re-enter the detail screen.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenDetail {
		t.Fatalf("Enter from results should return to detail; got %q", m.Screen())
	}
	viewAfterReentry := safeView(t, m)

	if viewAfterReentry != viewAtTop {
		t.Errorf(
			"after re-entering the detail screen, view does not show the top;\n"+
				"the scroll position must reset to zero on entry\nexpected (top):\n%s\ngot:\n%s",
			viewAtTop, viewAfterReentry,
		)
	}
}

// ---------------------------------------------------------------------------
// Scroll position resets when a different test is selected
// ---------------------------------------------------------------------------

// TestDetailScroll_PositionResetsOnSelectionChange verifies that when the
// user returns to results, selects a different test, and drills in, the new
// test's detail screen starts at the top regardless of where the previous
// test's scroll position was.
func TestDetailScroll_PositionResetsOnSelectionChange(t *testing.T) {
	const termWidth = 80
	const termHeight = 8

	// Build a result with two tests, both with many assertions.
	makeTest := func(id string) report.TestReport {
		run := manyAssertionRun(id, 15)
		return report.TestReport{TestName: id, Runs: []report.RunReport{run}}
	}
	result := report.Build("suite-under-test", time.Now(), time.Now(), []report.TestReport{makeTest("test-alpha"), makeTest("test-beta")}, "")
	runner := newFakeSuiteRunner().withResult(result)
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: termWidth, Height: termHeight})
	m = runSuiteToCompletion(t, m, runner)

	// Drill into test-alpha (cursor=0, the first test).
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenDetail {
		t.Fatalf("expected ScreenDetail, got %q", m.Screen())
	}
	viewAlphaTop := safeView(t, m)

	// Scroll down several times.
	for i := 0; i < 5; i++ {
		m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	viewAlphaScrolled := safeView(t, m)

	if viewAlphaScrolled == viewAlphaTop {
		t.Log("PgDown had no visible effect; selection-change reset assertion is vacuously true")
		return
	}

	// Return to results and move cursor to test-beta.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))

	// Drill into test-beta.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenDetail {
		t.Fatalf("expected ScreenDetail for test-beta, got %q", m.Screen())
	}

	// Return to results and go back to test-alpha.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	m, _ = safeUpdate(t, m, keyType(tea.KeyUp))
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenDetail {
		t.Fatalf("expected ScreenDetail for test-alpha on re-entry, got %q", m.Screen())
	}
	viewAlphaAfterChange := safeView(t, m)

	if viewAlphaAfterChange != viewAlphaTop {
		t.Errorf(
			"after changing selection and returning to test-alpha, view does not show the top;\n"+
				"the scroll position must reset when the selected test changes\nexpected (top):\n%s\ngot:\n%s",
			viewAlphaTop, viewAlphaAfterChange,
		)
	}
}
