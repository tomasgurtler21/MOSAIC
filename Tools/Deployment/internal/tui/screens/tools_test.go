package screens_test

// tools_test.go verifies the TextPromptScreen overlay: text capture, skip/skip-all key
// bindings, validation forwarding, Esc cancellation, and the help text.

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// customToolQuestion builds a TextQuestion for a QCustomTool prompt.
func customToolQuestion(toolName string, allowSkip, allowSkipAll bool) domain.TextQuestion {
	return domain.TextQuestion{
		Question: domain.Question{
			ID:           domain.QCustomTool,
			Subject:      toolName,
			Title:        "Name the MCP server for: " + toolName,
			Prompt:       "Enter the MCP server name (as used in your harness configuration).",
			Detail:       "This tool has no mapping for the selected harness. Provide the MCP server name or skip.",
			AllowSkip:    allowSkip,
			AllowSkipAll: allowSkipAll,
		},
	}
}

// customModelTextQuestion builds a TextQuestion for a QCustomModel prompt.
func customModelTextQuestion() domain.TextQuestion {
	return domain.TextQuestion{
		Question: domain.Question{
			ID:    domain.QCustomModel,
			Title: "Enter custom model ID",
		},
		Default: "",
	}
}

// typeText sends a sequence of rune key messages to a TextPromptScreen.
func typeTextIntoPrompt(s *screens.TextPromptScreen, text string) {
	for _, ch := range text {
		s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
}

// ---------------------------------------------------------------------------
// Text capture
// ---------------------------------------------------------------------------

// TestTextPromptScreen_TypeAndEnter_ReturnsAnsweredWithText verifies that the user can type
// a value and press Enter to confirm it, producing Answered with the entered text.
func TestTextPromptScreen_TypeAndEnter_ReturnsAnsweredWithText(t *testing.T) {
	q := customToolQuestion("my-generic-tool", false, false)
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	typeTextIntoPrompt(s, "my-mcp-server")
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter; want true")
	}
	ans := s.Answer()
	if ans.Status != domain.Answered {
		t.Errorf("Answer().Status = %q, want Answered", ans.Status)
	}
	if ans.Text != "my-mcp-server" {
		t.Errorf("Answer().Text = %q, want %q", ans.Text, "my-mcp-server")
	}
}

// TestTextPromptScreen_EmptyEnter_WhenNoValidation_Succeeds verifies that an empty value
// can be submitted when no validation function is attached. (Validation is the caller's
// responsibility; without it any text is accepted.)
func TestTextPromptScreen_EmptyEnter_WhenNoValidation_Succeeds(t *testing.T) {
	q := customModelTextQuestion()
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter with empty input and no validation; want true")
	}
}

// ---------------------------------------------------------------------------
// Validation forwarding
// ---------------------------------------------------------------------------

// TestTextPromptScreen_Validation_RejectsInvalidInput verifies that when a Validate function
// is attached to the TextQuestion, invalid input is rejected and Done() remains false.
func TestTextPromptScreen_Validation_RejectsInvalidInput(t *testing.T) {
	rejectEmpty := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New("value cannot be empty")
		}
		return nil
	}
	q := customToolQuestion("my-tool", false, false)
	q.Validate = rejectEmpty
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	// Attempt to confirm with empty input.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if s.Done() {
		t.Error("Done() = true after Enter with empty input when validation rejects it; want false")
	}
}

// TestTextPromptScreen_Validation_AcceptsValidInput verifies that valid input passes the
// Validate function and Done() is set.
func TestTextPromptScreen_Validation_AcceptsValidInput(t *testing.T) {
	rejectEmpty := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New("value cannot be empty")
		}
		return nil
	}
	q := customToolQuestion("my-tool", false, false)
	q.Validate = rejectEmpty
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	typeTextIntoPrompt(s, "valid-server-name")
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter with valid input; want true")
	}
}

// ---------------------------------------------------------------------------
// Skip and skip-all key bindings
// ---------------------------------------------------------------------------

// TestTextPromptScreen_SkipKey_ReturnsSkippedOne verifies that pressing 's' when AllowSkip
// is true returns SkippedOne without requiring text input.
func TestTextPromptScreen_SkipKey_ReturnsSkippedOne(t *testing.T) {
	q := customToolQuestion("my-tool", true, false)
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	if !s.Done() {
		t.Error("Done() = false after 's' with AllowSkip; want true")
	}
	if s.Answer().Status != domain.SkippedOne {
		t.Errorf("Answer().Status = %q, want SkippedOne", s.Answer().Status)
	}
}

