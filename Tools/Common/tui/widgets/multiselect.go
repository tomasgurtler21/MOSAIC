package widgets

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MultiSelectStyles holds the resolved styles used by a MultiSelect.
type MultiSelectStyles struct {
	Normal   lipgloss.Style
	Selected lipgloss.Style // cursor row
	Checked  lipgloss.Style // toggled-on item
	Disabled lipgloss.Style
	Cursor   string // default ">"
	CheckOn  string // checked indicator; default "[x]"
	CheckOff string // unchecked indicator; default "[ ]"
}

// DefaultMultiSelectStyles returns a plain-text style set usable in tests.
func DefaultMultiSelectStyles() MultiSelectStyles {
	return MultiSelectStyles{
		Normal:   lipgloss.NewStyle(),
		Selected: lipgloss.NewStyle().Bold(true),
		Checked:  lipgloss.NewStyle().Bold(true),
		Disabled: lipgloss.NewStyle(),
		Cursor:   ">",
		CheckOn:  "[x]",
		CheckOff: "[ ]",
	}
}

// MultiSelect is a keyboard-navigable multi-select list. Space toggles the item under the
// cursor; Enter confirms the current set of checked items.
type MultiSelect struct {
	items      []ListItem
	checked    map[string]bool // ID -> toggled on
	cursor     int
	offset     int
	height     int
	width      int
	done       bool
	back       bool
	styles     MultiSelectStyles
	upKey      key.Binding
	downKey    key.Binding
	toggleKey  key.Binding
	confirmKey key.Binding
	backKey    key.Binding
}

// NewMultiSelect creates a MultiSelect with the given items.
func NewMultiSelect(items []ListItem, height, width int, styles MultiSelectStyles) *MultiSelect {
	m := &MultiSelect{
		items:      items,
		checked:    make(map[string]bool),
		height:     height,
		width:      width,
		styles:     styles,
		upKey:      key.NewBinding(key.WithKeys("up", "k")),
		downKey:    key.NewBinding(key.WithKeys("down", "j")),
		toggleKey:  key.NewBinding(key.WithKeys(" ")),
		confirmKey: key.NewBinding(key.WithKeys("enter")),
		backKey:    key.NewBinding(key.WithKeys("esc")),
	}
	// Advance cursor to first enabled item.
	for i, item := range items {
		if !item.Disabled {
			m.cursor = i
			break
		}
	}
	return m
}

// Update processes a tea.KeyMsg. Other message types are ignored.
func (m *MultiSelect) Update(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(keyMsg, m.upKey):
		m.moveCursor(-1)
	case key.Matches(keyMsg, m.downKey):
		m.moveCursor(1)
	case key.Matches(keyMsg, m.toggleKey):
		m.toggle()
	case key.Matches(keyMsg, m.confirmKey):
		m.done = true
	case key.Matches(keyMsg, m.backKey):
		m.back = true
	}
	return nil
}

func (m *MultiSelect) moveCursor(delta int) {
	next := m.cursor + delta
	for next >= 0 && next < len(m.items) {
		if !m.items[next].Disabled {
			m.cursor = next
			m.adjustViewport()
			return
		}
		next += delta
	}
}

func (m *MultiSelect) toggle() {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		item := m.items[m.cursor]
		if !item.Disabled {
			m.checked[item.ID] = !m.checked[item.ID]
		}
	}
}

func (m *MultiSelect) adjustViewport() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.height {
		m.offset = m.cursor - m.height + 1
	}
}

// View renders the visible slice of items with check indicators.
func (m *MultiSelect) View() string {
	if len(m.items) == 0 {
		return m.styles.Disabled.Width(m.width).Render("(no items)")
	}

	checkOn := m.styles.CheckOn
	if checkOn == "" {
		checkOn = "[x]"
	}
	checkOff := m.styles.CheckOff
	if checkOff == "" {
		checkOff = "[ ]"
	}
	cursor := m.styles.Cursor
	if cursor == "" {
		cursor = ">"
	}
	cursorWidth := len([]rune(cursor)) + 1
	checkWidth := len([]rune(checkOn)) + 1

	var sb strings.Builder
	end := m.offset + m.height
	if end > len(m.items) {
		end = len(m.items)
	}

	for i := m.offset; i < end; i++ {
		item := m.items[i]
		prefix := strings.Repeat(" ", cursorWidth)
		if i == m.cursor {
			prefix = cursor + " "
		}

		check := checkOff
		if m.checked[item.ID] {
			check = checkOn
		}

		labelWidth := m.width - cursorWidth - checkWidth
		if labelWidth < 1 {
			labelWidth = 1
		}

		var line string
		switch {
		case item.Disabled:
			line = prefix + checkOff + " " + m.styles.Disabled.Width(labelWidth).Render(item.Label)
		case m.checked[item.ID] && i == m.cursor:
			line = prefix + check + " " + m.styles.Checked.Width(labelWidth).Render(item.Label)
		case m.checked[item.ID]:
			line = prefix + check + " " + m.styles.Checked.Width(labelWidth).Render(item.Label)
		case i == m.cursor:
			line = prefix + check + " " + m.styles.Selected.Width(labelWidth).Render(item.Label)
		default:
			line = prefix + check + " " + m.styles.Normal.Width(labelWidth).Render(item.Label)
		}

		if i > m.offset {
			sb.WriteByte('\n')
		}
		sb.WriteString(line)
	}
	return sb.String()
}

// Done reports whether the user confirmed the selection.
func (m *MultiSelect) Done() bool { return m.done }

// Back reports whether the user pressed back.
func (m *MultiSelect) Back() bool { return m.back }

// SelectedIDs returns the IDs of all toggled-on items, in declaration order.
func (m *MultiSelect) SelectedIDs() []string {
	var ids []string
	for _, item := range m.items {
		if m.checked[item.ID] {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

// IsChecked reports whether the item with the given ID is currently checked.
func (m *MultiSelect) IsChecked(id string) bool { return m.checked[id] }

// SetChecked sets the checked state for the item with the given ID.
func (m *MultiSelect) SetChecked(id string, checked bool) { m.checked[id] = checked }

// Reset clears the done and back flags.
func (m *MultiSelect) Reset() {
	m.done = false
	m.back = false
}

// Resize updates the visible height and render width.
func (m *MultiSelect) Resize(height, width int) {
	m.height = height
	m.width = width
	m.adjustViewport()
}

// CursorIndex returns the current cursor position.
func (m *MultiSelect) CursorIndex() int { return m.cursor }
