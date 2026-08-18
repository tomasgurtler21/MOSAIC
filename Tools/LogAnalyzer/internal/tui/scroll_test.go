package tui

// scroll_test.go verifies the scrolling behaviour of RunsScreen and
// RunDetailScreen.
//
// RunsScreen scrolling (T2.2):
//   With more runs than fit in the given height, View clips the run list to a
//   bounded subset; the selected run is always present in the output; scrolling
//   to the last run makes it appear; scrolling back to the first run makes it
//   appear again; reducing the height after navigating away keeps the selected
//   run visible.
//
// RunDetailScreen scrolling (T2.3):
//   With more actor rows than fit, View clips actor rows to a bounded subset;
//   scrolling down reveals later actors; scrolling past the last row does not
//   advance beyond it; scrolling up returns to the first actor; a very short
//   height does not panic and still renders the header and help bar.

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"mosaic-log-analyzer/internal/domain"
	"mosaic-log-analyzer/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers for scroll tests
// ---------------------------------------------------------------------------

// buildScrollRunsReport builds a domain.Report with n distinct named runs.
// Each run has a unique ID whose last segment encodes its index: "r000",
// "r001", …, "r009". Use runSuffix(i) to retrieve the identifiable substring.
func buildScrollRunsReport(n int) domain.Report {
	runs := make([]domain.RunReport, n)
	for i := range runs {
		runs[i] = domain.RunReport{
			Run: domain.NamedRun(fmt.Sprintf("20260101T%06dZ-r%03d", i, i)),
			Totals: domain.Totals{
				Tokens: domain.TokenUsage{Input: domain.Tokens(100)},
				Money: domain.CategoryMoney{
					Total:    domain.KnownMoney(domain.Money(1_000_000_000)),
					Complete: true,
				},
			},
			Orchestrator: domain.ActorReport{Actor: domain.Orchestrator()},
		}
	}
	return domain.Report{
		Runs:     runs,
		AllRuns:  domain.Totals{},
		Currency: domain.Currency,
		Quality:  domain.NewQualitySummary(nil),
	}
}

// runSuffix returns the unique 4-character tag embedded in run[i]'s ID,
// e.g. "r000" for i=0, "r009" for i=9.
func runSuffix(i int) string {
	return fmt.Sprintf("r%03d", i)
}

// buildScrollDetailRun builds a domain.RunReport with one orchestrator and
// agentCount agents labelled "Agent#1" … "Agent#N". Actor labels are used
// directly in View assertions.
func buildScrollDetailRun(agentCount int) domain.RunReport {
	agents := make([]domain.ActorReport, agentCount)
	for i := range agents {
		agents[i] = domain.ActorReport{
			Actor: domain.AgentInstance(domain.AgentInstanceID(fmt.Sprintf("Agent#%d", i+1))),
			Totals: domain.Totals{
				Tokens: domain.TokenUsage{Input: domain.Tokens(100)},
				Money: domain.CategoryMoney{
					Total:    domain.KnownMoney(domain.Money(1_000_000_000)),
					Complete: true,
				},
			},
		}
	}
	return domain.RunReport{
		Run: domain.NamedRun("20260101T120000Z-scrl"),
		Totals: domain.Totals{
			Tokens: domain.TokenUsage{Input: domain.Tokens(int64(agentCount+1) * 100)},
			Money: domain.CategoryMoney{
				Total:    domain.KnownMoney(domain.Money(int64(agentCount+1) * 1_000_000_000)),
				Complete: true,
			},
		},
		Orchestrator: domain.ActorReport{
			Actor: domain.Orchestrator(),
			Totals: domain.Totals{
				Tokens: domain.TokenUsage{Input: domain.Tokens(100)},
				Money: domain.CategoryMoney{
					Total:    domain.KnownMoney(domain.Money(1_000_000_000)),
					Complete: true,
				},
			},
		},
		Agents: agents,
	}
}

// pressDown sends one "j" key message to the RunsScreen and discards the
// returned Screen and Cmd (the pointer receiver is mutated in place).
func pressRunsDown(s *screens.RunsScreen) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
}

// pressDetailDown sends one "j" key message to the RunDetailScreen.
func pressDetailDown(s *screens.RunDetailScreen) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
}

// pressDetailUp sends one "k" key message to the RunDetailScreen.
func pressDetailUp(s *screens.RunDetailScreen) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
}

