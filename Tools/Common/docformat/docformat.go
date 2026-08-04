package docformat

import (
	"bytes"
	"fmt"
	"strings"

	"mosaic-common/mosaic"
)

// ---------------------------------------------------------------------------
// Document
// ---------------------------------------------------------------------------

// Document is an opaque parsed MOSAIC file. Bytes reproduces the source exactly when
// unmodified; every untouched region is preserved byte-for-byte when the document is
// modified.
type Document struct {
	hasFM   bool   // true when src had opening + closing "---" delimiters
	fmDelim []byte // "---\n" or "---\r\n"
	fm      *Frontmatter
	bodyRaw []byte // bytes after the closing delimiter (or full src when no frontmatter)
	body    *Body  // lazily initialised on first Body() call; non-nil after that
}

// Parse splits and parses a MOSAIC file. It never normalises: line endings, blank lines,
// quoting, key order, and comments are all content to preserve. The returned Document's
// Bytes method reproduces the source exactly when the document is unmodified.
func Parse(src []byte) (*Document, error) {
	rawFM, body, err := SplitFrontmatter(src)
	if err != nil {
		return nil, err
	}

	doc := &Document{bodyRaw: body}

	if rawFM == nil {
		// No frontmatter delimiters — the whole file is the body.
		doc.fm = &Frontmatter{present: false}
		doc.bodyRaw = body // body == src in this case
		return doc, nil
	}

	// Record which delimiter style was used so dirty entries can match.
	doc.hasFM = true
	if bytes.HasPrefix(src, []byte("---\r\n")) {
		doc.fmDelim = []byte("---\r\n")
	} else {
		doc.fmDelim = []byte("---\n")
	}

	fm, err := parseFrontmatterBytes(rawFM)
	if err != nil {
		return nil, err
	}
	fm.present = true
	doc.fm = fm
	return doc, nil
}

// Bytes returns the document serialised to bytes. When the document is unmodified the
// result is byte-identical to the input passed to Parse.
func (d *Document) Bytes() []byte {
	// Use the cached body if it exists (it may have been parsed and/or mutated).
	var bodyBytes []byte
	if d.body != nil {
		bodyBytes = d.body.bytes()
	} else {
		bodyBytes = d.bodyRaw
	}

	if !d.hasFM {
		// When the source had no frontmatter but keys were subsequently added via Set,
		// synthesise a minimal frontmatter block so the added keys appear in the output.
		// When no keys have been added, return the body unchanged (roundtrip invariant).
		if len(d.fm.entries) == 0 {
			return bodyBytes
		}
		nl := "\n"
		var buf bytes.Buffer
		buf.WriteString("---\n")
		for _, e := range d.fm.entries {
			buf.WriteString(serializeEntry(e.key, e.value, nl))
		}
		buf.WriteString("---\n")
		buf.Write(bodyBytes)
		return buf.Bytes()
	}
	nl := nlFromDelim(d.fmDelim)
	var buf bytes.Buffer
	buf.Write(d.fmDelim)
	for _, e := range d.fm.entries {
		if e.dirty {
			buf.WriteString(serializeEntry(e.key, e.value, nl))
		} else {
			buf.Write(e.rawBytes)
		}
	}
	buf.Write(d.fmDelim)
	buf.Write(bodyBytes)
	return buf.Bytes()
}

// Frontmatter returns the frontmatter section. Never nil; Present reports whether a block
// existed in the source.
func (d *Document) Frontmatter() *Frontmatter {
	return d.fm
}

// Body returns the document body. The same Body instance is returned on every call, so
// mutations via body node methods persist on subsequent calls to Body.
func (d *Document) Body() *Body {
	if d.body == nil {
		d.body = &Body{raw: d.bodyRaw}
	}
	return d.body
}

// Clone returns a deep copy of the document. Modifications to the clone do not affect the
// original. If the body has been parsed or mutated, the current serialised body bytes are
// captured into the clone so the clone starts from a fresh, unmodified state.
func (d *Document) Clone() *Document {
	// Capture the current body bytes (handles parsed/mutated body).
	var currentBodyRaw []byte
	if d.body != nil {
		currentBodyRaw = cloneBytes(d.body.bytes())
	} else {
		currentBodyRaw = cloneBytes(d.bodyRaw)
	}

	c := &Document{
		hasFM:   d.hasFM,
		fmDelim: cloneBytes(d.fmDelim),
		bodyRaw: currentBodyRaw,
	}
	if d.fm != nil {
		c.fm = d.fm.clone()
	}
	return c
}

