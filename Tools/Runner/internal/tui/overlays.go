package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-common/interaction"
)

// ---------------------------------------------------------------------------
// inlineSelectOne
// ---------------------------------------------------------------------------

// inlineSelectOne renders a simple interactive select-one question overlay.
type inlineSelectOne struct {
	q      interaction.ChoiceQuestion
	cursor int
	done   bool
	back   bool
	styles tuicommon.Theme
	width  int
	height int
}

func newInlineSelectOne(q interaction.ChoiceQuestion, styles tuicommon.Theme, width, height int) *inlineSelectOne {
	return &inlineSelectOne{q: q, styles: styles, width: width, height: height}
}

// totalOptions returns the number of selectable entries, including "None" when skip is allowed.
func (s *inlineSelectOne) totalOptions() int {
	if s.q.AllowSkip {
		return len(s.q.Options) + 1
	}
	return len(s.q.Options)
}

// isNoneEntry reports whether idx refers to the synthetic skip entry.
func (s *inlineSelectOne) isNoneEntry(idx int) bool {
	return s.q.AllowSkip && idx == len(s.q.Options)
}

func (s *inlineSelectOne) update(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	switch keyMsg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < s.totalOptions()-1 {
			s.cursor++
		}
	case "enter":
		if s.isNoneEntry(s.cursor) {
			s.done = true
		} else if s.cursor >= 0 && s.cursor < len(s.q.Options) {
			if !s.q.Options[s.cursor].Disabled {
				s.done = true
			}
		}
	case "esc":
		s.back = true
	}
	return s.done || s.back
}

func (s *inlineSelectOne) answer() interaction.ChoiceAnswer {
	if s.back {
		return interaction.ChoiceAnswer{Status: interaction.Cancelled}
	}
	if s.isNoneEntry(s.cursor) {
		return interaction.ChoiceAnswer{Status: interaction.SkippedOne}
	}
	if s.cursor >= 0 && s.cursor < len(s.q.Options) {
		return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: s.q.Options[s.cursor].ID}
	}
	return interaction.ChoiceAnswer{Status: interaction.SkippedOne}
}

func (s *inlineSelectOne) view() string {
	const prefixLen = 2
	labelWidth := s.width - prefixLen
	if labelWidth < 1 {
		labelWidth = 1
	}

	var sb strings.Builder
	sb.WriteString(s.styles.Style(tuicommon.RoleTitle).Width(s.width).Render(s.q.Title))
	sb.WriteByte('\n')
	if s.q.Prompt != "" {
		sb.WriteString(s.styles.Style(tuicommon.RoleBody).Width(s.width).Render(s.q.Prompt))
		sb.WriteByte('\n')
	}
	for i, opt := range s.q.Options {
		prefix := "  "
		if i == s.cursor {
			prefix = "▶ "
		}
		label := opt.Label
		if opt.Disabled {
			sb.WriteString(prefix + s.styles.Style(tuicommon.RoleMuted).Width(labelWidth).Render(label) + "\n")
		} else if i == s.cursor {
			sb.WriteString(prefix + s.styles.Style(tuicommon.RoleSelected).Width(labelWidth).Render(label) + "\n")
		} else {
			sb.WriteString(prefix + s.styles.Style(tuicommon.RoleBody).Width(labelWidth).Render(label) + "\n")
		}
	}
	if s.q.AllowSkip {
		noneIdx := len(s.q.Options)
		prefix := "  "
		if s.cursor == noneIdx {
			prefix = "▶ "
			sb.WriteString(prefix + s.styles.Style(tuicommon.RoleSelected).Width(labelWidth).Render("None") + "\n")
		} else {
			sb.WriteString(prefix + s.styles.Style(tuicommon.RoleBody).Width(labelWidth).Render("None") + "\n")
		}
	}
	sb.WriteString(s.styles.Style(tuicommon.RoleHelp).Width(s.width).Render("↑/k up  ↓/j down  enter select  esc cancel"))
	return sb.String()
}

// ---------------------------------------------------------------------------
// inlineText
// ---------------------------------------------------------------------------

// inlineText renders a text input overlay for AskText questions.
type inlineText struct {
	q      interaction.TextQuestion
	model  textinput.Model
	done   bool
	back   bool
	errMsg string
	styles tuicommon.Theme
	width  int
	height int
}

