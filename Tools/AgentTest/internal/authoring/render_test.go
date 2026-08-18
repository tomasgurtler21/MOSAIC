package authoring_test

// Tests for RenderReport and RenderDiagnostic: the shared diagnostic renderer
// that the CLI and TUI frontends both use. Tests verify that all five required
// fields appear in each rendered line, that diagnostics are emitted in
// Report.Sorted() order with no item dropped or truncated, that warnings and
// errors are distinguishable by severity text, that an empty report produces no
// output, and that optional fields (Line, Pointer, Hint) are omitted cleanly
// when absent.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/authoring"
)

// ── RenderDiagnostic ─────────────────────────────────────────────────────────

func TestRenderDiagnostic_AllFiveFields_PresentInOutput(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "unknown-field",
		Path:     "tests/run.suite.yaml",
		Line:     12,
		Pointer:  "tests[2].assertions.min_concurrency",
		Message:  "field is not recognised",
	}

	got := authoring.RenderDiagnostic(d)

	for _, want := range []string{"error", "unknown-field", "tests/run.suite.yaml", "tests[2].assertions.min_concurrency", "field is not recognised"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderDiagnostic: expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestRenderDiagnostic_ErrorSeverity_ContainsErrorKeyword(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "bad-field",
		Path:     "suite.yaml",
		Message:  "some error",
	}

	got := authoring.RenderDiagnostic(d)

	if !strings.Contains(got, "error") {
		t.Errorf("RenderDiagnostic: expected error diagnostic to contain \"error\", got: %s", got)
	}
}

func TestRenderDiagnostic_WarningSeverity_ContainsWarningKeyword(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityWarning,
		Code:     "deprecated-field",
		Path:     "suite.yaml",
		Message:  "renamed to timeout",
	}

	got := authoring.RenderDiagnostic(d)

	if !strings.Contains(got, "warning") {
		t.Errorf("RenderDiagnostic: expected warning diagnostic to contain \"warning\", got: %s", got)
	}
}

func TestRenderDiagnostic_WarningSeverity_DoesNotContainErrorKeyword(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityWarning,
		Code:     "deprecated-field",
		Path:     "suite.yaml",
		Message:  "renamed to timeout",
	}

	got := authoring.RenderDiagnostic(d)

	// "warning" must appear, "error" must not — so the two severities are unambiguously distinct
	if strings.Contains(got, "error") {
		t.Errorf("RenderDiagnostic: warning line must not contain \"error\", got: %s", got)
	}
}

func TestRenderDiagnostic_LineNumber_IncludedWhenNonZero(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "bad",
		Path:     "suite.yaml",
		Line:     7,
		Message:  "msg",
	}

	got := authoring.RenderDiagnostic(d)

	if !strings.Contains(got, ":7") {
		t.Errorf("RenderDiagnostic: expected line number 7 in colon-prefixed form \":7\" in output, got: %s", got)
	}
}

func TestRenderDiagnostic_LineNumberZero_NotIncluded(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "bad",
		Path:     "suite.yaml",
		Line:     0,
		Message:  "msg",
	}

	got := authoring.RenderDiagnostic(d)

	// Line == 0 means the format cannot supply a line; no ":0" should appear
	if strings.Contains(got, ":0") {
		t.Errorf("RenderDiagnostic: line 0 must not appear in output, got: %s", got)
	}
}

func TestRenderDiagnostic_Pointer_IncludedWhenNonEmpty(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "bad",
		Path:     "suite.yaml",
		Pointer:  "tests[1].timeout",
		Message:  "msg",
	}

	got := authoring.RenderDiagnostic(d)

	if !strings.Contains(got, "tests[1].timeout") {
		t.Errorf("RenderDiagnostic: expected pointer in output, got: %s", got)
	}
}

func TestRenderDiagnostic_Pointer_OmittedWhenEmpty(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "bad",
		Path:     "suite.yaml",
		Pointer:  "",
		Message:  "msg",
	}

	got := authoring.RenderDiagnostic(d)

	// No trailing or dangling separators from an absent pointer
	if strings.HasSuffix(got, " ") {
		t.Errorf("RenderDiagnostic: line must not end with a trailing space, got: %q", got)
	}
}

func TestRenderDiagnostic_Hint_IncludedWhenNonEmpty(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "bad",
		Path:     "suite.yaml",
		Message:  "field is not recognised",
		Hint:     "remove it or check the schema version",
	}

	got := authoring.RenderDiagnostic(d)

	if !strings.Contains(got, "remove it or check the schema version") {
		t.Errorf("RenderDiagnostic: expected hint text in output, got: %s", got)
	}
}

func TestRenderDiagnostic_Hint_OmittedWhenEmpty(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "bad",
		Path:     "suite.yaml",
		Message:  "msg",
		Hint:     "",
	}

	got := authoring.RenderDiagnostic(d)

	if strings.Contains(got, "hint:") {
		t.Errorf("RenderDiagnostic: no hint text but \"hint:\" appeared in output: %s", got)
	}
}

