package screens

// models.go implements the ModelSelectScreen overlay used for per-tier and per-agent model
// selection questions (QTierModel, QAgentModel). The overlay displays the tier/agent rationale
// text prominently and lists the harness model options. Skip (s) and skip-all (S) key bindings
// are shown when the question permits them. When AllowCustom is true, selecting the "Enter
// custom model ID…" row switches to an inline text input so the user never leaves the overlay.
//
// The screen is shown as an overlay during screenRunning; it is not a navigation step. Esc
// sends Cancelled, which aborts the run.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/widgets"
)

// modelCustomItemID is the sentinel list-item ID for the "Enter custom model ID…" row.
const modelCustomItemID = "__custom__"

// ModelSelectScreen is the interactive overlay for QTierModel and QAgentModel questions. It
// is instantiated by the root model when the app layer calls SelectOne with either of those
// question IDs and is driven through the model/update/view cycle without any real terminal.
type ModelSelectScreen struct {
	q    domain.ChoiceQuestion
	list *widgets.List

	// When customMode is true the list is hidden and textInput captures the model ID.
	customMode bool
	textInput  *widgets.TextInput

	done   bool
	back   bool // Esc was pressed; root model sends Cancelled
	answer domain.ChoiceAnswer

	width  int
	height int
	styles Styles
}

// NewModelSelectScreen creates the model selection overlay for the given question.
func NewModelSelectScreen(q domain.ChoiceQuestion, width, height int, styles Styles) *ModelSelectScreen {
	items := buildModelItems(q)
	listStyles := widgets.ListStyles{
		Normal:   styles.Body,
		Selected: styles.Selected,
		Disabled: styles.Muted,
		Cursor:   "▶",
	}
	listHeight := modelListHeight(q, height)
	s := &ModelSelectScreen{
		q:      q,
		list:   widgets.NewList(items, listHeight, width, listStyles),
		width:  width,
		height: height,
		styles: styles,
	}
	return s
}

// buildModelItems converts the question's option list to widget items and appends the
// virtual "Enter custom model ID…" item when AllowCustom is set.
func buildModelItems(q domain.ChoiceQuestion) []widgets.ListItem {
	items := make([]widgets.ListItem, 0, len(q.Options)+1)
	for _, opt := range q.Options {
		items = append(items, widgets.ListItem{
			ID:             opt.ID,
			Label:          opt.Label,
			Description:    opt.Description,
			Disabled:       opt.Disabled,
			DisabledReason: opt.DisabledReason,
		})
	}
	if q.AllowCustom {
		prompt := "Enter custom model ID…"
		if q.CustomPrompt != "" {
			prompt = q.CustomPrompt
		}
		items = append(items, widgets.ListItem{
			ID:    modelCustomItemID,
			Label: prompt,
		})
	}
	return items
}

// modelListHeight computes the available height for the list given the fixed header/footer rows.
func modelListHeight(q domain.ChoiceQuestion, totalHeight int) int {
	reserved := 5 // title + prompt + border(above detail) + border(below detail) + help
	if q.Detail != "" {
		// Estimate detail lines: count newlines in the text.
		reserved += strings.Count(q.Detail, "\n") + 2
	}
	h := totalHeight - reserved
	if h < 2 {
		return 2
	}
	return h
}

