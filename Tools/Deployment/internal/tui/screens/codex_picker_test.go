package screens_test

// codex_picker_test.go verifies that the TUI harness-selection screen lists and
// selects Codex when it is present in the registry-derived list.
//
// T12.3 -- TUI picker accepts Codex:
//   - The HarnessScreen built from a list that includes a Codex HarnessRef displays
//     Codex in its rendered view.
//   - Codex is selectable: pressing Enter with Codex highlighted sets Done() == true
//     and SelectedID() == "codex".
//
// Evidence of the "generic already" verdict for the TUI surface (I12.5 AC12.10):
//   NewHarnessScreen accepts []domain.HarnessRef and builds the list from that slice;
//   there is no internal fixture or hardcoded harness list. Codex appears in the picker
//   whenever the caller supplies it in the slice, which happens in production when the
//   harness registry (populated by the blank import in main.go) returns it from List().

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// codexHarness is a usable Codex HarnessRef for use in picker tests.
var codexHarness = domain.HarnessRef{
	ID:          "codex",
	DisplayName: "Codex",
	Tier:        domain.TierBuiltin,
	Usable:      true,
}

// ---------------------------------------------------------------------------
// T12.3: TUI harness picker lists and selects Codex
// ---------------------------------------------------------------------------

// TestHarnessScreen_WithCodex_DisplaysCodexInView verifies that when Codex is
// present in the harness list passed to NewHarnessScreen, its display name appears
// in the rendered view. The screen builds its list from the slice it receives; no
// hardcoded harness list exists inside HarnessScreen.
func TestHarnessScreen_WithCodex_DisplaysCodexInView(t *testing.T) {
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{codexHarness},
		80, 24, plainStyles(),
	)

	view := s.View()

	if !strings.Contains(view, "Codex") {
		t.Errorf("HarnessScreen view does not contain %q; Codex must appear in the picker when supplied in the harness list\nview:\n%s",
			"Codex", view)
	}
}

// TestHarnessScreen_WithCodex_SelectionReturnsCodxID verifies that pressing Enter
// when Codex is the highlighted item sets Done() == true and returns "codex" from
// SelectedID(). This proves Codex is selectable through the TUI picker.
func TestHarnessScreen_WithCodex_SelectionReturnsCodxID(t *testing.T) {
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{codexHarness},
		80, 24, plainStyles(),
	)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() == false after Enter on Codex harness; a usable harness must be selectable")
	}
	if id := s.SelectedID(); id != "codex" {
		t.Errorf("SelectedID() = %q, want %q; the selected ID must match the harness ID in the list",
			id, "codex")
	}
}

// TestHarnessScreen_CodexAmongOthers_CodexIsSelectable verifies that Codex can be
// navigated to and selected when the list contains multiple harnesses. The user navigates
// down to Codex and presses Enter; the screen must return its ID.
func TestHarnessScreen_CodexAmongOthers_CodexIsSelectable(t *testing.T) {
	// Place Codex second so navigation is required to reach it.
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{builtinHarness, codexHarness},
		80, 24, plainStyles(),
	)

	// Navigate down to the second item (Codex).
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	// Select it.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() == false after navigating to and selecting Codex; Codex must be selectable")
	}
	if id := s.SelectedID(); id != "codex" {
		t.Errorf("SelectedID() = %q, want %q; navigating to Codex and pressing Enter must select it",
			id, "codex")
	}
}

// TestHarnessScreen_CodexFromRegistryList_BuildsFromSlice verifies that HarnessScreen
// is constructed from the caller-supplied slice rather than from an internal fixture.
// This is the "generic already" property: the screen does not own a harness list.
// If the screen maintained its own list, supplying only Codex would produce a view
// that also shows the other built-ins; this test confirms it shows only what the
// caller supplies.
func TestHarnessScreen_CodexFromRegistryList_BuildsFromSlice(t *testing.T) {
	// Supply only Codex; no other harnesses.
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{codexHarness},
		80, 24, plainStyles(),
	)

	view := s.View()

	// Codex must be present.
	if !strings.Contains(view, "Codex") {
		t.Errorf("view does not contain %q; Codex must appear when supplied in the list", "Codex")
	}
	// Other built-ins must not appear (screen built from the supplied slice only).
	if strings.Contains(view, "Claude Code") {
		t.Errorf("view contains %q but only Codex was supplied; the screen must not inject its own harness list",
			"Claude Code")
	}
}
