package tui

// queue_progress_test.go specifies the queue-level progress figures for
// Stage 4: Queue Progress Figures.
//
// Coverage:
//
//   T4.1 - Queue-level run figures: the queue total equals the sum of
//           TotalRuns() across all selected suites' plans and is correct
//           from the moment the first suite starts; the queue-level remaining
//           count decreases monotonically across a suite boundary rather than
//           restarting.
//
//   T4.2 - Suite-level tally: the progress screen continues to show the
//           current suite's own running, finished and remaining counts,
//           now labelled to distinguish them from the queue-level figures.
//
//   T4.3 - Queue-position indicator: the progress screen names the suite
//           currently running and its 1-of-N position when more than one
//           suite was selected; no position indicator appears for a
//           single-suite selection.
//
//   T4.4 - Scope labels and readability: the progress screen carries
//           distinguishably labelled sections for queue-level and suite-level
//           figures; at the standard terminal width all existing progress
//           content remains present and un-truncated.
//
//   T4.5 - Suite-level tally preservation: the suite-level tally still
//           reports the current suite's correct numbers after the new
//           queue-level lines are added, and m.Tally() still reflects only
//           the current suite's fold.

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

// ---------------------------------------------------------------------------
// Shared helpers for queue-progress tests
// ---------------------------------------------------------------------------

// planWithNTests returns a Plan with n tests each contributing 1 run, so
// Plan.TotalRuns() == n. The suiteID is embedded in Suite.ID for traceability.
func planWithNTests(suiteID string, n int) preflight.Plan {
	tests := make([]preflight.ResolvedTest, n)
	return preflight.Plan{Suite: domain.TestSuite{ID: suiteID}, Tests: tests}
}

// suiteStartedEvent returns a ProgressSuiteStarted event naming suiteID with
// totalRuns declared runs, matching what the suite runner emits when it starts.
func suiteStartedEvent(suiteID string, totalRuns int) domain.ProgressEvent {
	return domain.ProgressEvent{
		Kind:       domain.ProgressSuiteStarted,
		SuiteID:    suiteID,
		TotalTests: totalRuns,
		TotalRuns:  totalRuns,
	}
}

// twoSuiteModelAtProgress sets up a model with two suites using the given plans
// and drives it through queue-wide pre-flight to the first suite's ScreenProgress.
// It returns the model on ScreenProgress and the cmd to start the first suite's
// run (which, when run, produces SuiteFinishedMsg for the first suite).
func twoSuiteModelAtProgress(t *testing.T, planA, planB preflight.Plan) (Model, tea.Cmd) {
	t.Helper()
	rec := newRecordingPreflight()
	rec.withPlan("suite-a.yaml", planA)
	rec.withPlan("suite-b.yaml", planB)

	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, runner)
	opts.Preflight = rec.preflight
	m := NewModel(opts)

	m = selectSuites(t, m, []int{0, 1}) // toggle both suites
	m, cmd := driveQueuePreflight(t, m)  // drives to ScreenProgress with first suite starting

	if m.Screen() != ScreenProgress {
		t.Fatalf("twoSuiteModelAtProgress: Screen() = %q, want %q", m.Screen(), ScreenProgress)
	}
	return m, cmd
}

// advanceToSecondSuite delivers the first suite's SuiteFinishedMsg (by running
// cmd) and returns the model on ScreenProgress for the second suite along with
// the cmd to start the second suite's runner. It fails the test if the second
// suite does not start.
func advanceToSecondSuite(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatalf("advanceToSecondSuite: first-suite cmd is nil; cannot advance")
	}
	// Run the first suite's cmd to get SuiteFinishedMsg for suite A.
	msg := runCmd(t, cmd)
	m, cmd2 := safeUpdate(t, m, msg)

	if m.Screen() != ScreenProgress {
		t.Fatalf("advanceToSecondSuite: Screen() after first suite finished = %q, want %q", m.Screen(), ScreenProgress)
	}
	return m, cmd2
}

// ---------------------------------------------------------------------------
// T4.1 - Queue-level run figures
// ---------------------------------------------------------------------------