// ---------------------------------------------------------------------------
// Frontmatter
// ---------------------------------------------------------------------------

// fmEntry holds one key-value pair from the frontmatter, including its original bytes so
// that unchanged entries can be emitted verbatim.
type fmEntry struct {
	key      string
	value    mosaic.FieldValue
	rawBytes []byte // exact original bytes for this key's lines (including all newlines)
	dirty    bool   // true when value was changed; serialise from value instead of rawBytes
}

// Frontmatter is the ordered, style-preserving representation of a YAML frontmatter block.
type Frontmatter struct {
	present bool
	entries []*fmEntry
}

// clone returns a deep copy.
func (f *Frontmatter) clone() *Frontmatter {
	c := &Frontmatter{present: f.present}
	for _, e := range f.entries {
		c.entries = append(c.entries, &fmEntry{
			key:      e.key,
			value:    cloneFieldValue(e.value),
			rawBytes: cloneBytes(e.rawBytes),
			dirty:    e.dirty,
		})
	}
	return c
}

// Present reports whether the source document contained a frontmatter block (opening and
// closing --- delimiters).
func (f *Frontmatter) Present() bool {
	return f.present
}

// Keys returns the frontmatter keys in source order.
func (f *Frontmatter) Keys() []string {
	keys := make([]string, len(f.entries))
	for i, e := range f.entries {
		keys[i] = e.key
	}
	return keys
}

// Get returns the FieldValue for the given key. The second return is false when the key is
// absent.
func (f *Frontmatter) Get(key string) (mosaic.FieldValue, bool) {
	for _, e := range f.entries {
		if e.key == key {
			return e.value, true
		}
	}
	return mosaic.FieldValue{}, false
}

// Fields returns all key-value pairs in source order.
func (f *Frontmatter) Fields() []mosaic.FrontmatterField {
	fields := make([]mosaic.FrontmatterField, len(f.entries))
	for i, e := range f.entries {
		fields[i] = mosaic.FrontmatterField{Key: e.key, Value: e.value}
	}
	return fields
}

// Set adds or overwrites the given key in place. If the key already exists its value is
// replaced; if not, it is appended. Style information in v is honoured on serialisation.
func (f *Frontmatter) Set(key string, v mosaic.FieldValue) error {
	for _, e := range f.entries {
		if e.key == key {
			e.value = v
			e.dirty = true
			return nil
		}
	}
	// New key — append.
	f.entries = append(f.entries, &fmEntry{
		key:   key,
		value: v,
		dirty: true,
	})
	return nil
}

// Insert adds a new key-value pair at the position described by at. A zero Position
// (both Before and After empty) appends the key.
func (f *Frontmatter) Insert(key string, v mosaic.FieldValue, at Position) error {
	newE := &fmEntry{key: key, value: v, dirty: true}

	if at.Before == "" && at.After == "" {
		f.entries = append(f.entries, newE)
		return nil
	}

	if at.Before != "" {
		idx := f.indexOfKey(at.Before)
		if idx < 0 {
			return fmt.Errorf("frontmatter: Insert Before: anchor key %q not found", at.Before)
		}
		f.entries = insertFMEntry(f.entries, idx, newE)
		return nil
	}

	// After.
	idx := f.indexOfKey(at.After)
	if idx < 0 {
		return fmt.Errorf("frontmatter: Insert After: anchor key %q not found", at.After)
	}
	f.entries = insertFMEntry(f.entries, idx+1, newE)
	return nil
}

// Remove removes the given key. Returns true if the key was present; false if it was
// absent (a no-op).
func (f *Frontmatter) Remove(key string) bool {
	for i, e := range f.entries {
		if e.key == key {
			f.entries = append(f.entries[:i], f.entries[i+1:]...)
			return true
		}
	}
	return false
}

// Reorder reorders the frontmatter keys according to order. Keys not listed in order keep
// their source-relative position and are appended after the listed keys, in their original
// relative order.
func (f *Frontmatter) Reorder(order []string) error {
	listed := make(map[string]bool, len(order))
	for _, k := range order {
		listed[k] = true
	}

	reordered := make([]*fmEntry, 0, len(f.entries))

	// First: listed keys in specified order.
	for _, k := range order {
		if e := f.findEntry(k); e != nil {
			reordered = append(reordered, e)
		}
	}

	// Then: unlisted keys in original relative order.
	for _, e := range f.entries {
		if !listed[e.key] {
			reordered = append(reordered, e)
		}
	}

	f.entries = reordered
	return nil
}

