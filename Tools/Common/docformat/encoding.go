package docformat

import (
	"bytes"
	"unicode/utf8"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// checkEncoding rejects UTF-16 (by BOM) and invalid UTF-8 inputs with a *FormatError and
// returns the length of a leading UTF-8 BOM (0 or 3) otherwise.
func checkEncoding(src []byte) (bomLen int, err error) {
	if bytes.HasPrefix(src, []byte{0xFF, 0xFE}) || bytes.HasPrefix(src, []byte{0xFE, 0xFF}) {
		return 0, &FormatError{Problem: FormatProblemUTF16, Line: 1}
	}
	if !utf8.Valid(src) {
		off := firstInvalidUTF8(src)
		return 0, &FormatError{
			Problem: FormatProblemInvalidUTF8,
			Line:    lineNumberAt(src, off),
			Excerpt: excerptOfLineAt(src, off),
		}
	}
	if bytes.HasPrefix(src, utf8BOM) {
		return len(utf8BOM), nil
	}
	return 0, nil
}

// firstInvalidUTF8 returns the byte offset of the first invalid UTF-8 sequence in src.
func firstInvalidUTF8(src []byte) int {
	for i := 0; i < len(src); {
		r, size := utf8.DecodeRune(src[i:])
		if r == utf8.RuneError && size <= 1 {
			return i
		}
		i += size
	}
	return len(src)
}

// lineNumberAt returns the 1-based line number of the byte at offset off in src.
func lineNumberAt(src []byte, off int) int {
	return bytes.Count(src[:off], []byte{'\n'}) + 1
}

// excerptOfLineAt renders the line of src that contains offset off.
func excerptOfLineAt(src []byte, off int) string {
	start := bytes.LastIndexByte(src[:off], '\n') + 1
	end := len(src)
	if nl := bytes.IndexByte(src[off:], '\n'); nl >= 0 {
		end = off + nl
	}
	return renderExcerpt(src[start:end], start == 0)
}