func TestRenderDiagnostic_EmptyCode_RendersPlaceholder(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "",
		Path:     "suite.yaml",
		Message:  "msg",
	}

	got := authoring.RenderDiagnostic(d)

	// Empty code must render as "?" so field position is never ambiguous
	if !strings.Contains(got, "?") {
		t.Errorf("RenderDiagnostic: empty Code must render as \"?\", got: %s", got)
	}
}

func TestRenderDiagnostic_EmptyPath_RendersPlaceholder(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "bad",
		Path:     "",
		Message:  "msg",
	}

	got := authoring.RenderDiagnostic(d)

	// Empty path must render as "<unknown>"
	if !strings.Contains(got, "<unknown>") {
		t.Errorf("RenderDiagnostic: empty Path must render as \"<unknown>\", got: %s", got)
	}
}

func TestRenderDiagnostic_NoTrailingNewline(t *testing.T) {
	d := authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "bad",
		Path:     "suite.yaml",
		Message:  "msg",
	}

	got := authoring.RenderDiagnostic(d)

	if strings.HasSuffix(got, "\n") {
		t.Errorf("RenderDiagnostic: returned string must not end with newline, got: %q", got)
	}
}

func TestRenderDiagnostic_ExactFormat_FullyPopulated(t *testing.T) {
	// Exact format: <severity>: <code>: <path>:<line> <pointer>: <message> (hint: <hint>)
	d := authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "unknown-field",
		Path:     "tests/first-run/first-run.suite.yaml",
		Line:     12,
		Pointer:  "tests[2].assertions.min_concurrency",
		Message:  "field is not recognised",
		Hint:     "remove it or check the schema version",
	}

	want := "error: unknown-field: tests/first-run/first-run.suite.yaml:12 tests[2].assertions.min_concurrency: field is not recognised (hint: remove it or check the schema version)"
	got := authoring.RenderDiagnostic(d)

	if got != want {
		t.Errorf("RenderDiagnostic exact format mismatch:\nwant: %s\n got: %s", want, got)
	}
}

func TestRenderDiagnostic_ExactFormat_NoOptionalFields(t *testing.T) {
	// Format without line, pointer, hint: <severity>: <code>: <path>: <message>
	d := authoring.Diagnostic{
		Severity: authoring.SeverityWarning,
		Code:     "deprecated-field",
		Path:     "tests/first-run/first-run.suite.yaml",
		Message:  "renamed to timeout",
	}

	want := "warning: deprecated-field: tests/first-run/first-run.suite.yaml: renamed to timeout"
	got := authoring.RenderDiagnostic(d)

	if got != want {
		t.Errorf("RenderDiagnostic exact format (no optional fields) mismatch:\nwant: %s\n got: %s", want, got)
	}
}

// ── RenderReport ─────────────────────────────────────────────────────────────

func TestRenderReport_EmptyReport_ReturnsEmptyString(t *testing.T) {
	rep := authoring.Report{}

	got := authoring.RenderReport(rep)

	if got != "" {
		t.Errorf("RenderReport: empty report must return \"\", got: %q", got)
	}
}

func TestRenderReport_SingleDiagnostic_AllFieldsPresent(t *testing.T) {
	rep := authoring.Report{
		Diagnostics: []authoring.Diagnostic{
			{
				Severity: authoring.SeverityError,
				Code:     "unknown-field",
				Path:     "suite.yaml",
				Pointer:  "tests[0].id",
				Message:  "id is required",
			},
		},
	}

	got := authoring.RenderReport(rep)

	for _, want := range []string{"error", "unknown-field", "suite.yaml", "tests[0].id", "id is required"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderReport: single diagnostic must contain %q, got:\n%s", want, got)
		}
	}
}

func TestRenderReport_MultipleDiagnostics_AllPresent(t *testing.T) {
	// Report with N diagnostics must produce exactly N lines — nothing dropped or truncated.
	const n = 10
	diags := make([]authoring.Diagnostic, n)
	for i := range diags {
		diags[i] = authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     strings.Repeat("a", i+1), // unique Code per entry keeps sort order deterministic
			Path:     "suite.yaml",
			Message:  strings.Repeat("x", i+1), // unique per entry
		}
	}

	rep := authoring.Report{Diagnostics: diags}
	got := authoring.RenderReport(rep)
	lines := strings.Split(got, "\n")

	if len(lines) != n {
		t.Errorf("RenderReport: expected %d lines for %d diagnostics, got %d lines:\n%s", n, n, len(lines), got)
	}
}

func TestRenderReport_LargeReport_NothingDropped(t *testing.T) {
	// Reports are unbounded by design; the renderer must not cap, truncate, or elide.
	const n = 100
	diags := make([]authoring.Diagnostic, n)
	for i := range diags {
		diags[i] = authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     strings.Repeat("b", i+1), // unique Code per entry keeps sort order deterministic
			Path:     "suite.yaml",
			// unique message so we can count occurrences
			Message: strings.Repeat("y", i+1),
		}
	}

	rep := authoring.Report{Diagnostics: diags}
	got := authoring.RenderReport(rep)
	lines := strings.Split(got, "\n")

	if len(lines) != n {
		t.Errorf("RenderReport large report: expected %d lines, got %d", n, len(lines))
	}
}