// TestQueueProgress_QueueTotal_ShownOnProgressScreen verifies that the
// progress screen's rendered view contains the queue-level total run count,
// which equals the sum of TotalRuns() across all selected suites' plans.
//
// With plan A having 4 runs and plan B having 7 runs the queue total is 11.
// The number 11 is chosen to be distinct from any other number the progress
// screen would show from the suite-level fold alone (which starts at 0 for all
// fields until the first suite-started event is folded).
//
// Fails at RED: the current progress screen renders only the suite-level fold,
// which shows 0 for all counts before any events arrive; "11" does not appear.
func TestQueueProgress_QueueTotal_ShownOnProgressScreen(t *testing.T) {
	const (
		runsA = 4
		runsB = 7
	)
	planA := planWithNTests("suite-a", runsA)
	planB := planWithNTests("suite-b", runsB)
	queueTotal := runsA + runsB // 11

	m, _ := twoSuiteModelAtProgress(t, planA, planB)

	// The model is now at ScreenProgress for the first suite's run. Before any
	// suite-started event is folded, the suite-level fold holds only zeros.
	// The queue total (11) must appear in the view — it comes from the plans
	// retained during queue-wide pre-flight, not from the fold.
	view := safeView(t, m)
	if !strings.Contains(view, fmt.Sprintf("%d", queueTotal)) {
		t.Errorf(
			"progress view does not contain the queue total %d;\n"+
				"the queue total must be derived from preflightedPlans.TotalRuns() and shown on the progress screen\nview:\n%s",
			queueTotal, view,
		)
	}
}

// TestQueueProgress_QueueTotal_CorrectBeforeFirstSuiteFinishes verifies that
// the queue total is visible and correct even before any test in the first suite
// has started, i.e. immediately after the model transitions to ScreenProgress.
//
// This guards against an implementation that only computes the queue total after
// the first suite-started progress event (by which point a suite might already
// have run a test, leaving the "before any event" window unobservable).
//
// Fails at RED: same as TestQueueProgress_QueueTotal_ShownOnProgressScreen.
func TestQueueProgress_QueueTotal_CorrectBeforeFirstSuiteFinishes(t *testing.T) {
	const (
		runsA = 3
		runsB = 6
	)
	planA := planWithNTests("suite-a", runsA)
	planB := planWithNTests("suite-b", runsB)
	queueTotal := runsA + runsB // 9

	m, _ := twoSuiteModelAtProgress(t, planA, planB)

	// The model is at ScreenProgress but no progress events have been folded
	// for the first suite yet. The queue total must already be present.
	view := safeView(t, m)
	if !strings.Contains(view, fmt.Sprintf("%d", queueTotal)) {
		t.Errorf(
			"queue total %d is not visible on the progress screen before the first suite-started event;\n"+
				"the total must be derived once during queue-wide pre-flight and shown immediately\nview:\n%s",
			queueTotal, view,
		)
	}
}

// TestQueueProgress_QueueTotal_SurvivesSuiteBoundary verifies that the queue
// total displayed on the progress screen is the same when the second suite is
// running as it was when the first suite was running. The total must not reset
// or change at the suite boundary.
//
// Fails at RED: without queue tracking, the number shown after the boundary is
// the second suite's suite-level count (runsB=7), not the queue total (11);
// or the queue total disappears entirely.
func TestQueueProgress_QueueTotal_SurvivesSuiteBoundary(t *testing.T) {
	const (
		runsA = 4
		runsB = 7
	)
	planA := planWithNTests("suite-a", runsA)
	planB := planWithNTests("suite-b", runsB)
	queueTotal := runsA + runsB // 11

	m, cmd := twoSuiteModelAtProgress(t, planA, planB)
	viewBeforeBoundary := safeView(t, m)

	m, _ = advanceToSecondSuite(t, m, cmd)
	viewAfterBoundary := safeView(t, m)

	if !strings.Contains(viewBeforeBoundary, fmt.Sprintf("%d", queueTotal)) {
		t.Errorf("before boundary: progress view does not contain queue total %d\nview:\n%s", queueTotal, viewBeforeBoundary)
	}
	if !strings.Contains(viewAfterBoundary, fmt.Sprintf("%d", queueTotal)) {
		t.Errorf(
			"after suite boundary: progress view no longer contains queue total %d;\n"+
				"the queue total must survive a suite boundary unchanged\nview:\n%s",
			queueTotal, viewAfterBoundary,
		)
	}
}

