// Package widgets provides the shared UI widget set used by every TUI screen.
// Widgets are style-agnostic: callers supply lipgloss.Style values through each widget's
// constructor so a future theme change requires no widget code change.
package widgets

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ListItem is one entry in a List. Label is displayed; ID is the stable identifier returned
// when the item is selected. Detail is shown in the detail pane when the list has one.
// Disabled items are rendered with the muted style and cannot be selected.
type ListItem struct {
	ID             string
	Label          string
	Description    string
	Detail         string
	Disabled       bool
	DisabledReason string
}

// ListStyles holds the resolved styles used by a List.
type ListStyles struct {
	Normal   lipgloss.Style
	Selected lipgloss.Style
	Disabled lipgloss.Style
	Cursor   string // character shown next to the selected row; default ">"
}

// DefaultListStyles returns a plain-text style set (no colours) that works without a real
// terminal. Callers replace individual fields with theme-derived styles.
func DefaultListStyles() ListStyles {
	return ListStyles{
		Normal:   lipgloss.NewStyle(),
		Selected: lipgloss.NewStyle().Bold(true),
		Disabled: lipgloss.NewStyle(),
		Cursor:   ">",
	}
}

// List is a keyboard-navigable single-select list. It never reads the terminal and maintains
// no async state, so it is safe to drive through Update/View in model tests.
type List struct {
	items    []ListItem
	cursor   int
	offset   int  // first visible index (viewport scrolling)
	height   int  // visible rows
	width    int  // render width
	done     bool // enter pressed on a non-disabled item
	back     bool // esc pressed
	styles   ListStyles
	upKey    key.Binding
	downKey  key.Binding
	selectKey key.Binding
	backKey  key.Binding
}

// NewList creates a List with the given items and visible height. Items are shown in the
// order they are provided; the cursor starts on the first non-disabled item.
func NewList(items []ListItem, height, width int, styles ListStyles) *List {
	l := &List{
		items:     items,
		height:    height,
		width:     width,
		styles:    styles,
		upKey:     key.NewBinding(key.WithKeys("up", "k")),
		downKey:   key.NewBinding(key.WithKeys("down", "j")),
		selectKey: key.NewBinding(key.WithKeys("enter")),
		backKey:   key.NewBinding(key.WithKeys("esc")),
	}
	// Advance cursor to first enabled item.
	for i, item := range items {
		if !item.Disabled {
			l.cursor = i
			break
		}
	}
	return l
}

// Update processes a tea.KeyMsg and returns a command. Other message types are ignored.
func (l *List) Update(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(keyMsg, l.upKey):
		l.moveCursor(-1)
	case key.Matches(keyMsg, l.downKey):
		l.moveCursor(1)
	case key.Matches(keyMsg, l.selectKey):
		if l.cursor >= 0 && l.cursor < len(l.items) && !l.items[l.cursor].Disabled {
			l.done = true
		}
	case key.Matches(keyMsg, l.backKey):
		l.back = true
	}
	return nil
}

// moveCursor moves the cursor by delta, skipping disabled items, and adjusts the viewport.
func (l *List) moveCursor(delta int) {
	next := l.cursor + delta
	for next >= 0 && next < len(l.items) {
		if !l.items[next].Disabled {
			l.cursor = next
			l.adjustViewport()
			return
		}
		next += delta
	}
}

// adjustViewport scrolls the viewport so the cursor is always visible.
func (l *List) adjustViewport() {
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+l.height {
		l.offset = l.cursor - l.height + 1
	}
}

// View renders the visible slice of items.
func (l *List) View() string {
	if len(l.items) == 0 {
		return l.styles.Disabled.Width(l.width).Render("(no items)")
	}

	var sb strings.Builder
	end := l.offset + l.height
	if end > len(l.items) {
		end = len(l.items)
	}
	cursor := l.styles.Cursor
	if cursor == "" {
		cursor = ">"
	}
	cursorWidth := len([]rune(cursor)) + 1 // cursor + space

	for i := l.offset; i < end; i++ {
		item := l.items[i]
		prefix := strings.Repeat(" ", cursorWidth)
		if i == l.cursor {
			prefix = cursor + " "
		}

		label := item.Label
		if item.Disabled && item.DisabledReason != "" {
			label = label + " (" + item.DisabledReason + ")"
		}
		labelWidth := l.width - cursorWidth
		if labelWidth < 1 {
			labelWidth = 1
		}

		var line string
		if item.Disabled {
			line = prefix + l.styles.Disabled.Width(labelWidth).Render(label)
		} else if i == l.cursor {
			line = prefix + l.styles.Selected.Width(labelWidth).Render(label)
		} else {
			line = prefix + l.styles.Normal.Width(labelWidth).Render(label)
		}

		if i > l.offset {
			sb.WriteByte('\n')
		}
		sb.WriteString(line)
	}
	return sb.String()
}

// Done reports whether the user confirmed a selection.
func (l *List) Done() bool { return l.done }

// Back reports whether the user pressed back/cancel on this widget.
func (l *List) Back() bool { return l.back }

// SelectedID returns the ID of the currently highlighted item. Call Done() first to confirm
// the user has made a deliberate selection.
func (l *List) SelectedID() string {
	if l.cursor >= 0 && l.cursor < len(l.items) {
		return l.items[l.cursor].ID
	}
	return ""
}

// SelectedItem returns the full ListItem at the current cursor position.
func (l *List) SelectedItem() (ListItem, bool) {
	if l.cursor >= 0 && l.cursor < len(l.items) {
		return l.items[l.cursor], true
	}
	return ListItem{}, false
}

// CursorIndex returns the current cursor position (0-based).
func (l *List) CursorIndex() int { return l.cursor }

// SetCursor moves the cursor to the item with the given ID. If no item matches, the cursor
// is unchanged.
func (l *List) SetCursor(id string) {
	for i, item := range l.items {
		if item.ID == id {
			l.cursor = i
			l.adjustViewport()
			return
		}
	}
}

// Reset clears the done and back flags, allowing the same List to be reused.
func (l *List) Reset() {
	l.done = false
	l.back = false
}

// Resize updates the visible height and render width.
func (l *List) Resize(height, width int) {
	l.height = height
	l.width = width
	l.adjustViewport()
}

// Items returns the list items (read-only view).
func (l *List) Items() []ListItem { return l.items }
