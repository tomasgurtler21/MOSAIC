// Package screens implements self-contained TUI setting screens for the
// AgentTest run-configuration flow. Each screen is a standalone model that
// signals completion (Done), back-navigation (Back) via boolean flags checked
// by the root model after each Update call.
//
// Import boundary: screens import only domain (for data types),
// mosaic-common/tui (for shared key bindings and theme constants),
// mosaic-common/tui/widgets (if widget helpers are needed), and third-party
// styling libraries. Screens NEVER import the parent tui package
// (mosaic-agent-test/internal/tui). There is no AgentTest-local widgets
// package; all widget imports come from the mosaic-common module.
package screens

import "github.com/charmbracelet/lipgloss"

// Styles is the resolved set of lipgloss styles passed to every screen
// constructor. It is populated by the root model from the active Theme so
// screens are style-agnostic. The zero value produces plain unstyled text,
// which is convenient for headless tests.
type Styles struct {
	Title    lipgloss.Style
	Body     lipgloss.Style
	Muted    lipgloss.Style
	Selected lipgloss.Style
	Help     lipgloss.Style
}
