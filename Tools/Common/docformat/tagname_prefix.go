package docformat

import "strings"

// TagNamePrefix returns the tag-name portion of a boundary name: everything before the
// first ":" in a compound name (e.g. "Workflow:brownfield-tdd" → "Workflow"), or the whole
// name when it contains no ":". This is the name that appears in the tag itself and in the
// closing tag; the part after the ":" comes from the opening tag's name attribute and is
// never serialised into a closing tag.
func TagNamePrefix(name string) string {
	if idx := strings.IndexByte(name, ':'); idx >= 0 {
		return name[:idx]
	}
	return name
}
