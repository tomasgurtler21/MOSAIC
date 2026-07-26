// Package screens implements the three TUI entry screens: harness selection, mode selection,
// and workspace path entry. Each screen is a self-contained model that signals completion
// (Done), back-navigation (Back), or cancellation (Cancelled) via boolean flags checked by
// the root model after each Update call. The root model drives these screens without any
// real terminal; the model/update/view cycle runs headlessly in tests.
//
// Screens import only domain (for data types), tui/widgets (for shared widgets), and
// third-party styling libraries. They never import the parent tui package.
package screens

import "github.com/charmbracelet/lipgloss"

// Styles is the resolved set of lipgloss styles passed to every screen constructor. It is
// populated by the root model from the active Theme so screens are style-agnostic.
type Styles struct {
	Title    lipgloss.Style
	Subtitle lipgloss.Style
	Body     lipgloss.Style
	Muted    lipgloss.Style
	Selected lipgloss.Style
	Checked  lipgloss.Style
	Success  lipgloss.Style
	Warning  lipgloss.Style
	Error    lipgloss.Style
	Help     lipgloss.Style
	Border   lipgloss.Style
}