// TestQueueProgress_RemainingCount_DecreasesAcrossSuiteBoundary verifies that
// the queue-level remaining count shown on the progress screen after the first
// suite completes is not greater than the remaining count shown while the first
// suite was running.
//
// With plan A = 4 runs and plan B = 7 runs, the queue total is 11.
// While suite A runs (before any A events): queue remaining = 11.
// After suite A finishes (all 4 of A's runs counted): queue remaining = 7.
// The remaining count must not restart at 7 as if only B existed from the start.
//
// Fails at RED: the current implementation resets the shared fold at each suite
// boundary and does not track completed suites' runs, so there is no queue
// remaining in the view; 7 (plan B TotalRuns) appears only after the boundary
// is crossed and B's suite-started event is folded.
func TestQueueProgress_RemainingCount_DecreasesAcrossSuiteBoundary(t *testing.T) {
	const (
		runsA = 4
		runsB = 7
	)
	planA := planWithNTests("suite-a", runsA)
	planB := planWithNTests("suite-b", runsB)

	m, cmd := twoSuiteModelAtProgress(t, planA, planB)

	// Before the boundary: queue remaining = 11 (all runs outstanding).
	// Fold the first suite's started event so the suite-level fold is populated
	// and we can distinguish suite-level from queue-level numbers.
	m = safeFold(t, m, suiteStartedEvent("suite-a", runsA))
	viewBeforeBoundary := safeView(t, m)

	// Cross the boundary: deliver the first suite's SuiteFinishedMsg.
	m, _ = advanceToSecondSuite(t, m, cmd)
	viewAfterBoundary := safeView(t, m)

	// The queue-level remaining after the boundary is 7 (only plan B is left).
	// It must NOT be greater than the queue-level remaining before the boundary.
	// Concretely: "7" must appear in the after-boundary view (queue remaining
	// equals plan B's runs), while "11" must not appear (all runs of A are done).
	if !strings.Contains(viewAfterBoundary, fmt.Sprintf("%d", runsB)) {
		t.Errorf(
			"after suite boundary: progress view does not contain %d (queue remaining for second suite);\n"+
				"completed suite runs must be counted so queue remaining = queueTotal - completedRuns\nbefore boundary view:\n%s\nafter boundary view:\n%s",
			runsB, viewBeforeBoundary, viewAfterBoundary,
		)
	}

	// The queue total must still appear (non-regressing from the boundary test).
	queueTotal := runsA + runsB
	if !strings.Contains(viewAfterBoundary, fmt.Sprintf("%d", queueTotal)) {
		t.Errorf(
			"after suite boundary: progress view does not contain queue total %d;\n"+
				"the total must survive the boundary\nview:\n%s",
			queueTotal, viewAfterBoundary,
		)
	}
}

// ---------------------------------------------------------------------------
// T4.2 - Suite-level tally still reports the current suite
// ---------------------------------------------------------------------------

// TestQueueProgress_SuiteTally_ReflectsCurrentSuiteOnly verifies that the
// suite-level tally (m.Tally()) reflects only the current suite's fold and not
// the accumulated queue total. When the second suite is running with 2 tests
// started, m.Tally().Running must be 2 (not the queue total's in-flight count).
//
// This test is expected to pass at RED because m.Tally() delegates to the shared
// fold, which is reset at each suite boundary. Its purpose is to pin that
// behaviour so the implementation does not accidentally merge the suite fold
// with the queue-level accumulator.
func TestQueueProgress_SuiteTally_ReflectsCurrentSuiteOnly(t *testing.T) {
	const (
		runsA = 3
		runsB = 5
	)
	planA := planWithNTests("suite-a", runsA)
	planB := planWithNTests("suite-b", runsB)

	m, cmd := twoSuiteModelAtProgress(t, planA, planB)
	m, _ = advanceToSecondSuite(t, m, cmd)

	// Fold the second suite's started and two test-started events.
	runKey1 := domain.RunKey{RunID: "20260828T120000Z-0001", TestName: "b-test-1", RunNumber: 1}
	runKey2 := domain.RunKey{RunID: "20260828T120000Z-0002", TestName: "b-test-2", RunNumber: 1}
	m = safeFold(t, m, suiteStartedEvent("suite-b", runsB))
	m = safeFold(t, m, domain.ProgressEvent{
		Kind:        domain.ProgressTestStarted,
		TestID:      "b-test-1",
		Repetition:  1,
		Repetitions: 1,
		Run:         runKey1,
	})
	m = safeFold(t, m, domain.ProgressEvent{
		Kind:        domain.ProgressTestStarted,
		TestID:      "b-test-2",
		Repetition:  1,
		Repetitions: 1,
		Run:         runKey2,
	})

	tally := m.Tally()

	// Suite-level: 2 running, 0 finished, 3 remaining (5 total - 2 running - 0 finished).
	if tally.Running != 2 {
		t.Errorf("Tally().Running = %d, want 2 (only second suite's in-flight tests counted, not queue total)", tally.Running)
	}
	if tally.Finished != 0 {
		t.Errorf("Tally().Finished = %d, want 0 (no tests have finished in the second suite yet)", tally.Finished)
	}
	if tally.Remaining != 3 {
		t.Errorf("Tally().Remaining = %d, want 3 (5 total for suite B - 2 running - 0 finished)", tally.Remaining)
	}
}

