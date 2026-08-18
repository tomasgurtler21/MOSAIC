package transform

import "strings"

// DeployedRegionContentChangedDetail renders the TODO detail text emitted when a
// managed region's canonical content changed relative to the previously deployed file
// and that region contains nested user-owned regions. nestedNames must already be sorted
// ascending and must be non-empty. When timestamp is empty the trailing "(detected …)"
// clause is omitted.
//
// Rendered form:
//
//	`Canonical content of deployed region "CommunicationProtocol" changed. Review your
//	 nested content against the updated text: ProtocolExtension, TeamNotes. (detected 2026-08-12T14:25:33Z)`
//
// Pure formatting utility; exported so tests can assert the rendered text without running a
// full transform. Takes no clock input.
func DeployedRegionContentChangedDetail(regionName string, nestedNames []string, timestamp string) string {
	var b strings.Builder
	b.WriteString(`Canonical content of deployed region "`)
	b.WriteString(regionName)
	b.WriteString(`" changed. Review your nested content against the updated text: `)
	b.WriteString(strings.Join(nestedNames, ", "))
	b.WriteString(".")
	if timestamp != "" {
		b.WriteString(" (detected ")
		b.WriteString(timestamp)
		b.WriteString(")")
	}
	return b.String()
}
