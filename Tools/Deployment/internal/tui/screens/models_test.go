package screens_test

// models_test.go verifies the ModelSelectScreen overlay: keyboard navigation through the
// model option list, rationale text display, skip/skip-all key bindings, custom model ID
// entry, and the disabled-option behaviour for the "apply tier mapping" option.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func modelEnterKey() tea.Msg { return tea.KeyMsg{Type: tea.KeyEnter} }
func modelEscKey() tea.Msg   { return tea.KeyMsg{Type: tea.KeyEsc} }
func modelDownKey() tea.Msg  { return tea.KeyMsg{Type: tea.KeyDown} }
func modelUpKey() tea.Msg    { return tea.KeyMsg{Type: tea.KeyUp} }
func modelSKey() tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
}
func modelShiftSKey() tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}}
}

// tierModelQuestion builds a ChoiceQuestion for a tier model selection with the given model
// IDs as options. AllowSkip, AllowSkipAll, and AllowCustom control which extra bindings are offered.
func tierModelQuestion(modelIDs []string, allowSkip, allowSkipAll, allowCustom bool, rationale string) domain.ChoiceQuestion {
	opts := make([]domain.Option, len(modelIDs))
	for i, id := range modelIDs {
		opts[i] = domain.Option{ID: id, Label: id}
	}
	return domain.ChoiceQuestion{
		Question: domain.Question{
			ID:           domain.QTierModel,
			Subject:      "heavy",
			Title:        "Select model for tier: heavy",
			Prompt:       "Choose the model to use for heavy-tier agents.",
			Detail:       rationale,
			AllowSkip:    allowSkip,
			AllowSkipAll: allowSkipAll,
			AllowCustom:  allowCustom,
		},
		Options: opts,
	}
}

// agentModelQuestion builds a ChoiceQuestion for per-agent model selection, optionally
// including a disabled "apply tier mapping" option (AC22.3: absent when no mapping exists).
func agentModelQuestion(modelIDs []string, includeTierMapping, tierMappingAvailable bool) domain.ChoiceQuestion {
	opts := []domain.Option{}
	if includeTierMapping {
		opt := domain.Option{
			ID:    "tier-mapping",
			Label: "Apply tier mapping",
		}
		if !tierMappingAvailable {
			opt.Disabled = true
			opt.DisabledReason = "no stored mapping for this tier"
		}
		opts = append(opts, opt)
	}
	for _, id := range modelIDs {
		opts = append(opts, domain.Option{ID: id, Label: id})
	}
	return domain.ChoiceQuestion{
		Question: domain.Question{
			ID:           domain.QAgentModel,
			Subject:      "test-runner",
			Title:        "Select model for agent: test-runner",
			AllowSkip:    true,
			AllowSkipAll: true,
			AllowCustom:  true,
		},
		Options: opts,
	}
}

// ---------------------------------------------------------------------------
// Rationale text display (AC22.2)
// ---------------------------------------------------------------------------

// TestModelSelectScreen_View_ShowsRationaleText verifies that the tier rationale is rendered
// in the overlay so the user understands why a tier exists while selecting a model for it.
func TestModelSelectScreen_View_ShowsRationaleText(t *testing.T) {
	rationale := "Heavy-tier agents perform tasks requiring deep reasoning and long context."
	q := tierModelQuestion([]string{"gpt-4o", "claude-3-opus"}, true, true, false, rationale)
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	view := s.View()

	if !strings.Contains(view, rationale) {
		t.Errorf("view does not contain the tier rationale text:\nwant: %q\ngot:\n%s", rationale, view)
	}
}

// TestModelSelectScreen_View_ShowsTitleAndPrompt verifies that the question title and prompt
// are displayed so the user has context for the selection.
func TestModelSelectScreen_View_ShowsTitleAndPrompt(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o"}, false, false, false, "Some rationale")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	view := s.View()

	if !strings.Contains(view, "Select model for tier: heavy") {
		t.Errorf("view does not contain the question title:\n%s", view)
	}
}

// TestModelSelectScreen_View_ShowsModelOptions verifies that each harness model ID appears
// in the overlay list so the user can choose among them.
func TestModelSelectScreen_View_ShowsModelOptions(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o", "claude-3-opus"}, false, false, false, "")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	view := s.View()

	for _, id := range []string{"gpt-4o", "claude-3-opus"} {
		if !strings.Contains(view, id) {
			t.Errorf("view does not contain model option %q:\n%s", id, view)
		}
	}
}

// ---------------------------------------------------------------------------
// Keyboard navigation and selection
// ---------------------------------------------------------------------------

