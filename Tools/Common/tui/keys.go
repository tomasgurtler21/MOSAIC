package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// HelpEntry is a single key + description pair shown in the help bar.
type HelpEntry struct {
	Key  string
	Desc string
}

// KeyMap holds the global key bindings that all screens share.
type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Back   key.Binding
	Cancel key.Binding
	Space  key.Binding
	Help   key.Binding
}

// GlobalKeys is the canonical key binding set. All screens reference this and render their
// help bar from it so the key scheme is discoverable and consistent.
var GlobalKeys = KeyMap{
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

// MatchesKey reports whether the tea.KeyMsg matches any of the given bindings.
func MatchesKey(msg tea.KeyMsg, bindings ...key.Binding) bool {
	return key.Matches(msg, bindings...)
}

// EntryScreenHelp returns the standard key hints shown at the bottom of entry screens.
func EntryScreenHelp() []HelpEntry {
	return []HelpEntry{
		{GlobalKeys.Up.Help().Key, GlobalKeys.Up.Help().Desc},
		{GlobalKeys.Down.Help().Key, GlobalKeys.Down.Help().Desc},
		{GlobalKeys.Select.Help().Key, GlobalKeys.Select.Help().Desc},
		{GlobalKeys.Back.Help().Key, GlobalKeys.Back.Help().Desc},
		{GlobalKeys.Cancel.Help().Key, GlobalKeys.Cancel.Help().Desc},
	}
}

// MultiSelectHelp returns the key hints for multi-select screens.
func MultiSelectHelp() []HelpEntry {
	return []HelpEntry{
		{GlobalKeys.Up.Help().Key, GlobalKeys.Up.Help().Desc},
		{GlobalKeys.Down.Help().Key, GlobalKeys.Down.Help().Desc},
		{GlobalKeys.Space.Help().Key, GlobalKeys.Space.Help().Desc},
		{GlobalKeys.Select.Help().Key, "confirm"},
		{GlobalKeys.Back.Help().Key, GlobalKeys.Back.Help().Desc},
		{GlobalKeys.Cancel.Help().Key, GlobalKeys.Cancel.Help().Desc},
	}
}

// TextInputHelp returns the key hints for text-input screens.
func TextInputHelp() []HelpEntry {
	return []HelpEntry{
		{GlobalKeys.Select.Help().Key, "confirm"},
		{GlobalKeys.Back.Help().Key, GlobalKeys.Back.Help().Desc},
		{GlobalKeys.Cancel.Help().Key, GlobalKeys.Cancel.Help().Desc},
	}
}
