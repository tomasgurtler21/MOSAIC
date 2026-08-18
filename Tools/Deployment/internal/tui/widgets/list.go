// Package widgets provides the shared UI widget set used by every TUI screen.
// The canonical implementations live in mosaic-common/tui/widgets; this package
// re-exports them so existing deployment import paths continue to work.
package widgets

import commonwidgets "mosaic-common/tui/widgets"

// Type aliases so all existing code referencing these types from this package compiles
// without modification.

type ListItem = commonwidgets.ListItem
type ListStyles = commonwidgets.ListStyles
type List = commonwidgets.List

// DefaultListStyles forwards to the shared widget package.
func DefaultListStyles() ListStyles { return commonwidgets.DefaultListStyles() }

// NewList forwards to the shared widget package.
func NewList(items []ListItem, height, width int, styles ListStyles) *List {
	return commonwidgets.NewList(items, height, width, styles)
}
