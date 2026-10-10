package domain

import "strings"

const (
	errorMarkerOpen  = "[error:"
	errorMarkerClose = "]"
)

// FormatErrorMarker returns the Summary marker for code: "[error:" + code + "]",
// e.g. "[error:E501]". Returns "" for ErrorNone.
func FormatErrorMarker(code ErrorCode) string {
	if code == ErrorNone {
		return ""
	}
	return errorMarkerOpen + string(code) + errorMarkerClose
}

// AppendErrorMarker returns summary followed by a single space and the marker
// of code. When code is ErrorNone it returns summary unchanged. When summary is
// empty it returns the marker alone (no leading space).
func AppendErrorMarker(summary string, code ErrorCode) string {
	marker := FormatErrorMarker(code)
	if marker == "" {
		return summary
	}
	if summary == "" {
		return marker
	}
	return summary + " " + marker
}

// SplitErrorMarker separates a trailing marker from a Summary cell value.
// The marker is recognised only at the very end of the (right-trimmed) cell,
// in the well-formed shape "[error:" CODE "]" where CODE is one or more ASCII
// letters or digits. It returns the summary text without the marker and
// without the single separating space, and the code verbatim. A cell with no
// trailing well-formed marker returns (cell, ErrorNone). Other bracket markers
// such as "[commit:abc]" are never consumed.
func SplitErrorMarker(cell string) (summary string, code ErrorCode) {
	trimmed := strings.TrimRight(cell, " \t\r\n")
	if !strings.HasSuffix(trimmed, errorMarkerClose) {
		return cell, ErrorNone
	}
	body := trimmed[:len(trimmed)-len(errorMarkerClose)]
	end := len(body)
	start := end
	for start > 0 && isASCIIAlnum(body[start-1]) {
		start--
	}
	if start == end || start < len(errorMarkerOpen) || body[start-len(errorMarkerOpen):start] != errorMarkerOpen {
		return cell, ErrorNone
	}
	text := body[:start-len(errorMarkerOpen)]
	text = strings.TrimSuffix(text, " ")
	return text, ErrorCode(body[start:end])
}

func isASCIIAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}
