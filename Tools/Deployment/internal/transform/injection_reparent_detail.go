package transform

import "strings"

// InjectionReparentedDetail renders the advisory TODO detail text emitted when a source
// [[INJECTION:]] region that existed in the previously deployed file now sits under a
// different parent. oldParent and newParent are anchor names; either may be empty, meaning
// the region sat at document top level, which renders as "top level". When timestamp is
// empty the trailing "(detected …)" clause is omitted.
//
// Rendered form:
//
//	`Injection "IdentityExtension" moved from Identity to Capabilities. Your content was
//	 preserved at the new location — review that it still reads correctly there. (detected 2026-08-12T14:25:33Z)`
//
// Empty parent names render as the literal "top level", so a top-level-to-nested move reads
// "moved from top level to Capabilities", and the reverse reads "moved from Identity to top level".
//
// Pure formatting utility; exported so tests can assert the rendered text without running a
// full transform. Takes no clock input.
func InjectionReparentedDetail(name, oldParent, newParent, timestamp string) string {
	formatParent := func(p string) string {
		if p == "" {
			return "top level"
		}
		return p
	}

	var sb strings.Builder
	sb.WriteString(`Injection "`)
	sb.WriteString(name)
	sb.WriteString(`" moved from `)
	sb.WriteString(formatParent(oldParent))
	sb.WriteString(` to `)
	sb.WriteString(formatParent(newParent))
	sb.WriteString(`. Your content was preserved at the new location — review that it still reads correctly there.`)
	if timestamp != "" {
		sb.WriteString(` (detected `)
		sb.WriteString(timestamp)
		sb.WriteString(`)`)
	}
	return sb.String()
}