// TestQueueProgress_SuiteLabel_PresentInProgressView verifies that the progress
// screen renders a label identifying the suite-level tally, so the user can
// distinguish it from the queue-level figures. The label must appear alongside
// the running/finished/remaining counts.
//
// Fails at RED: the current progress screen has no scope label on the tally
// line; "Suite:" does not appear in the view (only "Suite Progress" in the title).
func TestQueueProgress_SuiteLabel_PresentInProgressView(t *testing.T) {
	const (
		runsA = 3
		runsB = 5
	)
	planA := planWithNTests("suite-a", runsA)
	planB := planWithNTests("suite-b", runsB)

	m, cmd := twoSuiteModelAtProgress(t, planA, planB)
	m, _ = advanceToSecondSuite(t, m, cmd)

	// Fold a suite-started and one test-started event to populate the fold.
	runKey := domain.RunKey{RunID: "20260828T120000Z-0001", TestName: "b-test-1", RunNumber: 1}
	m = safeFold(t, m, suiteStartedEvent("suite-b", runsB))
	m = safeFold(t, m, domain.ProgressEvent{
		Kind:        domain.ProgressTestStarted,
		TestID:      "b-test-1",
		Repetition:  1,
		Repetitions: 1,
		Run:         runKey,
	})

	view := safeView(t, m)
	// The view must contain a label that identifies the suite-level scope.
	// We look for "Suite:" (with colon) to exclude the "Suite Progress" title
	// which does not carry a colon after "Suite".
	if !strings.Contains(view, "Suite:") {
		t.Errorf(
			"progress view does not contain \"Suite:\" label on the suite-level tally;\n"+
				"queue-level and suite-level figures must be labelled distinguishably\nview:\n%s",
			view,
		)
	}
}

// ---------------------------------------------------------------------------
// T4.3 - Queue-position indicator
// ---------------------------------------------------------------------------

// TestQueueProgress_PositionIndicator_ShownForMultiSuiteRun verifies that when
// two suites are selected and the second is executing, the progress view names
// the current suite and shows its position (e.g. "2 of 2" or the suite path
// with a position marker).
//
// Fails at RED: the current progress screen shows no suite name or position
// indicator; the view contains neither the suite path nor a position pattern.
func TestQueueProgress_PositionIndicator_ShownForMultiSuiteRun(t *testing.T) {
	planA := planWithNTests("suite-a", 2)
	planB := planWithNTests("suite-b", 3)

	m, cmd := twoSuiteModelAtProgress(t, planA, planB)
	m, _ = advanceToSecondSuite(t, m, cmd)

	// The model is now running the second suite.
	view := safeView(t, m)
	// The view must contain both a suite identifier and a position relative to
	// the total selected suites. We check for "2" (the position of suite B in
	// the selection) and "2" (the total count of selected suites) appearing in a
	// position-indicating context. The position pattern "2 of 2" is one valid
	// format; "suite-b.yaml" with positional metadata is another. We check for
	// the substring "of 2" which any reasonable position indicator must contain.
	if !strings.Contains(view, "of 2") {
		t.Errorf(
			"progress view does not contain position indicator \"of 2\" while the second of two suites is running;\n"+
				"the current suite's position must be shown when more than one suite was selected\nview:\n%s",
			view,
		)
	}
}

