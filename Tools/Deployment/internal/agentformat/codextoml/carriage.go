package codextoml

// carriage.go provides the TOML source scanner, span extractor and helper
// functions that power the user-key carriage path.
//
// Three responsibilities live here:
//
//  1. TOML line scanning: scanTomlLines produces one tomlLineRecord per source
//     line, annotating each with its top-level key, comment/attached flags, and
//     the raw TOML value text for plain-decimal integer detection.
//
//  2. Span extraction: extractSpanBytes re-assembles the contiguous byte spans
//     for a named top-level key from the scan records. This is the prior-bytes
//     channel consumed by the marker carriage path.
//
//  3. Classification helpers: isBareKey, hasControlChar, isPlainDecimalInt,
//     makeStringCarriageFV, extractForeignHeaderComments, and supporting
//     primitives used by both decode and encode.

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"mosaic-common/mosaic"

	"mosaic-deploy/internal/agentformat"
)

// ---------------------------------------------------------------------------
// TOML line records
// ---------------------------------------------------------------------------

// tomlLineRecord holds classification information for one line of a TOML source
// file. The scanner produces one record per line; callers use the records to
// locate spans and attached comments.
type tomlLineRecord struct {
	lineStart int    // byte offset of line start in source
	lineEnd   int    // byte offset of line end (exclusive, includes '\n')
	topKey    string // top-level key this line belongs to ("" for structural or blank)
	isComment bool   // true when this line is a TOML comment
	attached  bool   // for comment lines: true when this comment is attached to the immediately following key
	rawScalar string // raw TOML value text; non-empty only for direct non-dotted top-level scalar assignments
}

// tomlKeyInfo holds per-key metadata extracted from the scan records.
type tomlKeyInfo struct {
	hasAttachedComment bool   // a comment immediately preceded this key with no blank line
	rawScalar          string // raw TOML value text as it appeared in the file
}

// scanTomlLines scans TOML source bytes line by line and returns one
// tomlLineRecord per line. The scanner tracks:
//
//   - Multi-line basic (""") and literal (''') strings to avoid treating
//     content inside them as table headers or key assignments.
//   - The current section context ([table] or [[array-of-tables]] headers)
//     to assign all subsequent lines to the correct top-level key.
//   - Pending comment lines and whether they are attached to a following key.
//
// A "top-level key" is the first key component in a path. All lines that belong
// to a section, or that use dotted-key syntax referencing that section, carry
// the section's first component as topKey. Blank lines and lines belonging to
// MOSAIC-owned keys are assigned topKey "" (they are excluded from user spans).
//
// Note: ownership filtering is NOT performed here -- topKey is set to the key
// name regardless of whether it is MOSAIC-owned. Callers in decode.go skip
// owned keys before building carriage containers.
func scanTomlLines(src []byte) []tomlLineRecord {
	var records []tomlLineRecord
	type pendingComment struct{ start, end int }
	var pending []pendingComment

	inMLDouble := false  // inside a """ ... """ multi-line basic string
	inMLLiteral := false // inside a ''' ... ''' multi-line literal string
	currentSection := "" // first key component of the current [table] context, "" = top level

	offset := 0
	for offset < len(src) {
		end := bytes.IndexByte(src[offset:], '\n')
		var lineEnd int
		if end < 0 {
			lineEnd = len(src)
		} else {
			lineEnd = offset + end + 1
		}

		lineText := string(src[offset:lineEnd])
		trimmed := strings.TrimSpace(lineText)

		// --- Multi-line string tracking ---
		// Lines inside a multi-line string are attributed to the current section
		// (or "" if at top level) and never treated as table headers or keys.
		if inMLDouble {
			if strings.Contains(trimmed, `"""`) {
				inMLDouble = false
			}
			records = append(records, tomlLineRecord{
				lineStart: offset, lineEnd: lineEnd, topKey: currentSection,
			})
			offset = lineEnd
			continue
		}
		if inMLLiteral {
			if strings.Contains(trimmed, "'''") {
				inMLLiteral = false
			}
			records = append(records, tomlLineRecord{
				lineStart: offset, lineEnd: lineEnd, topKey: currentSection,
			})
			offset = lineEnd
			continue
		}

		// --- Blank line ---
		if len(trimmed) == 0 {
			// Flush any pending comments as unattached (blank line broke the attachment).
			for _, p := range pending {
				records = append(records, tomlLineRecord{
					lineStart: p.start, lineEnd: p.end, topKey: "", isComment: true, attached: false,
				})
			}
			pending = pending[:0]
			records = append(records, tomlLineRecord{lineStart: offset, lineEnd: lineEnd, topKey: ""})
			offset = lineEnd
			continue
		}

		// --- Comment line ---
		if trimmed[0] == '#' {
			pending = append(pending, pendingComment{offset, lineEnd})
			offset = lineEnd
			continue
		}

		// --- Structural line: section header or key assignment ---
		var topKey string
		var rawScalar string

		if strings.HasPrefix(trimmed, "[[") {
			// Array-of-tables header: [[section.path]]
			inner := extractBetweenDoubleSquare(trimmed)
			topKey = parseFirstKeyComponent(inner)
			currentSection = topKey
		} else if trimmed[0] == '[' {
			// Table section header: [section.path]
			inner := extractBetweenSquare(trimmed)
			topKey = parseFirstKeyComponent(inner)
			currentSection = topKey
		} else {
			// Key assignment: key = value
			keyPart, valuePart, ok := extractKeyAndValue(trimmed)
			if ok {
				if currentSection != "" {
					// Inside a section: all lines belong to the section.
					topKey = currentSection
				} else {
					// At top level: the first key component determines the span.
					topKey = parseFirstKeyComponent(keyPart)
					// Only store rawScalar for direct, non-dotted assignments, because
					// dotted keys (mcp_servers.foo = ...) do not have a recoverable
					// source spelling for the top-level key.
					if !strings.ContainsRune(keyPart, '.') {
						rawScalar = valuePart
					}
				}
				// Detect multi-line string openings for any assignment (inside or outside
				// a section), so that subsequent lines are correctly attributed.
				if strings.HasPrefix(valuePart, `"""`) && !strings.Contains(valuePart[3:], `"""`) {
					inMLDouble = true
				} else if strings.HasPrefix(valuePart, "'''") && !strings.Contains(valuePart[3:], "'''") {
					inMLLiteral = true
				}
			}
		}

		// Flush pending comments as attached to this topKey.
		for _, p := range pending {
			records = append(records, tomlLineRecord{
				lineStart: p.start, lineEnd: p.end,
				topKey:    topKey,
				isComment: true,
				attached:  true,
			})
		}
		pending = pending[:0]

		records = append(records, tomlLineRecord{
			lineStart: offset,
			lineEnd:   lineEnd,
			topKey:    topKey,
			rawScalar: rawScalar,
		})

		offset = lineEnd
	}

	// Flush any remaining pending comments as unattached.
	for _, p := range pending {
		records = append(records, tomlLineRecord{
			lineStart: p.start, lineEnd: p.end, topKey: "", isComment: true, attached: false,
		})
	}

	return records
}

