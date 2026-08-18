package docformat

import "bytes"

// ---------------------------------------------------------------------------
// Node read accessors
// ---------------------------------------------------------------------------

// Version returns the value of the region's version attribute on its opening tag.
// It returns "" when the tag carries no version attribute, when the attribute value
// is empty, or when the node has no opening tag. Never returns an error: an absent
// version is a legitimate state, not a failure.
func (n *Node) Version() string {
	return n.version
}

// Closed reports whether the region has a matching closing tag. It returns false for
// a region whose opening tag was never closed before end of document. This is the
// supported way to detect an unclosed region; callers must not re-derive a close-tag
// string and search for it.
func (n *Node) Closed() bool {
	return len(n.closeTag) > 0
}

// ---------------------------------------------------------------------------
// Node version mutator
// ---------------------------------------------------------------------------

// SetVersion sets, replaces, or removes the version attribute on the region's opening
// tag. An empty version removes the attribute. Only this node's opening tag line is
// re-rendered — in canonical form, preserving the line's existing terminator (LF or
// CRLF); every other byte of the document, including this node's content and closing
// tag, is untouched. Returns ErrVersionNotSettable when the node has no opening tag
// (a node constructed without an opening tag), and ErrInvalidVersion when version
// contains a quote character, a line terminator, or leading/trailing whitespace.
func (n *Node) SetVersion(version string) error {
	if len(n.openTag) == 0 {
		return ErrVersionNotSettable
	}
	if err := validateVersion(version); err != nil {
		return err
	}

	// Determine the line terminator of the existing opening tag.
	terminator := []byte("\n")
	if bytes.HasSuffix(n.openTag, []byte("\r\n")) {
		terminator = []byte("\r\n")
	}

	// Re-render the opening tag in canonical form. buildOpenTagLine always produces "\n".
	newTag := buildOpenTagLine(n.kind, n.name, version)

	// Restore the original terminator if it was CRLF.
	if len(terminator) == 2 {
		newTag = append(bytes.TrimSuffix(newTag, []byte("\n")), terminator...)
	}

	n.openTag = newTag
	n.version = version
	return nil
}