func (f *Frontmatter) indexOfKey(key string) int {
	for i, e := range f.entries {
		if e.key == key {
			return i
		}
	}
	return -1
}

func (f *Frontmatter) findEntry(key string) *fmEntry {
	for _, e := range f.entries {
		if e.key == key {
			return e
		}
	}
	return nil
}

// Position describes where to insert a new frontmatter key relative to an existing one.
// Before takes precedence over After when both are non-empty. Both empty means append.
type Position struct {
	Before string // insert immediately before this existing key
	After  string // insert immediately after this existing key
}

// ---------------------------------------------------------------------------
// Body and Node
// ---------------------------------------------------------------------------

// Body is the opaque document body (everything after the closing frontmatter delimiter).
// The body is parsed lazily: the first call to any navigation method triggers parsing.
type Body struct {
	raw    []byte
	parsed bool
	items  []bodyItem // top-level items; populated by ensureParsed
}

// NodeKind classifies a body node as a section or an injection point.
type NodeKind string

const (
	NodeSection   NodeKind = "section"
	NodeInjection NodeKind = "injection"
	NodeDeployed  NodeKind = "deployed" // tool-managed region
)

// Node is a section or injection block in the document body. The content between the
// boundary tags is stored as a list of child items (text spans and nested nodes) so that
// mutations and serialisation are exact.
type Node struct {
	kind     NodeKind
	name     string
	openTag  []byte     // raw bytes of the opening tag line, including its terminator
	items    []bodyItem // content items between the open and close tags
	closeTag []byte     // raw bytes of the closing tag line; empty when the tag is unclosed
	parent   *Node      // nil when the node is at the top level of the body
}

// ---------------------------------------------------------------------------
// Validate and canonical vocabulary
// ---------------------------------------------------------------------------

// Severity classifies the urgency of a validation issue.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is one structural finding produced by Validate.
type Issue struct {
	Severity Severity
	Code     string
	Message  string
	Line     int
	Node     string
}

// ValidateOptions controls which structural checks Validate performs.
type ValidateOptions struct {
	RequireCanonicalSections bool
	RequireInjectionParents  bool
	AllowUnknownInjections   bool
}

// CanonicalSections lists the canonical section names in their expected document order.
// Populated in vocabulary.go.
var CanonicalSections []string

// CanonicalInjections lists all recognised injection names.
// Populated in vocabulary.go.
var CanonicalInjections []string

// InjectionParent maps each canonical injection name to its expected parent section name.
// Populated in vocabulary.go.
var InjectionParent map[string]string

// ---------------------------------------------------------------------------
// Frontmatter parser
// ---------------------------------------------------------------------------

// parseFrontmatterBytes parses the raw YAML content (between the "---" delimiters) into
// an ordered list of frontmatter entries, preserving original bytes for each entry.
func parseFrontmatterBytes(data []byte) (*Frontmatter, error) {
	lines := splitLinesPreserving(data)

	type lineGroup struct {
		key       string
		lines     [][]byte
		startLine int // 1-indexed, for error messages
	}

	var groups []lineGroup
	seenKeys := map[string]bool{}

	var currentKey string
	var currentLines [][]byte
	var currentStartLine int

	for lineNum, line := range lines {
		// Detect tab at the start of any line (YAML disallows tab indentation).
		if len(line) > 0 && line[0] == '\t' {
			return nil, fmt.Errorf(
				"frontmatter: line %d: tab character used for indentation (YAML requires spaces)",
				lineNum+1,
			)
		}

		if key, ok := isTopLevelKeyLine(line); ok {
			// Save the previous group before starting a new one.
			if currentKey != "" {
				groups = append(groups, lineGroup{
					key:       currentKey,
					lines:     currentLines,
					startLine: currentStartLine,
				})
			}
			if seenKeys[key] {
				return nil, fmt.Errorf(
					"frontmatter: line %d: duplicate key %q", lineNum+1, key,
				)
			}
			seenKeys[key] = true
			currentKey = key
			currentLines = [][]byte{line}
			currentStartLine = lineNum + 1
		} else if currentKey != "" {
			currentLines = append(currentLines, line)
		} else if len(bytes.TrimSpace(line)) == 0 {
			// Empty line before the first key — skip.
			continue
		} else {
			return nil, fmt.Errorf(
				"frontmatter: line %d: unexpected content %q", lineNum+1, line,
			)
		}
	}

	if currentKey != "" {
		groups = append(groups, lineGroup{
			key:       currentKey,
			lines:     currentLines,
			startLine: currentStartLine,
		})
	}

	fm := &Frontmatter{}
	for _, g := range groups {
		value, err := parseGroupValue(g.key, g.lines, g.startLine)
		if err != nil {
			return nil, err
		}
		raw := combineLines(g.lines)
		fm.entries = append(fm.entries, &fmEntry{
			key:      g.key,
			value:    value,
			rawBytes: raw,
		})
	}
	return fm, nil
}