// ---------------------------------------------------------------------------
// T2.2: RunsScreen scrolling behaviour
// ---------------------------------------------------------------------------

// TestRunsScreen_SmallHeight_ClipsRunList verifies that when there are more
// runs than fit in the given height, View does not render all of them.
// At cursor=0 and a height that accommodates only a few rows, runs near the
// end of the list must not appear in the output.
//
// Fixed overhead for a simple report (no unattributable/quality/unpriced):
//   title + separator + blank + "All runs" + totals + blank + "Runs(N)" = 7 lines
//   footer: separator + help = 2 lines → 9 total fixed lines
//   per run: 2 lines (label row + totals row)
//
// At height=13: availableLines = 13 - 9 = 4 → VisibleItems(4, 2) = 2 runs.
// With 10 runs at cursor=0, runs[2..9] are beyond the viewport.
func TestRunsScreen_SmallHeight_ClipsRunList(t *testing.T) {
	const height = 13
	report := buildScrollRunsReport(10)
	s := screens.NewRunsScreen(report, 80, height, plainStyles())

	view := s.View(80, height)

	// Run at index 0 (cursor) must be visible.
	if !strings.Contains(view, runSuffix(0)) {
		t.Errorf("run[0] (%q) should be visible at cursor=0; not found in view", runSuffix(0))
	}

	// Run at index 9 must not be visible — it is beyond the clipped viewport.
	if strings.Contains(view, runSuffix(9)) {
		t.Errorf("run[9] (%q) should be clipped at height=%d cursor=0, but appears in view:\n%s",
			runSuffix(9), height, view)
	}
}

// TestRunsScreen_NavigateToLastRun_LastRunVisible verifies that after moving
// the cursor to the last run, that run appears in the rendered output and the
// first run (which has scrolled off) does not.
func TestRunsScreen_NavigateToLastRun_LastRunVisible(t *testing.T) {
	const height = 13
	report := buildScrollRunsReport(10)
	s := screens.NewRunsScreen(report, 80, height, plainStyles())

	// Navigate to the last run.
	for i := 0; i < 9; i++ {
		pressRunsDown(s)
	}
	view := s.View(80, height)

	// Last run must be visible (it is selected).
	if !strings.Contains(view, runSuffix(9)) {
		t.Errorf("run[9] (%q) should be visible after navigating to it; not found in view:\n%s",
			runSuffix(9), view)
	}

	// First run must have scrolled off.
	if strings.Contains(view, runSuffix(0)) {
		t.Errorf("run[0] (%q) should have scrolled off after navigating to run[9] height=%d, but appears in view:\n%s",
			runSuffix(0), height, view)
	}
}

// TestRunsScreen_NavigateBackToFirst_FirstRunVisible verifies that after
// navigating to the last run and then back to the first, the first run is
// visible and the last is not.
func TestRunsScreen_NavigateBackToFirst_FirstRunVisible(t *testing.T) {
	const height = 13
	report := buildScrollRunsReport(10)
	s := screens.NewRunsScreen(report, 80, height, plainStyles())

	// Navigate to run[9] then back to run[0].
	for i := 0; i < 9; i++ {
		pressRunsDown(s)
	}
	for i := 0; i < 9; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	}
	view := s.View(80, height)

	// First run must be visible (cursor is back at 0).
	if !strings.Contains(view, runSuffix(0)) {
		t.Errorf("run[0] (%q) should be visible after scrolling back to top; not found in view:\n%s",
			runSuffix(0), view)
	}

	// Last run must have scrolled off again.
	if strings.Contains(view, runSuffix(9)) {
		t.Errorf("run[9] (%q) should have scrolled off after returning to run[0] height=%d, but appears in view:\n%s",
			runSuffix(9), height, view)
	}
}

