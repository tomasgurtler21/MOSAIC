package transform

import (
	"bytes"

	"mosaic-common/docformat"
)

// regionChild is one direct child node of a region together with the byte range its
// boundary-tag-to-boundary-tag text occupies in the region's inner bytes.
type regionChild struct {
	node       *docformat.Node
	start, end int
}

// scanRegionChildren parses the inner bytes of a managed region and returns its direct
// child nodes with their byte ranges, in document order. ok is false when the bytes do not
// form a balanced region (an unclosed child swallows its siblings, so ranges cannot be trusted)
// or when a child cannot be located.
//
// The scan works on a padded copy that always ends with a line terminator; the returned
// padded bytes are the coordinate system for the ranges.
func scanRegionChildren(content []byte) (children []regionChild, padded []byte, ok bool) {
	nl := DetectLineEnding(content)
	padded = content
	if len(content) > 0 && content[len(content)-1] != '\n' {
		padded = append(append([]byte{}, content...), nl...)
	}

	wrapped := make([]byte, 0, len(padded)+64)
	wrapped = append(wrapped, "<InfrastructureAgents type=\"managed\">"...)
	wrapped = append(wrapped, nl...)
	wrapped = append(wrapped, padded...)
	wrapped = append(wrapped, "</InfrastructureAgents>"...)
	wrapped = append(wrapped, nl...)

	doc, err := docformat.Parse(wrapped)
	if err != nil {
		return nil, padded, false
	}
	wrapper, found := doc.Body().Deployed("InfrastructureAgents")
	if !found || !wrapper.Closed() {
		return nil, padded, false
	}

	cursor := 0
	for _, child := range wrapper.Children() {
		if !child.Closed() {
			return nil, padded, false
		}
		raw := child.Bytes()
		idx := indexAtLineStart(padded, raw, cursor)
		if idx < 0 {
			return nil, padded, false
		}
		children = append(children, regionChild{node: child, start: idx, end: idx + len(raw)})
		cursor = idx + len(raw)
	}
	return children, padded, true
}

// indexAtLineStart returns the first index >= from at which needle occurs at the start of a line.
func indexAtLineStart(haystack, needle []byte, from int) int {
	for from <= len(haystack) {
		rel := bytes.Index(haystack[from:], needle)
		if rel < 0 {
			return -1
		}
		idx := from + rel
		if idx == 0 || haystack[idx-1] == '\n' {
			return idx
		}
		from = idx + 1
	}
	return -1
}

// stripCustomChildren returns content without its direct user-owned (custom) child regions.
// Those regions are re-emitted by the nested-region machinery after the region is generated,
// so a generator must not write them itself. Content that cannot be scanned is returned as is.
func stripCustomChildren(content []byte) []byte {
	children, padded, ok := scanRegionChildren(content)
	if !ok {
		return content
	}
	var out bytes.Buffer
	cursor := 0
	stripped := false
	for _, c := range children {
		if c.node.Kind() != docformat.NodeCustom {
			continue
		}
		out.Write(padded[cursor:c.start])
		cursor = c.end
		stripped = true
	}
	if !stripped {
		return content
	}
	out.Write(padded[cursor:])
	return out.Bytes()
}
