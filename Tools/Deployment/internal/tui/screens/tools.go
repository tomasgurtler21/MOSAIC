package screens

// tools.go implements the TextPromptScreen overlay used for AskText questions: QCustomTool
// (name an unmapped MCP server) and QCustomModel (enter a free-form model ID). The screen
// shows the question context, a single-line text input, and optional skip/skip-all bindings.
//
// Pressing Esc sends Cancelled, which aborts the run. Pressing 's'/'S' (when permitted)
// returns SkippedOne/SkippedAll without any text.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/widgets"
)

// TextPromptScreen is the interactive overlay for AskText questions. It is instantiated by
// the root model and driven through the model/update/view cycle without any real terminal.
type TextPromptScreen struct {
	q         domain.TextQuestion
	textInput *widgets.TextInput
	done      bool
	cancelled bool
	answer    domain.TextAnswer
	width     int
	height    int
	styles    Styles
}

// NewTextPromptScreen creates a text-input overlay for the given AskText question.
func NewTextPromptScreen(q domain.TextQuestion, width, height int, styles Styles) *TextPromptScreen {
	inputStyles := widgets.TextInputStyles{
		Label:  styles.Subtitle,
		Input:  styles.Body,
		ErrMsg: styles.Error,
	}
	label := q.Prompt
	if label == "" {
		label = "Enter value:"
	}
	placeholder := ""
	if q.Default != "" {
		placeholder = q.Default
	}
	ti := widgets.NewTextInput(label, placeholder, width, inputStyles)
	if q.Validate != nil {
		ti.SetValidate(q.Validate)
	}
	if q.Default != "" {
		ti.SetValue(q.Default)
	}
	return &TextPromptScreen{
		q:         q,
		textInput: ti,
		width:     width,
		height:    height,
		styles:    styles,
	}
}

// Update processes a tea.Msg. Returns true when the screen has a final answer.
func (s *TextPromptScreen) Update(msg tea.Msg) bool {
	// Check skip / skip-all before delegating to the text input.
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "s":
			if s.q.AllowSkip {
				s.answer = domain.TextAnswer{Status: domain.SkippedOne}
				s.done = true
				return true
			}
		case "S":
			if s.q.AllowSkipAll {
				s.answer = domain.TextAnswer{Status: domain.SkippedAll}
				s.done = true
				return true
			}
		}
	}

	_ = s.textInput.Update(msg)

	if s.textInput.Back() {
		s.answer = domain.TextAnswer{Status: domain.Cancelled}
		s.done = true
		s.cancelled = true
		return true
	}
	if s.textInput.Done() {
		s.answer = domain.TextAnswer{Status: domain.Answered, Text: s.textInput.Value()}
		s.done = true
		return true
	}
	return false
}

// Done reports whether the screen has produced a final answer.
func (s *TextPromptScreen) Done() bool { return s.done }

// Cancelled reports whether Esc was pressed (run should be cancelled).
func (s *TextPromptScreen) Cancelled() bool { return s.cancelled }

// Answer returns the final TextAnswer. Only valid when Done() is true.
func (s *TextPromptScreen) Answer() domain.TextAnswer { return s.answer }

// View renders the text prompt overlay.
func (s *TextPromptScreen) View() string {
	var sb strings.Builder

	title := s.q.Title
	if title == "" {
		title = "Enter Value"
	}
	sb.WriteString(s.styles.Title.Width(s.width).Render(title))
	sb.WriteByte('\n')

	if s.q.Detail != "" {
		sb.WriteString(s.styles.Muted.Width(s.width).Render(s.q.Detail))
		sb.WriteByte('\n')
	}

	if s.q.Subject != "" {
		sb.WriteString(s.styles.Body.Width(s.width).Render("Tool: " + s.q.Subject))
		sb.WriteByte('\n')
	}

	rule := strings.Repeat("─", s.width)
	sb.WriteString(s.styles.Border.Width(s.width).Render(rule))
	sb.WriteByte('\n')

	sb.WriteString(s.textInput.View())
	sb.WriteByte('\n')

	sb.WriteString(s.styles.Help.Width(s.width).Render(s.buildHelp()))
	return sb.String()
}

// buildHelp assembles the context-sensitive help line.
func (s *TextPromptScreen) buildHelp() string {
	parts := []string{"enter confirm"}
	if s.q.AllowSkip {
		parts = append(parts, "s skip")
	}
	if s.q.AllowSkipAll {
		parts = append(parts, "S skip all")
	}
	parts = append(parts, "esc cancel run")
	return strings.Join(parts, "  ")
}

// Resize updates the overlay dimensions.
func (s *TextPromptScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.textInput.Resize(width)
}

// Reset clears the done/cancelled/answer state.
func (s *TextPromptScreen) Reset() {
	s.done = false
	s.cancelled = false
	s.answer = domain.TextAnswer{}
	s.textInput.Reset()
}
