package widgets

import commonwidgets "mosaic-common/tui/widgets"

// Type aliases so all existing code referencing these types from this package compiles
// without modification.

type TextInputStyles = commonwidgets.TextInputStyles
type TextInput = commonwidgets.TextInput

// DefaultTextInputStyles forwards to the shared widget package.
func DefaultTextInputStyles() TextInputStyles { return commonwidgets.DefaultTextInputStyles() }

// NewTextInput forwards to the shared widget package.
func NewTextInput(label, placeholder string, width int, styles TextInputStyles) *TextInput {
	return commonwidgets.NewTextInput(label, placeholder, width, styles)
}