// TestRunsScreen_ReducedHeight_SelectedRunStillVisible verifies that rendering
// at a smaller height after navigating to a mid-list run keeps the selected run
// visible, and clips runs that are no longer in the viewport.
func TestRunsScreen_ReducedHeight_SelectedRunStillVisible(t *testing.T) {
	const largeHeight = 30
	const smallHeight = 13
	report := buildScrollRunsReport(10)
	s := screens.NewRunsScreen(report, 80, largeHeight, plainStyles())

	// Navigate to run[5].
	for i := 0; i < 5; i++ {
		pressRunsDown(s)
	}

	// Render at reduced height — selected run must remain visible.
	view := s.View(80, smallHeight)

	if !strings.Contains(view, runSuffix(5)) {
		t.Errorf("run[5] (%q) should be visible at reduced height=%d; not found in view:\n%s",
			runSuffix(5), smallHeight, view)
	}

	// With cursor at 5 and height=13 (2 visible), run[9] is not in the window.
	if strings.Contains(view, runSuffix(9)) {
		t.Errorf("run[9] (%q) should be clipped at height=%d cursor=5, but appears in view:\n%s",
			runSuffix(9), smallHeight, view)
	}
}

// TestRunsScreen_ScrollHintVisible_WhenListIsClipped verifies that when the
// run list is clipped, the rendered output carries a scroll-position hint in
// the form "X-Y of N" so the user can tell content continues beyond the
// visible window.
func TestRunsScreen_ScrollHintVisible_WhenListIsClipped(t *testing.T) {
	const height = 13
	report := buildScrollRunsReport(10)
	s := screens.NewRunsScreen(report, 80, height, plainStyles())

	view := s.View(80, height)

	// The rendered output must contain "of 10" as part of a range hint.
	// A bare "10" could appear in the "Runs (10)" heading; the hint format
	// "X-Y of 10" is what distinguishes a scroll indicator from the count label.
	if !strings.Contains(view, "of 10") {
		t.Errorf("runs view at height=%d should carry a scroll hint containing \"of 10\"; got:\n%s",
			height, view)
	}
}

// TestRunsScreen_ZeroRuns_DoesNotPanic verifies that an empty run list renders
// without panicking, even at a very small height.
func TestRunsScreen_ZeroRuns_DoesNotPanic(t *testing.T) {
	report := buildScrollRunsReport(0)
	s := screens.NewRunsScreen(report, 80, 5, plainStyles())

	view := s.View(80, 5)

	if view == "" {
		t.Error("RunsScreen.View with zero runs returned empty string")
	}
}

// ---------------------------------------------------------------------------
// T2.3: RunDetailScreen scrolling behaviour
// ---------------------------------------------------------------------------

// TestRunDetailScreen_SmallHeight_ClipsActorRows verifies that when there are
// more actor rows than fit in the given height, View does not render all actors.
// At initial scroll=0 and a height that accommodates only a couple of actors,
// later agents must not appear in the output.
//
// Fixed overhead for a simple run (no unpriced models):
//   title + subtitle + sep + blank + "Run totals" + totals + blank = 7 lines
//   footer: sep + help = 2 lines → 9 total fixed lines (approximate)
//   per actor: 2 lines (label row + totals row)
//
// At height=13: VisibleItems(13-9, 2) = 2 actors visible.
// With 5 actors (1 orch + 4 agents) at scroll=0: Agent#3 and Agent#4 are
// beyond the viewport.
func TestRunDetailScreen_SmallHeight_ClipsActorRows(t *testing.T) {
	const height = 13
	run := buildScrollDetailRun(4) // 1 orchestrator + 4 agents = 5 actors
	s := screens.NewRunDetailScreen(run, 80, height, plainStyles())

	view := s.View(80, height)

	// Agent#4 (the last agent) must not be visible at the initial scroll position.
	if strings.Contains(view, "Agent#4") {
		t.Errorf("Agent#4 should be clipped at height=%d scroll=0, but appears in view:\n%s",
			height, view)
	}
}

// TestRunDetailScreen_ScrollDown_RevealsLaterActors verifies that scrolling
// down makes later actors visible and pushes earlier actors out of the viewport.
func TestRunDetailScreen_ScrollDown_RevealsLaterActors(t *testing.T) {
	const height = 13
	run := buildScrollDetailRun(4) // 1 orch + 4 agents
	s := screens.NewRunDetailScreen(run, 80, height, plainStyles())

	// Scroll past the orchestrator and early agents.
	for i := 0; i < 4; i++ {
		pressDetailDown(s)
	}
	view := s.View(80, height)

	// Agent#4 (last actor) should now be visible.
	if !strings.Contains(view, "Agent#4") {
		t.Errorf("Agent#4 should be visible after scrolling down 4 times; not found in view:\n%s", view)
	}

	// Orchestrator should have scrolled out of view.
	// The orchestrator actor label is "orchestrator" (lowercase, from Actor.Label()).
	// The section heading "Orchestrator" (upper) may still appear as a fixed header;
	// we test for the actor detail row by checking the lowercase label absence.
	if strings.Contains(view, "orchestrator") {
		t.Errorf("orchestrator actor row should have scrolled off after navigating to Agent#4; "+
			"still appears in view:\n%s", view)
	}
}