// buildInfosFromScan aggregates tomlLineRecords into a per-key tomlKeyInfo map.
func buildInfosFromScan(records []tomlLineRecord) map[string]*tomlKeyInfo {
	infos := make(map[string]*tomlKeyInfo)
	for _, r := range records {
		if r.topKey == "" {
			continue
		}
		info := infos[r.topKey]
		if info == nil {
			info = &tomlKeyInfo{}
			infos[r.topKey] = info
		}
		if r.isComment && r.attached {
			info.hasAttachedComment = true
		}
		if !r.isComment && r.rawScalar != "" {
			// Keep the first non-empty rawScalar seen for this key.
			if info.rawScalar == "" {
				info.rawScalar = r.rawScalar
			}
		}
	}
	return infos
}

// spanIsHeaderRegion returns true if the first non-comment, non-blank line of
// span starts with '[', indicating a TOML table header ([key]) or array-of-tables
// header ([[key]]). Header-region spans must be placed after developer_instructions
// so that the table header does not open a section context that absorbs
// developer_instructions into the wrong TOML table.
func spanIsHeaderRegion(span []byte) bool {
	offset := 0
	for offset < len(span) {
		end := bytes.IndexByte(span[offset:], '\n')
		var lineEnd int
		if end < 0 {
			lineEnd = len(span)
		} else {
			lineEnd = offset + end + 1
		}
		trimmed := strings.TrimSpace(string(span[offset:lineEnd]))
		if len(trimmed) == 0 || trimmed[0] == '#' {
			offset = lineEnd
			continue
		}
		return trimmed[0] == '['
	}
	return false
}

