package screens_test

// standaloneagents_test.go verifies the StandaloneAgentScreen:
//   - Agents with a category are rendered under a category heading.
//   - Agents without a category are rendered under "(uncategorized)" and always last.
//   - A mix of categorised and uncategorised agents displays coherently in one screen.
//   - The empty state renders a message rather than a blank screen.
//   - The error state renders the error message.
//   - Multi-select interaction: Space toggles, Enter confirms, Esc goes back.
//   - Heading rows are never included in SelectedIDs().
//   - Reset() clears Done and Back.
//
// All tests are RED-phase TDD tests. They fail until StandaloneAgentScreen is implemented.

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

// categorisedAgent returns an agent that carries the given category.
func categorisedAgent(key, name, category string) domain.Agent {
	return domain.Agent{
		Key:      key,
		Name:     name,
		Category: category,
		Role:     domain.RoleStandalone,
	}
}

// uncategorisedAgent returns an agent with an empty category.
func uncategorisedAgent(key, name string) domain.Agent {
	return domain.Agent{
		Key:  key,
		Name: name,
		Role: domain.RoleStandalone,
	}
}

var (
	// standaloneWithCategory has one named category.
	standaloneWithCategory = []screens.AgentCategory{
		{
			Name: "AI",
			Agents: []domain.Agent{
				categorisedAgent("openai-adapter", "OpenAI Adapter", "AI"),
				categorisedAgent("claude-adapter", "Claude Adapter", "AI"),
			},
		},
	}

	// standaloneWithoutCategory has a single empty-name category (uncategorised agents).
	standaloneWithoutCategory = []screens.AgentCategory{
		{
			Name: "",
			Agents: []domain.Agent{
				uncategorisedAgent("generic-agent", "Generic Agent"),
				uncategorisedAgent("plain-agent", "Plain Agent"),
			},
		},
	}

	// standaloneMixed has one named category and one empty-name category.
	standaloneMixed = []screens.AgentCategory{
		{
			Name: "AI",
			Agents: []domain.Agent{
				categorisedAgent("openai-adapter", "OpenAI Adapter", "AI"),
			},
		},
		{
			Name: "",
			Agents: []domain.Agent{
				uncategorisedAgent("generic-agent", "Generic Agent"),
			},
		},
	}
)

func newSAScreen(categories []screens.AgentCategory) *screens.StandaloneAgentScreen {
	return screens.NewStandaloneAgentScreen(categories, 80, 24, plainStyles(), "")
}

// ---------------------------------------------------------------------------
// Categorised agents (AC6.4)
// ---------------------------------------------------------------------------

// TestStandaloneAgentScreen_CategorisedAgents_CategoryHeadingIsVisible verifies that
// the category name appears in the rendered view as a heading when agents carry a category.
// AC6.4: categorised standalone agents must render and be selectable.
func TestStandaloneAgentScreen_CategorisedAgents_CategoryHeadingIsVisible(t *testing.T) {
	s := newSAScreen(standaloneWithCategory)

	view := s.View()

	if !strings.Contains(view, "AI") {
		t.Errorf("view does not contain the category heading 'AI';\n"+
			"categorised agents must render with a visible category heading.\nView:\n%s", view)
	}
}

// TestStandaloneAgentScreen_CategorisedAgents_AgentLabelsAreVisible verifies that agent
// names (or keys) appear in the rendered view for categorised agents.
func TestStandaloneAgentScreen_CategorisedAgents_AgentLabelsAreVisible(t *testing.T) {
	s := newSAScreen(standaloneWithCategory)

	view := s.View()

	if !strings.Contains(view, "OpenAI Adapter") {
		t.Errorf("view does not contain 'OpenAI Adapter'; want all categorised agent names visible.\n"+
			"View:\n%s", view)
	}
	if !strings.Contains(view, "Claude Adapter") {
		t.Errorf("view does not contain 'Claude Adapter'; want all categorised agent names visible.\n"+
			"View:\n%s", view)
	}
}

// TestStandaloneAgentScreen_CategorisedAgents_SelectableBySpaceAndEnter verifies that
// categorised agents can be selected by pressing Space and confirmed with Enter.
// AC6.4: categorised standalone agents must be selectable.
func TestStandaloneAgentScreen_CategorisedAgents_SelectableBySpaceAndEnter(t *testing.T) {
	s := newSAScreen(standaloneWithCategory)

	// The first selectable row (index 0 in the list) should be the first agent after the heading.
	// Pressing Space selects it; Enter confirms.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // toggle first agent
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Space+Enter on a categorised agent; want true (agents must be selectable)")
	}
	ids := s.SelectedIDs()
	if len(ids) == 0 {
		t.Error("SelectedIDs() is empty after toggling a categorised agent; want at least one selected ID")
	}
}

