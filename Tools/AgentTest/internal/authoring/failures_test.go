package authoring_test

// Tests for RunFailureReport and ReportWriteFailureReport: the two structured
// diagnostic constructors that produce authoring.Report values for suite-run
// failures and report-write failures respectively.
//
// All tests use a test-local stub type to satisfy authoring.StructuredFailure so
// that this package's tests remain independent of internal/agentdeploy. The
// structured case is exercised through that stub; assertions target Code,
// Severity, Pointer, Path and the *presence* of field content rather than exact
// composed message strings.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"mosaic-agent-test/internal/authoring"
)

// stubStructuredFailure is a test-local type that satisfies authoring.StructuredFailure.
// Using a local stub keeps authoring tests independent of the agentdeploy package.
type stubStructuredFailure struct {
	reason      string
	detail      string
	toolMessage string
	exitCode    int
}

var _ authoring.StructuredFailure = (*stubStructuredFailure)(nil)

func (s *stubStructuredFailure) FailureReason() string      { return s.reason }
func (s *stubStructuredFailure) FailureDetail() string      { return s.detail }
func (s *stubStructuredFailure) FailureToolMessage() string { return s.toolMessage }
func (s *stubStructuredFailure) FailureExitCode() int       { return s.exitCode }
func (s *stubStructuredFailure) Error() string {
	return fmt.Sprintf("stub failure: reason=%q detail=%q exitCode=%d", s.reason, s.detail, s.exitCode)
}

// ── test helpers ──────────────────────────────────────────────────────────────

// findDiagnosticByPointer returns the first diagnostic with the given Pointer,
// or nil if none is found.
func findDiagnosticByPointer(rep authoring.Report, pointer string) *authoring.Diagnostic {
	for i := range rep.Diagnostics {
		if rep.Diagnostics[i].Pointer == pointer {
			return &rep.Diagnostics[i]
		}
	}
	return nil
}

// collectPointers returns a set of all Pointer values in the report.
func collectPointers(rep authoring.Report) map[string]struct{} {
	m := make(map[string]struct{}, len(rep.Diagnostics))
	for _, d := range rep.Diagnostics {
		m[d.Pointer] = struct{}{}
	}
	return m
}

// pointerKeys returns the keys of a pointer set, for error messages.
func pointerKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ── RunFailureReport: nil and plain-error cases ───────────────────────────────

func TestRunFailureReport_NilError_ReturnsZeroReport(t *testing.T) {
	rep := authoring.RunFailureReport("suite/path", nil)
	if len(rep.Diagnostics) != 0 {
		t.Errorf("RunFailureReport(nil): expected zero Report, got %d diagnostics", len(rep.Diagnostics))
	}
}

func TestRunFailureReport_PlainError_ProducesSingleDiagnostic(t *testing.T) {
	err := errors.New("something went wrong")
	rep := authoring.RunFailureReport("suite/path", err)
	if len(rep.Diagnostics) != 1 {
		t.Errorf("RunFailureReport(plain error): expected 1 diagnostic, got %d", len(rep.Diagnostics))
	}
}

func TestRunFailureReport_PlainError_HasSuiteRunFailedCode(t *testing.T) {
	err := errors.New("something went wrong")
	rep := authoring.RunFailureReport("suite/path", err)
	if len(rep.Diagnostics) == 0 {
		t.Fatal("RunFailureReport(plain error): expected at least one diagnostic")
	}
	if rep.Diagnostics[0].Code != "suite-run-failed" {
		t.Errorf("RunFailureReport(plain error): Code = %q, want %q", rep.Diagnostics[0].Code, "suite-run-failed")
	}
}

func TestRunFailureReport_PlainError_HasErrorSeverity(t *testing.T) {
	err := errors.New("something went wrong")
	rep := authoring.RunFailureReport("suite/path", err)
	if len(rep.Diagnostics) == 0 {
		t.Fatal("RunFailureReport(plain error): expected at least one diagnostic")
	}
	if rep.Diagnostics[0].Severity != authoring.SeverityError {
		t.Errorf("RunFailureReport(plain error): Severity = %q, want %q", rep.Diagnostics[0].Severity, authoring.SeverityError)
	}
}

func TestRunFailureReport_PlainError_PathIsSuitePath(t *testing.T) {
	const suitePath = "path/to/my.suite.yaml"
	err := errors.New("something went wrong")
	rep := authoring.RunFailureReport(suitePath, err)
	if len(rep.Diagnostics) == 0 {
		t.Fatal("RunFailureReport(plain error): expected at least one diagnostic")
	}
	if rep.Diagnostics[0].Path != suitePath {
		t.Errorf("RunFailureReport(plain error): Path = %q, want %q", rep.Diagnostics[0].Path, suitePath)
	}
}