func TestRenderReport_Order_FollowsSortedOrder(t *testing.T) {
	// Add diagnostics in reverse-alphabetical path order; Sorted() must reorder them.
	rep := authoring.Report{
		Diagnostics: []authoring.Diagnostic{
			{Severity: authoring.SeverityError, Code: "a", Path: "z-suite.yaml", Message: "msg-z"},
			{Severity: authoring.SeverityError, Code: "a", Path: "a-suite.yaml", Message: "msg-a"},
			{Severity: authoring.SeverityError, Code: "a", Path: "m-suite.yaml", Message: "msg-m"},
		},
	}

	got := authoring.RenderReport(rep)
	lines := strings.Split(got, "\n")

	if len(lines) != 3 {
		t.Fatalf("RenderReport: expected 3 lines, got %d: %s", len(lines), got)
	}

	// After Sorted(), order must be a-suite → m-suite → z-suite
	if !strings.Contains(lines[0], "a-suite.yaml") {
		t.Errorf("RenderReport order: line 0 must be a-suite.yaml, got: %s", lines[0])
	}
	if !strings.Contains(lines[1], "m-suite.yaml") {
		t.Errorf("RenderReport order: line 1 must be m-suite.yaml, got: %s", lines[1])
	}
	if !strings.Contains(lines[2], "z-suite.yaml") {
		t.Errorf("RenderReport order: line 2 must be z-suite.yaml, got: %s", lines[2])
	}
}

func TestRenderReport_Order_MatchesReportSortedExactly(t *testing.T) {
	// RenderReport must not re-sort; it must use exactly Report.Sorted() order.
	rep := authoring.Report{
		Diagnostics: []authoring.Diagnostic{
			{Severity: authoring.SeverityError, Code: "b", Path: "file.yaml", Line: 5, Message: "second"},
			{Severity: authoring.SeverityError, Code: "a", Path: "file.yaml", Line: 2, Message: "first"},
			{Severity: authoring.SeverityError, Code: "c", Path: "file.yaml", Line: 9, Message: "third"},
		},
	}

	sorted := rep.Sorted()
	got := authoring.RenderReport(rep)
	lines := strings.Split(got, "\n")

	if len(lines) != len(sorted) {
		t.Fatalf("RenderReport: line count %d != sorted count %d", len(lines), len(sorted))
	}
	for i, line := range lines {
		if !strings.Contains(line, sorted[i].Message) {
			t.Errorf("RenderReport: line %d must contain message %q (per Sorted order), got: %s",
				i, sorted[i].Message, line)
		}
	}
}

func TestRenderReport_WarningAndError_BothDistinguishable(t *testing.T) {
	rep := authoring.Report{
		Diagnostics: []authoring.Diagnostic{
			{Severity: authoring.SeverityError, Code: "bad", Path: "a.yaml", Message: "error-msg"},
			{Severity: authoring.SeverityWarning, Code: "warn", Path: "b.yaml", Message: "warn-msg"},
		},
	}

	got := authoring.RenderReport(rep)

	if !strings.Contains(got, "error") {
		t.Errorf("RenderReport: output must contain \"error\" for error diagnostics, got:\n%s", got)
	}
	if !strings.Contains(got, "warning") {
		t.Errorf("RenderReport: output must contain \"warning\" for warning diagnostics, got:\n%s", got)
	}
}

func TestRenderReport_WarningOnly_ContainsWarningNotError(t *testing.T) {
	rep := authoring.Report{
		Diagnostics: []authoring.Diagnostic{
			{Severity: authoring.SeverityWarning, Code: "deprecated", Path: "suite.yaml", Message: "renamed"},
		},
	}

	got := authoring.RenderReport(rep)

	if !strings.Contains(got, "warning") {
		t.Errorf("RenderReport: warning-only report must contain \"warning\", got: %s", got)
	}
	if strings.Contains(got, "error") {
		t.Errorf("RenderReport: warning-only report must not contain \"error\", got: %s", got)
	}
}

func TestRenderReport_NoTrailingNewline(t *testing.T) {
	rep := authoring.Report{
		Diagnostics: []authoring.Diagnostic{
			{Severity: authoring.SeverityError, Code: "bad", Path: "suite.yaml", Message: "msg"},
		},
	}

	got := authoring.RenderReport(rep)

	if strings.HasSuffix(got, "\n") {
		t.Errorf("RenderReport: must not end with trailing newline, got: %q", got)
	}
}

func TestRenderReport_MultiLine_JoinedWithNewline(t *testing.T) {
	rep := authoring.Report{
		Diagnostics: []authoring.Diagnostic{
			{Severity: authoring.SeverityError, Code: "a", Path: "a.yaml", Message: "first"},
			{Severity: authoring.SeverityError, Code: "b", Path: "b.yaml", Message: "second"},
		},
	}

	got := authoring.RenderReport(rep)

	// Two diagnostics → exactly one "\n" separating two lines
	parts := strings.Split(got, "\n")
	if len(parts) != 2 {
		t.Errorf("RenderReport: two diagnostics must produce two lines joined by \"\\n\", got %d parts: %q", len(parts), got)
	}
}
