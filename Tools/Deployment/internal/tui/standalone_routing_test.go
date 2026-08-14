package tui

// standalone_routing_test.go verifies two related concerns for the standalone agent
// selection question (QStandaloneAgents):
//
//  1. Adapter: optionsToAgentCategories correctly reconstructs ordered agent categories
//     from the flat domain.Option slice produced by askStandaloneAgents, with empty-Group
//     options always placed last.
//
//  2. Routing: a QStandaloneAgents SelectMany question routes to the dedicated
//     StandaloneAgentScreen and not the generic inlineSelectOne fallback.
//
//  3. Round-trip: selecting standalone agents and confirming delivers the correct IDs
//     through the reply channel as a MultiChoiceAnswer.
//
// All tests are RED-phase TDD tests. They fail until:
//   - optionsToAgentCategories is implemented (currently returns nil).
//   - The QStandaloneAgents routing case is added to handleQuestionMsg.
//
// Tests are in the internal tui package so they can access unexported rootModel fields
// and drive key events through m.Update.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers: option fixtures for standalone agent scenarios
// ---------------------------------------------------------------------------

// standaloneAgentOptions returns a slice of options for standalone agents across two
// categories: one named ("AI") and one ungrouped (empty Group, placed last).
func standaloneAgentOptions() []domain.Option {
	return []domain.Option{
		{ID: "openai-adapter", Label: "OpenAI Adapter", Description: "Adapts OpenAI API", Group: "AI"},
		{ID: "claude-adapter", Label: "Claude Adapter", Description: "Adapts Claude API", Group: "AI"},
		{ID: "generic-agent", Label: "Generic Agent", Description: "No category", Group: ""},
	}
}

// namedCategoryOnlyOptions returns options that all carry the same named group.
func namedCategoryOnlyOptions() []domain.Option {
	return []domain.Option{
		{ID: "agent-a", Label: "Agent A", Description: "Desc A", Group: "Tools"},
		{ID: "agent-b", Label: "Agent B", Description: "Desc B", Group: "Tools"},
	}
}

// uncategorisedOnlyOptions returns options with no group.
func uncategorisedOnlyOptions() []domain.Option {
	return []domain.Option{
		{ID: "plain-a", Label: "Plain A", Description: "Desc A", Group: ""},
		{ID: "plain-b", Label: "Plain B", Description: "Desc B", Group: ""},
	}
}

// ---------------------------------------------------------------------------
// optionsToAgentCategories: empty input
// ---------------------------------------------------------------------------

// TestOptionsToAgentCategories_EmptyOptions_ReturnsEmpty verifies that a nil option
// slice produces no categories and does not panic.
func TestOptionsToAgentCategories_EmptyOptions_ReturnsEmpty(t *testing.T) {
	cats := optionsToAgentCategories(nil)

	if len(cats) != 0 {
		t.Errorf("optionsToAgentCategories(nil) returned %d categories; want 0", len(cats))
	}
}

// ---------------------------------------------------------------------------
// optionsToAgentCategories: named categories
// ---------------------------------------------------------------------------

// TestOptionsToAgentCategories_NamedCategoryOnly_ProducesOneCategory verifies that
// options sharing a Group value produce exactly one category with that name.
func TestOptionsToAgentCategories_NamedCategoryOnly_ProducesOneCategory(t *testing.T) {
	cats := optionsToAgentCategories(namedCategoryOnlyOptions())

	if len(cats) != 1 {
		t.Fatalf("got %d categories; want 1 (all options share group 'Tools')", len(cats))
	}
	if cats[0].Name != "Tools" {
		t.Errorf("cats[0].Name = %q; want %q", cats[0].Name, "Tools")
	}
	if len(cats[0].Agents) != 2 {
		t.Errorf("cats[0] has %d agents; want 2", len(cats[0].Agents))
	}
}