func TestRunFailureReport_PlainError_PointerIsFailure(t *testing.T) {
	err := errors.New("something went wrong")
	rep := authoring.RunFailureReport("suite/path", err)
	if len(rep.Diagnostics) == 0 {
		t.Fatal("RunFailureReport(plain error): expected at least one diagnostic")
	}
	if rep.Diagnostics[0].Pointer != "failure" {
		t.Errorf("RunFailureReport(plain error): Pointer = %q, want %q", rep.Diagnostics[0].Pointer, "failure")
	}
}

func TestRunFailureReport_PlainError_MessageContainsErrText(t *testing.T) {
	const errMsg = "something went wrong specifically"
	err := errors.New(errMsg)
	rep := authoring.RunFailureReport("suite/path", err)
	if len(rep.Diagnostics) == 0 {
		t.Fatal("RunFailureReport(plain error): expected at least one diagnostic")
	}
	if !strings.Contains(rep.Diagnostics[0].Message, errMsg) {
		t.Errorf("RunFailureReport(plain error): Message %q does not contain error text %q", rep.Diagnostics[0].Message, errMsg)
	}
}

func TestRunFailureReport_PlainError_ReportHasErrors(t *testing.T) {
	err := errors.New("something went wrong")
	rep := authoring.RunFailureReport("suite/path", err)
	if !rep.HasErrors() {
		t.Error("RunFailureReport(plain error): HasErrors() must be true")
	}
}

// ── RunFailureReport: structured-failure cases ────────────────────────────────

func TestRunFailureReport_StructuredFailure_LeadDiagnosticPresent(t *testing.T) {
	err := &stubStructuredFailure{reason: "deploy-failed", detail: "agent.yaml", toolMessage: "error: not found", exitCode: 1}
	rep := authoring.RunFailureReport("suite/path", err)

	pointers := collectPointers(rep)
	if _, ok := pointers["failure"]; !ok {
		t.Errorf("RunFailureReport(structured): lead diagnostic with pointer %q missing; got pointers: %v", "failure", pointerKeys(pointers))
	}
}

func TestRunFailureReport_StructuredFailure_ReasonDiagnosticPresent(t *testing.T) {
	const reason = "deploy-failed"
	err := &stubStructuredFailure{reason: reason, detail: "agent.yaml", toolMessage: "error msg", exitCode: 1}
	rep := authoring.RunFailureReport("suite/path", err)

	d := findDiagnosticByPointer(rep, "failure.reason")
	if d == nil {
		t.Errorf("RunFailureReport(structured): no diagnostic with pointer %q", "failure.reason")
		return
	}
	if !strings.Contains(d.Message, reason) {
		t.Errorf("RunFailureReport(structured): failure.reason Message %q does not contain reason %q", d.Message, reason)
	}
}

func TestRunFailureReport_StructuredFailure_DetailDiagnosticPresent(t *testing.T) {
	const detail = "path/to/agent.yaml"
	err := &stubStructuredFailure{reason: "deploy-failed", detail: detail, toolMessage: "error msg", exitCode: 1}
	rep := authoring.RunFailureReport("suite/path", err)

	d := findDiagnosticByPointer(rep, "failure.detail")
	if d == nil {
		t.Errorf("RunFailureReport(structured): no diagnostic with pointer %q", "failure.detail")
		return
	}
	if !strings.Contains(d.Message, detail) {
		t.Errorf("RunFailureReport(structured): failure.detail Message %q does not contain detail %q", d.Message, detail)
	}
}

func TestRunFailureReport_StructuredFailure_ToolOutputDiagnosticPresent(t *testing.T) {
	const toolMsg = "delegate said: something is broken"
	err := &stubStructuredFailure{reason: "deploy-failed", detail: "agent.yaml", toolMessage: toolMsg, exitCode: 1}
	rep := authoring.RunFailureReport("suite/path", err)

	d := findDiagnosticByPointer(rep, "failure.tool_output")
	if d == nil {
		t.Errorf("RunFailureReport(structured): no diagnostic with pointer %q", "failure.tool_output")
		return
	}
	if !strings.Contains(d.Message, toolMsg) {
		t.Errorf("RunFailureReport(structured): failure.tool_output Message %q does not contain tool message %q", d.Message, toolMsg)
	}
}

