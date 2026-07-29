package docformat

import (
	"bytes"
	"strings"
)

// ---------------------------------------------------------------------------
// bodyItem — the element type for body and node content
// ---------------------------------------------------------------------------

// bodyItem is implemented by textSpan and *Node.
type bodyItem interface {
	// bodyBytes returns the raw bytes that this item contributes to the document body.
	bodyBytes() []byte
	// visitInjections calls fn on every NodeInjection reachable from this item, in
	// document order. It is a no-op for text spans.
	visitInjections(fn func(*Node))
	// visitSections calls fn on every NodeSection reachable from this item, in
	// document order. It is a no-op for text spans.
	visitSections(fn func(*Node))
}

// textSpan holds a contiguous run of body bytes that contains no recognised boundary tags.
type textSpan struct {
	raw []byte
}

func (s *textSpan) bodyBytes() []byte              { return s.raw }
func (s *textSpan) visitInjections(_ func(*Node)) {}
func (s *textSpan) visitSections(_ func(*Node))   {}

// ---------------------------------------------------------------------------
// Node — bodyItem implementation
// ---------------------------------------------------------------------------

// bodyBytes serialises the node: open tag + content items + close tag.
func (n *Node) bodyBytes() []byte {
	var buf bytes.Buffer
	buf.Write(n.openTag)
	for _, item := range n.items {
		buf.Write(item.bodyBytes())
	}
	buf.Write(n.closeTag)
	return buf.Bytes()
}

// visitInjections calls fn on this node (if it is an injection) then recurses into items.
func (n *Node) visitInjections(fn func(*Node)) {
	if n.kind == NodeInjection {
		fn(n)
	}
	for _, item := range n.items {
		item.visitInjections(fn)
	}
}

// visitSections calls fn on this node (if it is a section) then recurses into items.
func (n *Node) visitSections(fn func(*Node)) {
	if n.kind == NodeSection {
		fn(n)
	}
	for _, item := range n.items {
		item.visitSections(fn)
	}
}

// ---------------------------------------------------------------------------
// Boundary tag lexer
// ---------------------------------------------------------------------------

// parseBoundaryTag checks whether line is a boundary tag line. A tag line is a line that,
// after stripping its line terminator, exactly matches one of:
//
//	[[SECTION:Name]]     [[/SECTION:Name]]
//	[[INJECTION:Name]]   [[/INJECTION:Name]]
//
// Name may contain colons and hyphens (e.g. "Workflow:quick-fix"). The function returns
// (kind, isClose, name, true) on a match, and ("", false, "", false) otherwise.
func parseBoundaryTag(line []byte) (kind NodeKind, isClose bool, name string, matched bool) {
	// Strip line terminator to get the pure content.
	s := strings.TrimRight(string(line), "\r\n")

	if !strings.HasPrefix(s, "[[") || !strings.HasSuffix(s, "]]") {
		return "", false, "", false
	}
	inner := s[2 : len(s)-2] // content between [[ and ]]

	switch {
	case strings.HasPrefix(inner, "SECTION:"):
		n := inner[8:]
		if n == "" {
			return "", false, "", false
		}
		return NodeSection, false, n, true

	case strings.HasPrefix(inner, "/SECTION:"):
		n := inner[9:]
		if n == "" {
			return "", false, "", false
		}
		return NodeSection, true, n, true

	case strings.HasPrefix(inner, "INJECTION:"):
		n := inner[10:]
		if n == "" {
			return "", false, "", false
		}
		return NodeInjection, false, n, true

	case strings.HasPrefix(inner, "/INJECTION:"):
		n := inner[11:]
		if n == "" {
			return "", false, "", false
		}
		return NodeInjection, true, n, true
	}

	return "", false, "", false
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

// parseBodyItems parses raw body bytes into a slice of top-level bodyItems.
//
// The parser is lenient: malformed tag structure (unbalanced tags, mismatched names,
// sections nested inside sections) does not cause an error. Structural correctness is
// validated separately by Validate. Specifically:
//   - An unmatched closing tag (when the stack is empty) is treated as literal text.
//   - Unclosed opening tags remain as nodes with an empty closeTag.
//   - A closing tag whose name does not match the open tag is still accepted as a close.
func parseBodyItems(raw []byte) []bodyItem {
	lines := splitLinesPreserving(raw)

	var items []bodyItem
	var stack []*Node
	var pending []byte // accumulated text bytes not yet wrapped in a textSpan

	flushPending := func() {
		if len(pending) > 0 {
			span := &textSpan{raw: pending}
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				top.items = append(top.items, span)
			} else {
				items = append(items, span)
			}
			pending = nil
		}
	}

	for _, line := range lines {
		kind, isClose, name, matched := parseBoundaryTag(line)
		if !matched {
			pending = append(pending, line...)
			continue
		}

		if !isClose {
			// Opening tag.
			flushPending()
			var parentNode *Node
			if len(stack) > 0 {
				parentNode = stack[len(stack)-1]
			}
			node := &Node{
				kind:    kind,
				name:    name,
				openTag: line, // subslice of raw; raw is never modified after parsing
				parent:  parentNode,
			}
			if parentNode != nil {
				parentNode.items = append(parentNode.items, node)
			} else {
				items = append(items, node)
			}
			stack = append(stack, node)
		} else {
			// Closing tag.
			if len(stack) == 0 {
				// Unmatched close: treat the line as literal text.
				pending = append(pending, line...)
				continue
			}
			flushPending()
			top := stack[len(stack)-1]
			top.closeTag = line // subslice of raw; safe since raw is immutable
			stack = stack[:len(stack)-1]
		}
	}

	flushPending()
	// Any nodes still on the stack are unclosed (closeTag is nil/empty).

	return items
}

