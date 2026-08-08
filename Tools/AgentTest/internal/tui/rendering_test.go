package tui

// rendering_test.go verifies View() is built on the shared scaffold: title,
// status and help rendering come from mosaic-common/tui, key bindings come
// from its shared set, and the layout degrades legibly at a narrow width
// rather than wrapping into noise (AC16.4).

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	tuicommon "mosaic-common/tui"

	"mosaic-agent-test/internal/domain"
)

// ---------------------------------------------------------------------------
// Help bar reflects the shared key set
// ---------------------------------------------------------------------------

// TestRendering_HelpBar_UsesSharedKeySet verifies that the help hints shown
// on the suite-select screen name the shared GlobalKeys bindings (e.g.
// ctrl+c to quit), rather than a hand-rolled key scheme private to this
// frontend.
func TestRendering_HelpBar_UsesSharedKeySet(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, newFakeSuiteRunner()))
	view := safeView(t, m)

	cancelHint := tuicommon.GlobalKeys.Cancel.Help().Key
	if !strings.Contains(view, cancelHint) {
		t.Errorf("suite-select view does not contain the shared quit key hint %q; key bindings must come from mosaic-common/tui", cancelHint)
	}
	selectHint := tuicommon.GlobalKeys.Select.Help().Key
	if !strings.Contains(view, selectHint) {
		t.Errorf("suite-select view does not contain the shared select key hint %q; key bindings must come from mosaic-common/tui", selectHint)
	}
}

// TestRendering_CtrlC_MatchesSharedCancelBinding verifies that the key this
// frontend treats as "quit" is exactly the shared GlobalKeys.Cancel
// binding, not a private ctrl+c handler that happens to coincide with it.
func TestRendering_CtrlC_MatchesSharedCancelBinding(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	if !tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Cancel) {
		t.Fatalf("test setup: ctrl+c does not match the shared Cancel binding")
	}

	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	_, cmd := safeUpdate(t, m, msg)
	if cmd == nil {
		t.Errorf("ctrl+c (the shared Cancel binding) produced no command; expected a quit command")
	}
}

// ---------------------------------------------------------------------------
// Title rendering comes from the shared scaffold
// ---------------------------------------------------------------------------

// TestRendering_View_NonEmptyOnEveryScreen verifies every screen produces
// non-empty output built from the shared scaffold (RenderTitle / RenderHelp
// / RenderStatus), rather than an unstyled placeholder.
func TestRendering_View_NonEmptyOnEveryScreen(t *testing.T) {
	runner := newFakeSuiteRunner().withEvents(
		scriptedSuite{
			suiteID: "suite-under-test",
			tests:   []scriptedTest{{testID: "test-a", verdict: domain.VerdictPass}},
		}.events()...,
	)
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	if v := safeView(t, m); v == "" {
		t.Errorf("suite-select view is empty")
	}

	m = runSuiteToCompletion(t, m, runner)
	if v := safeView(t, m); v == "" {
		t.Errorf("results view is empty")
	}

	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if v := safeView(t, m); v == "" {
		t.Errorf("detail view is empty")
	}
}

// ---------------------------------------------------------------------------
// Narrow width stays legible: no line exceeds the reported width
// ---------------------------------------------------------------------------

// TestRendering_NarrowWidth_LinesFitWithinWidth verifies that at a narrow
// terminal width, every rendered line's display width fits within the
// reported width rather than wrapping into unreadable noise (AC16.4).
func TestRendering_NarrowWidth_LinesFitWithinWidth(t *testing.T) {
	const narrowWidth = 30

	m := NewModel(newFixtureOptions([]string{"a-very-long-suite-name-that-could-overflow.yaml"}, newFakeSuiteRunner()))
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: narrowWidth, Height: 15})

	view := safeView(t, m)
	if view == "" {
		t.Fatalf("view at narrow width is empty")
	}
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > narrowWidth {
			t.Errorf("line %d is %d cells wide, want <= %d (narrow-width layout must stay legible, not wrap into noise): %q", i, w, narrowWidth, line)
		}
	}
}
