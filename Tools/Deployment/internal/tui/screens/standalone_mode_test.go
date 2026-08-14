package screens_test

// standalone_mode_test.go verifies that ModeStandaloneOnly is presented as a
// selectable entry in the ModeScreen and that selecting it yields the correct RunMode value.
//
// All tests in this file are RED-phase TDD tests. Because the modeItems list does not yet
// include an entry for ModeStandaloneOnly, every test that navigates the mode list to find
// it will fail until that entry is added in mode.go.
//
// Strategy: position-independent navigation. Tests press Down up to maxModeNavigations
// times and call SelectedMode() after Enter at each step. This insulates tests from
// reordering of existing items. positionOf and navigateToMode from utility_infra_mode_test.go
// are reused (same package).
//
// Verified behaviours:
//
// View content:
//   - The rendered view contains "standalone" text (case-insensitive).
//   - The standalone entry's detail text contains "standalone", "deploy", and "only",
//     asserting that ONLY deploy is supported for standalone agents (AC6.2).
//   - The entry has a non-empty description.
//
// Navigation and selection:
//   - ModeStandaloneOnly is present in the list within maxModeNavigations Down presses.
//   - Pressing Enter while on that entry sets Done()=true.
//   - SelectedMode() returns exactly domain.ModeStandaloneOnly after selection (AC6.3).
//   - The entry is reachable by keyboard alone.
//   - All pre-existing modes remain reachable after the new entry is inserted.
//   - Reset() clears Done() after selecting ModeStandaloneOnly.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// View content
// ---------------------------------------------------------------------------

// TestModeScreen_View_ContainsStandaloneText verifies that the rendered view mentions
// "standalone" (case-insensitive) so users can identify and discover the standalone mode.
func TestModeScreen_View_ContainsStandaloneText(t *testing.T) {
	s := newModeScreen()

	view := s.View()
	lower := strings.ToLower(collapseWhitespace(view))

	if !strings.Contains(lower, "standalone") {
		t.Errorf("mode screen view does not contain 'standalone' (case-insensitive);\n"+
			"want the standalone deploy mode to be visible and discoverable in the list.\n"+
			"View:\n%s", view)
	}
}

// TestModeScreen_StandaloneEntry_DetailMentionsOnlyDeploy verifies that after navigating
// to the standalone entry, the detail text states that only deploy is supported for
// standalone agents, satisfying AC6.2.
func TestModeScreen_StandaloneEntry_DetailMentionsOnlyDeploy(t *testing.T) {
	pos := positionOf(newModeScreen(), domain.ModeStandaloneOnly, maxModeNavigations)
	if pos < 0 {
		t.Fatalf("ModeStandaloneOnly entry not found in the mode list after %d Down presses; "+
			"add it to modeItems in mode.go", maxModeNavigations)
	}

	s := newModeScreen()
	for i := 0; i < pos; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	view := s.View()
	lower := strings.ToLower(collapseWhitespace(view))

	// AC6.2: detail must make clear that only deploy is supported for standalone agents.
	if !strings.Contains(lower, "standalone") {
		t.Errorf("detail pane after navigating to standalone entry does not contain 'standalone';\n"+
			"the detail text must describe the mode.\nView:\n%s", view)
	}
	if !strings.Contains(lower, "deploy") {
		t.Errorf("detail pane after navigating to standalone entry does not mention 'deploy';\n"+
			"the detail text must state that only deploy is supported for standalone agents (AC6.2).\n"+
			"View:\n%s", view)
	}
	// AC6.2 requires exclusivity: the text must convey that ONLY deploy is supported.
	// A detail that mentions both "deploy" and other modes without "only" or equivalent
	// would satisfy the two checks above but violate the acceptance criterion.
	if !strings.Contains(lower, "only") {
		t.Errorf("detail pane after navigating to standalone entry does not contain 'only';\n"+
			"AC6.2 requires the detail text to state that only deploy is supported, not just that\n"+
			"deploy is supported. Add the word 'only' (or equivalent exclusivity phrasing) to the\n"+
			"mode entry's detail text in mode.go.\nView:\n%s", view)
	}
}

// TestModeScreen_StandaloneEntry_HasNonEmptyDescription verifies that the standalone
// mode entry provides a non-empty description so users understand what it does.
func TestModeScreen_StandaloneEntry_HasNonEmptyDescription(t *testing.T) {
	pos := positionOf(newModeScreen(), domain.ModeStandaloneOnly, maxModeNavigations)
	if pos < 0 {
		t.Fatalf("ModeStandaloneOnly entry not found in the mode list after %d Down presses; "+
			"add it to modeItems in mode.go", maxModeNavigations)
	}

	s := newModeScreen()
	for i := 0; i < pos; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	view := s.View()
	if len(strings.TrimSpace(view)) == 0 {
		t.Error("view is empty after navigating to standalone entry; want non-empty description")
	}
}

