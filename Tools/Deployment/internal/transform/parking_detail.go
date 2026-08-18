package transform

import "strings"

// ParkedCustomRegionsDetail renders the TODO detail text for a parking event. names must
// already be sorted. When timestamp is empty the trailing timestamp clause is omitted.
//
// The rendered format is:
//
//	"Schema reorder detected. These custom regions were preserved at end of file —
//	 move them to the correct section: A, B, C. (detected 2026-08-11T17:07:26Z)"
//
// With an empty timestamp, the "(detected …)" clause is omitted:
//
//	"Schema reorder detected. These custom regions were preserved at end of file —
//	 move them to the correct section: A, B, C."
//
// This function is a pure formatting utility. It is exported so that tests can assert the
// exact rendered text without running a full transform. It takes no clock input; the caller
// is responsible for supplying the timestamp string from whatever clock it owns.
func ParkedCustomRegionsDetail(names []string, timestamp string) string {
	var b strings.Builder
	b.WriteString("Schema reorder detected. These custom regions were preserved at end of file — move them to the correct section: ")
	b.WriteString(strings.Join(names, ", "))
	b.WriteString(".")
	if timestamp != "" {
		b.WriteString(" (detected ")
		b.WriteString(timestamp)
		b.WriteString(")")
	}
	return b.String()
}