func TestRunFailureReport_StructuredFailure_ExitCodeDiagnosticPresent(t *testing.T) {
	err := &stubStructuredFailure{reason: "deploy-failed", detail: "agent.yaml", toolMessage: "error msg", exitCode: 42}
	rep := authoring.RunFailureReport("suite/path", err)

	d := findDiagnosticByPointer(rep, "failure.exit_code")
	if d == nil {
		t.Errorf("RunFailureReport(structured): no diagnostic with pointer %q", "failure.exit_code")
		return
	}
	if !strings.Contains(d.Message, "42") {
		t.Errorf("RunFailureReport(structured): failure.exit_code Message %q does not contain exit code 42", d.Message)
	}
}

func TestRunFailureReport_StructuredFailure_EmptyReason_ReasonDiagnosticAbsent(t *testing.T) {
	// AD-5: an empty Reason is omitted rather than rendered as an empty value.
	err := &stubStructuredFailure{reason: "", detail: "agent.yaml", toolMessage: "error msg", exitCode: 1}
	rep := authoring.RunFailureReport("suite/path", err)

	// Guard: assert the implementation produced output at all (the lead diagnostic
	// must be present even when reason is empty), then verify reason is absent.
	if findDiagnosticByPointer(rep, "failure") == nil {
		t.Fatal("RunFailureReport(structured, empty reason): expected lead 'failure' diagnostic to be present, proving the implementation produced output")
	}
	if d := findDiagnosticByPointer(rep, "failure.reason"); d != nil {
		t.Errorf("RunFailureReport(structured, empty reason): unexpected failure.reason diagnostic: %+v", d)
	}
}

func TestRunFailureReport_StructuredFailure_EmptyDetail_DetailDiagnosticAbsent(t *testing.T) {
	// AD-5: an empty Detail is omitted rather than rendered as an empty value.
	err := &stubStructuredFailure{reason: "deploy-failed", detail: "", toolMessage: "error msg", exitCode: 1}
	rep := authoring.RunFailureReport("suite/path", err)

	// Guard: assert the implementation produced output at all (the lead diagnostic
	// must be present even when detail is empty), then verify detail is absent.
	if findDiagnosticByPointer(rep, "failure") == nil {
		t.Fatal("RunFailureReport(structured, empty detail): expected lead 'failure' diagnostic to be present, proving the implementation produced output")
	}
	if d := findDiagnosticByPointer(rep, "failure.detail"); d != nil {
		t.Errorf("RunFailureReport(structured, empty detail): unexpected failure.detail diagnostic: %+v", d)
	}
}

func TestRunFailureReport_StructuredFailure_EmptyToolMessage_ToolOutputDiagnosticAbsent(t *testing.T) {
	// AD-5: an empty ToolMessage is omitted rather than rendered as an empty value.
	err := &stubStructuredFailure{reason: "deploy-failed", detail: "agent.yaml", toolMessage: "", exitCode: 1}
	rep := authoring.RunFailureReport("suite/path", err)

	// Guard: assert the implementation produced output at all (the lead diagnostic
	// must be present even when toolMessage is empty), then verify tool_output is absent.
	if findDiagnosticByPointer(rep, "failure") == nil {
		t.Fatal("RunFailureReport(structured, empty tool message): expected lead 'failure' diagnostic to be present, proving the implementation produced output")
	}
	if d := findDiagnosticByPointer(rep, "failure.tool_output"); d != nil {
		t.Errorf("RunFailureReport(structured, empty tool message): unexpected failure.tool_output diagnostic: %+v", d)
	}
}

func TestRunFailureReport_StructuredFailure_ZeroExitCode_ExitCodeDiagnosticAbsent(t *testing.T) {
	// Exit code 0 is omitted; a zero exit code does not indicate failure.
	err := &stubStructuredFailure{reason: "deploy-failed", detail: "agent.yaml", toolMessage: "error msg", exitCode: 0}
	rep := authoring.RunFailureReport("suite/path", err)

	// Guard: assert the implementation produced output at all (the lead diagnostic
	// must be present even when exitCode is zero), then verify exit_code is absent.
	if findDiagnosticByPointer(rep, "failure") == nil {
		t.Fatal("RunFailureReport(structured, exit code 0): expected lead 'failure' diagnostic to be present, proving the implementation produced output")
	}
	if d := findDiagnosticByPointer(rep, "failure.exit_code"); d != nil {
		t.Errorf("RunFailureReport(structured, exit code 0): unexpected failure.exit_code diagnostic: %+v", d)
	}
}