// TestModelSelectScreen_Enter_OnFirstOption_SetsDone verifies that pressing Enter on the
// first model option sets Done() and returns the correct option ID.
func TestModelSelectScreen_Enter_OnFirstOption_SetsDone(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o", "claude-3-opus"}, false, false, false, "")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	s.Update(modelEnterKey())

	if !s.Done() {
		t.Error("Done() = false after Enter; want true")
	}
	ans := s.Answer()
	if ans.Status != domain.Answered {
		t.Errorf("Answer().Status = %q, want Answered", ans.Status)
	}
	if ans.OptionID != "gpt-4o" {
		t.Errorf("Answer().OptionID = %q, want %q", ans.OptionID, "gpt-4o")
	}
}

// TestModelSelectScreen_Down_Then_Enter_SelectsSecondOption verifies that keyboard navigation
// to a different item and then Enter selects that item, not the initial cursor position.
func TestModelSelectScreen_Down_Then_Enter_SelectsSecondOption(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o", "claude-3-opus"}, false, false, false, "")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	s.Update(modelDownKey())
	s.Update(modelEnterKey())

	if !s.Done() {
		t.Error("Done() = false after Down + Enter; want true")
	}
	ans := s.Answer()
	if ans.OptionID != "claude-3-opus" {
		t.Errorf("Answer().OptionID = %q, want %q", ans.OptionID, "claude-3-opus")
	}
}

// ---------------------------------------------------------------------------
// Skip and skip-all key bindings
// ---------------------------------------------------------------------------

// TestModelSelectScreen_SkipKey_ReturnsSkippedOne verifies that pressing 's' when AllowSkip
// is true returns SkippedOne (the user chose to skip this tier's model selection).
func TestModelSelectScreen_SkipKey_ReturnsSkippedOne(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o"}, true, false, false, "")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	s.Update(modelSKey())

	if !s.Done() {
		t.Error("Done() = false after 's' with AllowSkip; want true")
	}
	if s.Answer().Status != domain.SkippedOne {
		t.Errorf("Answer().Status = %q, want SkippedOne", s.Answer().Status)
	}
}

// TestModelSelectScreen_SkipKey_WhenNotAllowed_DoesNotSetDone verifies that pressing 's'
// when AllowSkip is false has no effect — the user cannot skip a required model selection.
func TestModelSelectScreen_SkipKey_WhenNotAllowed_DoesNotSetDone(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o"}, false, false, false, "")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	s.Update(modelSKey())

	if s.Done() {
		t.Error("Done() = true after 's' when AllowSkip is false; want false")
	}
}

// TestModelSelectScreen_SkipAllKey_ReturnsSkippedAll verifies that pressing 'S' when
// AllowSkipAll is true returns SkippedAll (the app will not ask for any further tier models).
func TestModelSelectScreen_SkipAllKey_ReturnsSkippedAll(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o"}, false, true, false, "")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	s.Update(modelShiftSKey())

	if !s.Done() {
		t.Error("Done() = false after 'S' with AllowSkipAll; want true")
	}
	if s.Answer().Status != domain.SkippedAll {
		t.Errorf("Answer().Status = %q, want SkippedAll", s.Answer().Status)
	}
}

// ---------------------------------------------------------------------------
// Custom model ID entry (I22.4)
// ---------------------------------------------------------------------------

// TestModelSelectScreen_CustomEntry_ShowsTextInput verifies that selecting the "Enter custom
// model ID…" row switches the overlay to inline text-input mode.
func TestModelSelectScreen_CustomEntry_ShowsTextInput(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o"}, false, false, true, "")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	// Navigate to the custom entry row (it is appended after the model options).
	s.Update(modelDownKey()) // from gpt-4o to custom entry
	s.Update(modelEnterKey()) // select the custom entry row

	// The overlay should have switched to text-input mode.
	if s.Done() {
		t.Error("Done() = true after entering custom mode; want false (text input is now active)")
	}
	view := s.View()
	// The text input should be visible in the view.
	if !strings.Contains(view, "Model ID") && !strings.Contains(view, "enter confirm") {
		t.Errorf("view after custom entry selection does not show text input prompt:\n%s", view)
	}
}

