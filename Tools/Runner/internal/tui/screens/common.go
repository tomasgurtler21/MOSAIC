// Package screens implements the TUI screens for the mosaic-run tool.
// Each screen is a self-contained model that signals completion (Done), back-navigation
// (Back), or cancellation via boolean flags checked by the root model after each Update.
//
// Screens import only domain types, shared widgets, and styling libraries.
// They never import the parent tui package.
package screens

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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

// NormalizePath applies the standard normalisation rules to a raw filesystem
// path entered in the TUI:
//  1. Trim surrounding whitespace (including trailing newlines from paste).
//  2. If the result is at least two characters long and begins and ends with
//     a double-quote character ("), remove that matched pair.
//  3. No further processing -- interior quotes, separators, etc. are left intact.
//
// Single-quote stripping is deliberately omitted to align with Deployment's
// pathinput.Unquote convention (double-quote-only).
func NormalizePath(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return s
}
