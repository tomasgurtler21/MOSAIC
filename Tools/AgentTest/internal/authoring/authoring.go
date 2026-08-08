// Package authoring parses and schema-validates the three authored file
// formats — test suite, test definition, stub registry — into domain types,
// accumulating every diagnostic rather than stopping at the first.
package authoring

import (
	"sort"
	"strings"
)

// Source is one authored document plus where it came from, so diagnostics
// can name a file the author recognises.
type Source struct {
	Path string
	Data []byte
}

// Severity classifies a Diagnostic.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is one problem found while parsing or validating an authored
// document.
type Diagnostic struct {
	Severity Severity
	Code     string // stable machine-readable code, e.g. "unknown-field"
	Path     string // authored file path
	Line     int    // 0 when the format cannot supply one
	Pointer  string // structural location, e.g. "tests[2].assertions.min_concurrency"
	Message  string
	Hint     string // optional remediation
}

// Report accumulates diagnostics across one or more documents.
type Report struct {
	Diagnostics []Diagnostic
}

// HasErrors reports whether the report contains at least one diagnostic of
// SeverityError.
func (r Report) HasErrors() bool {
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Add appends diagnostics to the report.
func (r *Report) Add(d ...Diagnostic) {
	r.Diagnostics = append(r.Diagnostics, d...)
}

// Merge appends another report's diagnostics onto r.
func (r *Report) Merge(other Report) {
	r.Diagnostics = append(r.Diagnostics, other.Diagnostics...)
}

// Sorted returns diagnostics in a deterministic order — by Path, then Line,
// then Pointer, then Code — so a report is diff-stable across runs.
func (r Report) Sorted() []Diagnostic {
	out := make([]Diagnostic, len(r.Diagnostics))
	copy(out, r.Diagnostics)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Pointer != b.Pointer {
			return a.Pointer < b.Pointer
		}
		return a.Code < b.Code
	})
	return out
}

// Error renders the sorted report; implements the error interface.
func (r Report) Error() string {
	sorted := r.Sorted()
	lines := make([]string, 0, len(sorted))
	for _, d := range sorted {
		lines = append(lines, d.Path+": "+d.Message)
	}
	return strings.Join(lines, "\n")
}
