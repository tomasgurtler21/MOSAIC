package docformat

import "bytes"

// vocabulary_stage4.go declares the Stage 4 vocabulary additions: the CanonicalManagedBlocks
// registry, the ManagedBlockParent map, and the IsManagedBlockName and RetypeOpenTagLine helpers.

// CanonicalManagedBlocks lists the tool-managed block names — managed regions the tool
// emits nested inside a managed parent region. Members have no generator of their own, are
// not refresh-scope regions, and are not valid bundle targets; that is why they are a
// separate registry and not members of CanonicalDeployed.
//
// A member may appear in bare form (<Workflow type="managed">) or compound form
// (<Workflow type="managed" name="brownfield-tdd">); matching is on the tag-name prefix.
//
// Populated in vocabulary.go init(). Initial content: exactly one entry — "Workflow".
var CanonicalManagedBlocks []string

// ManagedBlockParent maps each managed block name to the managed region it must be nested
// inside. An absent key means no parent requirement. Unlike DeployedParent, whose values
// name enclosing sections, these values name enclosing managed regions.
//
// Populated in vocabulary.go init(). "Workflow" → "AvailableWorkflows".
var ManagedBlockParent map[string]string

// IsManagedBlockName reports whether name denotes a tool-managed block — a managed region
// the tool emits nested inside a managed parent region, rather than a top-level managed
// region with its own generator. The check is on the tag-name prefix, so both the bare form
// ("Workflow") and the compound form ("Workflow:brownfield-tdd") report true.
//
// Managed block names are NOT members of CanonicalDeployed: they have no generator of their
// own, are not valid bundle targets, and are not refresh-scope regions.
func IsManagedBlockName(name string) bool {
	prefix := TagNamePrefix(name)
	for _, n := range CanonicalManagedBlocks {
		if n == prefix {
			return true
		}
	}
	return false
}

// RetypeOpenTagLine returns a copy of an opening boundary tag line with its type attribute
// value replaced by the marker literal for kind ("core", "managed", "project", "custom").
//
// Everything else is preserved byte-for-byte: the tag name, every other attribute, the
// attribute order, the quoting style, the runs of spaces or tabs between attributes, and the
// line terminator (LF, CRLF, or absent).
//
// It returns (nil, false) when line is not an opening boundary tag — including a closing tag,
// a tag with no recognised type attribute, or any non-tag line. It never alters a closing tag.
func RetypeOpenTagLine(line []byte, kind NodeKind) (retyped []byte, ok bool) {
	// Validate: must be a recognised opening boundary tag with a known type attribute.
	_, isClose, _, _, matched := parseBoundaryTag(line)
	if !matched || isClose {
		return nil, false
	}

	// Determine the new type literal from the kind→attribute mapping.
	newTypeVal, exists := kindTypeAttr[kind]
	if !exists {
		return nil, false
	}

	// Separate the line terminator from the content bytes.
	body := line
	var terminator []byte
	if bytes.HasSuffix(body, []byte("\r\n")) {
		terminator = body[len(body)-2:]
		body = body[:len(body)-2]
	} else if bytes.HasSuffix(body, []byte("\n")) {
		terminator = body[len(body)-1:]
		body = body[:len(body)-1]
	}

	// Locate the type attribute value by scanning for " type=" or "\ttype=".
	// The attribute is always preceded by at least one whitespace character (space or tab)
	// because boundary tags have the form: <TagName SP+ attrs...>.
	qStart := -1
	for i := 1; i+5 <= len(body); i++ {
		prev := body[i-1]
		if (prev == ' ' || prev == '\t') &&
			body[i] == 't' && body[i+1] == 'y' && body[i+2] == 'p' &&
			body[i+3] == 'e' && body[i+4] == '=' {
			qStart = i + 5 // position of the opening quote character
			break
		}
	}
	if qStart < 0 || qStart >= len(body) {
		return nil, false
	}
	quote := body[qStart]
	if quote != '"' && quote != '\'' {
		return nil, false
	}
	// Find the matching closing quote within the body after the opening quote.
	closeIdx := bytes.IndexByte(body[qStart+1:], quote)
	if closeIdx < 0 {
		return nil, false
	}

	// Build the retyped line: everything up to and including the opening quote,
	// then the new type value, then from the closing quote character onwards.
	var buf bytes.Buffer
	buf.Write(body[:qStart+1])  // ..type='  or ..type="
	buf.WriteString(newTypeVal) // e.g. "managed"
	buf.Write(body[qStart+1+closeIdx:]) // closing quote + rest of tag + '>'
	if len(terminator) > 0 {
		buf.Write(terminator)
	}
	return buf.Bytes(), true
}