// TestRunDetailScreen_ScrollPastEnd_DoesNotExceedLastActor verifies that
// pressing down many times beyond the last actor does not scroll past it.
// After excess scrolling: the last actor is still in the viewport and the first
// actor has scrolled off.
func TestRunDetailScreen_ScrollPastEnd_DoesNotExceedLastActor(t *testing.T) {
	const height = 13
	run := buildScrollDetailRun(4) // 1 orch + 4 agents = 5 actors
	s := screens.NewRunDetailScreen(run, 80, height, plainStyles())

	// Press down far more times than the actor count.
	for i := 0; i < 100; i++ {
		pressDetailDown(s)
	}
	view := s.View(80, height)

	// Agent#4 (the last actor) must be visible.
	if !strings.Contains(view, "Agent#4") {
		t.Errorf("Agent#4 should be visible after scrolling past end; not found in view:\n%s", view)
	}

	// Orchestrator actor row must have scrolled off (too far up).
	if strings.Contains(view, "orchestrator") {
		t.Errorf("orchestrator actor row should be scrolled off after pressing down 100 times; "+
			"still appears in view:\n%s", view)
	}
}

// TestRunDetailScreen_ScrollUp_ReturnsToFirstActor verifies that scrolling up
// from a mid-list position returns earlier actors to view.
func TestRunDetailScreen_ScrollUp_ReturnsToFirstActor(t *testing.T) {
	const height = 13
	run := buildScrollDetailRun(4) // 1 orch + 4 agents
	s := screens.NewRunDetailScreen(run, 80, height, plainStyles())

	// Scroll down to the bottom, then back up.
	for i := 0; i < 4; i++ {
		pressDetailDown(s)
	}
	for i := 0; i < 4; i++ {
		pressDetailUp(s)
	}
	view := s.View(80, height)

	// Orchestrator (first actor) should be visible again.
	if !strings.Contains(view, "orchestrator") {
		t.Errorf("orchestrator actor row should be visible after scrolling back to top; "+
			"not found in view:\n%s", view)
	}

	// Agent#4 should have scrolled off.
	if strings.Contains(view, "Agent#4") {
		t.Errorf("Agent#4 should have scrolled off after returning to top; still in view:\n%s", view)
	}
}

// TestRunDetailScreen_ShortHeight_DoesNotPanic verifies that a very small
// height (including heights near or at zero) does not cause a panic, and that
// the title header and help bar are still rendered.
func TestRunDetailScreen_ShortHeight_DoesNotPanic(t *testing.T) {
	heights := []int{0, 1, 2, 3}
	run := buildScrollDetailRun(3)

	for _, h := range heights {
		t.Run(fmt.Sprintf("height=%d", h), func(t *testing.T) {
			s := screens.NewRunDetailScreen(run, 80, h, plainStyles())
			view := s.View(80, h)

			// Must not be empty — at least some content is always rendered.
			if view == "" {
				t.Errorf("RunDetailScreen.View(80, %d) returned empty string", h)
			}
			// Title must always be present.
			if !strings.Contains(view, "Log Analyzer") {
				t.Errorf("RunDetailScreen.View(80, %d) missing title header; got:\n%s", h, view)
			}
			// Help bar must always be present.
			if !strings.Contains(view, "↑/↓") {
				t.Errorf("RunDetailScreen.View(80, %d) missing help bar; got:\n%s", h, view)
			}
		})
	}
}

// TestRunDetailScreen_ScrollHintVisible_WhenActorsAreClipped verifies that
// when the actor list is clipped, the rendered output carries a scroll hint in
// the form "X-Y of N" so the user can tell more actors exist beyond the
// viewport.
func TestRunDetailScreen_ScrollHintVisible_WhenActorsAreClipped(t *testing.T) {
	const height = 13
	run := buildScrollDetailRun(4) // 1 orch + 4 agents = 5 actors
	s := screens.NewRunDetailScreen(run, 80, height, plainStyles())

	view := s.View(80, height)

	// The rendered output must contain "of 5" as part of a range hint.
	// A bare "5" could appear in token counts; the hint format "X-Y of 5"
	// distinguishes the scroll indicator from numeric data.
	if !strings.Contains(view, "of 5") {
		t.Errorf("run-detail view at height=%d should carry a scroll hint containing \"of 5\"; got:\n%s",
			height, view)
	}
}

