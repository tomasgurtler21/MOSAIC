package tui

import tuicommon "mosaic-common/tui"

// Type aliases for the shared TUI theme types. These keep all existing code in the
// deployment's tui package compiling without modification; the canonical definitions
// live in mosaic-common/tui.

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

type Theme = tuicommon.Theme

// DefaultTheme forwards to the shared default dark-terminal theme.
func DefaultTheme() Theme { return tuicommon.DefaultTheme() }