// splitLinesPreserving splits data into individual lines, keeping each line's terminator
// (LF or CRLF) attached to the line. The last segment (if non-empty and unterminated) is
// included as a final element.
func splitLinesPreserving(data []byte) [][]byte {
	var lines [][]byte
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			lines = append(lines, data)
			break
		}
		lines = append(lines, data[:i+1])
		data = data[i+1:]
	}
	return lines
}

// isTopLevelKeyLine returns (key, true) when line is a YAML mapping key at column 0.
// A valid key starts with a letter or digit, may contain letters, digits, underscores, and
// hyphens, and is followed by a colon and either a space, CR, LF, or end of line.
func isTopLevelKeyLine(line []byte) (string, bool) {
	if len(line) == 0 {
		return "", false
	}
	// Must start at column 0 (not indented, not a comment).
	c := line[0]
	if c == ' ' || c == '\t' || c == '#' || c == '-' {
		return "", false
	}

	// Find the colon.
	i := 0
	for i < len(line) {
		b := line[i]
		if b == ':' {
			break
		}
		if !isKeyChar(b) {
			return "", false
		}
		i++
	}
	if i == 0 || i >= len(line) {
		return "", false
	}
	key := string(line[:i])

	// After the colon must be a space, CR, LF, or end of line.
	if i+1 < len(line) {
		next := line[i+1]
		if next != ' ' && next != '\t' && next != '\r' && next != '\n' {
			return "", false
		}
	}
	return key, true
}

func isKeyChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_' || b == '-'
}

// combineLines concatenates lines into a single byte slice.
func combineLines(lines [][]byte) []byte {
	total := 0
	for _, l := range lines {
		total += len(l)
	}
	buf := make([]byte, 0, total)
	for _, l := range lines {
		buf = append(buf, l...)
	}
	return buf
}

// parseGroupValue extracts the FieldValue from a group of lines belonging to one key.
// lines[0] is the key line; lines[1:] are continuation lines for block values.
func parseGroupValue(key string, lines [][]byte, startLine int) (mosaic.FieldValue, error) {
	firstLine := lines[0]

	// Locate the colon in the key line.
	colonIdx := bytes.IndexByte(firstLine, ':')
	// afterColon is everything after the colon on the first line, stripped of its newline.
	afterColon := bytes.TrimRight(firstLine[colonIdx+1:], "\r\n")
	// valueText is the trimmed value portion (leading space removed).
	valueText := bytes.TrimLeft(afterColon, " \t")

	if len(valueText) == 0 {
		// Block value — look at continuation lines.
		return parseBlockValue(lines[1:], startLine)
	}

	if valueText[0] == '[' {
		return parseFlowSequence(valueText, startLine)
	}

	return parseScalarBytes(valueText)
}

// parseBlockValue processes continuation lines that follow a key with an empty inline value.
func parseBlockValue(lines [][]byte, startLine int) (mosaic.FieldValue, error) {
	// Skip empty lines at the start.
	for len(lines) > 0 && len(bytes.TrimSpace(lines[0])) == 0 {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return mosaic.ScalarValue("", mosaic.QuotePlain), nil
	}

	first := lines[0]
	// Tab check.
	if len(first) > 0 && first[0] == '\t' {
		return mosaic.FieldValue{}, fmt.Errorf(
			"frontmatter: line %d: tab character used for indentation", startLine+1,
		)
	}

	// Detect block type from the indented content.
	stripped := bytes.TrimLeft(first, " ")
	strippedStr := string(bytes.TrimRight(stripped, "\r\n"))

	if strings.HasPrefix(strippedStr, "- ") || strippedStr == "-" {
		return parseBlockSequence(lines, startLine)
	}
	// Assume block mapping.
	return parseBlockMapping(lines, startLine)
}