// ---------------------------------------------------------------------------
// Uncategorised agents (AC6.5)
// ---------------------------------------------------------------------------

// TestStandaloneAgentScreen_UncategorisedAgents_ShowsUncategorizedHeading verifies that
// agents with an empty category are rendered under the "(uncategorized)" heading.
// AC6.5: uncategorised agents must render without being dropped or mis-rendered.
func TestStandaloneAgentScreen_UncategorisedAgents_ShowsUncategorizedHeading(t *testing.T) {
	s := newSAScreen(standaloneWithoutCategory)

	view := s.View()

	if !strings.Contains(view, "uncategorized") {
		t.Errorf("view does not contain 'uncategorized' for agents with no category;\n"+
			"agents with an empty category must render under a '(uncategorized)' heading.\nView:\n%s", view)
	}
}

// TestStandaloneAgentScreen_UncategorisedAgents_AgentLabelsAreVisible verifies that agent
// names (or keys) appear in the rendered view for uncategorised agents.
func TestStandaloneAgentScreen_UncategorisedAgents_AgentLabelsAreVisible(t *testing.T) {
	s := newSAScreen(standaloneWithoutCategory)

	view := s.View()

	if !strings.Contains(view, "Generic Agent") {
		t.Errorf("view does not contain 'Generic Agent'; want all uncategorised agent names visible.\n"+
			"View:\n%s", view)
	}
	if !strings.Contains(view, "Plain Agent") {
		t.Errorf("view does not contain 'Plain Agent'; want all uncategorised agent names visible.\n"+
			"View:\n%s", view)
	}
}

// TestStandaloneAgentScreen_UncategorisedAgents_SelectableBySpaceAndEnter verifies that
// uncategorised agents can be selected and confirmed.
// AC6.5: uncategorised standalone agents must be selectable.
func TestStandaloneAgentScreen_UncategorisedAgents_SelectableBySpaceAndEnter(t *testing.T) {
	s := newSAScreen(standaloneWithoutCategory)

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Space+Enter on an uncategorised agent; want true")
	}
	ids := s.SelectedIDs()
	if len(ids) == 0 {
		t.Error("SelectedIDs() is empty after toggling an uncategorised agent; want at least one selected ID")
	}
}

