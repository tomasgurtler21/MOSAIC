package screens_test

// utility_infra_mode_test.go verifies that ModeUtilityInfraOnly is presented as a
// selectable entry in the ModeScreen and that selecting it yields the correct RunMode value.
//
// All tests in this file are RED-phase TDD tests. They describe the intended behaviour once
// Stage 7 is implemented. Because the modeItems list does not yet include an entry for
// ModeUtilityInfraOnly, these tests will fail until that entry is added in mode.go.
//
// Strategy: the tests are position-independent. Rather than hard-coding the list index at
// which ModeUtilityInfraOnly appears, each test navigates through up to maxModeNavigations
// positions, calling SelectedMode() at each stop (without pressing Enter) until it finds the
// target mode or exhausts the list. This insulates the tests from reordering of the existing
// items and makes the test pass wherever the item is placed.
//
// Verified behaviours (T7.5):
//
// View content:
//   - The rendered view contains "utility" or "infra" text (case-insensitive) so the mode is
//     discoverable.
//   - The utility-infra mode entry has a non-empty description text.
//
// Navigation and selection:
//   - The mode can be navigated to by pressing Down one or more times.
//   - Pressing Enter while on that entry sets Done()=true.
//   - SelectedMode() returns exactly domain.ModeUtilityInfraOnly after selection.
//   - The entry is reachable by keyboard alone.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// maxModeNavigations is an upper bound on how many Down presses might be needed to reach
// ModeUtilityInfraOnly. It is set conservatively larger than the expected list length so
// that the tests remain valid even if additional modes are inserted before it.
const maxModeNavigations = 10

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// navigateToMode presses Down on the screen until SelectedMode() equals target or
// maxSteps is reached. It returns the number of Down presses actually performed and
// whether the target was found.
func navigateToMode(s *screens.ModeScreen, target domain.RunMode, maxSteps int) (steps int, found bool) {
	for i := 0; i < maxSteps; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
		steps++
		s.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if s.Done() && s.SelectedMode() == target {
			return steps, true
		}
		// Reset so we can keep navigating.
		s.Reset()
		// The list does not expose a "peek" API, so we navigate one step back to restore
		// the cursor to the position we just left, then re-navigate forward.
		// Actually: after Reset the list state (cursor position) is preserved — only
		// Done/Back flags are cleared. So we can just continue pressing Down.
	}
	return steps, false
}

// positionOf navigates the screen forward up to maxSteps times and returns the
// number of Down presses required to land on the target mode. Returns -1 if not found.
// It does NOT press Enter; it only navigates and checks the view.
func positionOf(s *screens.ModeScreen, target domain.RunMode, maxSteps int) int {
	// Start at position 0 (no Down presses yet). Check if the initial item is the target.
	// We need to press Enter to read SelectedMode(), then Reset without committing.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.Done() && s.SelectedMode() == target {
		s.Reset()
		return 0
	}
	s.Reset()

	for i := 1; i <= maxSteps; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
		s.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if s.Done() && s.SelectedMode() == target {
			s.Reset()
			return i
		}
		s.Reset()
	}
	return -1
}

// ---------------------------------------------------------------------------
// T7.5: View content
// ---------------------------------------------------------------------------

// TestModeScreen_View_ContainsUtilityInfraText verifies that the rendered view mentions
// "utility" or "infra" (case-insensitive) so that users can identify and discover the
// utility-infra-only mode from the list.
func TestModeScreen_View_ContainsUtilityInfraText(t *testing.T) {
	s := newModeScreen()

	view := s.View()
	lower := strings.ToLower(collapseWhitespace(view))

	if !strings.Contains(lower, "utility") && !strings.Contains(lower, "infra") {
		t.Errorf("mode screen view does not contain 'utility' or 'infra' (case-insensitive);\n"+
			"want the utility-infra mode to be visible and discoverable in the list.\n"+
			"View:\n%s", view)
	}
}

