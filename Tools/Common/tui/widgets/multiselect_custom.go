package widgets

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/pastesafe"
)

// customEntry is one free-form row added by the user.
type customEntry struct {
	text    string
	checked bool
}

// customState holds the opt-in free-form entry state of a MultiSelect.
type customState struct {
	prompt  string
	entries []customEntry
	field   *pastesafe.Field // non-nil while the entry field is open
}

// EnableCustomEntry adds an "add entry" row labelled with prompt and enables
// custom entry rows. Calling it again only replaces the label.
func (m *MultiSelect) EnableCustomEntry(prompt string) {
	if m.custom != nil {
		m.custom.prompt = prompt
		return
	}
	m.custom = &customState{prompt: prompt}
	if m.cursor >= len(m.items) || m.items[m.cursor].Disabled {
		m.cursor = m.addRow()
	}
}

// CustomEntries returns the checked custom entries in the order they were
// first added (unchecked custom entries are omitted). Nil when none.
func (m *MultiSelect) CustomEntries() []string {
	if m.custom == nil {
		return nil
	}
	var out []string
	for _, e := range m.custom.entries {
		if e.checked {
			out = append(out, e.text)
		}
	}
	return out
}

// Editing reports whether the custom entry field is open. While it is open,
// every key goes to the field (parents must not intercept keys).
func (m *MultiSelect) Editing() bool { return m.custom != nil && m.custom.field != nil }

// rowCount is the number of navigable rows: items, custom entries, add-entry row.
func (m *MultiSelect) rowCount() int {
	if m.custom == nil {
		return len(m.items)
	}
	return len(m.items) + len(m.custom.entries) + 1
}

// addRow is the index of the add-entry row (only meaningful with custom entry enabled).
func (m *MultiSelect) addRow() int {
	if m.custom == nil {
		return -1
	}
	return len(m.items) + len(m.custom.entries)
}

func (m *MultiSelect) rowDisabled(i int) bool {
	return i < len(m.items) && m.items[i].Disabled
}

// toggleCustomRow acts on a cursor row past the items: the add-entry row opens
// the field, a custom row flips its checked state.
func (m *MultiSelect) toggleCustomRow() {
	if m.custom == nil {
		return
	}
	if m.cursor == m.addRow() {
		m.custom.field = pastesafe.NewField(pastesafe.WithPlaceholder(m.custom.prompt))
		return
	}
	idx := m.cursor - len(m.items)
	if idx >= 0 && idx < len(m.custom.entries) {
		m.custom.entries[idx].checked = !m.custom.entries[idx].checked
	}
}

// updateEditing routes a message to the open entry field and applies or
// discards the entry when the field finishes.
func (m *MultiSelect) updateEditing(msg tea.Msg) tea.Cmd {
	f := m.custom.field
	cmd := f.Update(msg)
	switch {
	case f.Back():
		m.custom.field = nil
	case f.Done():
		m.custom.field = nil
		m.applyEntry(f.Value())
		m.cursor = m.addRow()
	}
	return cmd
}

// applyEntry adds text as a checked custom row. An entry naming an item ID
// checks that item instead; a repeated entry is checked but not duplicated.
func (m *MultiSelect) applyEntry(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	for _, item := range m.items {
		if item.ID == text && !item.Disabled {
			m.checked[item.ID] = true
			return
		}
	}
	for i := range m.custom.entries {
		if m.custom.entries[i].text == text {
			m.custom.entries[i].checked = true
			return
		}
	}
	m.custom.entries = append(m.custom.entries, customEntry{text: text, checked: true})
}

// customRowLabel returns the label and checked state of a row past the items.
func (m *MultiSelect) customRowLabel(i int) (label string, checked, isAdd bool) {
	if i == m.addRow() {
		label = "+ " + m.custom.prompt
		if m.Editing() {
			label = m.custom.prompt + ": " + m.custom.field.View()
		}
		return label, false, true
	}
	e := m.custom.entries[i-len(m.items)]
	return e.text, e.checked, false
}
