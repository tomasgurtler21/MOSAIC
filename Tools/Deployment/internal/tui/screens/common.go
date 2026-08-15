// Package screens implements the three TUI entry screens: harness selection, mode selection,
// and workspace path entry. Each screen is a self-contained model that signals completion
// (Done), back-navigation (Back), or cancellation (Cancelled) via boolean flags checked by
// the root model after each Update call. The root model drives these screens without any
// real terminal; the model/update/view cycle runs headlessly in tests.
//
// Screens import only domain (for data types), tui/widgets (for shared widgets), and
// third-party styling libraries. They never import the parent tui package.
package screens

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// formatFollowUpLine composes the complete bullet text for one follow-up line, including the
// leading "  • " marker, and appends the owning agent when that owner is not already named in
// the line's own text.
//
// lead   — the primary token shown first: TodoItem.Subject on the summary screen,
//
//	string(Gap.Kind) on the review screen.
//
// detail — the secondary token: TodoItem.Detail on the summary screen, Gap.Subject on the
//
//	review screen. May be empty.
//
// owner  — TodoItem.Owner / Gap.Owner. Empty when the item is run-level rather than
//
//	artifact-level.
func formatFollowUpLine(lead, detail, owner string) string {
	base := lead
	if detail != "" {
		base = lead + ": " + detail
	}
	if owner != "" && !strings.Contains(base, owner) {
		base = base + " (agent: " + owner + ")"
	}
	return "  • " + base
}

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
