package docformat

import (
	"strings"

	"mosaic-common/mosaic"
)

// RenderFrontmatter renders an ordered sequence of key/FieldValue pairs as YAML
// frontmatter text by calling the same per-entry serialisers the Markdown path uses.
// It does NOT add the opening or closing "---" delimiters; the caller is responsible
// for those.
//
// The nl parameter is the newline convention ("\n" or "\r\n"). Pass "\n" unless the
// target document was originally written with CRLF line endings.
//
// ESCAPING LIMITS (binding on callers -- do not change these without understanding the
// downstream consumers):
//   - Only '\' and '"' are escaped in a double-quoted scalar. Newline (\n), carriage
//     return (\r), tab (\t), and all other control characters are NOT escaped, so a
//     scalar containing a raw newline or carriage return will render as YAML that re-
//     parses to a different value or fails to parse at all.
//   - An unquoted scalar (QuotePlain) is emitted verbatim, with no quoting and no
//     escaping. A scalar containing a leading '#', a leading ':', a ': ' sequence,
//     leading/trailing spaces, or any other YAML-special character must be passed with
//     QuoteDouble or QuoteSingle to be representable.
//   - The parse side (docformat.Parse) understands only the escape sequences \"  \\
//     \n  \r  \t  inside a double-quoted scalar. It does not understand \uXXXX or any
//     other YAML escape form.
//
// The obligation to produce a representable value sits on the caller; this wrapper does
// not validate inputs. A Codex translator's decode path must select a quote style such
// that the rendered value re-parses to its original text.
//
// This symbol delegates to the existing unexported per-entry serialisers rather than
// reimplementing them, so its output is byte-identical to what the Markdown path
// produces for the same key/value pairs.
func RenderFrontmatter(fields []mosaic.FrontmatterField, nl string) string {
	var sb strings.Builder
	for _, f := range fields {
		sb.WriteString(serializeEntry(f.Key, f.Value, nl))
	}
	return sb.String()
}