// Update processes a tea.Msg. Returns true when the screen has a final answer. The caller
// must check Done() and then call Answer() to retrieve the result.
func (s *ModelSelectScreen) Update(msg tea.Msg) bool {
	if s.customMode && s.textInput != nil {
		_ = s.textInput.Update(msg)
		if s.textInput.Done() {
			s.answer = domain.ChoiceAnswer{Status: domain.Answered, Custom: s.textInput.Value()}
			s.done = true
			return true
		}
		if s.textInput.Back() {
			// Cancel custom entry and return to the option list.
			s.customMode = false
			s.textInput = nil
			return false
		}
		return false
	}

	// Check skip / skip-all key bindings before delegating to the list.
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "s":
			if s.q.AllowSkip {
				s.answer = domain.ChoiceAnswer{Status: domain.SkippedOne}
				s.done = true
				return true
			}
		case "S":
			if s.q.AllowSkipAll {
				s.answer = domain.ChoiceAnswer{Status: domain.SkippedAll}
				s.done = true
				return true
			}
		}
	}

	_ = s.list.Update(msg)

	if s.list.Back() {
		s.list.Reset()
		s.back = true
		s.answer = domain.ChoiceAnswer{Status: domain.Cancelled}
		s.done = true
		return true
	}

	if s.list.Done() {
		id := s.list.SelectedID()
		s.list.Reset()

		if id == modelCustomItemID {
			// Transition to inline custom model ID entry.
			inputStyles := widgets.TextInputStyles{
				Label:  s.styles.Subtitle,
				Input:  s.styles.Body,
				ErrMsg: s.styles.Error,
			}
			label := "Model ID:"
			if s.q.CustomPrompt != "" {
				label = s.q.CustomPrompt
			}
			ti := widgets.NewTextInput(label, "e.g. provider/model-name", s.width, inputStyles)
			s.textInput = ti
			s.customMode = true
			return false
		}

		s.answer = domain.ChoiceAnswer{Status: domain.Answered, OptionID: id}
		s.done = true
		return true
	}

	return false
}

// Done reports whether the screen has produced a final answer.
func (s *ModelSelectScreen) Done() bool { return s.done }

// Back reports whether Esc was pressed (run should be cancelled).
func (s *ModelSelectScreen) Back() bool { return s.back }

// Answer returns the final ChoiceAnswer. Only valid when Done() is true.
func (s *ModelSelectScreen) Answer() domain.ChoiceAnswer { return s.answer }

// View renders the model selection overlay.
func (s *ModelSelectScreen) View() string {
	var sb strings.Builder

	title := s.q.Title
	if title == "" {
		title = "Select Model"
	}
	sb.WriteString(s.styles.Title.Width(s.width).Render(title))
	sb.WriteByte('\n')

	if s.q.Prompt != "" {
		sb.WriteString(s.styles.Subtitle.Width(s.width).Render(s.q.Prompt))
		sb.WriteByte('\n')
	}

	// Rationale / detail block: shown between horizontal rules so it stands out.
	if s.q.Detail != "" {
		rule := strings.Repeat("─", s.width)
		sb.WriteString(s.styles.Border.Width(s.width).Render(rule))
		sb.WriteByte('\n')
		sb.WriteString(s.styles.Muted.Width(s.width).Render(s.q.Detail))
		sb.WriteByte('\n')
		sb.WriteString(s.styles.Border.Width(s.width).Render(rule))
		sb.WriteByte('\n')
	}

	if s.customMode && s.textInput != nil {
		sb.WriteString(s.textInput.View())
		sb.WriteByte('\n')
		sb.WriteString(s.styles.Help.Width(s.width).Render("enter confirm  esc back to list"))
		return sb.String()
	}

	sb.WriteString(s.list.View())
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Help.Width(s.width).Render(s.buildHelp()))
	return sb.String()
}

// buildHelp assembles the context-sensitive help line based on which actions are available.
func (s *ModelSelectScreen) buildHelp() string {
	parts := []string{"↑/k up", "↓/j down", "enter select"}
	if s.q.AllowSkip {
		parts = append(parts, "s skip")
	}
	if s.q.AllowSkipAll {
		parts = append(parts, "S skip all")
	}
	parts = append(parts, "esc cancel run")
	return strings.Join(parts, "  ")
}

// Resize updates the overlay dimensions and propagates to sub-widgets.
func (s *ModelSelectScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	listHeight := modelListHeight(s.q, height)
	s.list.Resize(listHeight, width)
	if s.textInput != nil {
		s.textInput.Resize(width)
	}
}

// Reset clears the done/back/answer state so the screen can be reused.
func (s *ModelSelectScreen) Reset() {
	s.done = false
	s.back = false
	s.answer = domain.ChoiceAnswer{}
	s.customMode = false
	s.textInput = nil
	s.list.Reset()
}