// TestRunDetailScreen_ZeroAgents_DoesNotPanic verifies the edge case of a run
// with no agents (only an orchestrator) renders cleanly.
func TestRunDetailScreen_ZeroAgents_DoesNotPanic(t *testing.T) {
	run := buildScrollDetailRun(0) // orchestrator only
	s := screens.NewRunDetailScreen(run, 80, 10, plainStyles())

	view := s.View(80, 10)

	if view == "" {
		t.Error("RunDetailScreen.View with zero agents returned empty string")
	}
}

// ---------------------------------------------------------------------------
// Additional coverage added after review
// ---------------------------------------------------------------------------

// TestRunsScreen_ShortHeight_DoesNotPanic verifies that RunsScreen.View does
// not panic at very small heights (including zero) and that the title header
// and help bar are still present in the output.
//
// The contract states: "Rendering at height <= 0 or with zero runs does not
// panic." This test mirrors TestRunDetailScreen_ShortHeight_DoesNotPanic.
func TestRunsScreen_ShortHeight_DoesNotPanic(t *testing.T) {
	heights := []int{0, 1, 2, 3}
	report := buildScrollRunsReport(3)

	for _, h := range heights {
		t.Run(fmt.Sprintf("height=%d", h), func(t *testing.T) {
			s := screens.NewRunsScreen(report, 80, h, plainStyles())
			view := s.View(80, h)

			// Must not be empty — at least some content is always rendered.
			if view == "" {
				t.Errorf("RunsScreen.View(80, %d) returned empty string", h)
			}
			// Title must always be present.
			if !strings.Contains(view, "Log Analyzer") {
				t.Errorf("RunsScreen.View(80, %d) missing title header; got:\n%s", h, view)
			}
			// Help bar must always be present.
			if !strings.Contains(view, "↑/↓") {
				t.Errorf("RunsScreen.View(80, %d) missing help bar; got:\n%s", h, view)
			}
		})
	}
}

// TestRunDetailScreen_ReducedHeight_SelectedActorStillVisible verifies that
// rendering RunDetailScreen at a smaller height after scrolling to a mid-list
// actor keeps that actor visible and clips actors outside the viewport.
//
// This mirrors TestRunsScreen_ReducedHeight_SelectedRunStillVisible and
// exercises the AC2.4 invariant ("Rendering the same screen at a reduced
// height keeps the scroll offset valid") for RunDetailScreen.
//
// Arrange: 4 agents (5 actors total). Navigate to Agent#3 at a large height
// (all actors fit, so scroll=2 after 2 presses past orchestrator and Agent#1).
// Act:     Render at height=13 (≈ 2 actors visible in the content area).
// Assert:  Agent#3 is visible; Agent#1 (which has scrolled off) is not.
func TestRunDetailScreen_ReducedHeight_SelectedActorStillVisible(t *testing.T) {
	const largeHeight = 30
	const smallHeight = 13
	run := buildScrollDetailRun(4) // 1 orchestrator + 4 agents = 5 actors
	s := screens.NewRunDetailScreen(run, 80, largeHeight, plainStyles())

	// Scroll down 3 times to reach Agent#3 (orchestrator → Agent#1 → Agent#2 → Agent#3).
	for i := 0; i < 3; i++ {
		pressDetailDown(s)
	}

	// Render at reduced height — the selected actor must remain visible.
	view := s.View(80, smallHeight)

	if !strings.Contains(view, "Agent#3") {
		t.Errorf("Agent#3 should be visible at reduced height=%d after scrolling to it; not found in view:\n%s",
			smallHeight, view)
	}

	// Agent#1 should have scrolled off — it is above the viewport.
	if strings.Contains(view, "Agent#1") {
		t.Errorf("Agent#1 should be clipped at height=%d with scroll at Agent#3, but appears in view:\n%s",
			smallHeight, view)
	}
}

