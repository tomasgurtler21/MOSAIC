package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
)

// keyMap holds the global key bindings that all screens share.
type keyMap = tuicommon.KeyMap

// globalKeys is the canonical key binding set shared with mosaic-deploy and
// mosaic-run.
var globalKeys = tuicommon.GlobalKeys

// matchesKey reports whether the tea.KeyMsg matches any of the given bindings.
func matchesKey(msg tea.KeyMsg, bindings ...key.Binding) bool {
	return tuicommon.MatchesKey(msg, bindings...)
}

// entryScreenHelp returns the standard key hints for entry screens.
func entryScreenHelp() []helpEntry {
	return tuicommon.EntryScreenHelp()
}

// textInputHelp returns the key hints for text-input screens.
func textInputHelp() []helpEntry {
	return tuicommon.TextInputHelp()
}