// parseBlockSequence parses indented block sequence lines into a KindList with ListBlock.
//
// Each list item begins with a "  - " marker at a consistent indentation level. Items may
// be simple scalars ("  - value") or inline mappings ("  - key: value" followed by
// continuation lines "    key2: value2"). Continuation lines are collected and parsed as
// KindMapping items, enabling YAML list-of-maps support (e.g. the `triggers` field in
// infrastructure agent frontmatter).
func parseBlockSequence(lines [][]byte, startLine int) (mosaic.FieldValue, error) {
	// Determine the indentation level of the sequence from the first non-empty line.
	itemIndentLen := -1
	for _, l := range lines {
		if len(bytes.TrimSpace(l)) > 0 {
			itemIndentLen = len(l) - len(bytes.TrimLeft(l, " "))
			break
		}
	}
	if itemIndentLen < 0 {
		return mosaic.ListValue(nil, mosaic.ListBlock), nil
	}

	// Group lines into per-item groups. A new item starts when we encounter "- " at
	// exactly the detected indentation level. Continuation lines (deeper indentation,
	// used for mapping-valued items) are attached to the preceding item.
	type itemGroup struct {
		firstLineText []byte   // text after "- " on the opening line
		continuation  [][]byte // subsequent lines belonging to this item
		startLine     int
	}
	var groups []itemGroup

	for i, line := range lines {
		lineNum := startLine + 1 + i

		if len(bytes.TrimSpace(line)) == 0 {
			// Empty lines belong to the current group for structural completeness,
			// but are ignored during per-item parsing.
			if len(groups) > 0 {
				groups[len(groups)-1].continuation = append(groups[len(groups)-1].continuation, line)
			}
			continue
		}
		if len(line) > 0 && line[0] == '\t' {
			return mosaic.FieldValue{}, fmt.Errorf(
				"frontmatter: line %d: tab character used for indentation", lineNum,
			)
		}

		// Count leading spaces on this line.
		leadingSpaces := 0
		for _, b := range line {
			if b == ' ' {
				leadingSpaces++
			} else {
				break
			}
		}
		stripped := bytes.TrimLeft(line, " ")
		strippedStr := string(bytes.TrimRight(stripped, "\r\n"))

		if leadingSpaces == itemIndentLen && (strings.HasPrefix(strippedStr, "- ") || strippedStr == "-") {
			// New list item at the canonical indentation level.
			var itemText []byte
			if strings.HasPrefix(strippedStr, "- ") {
				itemText = []byte(strippedStr[2:])
			}
			groups = append(groups, itemGroup{
				firstLineText: itemText,
				startLine:     lineNum,
			})
		} else if len(groups) > 0 {
			// Continuation line (deeper indentation): belongs to the current item.
			groups[len(groups)-1].continuation = append(groups[len(groups)-1].continuation, line)
		} else {
			return mosaic.FieldValue{}, fmt.Errorf(
				"frontmatter: line %d: expected list item ('- '), got %q", lineNum, strippedStr,
			)
		}
	}

	// Parse each item group into a FieldValue.
	var items []mosaic.FieldValue
	for _, g := range groups {
		// An item is a mapping when it has non-empty continuation lines or when its
		// first-line text looks like "key: value".
		hasContinuation := false
		for _, l := range g.continuation {
			if len(bytes.TrimSpace(l)) > 0 {
				hasContinuation = true
				break
			}
		}
		isMapping := hasContinuation || blockSequenceItemLooksLikeMapping(g.firstLineText)

		if isMapping {
			// Build a flat list of mapping lines: the first-line text (acting as the
			// first "key: value" pair) followed by the non-empty continuation lines.
			var mappingLines [][]byte
			if len(g.firstLineText) > 0 {
				mappingLines = append(mappingLines, g.firstLineText)
			}
			for _, cl := range g.continuation {
				if len(bytes.TrimSpace(cl)) > 0 {
					mappingLines = append(mappingLines, cl)
				}
			}
			item, err := parseBlockMapping(mappingLines, g.startLine)
			if err != nil {
				return mosaic.FieldValue{}, err
			}
			items = append(items, item)
		} else {
			item, err := parseScalarBytes(g.firstLineText)
			if err != nil {
				return mosaic.FieldValue{}, fmt.Errorf("frontmatter: line %d: %w", g.startLine, err)
			}
			items = append(items, item)
		}
	}

	return mosaic.ListValue(items, mosaic.ListBlock), nil
}

// blockSequenceItemLooksLikeMapping reports whether the text after "- " on a block
// sequence item's opening line looks like a YAML mapping entry ("key: value" or "key:").
// This heuristic drives the decision to parse the item (and its continuation lines) as a
// KindMapping rather than a KindScalar. False positives are intentionally avoided by
// requiring that the colon be followed by a space, tab, or end-of-content (bare "key:"
// form). URLs such as "https://example.com" are not matched because their colon is
// followed by "/".
func blockSequenceItemLooksLikeMapping(text []byte) bool {
	for i, b := range text {
		if b != ':' || i == 0 {
			continue
		}
		if i+1 >= len(text) {
			return true // "key:" at end of line
		}
		next := text[i+1]
		if next == ' ' || next == '\t' || next == '\r' || next == '\n' {
			return true // "key: value"
		}
	}
	return false
}

