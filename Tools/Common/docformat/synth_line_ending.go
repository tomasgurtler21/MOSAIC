package docformat

import (
	"bytes"
	"strings"
)

// bodyLineEnding detects the line-ending style of body from its first line terminator:
// "\r\n" when that terminator is CRLF, otherwise "\n" (also when body has no terminator).
func bodyLineEnding(body []byte) string {
	i := bytes.IndexByte(body, '\n')
	if i > 0 && body[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// synthesiseFrontmatter renders a frontmatter block for entries on a document that had none,
// followed by body. The block follows the body's line-ending style.
func synthesiseFrontmatter(entries []*fmEntry, body []byte) []byte {
	nl := bodyLineEnding(body)
	var buf strings.Builder
	buf.WriteString("---" + nl)
	for _, e := range entries {
		buf.WriteString(serializeEntry(e.key, e.value, nl))
	}
	buf.WriteString("---" + nl)
	buf.Write(body)
	return []byte(buf.String())
}
