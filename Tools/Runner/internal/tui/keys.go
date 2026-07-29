package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
)

// helpEntry is a single key + description pair shown in the help bar.
type helpEntry = tuicommon.HelpEntry

// keyMap holds the global key bindings that all screens share.
type keyMap = tuicommon.KeyMap

// globalKeys is the canonical key binding set. All screens reference this so the key
// scheme is discoverable and consistent with the deployment tool.
var globalKeys = tuicommon.GlobalKeys

// matchesKey reports whether the tea.KeyMsg matches any of the given bindings.
func matchesKey(msg tea.KeyMsg, bindings ...key.Binding) bool {
	return tuicommon.MatchesKey(msg, bindings...)
}

// entryScreenHelp returns the standard key hints shown at the bottom of entry screens.
func entryScreenHelp() []helpEntry {
	return tuicommon.EntryScreenHelp()
}

// multiSelectHelp returns the key hints for multi-select screens.
func multiSelectHelp() []helpEntry {
	return tuicommon.MultiSelectHelp()
}

// textInputHelp returns the key hints for text-input screens.
func textInputHelp() []helpEntry {
	return tuicommon.TextInputHelp()
}
