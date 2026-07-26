package tui

import "github.com/charmbracelet/lipgloss"

// StyleRole identifies a visual role in the interface. Every screen uses these roles to look
// up styles from the active Theme, so a future alternate theme is a single value change rather
// than a code change.
type StyleRole string

const (
	RoleTitle    StyleRole = "title"    // screen and section headings
	RoleSubtitle StyleRole = "subtitle" // secondary headings
	RoleBody     StyleRole = "body"     // default body text
	RoleMuted    StyleRole = "muted"    // de-emphasised text (disabled, hints)
	RoleSelected StyleRole = "selected" // focused list row or active input field
	RoleChecked  StyleRole = "checked"  // toggled-on item in a multi-select
	RoleSuccess  StyleRole = "success"  // created/updated outcome indicators
	RoleWarning  StyleRole = "warning"  // gaps, skips, fallback location notices
	RoleError    StyleRole = "error"    // failures, invalid input feedback
	RoleHelp     StyleRole = "help"     // key-hint footer
	RoleBorder   StyleRole = "border"   // panel borders and separators
)

// Theme holds the resolved lipgloss styles for each visual role. It is a value type with no
// behaviour beyond style lookup, so a screen never constructs inline styles and an alternate
// theme is a single Options change.
type Theme struct {
	Styles map[StyleRole]lipgloss.Style
}

// Style returns the style for the given role. When the role is absent or the Styles map is
// nil, it returns a zero-value lipgloss.Style (a no-op that produces plain text). It never
// panics, so a partially-populated Theme is always safe to use.
func (t Theme) Style(role StyleRole) lipgloss.Style {
	if t.Styles == nil {
		return lipgloss.NewStyle()
	}
	s, ok := t.Styles[role]
	if !ok {
		return lipgloss.NewStyle()
	}
	return s
}

// DefaultTheme returns the built-in dark-terminal theme with every role populated. An Options{}
// with a zero Theme is valid: Run substitutes DefaultTheme().
func DefaultTheme() Theme {
	return Theme{
		Styles: map[StyleRole]lipgloss.Style{
			RoleTitle:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")),
			RoleSubtitle: lipgloss.NewStyle().Foreground(lipgloss.Color("14")),
			RoleBody:     lipgloss.NewStyle().Foreground(lipgloss.Color("15")),
			RoleMuted:    lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
			RoleSelected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")),
			RoleChecked:  lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
			RoleSuccess:  lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
			RoleWarning:  lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
			RoleError:    lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
			RoleHelp:     lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
			RoleBorder:   lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		},
	}
}