// parseBlockMapping parses indented "  key: value" lines into a KindMapping.
func parseBlockMapping(lines [][]byte, startLine int) (mosaic.FieldValue, error) {
	var pairs []mosaic.FieldPair
	for i, line := range lines {
		lineNum := startLine + 1 + i
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if len(line) > 0 && line[0] == '\t' {
			return mosaic.FieldValue{}, fmt.Errorf(
				"frontmatter: line %d: tab character used for indentation", lineNum,
			)
		}
		stripped := bytes.TrimLeft(line, " ")
		colonIdx := bytes.IndexByte(stripped, ':')
		if colonIdx <= 0 {
			return mosaic.FieldValue{}, fmt.Errorf(
				"frontmatter: line %d: expected mapping entry (key: value), got %q", lineNum, stripped,
			)
		}
		key := string(stripped[:colonIdx])
		after := bytes.TrimRight(stripped[colonIdx+1:], "\r\n")
		after = bytes.TrimLeft(after, " \t")
		val, err := parseScalarBytes(after)
		if err != nil {
			return mosaic.FieldValue{}, fmt.Errorf("frontmatter: line %d: %w", lineNum, err)
		}
		pairs = append(pairs, mosaic.FieldPair{Key: key, Value: val})
	}
	return mosaic.MappingValue(pairs), nil
}

// parseFlowSequence parses a "[item1, item2, ...]" value into a KindList with ListFlow.
func parseFlowSequence(data []byte, startLine int) (mosaic.FieldValue, error) {
	// Find the matching ']'.
	end, comment, err := findFlowSequenceEnd(data, startLine)
	if err != nil {
		return mosaic.FieldValue{}, err
	}

	inner := data[1:end] // content between '[' and ']'
	items, err := splitFlowItems(inner, startLine)
	if err != nil {
		return mosaic.FieldValue{}, err
	}

	result := mosaic.ListValue(items, mosaic.ListFlow)
	if len(comment) > 0 {
		result.Comment = string(comment)
	}
	return result, nil
}

// findFlowSequenceEnd finds the closing ']' of a flow sequence starting with '['.
// Returns (indexOfClosingBracket, commentBytes, error).
func findFlowSequenceEnd(data []byte, startLine int) (int, []byte, error) {
	i := 1
	for i < len(data) {
		switch data[i] {
		case ']':
			rest := bytes.TrimLeft(data[i+1:], " \t")
			var comment []byte
			if len(rest) > 0 && rest[0] == '#' {
				comment = rest
			}
			return i, comment, nil
		case '"':
			end := findEndOfDoubleQuotedAt(data, i)
			if end < 0 {
				return -1, nil, fmt.Errorf(
					"frontmatter: line %d: unclosed double-quoted string in flow sequence",
					startLine,
				)
			}
			i = end
			continue
		case '\'':
			end := findEndOfSingleQuotedAt(data, i)
			if end < 0 {
				return -1, nil, fmt.Errorf(
					"frontmatter: line %d: unclosed single-quoted string in flow sequence",
					startLine,
				)
			}
			i = end
			continue
		}
		i++
	}
	return -1, nil, fmt.Errorf(
		"frontmatter: line %d: unclosed flow sequence — '[' has no matching ']'",
		startLine,
	)
}

// splitFlowItems splits the inner content of a flow sequence by commas, respecting quotes.
func splitFlowItems(inner []byte, startLine int) ([]mosaic.FieldValue, error) {
	var items []mosaic.FieldValue
	var current []byte
	i := 0

	for i < len(inner) {
		switch inner[i] {
		case ',':
			trimmed := bytes.TrimSpace(current)
			if len(trimmed) > 0 {
				item, err := parseScalarBytes(trimmed)
				if err != nil {
					return nil, fmt.Errorf("frontmatter: line %d: %w", startLine, err)
				}
				items = append(items, item)
			}
			current = nil
		case '"':
			end := findEndOfDoubleQuotedAt(inner, i)
			if end < 0 {
				return nil, fmt.Errorf(
					"frontmatter: line %d: unclosed double-quoted string", startLine,
				)
			}
			current = append(current, inner[i:end]...)
			i = end
			continue
		case '\'':
			end := findEndOfSingleQuotedAt(inner, i)
			if end < 0 {
				return nil, fmt.Errorf(
					"frontmatter: line %d: unclosed single-quoted string", startLine,
				)
			}
			current = append(current, inner[i:end]...)
			i = end
			continue
		default:
			current = append(current, inner[i])
		}
		i++
	}

	// Last item.
	if trimmed := bytes.TrimSpace(current); len(trimmed) > 0 {
		item, err := parseScalarBytes(trimmed)
		if err != nil {
			return nil, fmt.Errorf("frontmatter: line %d: %w", startLine, err)
		}
		items = append(items, item)
	}

	return items, nil
}

