package tui

import (
	"strings"
)

// Layout constants shared by every screen.
const (
	DefaultWidth  = 80
	DefaultHeight = 24

	TitleHeight  = 3
	StatusHeight = 1
	HelpHeight   = 1
	BorderHeight = 1
)

// RenderTitle renders the screen title bar. It pads to the full width and draws a thin border
// under the title so the content area is visually separated.
func RenderTitle(title, subtitle string, width int, styles Theme) string {
	titleLine := styles.Style(RoleTitle).Width(width).Render(title)
	subtitleLine := ""
	if subtitle != "" {
		subtitleLine = "\n" + styles.Style(RoleSubtitle).Width(width).Render(subtitle)
	}
	border := styles.Style(RoleBorder).Width(width).Render(strings.Repeat("─", width))
	if subtitle != "" {
		return titleLine + subtitleLine + "\n" + border
	}
	return titleLine + "\n" + border
}

// RenderHelp renders the key-hint bar at the bottom of the screen from a slice of HelpEntry
// values. Entries are joined with " · " as a separator. The result is styled as RoleHelp and
// padded to the full width.
func RenderHelp(entries []HelpEntry, width int, styles Theme) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, e.Key+" "+e.Desc)
	}
	text := strings.Join(parts, "  ·  ")
	return styles.Style(RoleHelp).Width(width).Render(text)
}

// RenderStatus renders the single-line status/error bar just above the help bar. An empty
// message renders as a blank line so layout dimensions are stable.
func RenderStatus(msg string, isError bool, width int, styles Theme) string {
	if msg == "" {
		return strings.Repeat(" ", width)
	}
	role := RoleMuted
	if isError {
		role = RoleError
	}
	return styles.Style(role).Width(width).Render(msg)
}

// ContentHeight calculates the available height for the content area after title, status, and
// help bar have been reserved. Accounts for a two-line title (title + border) when subtitle
// is present.
func ContentHeight(totalHeight int, hasSubtitle bool) int {
	reserved := 2 + StatusHeight + HelpHeight // title line + border + status + help
	if hasSubtitle {
		reserved++ // subtitle line
	}
	h := totalHeight - reserved
	if h < 1 {
		return 1
	}
	return h
}

// Truncate truncates s to at most maxLen runes, appending "…" when truncation occurs.
func Truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	return string(runes[:maxLen-1]) + "…"
}

// Wrap wraps s to at most maxLen runes per line, breaking at word boundaries
// where possible. Any token longer than maxLen is hard-broken at exactly
// maxLen runes so that no output line ever exceeds the limit. When maxLen is
// not positive, s is returned unchanged without panicking.
func Wrap(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}

	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}

	var lines []string
	var current []rune

	for _, word := range words {
		wordRunes := []rune(word)

		// Hard-break any token that is itself longer than maxLen.
		for len(wordRunes) > maxLen {
			if len(current) > 0 {
				lines = append(lines, string(current))
				current = nil
			}
			lines = append(lines, string(wordRunes[:maxLen]))
			wordRunes = wordRunes[maxLen:]
		}

		if len(wordRunes) == 0 {
			continue
		}

		switch {
		case len(current) == 0:
			// Start a new line with this word.
			current = wordRunes
		case len(current)+1+len(wordRunes) <= maxLen:
			// Word fits on the current line with a separating space.
			current = append(current, ' ')
			current = append(current, wordRunes...)
		default:
			// Word does not fit: flush the current line and start a new one.
			lines = append(lines, string(current))
			current = wordRunes
		}
	}

	if len(current) > 0 {
		lines = append(lines, string(current))
	}

	if len(lines) == 0 {
		return s
	}

	return strings.Join(lines, "\n")
}
