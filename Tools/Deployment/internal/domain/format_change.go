package domain

import "strings"

// LineEndingStyle classifies the line terminators of one file's bytes.
//
// Only "\n" bytes are terminators. Each "\n" is a CRLF terminator when the byte before it is
// "\r", and an LF terminator otherwise. A "\r" not followed by "\n" is not a terminator and
// does not affect the class. A leading UTF-8 BOM does not affect the class.
// With L = number of LF terminators and C = number of CRLF terminators, the classes are:
type LineEndingStyle string

const (
	LineEndingNone  LineEndingStyle = "none"  // L == 0 && C == 0 (includes empty content)
	LineEndingLF    LineEndingStyle = "lf"    // L >= 1 && C == 0
	LineEndingCRLF  LineEndingStyle = "crlf"  // C >= 1 && L == 0
	LineEndingMixed LineEndingStyle = "mixed" // L >= 1 && C >= 1
)

// FormatChange describes how a rewrite changed the formatting of an existing file.
type FormatChange struct {
	BOMBefore         bool            // existing file started with a UTF-8 BOM
	BOMAfter          bool            // written bytes start with a UTF-8 BOM
	LineEndingsBefore LineEndingStyle // classification of the existing file
	LineEndingsAfter  LineEndingStyle // classification of the written bytes
}

// BOMChanged reports BOMBefore != BOMAfter.
func (c FormatChange) BOMChanged() bool {
	return c.BOMBefore != c.BOMAfter
}

// LineEndingsChanged reports that both styles are not LineEndingNone and they differ.
func (c FormatChange) LineEndingsChanged() bool {
	return c.LineEndingsBefore != LineEndingNone && c.LineEndingsAfter != LineEndingNone &&
		c.LineEndingsBefore != c.LineEndingsAfter
}

// String joins the changed parts with "; ", BOM part first: "BOM removed" | "BOM added", then
// "line endings <before> -> <after>" with styles rendered as "LF", "CRLF", "mixed".
// Examples: "BOM removed; line endings CRLF -> LF", "BOM added", "line endings mixed -> CRLF".
// Returns "" when nothing changed.
func (c FormatChange) String() string {
	var parts []string
	if c.BOMChanged() {
		if c.BOMBefore {
			parts = append(parts, "BOM removed")
		} else {
			parts = append(parts, "BOM added")
		}
	}
	if c.LineEndingsChanged() {
		parts = append(parts, "line endings "+lineEndingLabel(c.LineEndingsBefore)+" -> "+lineEndingLabel(c.LineEndingsAfter))
	}
	return strings.Join(parts, "; ")
}

func lineEndingLabel(s LineEndingStyle) string {
	switch s {
	case LineEndingLF:
		return "LF"
	case LineEndingCRLF:
		return "CRLF"
	}
	return string(s)
}