func TestRunFailureReport_StructuredFailure_EmptyReason_LeadDiagnosticHasHint(t *testing.T) {
	// When FailureReason is empty the tool reported no structured reason.
	// The lead diagnostic must carry a hint stating this so the user knows that
	// the delegate's own output is the only detail available.
	err := &stubStructuredFailure{reason: "", toolMessage: "raw output line", exitCode: 1}
	rep := authoring.RunFailureReport("suite/path", err)

	d := findDiagnosticByPointer(rep, "failure")
	if d == nil {
		t.Fatal("RunFailureReport(empty reason): no lead diagnostic found")
	}
	if d.Hint == "" {
		t.Error("RunFailureReport(empty reason): lead diagnostic must carry a hint when reason is empty, Hint was empty")
	}
}

func TestRunFailureReport_StructuredFailure_AllDiagnosticsHaveSuiteRunFailedCode(t *testing.T) {
	// Every diagnostic in a run-failure report shares the same machine-readable code.
	err := &stubStructuredFailure{reason: "deploy-failed", detail: "agent.yaml", toolMessage: "error msg", exitCode: 1}
	rep := authoring.RunFailureReport("suite/path", err)

	// Guard: assert the implementation produced output at all before iterating.
	// Without this guard the loop body never executes against an empty report,
	// making the test vacuously pass.
	if len(rep.Diagnostics) == 0 {
		t.Fatal("RunFailureReport(structured): expected at least one diagnostic; got empty report")
	}
	for i, d := range rep.Diagnostics {
		if d.Code != "suite-run-failed" {
			t.Errorf("RunFailureReport(structured): diagnostic[%d].Code = %q, want %q", i, d.Code, "suite-run-failed")
		}
	}
}

func TestRunFailureReport_StructuredFailure_AllDiagnosticsHaveErrorSeverity(t *testing.T) {
	// AD-4: every diagnostic in these reports carries SeverityError.
	err := &stubStructuredFailure{reason: "deploy-failed", detail: "agent.yaml", toolMessage: "error msg", exitCode: 1}
	rep := authoring.RunFailureReport("suite/path", err)

	// Guard: assert the implementation produced output at all before iterating.
	// Without this guard the loop body never executes against an empty report,
	// making the test vacuously pass.
	if len(rep.Diagnostics) == 0 {
		t.Fatal("RunFailureReport(structured): expected at least one diagnostic; got empty report")
	}
	for i, d := range rep.Diagnostics {
		if d.Severity != authoring.SeverityError {
			t.Errorf("RunFailureReport(structured): diagnostic[%d].Severity = %q, want %q", i, d.Severity, authoring.SeverityError)
		}
	}
}

func TestRunFailureReport_StructuredFailure_AllDiagnosticsUseSuitePath(t *testing.T) {
	const suitePath = "path/to/suite.yaml"
	err := &stubStructuredFailure{reason: "deploy-failed", detail: "agent.yaml", toolMessage: "error msg", exitCode: 1}
	rep := authoring.RunFailureReport(suitePath, err)

	// Guard: assert the implementation produced output at all before iterating.
	// Without this guard the loop body never executes against an empty report,
	// making the test vacuously pass.
	if len(rep.Diagnostics) == 0 {
		t.Fatal("RunFailureReport(structured): expected at least one diagnostic; got empty report")
	}
	for i, d := range rep.Diagnostics {
		if d.Path != suitePath {
			t.Errorf("RunFailureReport(structured): diagnostic[%d].Path = %q, want %q", i, d.Path, suitePath)
		}
	}
}

func TestRunFailureReport_StructuredFailure_NoDiagnosticContainsMoreThanOneFieldValue(t *testing.T) {
	// AD-3: each field value gets its own diagnostic. No single diagnostic may
	// carry more than one of the four structured-failure field values, because
	// that would indicate interpolation rather than individual visibility.
	const reason = "reason-code"
	const detail = "detail-value"
	const toolMsg = "tool-message-text"
	const exitCode = 99

	err := &stubStructuredFailure{reason: reason, detail: detail, toolMessage: toolMsg, exitCode: exitCode}
	rep := authoring.RunFailureReport("suite/path", err)

	// Guard: assert the implementation produced output at all before iterating.
	// Without this guard the loop body never executes against an empty report,
	// making the test vacuously pass.
	if len(rep.Diagnostics) == 0 {
		t.Fatal("RunFailureReport(structured): expected at least one diagnostic; got empty report")
	}
	for i, d := range rep.Diagnostics {
		count := 0
		if strings.Contains(d.Message, reason) {
			count++
		}
		if strings.Contains(d.Message, detail) {
			count++
		}
		if strings.Contains(d.Message, toolMsg) {
			count++
		}
		if strings.Contains(d.Message, fmt.Sprintf("%d", exitCode)) {
			count++
		}
		if count > 1 {
			t.Errorf("RunFailureReport(structured): diagnostic[%d] contains %d field values (want at most 1); Message=%q", i, count, d.Message)
		}
	}
}

