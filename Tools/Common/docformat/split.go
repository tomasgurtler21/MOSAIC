package docformat

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

// SplitFrontmatter is the low-level frontmatter/body split, exposed for consumers that need
// only the raw body bytes without a full parse.
//
// Rules:
//   - src is checked for encoding first: UTF-16 and invalid UTF-8 inputs are rejected with a
//     *FormatError. A leading UTF-8 BOM is ignored for fence detection.
//   - If the first non-blank line is not a "---" delimiter line, frontmatter is nil and body is
//     src (BOM included).
//   - A "---" line with trailing spaces or tabs is accepted as a delimiter (opening or closing).
//     Other fence-like lines (leading blank lines, leading whitespace, invisible characters,
//     CR-only line endings) are rejected with a *FormatError.
//   - If src begins with "---" but has no closing "---", a *FormatError is returned.
//   - If the opening and closing delimiters are present, frontmatter contains the bytes
//     between them (excluding the delimiter lines themselves), and body contains everything
//     after the closing delimiter line.
//   - Line endings (LF and CRLF) are treated as content and are never normalised.
func SplitFrontmatter(src []byte) (frontmatter, body []byte, err error) {
	if len(src) == 0 {
		return nil, nil, nil
	}
	res, err := splitDetailed(src)
	if err != nil {
		return nil, nil, err
	}
	if !res.found {
		return nil, src, nil
	}
	return res.frontmatter, res.body, nil
}

// splitResult is the outcome of splitting a source file.
type splitResult struct {
	found       bool   // opening and closing fences were found
	bom         bool   // src starts with a UTF-8 BOM
	frontmatter []byte // bytes between the fences
	body        []byte // bytes after the closing fence; without the BOM when no frontmatter
	openNL      string // terminator of the accepted opening fence line: "\n" or "\r\n"
	closeLine   []byte // the closing fence line as found, including its terminator if any
}

// splitDetailed implements the encoding checks and the fence policy for non-empty src.
func splitDetailed(src []byte) (splitResult, error) {
	bomLen, err := checkEncoding(src)
	if err != nil {
		return splitResult{}, err
	}
	res := splitResult{bom: bomLen > 0, body: src[bomLen:]}

	openEnd, openNL, ok, err := detectOpening(src, bomLen)
	if err != nil || !ok {
		return res, err
	}
	closeStart, closeEnd, err := findClosing(src, openEnd)
	if err != nil {
		return splitResult{}, err
	}
	res.found = true
	res.openNL = openNL
	res.closeLine = src[closeStart:closeEnd]
	res.frontmatter = src[openEnd:closeStart]
	res.body = src[closeEnd:]
	return res, nil
}

// nextLine returns the line of src starting at pos without its '\n' terminator, and whether
// a '\n' terminator was present.
func nextLine(src []byte, pos int) (line []byte, hasNL bool) {
	nl := bytes.IndexByte(src[pos:], '\n')
	if nl < 0 {
		return src[pos:], false
	}
	return src[pos : pos+nl], true
}

// detectOpening looks at the first non-blank line of src (after the BOM). It reports
// ok=true with the offset just past the opening fence line and its terminator when an
// opening fence is accepted, ok=false when the file has no frontmatter, and an error when
// the line is fence-like but not acceptable.
func detectOpening(src []byte, bomLen int) (openEnd int, nl string, ok bool, err error) {
	pos := bomLen
	lineNo := 1
	for pos < len(src) {
		line, hasNL := nextLine(src, pos)
		if isBlankLine(line) {
			if !hasNL {
				return 0, "", false, nil
			}
			pos += len(line) + 1
			lineNo++
			continue
		}
		crOnly := lineNo == 1 && isCROnlyFenceStart(line, hasNL)
		if !crOnly && !isOpeningFenceLike(line) {
			return 0, "", false, nil
		}
		if crOnly {
			return 0, "", false, &FormatError{
				Problem: FormatProblemCROnlyLineEndings,
				Line:    1,
				Excerpt: renderExcerpt(src[:pos+len(line)], true),
			}
		}
		excerptStart := pos
		if lineNo == 1 {
			excerptStart = 0
		}
		excerpt := renderExcerpt(src[excerptStart:pos+len(line)], excerptStart == 0)
		if lineNo != 1 {
			return 0, "", false, &FormatError{Problem: FormatProblemMalformedFence, Line: lineNo, Excerpt: excerpt}
		}
		return acceptOpening(line, hasNL, pos, excerpt)
	}
	return 0, "", false, nil
}

