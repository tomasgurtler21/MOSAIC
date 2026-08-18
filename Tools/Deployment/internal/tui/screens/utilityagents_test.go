package screens_test

// utilityagents_test.go verifies the UtilityAgentScreen:
//   - Only the agents passed in are offered (allow-list filtering is the caller's concern, but
//     the test verifies the screen honours exactly the set it receives).
//   - Description is shown in the detail pane on focus.
//   - Multi-select: Space toggles, Enter confirms, Esc goes back.
//   - Empty state and error state are rendered instead of a blank screen.
//   - Keyboard-only operability.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

var utilityAgents = []domain.Agent{
	{Key: "orchestration-architect", Name: "Orchestration Architect", Description: "Designs orchestration workflows.", Role: domain.RoleUtility},
	{Key: "workflow-creator", Name: "Workflow Creator", Description: "Creates new workflows from templates.", Role: domain.RoleUtility},
	{Key: "harness-bug-hunter", Name: "Harness Bug Hunter", Description: "Finds issues in harness configurations.", Role: domain.RoleUtility},
}

func newUAScreen(agents []domain.Agent) *screens.UtilityAgentScreen {
	return screens.NewUtilityAgentScreen(agents, 80, 24, plainStyles(), "")
}

// ---------------------------------------------------------------------------
// Allow-list filtering (honoured at call site)
// ---------------------------------------------------------------------------

// TestUtilityAgentScreen_ShowsOnlyProvidedAgents verifies that the screen displays exactly
// the agents it receives. This indirectly tests that the caller (app layer) is expected to
// pass the filtered list, and the screen does not add or remove entries.
func TestUtilityAgentScreen_ShowsOnlyProvidedAgents(t *testing.T) {
	s := newUAScreen(utilityAgents[:2]) // only the first two

	view := s.View()

	if !strings.Contains(view, "Orchestration Architect") {
		t.Error("view must contain 'Orchestration Architect'")
	}
	if !strings.Contains(view, "Workflow Creator") {
		t.Error("view must contain 'Workflow Creator'")
	}
	if strings.Contains(view, "Harness Bug Hunter") {
		t.Error("view must NOT contain 'Harness Bug Hunter' (not in the provided slice)")
	}
}

// ---------------------------------------------------------------------------
// Selection
// ---------------------------------------------------------------------------

// TestUtilityAgentScreen_Space_TogglesSelection verifies that pressing Space toggles the
// focused agent's selection state, reflected in SelectedIDs().
func TestUtilityAgentScreen_Space_TogglesSelection(t *testing.T) {
	s := newUAScreen(utilityAgents)

	// Initially no selections.
	if len(s.SelectedIDs()) != 0 {
		t.Fatalf("SelectedIDs() = %v initially; want empty", s.SelectedIDs())
	}

	// Toggle the first agent.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	ids := s.SelectedIDs()
	if !containsStr(ids, "orchestration-architect") {
		t.Errorf("SelectedIDs() = %v after Space; want 'orchestration-architect'", ids)
	}
}

// TestUtilityAgentScreen_Enter_SetsDone verifies that pressing Enter confirms the selection.
func TestUtilityAgentScreen_Enter_SetsDone(t *testing.T) {
	s := newUAScreen(utilityAgents)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter; want true")
	}
}

// TestUtilityAgentScreen_Esc_SetsBack verifies that pressing Esc goes back without confirming.
func TestUtilityAgentScreen_Esc_SetsBack(t *testing.T) {
	s := newUAScreen(utilityAgents)

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc; want true")
	}
	if s.Done() {
		t.Error("Done() = true after Esc; want false")
	}
}

// TestUtilityAgentScreen_MultiSelect_AccumulatesSelections verifies that multiple agents
// can be selected by navigating and toggling each one.
func TestUtilityAgentScreen_MultiSelect_AccumulatesSelections(t *testing.T) {
	s := newUAScreen(utilityAgents)

	// Toggle first agent.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	// Move down and toggle second agent.
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	ids := s.SelectedIDs()
	if !containsStr(ids, "orchestration-architect") {
		t.Errorf("SelectedIDs() = %v; want 'orchestration-architect'", ids)
	}
	if !containsStr(ids, "workflow-creator") {
		t.Errorf("SelectedIDs() = %v; want 'workflow-creator'", ids)
	}
}

// ---------------------------------------------------------------------------
// Detail pane
// ---------------------------------------------------------------------------

// TestUtilityAgentScreen_DetailPane_ShowsDescription verifies that the detail pane shows the
// focused agent's description. The check uses a prefix of the description that fits on a
// single detail pane line, since the full sentence may be word-wrapped at the column boundary.
func TestUtilityAgentScreen_DetailPane_ShowsDescription(t *testing.T) {
	s := newUAScreen(utilityAgents)

	view := s.View()

	// "Designs orchestration" is the first 21 chars of the description — it fits on one
	// line of the detail pane and will not be split by word-wrap.
	if !strings.Contains(view, "Designs orchestration") {
		t.Errorf("detail pane must show the agent description; got:\n%s", view)
	}
}