// TestQueueProgress_PositionIndicator_NamesCurrentSuite verifies that the
// progress view includes the name of the suite currently running so the user
// can identify which suite is in flight when multiple are queued.
//
// Fails at RED: the current progress screen shows no suite name.
func TestQueueProgress_PositionIndicator_NamesCurrentSuite(t *testing.T) {
	planA := planWithNTests("suite-a", 2)
	planB := planWithNTests("suite-b", 3)

	m, cmd := twoSuiteModelAtProgress(t, planA, planB)

	// While the first suite is running, its name must appear in the view.
	viewFirstSuite := safeView(t, m)
	if !strings.Contains(viewFirstSuite, "suite-a.yaml") {
		t.Errorf(
			"progress view does not contain the current suite name %q;\n"+
				"the suite currently running must be named in the progress screen\nview:\n%s",
			"suite-a.yaml", viewFirstSuite,
		)
	}

	// After advancing to the second suite, its name must appear.
	m, _ = advanceToSecondSuite(t, m, cmd)
	viewSecondSuite := safeView(t, m)
	if !strings.Contains(viewSecondSuite, "suite-b.yaml") {
		t.Errorf(
			"progress view does not contain the second suite name %q after it starts;\n"+
				"the suite name must update when the next suite in the queue begins\nview:\n%s",
			"suite-b.yaml", viewSecondSuite,
		)
	}
}

// TestQueueProgress_PositionIndicator_AbsentForSingleSuite verifies that when
// only one suite was selected, the progress screen does not show a queue-position
// indicator. No "1 of 1" or similar text must appear.
//
// Fails at RED if the implementation naively adds the indicator regardless of
// queue size. At RED with the current implementation, this test passes (no
// indicator is rendered at all), so this test is a GREEN-phase regression guard.
// It is included because the implementation task (I4.8) must handle the
// single-suite case explicitly (AC4.5).
func TestQueueProgress_PositionIndicator_AbsentForSingleSuite(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	m := NewModel(opts)

	m, cmd := startSuiteFromSuiteSelect(t, m)
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenProgress)
	}
	_ = cmd

	view := safeView(t, m)
	// A position indicator in a single-suite run must not appear. "of 1" is the
	// pattern any N-of-M indicator would produce, and must be absent.
	if strings.Contains(view, "of 1") {
		t.Errorf(
			"progress view contains position indicator \"of 1\" for a single-suite run;\n"+
				"no position indicator must be rendered when only one suite was selected\nview:\n%s",
			view,
		)
	}
}

// TestQueueProgress_PositionIndicator_AdvancesWithQueue verifies that the
// position indicator increments when the second suite starts: it must show
// "1 of 2" while the first suite is active and "2 of 2" while the second is.
//
// Fails at RED: the current progress screen shows no position indicator at all.
func TestQueueProgress_PositionIndicator_AdvancesWithQueue(t *testing.T) {
	planA := planWithNTests("suite-a", 2)
	planB := planWithNTests("suite-b", 2)

	m, cmd := twoSuiteModelAtProgress(t, planA, planB)

	viewFirst := safeView(t, m)
	if !strings.Contains(viewFirst, "1 of 2") {
		t.Errorf("while first suite is running: view does not contain \"1 of 2\"\nview:\n%s", viewFirst)
	}

	m, _ = advanceToSecondSuite(t, m, cmd)
	viewSecond := safeView(t, m)
	if !strings.Contains(viewSecond, "2 of 2") {
		t.Errorf("while second suite is running: view does not contain \"2 of 2\"\nview:\n%s", viewSecond)
	}
}

// ---------------------------------------------------------------------------
// T4.4 - Scope labels distinguishable and standard-width readability
// ---------------------------------------------------------------------------

