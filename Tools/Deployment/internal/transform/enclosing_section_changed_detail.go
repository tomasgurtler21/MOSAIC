package transform

import "strings"

// EnclosingSectionChangedDetail renders the TODO detail text emitted when a
// core section directly enclosing custom regions changed content on update.
// sectionName is the name of the changed section. customNames must already be
// sorted ascending and must be non-empty. When timestamp is empty the trailing
// "(detected ...)" clause is omitted.
//
// Rendered form:
//
//	Section "Identity" changed and contains your custom content.
//	Review for contradictions: MyExtension, TeamPolicy.
//	(detected 2026-08-17T22:41:49Z)
//
// Pure formatting utility; exported so tests can assert the rendered text
// without running a full transform. Takes no clock input.
func EnclosingSectionChangedDetail(sectionName string, customNames []string, timestamp string) string {
	var b strings.Builder
	b.WriteString(`Section "`)
	b.WriteString(sectionName)
	b.WriteString(`" changed and contains your custom content.`)
	b.WriteString("\nReview for contradictions: ")
	b.WriteString(strings.Join(customNames, ", "))
	b.WriteString(".")
	if timestamp != "" {
		b.WriteString("\n(detected ")
		b.WriteString(timestamp)
		b.WriteString(")")
	}
	return b.String()
}
