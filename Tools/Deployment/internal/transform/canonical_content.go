package transform

import (
	"bytes"

	"mosaic-common/docformat"
)

// CanonicalContent returns the tool-managed portion of a managed region's content:
// the node's content bytes with the full marker block of every direct user-owned child
// region (custom and injection) removed, then leading and trailing whitespace
// trimmed from the result.
//
// It exists to make the previously-deployed and newly-generated forms of a region
// comparable. A deployed region's content is canonical text plus re-emitted nested user
// regions; freshly generated content is canonical text only. Comparing raw content would
// report a change on every run where nested content exists.
//
// A node with no user-owned children yields its content trimmed. A nil node yields nil.
// Pure; no I/O, no clock.

// SectionOwnContent returns the section-own portion of a section node's
// content: the node's content bytes with the full marker block of every direct
// child region removed (custom, injection, and managed/deployed children),
// then leading and trailing whitespace trimmed.
//
// This is broader than CanonicalContent, which excises only custom and
// injection children. SectionOwnContent additionally excises managed
// (NodeDeployed) children so that a regenerated managed sub-region does not
// register as a core-prose change.
//
// Use case: comparing the deployed and output versions of a core section to
// detect whether the section's own text changed, ignoring all nested region
// content that has its own change-detection path.
//
// A node with no children yields its content trimmed. A nil node yields nil.
// Pure; no I/O, no clock.
func SectionOwnContent(n *docformat.Node) []byte {
	if n == nil {
		return nil
	}
	content := n.Content()
	// Excise the full marker block of every direct child region: custom, injection,
	// and managed (NodeDeployed). This ensures that regenerated managed sub-regions
	// do not masquerade as changes to the section's own core prose.
	for _, child := range n.Children() {
		if child.Kind() == docformat.NodeCustom ||
			child.Kind() == docformat.NodeInjection ||
			child.Kind() == docformat.NodeDeployed {
			childBytes := child.Bytes()
			content = bytes.Replace(content, childBytes, nil, 1)
		}
	}
	return bytes.TrimSpace(content)
}

func CanonicalContent(n *docformat.Node) []byte {
	if n == nil {
		return nil
	}
	content := n.Content()
	// Excise the full marker block of each direct user-owned child region.
	// Both custom and injection children are removed so the result
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
