package mdtable

import "strings"

// EscapeCell returns v with every "|" replaced by `\|`. No other character is
// changed (backslashes are not doubled).
func EscapeCell(v string) string {
	return strings.ReplaceAll(v, "|", `\|`)
}

// UnescapeCell reverses EscapeCell: every `\|` becomes "|".
func UnescapeCell(cell string) string {
	return strings.ReplaceAll(cell, `\|`, "|")
}

var lineBreakReplacer = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")

// SanitizeCell makes v safe to write as one table cell: each line break becomes
// one space, the result is trimmed, and pipes are escaped as by EscapeCell.
func SanitizeCell(v string) string {
	return EscapeCell(strings.TrimSpace(lineBreakReplacer.Replace(v)))
}

// splitUnescaped splits s on "|" except where the pipe is preceded by a
// backslash; such a pipe stays in the cell together with its backslash.
func splitUnescaped(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '|' && (i == 0 || s[i-1] != '\\') {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}