// ---------------------------------------------------------------------------
// Navigation and selection
// ---------------------------------------------------------------------------

// TestModeScreen_StandaloneOnly_EntryExistsInList verifies that a Down-navigation sequence
// can land on ModeStandaloneOnly within a reasonable number of steps. This is a structural
// assertion: the item must be present somewhere in the modeItems slice.
func TestModeScreen_StandaloneOnly_EntryExistsInList(t *testing.T) {
	pos := positionOf(newModeScreen(), domain.ModeStandaloneOnly, maxModeNavigations)
	if pos < 0 {
		t.Errorf("ModeStandaloneOnly is not present in the mode list after %d Down presses;\n"+
			"add a widgets.ListItem with ID=%q to the modeItems slice in mode.go",
			maxModeNavigations, domain.ModeStandaloneOnly)
	}
}

// TestModeScreen_StandaloneOnly_IsSelectableByKeyboard verifies that ModeStandaloneOnly
// is reachable by pressing Down one or more times and then Enter, and that after selection
// Done() is true and SelectedMode() returns exactly domain.ModeStandaloneOnly (AC6.3).
func TestModeScreen_StandaloneOnly_IsSelectableByKeyboard(t *testing.T) {
	s := newModeScreen()

	_, found := navigateToMode(s, domain.ModeStandaloneOnly, maxModeNavigations)

	if !found {
		t.Errorf("ModeStandaloneOnly is not reachable by pressing Down up to %d times and Enter;\n"+
			"add an entry for ModeStandaloneOnly to modeItems in mode.go", maxModeNavigations)
		return
	}
	if !s.Done() {
		t.Error("Done() = false after selecting ModeStandaloneOnly; want true")
	}
	if s.SelectedMode() != domain.ModeStandaloneOnly {
		t.Errorf("SelectedMode() = %q; want %q",
			s.SelectedMode(), domain.ModeStandaloneOnly)
	}
}

// TestModeScreen_StandaloneOnly_SelectedModeReturnsCorrectConstant verifies that
// SelectedMode() returns exactly domain.ModeStandaloneOnly when the standalone entry is
// selected. The value must match the domain constant exactly so both frontends produce the
// same RunMode string (AC6.3).
func TestModeScreen_StandaloneOnly_SelectedModeReturnsCorrectConstant(t *testing.T) {
	s := newModeScreen()

	_, found := navigateToMode(s, domain.ModeStandaloneOnly, maxModeNavigations)
	if !found {
		t.Fatalf("ModeStandaloneOnly entry not found in the mode list after %d Down presses; "+
			"add it to modeItems in mode.go", maxModeNavigations)
	}

	got := s.SelectedMode()
	want := domain.ModeStandaloneOnly
	if got != want {
		t.Errorf("SelectedMode() = %q; want %q — value must match the domain constant exactly, "+
			"not just be a similar string", got, want)
	}
}

// TestModeScreen_StandaloneOnly_DoesNotAffectOtherModes verifies that all pre-existing mode
// entries remain reachable after adding ModeStandaloneOnly. Inserting the new item must not
// remove or displace any existing mode.
func TestModeScreen_StandaloneOnly_DoesNotAffectOtherModes(t *testing.T) {
	knownModes := []domain.RunMode{
		domain.ModeDeployNew,
		domain.ModeUpdate,
		domain.ModeWorkflowsOnly,
		domain.ModePromote,
		domain.ModeTransformHarness,
		domain.ModeUtilityInfraOnly,
	}
	for _, want := range knownModes {
		pos := positionOf(newModeScreen(), want, maxModeNavigations)
		if pos < 0 {
			t.Errorf("mode %q is no longer reachable in the mode list; "+
				"adding ModeStandaloneOnly to modeItems must not remove or displace any existing mode",
				want)
		}
	}
}

// TestModeScreen_SelectStandalone_ThenReset_ClearsDone verifies that after selecting
// ModeStandaloneOnly, calling Reset() clears Done(), consistent with the Reset contract of
// all other mode entries.
func TestModeScreen_SelectStandalone_ThenReset_ClearsDone(t *testing.T) {
	s := newModeScreen()

	_, found := navigateToMode(s, domain.ModeStandaloneOnly, maxModeNavigations)
	if !found {
		t.Fatalf("ModeStandaloneOnly not found in list after %d Down presses", maxModeNavigations)
	}
	if !s.Done() {
		t.Fatal("precondition: Done() must be true before Reset")
	}

	s.Reset()

	if s.Done() {
		t.Error("Done() = true after Reset; want false (Reset must clear Done after standalone selection)")
	}
}