// buildScrollDetailRunMixed builds a domain.RunReport with one orchestrator and
// agentCount agents. Agents whose index (0-based) appears in modelledAgents
// get a non-empty Models slice (producing a 3-line actor row: label + totals +
// models); the rest have an empty Models slice (2-line row). The orchestrator
// always has no models (2-line row).
//
// This helper exists specifically to exercise the design requirement that
// "the line budget must be derived from the maximum per-item line count so a
// mixed list cannot overflow" (ContractsDesign.md, RunDetailScreen note).
func buildScrollDetailRunMixed(agentCount int, modelledAgents map[int]bool) domain.RunReport {
	agents := make([]domain.ActorReport, agentCount)
	for i := range agents {
		ar := domain.ActorReport{
			Actor: domain.AgentInstance(domain.AgentInstanceID(fmt.Sprintf("Agent#%d", i+1))),
			Totals: domain.Totals{
				Tokens: domain.TokenUsage{Input: domain.Tokens(100)},
				Money: domain.CategoryMoney{
					Total:    domain.KnownMoney(domain.Money(1_000_000_000)),
					Complete: true,
				},
			},
		}
		if modelledAgents[i] {
			ar.Models = []domain.ModelID{domain.ModelID(fmt.Sprintf("model-agent%d", i+1))}
		}
		agents[i] = ar
	}
	return domain.RunReport{
		Run: domain.NamedRun("20260101T120000Z-mixd"),
		Totals: domain.Totals{
			Tokens: domain.TokenUsage{Input: domain.Tokens(int64(agentCount+1) * 100)},
			Money: domain.CategoryMoney{
				Total:    domain.KnownMoney(domain.Money(int64(agentCount+1) * 1_000_000_000)),
				Complete: true,
			},
		},
		Orchestrator: domain.ActorReport{
			Actor: domain.Orchestrator(),
			Totals: domain.Totals{
				Tokens: domain.TokenUsage{Input: domain.Tokens(100)},
				Money: domain.CategoryMoney{
					Total:    domain.KnownMoney(domain.Money(1_000_000_000)),
					Complete: true,
				},
			},
			// No Models: orchestrator is always a 2-line row.
		},
		Agents: agents,
	}
}

// ---------------------------------------------------------------------------
// Additional coverage: mixed actor-row line counts (Major review finding)
// ---------------------------------------------------------------------------

// TestRunDetailScreen_MixedLineCountActors_NoOverflow verifies that when the
// actor list contains both 2-line rows (no models) and 3-line rows (with
// models), the visible item count is derived from the worst-case line count
// (3 lines per actor), not the best-case or the orchestrator's line count.
//
// Actor list (4 actors total):
//   [0] Orchestrator — 2-line row (no models)
//   [1] Agent#1      — 3-line row (has model "model-agent1")
//   [2] Agent#2      — 2-line row (no models)
//   [3] Agent#3      — 3-line row (has model "model-agent3")
//
// At height=15, with fixed overhead ≈ 9 lines (title+subtitle+sep+blank+
// run-totals+totals+blank+sep+help), available ≈ 6 lines.
//
//   Correct implementation (linesPerItem = max = 3):
//     VisibleItems(6, 3) = 2 actors → only [0] and [1] are rendered.
//     Agent#2 is NOT in the view.
//
//   Buggy implementation (linesPerItem derived from orch's 2 lines, or min):
//     VisibleItems(6, 2) = 3 actors → [0], [1], [2] rendered → Agent#2 IS visible,
//     and the 3rd actor's extra line overflows the height budget.
//
// The header and help bar must both be present: their absence indicates that the
// actor content overflowed the height, which is the exact failure mode being
// guarded against.
func TestRunDetailScreen_MixedLineCountActors_NoOverflow(t *testing.T) {
	const height = 15

	// 3 agents: index 0 (Agent#1) has models, index 1 (Agent#2) does not,
	// index 2 (Agent#3) has models.
	run := buildScrollDetailRunMixed(3, map[int]bool{0: true, 2: true})
	s := screens.NewRunDetailScreen(run, 80, height, plainStyles())

	view := s.View(80, height)

	// Header must be present — indicates no overflow.
	if !strings.Contains(view, "Log Analyzer") {
		t.Errorf("RunDetailScreen.View(80, %d) missing title header (possible overflow); got:\n%s",
			height, view)
	}

	// Help bar must be present — indicates no overflow.
	if !strings.Contains(view, "↑/↓") {
		t.Errorf("RunDetailScreen.View(80, %d) missing help bar (possible overflow); got:\n%s",
			height, view)
	}

	// Agent#2 (3-line-count actor at index 2) must NOT be visible when scroll=0.
	// A correct impl uses linesPerItem=3 (max), yielding 2 visible actors ([0],[1]).
	// A buggy impl uses linesPerItem=2 (min), yielding 3 visible actors and showing
	// Agent#2 — while also overflowing the height with the extra model line.
	if strings.Contains(view, "Agent#2") {
		t.Errorf("Agent#2 should be clipped at height=%d scroll=0 when linesPerItem=max(3): "+
			"its presence indicates the viewport used the minimum (2) rather than maximum (3) "+
			"per-item line count; view:\n%s", height, view)
	}
}