// TestOptionsToAgentCategories_NamedCategory_FieldsMappedCorrectly verifies that
// Option.ID→Agent.Key, Option.Label→Agent.Name, Option.Description→Agent.Description,
// and Option.Group→Agent.Category.
func TestOptionsToAgentCategories_NamedCategory_FieldsMappedCorrectly(t *testing.T) {
	opts := []domain.Option{
		{ID: "my-agent", Label: "My Agent", Description: "Does things", Group: "Ops"},
	}

	cats := optionsToAgentCategories(opts)

	if len(cats) != 1 || len(cats[0].Agents) != 1 {
		t.Fatalf("expected one category with one agent; got %d categories", len(cats))
	}
	a := cats[0].Agents[0]
	if a.Key != "my-agent" {
		t.Errorf("Agent.Key = %q; want %q (Option.ID must map to Agent.Key)", a.Key, "my-agent")
	}
	if a.Name != "My Agent" {
		t.Errorf("Agent.Name = %q; want %q (Option.Label must map to Agent.Name)", a.Name, "My Agent")
	}
	if a.Description != "Does things" {
		t.Errorf("Agent.Description = %q; want %q", a.Description, "Does things")
	}
	if a.Category != "Ops" {
		t.Errorf("Agent.Category = %q; want %q (Option.Group must map to Agent.Category)", a.Category, "Ops")
	}
}

// TestOptionsToAgentCategories_MultipleNamedCategories_OrderByFirstAppearance verifies
// that categories are ordered by first appearance of each Group value, not alphabetically.
// Given options in order [AI, Tools, AI], the result must be [AI, Tools].
func TestOptionsToAgentCategories_MultipleNamedCategories_OrderByFirstAppearance(t *testing.T) {
	opts := []domain.Option{
		{ID: "a1", Label: "AI Agent 1", Group: "AI"},
		{ID: "t1", Label: "Tools Agent 1", Group: "Tools"},
		{ID: "a2", Label: "AI Agent 2", Group: "AI"},
	}

	cats := optionsToAgentCategories(opts)

	if len(cats) != 2 {
		t.Fatalf("got %d categories; want 2 (AI, Tools)", len(cats))
	}
	if cats[0].Name != "AI" {
		t.Errorf("cats[0].Name = %q; want %q (AI appeared first)", cats[0].Name, "AI")
	}
	if cats[1].Name != "Tools" {
		t.Errorf("cats[1].Name = %q; want %q", cats[1].Name, "Tools")
	}
}

// TestOptionsToAgentCategories_AgentOrderWithinCategoryPreserved verifies that the
// declaration order of agents within a category is preserved.
func TestOptionsToAgentCategories_AgentOrderWithinCategoryPreserved(t *testing.T) {
	opts := []domain.Option{
		{ID: "agent-c", Label: "C", Group: "Group"},
		{ID: "agent-a", Label: "A", Group: "Group"},
		{ID: "agent-b", Label: "B", Group: "Group"},
	}

	cats := optionsToAgentCategories(opts)

	if len(cats) != 1 || len(cats[0].Agents) != 3 {
		t.Fatalf("expected one category with three agents; got %d categories", len(cats))
	}
	wantOrder := []string{"agent-c", "agent-a", "agent-b"}
	for i, want := range wantOrder {
		if cats[0].Agents[i].Key != want {
			t.Errorf("Agents[%d].Key = %q; want %q (declaration order must be preserved)",
				i, cats[0].Agents[i].Key, want)
		}
	}
}

// ---------------------------------------------------------------------------
// optionsToAgentCategories: empty-group (uncategorised) agents
// ---------------------------------------------------------------------------

// TestOptionsToAgentCategories_UncategorisedOnly_EmptyNameCategory verifies that options
// with an empty Group produce a category with an empty Name (the implementation renders
// this as "(uncategorized)").
func TestOptionsToAgentCategories_UncategorisedOnly_EmptyNameCategory(t *testing.T) {
	cats := optionsToAgentCategories(uncategorisedOnlyOptions())

	if len(cats) != 1 {
		t.Fatalf("got %d categories; want 1 (all options have empty group)", len(cats))
	}
	if cats[0].Name != "" {
		t.Errorf("cats[0].Name = %q; want empty string for agents with no group", cats[0].Name)
	}
	if len(cats[0].Agents) != 2 {
		t.Errorf("cats[0] has %d agents; want 2", len(cats[0].Agents))
	}
}

