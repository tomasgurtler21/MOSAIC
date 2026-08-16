package screens_test

// deployagents_test.go verifies the DeployAgentScreen:
//   - The screen renders every category and agent it is given, using only the group names
//     supplied to it — no catalog category name is hardcoded in the screen.
//   - Agents with a named category are rendered under a visible heading.
//   - Agents with an empty-name category are rendered under "(uncategorized)" and always last.
//   - Multi-select interaction: Space toggles, Enter confirms, Esc goes back.
//   - Heading rows are never included in SelectedIDs().
//   - The empty state renders a message rather than a blank screen; only Esc works.
//   - The error state renders the error message; only Esc works.
//   - Reset() clears Done and Back.
//
// All tests are RED-phase TDD tests. They compile but fail until DeployAgentScreen is
// implemented in screens/deployagents.go.
//
// Synthetic category names ("SynthGroup-Alpha", "SynthGroup-Beta") are used throughout.
// A screen that passes these tests must render whatever group names it is given, proving
// that no live catalog category name is hardcoded (per AC5.9).

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Fixtures — synthetic category names prove no hardcoding
// ---------------------------------------------------------------------------

// deployAgent builds an agent suitable for DeployAgentScreen tests.
func deployAgent(key, name, category string, role domain.AgentRole) domain.Agent {
	return domain.Agent{
		Key:      key,
		Name:     name,
		Category: category,
		Role:     role,
	}
}

var (
	// deployWithSynthCategories has two named categories with invented names.
	// The names "SynthGroup-Alpha" and "SynthGroup-Beta" do not appear anywhere in the
	// catalog, proving that the screen renders whatever it is given.
	deployWithSynthCategories = []screens.AgentCategory{
		{
			Name: "SynthGroup-Alpha",
			Agents: []domain.Agent{
				deployAgent("alpha-one", "Alpha One", "SynthGroup-Alpha", domain.RoleSubagent),
				deployAgent("alpha-two", "Alpha Two", "SynthGroup-Alpha", domain.RoleSubagent),
			},
		},
		{
			Name: "SynthGroup-Beta",
			Agents: []domain.Agent{
				deployAgent("beta-one", "Beta One", "SynthGroup-Beta", domain.RoleUtility),
			},
		},
	}

	// deployWithUncategorised has only uncategorised agents (empty Name category).
	deployWithUncategorised = []screens.AgentCategory{
		{
			Name: "",
			Agents: []domain.Agent{
				deployAgent("plain-agent", "Plain Agent", "", domain.RoleStandalone),
				deployAgent("other-agent", "Other Agent", "", domain.RoleSubagent),
			},
		},
	}

	// deployMixed has one named category and one empty-name category.
	deployMixed = []screens.AgentCategory{
		{
			Name: "SynthGroup-Alpha",
			Agents: []domain.Agent{
				deployAgent("alpha-one", "Alpha One", "SynthGroup-Alpha", domain.RoleSubagent),
			},
		},
		{
			Name: "",
			Agents: []domain.Agent{
				deployAgent("plain-agent", "Plain Agent", "", domain.RoleStandalone),
			},
		},
	}
)

func newDAScreen(categories []screens.AgentCategory) *screens.DeployAgentScreen {
	return screens.NewDeployAgentScreen(categories, 80, 24, plainStyles(), "")
}