func TestRunFailureReport_EmptySuitePath_ProducesWellFormedReport(t *testing.T) {
	// An empty suitePath is contractually valid; the renderer substitutes "<unknown>".
	err := errors.New("something went wrong")
	rep := authoring.RunFailureReport("", err)

	if len(rep.Diagnostics) == 0 {
		t.Error("RunFailureReport(empty path): must produce at least one diagnostic")
	}
	for i, d := range rep.Diagnostics {
		if d.Code == "" {
			t.Errorf("RunFailureReport(empty path): diagnostic[%d].Code is empty", i)
		}
		if d.Severity == "" {
			t.Errorf("RunFailureReport(empty path): diagnostic[%d].Severity is empty", i)
		}
	}
}

func TestRunFailureReport_StructuredFailure_WrappedError_FieldsExtracted(t *testing.T) {
	// errors.As must traverse wrapping, so a *stubStructuredFailure buried inside
	// fmt.Errorf("%w") still surfaces its fields as individual diagnostics.
	const reason = "wrapped-reason"
	inner := &stubStructuredFailure{reason: reason, detail: "d", toolMessage: "msg", exitCode: 2}
	wrapped := fmt.Errorf("outer wrapper: %w", inner)

	rep := authoring.RunFailureReport("suite/path", wrapped)

	d := findDiagnosticByPointer(rep, "failure.reason")
	if d == nil {
		t.Errorf("RunFailureReport(wrapped structured): expected failure.reason diagnostic for wrapped error")
		return
	}
	if !strings.Contains(d.Message, reason) {
		t.Errorf("RunFailureReport(wrapped structured): failure.reason Message %q does not contain %q", d.Message, reason)
	}
}

// ── ReportWriteFailureReport: nil and write-failed cases ─────────────────────

func TestReportWriteFailureReport_NilError_ReturnsZeroReport(t *testing.T) {
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", nil)
	if len(rep.Diagnostics) != 0 {
		t.Errorf("ReportWriteFailureReport(nil): expected zero Report, got %d diagnostics", len(rep.Diagnostics))
	}
}

func TestReportWriteFailureReport_WriteFailed_CodeIsReportUnwritable(t *testing.T) {
	err := errors.New("permission denied")
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", err)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(write failed): expected at least one diagnostic")
	}
	if rep.Diagnostics[0].Code != "report-unwritable" {
		t.Errorf("ReportWriteFailureReport(write failed): Code = %q, want %q", rep.Diagnostics[0].Code, "report-unwritable")
	}
}

func TestReportWriteFailureReport_WriteFailed_SeverityIsError(t *testing.T) {
	err := errors.New("permission denied")
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", err)

	// Guard: assert the implementation produced output at all before iterating.
	// Without this guard the loop body never executes against an empty report,
	// making the test vacuously pass.
	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(write failed): expected at least one diagnostic; got empty report")
	}
	for i, d := range rep.Diagnostics {
		if d.Severity != authoring.SeverityError {
			t.Errorf("ReportWriteFailureReport(write failed): diagnostic[%d].Severity = %q, want %q", i, d.Severity, authoring.SeverityError)
		}
	}
}

func TestReportWriteFailureReport_WriteFailed_PathIsReportPath(t *testing.T) {
	const reportPath = "/var/output/test-report.json"
	err := errors.New("permission denied")
	rep := authoring.ReportWriteFailureReport(reportPath, err)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(write failed): expected at least one diagnostic")
	}
	if rep.Diagnostics[0].Path != reportPath {
		t.Errorf("ReportWriteFailureReport(write failed): Path = %q, want %q", rep.Diagnostics[0].Path, reportPath)
	}
}

func TestReportWriteFailureReport_WriteFailed_MessageContainsErrText(t *testing.T) {
	const errMsg = "disk quota exceeded"
	err := errors.New(errMsg)
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", err)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(write failed): expected at least one diagnostic")
	}
	if !strings.Contains(rep.Diagnostics[0].Message, errMsg) {
		t.Errorf("ReportWriteFailureReport(write failed): Message %q does not contain error text %q", rep.Diagnostics[0].Message, errMsg)
	}
}

func TestReportWriteFailureReport_WriteFailed_PointerIsFailure(t *testing.T) {
	err := errors.New("permission denied")
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", err)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(write failed): expected at least one diagnostic")
	}
	if rep.Diagnostics[0].Pointer != "failure" {
		t.Errorf("ReportWriteFailureReport(write failed): Pointer = %q, want %q", rep.Diagnostics[0].Pointer, "failure")
	}
}

