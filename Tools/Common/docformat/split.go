package docformat

import (
	"bytes"
	"errors"
)

// SplitFrontmatter is the low-level frontmatter/body split, exposed for consumers that need
// only the raw body bytes without a full parse.
//
// Rules:
//   - If src does not begin with a "---" delimiter line, frontmatter is nil and body is src.
//   - If src begins with "---" but has no closing "---", an error is returned.
//   - If the opening and closing delimiters are present, frontmatter contains the bytes
//     between them (excluding the delimiter lines themselves), and body contains everything
//     after the closing delimiter line including its trailing newline.
//   - Line endings (LF and CRLF) are treated as content and are never normalised.
func SplitFrontmatter(src []byte) (frontmatter, body []byte, err error) {
	if len(src) == 0 {
		return nil, nil, nil
	}

	// The opening delimiter must be the very first line.
	var delimLen int
	if bytes.HasPrefix(src, []byte("---\r\n")) {
		delimLen = 5
	} else if bytes.HasPrefix(src, []byte("---\n")) {
		delimLen = 4
	} else {
		// No frontmatter delimiter.
		return nil, src, nil
	}

	rest := src[delimLen:]

	// Find the closing "---" at the start of a line.
	closingStart, closingEnd := findClosingDelimiter(rest)
	if closingStart < 0 {
		return nil, nil, errors.New(
			"frontmatter: unclosed frontmatter block — opening '---' delimiter has no matching closing '---'",
		)
	}

	return rest[:closingStart], rest[closingEnd:], nil
}

// findClosingDelimiter searches data (the content after the opening "---" delimiter line) for
// the first line that is exactly "---" (with LF or CRLF). Returns (startOfLine, endOfLine)
// where startOfLine is the byte offset of the "---" and endOfLine is just past the newline.
// Returns (-1, -1) when no closing delimiter is found.
func findClosingDelimiter(data []byte) (start, end int) {
	// Handle the case where data itself begins with "---" (empty frontmatter block).
	if bytes.HasPrefix(data, []byte("---\r\n")) {
		return 0, 5
	}
	if bytes.HasPrefix(data, []byte("---\n")) {
		return 0, 4
	}
	// Edge: "---" with no trailing newline at very end of file.
	if bytes.Equal(data, []byte("---")) {
		return 0, 3
	}

	i := 0
	for i < len(data) {
		nl := bytes.IndexByte(data[i:], '\n')
		if nl < 0 {
			break
		}
		lineEnd := i + nl + 1 // byte offset past the '\n'

		next := data[lineEnd:]
		if bytes.HasPrefix(next, []byte("---\r\n")) {
			return lineEnd, lineEnd + 5
		}
		if bytes.HasPrefix(next, []byte("---\n")) {
			return lineEnd, lineEnd + 4
		}
		if bytes.Equal(next, []byte("---")) {
			return lineEnd, lineEnd + 3
		}

		i = lineEnd
	}

	return -1, -1
}