// TestStandaloneAgentScreen_UncategorisedAgents_OnlyUncategorizedHeadingShown verifies
// that when every agent is uncategorised, exactly one "(uncategorized)" heading appears —
// no spurious empty heading rows and no mis-rendering.
func TestStandaloneAgentScreen_UncategorisedAgents_OnlyUncategorizedHeadingShown(t *testing.T) {
	s := newSAScreen(standaloneWithoutCategory)

	view := s.View()
	lower := strings.ToLower(view)

	// There should be an "(uncategorized)" heading.
	if !strings.Contains(lower, "uncategorized") {
		t.Errorf("view lacks '(uncategorized)' heading when all agents have no category:\n%s", view)
	}
	// There must be no named category heading (i.e., "AI" must not appear).
	if strings.Contains(view, "AI") {
		t.Errorf("view contains spurious 'AI' category heading; only '(uncategorized)' should appear:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Mixed agents (AC6.6)
// ---------------------------------------------------------------------------

// TestStandaloneAgentScreen_MixedAgents_BothCategoryAndUncategorizedVisible verifies that
// a mixed set of categorised and uncategorised agents displays coherently in one screen.
// AC6.6: a mixed set must display coherently.
func TestStandaloneAgentScreen_MixedAgents_BothCategoryAndUncategorizedVisible(t *testing.T) {
	s := newSAScreen(standaloneMixed)

	view := s.View()

	if !strings.Contains(view, "AI") {
		t.Errorf("view does not contain named category 'AI' in mixed set:\n%s", view)
	}
	if !strings.Contains(strings.ToLower(view), "uncategorized") {
		t.Errorf("view does not contain 'uncategorized' heading in mixed set:\n%s", view)
	}
}

// TestStandaloneAgentScreen_MixedAgents_AllAgentLabelsVisible verifies that every agent
// from both the named and uncategorised groups appears in the view.
func TestStandaloneAgentScreen_MixedAgents_AllAgentLabelsVisible(t *testing.T) {
	s := newSAScreen(standaloneMixed)

	view := s.View()

	if !strings.Contains(view, "OpenAI Adapter") {
		t.Errorf("view does not contain categorised agent 'OpenAI Adapter' in mixed set:\n%s", view)
	}
	if !strings.Contains(view, "Generic Agent") {
		t.Errorf("view does not contain uncategorised agent 'Generic Agent' in mixed set:\n%s", view)
	}
}

// TestStandaloneAgentScreen_MixedAgents_UncategorisedGroupAppearsLast verifies that the
// "(uncategorized)" heading appears after any named category heading in the rendered view.
// The rendering contract places uncategorised agents always last.
func TestStandaloneAgentScreen_MixedAgents_UncategorisedGroupAppearsLast(t *testing.T) {
	s := newSAScreen(standaloneMixed)

	view := s.View()

	aiIdx := strings.Index(view, "AI")
	uncatIdx := strings.Index(strings.ToLower(view), "uncategorized")

	if aiIdx < 0 {
		t.Fatalf("view does not contain 'AI' heading:\n%s", view)
	}
	if uncatIdx < 0 {
		t.Fatalf("view does not contain 'uncategorized' heading:\n%s", view)
	}
	if uncatIdx < aiIdx {
		t.Errorf("'(uncategorized)' heading appears before the 'AI' named-category heading;\n"+
			"uncategorised agents must always be rendered last.\nView:\n%s", view)
	}
}

// TestStandaloneAgentScreen_MixedAgents_AllAgentsSelectableByKeyboard verifies that agents
// from both groups can be selected using keyboard navigation.
func TestStandaloneAgentScreen_MixedAgents_AllAgentsSelectableByKeyboard(t *testing.T) {
	s := newSAScreen(standaloneMixed)

	// Navigate to the first selectable row (first agent under "AI"), toggle it.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after selecting in a mixed-agent screen; want true")
	}
}

// ---------------------------------------------------------------------------
// Heading rows not selectable
// ---------------------------------------------------------------------------

// TestStandaloneAgentScreen_HeadingRows_NotInSelectedIDs verifies that category heading
// rows never appear in SelectedIDs(). Heading rows are non-selectable by contract.
func TestStandaloneAgentScreen_HeadingRows_NotInSelectedIDs(t *testing.T) {
	s := newSAScreen(standaloneWithCategory)

	// Select all reachable rows by pressing Space many times (including while on any row).
	for i := 0; i < 10; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	ids := s.SelectedIDs()
	// Precondition: at least one ID must have been selected. If SelectedIDs returns nil or
	// empty the for-loop below never executes, making this test vacuous in RED phase (the
	// stub returns nil, so the loop body is unreachable and no assertion fires). This guard
	// forces the test to fail in RED until the implementation returns real selected keys.
	if len(ids) == 0 {
		t.Fatal("SelectedIDs() returned an empty slice after pressing Space 10 times on an agent screen;\n" +
			"the implementation must return selected agent keys so the heading-row check below can\n" +
			"actually execute. A stub that returns nil passes vacuously and provides no RED-phase signal.")
	}
	for _, id := range ids {
		// Category names must not appear as selected IDs.
		if id == "AI" || id == "" {
			t.Errorf("SelectedIDs() contains %q which looks like a category heading, not an agent key;\n"+
				"heading rows must never be returned by SelectedIDs()", id)
		}
	}
}

// TestStandaloneAgentScreen_SelectedIDs_ContainsAgentKeysOnly verifies that SelectedIDs()
// returns agent keys (not category names or empty strings) after selection.
func TestStandaloneAgentScreen_SelectedIDs_ContainsAgentKeysOnly(t *testing.T) {
	s := newSAScreen(standaloneWithCategory)

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // toggle first agent
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	ids := s.SelectedIDs()
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after Enter")
	}
	for _, id := range ids {
		if id == "" {
			t.Errorf("SelectedIDs() contains empty string; want agent keys only")
		}
		if id == "AI" {
			t.Errorf("SelectedIDs() contains category name 'AI'; want agent keys only")
		}
	}
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

// TestStandaloneAgentScreen_EmptyState_RendersMessage verifies that an empty category
// list renders an informational message rather than a blank or panicking view.
func TestStandaloneAgentScreen_EmptyState_RendersMessage(t *testing.T) {
	s := screens.NewStandaloneAgentScreen(nil, 80, 24, plainStyles(), "")

	view := s.View()

	if strings.TrimSpace(view) == "" {
		t.Error("empty-state view must not be blank; want an informational message")
	}
}

// TestStandaloneAgentScreen_EmptyState_EscSetsBack verifies that Esc works in the empty state.
func TestStandaloneAgentScreen_EmptyState_EscSetsBack(t *testing.T) {
	s := screens.NewStandaloneAgentScreen(nil, 80, 24, plainStyles(), "")

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc in empty state; want true")
	}
}

// ---------------------------------------------------------------------------
// Error state
// ---------------------------------------------------------------------------

// TestStandaloneAgentScreen_ErrorState_RendersMessage verifies that a non-empty errorMsg
// is displayed prominently.
func TestStandaloneAgentScreen_ErrorState_RendersMessage(t *testing.T) {
	const errMsg = "failed to load standalone agents: permission denied"
	s := screens.NewStandaloneAgentScreen(standaloneWithCategory, 80, 24, plainStyles(), errMsg)

	view := s.View()

	if !strings.Contains(view, "failed to load standalone agents: permission denied") {
		t.Errorf("error-state view must contain the error message; got:\n%s", view)
	}
}

// TestStandaloneAgentScreen_ErrorState_EscSetsBack verifies that Esc sets Back() in the
// error state, consistent with UtilityAgentScreen's error state behaviour.
func TestStandaloneAgentScreen_ErrorState_EscSetsBack(t *testing.T) {
	s := screens.NewStandaloneAgentScreen(standaloneWithCategory, 80, 24, plainStyles(), "error")

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc in error state; want true")
	}
}