func TestReportWriteFailureReport_WriteFailed_HintNamesReportPathFlag(t *testing.T) {
	err := errors.New("permission denied")
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", err)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(write failed): expected at least one diagnostic")
	}
	if !strings.Contains(rep.Diagnostics[0].Hint, "--report-path") {
		t.Errorf("ReportWriteFailureReport(write failed): Hint %q must mention --report-path", rep.Diagnostics[0].Hint)
	}
}

func TestReportWriteFailureReport_WriteFailed_HintNamesNoReportFlag(t *testing.T) {
	err := errors.New("permission denied")
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", err)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(write failed): expected at least one diagnostic")
	}
	if !strings.Contains(rep.Diagnostics[0].Hint, "--no-report") {
		t.Errorf("ReportWriteFailureReport(write failed): Hint %q must mention --no-report", rep.Diagnostics[0].Hint)
	}
}

func TestReportWriteFailureReport_WriteFailed_HintNamesTUIInlineEditor(t *testing.T) {
	// The design's normative hint content requires naming three override mechanisms:
	// --report-path, --no-report, and the TUI's inline report-path editor (the 'e'
	// key on the suite-select screen). This test asserts the third element is present.
	// Exact wording is left to the implementer; this assertion checks for language
	// that naturally identifies an inline/interactive editor (edit, inline, or TUI).
	err := errors.New("permission denied")
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", err)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(write failed): expected at least one diagnostic")
	}
	hint := rep.Diagnostics[0].Hint
	hasTUIEditorReference := strings.Contains(hint, "edit") ||
		strings.Contains(hint, "inline") ||
		strings.Contains(hint, "TUI")
	if !hasTUIEditorReference {
		t.Errorf("ReportWriteFailureReport(write failed): Hint %q must mention the TUI inline report-path editor; expected hint to contain \"edit\", \"inline\", or \"TUI\"", hint)
	}
}

func TestReportWriteFailureReport_WriteFailed_ReportHasErrors(t *testing.T) {
	err := errors.New("permission denied")
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", err)
	if !rep.HasErrors() {
		t.Error("ReportWriteFailureReport(write failed): HasErrors() must be true")
	}
}

// ── ReportWriteFailureReport: unconfigured-writer case ───────────────────────

func TestReportWriteFailureReport_UnconfiguredWriter_CodeIsReportWriterUnconfigured(t *testing.T) {
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", authoring.ErrReportWriterUnconfigured)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(unconfigured): expected at least one diagnostic")
	}
	if rep.Diagnostics[0].Code != "report-writer-unconfigured" {
		t.Errorf("ReportWriteFailureReport(unconfigured): Code = %q, want %q", rep.Diagnostics[0].Code, "report-writer-unconfigured")
	}
}

func TestReportWriteFailureReport_UnconfiguredWriter_DistinctFromGenericWriteError(t *testing.T) {
	// AD-6: ErrReportWriterUnconfigured is a distinct condition, not a generic write error.
	unconfiguredRep := authoring.ReportWriteFailureReport("/path/to/report.json", authoring.ErrReportWriterUnconfigured)
	genericRep := authoring.ReportWriteFailureReport("/path/to/report.json", errors.New("disk error"))

	if len(unconfiguredRep.Diagnostics) == 0 || len(genericRep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport: expected diagnostics from both calls")
	}
	if unconfiguredRep.Diagnostics[0].Code == genericRep.Diagnostics[0].Code {
		t.Errorf("ReportWriteFailureReport: unconfigured writer and generic write error must produce different codes, both got %q", unconfiguredRep.Diagnostics[0].Code)
	}
}

func TestReportWriteFailureReport_UnconfiguredWriter_HintNamesReportPathFlag(t *testing.T) {
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", authoring.ErrReportWriterUnconfigured)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(unconfigured): expected at least one diagnostic")
	}
	if !strings.Contains(rep.Diagnostics[0].Hint, "--report-path") {
		t.Errorf("ReportWriteFailureReport(unconfigured): Hint %q must mention --report-path", rep.Diagnostics[0].Hint)
	}
}

func TestReportWriteFailureReport_UnconfiguredWriter_HintNamesNoReportFlag(t *testing.T) {
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", authoring.ErrReportWriterUnconfigured)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(unconfigured): expected at least one diagnostic")
	}
	if !strings.Contains(rep.Diagnostics[0].Hint, "--no-report") {
		t.Errorf("ReportWriteFailureReport(unconfigured): Hint %q must mention --no-report", rep.Diagnostics[0].Hint)
	}
}