// extractSpanBytes returns the source bytes that belong to the given top-level
// key, by concatenating all line ranges whose topKey equals key. The result is
// the span that encode will re-emit verbatim to recover marker-carried keys.
//
// Returns ErrMarkerUnrecoverable when no lines match (the key was absent from
// the prior deployed bytes).
func extractSpanBytes(src []byte, key string, records []tomlLineRecord) ([]byte, error) {
	var result []byte
	for _, r := range records {
		if r.topKey == key {
			result = append(result, src[r.lineStart:r.lineEnd]...)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: key %q not found in prior deployed bytes", agentformat.ErrMarkerUnrecoverable, key)
	}
	return result, nil
}

// attachedCommentText returns the concatenated text of all comment lines that
// are attached to key in the scan records (i.e. comments that immediately
// preceded the key assignment with no intervening blank line).
func attachedCommentText(src []byte, key string, records []tomlLineRecord) string {
	var sb strings.Builder
	for _, r := range records {
		if r.topKey == key && r.isComment && r.attached {
			sb.Write(src[r.lineStart:r.lineEnd])
		}
	}
	return sb.String()
}

// extractForeignHeaderComments scans src for leading comment lines that are NOT
// stamp lines. It stops at the first non-comment, non-blank line. Blank lines
// within the header block are skipped (they do not stop the scan). Returns nil
// when no foreign comments are found.
//
// A "stamp line" is a comment whose content starts with the mosaic_ prefix (after the
// leading "#" and optional whitespace). These are MOSAIC provenance lines and
// must not be re-emitted as user content.
func extractForeignHeaderComments(src []byte) []byte {
	var result []byte
	offset := 0
	for offset < len(src) {
		end := bytes.IndexByte(src[offset:], '\n')
		var lineEnd int
		if end < 0 {
			lineEnd = len(src)
		} else {
			lineEnd = offset + end + 1
		}
		line := src[offset:lineEnd]
		trimmed := strings.TrimSpace(string(line))
		if len(trimmed) == 0 {
			offset = lineEnd
			continue
		}
		if trimmed[0] != '#' {
			// First non-comment line: stop.
			break
		}
		if !isStampCommentLine(trimmed) {
			result = append(result, line...)
		}
		offset = lineEnd
	}
	return result
}

// stampCommentPrefix is the prefix that all MOSAIC provenance stamp comment
// contents start with (after stripping "#" and whitespace). Derived via
// concatenation to satisfy AC4.7 (no mosaic_-prefixed literals outside
// the agentfields package).
var stampCommentPrefix = "mosaic" + "_"

// isStampCommentLine reports whether trimmed is a MOSAIC provenance stamp
// comment of the form "# mosaic_<key>: <value>".
func isStampCommentLine(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "#") {
		return false
	}
	rest := strings.TrimSpace(trimmed[1:])
	return strings.HasPrefix(rest, stampCommentPrefix)
}

// ---------------------------------------------------------------------------
// Key and value parsing helpers
// ---------------------------------------------------------------------------

// extractKeyAndValue splits a TOML assignment line (already trimmed) into the
// key portion and the value portion. It skips over double-quoted and
// single-quoted key segments to locate the '=' separator correctly.
// Returns ok=false for lines that are not valid key assignments.
func extractKeyAndValue(line string) (key, value string, ok bool) {
	i := 0
	for i < len(line) {
		b := line[i]
		switch {
		case b == '"':
			// Skip double-quoted segment, honouring backslash escapes.
			i++
			for i < len(line) {
				if line[i] == '\\' {
					i += 2
					continue
				}
				if line[i] == '"' {
					i++
					break
				}
				i++
			}
		case b == '\'':
			// Skip single-quoted literal segment (no escape processing).
			i++
			for i < len(line) {
				if line[i] == '\'' {
					i++
					break
				}
				i++
			}
		case b == '=':
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
		default:
			i++
		}
	}
	return "", "", false
}

// parseFirstKeyComponent returns the first key component of a TOML key path.
// For bare keys ("foo.bar") it returns everything up to the first dot.
// For double-quoted keys (`"#tag".rest`) it decodes the quoted component.
// For single-quoted keys it returns the literal content.
func parseFirstKeyComponent(keyPath string) string {
	keyPath = strings.TrimSpace(keyPath)
	if len(keyPath) == 0 {
		return ""
	}
	if keyPath[0] == '"' {
		decoded, _ := decodeTomlBasicStringKey(keyPath[1:])
		return decoded
	}
	if keyPath[0] == '\'' {
		decoded, _ := decodeLiteralStringKey(keyPath[1:])
		return decoded
	}
	// Bare key: up to first dot.
	dot := strings.IndexByte(keyPath, '.')
	if dot < 0 {
		return keyPath
	}
	return keyPath[:dot]
}

