package docformat

import "bytes"

// fencePair holds the opening and closing "---" fence lines of a frontmatter block exactly
// as they are written back: each fence keeps its own line terminator, and a closing fence
// at end of input keeps having none.
type fencePair struct {
	open  []byte // "---\n" or "---\r\n"
	close []byte // "---\n", "---\r\n" or "---" (closing fence at EOF without a terminator)
}

// newFencePair builds the pair for an accepted opening terminator and the closing fence line
// (including its terminator, if any) found in the source. A closing fence with trailing
// spaces or tabs is written back as the canonical fence plus its terminator.
func newFencePair(openNL string, closeLine []byte) fencePair {
	return fencePair{
		open:  []byte("---" + openNL),
		close: []byte("---" + lineTerminator(closeLine)),
	}
}

// lineTerminator returns the terminator ("\r\n", "\n" or "") at the end of line.
func lineTerminator(line []byte) string {
	switch {
	case bytes.HasSuffix(line, []byte("\r\n")):
		return "\r\n"
	case bytes.HasSuffix(line, []byte("\n")):
		return "\n"
	}
	return ""
}

// lineEnding is the line ending dirty frontmatter entries follow: the opening fence's.
func (f fencePair) lineEnding() string {
	return nlFromDelim(f.open)
}

func (f fencePair) clone() fencePair {
	return fencePair{open: cloneBytes(f.open), close: cloneBytes(f.close)}
}