// ---------------------------------------------------------------------------
// Additional coverage: variable fixed-overhead sections (Major review finding)
// ---------------------------------------------------------------------------

// TestRunsScreen_WithUnattributable_ClipsCorrectly verifies that when the
// report includes an Unattributable bucket (which adds ~4 lines to the fixed
// overhead), the visible run count is derived from the lines actually consumed
// by all fixed blocks — not from a hardcoded constant.
//
// Overhead breakdown at height=15 with Unattributable present:
//   Header (title+sep+blank+AllRuns+totals+blank+Runs(N)):  7 lines
//   Unattributable section (blank+sep+label+totals):        4 lines
//   Footer (sep+help):                                      2 lines
//   Total:                                                 13 lines — available = 2
//   VisibleItems(2, 2) = 1 run (only run[0] at cursor=0)
//
//   Buggy implementation (overhead hardcoded to 9, ignoring Unattributable):
//     VisibleItems(6, 2) = 3 runs — renders run[0..2] — then the 4 unattributable
//     lines push total output to 9+6+4=19 lines, overflowing height=15 and
//     dropping the help bar off the visible area.
//
// Assertions: run[0] visible (cursor), run[2] NOT visible (clipped), header and
// help bar present (no overflow).
func TestRunsScreen_WithUnattributable_ClipsCorrectly(t *testing.T) {
	const height = 15

	report := buildScrollRunsReport(5) // 5 named runs
	// Attach an unattributable bucket — this adds 4 lines to the fixed overhead.
	unattribTotals := domain.Totals{
		Tokens: domain.TokenUsage{Input: domain.Tokens(50)},
		Money: domain.CategoryMoney{
			Total:    domain.KnownMoney(domain.Money(500_000_000)),
			Complete: true,
		},
	}
	report.Unattributable = &domain.RunReport{
		Run:    domain.UnattributableRun(),
		Totals: unattribTotals,
	}

	s := screens.NewRunsScreen(report, 80, height, plainStyles())
	view := s.View(80, height)

	// Header must be present.
	if !strings.Contains(view, "Log Analyzer") {
		t.Errorf("RunsScreen.View(80, %d) missing title header; got:\n%s", height, view)
	}

	// Help bar must be present — its absence signals the unattributable section
	// consumed lines that the fixed-overhead calculation failed to reserve.
	if !strings.Contains(view, "↑/↓") {
		t.Errorf("RunsScreen.View(80, %d) missing help bar (likely overflow from "+
			"un-accounted unattributable section); got:\n%s", height, view)
	}

	// run[0] (cursor) must be visible.
	if !strings.Contains(view, runSuffix(0)) {
		t.Errorf("run[0] (%q) should be visible at cursor=0 height=%d with Unattributable present; "+
			"not found in view:\n%s", runSuffix(0), height, view)
	}

	// run[2] must NOT be visible: with correct overhead=13 only 1 run fits.
	// If it appears, the implementation used a hardcoded overhead that ignores the
	// unattributable section, incorrectly computing 3 visible runs.
	if strings.Contains(view, runSuffix(2)) {
		t.Errorf("run[2] (%q) should be clipped at height=%d when Unattributable adds 4 fixed "+
			"lines (correct available=2, VisibleItems=1); its presence indicates a hardcoded "+
			"fixed-overhead constant rather than deriving from the actually-rendered fixed blocks; "+
			"view:\n%s", runSuffix(2), height, view)
	}
}