// ---------------------------------------------------------------------------
// Navigation: Enter and Esc
// ---------------------------------------------------------------------------

// TestStandaloneAgentScreen_Enter_SetsDone verifies that pressing Enter confirms the
// selection and sets Done() = true.
func TestStandaloneAgentScreen_Enter_SetsDone(t *testing.T) {
	s := newSAScreen(standaloneWithCategory)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter; want true (Enter must confirm the selection)")
	}
}

// TestStandaloneAgentScreen_Esc_SetsBack verifies that pressing Esc goes back without
// confirming the selection.
func TestStandaloneAgentScreen_Esc_SetsBack(t *testing.T) {
	s := newSAScreen(standaloneWithCategory)

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc; want true")
	}
	if s.Done() {
		t.Error("Done() = true after Esc; want false (Esc must not confirm)")
	}
}

// TestStandaloneAgentScreen_Esc_DoesNotDeliverPartialSelections verifies that pressing
// Esc after selecting some agents does not mark Done() true. Partial selections are
// discarded on back-navigation.
func TestStandaloneAgentScreen_Esc_DoesNotDeliverPartialSelections(t *testing.T) {
	s := newSAScreen(standaloneWithCategory)

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select first agent
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})                       // cancel

	if s.Done() {
		t.Error("Done() = true after Esc; Esc must not confirm a partial selection")
	}
	if !s.Back() {
		t.Error("Back() = false after Esc; want true")
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestStandaloneAgentScreen_Reset_ClearsDoneAndBack verifies that Reset restores the
// idle state after a confirmation or back-navigation.
func TestStandaloneAgentScreen_Reset_ClearsDoneAndBack(t *testing.T) {
	s := newSAScreen(standaloneWithCategory)
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
// Full keyboard-only flow
// ---------------------------------------------------------------------------

// TestStandaloneAgentScreen_KeyboardOnly_SelectionFlow verifies that a complete
// standalone-agent selection from start to finish requires only keyboard input.
func TestStandaloneAgentScreen_KeyboardOnly_SelectionFlow(t *testing.T) {
	s := newSAScreen(standaloneWithCategory)

	// Toggle first agent, navigate down, toggle second, confirm.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // toggle
	s.Update(tea.KeyMsg{Type: tea.KeyDown})                      // move down
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // toggle
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})                     // confirm

	if !s.Done() {
		t.Error("Done() = false after keyboard-only selection flow; want true")
	}
	ids := s.SelectedIDs()
	if len(ids) == 0 {
		t.Error("SelectedIDs() is empty after keyboard-only selection; want selected agents")
	}
}
