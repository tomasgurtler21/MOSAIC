package tui

import tuicommon "mosaic-common/tui"

// StyleRole aliases the shared theme style role type.
type StyleRole = tuicommon.StyleRole

const (
	RoleTitle    = tuicommon.RoleTitle
	RoleSubtitle = tuicommon.RoleSubtitle
	RoleBody     = tuicommon.RoleBody
	RoleMuted    = tuicommon.RoleMuted
	RoleSelected = tuicommon.RoleSelected
	RoleChecked  = tuicommon.RoleChecked
	RoleSuccess  = tuicommon.RoleSuccess
	RoleWarning  = tuicommon.RoleWarning
	RoleError    = tuicommon.RoleError
	RoleHelp     = tuicommon.RoleHelp
	RoleBorder   = tuicommon.RoleBorder
)

// Theme aliases the shared TUI theme type.
type Theme = tuicommon.Theme

// DefaultTheme returns the default dark-terminal theme.
func DefaultTheme() Theme { return tuicommon.DefaultTheme() }