func TestReportWriteFailureReport_UnconfiguredWriter_HintNamesTUIInlineEditor(t *testing.T) {
	// The design's normative hint content applies equally to the unconfigured-writer
	// case: all three override mechanisms must be named (--report-path, --no-report,
	// and the TUI's inline report-path editor). This test asserts the third element.
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", authoring.ErrReportWriterUnconfigured)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(unconfigured): expected at least one diagnostic")
	}
	hint := rep.Diagnostics[0].Hint
	hasTUIEditorReference := strings.Contains(hint, "edit") ||
		strings.Contains(hint, "inline") ||
		strings.Contains(hint, "TUI")
	if !hasTUIEditorReference {
		t.Errorf("ReportWriteFailureReport(unconfigured): Hint %q must mention the TUI inline report-path editor; expected hint to contain \"edit\", \"inline\", or \"TUI\"", hint)
	}
}

func TestReportWriteFailureReport_UnconfiguredWriter_PathIsReportPath(t *testing.T) {
	const reportPath = "/output/test-report.json"
	rep := authoring.ReportWriteFailureReport(reportPath, authoring.ErrReportWriterUnconfigured)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(unconfigured): expected at least one diagnostic")
	}
	if rep.Diagnostics[0].Path != reportPath {
		t.Errorf("ReportWriteFailureReport(unconfigured): Path = %q, want %q", rep.Diagnostics[0].Path, reportPath)
	}
}

func TestReportWriteFailureReport_WrappedUnconfigured_DetectedViaErrorsIs(t *testing.T) {
	// errors.Is must traverse wrapping so a wrapped ErrReportWriterUnconfigured
	// is still reported as the unconfigured condition, not a generic write error.
	wrapped := fmt.Errorf("write seam: %w", authoring.ErrReportWriterUnconfigured)
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", wrapped)

	if len(rep.Diagnostics) == 0 {
		t.Fatal("ReportWriteFailureReport(wrapped unconfigured): expected at least one diagnostic")
	}
	if rep.Diagnostics[0].Code != "report-writer-unconfigured" {
		t.Errorf("ReportWriteFailureReport(wrapped unconfigured): Code = %q, want %q (errors.Is must traverse wrapping)", rep.Diagnostics[0].Code, "report-writer-unconfigured")
	}
}

// ── Rendering through RenderReport ───────────────────────────────────────────

func TestRunFailureReport_Rendered_StructuredFields_EachIndividuallyVisible(t *testing.T) {
	// AC2.3: reason, detail, tool message and exit code each individually present
	// in the rendered output — not collapsed into a single interpolated line.
	const reason = "render-failed"
	const detail = "path/to/workflow.yaml"
	const toolMsg = "delegate diagnostic line"
	const exitCode = 5

	err := &stubStructuredFailure{reason: reason, detail: detail, toolMessage: toolMsg, exitCode: exitCode}
	rep := authoring.RunFailureReport("suite/path", err)
	rendered := authoring.RenderReport(rep)

	for _, want := range []string{reason, detail, toolMsg, "5"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("RunFailureReport rendered: expected %q to appear in output; got:\n%s", want, rendered)
		}
	}
}

func TestRunFailureReport_Rendered_MultilineToolMessage_NotTruncated(t *testing.T) {
	// AC2.6: multi-line delegate output must be preserved verbatim; nothing truncated.
	const line1 = "delegate line one"
	const line2 = "delegate line two"
	const line3 = "delegate line three"
	toolMsg := line1 + "\n" + line2 + "\n" + line3

	err := &stubStructuredFailure{reason: "render-failed", toolMessage: toolMsg, exitCode: 1}
	rep := authoring.RunFailureReport("suite/path", err)
	rendered := authoring.RenderReport(rep)

	for _, want := range []string{line1, line2, line3} {
		if !strings.Contains(rendered, want) {
			t.Errorf("RunFailureReport rendered (multiline tool message): %q missing from output; got:\n%s", want, rendered)
		}
	}
}

func TestRunFailureReport_Rendered_UsesEstablishedLineFormat(t *testing.T) {
	// Each rendered line must follow the established format:
	// <severity>: <code>: <path>[ <pointer>]: <message>[ (hint: <hint>)]
	err := errors.New("plain error")
	rep := authoring.RunFailureReport("suite/my.suite.yaml", err)
	rendered := authoring.RenderReport(rep)

	for _, want := range []string{"error:", "suite-run-failed:", "suite/my.suite.yaml"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("RunFailureReport rendered: expected %q in output (established line format); got:\n%s", want, rendered)
		}
	}
}