// ---------------------------------------------------------------------------
// Group names rendered from input — no hardcoded names (AC5.9)
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_SynthGroupName_IsVisibleInView verifies that the screen renders
// the invented category heading "SynthGroup-Alpha" exactly as supplied. A screen that
// hardcodes real catalog category names would fail to show this invented name.
func TestDeployAgentScreen_SynthGroupName_IsVisibleInView(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)

	view := s.View()

	if !strings.Contains(view, "SynthGroup-Alpha") {
		t.Errorf("view does not contain invented category heading 'SynthGroup-Alpha';\n"+
			"the screen must render whatever category names it is given — no hardcoding allowed.\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_SecondSynthGroupName_IsVisibleInView verifies that the second
// invented category name "SynthGroup-Beta" is also rendered. The screen must handle
// multiple groups independently of their names.
func TestDeployAgentScreen_SecondSynthGroupName_IsVisibleInView(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)

	view := s.View()

	if !strings.Contains(view, "SynthGroup-Beta") {
		t.Errorf("view does not contain second invented category heading 'SynthGroup-Beta';\n"+
			"the screen must render all supplied category names.\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_AgentLabels_AllVisibleAcrossGroups verifies that agent names from
// multiple groups all appear in the rendered view.
func TestDeployAgentScreen_AgentLabels_AllVisibleAcrossGroups(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)

	view := s.View()

	wantLabels := []string{"Alpha One", "Alpha Two", "Beta One"}
	for _, label := range wantLabels {
		if !strings.Contains(view, label) {
			t.Errorf("view does not contain agent label %q; all agent names must be visible.\nView:\n%s",
				label, view)
		}
	}
}

// ---------------------------------------------------------------------------
// Uncategorised agents
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_UncategorisedAgents_ShowsUncategorizedHeading verifies that
// agents in an empty-Name category are rendered under "(uncategorized)".
func TestDeployAgentScreen_UncategorisedAgents_ShowsUncategorizedHeading(t *testing.T) {
	s := newDAScreen(deployWithUncategorised)

	view := s.View()

	if !strings.Contains(strings.ToLower(view), "uncategorized") {
		t.Errorf("view does not contain 'uncategorized' heading for empty-Name category;\n"+
			"agents with no category must render under '(uncategorized)'.\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_UncategorisedAgents_AgentLabelsVisible verifies that agent names
// in the empty-Name category appear in the rendered view.
func TestDeployAgentScreen_UncategorisedAgents_AgentLabelsVisible(t *testing.T) {
	s := newDAScreen(deployWithUncategorised)

	view := s.View()

	if !strings.Contains(view, "Plain Agent") {
		t.Errorf("view does not contain 'Plain Agent'; uncategorised agent names must be visible.\nView:\n%s", view)
	}
	if !strings.Contains(view, "Other Agent") {
		t.Errorf("view does not contain 'Other Agent'; uncategorised agent names must be visible.\nView:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Mixed groups — ordering contract
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_MixedGroups_NamedGroupBeforeUncategorised verifies that the named
// category heading appears before the "(uncategorized)" heading in the rendered view.
func TestDeployAgentScreen_MixedGroups_NamedGroupBeforeUncategorised(t *testing.T) {
	s := newDAScreen(deployMixed)

	view := s.View()

	namedIdx := strings.Index(view, "SynthGroup-Alpha")
	uncatIdx := strings.Index(strings.ToLower(view), "uncategorized")

	if namedIdx < 0 {
		t.Fatalf("view does not contain named category 'SynthGroup-Alpha':\n%s", view)
	}
	if uncatIdx < 0 {
		t.Fatalf("view does not contain 'uncategorized' heading:\n%s", view)
	}
	if uncatIdx < namedIdx {
		t.Errorf("'(uncategorized)' appears before named category 'SynthGroup-Alpha';\n"+
			"the uncategorised group must always render last.\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_MixedGroups_AllAgentsVisible verifies that every agent from both
// the named and uncategorised groups appears in the view.
func TestDeployAgentScreen_MixedGroups_AllAgentsVisible(t *testing.T) {
	s := newDAScreen(deployMixed)

	view := s.View()

	if !strings.Contains(view, "Alpha One") {
		t.Errorf("view does not contain categorised agent 'Alpha One':\n%s", view)
	}
	if !strings.Contains(view, "Plain Agent") {
		t.Errorf("view does not contain uncategorised agent 'Plain Agent':\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Heading rows not included in SelectedIDs
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_HeadingRows_NotInSelectedIDs verifies that category heading rows
// never appear in SelectedIDs(). This is critical: headings are non-selectable decorators.
func TestDeployAgentScreen_HeadingRows_NotInSelectedIDs(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)

	// Select several rows by pressing Space many times; also navigate down.
	for i := 0; i < 12; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	ids := s.SelectedIDs()
	if len(ids) == 0 {
		t.Fatal("SelectedIDs() returned empty after pressing Space 12 times;\n" +
			"the implementation must return selected agent keys so the heading-row check below executes.\n" +
			"A stub that returns nil passes vacuously and provides no RED-phase signal.")
	}
	for _, id := range ids {
		if id == "SynthGroup-Alpha" || id == "SynthGroup-Beta" || id == "" {
			t.Errorf("SelectedIDs() contains %q which looks like a category heading, not an agent key;\n"+
				"heading rows must never be returned by SelectedIDs()", id)
		}
	}
}

// TestDeployAgentScreen_SelectedIDs_ContainsAgentKeysOnly verifies that SelectedIDs returns
// agent keys (not category names or empty strings) after a selection.
func TestDeployAgentScreen_SelectedIDs_ContainsAgentKeysOnly(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Fatal("precondition: Done() must be true after Enter")
	}
	for _, id := range s.SelectedIDs() {
		if id == "" {
			t.Errorf("SelectedIDs() contains empty string; want agent keys only")
		}
		if strings.HasPrefix(id, "SynthGroup") {
			t.Errorf("SelectedIDs() contains category name %q; want agent keys only", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Multi-select interaction
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_SpaceAndEnter_ConfirmsSelection verifies that Space toggles an agent
// and Enter confirms the selection, setting Done()=true.
func TestDeployAgentScreen_SpaceAndEnter_ConfirmsSelection(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Space+Enter; want true (agents must be selectable by Space+Enter)")
	}
	ids := s.SelectedIDs()
	if len(ids) == 0 {
		t.Error("SelectedIDs() is empty after toggling an agent; want at least one selected ID")
	}
}

// TestDeployAgentScreen_MultipleAgentsAcrossGroups_Selectable verifies that agents from
// different groups can all be selected in one session.
func TestDeployAgentScreen_MultipleAgentsAcrossGroups_Selectable(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)

	// Select first agent (under SynthGroup-Alpha heading), navigate down multiple times,
	// select another, then confirm.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select first agent
	s.Update(tea.KeyMsg{Type: tea.KeyDown})                      // navigate
	s.Update(tea.KeyMsg{Type: tea.KeyDown})                      // navigate
	s.Update(tea.KeyMsg{Type: tea.KeyDown})                      // navigate
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select another
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})                     // confirm

	if !s.Done() {
		t.Error("Done() = false after multi-group selection; want true")
	}
}

// ---------------------------------------------------------------------------
// Navigation: Enter and Esc
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_Enter_SetsDone verifies that Enter confirms the selection.
func TestDeployAgentScreen_Enter_SetsDone(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter; want true")
	}
}

// TestDeployAgentScreen_Esc_SetsBack verifies that Esc goes back without confirming.
func TestDeployAgentScreen_Esc_SetsBack(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc; want true")
	}
	if s.Done() {
		t.Error("Done() = true after Esc; Esc must not confirm the selection")
	}
}

// TestDeployAgentScreen_Esc_DoesNotDeliverPartialSelection verifies that Esc after
// selecting some agents does not set Done().
func TestDeployAgentScreen_Esc_DoesNotDeliverPartialSelection(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if s.Done() {
		t.Error("Done() = true after partial selection + Esc; Esc must not confirm")
	}
	if !s.Back() {
		t.Error("Back() = false after Esc; want true")
	}
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_EmptyState_RendersMessage verifies that nil categories renders
// an informational message rather than a blank screen.
func TestDeployAgentScreen_EmptyState_RendersMessage(t *testing.T) {
	s := screens.NewDeployAgentScreen(nil, 80, 24, plainStyles(), "")

	view := s.View()

	if strings.TrimSpace(view) == "" {
		t.Error("empty-state view must not be blank; want an informational message")
	}
}

// TestDeployAgentScreen_EmptyState_EscSetsBack verifies that Esc works in the empty state.
func TestDeployAgentScreen_EmptyState_EscSetsBack(t *testing.T) {
	s := screens.NewDeployAgentScreen(nil, 80, 24, plainStyles(), "")

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc in empty state; want true")
	}
}

// ---------------------------------------------------------------------------
// Error state
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_ErrorState_RendersMessage verifies that a non-empty errorMsg
// is displayed in the view.
func TestDeployAgentScreen_ErrorState_RendersMessage(t *testing.T) {
	const errMsg = "failed to load agents: connection refused"
	s := screens.NewDeployAgentScreen(deployWithSynthCategories, 80, 24, plainStyles(), errMsg)

	view := s.View()

	if !strings.Contains(view, "failed to load agents: connection refused") {
		t.Errorf("error-state view does not contain the error message; got:\n%s", view)
	}
}

// TestDeployAgentScreen_ErrorState_EscSetsBack verifies that Esc sets Back() in the
// error state.
func TestDeployAgentScreen_ErrorState_EscSetsBack(t *testing.T) {
	s := screens.NewDeployAgentScreen(deployWithSynthCategories, 80, 24, plainStyles(), "error")

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc in error state; want true")
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_Reset_ClearsDoneAndBack verifies that Reset restores idle state.
func TestDeployAgentScreen_Reset_ClearsDoneAndBack(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)
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
// Keyboard-only full flow
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_KeyboardOnly_FullSelectionFlow verifies that a complete
// multi-group agent selection requires only keyboard input.
func TestDeployAgentScreen_KeyboardOnly_FullSelectionFlow(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // toggle
	s.Update(tea.KeyMsg{Type: tea.KeyDown})                      // move down
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // toggle another
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})                     // confirm

	if !s.Done() {
		t.Error("Done() = false after keyboard-only selection flow; want true")
	}
	ids := s.SelectedIDs()
	if len(ids) == 0 {
		t.Error("SelectedIDs() is empty after keyboard-only selection; want selected agents")
	}
}

// ---------------------------------------------------------------------------
// Resize does not panic
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_Resize_DoesNotPanic verifies that calling Resize on an active
// screen does not panic.
func TestDeployAgentScreen_Resize_DoesNotPanic(t *testing.T) {
	s := newDAScreen(deployWithSynthCategories)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Resize panicked: %v", r)
		}
	}()

	s.Resize(120, 40)
	_ = s.View()
}