// TestOptionsToAgentCategories_MixedGroups_EmptyGroupAlwaysLast verifies the key invariant:
// options with an empty Group are always placed in the last category, regardless of their
// position in the input slice.
func TestOptionsToAgentCategories_MixedGroups_EmptyGroupAlwaysLast(t *testing.T) {
	// Note: empty-group option appears first in the input — but must still be last in output.
	opts := []domain.Option{
		{ID: "plain-1", Label: "Plain 1", Group: ""},   // uncategorised — input position 0
		{ID: "ai-agent", Label: "AI Agent", Group: "AI"}, // named
	}

	cats := optionsToAgentCategories(opts)

	if len(cats) != 2 {
		t.Fatalf("got %d categories; want 2 (AI and empty-group)", len(cats))
	}
	last := cats[len(cats)-1]
	if last.Name != "" {
		t.Errorf("last category Name = %q; want empty string;\n"+
			"the empty-Group category must always be placed last, even if its options appear first in the input",
			last.Name)
	}
}

// TestOptionsToAgentCategories_MixedGroups_NamedCategoriesBeforeEmpty verifies that named
// categories precede the empty-Name category in the output.
func TestOptionsToAgentCategories_MixedGroups_NamedCategoriesBeforeEmpty(t *testing.T) {
	cats := optionsToAgentCategories(standaloneAgentOptions())

	if len(cats) < 2 {
		t.Fatalf("got %d categories; want at least 2", len(cats))
	}
	// The last category must have an empty name.
	last := cats[len(cats)-1]
	if last.Name != "" {
		t.Errorf("last category Name = %q; want empty string (uncategorised agents must be last)",
			last.Name)
	}
	// The non-last categories must have non-empty names.
	for i, cat := range cats[:len(cats)-1] {
		if cat.Name == "" {
			t.Errorf("cats[%d].Name is empty but it is not the last category; named categories must precede the uncategorised group",
				i)
		}
	}
}

// TestOptionsToAgentCategories_MixedGroups_TotalAgentCountPreserved verifies that no
// option is dropped when processing a mixed set.
func TestOptionsToAgentCategories_MixedGroups_TotalAgentCountPreserved(t *testing.T) {
	opts := standaloneAgentOptions() // 3 options: 2 in "AI", 1 in ""

	cats := optionsToAgentCategories(opts)

	total := 0
	for _, cat := range cats {
		total += len(cat.Agents)
	}
	if total != len(opts) {
		t.Errorf("total agents across all categories = %d; want %d (no option must be dropped)",
			total, len(opts))
	}
}

// ---------------------------------------------------------------------------
// Routing: QStandaloneAgents does not use the generic fallback
// ---------------------------------------------------------------------------

// TestMultiSelectRouting_QStandaloneAgents_DoesNotUseGenericFallback verifies that a
// QStandaloneAgents SelectMany question does NOT activate the generic inlineSelectOne
// overlay. The dedicated StandaloneAgentScreen must be used instead.
func TestMultiSelectRouting_QStandaloneAgents_DoesNotUseGenericFallback(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QStandaloneAgents, standaloneAgentOptions())

	m.Update(qMsg)

	if m.selectOverlay != nil {
		t.Error("selectOverlay is non-nil after QStandaloneAgents SelectMany; " +
			"the generic inlineSelectOne must not handle this question — use the dedicated StandaloneAgentScreen")
	}
	if m.screen != screenQuestion {
		t.Errorf("screen = %v after QStandaloneAgents; want screenQuestion", m.screen)
	}
}

// TestMultiSelectRouting_QStandaloneAgents_ViewShowsStandaloneAgentTitle verifies that
// after a QStandaloneAgents question the View output comes from StandaloneAgentScreen,
// which renders a title containing "Standalone" (not the generic question title).
func TestMultiSelectRouting_QStandaloneAgents_ViewShowsStandaloneAgentTitle(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QStandaloneAgents, standaloneAgentOptions())
	m.Update(qMsg)

	view := m.View()

	if !strings.Contains(view, "Standalone") {
		t.Errorf("View() after QStandaloneAgents does not contain 'Standalone'; "+
			"the StandaloneAgentScreen renders that title but the generic overlay does not:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: QStandaloneAgents — multiple selections deliver all IDs
// ---------------------------------------------------------------------------

// TestMultiSelectRoundTrip_QStandaloneAgents_MultipleSelectionsDeliverAllIDs verifies that
// selecting multiple standalone agents and confirming delivers all selected IDs through the
// reply channel as a MultiChoiceAnswer with Status=Answered.
func TestMultiSelectRoundTrip_QStandaloneAgents_MultipleSelectionsDeliverAllIDs(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QStandaloneAgents, standaloneAgentOptions())
	m.Update(qMsg)

	// Select the first agent (Space), move down (Down), select the second (Space), confirm (Enter).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenRunning {
		t.Errorf("screen = %v after confirming standalone agent selection; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.Answered {
			t.Errorf("reply Status = %q; want Answered", ans.multiChoiceAns.Status)
		}
		ids := ans.multiChoiceAns.OptionIDs
		if len(ids) != 2 {
			t.Errorf("reply contains %d IDs %v; want 2 ([openai-adapter claude-adapter]) — "+
				"the multi-select round trip must deliver every selected ID",
				len(ids), ids)
		}
	default:
		t.Error("reply channel has no message after confirming standalone agent selection")
	}
}