func newInlineText(q interaction.TextQuestion, styles tuicommon.Theme, width, height int) *inlineText {
	m := textinput.New()
	m.Placeholder = q.Prompt
	if q.Default != "" {
		m.SetValue(q.Default)
	}
	m.Focus()
	return &inlineText{q: q, model: m, styles: styles, width: width, height: height}
}

func (t *inlineText) init() tea.Cmd {
	return textinput.Blink
}

func (t *inlineText) update(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyEnter:
			val := t.model.Value()
			if t.q.Validate != nil {
				if err := t.q.Validate(val); err != nil {
					t.errMsg = err.Error()
					return nil
				}
			}
			t.errMsg = ""
			t.done = true
			return nil
		case tea.KeyEsc:
			t.back = true
			return nil
		}
	}
	var cmd tea.Cmd
	t.model, cmd = t.model.Update(msg)
	return cmd
}

func (t *inlineText) answer() interaction.TextAnswer {
	if t.back {
		return interaction.TextAnswer{Status: interaction.Cancelled}
	}
	return interaction.TextAnswer{Status: interaction.Answered, Text: t.model.Value()}
}

func (t *inlineText) view() string {
	var sb strings.Builder
	sb.WriteString(t.styles.Style(tuicommon.RoleTitle).Width(t.width).Render(t.q.Title))
	sb.WriteByte('\n')
	if t.q.Detail != "" {
		sb.WriteString(t.styles.Style(tuicommon.RoleMuted).Width(t.width).Render(t.q.Detail))
		sb.WriteByte('\n')
	}
	sb.WriteString(t.model.View())
	if t.errMsg != "" {
		sb.WriteByte('\n')
		sb.WriteString(t.styles.Style(tuicommon.RoleError).Width(t.width).Render(t.errMsg))
	}
	sb.WriteByte('\n')
	sb.WriteString(t.styles.Style(tuicommon.RoleHelp).Width(t.width).Render("enter confirm  esc cancel"))
	return sb.String()
}

// ---------------------------------------------------------------------------
// inlineConfirm
// ---------------------------------------------------------------------------

// inlineConfirm renders a yes/no question overlay.
type inlineConfirm struct {
	q      interaction.Question
	cursor int // 0 = yes, 1 = no
	done   bool
	back   bool
	styles tuicommon.Theme
	width  int
}

func newInlineConfirm(q interaction.Question, styles tuicommon.Theme, width int) *inlineConfirm {
	return &inlineConfirm{q: q, cursor: 1, styles: styles, width: width}
}

func (c *inlineConfirm) update(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	switch keyMsg.String() {
	case "left", "h", "y", "Y":
		c.cursor = 0
	case "right", "l", "n", "N":
		c.cursor = 1
	case "enter":
		c.done = true
	case "esc":
		c.back = true
	}
	return c.done || c.back
}

func (c *inlineConfirm) answer() interaction.ConfirmAnswer {
	if c.back {
		return interaction.ConfirmAnswer{Status: interaction.Cancelled}
	}
	return interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: c.cursor == 0}
}

func (c *inlineConfirm) view() string {
	var sb strings.Builder
	sb.WriteString(c.styles.Style(tuicommon.RoleTitle).Width(c.width).Render(c.q.Title))
	sb.WriteByte('\n')
	if c.q.Prompt != "" {
		sb.WriteString(c.styles.Style(tuicommon.RoleBody).Width(c.width).Render(c.q.Prompt))
		sb.WriteByte('\n')
	}

	yes, no := "[ Yes ]", "[ No ]"
	if c.cursor == 0 {
		yes = c.styles.Style(tuicommon.RoleSelected).Render("[ Yes ]")
		no = c.styles.Style(tuicommon.RoleBody).Render("[ No ]")
	} else {
		yes = c.styles.Style(tuicommon.RoleBody).Render("[ Yes ]")
		no = c.styles.Style(tuicommon.RoleSelected).Render("[ No ]")
	}
	sb.WriteString("\n  " + yes + "  " + no + "\n")
	sb.WriteString(c.styles.Style(tuicommon.RoleHelp).Width(c.width).Render("←/h yes  →/l no  enter confirm  esc cancel"))
	return sb.String()
}
