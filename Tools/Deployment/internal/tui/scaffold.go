package tui

import tuicommon "mosaic-common/tui"

// Layout constants forwarded from the shared TUI scaffold. The canonical definitions
// live in mosaic-common/tui; these make the existing deployment tui package compile
// without changes to files that reference the unexported names.

const (
	defaultWidth  = tuicommon.DefaultWidth
	defaultHeight = tuicommon.DefaultHeight

	titleHeight  = tuicommon.TitleHeight
	statusHeight = tuicommon.StatusHeight
	helpHeight   = tuicommon.HelpHeight
	borderHeight = tuicommon.BorderHeight
)

// renderTitle forwards to the shared scaffold.
func renderTitle(title, subtitle string, width int, styles Theme) string {
	return tuicommon.RenderTitle(title, subtitle, width, styles)
}

// renderHelp forwards to the shared scaffold.
func renderHelp(entries []helpEntry, width int, styles Theme) string {
	return tuicommon.RenderHelp(entries, width, styles)
}

// renderStatus forwards to the shared scaffold.
func renderStatus(msg string, isError bool, width int, styles Theme) string {
	return tuicommon.RenderStatus(msg, isError, width, styles)
}

// contentHeight forwards to the shared scaffold.
func contentHeight(totalHeight int, hasSubtitle bool) int {
	return tuicommon.ContentHeight(totalHeight, hasSubtitle)
}

// truncate forwards to the shared scaffold.
func truncate(s string, maxLen int) string {
	return tuicommon.Truncate(s, maxLen)
}