// TestModelSelectScreen_CustomEntry_ConfirmTextInput_ReturnsCustomAnswer verifies that
// entering a model ID in the inline text input and pressing Enter returns the typed text in
// ChoiceAnswer.Custom.
func TestModelSelectScreen_CustomEntry_ConfirmTextInput_ReturnsCustomAnswer(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o"}, false, false, true, "")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	// Select the custom entry row.
	s.Update(modelDownKey())
	s.Update(modelEnterKey())

	// Type the model ID character by character and confirm.
	for _, ch := range "my-custom-model" {
		s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	s.Update(modelEnterKey())

	if !s.Done() {
		t.Error("Done() = false after confirming custom model ID; want true")
	}
	ans := s.Answer()
	if ans.Status != domain.Answered {
		t.Errorf("Answer().Status = %q, want Answered", ans.Status)
	}
	if ans.Custom != "my-custom-model" {
		t.Errorf("Answer().Custom = %q, want %q", ans.Custom, "my-custom-model")
	}
}

// TestModelSelectScreen_CustomEntry_EscReturnsToList verifies that pressing Esc in the
// inline text input cancels custom entry and returns to the option list without setting Done.
func TestModelSelectScreen_CustomEntry_EscReturnsToList(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o"}, false, false, true, "")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	// Enter custom mode.
	s.Update(modelDownKey())
	s.Update(modelEnterKey())

	// Cancel out of the text input.
	s.Update(modelEscKey())

	// Should not be done; the option list should be shown again.
	if s.Done() {
		t.Error("Done() = true after Esc in custom entry mode; want false (should return to list)")
	}
	view := s.View()
	if !strings.Contains(view, "gpt-4o") {
		t.Errorf("view after Esc from custom entry does not show model list:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Esc sends Cancelled (run abort)
// ---------------------------------------------------------------------------

// TestModelSelectScreen_Esc_SendsCancelled verifies that pressing Esc in the normal list
// view sends Cancelled (not Back), which causes the app layer to abort the run.
func TestModelSelectScreen_Esc_SendsCancelled(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o"}, true, true, false, "")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	s.Update(modelEscKey())

	if !s.Done() {
		t.Error("Done() = false after Esc; want true (Cancelled must signal the root model)")
	}
	if s.Answer().Status != domain.Cancelled {
		t.Errorf("Answer().Status = %q, want Cancelled", s.Answer().Status)
	}
}

// ---------------------------------------------------------------------------
// Conditional tier mapping option (AC22.3)
// ---------------------------------------------------------------------------

// TestModelSelectScreen_TierMapping_PresentWhenAvailable verifies that the "Apply tier
// mapping" option appears in the agent model list when a tier mapping is stored.
func TestModelSelectScreen_TierMapping_PresentWhenAvailable(t *testing.T) {
	q := agentModelQuestion([]string{"gpt-4o"}, true, true)
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	view := s.View()

	if !strings.Contains(collapseWhitespace(view), "Apply tier mapping") {
		t.Errorf("view does not show 'Apply tier mapping' when a mapping exists:\n%s", view)
	}
}

// TestModelSelectScreen_TierMapping_ShowsDisabledReasonWhenNoMapping verifies that the
// "Apply tier mapping" option is shown but disabled with an explanatory reason when no
// stored mapping exists for this agent's tier (AC22.3).
func TestModelSelectScreen_TierMapping_ShowsDisabledReasonWhenNoMapping(t *testing.T) {
	q := agentModelQuestion([]string{"gpt-4o"}, true, false)
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "no stored mapping for this tier") {
		t.Errorf("view does not explain why tier mapping is unavailable:\n%s", s.View())
	}
}

// TestModelSelectScreen_TierMapping_Absent_WhenNotOffered verifies that when the app does
// not include a tier-mapping option at all, the overlay does not invent one (AC22.3).
func TestModelSelectScreen_TierMapping_Absent_WhenNotOffered(t *testing.T) {
	q := agentModelQuestion([]string{"gpt-4o"}, false, false)
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	view := s.View()

	if strings.Contains(view, "Apply tier mapping") {
		t.Errorf("view shows 'Apply tier mapping' when the app did not offer it; must not be invented:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Help text
// ---------------------------------------------------------------------------

// TestModelSelectScreen_HelpText_ShowsAvailableActions verifies that the help line at the
// bottom of the overlay accurately reflects which skip/cancel actions are available.
func TestModelSelectScreen_HelpText_ShowsAvailableActions(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o"}, true, true, false, "")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "s skip") {
		t.Errorf("help text does not mention 's skip' when AllowSkip is true:\n%s", s.View())
	}
	if !strings.Contains(view, "S skip all") {
		t.Errorf("help text does not mention 'S skip all' when AllowSkipAll is true:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestModelSelectScreen_Reset_ClearsDoneAndAnswer verifies that Reset restores the screen to
// an idle state.
func TestModelSelectScreen_Reset_ClearsDoneAndAnswer(t *testing.T) {
	q := tierModelQuestion([]string{"gpt-4o"}, false, false, false, "")
	s := screens.NewModelSelectScreen(q, 80, 24, plainStyles())

	s.Update(modelEnterKey())
	if !s.Done() {
		t.Fatal("precondition: Done() must be true before Reset")
	}

	s.Reset()

	if s.Done() {
		t.Error("Done() = true after Reset; want false")
	}
	if s.Answer().Status != "" {
		t.Errorf("Answer().Status = %q after Reset; want empty", s.Answer().Status)
	}
}