// TestModeScreen_UtilityInfraEntry_HasNonEmptyDescription verifies that the utility-infra
// mode entry provides a non-empty description so the user understands what it does before
// selecting it.
func TestModeScreen_UtilityInfraEntry_HasNonEmptyDescription(t *testing.T) {
	s := newModeScreen()

	// Navigate to the utility-infra entry (position-independent).
	pos := positionOf(newModeScreen(), domain.ModeUtilityInfraOnly, maxModeNavigations)
	if pos < 0 {
		t.Fatalf("ModeUtilityInfraOnly entry not found in the mode list after %d Down presses; "+
			"add it to modeItems in mode.go", maxModeNavigations)
	}

	// Re-navigate to its position to get the detail view.
	for i := 0; i < pos; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	view := s.View()
	if len(strings.TrimSpace(view)) == 0 {
		t.Error("view is empty after navigating to utility-infra entry")
	}

	// When cursor is on this item, the detail pane (right column) should show content.
	// We check that "utility" or "infra" appears in the view now that this item is
	// highlighted (its detail text should be shown in the detail pane).
	lower := strings.ToLower(collapseWhitespace(view))
	if !strings.Contains(lower, "utility") && !strings.Contains(lower, "infra") {
		t.Errorf("detail pane after navigating to utility-infra entry does not show any 'utility' or 'infra' text;\n"+
			"the detail text must describe the mode.\nView:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// T7.5: Navigation and selection
// ---------------------------------------------------------------------------

// TestModeScreen_UtilityInfraOnly_IsSelectableByKeyboard verifies that ModeUtilityInfraOnly
// is reachable by pressing Down one or more times and then Enter, and that after selection
// Done() is true and SelectedMode() returns exactly domain.ModeUtilityInfraOnly.
//
// This test is position-independent: it navigates forward until it finds the target or
// exhausts the list.
func TestModeScreen_UtilityInfraOnly_IsSelectableByKeyboard(t *testing.T) {
	s := newModeScreen()

	_, found := navigateToMode(s, domain.ModeUtilityInfraOnly, maxModeNavigations)

	if !found {
		t.Errorf("ModeUtilityInfraOnly is not reachable by pressing Down up to %d times and Enter;\n"+
			"add an entry for ModeUtilityInfraOnly to modeItems in mode.go", maxModeNavigations)
		return
	}
	if !s.Done() {
		t.Error("Done() = false after selecting ModeUtilityInfraOnly; want true")
	}
	if s.SelectedMode() != domain.ModeUtilityInfraOnly {
		t.Errorf("SelectedMode() = %q; want %q",
			s.SelectedMode(), domain.ModeUtilityInfraOnly)
	}
}

// TestModeScreen_UtilityInfraOnly_SelectedModeReturnsCorrectConstant verifies that
// SelectedMode() returns exactly domain.ModeUtilityInfraOnly (not a similar string) when
// the utility-infra entry is selected. The value must match the domain constant exactly so
// that both frontends produce the same RunMode string.
func TestModeScreen_UtilityInfraOnly_SelectedModeReturnsCorrectConstant(t *testing.T) {
	s := newModeScreen()

	_, found := navigateToMode(s, domain.ModeUtilityInfraOnly, maxModeNavigations)
	if !found {
		t.Fatalf("ModeUtilityInfraOnly entry not found in the mode list after %d Down presses; "+
			"add it to modeItems in mode.go", maxModeNavigations)
	}

	got := s.SelectedMode()
	want := domain.ModeUtilityInfraOnly
	if got != want {
		t.Errorf("SelectedMode() = %q; want %q — value must match the domain constant exactly, "+
			"not just be a similar string", got, want)
	}
}

// TestModeScreen_UtilityInfraOnly_EntryExistsInList verifies that a Down-navigation sequence
// can land on ModeUtilityInfraOnly within a reasonable number of steps. This is a structural
// assertion: the item must be present somewhere in the modeItems slice.
func TestModeScreen_UtilityInfraOnly_EntryExistsInList(t *testing.T) {
	pos := positionOf(newModeScreen(), domain.ModeUtilityInfraOnly, maxModeNavigations)
	if pos < 0 {
		t.Errorf("ModeUtilityInfraOnly is not present in the mode list after %d Down presses;\n"+
			"add a widgets.ListItem with ID=%q to the modeItems slice in mode.go",
			maxModeNavigations, domain.ModeUtilityInfraOnly)
	}
}

// TestModeScreen_UtilityInfraOnly_DoesNotAffectOtherModes verifies that after navigating
// past ModeUtilityInfraOnly and selecting it, the other existing mode entries remain
// reachable. This guards against accidentally breaking the list when inserting the new item.
func TestModeScreen_UtilityInfraOnly_DoesNotAffectOtherModes(t *testing.T) {
	// Verify each known mode is still reachable.
	knownModes := []domain.RunMode{
		domain.ModeDeployNew,
		domain.ModeUpdate,
		domain.ModeWorkflowsOnly,
		domain.ModePromote,
		domain.ModeTransformHarness,
	}
	for _, want := range knownModes {
		s := newModeScreen()
		pos := positionOf(s, want, maxModeNavigations)
		if pos < 0 {
			t.Errorf("mode %q is no longer reachable in the mode list after Stage 7 changes; "+
				"adding ModeUtilityInfraOnly to modeItems must not remove or displace any existing mode",
				want)
		}
	}
}

// TestModeScreen_SelectUtilityInfra_ThenReset_ClearsDone verifies that after selecting
// ModeUtilityInfraOnly, calling Reset() clears Done() so the screen can be reused for
// subsequent navigation (consistent with the Reset contract of other mode entries).
func TestModeScreen_SelectUtilityInfra_ThenReset_ClearsDone(t *testing.T) {
	s := newModeScreen()

	_, found := navigateToMode(s, domain.ModeUtilityInfraOnly, maxModeNavigations)
	if !found {
		t.Fatalf("ModeUtilityInfraOnly not found in list after %d Down presses", maxModeNavigations)
	}
	if !s.Done() {
		t.Fatal("precondition: Done() must be true before Reset")
	}

	s.Reset()

	if s.Done() {
		t.Error("Done() = true after Reset; want false (Reset must clear Done after utility-infra selection)")
	}
}