// TestUtilityAgentScreen_DetailPane_UpdatesOnNavigation verifies that the detail pane updates
// as the cursor moves between agents.
func TestUtilityAgentScreen_DetailPane_UpdatesOnNavigation(t *testing.T) {
	s := newUAScreen(utilityAgents)

	view1 := s.View()
	if !strings.Contains(view1, "Designs orchestration") {
		t.Errorf("initial detail pane must show first agent's description; got:\n%s", view1)
	}

	s.Update(tea.KeyMsg{Type: tea.KeyDown})

	view2 := s.View()
	// "Creates new workflows" is unique to the second agent's description.
	if !strings.Contains(view2, "Creates new workflows") {
		t.Errorf("detail pane must update to second agent after navigating down; got:\n%s", view2)
	}
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

// TestUtilityAgentScreen_EmptyState_RendersMessage verifies that an empty agent list shows
// an informational message rather than a blank or panicking view.
func TestUtilityAgentScreen_EmptyState_RendersMessage(t *testing.T) {
	s := screens.NewUtilityAgentScreen(nil, 80, 24, plainStyles(), "")

	view := s.View()

	if strings.TrimSpace(view) == "" {
		t.Error("empty-state view must not be blank")
	}
	if !strings.Contains(strings.ToLower(view), "no utility agent") && !strings.Contains(strings.ToLower(view), "allow-list") {
		t.Errorf("empty-state view must contain an informational message about the allow-list; got:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Error state
// ---------------------------------------------------------------------------

// TestUtilityAgentScreen_ErrorState_RendersMessage verifies that an error message is
// displayed prominently when errorMsg is non-empty.
func TestUtilityAgentScreen_ErrorState_RendersMessage(t *testing.T) {
	s := screens.NewUtilityAgentScreen(utilityAgents, 80, 24, plainStyles(), "failed to load utility agents: permission denied")

	view := s.View()

	if !strings.Contains(view, "failed to load utility agents: permission denied") {
		t.Errorf("error-state view must contain the error message; got:\n%s", view)
	}
}

// TestUtilityAgentScreen_ErrorState_EscSetsBack verifies that Esc works in error state.
func TestUtilityAgentScreen_ErrorState_EscSetsBack(t *testing.T) {
	s := screens.NewUtilityAgentScreen(utilityAgents, 80, 24, plainStyles(), "error")
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc in error state; want true")
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestUtilityAgentScreen_Reset_ClearsDoneAndBack verifies that Reset restores idle state.
func TestUtilityAgentScreen_Reset_ClearsDoneAndBack(t *testing.T) {
	s := newUAScreen(utilityAgents)
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after Enter")
	}

	s.Reset()

	if s.Done() {
		t.Error("Done() = true after Reset; want false")
	}
	if s.Back() {
		t.Error("Back() = true after Reset; want false")
	}
}

// ---------------------------------------------------------------------------
// Keyboard-only operability
// ---------------------------------------------------------------------------

// TestUtilityAgentScreen_Deselect_RemovesFromSelectedIDs verifies that toggling an already-
// selected agent off removes it from SelectedIDs(). This guards against a state management
// bug where agents can only accumulate and never be removed.
func TestUtilityAgentScreen_Deselect_RemovesFromSelectedIDs(t *testing.T) {
	s := newUAScreen(utilityAgents)

	// Toggle on.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !containsStr(s.SelectedIDs(), "orchestration-architect") {
		t.Fatal("precondition: 'orchestration-architect' must be selected after first toggle")
	}

	// Toggle off.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	ids := s.SelectedIDs()
	if containsStr(ids, "orchestration-architect") {
		t.Errorf("SelectedIDs() = %v after toggling off; 'orchestration-architect' must be removed", ids)
	}
}

// TestUtilityAgentScreen_VimUp_NavigatesBackward verifies that the 'k' key (vim-style up)
// moves the cursor to the previous agent, symmetric with 'j' moving down.
func TestUtilityAgentScreen_VimUp_NavigatesBackward(t *testing.T) {
	s := newUAScreen(utilityAgents)

	// Move down to the second agent, then back up to the first using 'k'.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})

	// Toggle: if the cursor is back on the first agent, 'orchestration-architect' is selected.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	ids := s.SelectedIDs()
	if !containsStr(ids, "orchestration-architect") {
		t.Errorf("SelectedIDs() = %v after j+k+space; 'k' must move cursor back to first agent", ids)
	}
	if containsStr(ids, "workflow-creator") {
		t.Errorf("SelectedIDs() = %v; 'workflow-creator' must not be selected (cursor moved back to first agent)", ids)
	}
}

// ---------------------------------------------------------------------------
// Keyboard-only operability
// ---------------------------------------------------------------------------

// TestUtilityAgentScreen_KeyboardOnly_FullSelectionFlow verifies that a utility-agent
// selection from start to finish requires only keyboard input.
func TestUtilityAgentScreen_KeyboardOnly_FullSelectionFlow(t *testing.T) {
	s := newUAScreen(utilityAgents)

	// Navigate and select using keyboard.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})          // toggle first
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})          // vim-style down
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})          // toggle second
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})                               // confirm

	if !s.Done() {
		t.Error("Done() = false after keyboard-only selection flow; want true")
	}
	ids := s.SelectedIDs()
	if len(ids) != 2 {
		t.Errorf("SelectedIDs() = %v; want 2 agents selected", ids)
	}
}
