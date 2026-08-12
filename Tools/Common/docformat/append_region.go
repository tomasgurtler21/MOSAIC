package docformat

import (
	"errors"
	"strings"
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

// ErrInvalidRegionKind is returned when AppendRegion or InsertRegionAfter is called
// with NodeSection or NodeDeployed. Structure comes from the source document; the
// append API only accepts user-owned region kinds (NodeInjection, NodeCustom).
var ErrInvalidRegionKind = errors.New("region kind cannot be appended")

// ErrInvalidRegionName is returned when the supplied name is empty or does not match
// the valid tag-name syntax (letter-led segments of letters, digits, and hyphens,
// separated by colons).
var ErrInvalidRegionName = errors.New("region name is not a valid tag name")

// ErrDuplicateRegionName is returned when the supplied name is already in use anywhere
// in the document. Names must be globally unique within a document across all node kinds.
var ErrDuplicateRegionName = errors.New("region name already present in document")

// ErrSiblingNotInDocument is returned by InsertRegionAfter when the sibling node was
// obtained from a different body.
var ErrSiblingNotInDocument = errors.New("sibling node does not belong to this body")

// ---------------------------------------------------------------------------
// Name validation
// ---------------------------------------------------------------------------

// isValidTagName reports whether name is a syntactically valid boundary tag name.
// Valid names match [A-Za-z][A-Za-z0-9-]*(:[A-Za-z][A-Za-z0-9-]*)* — one or more
// colon-separated segments, each beginning with a letter. An empty name is invalid.
func isValidTagName(name string) bool {
	if name == "" {
		return false
	}
	for _, seg := range strings.Split(name, ":") {
		if len(seg) == 0 {
			return false
		}
		c := seg[0]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
			return false
		}
		for i := 1; i < len(seg); i++ {
			b := seg[i]
			if !((b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') ||
				(b >= '0' && b <= '9') || b == '-') {
				return false
			}
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Tag construction helpers
// ---------------------------------------------------------------------------

// kindUpperCase maps a NodeKind to its uppercase tag keyword.
func kindUpperCase(k NodeKind) string {
	return strings.ToUpper(string(k))
}

// buildOpenTagLine returns the opening tag line for a region, including its trailing newline.
func buildOpenTagLine(kind NodeKind, name string) []byte {
	return []byte("[[" + kindUpperCase(kind) + ":" + name + "]]\n")
}

// buildCloseTagLine returns the closing tag line for a region, including its trailing newline.
func buildCloseTagLine(kind NodeKind, name string) []byte {
	return []byte("[[/" + kindUpperCase(kind) + ":" + name + "]]\n")
}

// ---------------------------------------------------------------------------
// Duplicate-name detection
// ---------------------------------------------------------------------------

// nameInUse reports whether name is already used by any node at any depth in the body.
func (b *Body) nameInUse(name string) bool {
	b.ensureParsed()
	for _, item := range b.items {
		if n, ok := item.(*Node); ok {
			if nameInSubtree(n, name) {
				return true
			}
		}
	}
	return false
}

// nameInSubtree reports whether name matches the name of n or any descendant node.
func nameInSubtree(n *Node, name string) bool {
	if n.name == name {
		return true
	}
	for _, item := range n.items {
		if child, ok := item.(*Node); ok {
			if nameInSubtree(child, name) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Seam and content normalisation helpers
// ---------------------------------------------------------------------------

// lastBodyByte returns the last byte of the body's current serialised content, or 0 if
// the body is empty. It is used to check whether a newline must be inserted before a new
// opening tag.
func lastBodyByte(b *Body) byte {
	data := b.bytes()
	if len(data) == 0 {
		return 0
	}
	return data[len(data)-1]
}

// lastContentByte returns the last byte of a node's inner content, or 0 when the content
// is empty. Used to decide whether a seam newline is needed before an appended opening tag.
func lastContentByte(n *Node) byte {
	data := n.Content()
	if len(data) == 0 {
		return 0
	}
	return data[len(data)-1]
}

// ensureTrailingNewline returns content unchanged when it already ends in '\n', or a new
// slice with '\n' appended otherwise. The original slice is never modified.
func ensureTrailingNewline(content []byte) []byte {
	if len(content) > 0 && content[len(content)-1] == '\n' {
		return content
	}
	out := make([]byte, len(content)+1)
	copy(out, content)
	out[len(content)] = '\n'
	return out
}

// ---------------------------------------------------------------------------
// Node construction
// ---------------------------------------------------------------------------

// buildRegionNode constructs a new Node for an appended or inserted region. The node's
// content items are produced by parsing contentBytes so that any nested tags in the
// supplied content are represented as live child nodes. parent may be nil for top-level
// nodes. The body pointer is set so that duplicate-name checks via Node.AppendRegion work.
func buildRegionNode(kind NodeKind, name string, contentBytes []byte, parent *Node, body *Body) *Node {
	openTag := buildOpenTagLine(kind, name)
	closeTag := buildCloseTagLine(kind, name)
	items := parseBodyItems(contentBytes)

	node := &Node{
		kind:     kind,
		name:     name,
		openTag:  openTag,
		items:    items,
		closeTag: closeTag,
		parent:   parent,
		body:     body,
	}
	// Fix up parent and body pointers on direct children so the tree is consistent.
	for _, item := range items {
		if child, ok := item.(*Node); ok {
			child.parent = node
			child.body = body
		}
	}
	return node
}

// ---------------------------------------------------------------------------
// Body.AppendRegion
// ---------------------------------------------------------------------------

// AppendRegion appends a new top-level region to the end of the body and returns the
// live node. Every pre-existing byte of the body is preserved unchanged. When the body
// does not end in a newline, one is inserted before the opening tag so the tag occupies
// its own line.
//
// kind must be NodeInjection or NodeCustom; NodeSection and NodeDeployed are rejected
// with ErrInvalidRegionKind. name must be a syntactically valid tag name and must not
// already be in use anywhere in the document; violations return ErrInvalidRegionName and
// ErrDuplicateRegionName respectively. content is written verbatim; a trailing newline is
// added when absent so the closing tag occupies its own line.
func (b *Body) AppendRegion(kind NodeKind, name string, content []byte) (*Node, error) {
	b.ensureParsed()

	if kind == NodeSection || kind == NodeDeployed {
		return nil, ErrInvalidRegionKind
	}
	if !isValidTagName(name) {
		return nil, ErrInvalidRegionName
	}
	if b.nameInUse(name) {
		return nil, ErrDuplicateRegionName
	}

	// Insert a seam newline when the existing body content does not end in '\n', so the
	// new opening tag is always on its own line. The textSpan is added to b.items before
	// the new node; its bytes become part of the body's serialisation without rewriting
	// any preceding byte.
	if lastBodyByte(b) != '\n' {
		b.items = append(b.items, &textSpan{raw: []byte("\n")})
	}

	// Ensure the supplied content ends with a newline so the closing tag is on its own line.
	contentBytes := ensureTrailingNewline(content)

	node := buildRegionNode(kind, name, contentBytes, nil, b)
	b.items = append(b.items, node)

	return node, nil
}

// ---------------------------------------------------------------------------
// Node.AppendRegion
// ---------------------------------------------------------------------------

// AppendRegion appends a new region at the end of this node's content and returns the
// live node. Same kind, name, and newline rules as Body.AppendRegion. The duplicate-name
// check spans the entire document, not just this node's subtree.
func (n *Node) AppendRegion(kind NodeKind, name string, content []byte) (*Node, error) {
	b := n.body
	if b == nil {
		// This should not occur for nodes obtained through normal document parsing,
		// but guard defensively so the caller gets a clear error rather than a panic.
		return nil, errors.New("node does not belong to a parsed body")
	}

	if kind == NodeSection || kind == NodeDeployed {
		return nil, ErrInvalidRegionKind
	}
	if !isValidTagName(name) {
		return nil, ErrInvalidRegionName
	}
	if b.nameInUse(name) {
		return nil, ErrDuplicateRegionName
	}

	// Seam: if this node's content does not end in '\n', insert one before the opening tag.
	if lastContentByte(n) != '\n' {
		n.items = append(n.items, &textSpan{raw: []byte("\n")})
	}

	contentBytes := ensureTrailingNewline(content)

	child := buildRegionNode(kind, name, contentBytes, n, b)
	n.items = append(n.items, child)

	return child, nil
}

// ---------------------------------------------------------------------------
// Body.InsertRegionAfter
// ---------------------------------------------------------------------------

// InsertRegionAfter inserts a new region immediately after sibling, within sibling's
// parent node or at body top level when sibling is a top-level node. Same kind, name,
// and newline rules as Body.AppendRegion. sibling must belong to this body; a node from
// a different body returns ErrSiblingNotInDocument.
func (b *Body) InsertRegionAfter(sibling *Node, kind NodeKind, name string, content []byte) (*Node, error) {
	b.ensureParsed()

	if kind == NodeSection || kind == NodeDeployed {
		return nil, ErrInvalidRegionKind
	}
	if !isValidTagName(name) {
		return nil, ErrInvalidRegionName
	}
	if b.nameInUse(name) {
		return nil, ErrDuplicateRegionName
	}

	// Locate the container (the items slice) that holds the sibling, and the sibling's
	// index within it. This also verifies that the sibling belongs to this body.
	container, idx, found := b.findItemContainer(sibling)
	if !found {
		return nil, ErrSiblingNotInDocument
	}

	// Seam: the sibling's close tag ends in '\n' in well-formed documents, so no extra
	// newline is normally needed. We check defensively in case the close tag is absent
	// (unclosed tag) or lacks a terminator.
	siblingLast := lastItemByte(sibling)
	var seam []bodyItem
	if siblingLast != 0 && siblingLast != '\n' {
		seam = []bodyItem{&textSpan{raw: []byte("\n")}}
	}

	contentBytes := ensureTrailingNewline(content)

	// The new node's parent is sibling's parent (nil when sibling is top-level).
	node := buildRegionNode(kind, name, contentBytes, sibling.parent, b)

	// Splice the seam and new node after idx in the container.
	insertAt := idx + 1
	extra := len(seam) + 1 // seam items + the new node
	newSlice := make([]bodyItem, 0, len(*container)+extra)
	newSlice = append(newSlice, (*container)[:insertAt]...)
	newSlice = append(newSlice, seam...)
	newSlice = append(newSlice, node)
	newSlice = append(newSlice, (*container)[insertAt:]...)
	*container = newSlice

	return node, nil
}

// ---------------------------------------------------------------------------
// Container search helpers
// ---------------------------------------------------------------------------

// findItemContainer searches the body's item tree for the given sibling node and returns
// a pointer to the items slice that contains it, together with the sibling's index within
// that slice. found is false when the sibling is not reachable from this body.
func (b *Body) findItemContainer(sibling *Node) (container *[]bodyItem, idx int, found bool) {
	// Check top-level items first.
	for i, item := range b.items {
		if n, ok := item.(*Node); ok {
			if n == sibling {
				return &b.items, i, true
			}
		}
	}
	// Recurse into each top-level node.
	for _, item := range b.items {
		if n, ok := item.(*Node); ok {
			if container, idx, found = findItemContainerInNode(n, sibling); found {
				return container, idx, true
			}
		}
	}
	return nil, -1, false
}

// findItemContainerInNode recurses into n's items searching for sibling.
func findItemContainerInNode(n *Node, sibling *Node) (container *[]bodyItem, idx int, found bool) {
	for i, item := range n.items {
		if child, ok := item.(*Node); ok {
			if child == sibling {
				return &n.items, i, true
			}
			if container, idx, found = findItemContainerInNode(child, sibling); found {
				return container, idx, true
			}
		}
	}
	return nil, -1, false
}

// lastItemByte returns the last byte of the serialised form of item, or 0 when the
// serialised form is empty.
func lastItemByte(item bodyItem) byte {
	data := item.bodyBytes()
	if len(data) == 0 {
		return 0
	}
	return data[len(data)-1]
}
