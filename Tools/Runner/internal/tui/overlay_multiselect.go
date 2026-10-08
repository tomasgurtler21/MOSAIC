package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
	tuicommon "mosaic-common/tui"
	"mosaic-common/tui/widgets"
)

// minMultiSelectRows keeps the option list usable on very small terminals.
const minMultiSelectRows = 5

// inlineMultiSelect renders a select-many question overlay on top of the
// common MultiSelect widget: pre-checked defaults, toggling and free-form
// entries when the question allows them.
type inlineMultiSelect struct {
	q      interaction.ChoiceQuestion
	list   *widgets.MultiSelect
	styles tuicommon.Theme
	width  int
}

func newInlineMultiSelect(q interaction.ChoiceQuestion, styles tuicommon.Theme, width, height int) *inlineMultiSelect {
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
	rows := height - 6
	if rows < minMultiSelectRows {
		rows = minMultiSelectRows
	}
	list := widgets.NewMultiSelect(items, rows, width, multiSelectStyles(styles))
	if q.AllowCustom {
		list.EnableCustomEntry(q.CustomPrompt)
	}
	for _, item := range items {
		if !item.Disabled && containsID(q.DefaultOptionIDs, item.ID) {
			list.SetChecked(item.ID, true)
		}
	}
	return &inlineMultiSelect{q: q, list: list, styles: styles, width: width}
}

func multiSelectStyles(t tuicommon.Theme) widgets.MultiSelectStyles {
	s := widgets.DefaultMultiSelectStyles()
	s.Normal = t.Style(tuicommon.RoleBody)
	s.Selected = t.Style(tuicommon.RoleSelected)
	s.Checked = t.Style(tuicommon.RoleChecked)
	s.Disabled = t.Style(tuicommon.RoleMuted)
	return s
}

func containsID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func (s *inlineMultiSelect) init() tea.Cmd { return nil }

// update forwards msg to the list and reports whether the user finished,
// by confirming the selection or cancelling with Esc.
func (s *inlineMultiSelect) update(msg tea.Msg) bool {
	s.list.Update(msg)
	return s.list.Done() || s.list.Back()
}

func (s *inlineMultiSelect) answer() interaction.MultiChoiceAnswer {
	if s.list.Back() {
		return interaction.MultiChoiceAnswer{Status: interaction.Cancelled}
	}
	return interaction.MultiChoiceAnswer{
		Status:    interaction.Answered,
		OptionIDs: s.list.SelectedIDs(),
		Custom:    s.list.CustomEntries(),
	}
}

func (s *inlineMultiSelect) resize(width, height int) {
	s.width = width
	rows := height - 6
	if rows < minMultiSelectRows {
		rows = minMultiSelectRows
	}
	s.list.Resize(rows, width)
}

func (s *inlineMultiSelect) view() string {
	var sb strings.Builder
	sb.WriteString(s.styles.Style(tuicommon.RoleTitle).Width(s.width).Render(s.q.Title))
	sb.WriteByte('\n')
	if s.q.Prompt != "" {
		sb.WriteString(s.styles.Style(tuicommon.RoleBody).Width(s.width).Render(s.q.Prompt))
		sb.WriteByte('\n')
	}
	sb.WriteString(s.list.View())
	sb.WriteByte('\n')
	help := "up/down move  space toggle  enter confirm  esc cancel"
	if s.list.Editing() {
		help = "type an entry  enter add  esc discard"
	}
	sb.WriteString(s.styles.Style(tuicommon.RoleHelp).Width(s.width).Render(help))
	return sb.String()
}
