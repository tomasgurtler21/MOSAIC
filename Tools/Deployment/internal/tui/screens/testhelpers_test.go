package screens_test

// testhelpers_test.go provides shared helpers used across all screen test files.

import "strings"

// collapseWhitespace joins the words in s with single spaces, removing all newlines and
// extra whitespace. This is used to compare view output that lipgloss may have wrapped
// across multiple lines, without coupling tests to the exact terminal width used in rendering.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