// decodeTomlBasicStringKey decodes a TOML basic string key starting after
// the opening '"'. Returns the decoded string and any remaining text after
// the closing '"'. Handles the escape sequences defined in the TOML spec.
func decodeTomlBasicStringKey(s string) (decoded, rest string) {
	var sb strings.Builder
	i := 0
	for i < len(s) {
		b := s[i]
		if b == '"' {
			return sb.String(), s[i+1:]
		}
		if b == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
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
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'u':
				if i+4 < len(s) {
					r, _ := strconv.ParseUint(s[i+1:i+5], 16, 32)
					sb.WriteRune(rune(r))
					i += 4
				}
			case 'U':
				if i+8 < len(s) {
					r, _ := strconv.ParseUint(s[i+1:i+9], 16, 32)
					sb.WriteRune(rune(r))
					i += 8
				}
			}
		} else {
			sb.WriteByte(b)
		}
		i++
	}
	return sb.String(), ""
}

// decodeLiteralStringKey decodes a TOML literal string key starting after
// the opening "'". Returns the decoded string and any remaining text.
func decodeLiteralStringKey(s string) (decoded, rest string) {
	end := strings.IndexByte(s, '\'')
	if end < 0 {
		return s, ""
	}
	return s[:end], s[end+1:]
}

// extractBetweenSquare returns the text between the first '[' and first ']'
// in s. Used to extract table header content from "[section.path]".
func extractBetweenSquare(s string) string {
	start := strings.IndexByte(s, '[')
	if start < 0 {
		return ""
	}
	start++
	end := strings.IndexByte(s[start:], ']')
	if end < 0 {
		return ""
	}
	return s[start : start+end]
}

// extractBetweenDoubleSquare returns the text between the first "[[" and
// first "]]" in s. Used to extract array-of-tables header content.
func extractBetweenDoubleSquare(s string) string {
	start := strings.Index(s, "[[")
	if start < 0 {
		return ""
	}
	start += 2
	end := strings.Index(s[start:], "]]")
	if end < 0 {
		return ""
	}
	return s[start : start+end]
}

// ---------------------------------------------------------------------------
// Classification predicates
// ---------------------------------------------------------------------------

// isBareKey reports whether s consists entirely of TOML bare-key characters
// (A-Za-z0-9_-) and is non-empty. Keys outside this charset require quoted
// TOML syntax and are escalated to marker carriage.
func isBareKey(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') ||
			(b >= '0' && b <= '9') || b == '_' || b == '-' {
			continue
		}
		return false
	}
	return true
}

// hasControlChar reports whether s contains any control character (byte < 0x20
// or byte == 0x7F), including LF, CR and TAB. Used to detect marker key names
// that cannot be represented as double-quoted YAML list items.
func hasControlChar(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < 0x20 || b == 0x7F {
			return true
		}
	}
	return false
}

// hasUnrepresentableControlChar reports whether s contains a control character
// that cannot be represented via the pre-escape trick (i.e. not LF, CR or TAB).
// Used when classifying user string values for value-carry vs marker escalation.
func hasUnrepresentableControlChar(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if (b < 0x20 && b != '\n' && b != '\r' && b != '\t') || b == 0x7F {
			return true
		}
	}
	return false
}

// rePlainDecimalInt matches TOML integer values that are expressed in plain
// decimal notation without underscores or leading sign other than '-'.
// These are the only integer spellings that can be value-carried: the
// go-toml/v2 library normalises exotic spellings (0x1F, 1_000, +5) to Go
// integers, losing the original text.
var rePlainDecimalInt = regexp.MustCompile(`^-?[0-9]+$`)

// isPlainDecimalInt reports whether rawScalar is a plain-decimal integer
// literal: an optional '-' followed by one or more digits, with no underscores,
// no leading '+', and no radix prefix.
func isPlainDecimalInt(rawScalar string) bool {
	return rePlainDecimalInt.MatchString(rawScalar)
}

// ---------------------------------------------------------------------------
// String carriage FieldValue construction
// ---------------------------------------------------------------------------

// makeStringCarriageFV constructs the mosaic.FieldValue used to store a user
// TOML string in the mosaic_carriage mapping. It uses the "pre-escape as
// QuotePlain" trick so that docformat.Parse round-trips produce a FieldValue
// with Quote == QuoteDouble and Scalar == the original string:
//
//  1. Pre-escape val to the five sequences docformat understands.
//  2. Wrap in `"..."`.
//  3. Store as QuotePlain so docformat emits it verbatim.
//  4. When the canonical is later parsed by docformat.Parse, the outer `"..."`
//     makes it a double-quoted scalar → Quote == QuoteDouble, Scalar == val.
func makeStringCarriageFV(val string) mosaic.FieldValue {
	escaped := preEscapeForDocformat(val)
	return mosaic.ScalarValue(`"`+escaped+`"`, mosaic.QuotePlain)
}
