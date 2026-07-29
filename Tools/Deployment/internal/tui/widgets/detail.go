package widgets

import commonwidgets "mosaic-common/tui/widgets"

// Type aliases so all existing code referencing these types from this package compiles
// without modification.

type DetailPaneStyles = commonwidgets.DetailPaneStyles
type DetailPane = commonwidgets.DetailPane

// DefaultDetailPaneStyles forwards to the shared widget package.
func DefaultDetailPaneStyles() DetailPaneStyles { return commonwidgets.DefaultDetailPaneStyles() }

// NewDetailPane forwards to the shared widget package.
func NewDetailPane(height, width int, styles DetailPaneStyles) *DetailPane {
	return commonwidgets.NewDetailPane(height, width, styles)
}
