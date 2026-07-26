package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// keyMap holds the global key bindings that all screens share.
type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Back   key.Binding
	Cancel key.Binding
	Space  key.Binding
	Help   key.Binding
}

// globalKeys is the canonical key binding set. All screens reference this and render their
// help bar from it so the key scheme is discoverable and consistent.
var globalKeys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Select: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Space: key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("space", "toggle"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
}

// matchesKey reports whether the tea.KeyMsg matches any of the given bindings.
func matchesKey(msg tea.KeyMsg, bindings ...key.Binding) bool {
	return key.Matches(msg, bindings...)
}

// entryScreenHelp returns the standard key hints shown at the bottom of entry screens.
func entryScreenHelp() []helpEntry {
	return []helpEntry{
		{globalKeys.Up.Help().Key, globalKeys.Up.Help().Desc},
		{globalKeys.Down.Help().Key, globalKeys.Down.Help().Desc},
		{globalKeys.Select.Help().Key, globalKeys.Select.Help().Desc},
		{globalKeys.Back.Help().Key, globalKeys.Back.Help().Desc},
		{globalKeys.Cancel.Help().Key, globalKeys.Cancel.Help().Desc},
	}
}

// multiSelectHelp returns the key hints for multi-select screens.
func multiSelectHelp() []helpEntry {
	return []helpEntry{
		{globalKeys.Up.Help().Key, globalKeys.Up.Help().Desc},
		{globalKeys.Down.Help().Key, globalKeys.Down.Help().Desc},
		{globalKeys.Space.Help().Key, globalKeys.Space.Help().Desc},
		{globalKeys.Select.Help().Key, "confirm"},
		{globalKeys.Back.Help().Key, globalKeys.Back.Help().Desc},
		{globalKeys.Cancel.Help().Key, globalKeys.Cancel.Help().Desc},
	}
}

// textInputHelp returns the key hints for text-input screens.
func textInputHelp() []helpEntry {
	return []helpEntry{
		{globalKeys.Select.Help().Key, "confirm"},
		{globalKeys.Back.Help().Key, globalKeys.Back.Help().Desc},
		{globalKeys.Cancel.Help().Key, globalKeys.Cancel.Help().Desc},
	}
}

// helpEntry is a single key + description pair shown in the help bar.
type helpEntry struct {
	Key  string
	Desc string
}