// parseScalarBytes parses a single scalar value, detecting quote style and inline comment.
func parseScalarBytes(data []byte) (mosaic.FieldValue, error) {
	if len(data) == 0 {
		return mosaic.ScalarValue("", mosaic.QuotePlain), nil
	}

	switch data[0] {
	case '"':
		end := findEndOfDoubleQuotedAt(data, 0)
		if end < 0 {
			return mosaic.FieldValue{}, fmt.Errorf("unclosed double-quoted string")
		}
		raw := data[1 : end-1] // strip outer quotes
		unescaped := unescapeDoubleQuoted(raw)
		rest := bytes.TrimLeft(data[end:], " \t")
		var comment string
		if len(rest) > 0 && rest[0] == '#' {
			comment = string(rest)
		}
		fv := mosaic.ScalarValue(unescaped, mosaic.QuoteDouble)
		fv.Comment = comment
		return fv, nil

	case '\'':
		end := findEndOfSingleQuotedAt(data, 0)
		if end < 0 {
			return mosaic.FieldValue{}, fmt.Errorf("unclosed single-quoted string")
		}
		raw := data[1 : end-1] // strip outer quotes
		unescaped := unescapeSingleQuoted(raw)
		rest := bytes.TrimLeft(data[end:], " \t")
		var comment string
		if len(rest) > 0 && rest[0] == '#' {
			comment = string(rest)
		}
		fv := mosaic.ScalarValue(unescaped, mosaic.QuoteSingle)
		fv.Comment = comment
		return fv, nil

	default:
		value, comment := splitPlainAndComment(data)
		fv := mosaic.ScalarValue(string(value), mosaic.QuotePlain)
		if len(comment) > 0 {
			fv.Comment = string(comment)
		}
		return fv, nil
	}
}

// splitPlainAndComment splits a plain scalar into its value and optional trailing comment.
// A comment begins with '#' preceded by at least one space or tab character.
func splitPlainAndComment(data []byte) (value, comment []byte) {
	for i := 1; i < len(data); i++ {
		if data[i] == '#' && (data[i-1] == ' ' || data[i-1] == '\t') {
			v := bytes.TrimRight(data[:i], " \t")
			return v, data[i:]
		}
	}
	return data, nil
}

// findEndOfDoubleQuotedAt finds the exclusive end position of a double-quoted string
// that starts at position start in data.
func findEndOfDoubleQuotedAt(data []byte, start int) int {
	i := start + 1
	for i < len(data) {
		if data[i] == '\\' {
			i += 2
			continue
		}
		if data[i] == '"' {
			return i + 1
		}
		i++
	}
	return -1
}

// findEndOfSingleQuotedAt finds the exclusive end position of a single-quoted YAML string
// that starts at position start in data. In YAML, '' is the only escape (a literal quote).
func findEndOfSingleQuotedAt(data []byte, start int) int {
	i := start + 1
	for i < len(data) {
		if data[i] == '\'' {
			if i+1 < len(data) && data[i+1] == '\'' {
				i += 2 // escaped single quote
				continue
			}
			return i + 1
		}
		i++
	}
	return -1
}

// unescapeDoubleQuoted converts YAML double-quote escape sequences to their character values.
func unescapeDoubleQuoted(data []byte) string {
	var sb strings.Builder
	i := 0
	for i < len(data) {
		if data[i] == '\\' && i+1 < len(data) {
			switch data[i+1] {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			default:
				sb.WriteByte(data[i])
				sb.WriteByte(data[i+1])
			}
			i += 2
		} else {
			sb.WriteByte(data[i])
			i++
		}
	}
	return sb.String()
}

// unescapeSingleQuoted converts YAML single-quote escape sequences ('' → ') to their values.
func unescapeSingleQuoted(data []byte) string {
	return strings.ReplaceAll(string(data), "''", "'")
}

