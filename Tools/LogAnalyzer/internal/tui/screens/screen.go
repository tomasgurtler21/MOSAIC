// Package screens implements the TUI screens for the mosaic-log-analyzer tool.
// Each screen is a self-contained model that satisfies the Screen interface.
// Screens import only domain/app types, shared widgets, and styling libraries.
// They never import the parent tui package.
package screens

import (
	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
)

// ScreenID names each screen for navigation and testing.
type ScreenID string

const (
	ScreenSource    ScreenID = "source"
	ScreenRuns      ScreenID = "runs"
	ScreenRunDetail ScreenID = "run-detail"
	ScreenPricing   ScreenID = "pricing"
	ScreenNoData    ScreenID = "no-data"
)

// Screen is the contract every screen satisfies so the top-level model can
// route uniformly.
type Screen interface {
	ID() ScreenID
	Init() tea.Cmd
	Update(msg tea.Msg) (Screen, tea.Cmd)
	View(width, height int) string
	// Help returns the key hints rendered by the shared scaffold.
	Help() string
}

// Styles is the resolved set of lipgloss styles passed to every screen
// constructor. It is populated by the root model from the active Theme so
// screens are style-agnostic.
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