// TestRunDetailScreen_WithUnpricedModels_ClipsCorrectly verifies that when the
// run has unpriced models (which adds ~2 lines to the fixed footer), the visible
// actor count is derived from the lines actually consumed by all fixed blocks.
//
// Overhead breakdown at height=13 with UnpricedModels present:
//   Header (title+subtitle+sep+blank+RunTotals+totals+blank):  7 lines
//   Unpriced-models notice (blank+warning line):                2 lines
//   Footer (sep+help):                                          2 lines
//   Total:                                                     11 lines — available = 2
//   VisibleItems(2, 2) = 1 actor (only orchestrator at scroll=0)
//
//   Buggy implementation (overhead hardcoded to 9, ignoring unpriced notice):
//     VisibleItems(4, 2) = 2 actors — renders orch and Agent#1 — then the 2
//     unpriced lines push total to 9+4+2=15 lines, overflowing height=13 and
//     dropping the help bar off the visible area.
//
// Assertions: orchestrator visible (first actor at scroll=0), Agent#1 NOT visible
// (clipped), header and help bar present (no overflow).
func TestRunDetailScreen_WithUnpricedModels_ClipsCorrectly(t *testing.T) {
	const height = 13

	// 1 orchestrator + 3 agents, all 2-line rows (no models on any actor).
	run := buildScrollDetailRun(3)
	// Attach unpriced models at the run level — adds 2 lines to the fixed footer.
	run.UnpricedModels = []domain.ModelID{"model-x"}

	s := screens.NewRunDetailScreen(run, 80, height, plainStyles())
	view := s.View(80, height)

	// Header must be present.
	if !strings.Contains(view, "Log Analyzer") {
		t.Errorf("RunDetailScreen.View(80, %d) missing title header; got:\n%s", height, view)
	}

	// Help bar must be present — its absence signals the unpriced notice consumed
	// lines that the fixed-overhead calculation failed to reserve.
	if !strings.Contains(view, "↑/↓") {
		t.Errorf("RunDetailScreen.View(80, %d) missing help bar (likely overflow from "+
			"un-accounted unpriced-models notice); got:\n%s", height, view)
	}

	// The orchestrator (first actor, scroll=0) must be visible.
	if !strings.Contains(view, "orchestrator") {
		t.Errorf("orchestrator actor row should be visible at scroll=0 height=%d with "+
			"UnpricedModels present; not found in view:\n%s", height, view)
	}

	// Agent#1 must NOT be visible: with correct overhead=11 only 1 actor fits.
	// If it appears, the implementation used a hardcoded overhead that ignores the
	// unpriced-models notice, incorrectly computing 2 visible actors.
	if strings.Contains(view, "Agent#1") {
		t.Errorf("Agent#1 should be clipped at height=%d when UnpricedModels adds 2 fixed "+
			"footer lines (correct available=2, VisibleItems=1); its presence indicates a "+
			"hardcoded fixed-overhead constant that ignores the unpriced-models notice; "+
			"view:\n%s", height, view)
	}
}

// TestRunsScreen_ExactlyOneRunFits_SelectedRunVisible verifies the boundary
// case where exactly one run fits in the content area.
//
// The fixed overhead of RunsScreen (title + sep + blank + "All runs" + totals
// + blank + "Runs(N)" = 7 header lines, sep + help = 2 footer lines = 9 total)
// means that at height=11, availableLines=2, and with 2 lines per run,
// VisibleItems(2, 2) = 1 run visible.  With 5 runs at cursor=0, only run[0]
// should appear; run[4] must not.
func TestRunsScreen_ExactlyOneRunFits_SelectedRunVisible(t *testing.T) {
	// height=11 → availableLines = 11 - 9 = 2 → VisibleItems(2, 2) = 1 run.
	const height = 11
	report := buildScrollRunsReport(5)
	s := screens.NewRunsScreen(report, 80, height, plainStyles())

	view := s.View(80, height)

	// The selected run (cursor=0) must be visible.
	if !strings.Contains(view, runSuffix(0)) {
		t.Errorf("run[0] (%q) should be visible when exactly one run fits (height=%d); not found in view:\n%s",
			runSuffix(0), height, view)
	}

	// run[4] must be clipped — it is beyond the single-item viewport.
	if strings.Contains(view, runSuffix(4)) {
		t.Errorf("run[4] (%q) should be clipped when exactly one run fits (height=%d), but appears in view:\n%s",
			runSuffix(4), height, view)
	}
}
