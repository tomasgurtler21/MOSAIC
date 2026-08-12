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
	// visitDeployed calls fn on every NodeDeployed reachable from this item, in
	// document order. It is a no-op for text spans.
	visitDeployed(fn func(*Node))
	// visitCustom calls fn on every NodeCustom reachable from this item, in
	// document order. It is a no-op for text spans.
	visitCustom(fn func(*Node))
	// visitRegions calls fn on every NodeInjection, NodeDeployed, or NodeCustom
	// reachable from this item, in document order. It is a no-op for text spans.
	visitRegions(fn func(*Node))
	// visitUserRegions calls fn on every NodeInjection or NodeCustom reachable from
	// this item, in document order. It is a no-op for text spans.
	visitUserRegions(fn func(*Node))
}

// textSpan holds a contiguous run of body bytes that contains no recognised boundary tags.
type textSpan struct {
	raw []byte
}

func (s *textSpan) bodyBytes() []byte               { return s.raw }
func (s *textSpan) visitInjections(_ func(*Node))  {}
func (s *textSpan) visitSections(_ func(*Node))    {}
func (s *textSpan) visitDeployed(_ func(*Node))    {}
func (s *textSpan) visitCustom(_ func(*Node))      {}
func (s *textSpan) visitRegions(_ func(*Node))     {}
func (s *textSpan) visitUserRegions(_ func(*Node)) {}

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

// visitDeployed calls fn on this node (if it is a deployed region) then recurses into items.
func (n *Node) visitDeployed(fn func(*Node)) {
	if n.kind == NodeDeployed {
		fn(n)
	}
	for _, item := range n.items {
		item.visitDeployed(fn)
	}
}

// visitCustom calls fn on this node (if it is a custom region) then recurses into items.
func (n *Node) visitCustom(fn func(*Node)) {
	if n.kind == NodeCustom {
		fn(n)
	}
	for _, item := range n.items {
		item.visitCustom(fn)
	}
}

// visitRegions calls fn on this node (if it is an injection, deployed, or custom region)
// then recurses into items.
func (n *Node) visitRegions(fn func(*Node)) {
	if n.kind == NodeInjection || n.kind == NodeDeployed || n.kind == NodeCustom {
		fn(n)
	}
	for _, item := range n.items {
		item.visitRegions(fn)
	}
}

// visitUserRegions calls fn on this node (if it is an injection or custom region) then
// recurses into items.
func (n *Node) visitUserRegions(fn func(*Node)) {
	if n.kind == NodeInjection || n.kind == NodeCustom {
		fn(n)
	}
	for _, item := range n.items {
		item.visitUserRegions(fn)
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

	case strings.HasPrefix(inner, "DEPLOYED:"):
		n := inner[9:]
		if n == "" {
			return "", false, "", false
		}
		return NodeDeployed, false, n, true

	case strings.HasPrefix(inner, "/DEPLOYED:"):
		n := inner[10:]
		if n == "" {
			return "", false, "", false
		}
		return NodeDeployed, true, n, true

	case strings.HasPrefix(inner, "CUSTOM:"):
		n := inner[7:]
		if n == "" {
			return "", false, "", false
		}
		return NodeCustom, false, n, true

	case strings.HasPrefix(inner, "/CUSTOM:"):
		n := inner[8:]
		if n == "" {
			return "", false, "", false
		}
		return NodeCustom, true, n, true
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

// setBodyOnItems sets the body pointer on every Node reachable from items, recursively.
// This is called after parseBodyItems so that every node knows which body it belongs to,
// enabling AppendRegion to check for document-wide name uniqueness.
func setBodyOnItems(items []bodyItem, b *Body) {
	for _, item := range items {
		if n, ok := item.(*Node); ok {
			n.body = b
			setBodyOnItems(n.items, b)
		}
	}
}

// ensureParsed triggers parsing of the raw body bytes on the first call.
func (b *Body) ensureParsed() {
	if b.parsed {
		return
	}
	b.items = parseBodyItems(b.raw)
	setBodyOnItems(b.items, b)
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

// TopLevelNodes returns all direct child nodes of the body in document order,
// regardless of kind. Text spans between nodes are not included.
func (b *Body) TopLevelNodes() []*Node {
	b.ensureParsed()
	var nodes []*Node
	for _, item := range b.items {
		if n, ok := item.(*Node); ok {
			nodes = append(nodes, n)
		}
	}
	return nodes
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

// Deployed returns the first deployed region with the given name at any nesting depth.
// The name is matched case-sensitively.
func (b *Body) Deployed(name string) (*Node, bool) {
	b.ensureParsed()
	for _, item := range b.items {
		if n, ok := item.(*Node); ok {
			if result := findDeployed(n, name); result != nil {
				return result, true
			}
		}
	}
	return nil, false
}

// findDeployed performs a depth-first search for a deployed region named name within
// the subtree rooted at n.
func findDeployed(n *Node, name string) *Node {
	if n.kind == NodeDeployed && n.name == name {
		return n
	}
	for _, item := range n.items {
		if child, ok := item.(*Node); ok {
			if result := findDeployed(child, name); result != nil {
				return result
			}
		}
	}
	return nil
}

// DeployedRegions returns every deployed region at any nesting depth, in document order.
func (b *Body) DeployedRegions() []*Node {
	b.ensureParsed()
	var deployed []*Node
	for _, item := range b.items {
		item.visitDeployed(func(n *Node) {
			deployed = append(deployed, n)
		})
	}
	return deployed
}

// Regions returns every injection, deployed, and custom region at any nesting depth,
// interleaved in document order.
func (b *Body) Regions() []*Node {
	b.ensureParsed()
	var regions []*Node
	for _, item := range b.items {
		item.visitRegions(func(n *Node) {
			regions = append(regions, n)
		})
	}
	return regions
}

// CustomRegions returns every [[CUSTOM:]] region at any nesting depth, in document order.
func (b *Body) CustomRegions() []*Node {
	b.ensureParsed()
	var customs []*Node
	for _, item := range b.items {
		item.visitCustom(func(n *Node) {
			customs = append(customs, n)
		})
	}
	return customs
}

// Custom returns the first [[CUSTOM:]] region with the given name at any nesting depth.
// The name is matched case-sensitively.
func (b *Body) Custom(name string) (*Node, bool) {
	b.ensureParsed()
	for _, item := range b.items {
		if n, ok := item.(*Node); ok {
			if result := findCustom(n, name); result != nil {
				return result, true
			}
		}
	}
	return nil, false
}

// findCustom performs a depth-first search for a custom region named name within the
// subtree rooted at n.
func findCustom(n *Node, name string) *Node {
	if n.kind == NodeCustom && n.name == name {
		return n
	}
	for _, item := range n.items {
		if child, ok := item.(*Node); ok {
			if result := findCustom(child, name); result != nil {
				return result
			}
		}
	}
	return nil
}

// UserRegions returns every [[INJECTION:]] and [[CUSTOM:]] region at any nesting depth,
// interleaved in document order.
func (b *Body) UserRegions() []*Node {
	b.ensureParsed()
	var userRegions []*Node
	for _, item := range b.items {
		item.visitUserRegions(func(n *Node) {
			userRegions = append(userRegions, n)
		})
	}
	return userRegions
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
	// Propagate the body pointer so any new nested nodes can participate in
	// document-wide name checks (e.g. AppendRegion's duplicate detection).
	if n.body != nil {
		setBodyOnItems(newItems, n.body)
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