// TestMultiSelectRoundTrip_QStandaloneAgents_ConfirmWithNoSelection_ProducesSkip verifies
// that confirming the StandaloneAgentScreen without selecting any agent produces a
// MultiChoiceAnswer with Status=SkippedOne rather than Answered with an empty OptionIDs
// slice. The app layer uses Status==Answered to decide whether to apply selections.
func TestMultiSelectRoundTrip_QStandaloneAgents_ConfirmWithNoSelection_ProducesSkip(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QStandaloneAgents, standaloneAgentOptions())
	m.Update(qMsg)

	// Confirm without selecting anything.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenRunning {
		t.Errorf("screen = %v after empty standalone confirmation; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.SkippedOne {
			t.Errorf("reply Status = %q after confirming with no selection; want SkippedOne "+
				"(empty confirmation must not produce Answered with an empty slice — "+
				"the app layer uses Status==Answered to decide whether to apply selections)",
				ans.multiChoiceAns.Status)
		}
	default:
		t.Error("reply channel has no message after empty confirmation on StandaloneAgentScreen")
	}
}

// TestMultiSelectRoundTrip_QStandaloneAgents_Esc_ProducesCancelled verifies that pressing
// Esc on the StandaloneAgentScreen sends a MultiChoiceAnswer with Status=Cancelled. Partial
// selections made before pressing Esc must not be delivered.
func TestMultiSelectRoundTrip_QStandaloneAgents_Esc_ProducesCancelled(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QStandaloneAgents, standaloneAgentOptions())
	m.Update(qMsg)

	// Precondition: verify the dedicated StandaloneAgentScreen is active, not the generic
	// inlineSelectOne overlay. In RED phase the routing is not yet wired, so the view will
	// not contain "Standalone" — this t.Fatal fires and the test fails for the right reason.
	// Without this guard the generic overlay's Esc handler coincidentally produces Cancelled,
	// making the test pass for the wrong reason and providing no coverage of the standalone
	// overlay's Esc behaviour.
	{
		view := m.View()
		if !strings.Contains(view, "Standalone") {
			t.Fatalf("precondition: the dedicated StandaloneAgentScreen is not active after "+
				"QStandaloneAgents; want the view to contain \"Standalone\" (the screen's title).\n"+
				"Wire the QStandaloneAgents routing case to StandaloneAgentScreen in handleQuestionMsg.\n"+
				"View:\n%s", view)
		}
	}

	// Select one agent, then cancel — partial selection must not be delivered.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.screen != screenRunning {
		t.Errorf("screen = %v after Esc on standalone agent screen; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.Cancelled {
			t.Errorf("reply Status = %q after Esc on standalone agent screen; want Cancelled "+
				"(Esc must not deliver partial selections)",
				ans.multiChoiceAns.Status)
		}
	default:
		t.Error("reply channel has no message after Esc on StandaloneAgentScreen")
	}
}

// ---------------------------------------------------------------------------
// Resize propagation
// ---------------------------------------------------------------------------

// TestMultiSelectRouting_QStandaloneAgents_ResizeDoesNotPanic verifies that a
// WindowSizeMsg does not panic when the standalone agent overlay is active.
func TestMultiSelectRouting_QStandaloneAgents_ResizeDoesNotPanic(t *testing.T) {
	m := newRoutingModel()
	m.Update(buildSelectManyMsg(domain.QStandaloneAgents, standaloneAgentOptions()))

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("WindowSizeMsg panicked with active standalone agent overlay: %v", r)
		}
	}()

	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
}
