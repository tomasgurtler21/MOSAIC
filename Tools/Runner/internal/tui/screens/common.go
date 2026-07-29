// Package screens implements the TUI screens for the mosaic-run tool.
// Each screen is a self-contained model that signals completion (Done), back-navigation
// (Back), or cancellation via boolean flags checked by the root model after each Update.
//
// Screens import only domain types, shared widgets, and styling libraries.
// They never import the parent tui package.
package screens

import "github.com/charmbracelet/lipgloss"

// Styles is the resolved set of lipgloss styles passed to every screen constructor.
// It is populated by the root model from the active Theme so screens are style-agnostic.
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
