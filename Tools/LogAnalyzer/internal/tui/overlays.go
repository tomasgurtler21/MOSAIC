package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-common/interaction"
)

// ---------------------------------------------------------------------------
// overlaySelectOne — inline select-one question
// ---------------------------------------------------------------------------

// overlaySelectOne renders a simple interactive select-one question overlay
// on top of the current screen.
type overlaySelectOne struct {
	q      interaction.ChoiceQuestion
	cursor int
	done   bool
	back   bool
	theme  tuicommon.Theme
	width  int
	height int
}

func newOverlaySelectOne(q interaction.ChoiceQuestion, theme tuicommon.Theme, width, height int) *overlaySelectOne {
	return &overlaySelectOne{q: q, theme: theme, width: width, height: height}
}

// totalOptions returns the number of options including the optional skip entry.
func (o *overlaySelectOne) totalOptions() int {
	if o.q.AllowSkip {
		return len(o.q.Options) + 1
	}
	return len(o.q.Options)
}

// isSkipEntry reports whether idx refers to the synthetic skip entry.
func (o *overlaySelectOne) isSkipEntry(idx int) bool {
	return o.q.AllowSkip && idx == len(o.q.Options)
}

// update processes a key message. Returns true when the overlay is done.
func (o *overlaySelectOne) update(msg tea.Msg) bool {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	switch key.String() {
	case "up", "k":
		if o.cursor > 0 {
			o.cursor--
		}
	case "down", "j":
		if o.cursor < o.totalOptions()-1 {
			o.cursor++
		}
	case "enter":
		if o.isSkipEntry(o.cursor) {
			o.done = true
		} else if o.cursor >= 0 && o.cursor < len(o.q.Options) {
			if !o.q.Options[o.cursor].Disabled {
				o.done = true
			}
		}
	case "esc":
		o.back = true
	}
	return o.done || o.back
}

// answer constructs the ChoiceAnswer from the current overlay state.
func (o *overlaySelectOne) answer() interaction.ChoiceAnswer {
	if o.back {
		return interaction.ChoiceAnswer{Status: interaction.Cancelled}
	}
	if o.isSkipEntry(o.cursor) {
		return interaction.ChoiceAnswer{Status: interaction.SkippedOne}
	}
	if o.cursor >= 0 && o.cursor < len(o.q.Options) {
		return interaction.ChoiceAnswer{
			Status:   interaction.Answered,
			OptionID: o.q.Options[o.cursor].ID,
		}
	}
	return interaction.ChoiceAnswer{Status: interaction.SkippedOne}
}

// view renders the select overlay.
func (o *overlaySelectOne) view() string {
	var sb strings.Builder
	sb.WriteString(o.theme.Style(tuicommon.RoleTitle).Width(o.width).Render(o.q.Title))
	sb.WriteByte('\n')
	if o.q.Prompt != "" {
		sb.WriteString(o.theme.Style(tuicommon.RoleBody).Width(o.width).Render(o.q.Prompt))
		sb.WriteByte('\n')
	}

	for i, opt := range o.q.Options {
		cursor := "  "
		style := o.theme.Style(tuicommon.RoleBody)
		if i == o.cursor {
			cursor = "> "
			style = o.theme.Style(tuicommon.RoleSelected)
		}
		if opt.Disabled {
			style = o.theme.Style(tuicommon.RoleMuted)
		}
		line := cursor + opt.Label
		if opt.Description != "" {
			line += "  " + opt.Description
		}
		sb.WriteString(style.Width(o.width).Render(line))
		sb.WriteByte('\n')
	}

	if o.q.AllowSkip {
		cursor := "  "
		style := o.theme.Style(tuicommon.RoleBody)
		if o.isSkipEntry(o.cursor) {
			cursor = "> "
			style = o.theme.Style(tuicommon.RoleSelected)
		}
		sb.WriteString(style.Width(o.width).Render(cursor + "Skip"))
		sb.WriteByte('\n')
	}

	sb.WriteString(o.theme.Style(tuicommon.RoleHelp).Width(o.width).Render(
		"↑/↓ navigate  enter select  esc cancel  ctrl+c quit",
	))
	return sb.String()
}

// ---------------------------------------------------------------------------
// overlayText — inline text-input question
// ---------------------------------------------------------------------------

// overlayText renders a simple text-input question overlay.
type overlayText struct {
	q      interaction.TextQuestion
	input  textinput.Model
	errMsg string
	done   bool
	back   bool
	theme  tuicommon.Theme
	width  int
	height int
}

func newOverlayText(q interaction.TextQuestion, theme tuicommon.Theme, width, height int) *overlayText {
	m := textinput.New()
	if q.Default != "" {
		m.SetValue(q.Default)
	}
	m.Width = width - 4
	m.Focus()
	return &overlayText{
		q:     q,
		input: m,
		theme: theme,
		width: width,
		height: height,
	}
}

// init returns the cursor blink command for the text input.
func (o *overlayText) init() tea.Cmd { return textinput.Blink }

// update processes a message. Returns a Cmd for cursor blinking.
func (o *overlayText) update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyEnter:
			val := o.input.Value()
			if o.q.Validate != nil {
				if err := o.q.Validate(val); err != nil {
					o.errMsg = err.Error()
					return nil
				}
			}
			o.errMsg = ""
			o.done = true
			return nil
		case tea.KeyEsc:
			o.back = true
			return nil
		}
	}
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(msg)
	return cmd
}

// answer constructs the TextAnswer from the current overlay state.
func (o *overlayText) answer() interaction.TextAnswer {
	if o.back {
		return interaction.TextAnswer{Status: interaction.Cancelled}
	}
	return interaction.TextAnswer{Status: interaction.Answered, Text: o.input.Value()}
}

// view renders the text-input overlay.
func (o *overlayText) view() string {
	var sb strings.Builder
	sb.WriteString(o.theme.Style(tuicommon.RoleTitle).Width(o.width).Render(o.q.Title))
	sb.WriteByte('\n')
	if o.q.Prompt != "" {
		sb.WriteString(o.theme.Style(tuicommon.RoleBody).Width(o.width).Render(o.q.Prompt))
		sb.WriteByte('\n')
	}
	if o.q.Detail != "" {
		sb.WriteString(o.theme.Style(tuicommon.RoleMuted).Width(o.width).Render(o.q.Detail))
		sb.WriteByte('\n')
	}
	sb.WriteString(o.input.View())
	sb.WriteByte('\n')
	if o.errMsg != "" {
		sb.WriteString(o.theme.Style(tuicommon.RoleError).Width(o.width).Render(o.errMsg))
		sb.WriteByte('\n')
	}
	sb.WriteString(o.theme.Style(tuicommon.RoleHelp).Width(o.width).Render(
		"enter confirm  esc cancel  ctrl+c quit",
	))
	return sb.String()
}