// ---------------------------------------------------------------------------
// Body methods
// ---------------------------------------------------------------------------

// ensureParsed triggers parsing of the raw body bytes on the first call.
func (b *Body) ensureParsed() {
	if b.parsed {
		return
	}
	b.items = parseBodyItems(b.raw)
	b.parsed = true
}

// bytes returns the current body bytes. Before parsing, the original raw bytes are returned
// verbatim. After parsing (and possibly after mutations), the body is reconstructed from the
// item tree. Reconstruction is byte-identical to the raw bytes for an unmutated parse
// because item bytes are slices of the original raw.
func (b *Body) bytes() []byte {
	if !b.parsed {
		return b.raw
	}
	var buf bytes.Buffer
	for _, item := range b.items {
		buf.Write(item.bodyBytes())
	}
	return buf.Bytes()
}

// Section returns the first top-level section node with the given name. The name is matched
// case-sensitively. Compound names such as "Workflow:quick-fix" are matched exactly.
func (b *Body) Section(name string) (*Node, bool) {
	b.ensureParsed()
	for _, item := range b.items {
		if n, ok := item.(*Node); ok && n.kind == NodeSection && n.name == name {
			return n, true
		}
	}
	return nil, false
}

// SectionDeep searches at any nesting depth for the first section node with the given name.
// Analogous to the existing Injection() method which already searches depth-first.
func (b *Body) SectionDeep(name string) (*Node, bool) {
	b.ensureParsed()
	for _, item := range b.items {
		if n, ok := item.(*Node); ok {
			if result := findSection(n, name); result != nil {
				return result, true
			}
		}
	}
	return nil, false
}

// findSection performs a depth-first search for a section named name within the subtree
// rooted at n.
func findSection(n *Node, name string) *Node {
	if n.kind == NodeSection && n.name == name {
		return n
	}
	for _, item := range n.items {
		if child, ok := item.(*Node); ok {
			if result := findSection(child, name); result != nil {
				return result
			}
		}
	}
	return nil
}

// Injection searches at any nesting depth for the first injection node with the given name.
// The name is matched case-sensitively.
func (b *Body) Injection(name string) (*Node, bool) {
	b.ensureParsed()
	for _, item := range b.items {
		if n, ok := item.(*Node); ok {
			if result := findInjection(n, name); result != nil {
				return result, true
			}
		}
	}
	return nil, false
}

// findInjection performs a depth-first search for an injection named name within the subtree
// rooted at n.
func findInjection(n *Node, name string) *Node {
	if n.kind == NodeInjection && n.name == name {
		return n
	}
	for _, item := range n.items {
		if child, ok := item.(*Node); ok {
			if result := findInjection(child, name); result != nil {
				return result
			}
		}
	}
	return nil
}

// Sections returns all top-level section nodes in document order.
func (b *Body) Sections() []*Node {
	b.ensureParsed()
	var sections []*Node
	for _, item := range b.items {
		if n, ok := item.(*Node); ok && n.kind == NodeSection {
			sections = append(sections, n)
		}
	}
	return sections
}

// SectionsDeep returns all section nodes at any nesting depth, in document order.
// Analogous to the existing Injections() method which already enumerates depth-first.
func (b *Body) SectionsDeep() []*Node {
	b.ensureParsed()
	var sections []*Node
	for _, item := range b.items {
		item.visitSections(func(n *Node) {
			sections = append(sections, n)
		})
	}
	return sections
}

// Injections returns all injection nodes at any nesting depth, in document order.
func (b *Body) Injections() []*Node {
	b.ensureParsed()
	var injections []*Node
	for _, item := range b.items {
		item.visitInjections(func(n *Node) {
			injections = append(injections, n)
		})
	}
	return injections
}

// ---------------------------------------------------------------------------
// Node methods
// ---------------------------------------------------------------------------

// Kind reports whether this node is a section or an injection.
func (n *Node) Kind() NodeKind { return n.kind }

// Name returns the boundary tag name (e.g. "Identity", "Workflow:quick-fix").
func (n *Node) Name() string { return n.name }

// Parent returns the enclosing node, or nil when this node is at the top level of the body.
func (n *Node) Parent() *Node { return n.parent }

// Children returns the direct child nodes (sections and injections) in document order.
// Text spans between children are not included.
func (n *Node) Children() []*Node {
	var children []*Node
	for _, item := range n.items {
		if child, ok := item.(*Node); ok {
			children = append(children, child)
		}
	}
	return children
}

// Content returns the inner bytes of this node, excluding the opening and closing tag lines.
func (n *Node) Content() []byte {
	var buf bytes.Buffer
	for _, item := range n.items {
		buf.Write(item.bodyBytes())
	}
	return buf.Bytes()
}

// SetContent replaces this node's content with b. The replacement bytes are re-parsed for
// nested boundary tags: tags present in b produce live child nodes; tags absent from b cause
// the corresponding child nodes to be removed. The node's own boundary tags are unchanged.
func (n *Node) SetContent(b []byte) error {
	newItems := parseBodyItems(b)
	// Fix up the parent pointer for direct children so they point back to n.
	for _, item := range newItems {
		if child, ok := item.(*Node); ok {
			child.parent = n
		}
	}
	n.items = newItems
	return nil
}

// Clear empties this node's content. IsEmpty returns true after Clear. The boundary tags
// are preserved.
func (n *Node) Clear() error {
	n.items = nil
	return nil
}

// IsEmpty reports whether the node's content (between its boundary tags) is whitespace-only.
func (n *Node) IsEmpty() bool {
	return len(bytes.TrimSpace(n.Content())) == 0
}

// Bytes returns the full bytes of this node including the opening and closing boundary tag
// lines and all content between them.
func (n *Node) Bytes() []byte {
	return n.bodyBytes()
}