// ---------------------------------------------------------------------------
// Serialiser for dirty entries
// ---------------------------------------------------------------------------

// nlFromDelim returns the newline string ("\r\n" or "\n") implied by the delimiter.
func nlFromDelim(delim []byte) string {
	if bytes.HasSuffix(delim, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// serializeEntry serialises one frontmatter entry to its YAML text representation.
func serializeEntry(key string, v mosaic.FieldValue, nl string) string {
	switch v.Kind {
	case mosaic.KindScalar:
		return serializeScalarEntry(key, v, nl)
	case mosaic.KindList:
		return serializeListEntry(key, v, nl)
	case mosaic.KindMapping:
		return serializeMappingEntry(key, v, nl)
	}
	return key + ":" + nl
}

func serializeScalarEntry(key string, v mosaic.FieldValue, nl string) string {
	var sb strings.Builder
	sb.WriteString(key)
	sb.WriteString(": ")
	sb.WriteString(serializeScalarInline(v))
	if v.Comment != "" {
		sb.WriteString(" ")
		sb.WriteString(v.Comment)
	}
	sb.WriteString(nl)
	return sb.String()
}

func serializeListEntry(key string, v mosaic.FieldValue, nl string) string {
	var sb strings.Builder
	if v.List == mosaic.ListFlow {
		sb.WriteString(key)
		sb.WriteString(": [")
		for i, item := range v.Items {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(serializeScalarInline(item))
		}
		sb.WriteString("]")
		if v.Comment != "" {
			sb.WriteString(" ")
			sb.WriteString(v.Comment)
		}
		sb.WriteString(nl)
	} else {
		// Block style.
		sb.WriteString(key)
		sb.WriteString(":")
		sb.WriteString(nl)
		for _, item := range v.Items {
			sb.WriteString("  - ")
			sb.WriteString(serializeScalarInline(item))
			sb.WriteString(nl)
		}
	}
	return sb.String()
}

func serializeMappingEntry(key string, v mosaic.FieldValue, nl string) string {
	var sb strings.Builder
	sb.WriteString(key)
	sb.WriteString(":")
	sb.WriteString(nl)
	for _, pair := range v.Pairs {
		sb.WriteString("  ")
		sb.WriteString(pair.Key)
		sb.WriteString(": ")
		switch pair.Value.Kind {
		case mosaic.KindList:
			if pair.Value.List == mosaic.ListFlow {
				sb.WriteString("[")
				for i, item := range pair.Value.Items {
					if i > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(serializeScalarInline(item))
				}
				sb.WriteString("]")
			} else {
				// Nested block list inside a mapping — unusual, but handle gracefully.
				sb.WriteString(nl)
				for _, item := range pair.Value.Items {
					sb.WriteString("    - ")
					sb.WriteString(serializeScalarInline(item))
					sb.WriteString(nl)
				}
				continue
			}
		default:
			sb.WriteString(serializeScalarInline(pair.Value))
		}
		sb.WriteString(nl)
	}
	return sb.String()
}

// serializeScalarInline serialises a scalar FieldValue without the key prefix or newline.
func serializeScalarInline(v mosaic.FieldValue) string {
	switch v.Quote {
	case mosaic.QuoteDouble:
		return `"` + escapeDoubleQuoted(v.Scalar) + `"`
	case mosaic.QuoteSingle:
		return `'` + escapeSingleQuoted(v.Scalar) + `'`
	default:
		return v.Scalar
	}
}

func escapeDoubleQuoted(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func escapeSingleQuoted(s string) string {
	return strings.ReplaceAll(s, `'`, `''`)
}

// ---------------------------------------------------------------------------
// Helper utilities
// ---------------------------------------------------------------------------

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func cloneFieldValue(v mosaic.FieldValue) mosaic.FieldValue {
	result := v
	if v.Items != nil {
		result.Items = make([]mosaic.FieldValue, len(v.Items))
		for i, item := range v.Items {
			result.Items[i] = cloneFieldValue(item)
		}
	}
	if v.Pairs != nil {
		result.Pairs = make([]mosaic.FieldPair, len(v.Pairs))
		for i, p := range v.Pairs {
			result.Pairs[i] = mosaic.FieldPair{Key: p.Key, Value: cloneFieldValue(p.Value)}
		}
	}
	return result
}

func insertFMEntry(entries []*fmEntry, idx int, e *fmEntry) []*fmEntry {
	entries = append(entries, nil)
	copy(entries[idx+1:], entries[idx:])
	entries[idx] = e
	return entries
}
