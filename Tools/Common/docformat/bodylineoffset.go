package docformat

import "bytes"

// BodyLineOffset returns the number of lines that precede the first body line in the
// document's current serialisation — that is, the height of the frontmatter block including
// both "---" delimiter lines. It returns 0 for a document that serialises without a
// frontmatter block.
//
// Adding this offset to a body-relative line number yields the line the user sees when
// opening the file. The value tracks the document's current state: a document that gained
// frontmatter keys after parsing reports the offset its serialised form will have.
//
// Line counting rule: a line is a maximal byte run terminated by '\n'; a '\r\n' terminator
// counts as one line, not two; a final unterminated run counts as one line.
//
// Contract:
//   - No frontmatter (or no keys added): returns 0.
//   - Source had frontmatter: returns 2 + number of serialised entry lines.
//   - No frontmatter in source but keys added afterwards (synthesised): returns 2 + entry lines.
//   - CRLF and LF delimiter styles yield the same offset for the same number of content lines.
func (d *Document) BodyLineOffset() int {
	// Fast path: no frontmatter and no keys have been added — body starts at file line 1.
	if !d.hasFM && len(d.fm.entries) == 0 {
		return 0
	}

	// Use the defining invariant: offset = countLines(Bytes()) - countLines(body bytes).
	// This is correct for all three cases: has-frontmatter, synthesised-frontmatter, and
	// both LF and CRLF delimiter styles (CRLF counts as one line per '\n').
	total := countBodyOffsetLines(d.Bytes())
	var bodyBytes []byte
	if d.body != nil {
		bodyBytes = d.body.bytes()
	} else {
		bodyBytes = d.bodyRaw
	}
	return total - countBodyOffsetLines(bodyBytes)
}

// countBodyOffsetLines counts the number of lines in data using the contract rule:
// a line is a maximal byte run terminated by '\n'; '\r\n' counts as one line (not two);
// a final unterminated run counts as one line.
func countBodyOffsetLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte{'\n'})
	// A final byte run not terminated by '\n' is still one line.
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}
