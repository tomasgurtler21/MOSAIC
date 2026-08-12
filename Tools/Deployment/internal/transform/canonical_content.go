package transform

import (
	"bytes"

	"mosaic-common/docformat"
)

// CanonicalContent returns the tool-managed portion of a [[DEPLOYED:]] region's content:
// the node's content bytes with the full marker block of every direct user-owned child
// region ([[CUSTOM:]] and [[INJECTION:]]) removed, then leading and trailing whitespace
// trimmed from the result.
//
// It exists to make the previously-deployed and newly-generated forms of a region
// comparable. A deployed region's content is canonical text plus re-emitted nested user
// regions; freshly generated content is canonical text only. Comparing raw content would
// report a change on every run where nested content exists.
//
// A node with no user-owned children yields its content trimmed. A nil node yields nil.
// Pure; no I/O, no clock.
func CanonicalContent(n *docformat.Node) []byte {
	if n == nil {
		return nil
	}
	content := n.Content()
	// Excise the full marker block of each direct user-owned child region.
	// Both [[CUSTOM:]] and [[INJECTION:]] children are removed so the result
	// contains only the tool-managed canonical text.
	for _, child := range n.Children() {
		if child.Kind() == docformat.NodeCustom || child.Kind() == docformat.NodeInjection {
			childBytes := child.Bytes()
			// Remove the first (and only expected) occurrence of this child's bytes.
			content = bytes.Replace(content, childBytes, nil, 1)
		}
	}
	return bytes.TrimSpace(content)
}