// TestQueueProgress_ScopeLabels_BothPresentAndDistinct verifies that the
// progress screen carries two distinct scope labels — one for the queue-level
// figures and one for the suite-level tally — so the user can tell which number
// counts which scope.
//
// Fails at RED: the current progress screen has no scope labels; only the single
// unlabelled tally line "Running: X | Finished: Y | Remaining: Z" is rendered.
func TestQueueProgress_ScopeLabels_BothPresentAndDistinct(t *testing.T) {
	planA := planWithNTests("suite-a", 3)
	planB := planWithNTests("suite-b", 4)

	m, _ := twoSuiteModelAtProgress(t, planA, planB)
	view := safeView(t, m)

	// Both a queue-level and a suite-level label must be present. The labels
	// must be different strings. We check for two common label conventions:
	// the queue label must contain "Queue" or "queue" and the suite label must
	// contain "Suite:" (with colon, to distinguish from the "Suite Progress" title).
	hasQueueLabel := strings.Contains(view, "Queue:") || strings.Contains(view, "queue:")
	hasSuiteLabel := strings.Contains(view, "Suite:")
	if !hasQueueLabel {
		t.Errorf(
			"progress view does not contain a queue-level scope label (\"Queue:\" or \"queue:\");\n"+
				"queue-level figures must be labelled distinguishably from suite-level figures\nview:\n%s",
			view,
		)
	}
	if !hasSuiteLabel {
		t.Errorf(
			"progress view does not contain a suite-level scope label (\"Suite:\");\n"+
				"suite-level figures must be labelled distinguishably from queue-level figures\nview:\n%s",
			view,
		)
	}
}

// TestQueueProgress_StandardWidth_TotalTestsLinePresent verifies that at the
// standard terminal width (tuicommon.DefaultWidth = 80), the existing "Total
// tests:" line is still present in the progress view after the queue-level
// lines are added, and is not displaced or truncated.
//
// This guards against the additional queue-level output crowding out existing
// content on an 80-column terminal (AC4.7).
//
// May pass at RED since "Total tests:" already appears in the current view;
// its value as a test is pinning this guarantee through the implementation.
func TestQueueProgress_StandardWidth_TotalTestsLinePresent(t *testing.T) {
	planA := planWithNTests("suite-a", 3)
	planB := planWithNTests("suite-b", 4)

	m, _ := twoSuiteModelAtProgress(t, planA, planB)

	// Apply standard terminal width.
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: tuicommon.DefaultWidth, Height: 24})

	// Fold a suite-started event so the total-tests line carries a non-zero count.
	m = safeFold(t, m, suiteStartedEvent("suite-a", 3))
	view := safeView(t, m)

	if !strings.Contains(view, "Total tests:") {
		t.Errorf(
			"progress view at width=%d does not contain \"Total tests:\";\n"+
				"existing progress content must be preserved and not crowded out by new queue-level lines\nview:\n%s",
			tuicommon.DefaultWidth, view,
		)
	}
}

// TestQueueProgress_StandardWidth_QueueLinesDoNotTruncateExistingContent
// verifies that at the standard terminal width, no line in the progress view
// is truncated at or past the terminal boundary because of the new queue lines.
// Truncation is detectable when a line ends with the tuicommon.Truncate ellipsis.
//
// Fails at RED if the implementation renders the queue lines in a way that
// pushes other content past the standard width.
func TestQueueProgress_StandardWidth_QueueLinesDoNotTruncateExistingContent(t *testing.T) {
	planA := planWithNTests("suite-a", 3)
	planB := planWithNTests("suite-b", 4)

	m, _ := twoSuiteModelAtProgress(t, planA, planB)
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: tuicommon.DefaultWidth, Height: 24})

	// Use a scripted running test with a long name so truncation would be
	// visible if it occurs.
	runKey := domain.RunKey{RunID: "20260828T120000Z-0001", TestName: "a-test-with-a-moderately-long-name", RunNumber: 1}
	m = safeFold(t, m, suiteStartedEvent("suite-a", 3))
	m = safeFold(t, m, domain.ProgressEvent{
		Kind:        domain.ProgressTestStarted,
		TestID:      "a-test-with-a-moderately-long-name",
		Repetition:  1,
		Repetitions: 3,
		Run:         runKey,
	})

	view := safeView(t, m)
	for i, line := range strings.Split(view, "\n") {
		// The shared tuicommon.Truncate function appends "…" when a line is clipped.
		if strings.HasSuffix(strings.TrimRight(line, " "), "…") {
			t.Errorf(
				"progress view line %d appears truncated at standard width=%d: %q;\n"+
					"queue-level lines must not push content past the terminal width",
				i, tuicommon.DefaultWidth, line,
			)
		}
	}
}

