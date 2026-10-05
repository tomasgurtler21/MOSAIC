package tui

// nav_runselectscreen_test.go verifies setup.RunSelectScreen directly: the
// new-run sentinel, selection/back key handling, and the rendering and
// activation rules for unresumable choices.
//
// Tests are in package tui (internal) so fixtures can be shared with the
// rootModel-level navigation tests in this package.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
	"mosaic-run/internal/tui/screens/setup"
)

// ---------------------------------------------------------------------------
// Run selection screen: RunSelectScreen screen type
// ---------------------------------------------------------------------------

// TestRunSelectScreen_NewRunSentinelID verifies that the sentinel constant is defined
// and has the expected value.
func TestRunSelectScreen_NewRunSentinelID(t *testing.T) {
	const wantID = "__new_run__"
	if setup.NewRunSentinelID != wantID {
		t.Errorf("NewRunSentinelID = %q, want %q", setup.NewRunSentinelID, wantID)
	}
}

// TestRunSelectScreen_IsNewRun_TrueOnFirstEntry verifies that the newly constructed
// RunSelectScreen has "Start a new run" as the first (selected) item, so IsNewRun()
// returns true without any navigation.
func TestRunSelectScreen_IsNewRun_TrueOnFirstEntry(t *testing.T) {
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		newTestResumeChoice("20260701T120000Z-a3f9"),
	}}
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := setup.NewRunSelectScreen(q, 80, 24, style)

	// Simulate selection (Enter) without navigating.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter; want true")
	}
	if !s.IsNewRun() {
		t.Error("IsNewRun() = false when first item was selected; want true (first item is 'Start new run')")
	}
	if s.SelectedChoiceID() != setup.NewRunSentinelID {
		t.Errorf("SelectedChoiceID() = %q when IsNewRun() is true; want %q", s.SelectedChoiceID(), setup.NewRunSentinelID)
	}
}

// TestRunSelectScreen_SelectedChoiceID_AfterNavigation verifies that navigating to a
// resumable choice and pressing Enter sets SelectedChoiceID correctly.
func TestRunSelectScreen_SelectedChoiceID_AfterNavigation(t *testing.T) {
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		newTestResumeChoice("20260701T120000Z-a3f9"),
	}}
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := setup.NewRunSelectScreen(q, 80, 24, style)

	// Navigate past "Start new run" to the candidate.
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter on candidate; want true")
	}
	if s.IsNewRun() {
		t.Error("IsNewRun() = true after selecting candidate; want false")
	}
	if s.SelectedChoiceID() != "20260701T120000Z-a3f9" {
		t.Errorf("SelectedChoiceID() = %q, want %q", s.SelectedChoiceID(), "20260701T120000Z-a3f9")
	}
}

// TestRunSelectScreen_Back_TrueOnEsc verifies that pressing Esc sets Back() to true.
func TestRunSelectScreen_Back_TrueOnEsc(t *testing.T) {
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		newTestResumeChoice("20260701T120000Z-a3f9"),
	}}
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := setup.NewRunSelectScreen(q, 80, 24, style)

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc; want true")
	}
}

// ---------------------------------------------------------------------------
// Run selection screen: unresumable choices (T2.4 / AC2.5, TUI side)
// ---------------------------------------------------------------------------

// TestRunSelectScreen_UnresumableChoice_RenderedWithReason verifies that an
// unresumable choice appears in the rendered list, carrying the reason it
// cannot be resumed (runscan.UnresumableReason.Description()).
//
// Currently fails (RED): NewRunSelectScreen does not yet render
// ChoiceUnresumable entries at all (see the RED placeholder comment on
// NewRunSelectScreen in screens/setup.go); the reason text is absent from
// the view entirely, not merely missing a specific word.
func TestRunSelectScreen_UnresumableChoice_RenderedWithReason(t *testing.T) {
	unresumableID := "20260601T090000Z-1234"
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		newTestResumeChoice("20260701T120000Z-a3f9"),
		newTestUnresumableChoice(unresumableID, runscan.ReasonCompleted),
	}}
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := setup.NewRunSelectScreen(q, 80, 24, style)

	view := s.View()
	if !containsAny(view, unresumableID) {
		t.Errorf("unresumable run %q does not appear in the rendered view at all; it must be shown, not hidden.\nview:\n%s", unresumableID, view)
	}
	if !containsAny(view, runscan.ReasonCompleted.Description()) {
		t.Errorf("unresumable run's reason (%q) does not appear in the rendered view.\nview:\n%s", runscan.ReasonCompleted.Description(), view)
	}
}

// TestRunSelectScreen_UnresumableChoice_CannotBeActivated verifies that
// pressing Enter while the cursor would land on an unresumable choice is a
// no-op: cursor movement skips non-selectable items entirely (mirroring
// widgets.List's existing Disabled-item behaviour), so Done() never becomes
// true by landing on one.
//
// Currently fails (RED): the unresumable choice is not in the list at all
// (see TestRunSelectScreen_UnresumableChoice_RenderedWithReason), so this
// test cannot observe the intended "present but skipped" behaviour; as
// written it exercises the actually-reachable items only and documents the
// contract cursor movement must satisfy once I2.4 adds them.
func TestRunSelectScreen_UnresumableChoice_CannotBeActivated(t *testing.T) {
	unresumableID := "20260601T090000Z-1234"
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		newTestUnresumableChoice(unresumableID, runscan.ReasonCompleted),
		newTestResumeChoice("20260701T120000Z-a3f9"),
	}}
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := setup.NewRunSelectScreen(q, 80, 24, style)

	// Move down once: with the unresumable entry present and non-selectable,
	// the cursor must skip it and land on the resumable candidate, not on the
	// unresumable entry.
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Fatal("Done() = false after Enter on the item following 'start new run'; want true (a selectable item was reachable)")
	}
	if s.SelectedChoiceID() == unresumableID {
		t.Errorf("SelectedChoiceID() = %q; the unresumable choice must never be selectable", unresumableID)
	}
	if s.SelectedChoiceID() != "20260701T120000Z-a3f9" {
		t.Errorf("SelectedChoiceID() = %q, want %q (cursor must skip the non-selectable unresumable entry)",
			s.SelectedChoiceID(), "20260701T120000Z-a3f9")
	}
}

// TestRunSelectScreen_NewRunReachable_WithUnresumableEntriesPresent verifies
// that "start a new run" remains the first, immediately activatable item
// even when the question also carries unresumable entries -- not just when
// every choice is resumable, which is the only shape the pre-existing
// reachability tests in this file exercise.
func TestRunSelectScreen_NewRunReachable_WithUnresumableEntriesPresent(t *testing.T) {
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		newTestUnresumableChoice("20260601T090000Z-1234", runscan.ReasonCompleted),
		newTestUnresumableChoice("20260602T090000Z-5678", runscan.ReasonCompleted),
	}}
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := setup.NewRunSelectScreen(q, 80, 24, style)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Fatal("Done() = false after Enter on the first item; want true")
	}
	if !s.IsNewRun() {
		t.Error("IsNewRun() = false; want true -- 'start a new run' must be reachable and activatable " +
			"even when the question also contains only unresumable entries besides it")
	}
}