func TestRunFailureReport_Rendered_EmptyPath_UsesUnknownPlaceholder(t *testing.T) {
	// When suitePath is empty the renderer substitutes "<unknown>" per RenderDiagnostic contract.
	err := errors.New("plain error")
	rep := authoring.RunFailureReport("", err)
	rendered := authoring.RenderReport(rep)

	if !strings.Contains(rendered, "<unknown>") {
		t.Errorf("RunFailureReport rendered (empty path): expected \"<unknown>\" placeholder; got:\n%s", rendered)
	}
}

func TestRunFailureReport_Rendered_StructuredFailure_NoSingleLineContainsAllFields(t *testing.T) {
	// AD-3: each field is placed in its own diagnostic, so no single rendered line
	// should contain all four field values simultaneously (which would indicate
	// collapsed interpolation rather than individual visibility).
	const reason = "deploy-failed"
	const detail = "path/to/agent.yaml"
	const toolMsg = "tool output text"
	const exitCode = 7

	err := &stubStructuredFailure{reason: reason, detail: detail, toolMessage: toolMsg, exitCode: exitCode}
	rep := authoring.RunFailureReport("suite/path", err)
	rendered := authoring.RenderReport(rep)

	// Guard: assert the implementation produced non-empty rendered output at all.
	// Without this guard the per-line loop iterates only the empty string "", which
	// contains none of the field values, so the assertion vacuously passes.
	if rendered == "" {
		t.Fatal("RunFailureReport(structured): RenderReport produced an empty string; expected non-empty rendered output")
	}
	for _, line := range strings.Split(rendered, "\n") {
		present := 0
		if strings.Contains(line, reason) {
			present++
		}
		if strings.Contains(line, detail) {
			present++
		}
		if strings.Contains(line, toolMsg) {
			present++
		}
		if strings.Contains(line, fmt.Sprintf("%d", exitCode)) {
			present++
		}
		if present > 1 {
			t.Errorf("RunFailureReport rendered: single line contains %d distinct field values (want at most 1); line: %q", present, line)
		}
	}
}

func TestReportWriteFailureReport_Rendered_PathAndHintVisible(t *testing.T) {
	// AC2.5: the rendered output must name the target path and the report-path overrides.
	const reportPath = "/output/test-report.json"
	err := errors.New("permission denied")
	rep := authoring.ReportWriteFailureReport(reportPath, err)
	rendered := authoring.RenderReport(rep)

	for _, want := range []string{reportPath, "--report-path", "--no-report"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("ReportWriteFailureReport rendered: expected %q to appear in output; got:\n%s", want, rendered)
		}
	}
}

func TestReportWriteFailureReport_Rendered_UsesEstablishedLineFormat(t *testing.T) {
	// No new rendering format is introduced: the existing RenderReport format applies.
	err := errors.New("disk error")
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", err)
	rendered := authoring.RenderReport(rep)

	for _, want := range []string{"error:", "report-unwritable:"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("ReportWriteFailureReport rendered: expected %q in output (established line format); got:\n%s", want, rendered)
		}
	}
}

func TestReportWriteFailureReport_Rendered_UnconfiguredCase_HasDistinctCode(t *testing.T) {
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", authoring.ErrReportWriterUnconfigured)
	rendered := authoring.RenderReport(rep)

	if !strings.Contains(rendered, "report-writer-unconfigured") {
		t.Errorf("ReportWriteFailureReport rendered (unconfigured): expected code %q in output; got:\n%s", "report-writer-unconfigured", rendered)
	}
}

func TestReportWriteFailureReport_Rendered_HintAppearsInEstablishedHintFormat(t *testing.T) {
	// The hint must use the established "(hint: ...)" wrapping from RenderDiagnostic.
	err := errors.New("permission denied")
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", err)
	rendered := authoring.RenderReport(rep)

	if !strings.Contains(rendered, "(hint:") {
		t.Errorf("ReportWriteFailureReport rendered: expected hint in \"(hint: ...)\" format; got:\n%s", rendered)
	}
}

func TestReportWriteFailureReport_Rendered_UnconfiguredCase_HintAppearsInEstablishedFormat(t *testing.T) {
	rep := authoring.ReportWriteFailureReport("/path/to/report.json", authoring.ErrReportWriterUnconfigured)
	rendered := authoring.RenderReport(rep)

	if !strings.Contains(rendered, "(hint:") {
		t.Errorf("ReportWriteFailureReport rendered (unconfigured): expected hint in \"(hint: ...)\" format; got:\n%s", rendered)
	}
}