// ---------------------------------------------------------------------------
// T4.5 - Suite-level tally preserved with correct Tally() accessor
// ---------------------------------------------------------------------------

// TestQueueProgress_SuiteTally_Accessor_SingleSuite verifies that for a
// single-suite run m.Tally() continues to return the suite's own fold counts,
// unchanged by the queue-level additions. This pins the accessor's contract
// through the Stage 4 implementation.
//
// Expected to pass at RED (Tally() delegates to the unchanged shared fold).
// Included to guard against the implementation accidentally merging the queue
// accumulator into the suite-level fold.
func TestQueueProgress_SuiteTally_Accessor_SingleSuite(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	m := NewModel(opts)

	m, _ = startSuiteFromSuiteSelect(t, m)
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenProgress)
	}

	// Fold a standard 3-run sequence: suite-started, two tests started and one finished.
	runA := domain.RunKey{RunID: "20260828T120000Z-0001", TestName: "test-a", RunNumber: 1}
	runB := domain.RunKey{RunID: "20260828T120000Z-0002", TestName: "test-b", RunNumber: 1}
	m = safeFold(t, m, suiteStartedEvent("suite", 3))
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-a", Repetition: 1, Repetitions: 1, Run: runA})
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-b", Repetition: 1, Repetitions: 1, Run: runB})
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressTestFinished, TestID: "test-a", Repetition: 1, Repetitions: 1, Verdict: domain.VerdictPass, Run: runA})

	tally := m.Tally()
	// After: 1 running (test-b), 1 finished (test-a), 1 remaining (3 total - 1 running - 1 finished).
	if tally.Running != 1 {
		t.Errorf("Tally().Running = %d, want 1 (only test-b is in flight)", tally.Running)
	}
	if tally.Finished != 1 {
		t.Errorf("Tally().Finished = %d, want 1 (test-a has finished)", tally.Finished)
	}
	if tally.Remaining != 1 {
		t.Errorf("Tally().Remaining = %d, want 1 (3 total - 1 running - 1 finished)", tally.Remaining)
	}
}

// TestQueueProgress_SuiteTally_RenderedInView_SingleSuite verifies that the
// suite-level tally numbers are still rendered in the progress view after the
// Stage 4 changes for a single-suite run, so the existing behaviour is not
// erased by the queue-level additions.
//
// Fails at RED if the implementation removes or mislabels the suite tally line.
// Also fails at RED because of the "Suite:" label check — the current view has
// no such label (only the unlabelled tally line).
func TestQueueProgress_SuiteTally_RenderedInView_SingleSuite(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	m := NewModel(opts)

	m, _ = startSuiteFromSuiteSelect(t, m)
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenProgress)
	}

	// Fold events: 2 total runs, 1 finished, 0 running, 1 remaining.
	runA := domain.RunKey{RunID: "20260828T120000Z-0001", TestName: "test-a", RunNumber: 1}
	m = safeFold(t, m, suiteStartedEvent("suite", 2))
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-a", Repetition: 1, Repetitions: 1, Run: runA})
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressTestFinished, TestID: "test-a", Repetition: 1, Repetitions: 1, Verdict: domain.VerdictPass, Run: runA})

	view := safeView(t, m)

	// The view must contain the suite-level tally numbers. The current tally is
	// Running=0, Finished=1, Remaining=1. Check that these values appear in the
	// view, proving the suite-level tally is still rendered after Stage 4 adds
	// queue-level lines alongside it.
	if !strings.Contains(view, "Finished: 1") {
		t.Errorf(
			"progress view does not contain \"Finished: 1\" for the suite-level tally;\n"+
				"suite-level figures must remain visible after queue-level lines are added\nview:\n%s",
			view,
		)
	}
	if !strings.Contains(view, "Remaining: 1") {
		t.Errorf(
			"progress view does not contain \"Remaining: 1\" for the suite-level tally;\n"+
				"suite-level remaining must remain visible after queue-level lines are added\nview:\n%s",
			view,
		)
	}
	// The suite-level tally must now carry a scope label ("Suite:").
	if !strings.Contains(view, "Suite:") {
		t.Errorf(
			"progress view does not contain the \"Suite:\" scope label on the tally;\n"+
				"the suite-level tally must be labelled to distinguish it from queue-level figures\nview:\n%s",
			view,
		)
	}
}