// TestTextPromptScreen_SkipAllKey_ReturnsSkippedAll verifies that pressing 'S' when
// AllowSkipAll is true returns SkippedAll.
func TestTextPromptScreen_SkipAllKey_ReturnsSkippedAll(t *testing.T) {
	q := customToolQuestion("my-tool", true, true)
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})

	if !s.Done() {
		t.Error("Done() = false after 'S' with AllowSkipAll; want true")
	}
	if s.Answer().Status != domain.SkippedAll {
		t.Errorf("Answer().Status = %q, want SkippedAll", s.Answer().Status)
	}
}

// TestTextPromptScreen_SkipKey_WhenNotAllowed_DoesNotComplete verifies that 's' has no
// effect when AllowSkip is false.
func TestTextPromptScreen_SkipKey_WhenNotAllowed_DoesNotComplete(t *testing.T) {
	q := customToolQuestion("my-tool", false, false)
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	if s.Done() {
		t.Error("Done() = true after 's' when AllowSkip is false; want false")
	}
}

// ---------------------------------------------------------------------------
// Cancellation
// ---------------------------------------------------------------------------

// TestTextPromptScreen_Esc_SendsCancelled verifies that pressing Esc sets Done() and returns
// Cancelled, causing the root model to abort the run.
func TestTextPromptScreen_Esc_SendsCancelled(t *testing.T) {
	q := customToolQuestion("my-tool", true, true)
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Done() {
		t.Error("Done() = false after Esc; want true")
	}
	if s.Answer().Status != domain.Cancelled {
		t.Errorf("Answer().Status = %q, want Cancelled", s.Answer().Status)
	}
	if !s.Cancelled() {
		t.Error("Cancelled() = false after Esc; want true")
	}
}

// ---------------------------------------------------------------------------
// View content
// ---------------------------------------------------------------------------

// TestTextPromptScreen_View_ShowsToolName verifies that the tool or subject name appears in
// the view so the user knows which tool they are naming.
func TestTextPromptScreen_View_ShowsToolName(t *testing.T) {
	q := customToolQuestion("my-unmapped-tool", true, true)
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	view := s.View()

	if !strings.Contains(view, "my-unmapped-tool") {
		t.Errorf("view does not show the tool name 'my-unmapped-tool':\n%s", view)
	}
}

// TestTextPromptScreen_View_ShowsTitleAndDetail verifies that the title and detail text are
// rendered so the user understands the context of the prompt.
func TestTextPromptScreen_View_ShowsTitleAndDetail(t *testing.T) {
	q := customToolQuestion("some-tool", false, false)
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	view := s.View()

	if !strings.Contains(view, "Name the MCP server for") {
		t.Errorf("view does not show the question title:\n%s", view)
	}
	if !strings.Contains(view, "no mapping") {
		t.Errorf("view does not show the question detail:\n%s", view)
	}
}

// TestTextPromptScreen_View_HelpText_ShowsSkipWhenAllowed verifies that the help bar shows
// the 's skip' option only when AllowSkip is true.
func TestTextPromptScreen_View_HelpText_ShowsSkipWhenAllowed(t *testing.T) {
	q := customToolQuestion("some-tool", true, true)
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "s skip") {
		t.Errorf("help text does not mention 's skip' when AllowSkip is true:\n%s", s.View())
	}
	if !strings.Contains(view, "S skip all") {
		t.Errorf("help text does not mention 'S skip all' when AllowSkipAll is true:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Default value pre-fill
// ---------------------------------------------------------------------------

// TestTextPromptScreen_DefaultValue_IsPreFilled verifies that when the TextQuestion has a
// Default value, it is pre-filled in the text input.
func TestTextPromptScreen_DefaultValue_IsPreFilled(t *testing.T) {
	q := domain.TextQuestion{
		Question: domain.Question{
			ID:    domain.QCustomModel,
			Title: "Enter custom model ID",
		},
		Default: "provider/default-model",
	}
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	// Confirm the pre-filled value without typing.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after confirming a pre-filled value; want true")
	}
	if s.Answer().Text != "provider/default-model" {
		t.Errorf("Answer().Text = %q, want %q", s.Answer().Text, "provider/default-model")
	}
}