// acceptOpening decides whether the fence-like first line is an acceptable opening fence.
func acceptOpening(line []byte, hasNL bool, pos int, excerpt string) (int, string, bool, error) {
	content := line
	cr := bytes.HasSuffix(content, []byte{'\r'})
	if cr {
		content = content[:len(content)-1]
	}
	if !isFenceWithTrailingWS(content) {
		return 0, "", false, &FormatError{Problem: FormatProblemMalformedFence, Line: 1, Excerpt: excerpt}
	}
	switch {
	case hasNL && cr:
		return pos + len(line) + 1, "\r\n", true, nil
	case hasNL:
		return pos + len(line) + 1, "\n", true, nil
	case cr:
		return 0, "", false, &FormatError{Problem: FormatProblemCROnlyLineEndings, Line: 1, Excerpt: excerpt}
	}
	return 0, "", false, &FormatError{Problem: FormatProblemUnclosedFrontmatter, Line: 1, Excerpt: excerpt}
}

// findClosing searches src from offset from (just past the opening fence line) for the
// closing fence. It returns the offsets of the start of the closing fence line and just
// past its terminator.
func findClosing(src []byte, from int) (start, end int, err error) {
	pos := from
	lineNo := 2
	for pos < len(src) {
		line, hasNL := nextLine(src, pos)
		lineEnd := pos + len(line)
		if hasNL {
			lineEnd++
		}
		if isClosingCandidate(line) {
			if isClosingFence(line, hasNL) {
				return pos, lineEnd, nil
			}
			if isClosingFenceLike(line) {
				return 0, 0, &FormatError{
					Problem: FormatProblemMalformedFence,
					Line:    lineNo,
					Excerpt: renderExcerpt(line, false),
				}
			}
		}
		pos = lineEnd
		lineNo++
	}
	return 0, 0, &FormatError{
		Problem: FormatProblemUnclosedFrontmatter,
		Line:    1,
		Excerpt: excerptOfLineAt(src, 0),
	}
}

// isCROnlyFenceStart reports whether line starts with "---", optional spaces or tabs, and a
// CR that is not part of a CRLF terminator (the file uses CR-only line endings).
func isCROnlyFenceStart(line []byte, hasNL bool) bool {
	if !bytes.HasPrefix(line, []byte("---")) {
		return false
	}
	i := 3
	for i < len(line) && isSpaceOrTab(line[i]) {
		i++
	}
	if i >= len(line) || line[i] != '\r' {
		return false
	}
	return !(hasNL && i == len(line)-1)
}

// isInvisible reports whether r is one of the invisible characters the fence policy knows.
func isInvisible(r rune) bool {
	switch r {
	case 0xFEFF, 0x200B, 0x200C, 0x200D, 0x2060, 0x00A0:
		return true
	}
	return false
}

func isSpaceOrTab(b byte) bool { return b == ' ' || b == '\t' }

// isBlankLine reports whether line holds only ASCII spaces, tabs and an optional CR.
func isBlankLine(line []byte) bool {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	for _, b := range line {
		if !isSpaceOrTab(b) {
			return false
		}
	}
	return true
}

// isOpeningFenceLike reports whether line equals "---" after removing ASCII spaces, tabs and
// invisible characters at both ends and a trailing CR.
func isOpeningFenceLike(line []byte) bool {
	s := strings.TrimSuffix(string(line), "\r")
	s = strings.TrimFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || isInvisible(r) })
	return s == "---"
}

// isFenceWithTrailingWS reports whether content is "---" followed by ASCII spaces or tabs.
func isFenceWithTrailingWS(content []byte) bool {
	if !bytes.HasPrefix(content, []byte("---")) {
		return false
	}
	for _, b := range content[3:] {
		if !isSpaceOrTab(b) {
			return false
		}
	}
	return true
}

// isClosingCandidate reports whether line starts at column 0 with '-' or an invisible character.
func isClosingCandidate(line []byte) bool {
	if len(line) == 0 {
		return false
	}
	if line[0] == '-' {
		return true
	}
	r, _ := utf8.DecodeRune(line)
	return isInvisible(r)
}

// isClosingFence reports whether line is an accepted closing fence.
func isClosingFence(line []byte, hasNL bool) bool {
	if hasNL {
		line = bytes.TrimSuffix(line, []byte{'\r'})
	}
	return isFenceWithTrailingWS(line)
}

// isClosingFenceLike reports whether line equals "---" after removing invisible characters,
// trailing ASCII spaces/tabs and a trailing CR.
func isClosingFenceLike(line []byte) bool {
	s := strings.Map(func(r rune) rune {
		if isInvisible(r) {
			return -1
		}
		return r
	}, string(line))
	s = strings.TrimRight(strings.TrimSuffix(s, "\r"), " \t")
	return s == "---"
}
