package widgets

import commonwidgets "mosaic-common/tui/widgets"

// Type aliases so all existing code referencing these types from this package compiles
// without modification.

type MultiSelectStyles = commonwidgets.MultiSelectStyles
type MultiSelect = commonwidgets.MultiSelect

// DefaultMultiSelectStyles forwards to the shared widget package.
func DefaultMultiSelectStyles() MultiSelectStyles { return commonwidgets.DefaultMultiSelectStyles() }

// NewMultiSelect forwards to the shared widget package.
func NewMultiSelect(items []ListItem, height, width int, styles MultiSelectStyles) *MultiSelect {
	return commonwidgets.NewMultiSelect(items, height, width, styles)
}
