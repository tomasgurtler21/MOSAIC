package docformat

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// FormatProblem identifies which file-format rejection occurred.
type FormatProblem string

const (
	FormatProblemUTF16               FormatProblem = "utf16"
	FormatProblemInvalidUTF8         FormatProblem = "invalid-utf8"
	FormatProblemCROnlyLineEndings   FormatProblem = "cr-only-line-endings"
	FormatProblemMalformedFence      FormatProblem = "malformed-fence"
	FormatProblemUnclosedFrontmatter FormatProblem = "unclosed-frontmatter"
)

// FormatError is returned (possibly wrapped) by SplitFrontmatter and Parse for every
// file-format rejection. docformat has no file path; callers wrap it to add one.
type FormatError struct {
	Problem FormatProblem
	Line    int    // 1-based; counted from the start of the input as given (BOM is on line 1)
	Excerpt string // offending line with invisible characters made visible; "" when not applicable
}

// maxExcerptUnits is the number of source units of a line rendered before truncation.
const maxExcerptUnits = 80

// Error returns an ASCII message containing the line number and, when present, the excerpt.
func (e *FormatError) Error() string {
	msg := fmt.Sprintf("frontmatter: line %d: %s", e.Line, problemText(e.Problem))
	if e.Excerpt != "" {
		msg += fmt.Sprintf(": %q", e.Excerpt)
	}
	return msg
}

// problemText returns the fixed short description for a problem.
func problemText(p FormatProblem) string {
	switch p {
	case FormatProblemUTF16:
		return "file is UTF-16 encoded; UTF-8 is required"
	case FormatProblemInvalidUTF8:
		return "invalid UTF-8 byte sequence"
	case FormatProblemCROnlyLineEndings:
		return "CR-only line endings are not supported"
	case FormatProblemMalformedFence:
		return "malformed frontmatter delimiter"
	case FormatProblemUnclosedFrontmatter:
		return "unclosed frontmatter block, opening '---' delimiter has no matching closing '---'"
	}
	return string(p)
}

// renderExcerpt renders one line (without its '\n' terminator) with invisible and control
// characters made visible. leadingBOM reports that the line starts at the start of the input,
// so a U+FEFF at its very start is shown as <BOM>.
func renderExcerpt(line []byte, leadingBOM bool) string {
	var sb strings.Builder
	units := 0
	for i := 0; i < len(line); {
		if units == maxExcerptUnits {
			sb.WriteString("...")
			break
		}
		r, size := utf8.DecodeRune(line[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			fmt.Fprintf(&sb, "<0x%02X>", line[i])
		case r == 0xFEFF && i == 0 && leadingBOM:
			sb.WriteString("<BOM>")
		case r == '\r':
			sb.WriteString("<CR>")
		case r == '\t':
			sb.WriteString("<TAB>")
		case r < 0x20 || r == 0x7F:
			fmt.Fprintf(&sb, "<0x%02X>", r)
		case r > 0x7F:
			fmt.Fprintf(&sb, "<U+%04X>", r)
		default:
			sb.WriteRune(r)
		}
		i += size
		units++
	}
	return sb.String()
}
