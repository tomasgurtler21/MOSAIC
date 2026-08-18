package screens

// conflicts.go implements the ConflictScreen overlay shown for QLocalModification questions.
// The screen makes clear which file was locally modified, displays the modification detail,
// and offers three decisions: overwrite, skip, backup-then-overwrite. The options come from
// the question's option list so the app controls the decision labels.
//
// The screen explicitly warns that no run-level rollback exists, so the user understands
// the permanence of the overwrite and backup-then-overwrite choices.
//
// Pressing Esc sends Cancelled (the run is aborted). There is no back-navigation: a
// mid-flow conflict decision is not a step the user can undo by pressing Esc.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/widgets"
)

// ConflictScreen is the interactive overlay for QLocalModification questions.
type ConflictScreen struct {
	q      domain.ChoiceQuestion
	list   *widgets.List
	done   bool
	back   bool
	answer domain.ChoiceAnswer
	width  int
	height int
	styles Styles
}

// NewConflictScreen creates the locally-modified file conflict overlay.
func NewConflictScreen(q domain.ChoiceQuestion, width, height int, styles Styles) *ConflictScreen {
	items := make([]widgets.ListItem, len(q.Options))
	for i, opt := range q.Options {
		items[i] = widgets.ListItem{
			ID:             opt.ID,
			Label:          opt.Label,
			Description:    opt.Description,
			Disabled:       opt.Disabled,
			DisabledReason: opt.DisabledReason,
		}
	}
	listStyles := widgets.ListStyles{
		Normal:   styles.Body,
		Selected: styles.Selected,
		Disabled: styles.Muted,
		Cursor:   "▶",
	}
	listHeight := conflictListHeight(q, height)
	return &ConflictScreen{
		q:      q,
		list:   widgets.NewList(items, listHeight, width, listStyles),
		width:  width,
		height: height,
		styles: styles,
	}
}

// conflictListHeight computes the visible list height given the fixed header rows.
func conflictListHeight(q domain.ChoiceQuestion, totalHeight int) int {
	// title + warning + file label + blank + detail lines + blank + warning note + help
	reserved := 8
	if q.Detail != "" {
		reserved += strings.Count(q.Detail, "\n") + 1
	}
	h := totalHeight - reserved
	if h < 2 {
		return 2
	}
	return h
}

// Update processes a tea.Msg. Returns true when the screen has a final answer.
func (s *ConflictScreen) Update(msg tea.Msg) bool {
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
		s.answer = domain.ChoiceAnswer{Status: domain.Answered, OptionID: id}
		s.done = true
		return true
	}

	return false
}

// Done reports whether the screen has produced a final answer.
func (s *ConflictScreen) Done() bool { return s.done }

// Back reports whether Esc was pressed (run should be cancelled).
func (s *ConflictScreen) Back() bool { return s.back }

// Answer returns the final ChoiceAnswer. Only valid when Done() is true.
func (s *ConflictScreen) Answer() domain.ChoiceAnswer { return s.answer }

// View renders the conflict decision overlay.
func (s *ConflictScreen) View() string {
	var sb strings.Builder

	// Title + warning.
	sb.WriteString(s.styles.Warning.Width(s.width).Render("Locally Modified File"))
	sb.WriteByte('\n')

	// File path.
	filePath := s.q.Subject
	if filePath == "" {
		filePath = "(unknown path)"
	}
	sb.WriteString(s.styles.Body.Width(s.width).Render(fmt.Sprintf("File: %s", filePath)))
	sb.WriteByte('\n')

	// Modification detail.
	if s.q.Detail != "" {
		rule := strings.Repeat("─", s.width)
		sb.WriteString(s.styles.Border.Width(s.width).Render(rule))
		sb.WriteByte('\n')
		sb.WriteString(s.styles.Muted.Width(s.width).Render(s.q.Detail))
		sb.WriteByte('\n')
		sb.WriteString(s.styles.Border.Width(s.width).Render(rule))
		sb.WriteByte('\n')
	}

	// Permanent-action warning note.
	note := "Note: overwrite and backup-then-overwrite cannot be undone at the run level."
	sb.WriteString(s.styles.Warning.Width(s.width).Render(note))
	sb.WriteByte('\n')

	if s.q.Prompt != "" {
		sb.WriteString(s.styles.Body.Width(s.width).Render(s.q.Prompt))
		sb.WriteByte('\n')
	}

	sb.WriteString(s.list.View())
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc cancel run"))
	return sb.String()
}

// Resize updates the overlay dimensions.
func (s *ConflictScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	listHeight := conflictListHeight(s.q, height)
	s.list.Resize(listHeight, width)
}

// Reset clears the done/back/answer state.
func (s *ConflictScreen) Reset() {
	s.done = false
	s.back = false
	s.answer = domain.ChoiceAnswer{}
	s.list.Reset()
}
