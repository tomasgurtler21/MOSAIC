package transform

import "strings"

// FallthroughCustomRegionsDetail renders the TODO detail text emitted when one or more
// custom regions could not be anchored to their recorded parent in the output document
// and were placed at body level instead. names must already be sorted ascending. When
// timestamp is empty the trailing "(detected …)" clause is omitted.
//
// Rendered form:
//
//	"Parent section no longer exists. These custom regions were placed at body level —
//	 move them to the correct section: A, B, C. (detected 2026-08-12T14:25:33Z)"
//
// With an empty timestamp, the "(detected …)" clause and its preceding space are omitted:
//
//	"Parent section no longer exists. These custom regions were placed at body level —
//	 move them to the correct section: A, B, C."
//
// Pure formatting utility; exported so tests can assert the rendered text without running a
// full transform. Takes no clock input.
func FallthroughCustomRegionsDetail(names []string, timestamp string) string {
	var b strings.Builder
	b.WriteString("Parent section no longer exists. These custom regions were placed at body level — move them to the correct section: ")
	b.WriteString(strings.Join(names, ", "))
	b.WriteString(".")
	if timestamp != "" {
		b.WriteString(" (detected ")
		b.WriteString(timestamp)
		b.WriteString(")")
	}
	return b.String()
}
