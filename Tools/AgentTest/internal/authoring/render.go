package authoring

import (
	"fmt"
	"strings"
)

// RenderDiagnostic renders a single diagnostic as one line, in the same format
// RenderReport uses for each of its lines. The returned string has no trailing
// newline.
//
// Format: <severity>: <code>: <path>[:<line>][ <pointer>]: <message>[ (hint: <hint>)]
//
// Placeholder substitutions: empty Code → "?", empty Path → "<unknown>".
func RenderDiagnostic(d Diagnostic) string {
	code := d.Code
	if code == "" {
		code = "?"
	}

	path := d.Path
	if path == "" {
		path = "<unknown>"
	}

	// Build the path segment: path[:<line>][ <pointer>]
	var pathSeg string
	if d.Line > 0 {
		pathSeg = fmt.Sprintf("%s:%d", path, d.Line)
	} else {
		pathSeg = path
	}
	if d.Pointer != "" {
		pathSeg = pathSeg + " " + d.Pointer
	}

	line := fmt.Sprintf("%s: %s: %s: %s", string(d.Severity), code, pathSeg, d.Message)

	if d.Hint != "" {
		line = line + " (hint: " + d.Hint + ")"
	}

	return line
}

// RenderReport renders every diagnostic in rep, one per line, in Report.Sorted()
// order. It is the single rendering of an authoring.Report shared by the CLI and
// TUI frontends, so the two can never present the same report differently.
//
// The returned string has no trailing newline. An empty report renders as the
// empty string. No diagnostic is ever capped, elided or truncated: reports are
// unbounded in length by design.
func RenderReport(rep Report) string {
	sorted := rep.Sorted()
	if len(sorted) == 0 {
		return ""
	}

	lines := make([]string, len(sorted))
	for i, d := range sorted {
		lines[i] = RenderDiagnostic(d)
	}
	return strings.Join(lines, "\n")
}
