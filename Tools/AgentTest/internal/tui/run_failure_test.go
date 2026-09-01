package tui

// run_failure_test.go verifies the TUI's failure-detail surface for two
// post-run failure paths: a failed suite run and a failed report-file write.
//
// Both paths route through the shared detail pane. These tests verify that:
//
//   - A SuiteFinishedMsg carrying an error surfaces that error's full text
//     through FailureDetail() and in the rendered view, on ScreenResults,
//     untruncated, with a title identifying the operation as a suite run failure.
//   - A failing (or nil) WriteFile surfaces the write-failure detail through
//     the same pane while the run's own results remain listed (Result() returns
//     the completed result and resultErr stays nil).
//   - A successful run with a successful (or unconfigured) report write shows
//     no failure detail (FailureDetail() returns shown == false).
//
// The failure-detail content matrix from the design specifies:
//   - Suite run failed: title = "Suite run failed", body = msg.Err.Error() verbatim
//   - Report write failed: title = "Cannot write report to <reportPath>",
//     body = write error text verbatim
//
// SuiteFinishedMsg is delivered directly to Model.Update — no real runner is
// involved. A report-write failure is scripted by supplying a failing or nil
// Options.WriteFile alongside a non-empty Options.ReportPath.

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// runFailureResult builds a minimal report.Result carrying one passing test,
// so report-write-failure tests can verify the run result is retained.
func runFailureResult(suiteID, testID string) report.Result {
	run := report.RunReport{
		Key:      domain.RunKey{TestName: testID, RunNumber: 1},
		Verdict:  domain.VerdictPass,
		Duration: time.Second,
	}
	tr := report.TestReport{TestName: testID, Runs: []report.RunReport{run}}
	return report.Build(suiteID, time.Now(), time.Now(), []report.TestReport{tr}, "")
}

// tuiStubStructuredFailure is a test-local error type that satisfies the
// authoring.StructuredFailure interface. Using a local stub keeps this test
// file independent of the agentdeploy package.
//
// Compile-time assertion: *tuiStubStructuredFailure must satisfy authoring.StructuredFailure.
var _ authoring.StructuredFailure = (*tuiStubStructuredFailure)(nil)

type tuiStubStructuredFailure struct {
	reason      string
	detail      string
	toolMessage string
	exitCode    int
	errMsg      string
}

func (s *tuiStubStructuredFailure) FailureReason() string      { return s.reason }
func (s *tuiStubStructuredFailure) FailureDetail() string      { return s.detail }
func (s *tuiStubStructuredFailure) FailureToolMessage() string { return s.toolMessage }
func (s *tuiStubStructuredFailure) FailureExitCode() int       { return s.exitCode }
func (s *tuiStubStructuredFailure) Error() string              { return s.errMsg }

// optsWithFailingWrite builds Options whose WriteFile always returns err, so a
// SuiteFinishedMsg{Result: …} drives the report-write-failure path.
func optsWithFailingWrite(writeErr error) Options {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.ReportPath = "output/report.json"
	opts.WriteFile = func(path string, data []byte) error { return writeErr }
	return opts
}

// ---------------------------------------------------------------------------
// T3.1 — Suite run failed: full error text through the detail pane
// ---------------------------------------------------------------------------

// TestRunFailure_DetailPane_ShownAfterRunError verifies that delivering a
// SuiteFinishedMsg carrying a non-nil error causes FailureDetail() to return
// shown == true. The pane must be live on the screen the model lands on.
func TestRunFailure_DetailPane_ShownAfterRunError(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: errors.New("catastrophic run failure")})

	_, _, shown := m.FailureDetail()
	if !shown {
		t.Errorf("FailureDetail() shown = false after SuiteFinishedMsg{Err: non-nil}; the detail pane must be live when a suite run fails")
	}
}

// TestRunFailure_DetailPane_TitleIdentifiesRunFailure verifies the pane title
// names the failing operation as a suite run failure, not a generic error or
// a write failure.
func TestRunFailure_DetailPane_TitleIdentifiesRunFailure(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: errors.New("run error")})

	title, _, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a run failure; precondition not met")
	}
	// The design specifies the exact title "Suite run failed". Assert all three
	// words are present so a title containing only one of them (e.g. "Suite
	// error" or "Operation failed") is correctly rejected.
	lc := strings.ToLower(title)
	if !strings.Contains(lc, "suite") {
		t.Errorf("FailureDetail() title %q does not contain \"suite\"; the design-specified title is \"Suite run failed\"", title)
	}
	if !strings.Contains(lc, "run") {
		t.Errorf("FailureDetail() title %q does not contain \"run\"; the design-specified title is \"Suite run failed\"", title)
	}
	if !strings.Contains(lc, "failed") {
		t.Errorf("FailureDetail() title %q does not contain \"failed\"; the design-specified title is \"Suite run failed\"", title)
	}
}

// TestRunFailure_DetailPane_BodyIsStructuredDiagnostic verifies that the pane
// body is the structured diagnostic rendering of the run failure — not a bare
// error.Error() string. The body must carry the machine-readable diagnostic
// code ("suite-run-failed"), the severity prefix ("error:"), and the
// underlying error message so the cause is still visible.
//
// This replaces the former verbatim-text expectation, which contradicts the
// design: the body is now rendered through authoring.RenderReport so every
// failure surface presents diagnostics of equivalent richness.
func TestRunFailure_DetailPane_BodyIsStructuredDiagnostic(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	runErr := errors.New("the suite runner returned this failure message")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: runErr})

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a run failure; precondition not met")
	}
	// The body must contain the machine-readable diagnostic code.
	// With the old bare-error body this assertion fails (RED phase).
	if !strings.Contains(body, "suite-run-failed") {
		t.Errorf("FailureDetail() body %q does not contain the diagnostic code %q; the body must be a structured RenderReport rendering, not a bare error string", body, "suite-run-failed")
	}
	// The severity prefix must also be present.
	if !strings.Contains(body, "error:") {
		t.Errorf("FailureDetail() body %q does not contain the severity prefix \"error:\"; the structured rendering must include severity", body)
	}
	// The underlying error message must still be present in the body so the
	// cause is visible without further navigation.
	if !strings.Contains(body, runErr.Error()) {
		t.Errorf("FailureDetail() body %q does not contain the underlying error text %q; the error cause must be visible in the structured body", body, runErr.Error())
	}
}

// TestRunFailure_LandsOnResultsScreen verifies that handleSuiteFinished always
// transitions to ScreenResults, even when the run failed.
func TestRunFailure_LandsOnResultsScreen(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: errors.New("run failed")})

	if m.Screen() != ScreenResults {
		t.Errorf("Screen() after SuiteFinishedMsg{Err: non-nil} = %q, want %q; handleSuiteFinished must always land on the results screen", m.Screen(), ScreenResults)
	}
}

// TestRunFailure_StatusBarKeepsSummary verifies that the status bar retains a
// short summary when a run fails. The design keeps the existing status-bar
// wording (msg.Err.Error()) in addition to routing the same text through the
// pane, so both surfaces carry the error.
func TestRunFailure_StatusBarKeepsSummary(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	runErr := errors.New("run error summary text")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: runErr})
	view := safeView(t, m)

	if !strings.Contains(view, runErr.Error()) {
		t.Errorf("rendered view does not contain the run error text %q in the status bar; the bar must keep a short summary while the pane holds the full detail", runErr.Error())
	}
}

// TestRunFailure_ViewContainsErrorTextOnResultsScreen verifies that the
// rendered view on the results screen contains the full error text, which
// must be present through the embedded detail pane rather than only the
// one-line status bar.
func TestRunFailure_ViewContainsErrorTextOnResultsScreen(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	runErr := errors.New("visible run error text on results screen")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: runErr})
	view := safeView(t, m)

	if !strings.Contains(view, "visible run error text on results screen") {
		t.Errorf("rendered results-screen view does not contain the run error text; the full error must be visible through the detail pane embedded in the results screen")
	}
}

// TestRunFailure_LargeError_NothingTruncated verifies that an error message
// too long for the status bar still appears in full in FailureDetail(). The
// status bar clips; the pane must not.
func TestRunFailure_LargeError_NothingTruncated(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	// Build an error longer than any reasonable status bar, ending in a
	// unique marker that must survive in the body.
	large := "prefix " + strings.Repeat("x", 300) + " TAIL-MARKER"
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: errors.New(large)})

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a run failure with a large error message")
	}
	if !strings.Contains(body, "TAIL-MARKER") {
		t.Errorf("FailureDetail() body does not contain the tail of the large error message (TAIL-MARKER); the body must be untruncated regardless of the one-line status-bar width")
	}
}

// TestRunFailure_StructuredFailure_ReasonInBody verifies that when the error
// satisfies authoring.StructuredFailure and FailureReason() is non-empty, the
// reason value appears in the detail body under the "failure.reason" pointer.
// With the old bare-error body this assertion fails (RED phase).
func TestRunFailure_StructuredFailure_ReasonInBody(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	runErr := &tuiStubStructuredFailure{
		reason: "config-invalid",
		errMsg: "stub: config-invalid",
	}
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: runErr})

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a structured run failure; precondition not met")
	}
	if !strings.Contains(body, "failure.reason") {
		t.Errorf("FailureDetail() body %q does not contain \"failure.reason\"; a structured failure with a non-empty Reason must produce a failure.reason diagnostic", body)
	}
	if !strings.Contains(body, "config-invalid") {
		t.Errorf("FailureDetail() body %q does not contain the reason value %q; the reason must appear in the body verbatim", body, "config-invalid")
	}
}

// TestRunFailure_StructuredFailure_DetailInBody verifies that when the error
// satisfies authoring.StructuredFailure and FailureDetail() is non-empty, the
// detail value appears in the body under the "failure.detail" pointer.
// With the old bare-error body this assertion fails (RED phase).
func TestRunFailure_StructuredFailure_DetailInBody(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	runErr := &tuiStubStructuredFailure{
		reason: "validation-failed",
		detail: "tests/my-suite.yaml",
		errMsg: "stub: validation-failed on tests/my-suite.yaml",
	}
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: runErr})

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a structured run failure; precondition not met")
	}
	if !strings.Contains(body, "failure.detail") {
		t.Errorf("FailureDetail() body %q does not contain \"failure.detail\"; a structured failure with a non-empty Detail must produce a failure.detail diagnostic", body)
	}
	if !strings.Contains(body, "tests/my-suite.yaml") {
		t.Errorf("FailureDetail() body %q does not contain the detail value %q; the offending input must appear in the body", body, "tests/my-suite.yaml")
	}
}

// TestRunFailure_StructuredFailure_ToolOutputInBody verifies that when the
// error satisfies authoring.StructuredFailure and FailureToolMessage() is
// non-empty, the delegate's own output appears in the body under the
// "failure.tool_output" pointer, verbatim and untruncated.
// With the old bare-error body this assertion fails (RED phase).
func TestRunFailure_StructuredFailure_ToolOutputInBody(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	toolMsg := "line one from the delegate tool\nline two from the delegate tool\nline three"
	runErr := &tuiStubStructuredFailure{
		reason:      "tool-error",
		toolMessage: toolMsg,
		errMsg:      "stub: tool-error",
	}
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: runErr})

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a structured run failure; precondition not met")
	}
	if !strings.Contains(body, "failure.tool_output") {
		t.Errorf("FailureDetail() body %q does not contain \"failure.tool_output\"; a structured failure with a non-empty ToolMessage must produce a failure.tool_output diagnostic", body)
	}
	if !strings.Contains(body, "line three") {
		t.Errorf("FailureDetail() body does not contain the last line of the tool output; the verbatim multi-line message must be present in full in the body (nothing truncated)")
	}
}

// TestRunFailure_StructuredFailure_ExitCodeInBody verifies that when the error
// satisfies authoring.StructuredFailure and FailureExitCode() is non-zero, the
// exit code appears in the body under the "failure.exit_code" pointer.
// With the old bare-error body this assertion fails (RED phase).
func TestRunFailure_StructuredFailure_ExitCodeInBody(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	runErr := &tuiStubStructuredFailure{
		reason:   "non-zero-exit",
		exitCode: 2,
		errMsg:   "stub: exited with code 2",
	}
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: runErr})

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a structured run failure; precondition not met")
	}
	if !strings.Contains(body, "failure.exit_code") {
		t.Errorf("FailureDetail() body %q does not contain \"failure.exit_code\"; a structured failure with a non-zero exit code must produce a failure.exit_code diagnostic", body)
	}
	if !strings.Contains(body, "2") {
		t.Errorf("FailureDetail() body %q does not contain the exit code value \"2\"; the exit code must be present in the body", body)
	}
}

// TestRunFailure_StructuredFailure_EmptyFieldsOmittedFromBody verifies that
// when an error satisfies authoring.StructuredFailure but all optional fields
// are empty (reason="", detail="", toolMessage="", exitCode=0), only the lead
// diagnostic appears — no empty-value diagnostics are rendered.
// The "no structured reason" hint must be present in the lead diagnostic.
// With the old bare-error body the structured-code assertion fails (RED phase).
func TestRunFailure_StructuredFailure_EmptyFieldsOmittedFromBody(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	// All optional structured fields are zero/empty.
	runErr := &tuiStubStructuredFailure{
		errMsg: "undifferentiated run failure",
	}
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: runErr})

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a structured run failure with empty fields; precondition not met")
	}
	// Must not produce diagnostics for empty fields.
	if strings.Contains(body, "failure.reason") {
		t.Errorf("FailureDetail() body contains \"failure.reason\" when FailureReason() is empty; empty fields must be omitted")
	}
	if strings.Contains(body, "failure.detail") {
		t.Errorf("FailureDetail() body contains \"failure.detail\" when FailureDetail() is empty; empty fields must be omitted")
	}
	if strings.Contains(body, "failure.tool_output") {
		t.Errorf("FailureDetail() body contains \"failure.tool_output\" when FailureToolMessage() is empty; empty fields must be omitted")
	}
	if strings.Contains(body, "failure.exit_code") {
		t.Errorf("FailureDetail() body contains \"failure.exit_code\" when FailureExitCode() is 0; zero exit code must be omitted")
	}
	// The lead diagnostic must still be present with the diagnostic code.
	if !strings.Contains(body, "suite-run-failed") {
		t.Errorf("FailureDetail() body %q does not contain \"suite-run-failed\"; the lead diagnostic must always be present", body)
	}
}

// TestRunFailure_StructuredFailure_HintWhenNoReason verifies that when the
// error satisfies authoring.StructuredFailure but FailureReason() returns "",
// the body carries a hint on the lead diagnostic explaining that no structured
// reason was reported and the tool's own output is the only detail available.
// With the old bare-error body this assertion fails (RED phase).
func TestRunFailure_StructuredFailure_HintWhenNoReason(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	runErr := &tuiStubStructuredFailure{
		reason: "", // empty: undifferentiated failure
		errMsg: "undifferentiated run failure",
	}
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: runErr})

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a structured run failure with no reason; precondition not met")
	}
	// The design specifies a hint explaining that no structured reason is available.
	if !strings.Contains(body, "hint:") {
		t.Errorf("FailureDetail() body %q does not contain a \"hint:\"; the lead diagnostic must carry a hint when FailureReason() is empty", body)
	}
	// The hint must mention "no structured reason" or similar wording.
	if !strings.Contains(body, "no structured reason") {
		t.Errorf("FailureDetail() body %q does not mention \"no structured reason\" in the hint; the hint must explain the absence of a structured reason code", body)
	}
}

// ---------------------------------------------------------------------------
// T3.2 — Report write failed: detail pane shown, run results still listed
// ---------------------------------------------------------------------------

// TestReportWriteFailure_FailingWriteFile_DetailPaneShown verifies that a
// WriteFile function that returns an error causes FailureDetail() to show the
// write-failure detail.
func TestReportWriteFailure_FailingWriteFile_DetailPaneShown(t *testing.T) {
	opts := optsWithFailingWrite(errors.New("disk full: no space left on device"))
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	_, _, shown := m.FailureDetail()
	if !shown {
		t.Errorf("FailureDetail() shown = false after WriteFile returned an error; the detail pane must be live when the report write fails")
	}
}

// TestReportWriteFailure_NilWriteFile_DetailPaneShown verifies that a nil
// WriteFile (a wiring omission) is reported as a write failure, not silently
// skipped, when ReportPath is non-empty.
func TestReportWriteFailure_NilWriteFile_DetailPaneShown(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.ReportPath = "output/report.json"
	opts.WriteFile = nil // nil is a wiring error, not a skip
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	_, _, shown := m.FailureDetail()
	if !shown {
		t.Errorf("FailureDetail() shown = false when WriteFile is nil and ReportPath is set; a nil WriteFile must be surfaced as a write failure so the omission is not invisible")
	}
}

// TestReportWriteFailure_TitleContainsReportPath verifies that the pane title
// names the file that could not be written, so the user knows which output
// path failed without reading the body.
func TestReportWriteFailure_TitleContainsReportPath(t *testing.T) {
	opts := optsWithFailingWrite(errors.New("permission denied"))
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	title, _, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a write failure; precondition not met")
	}
	if !strings.Contains(title, "output/report.json") {
		t.Errorf("FailureDetail() title %q does not contain the report path %q; the title must name the file that could not be written", title, "output/report.json")
	}
}

// TestReportWriteFailure_TitleDistinctFromRunFailureTitle verifies that the
// write-failure pane title is distinct from the run-failure pane title, so the
// user can tell the two failure kinds apart without reading the body.
func TestReportWriteFailure_TitleDistinctFromRunFailureTitle(t *testing.T) {
	// Run failure.
	runnerRun := newFakeSuiteRunner()
	mRun := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runnerRun))
	mRun, _ = safeUpdate(t, mRun, SuiteFinishedMsg{Err: errors.New("run failed")})
	titleRun, _, shownRun := mRun.FailureDetail()
	if !shownRun {
		t.Fatalf("FailureDetail() not shown after run failure; precondition not met")
	}

	// Report write failure.
	mWrite := NewModel(optsWithFailingWrite(errors.New("disk full")))
	result := runFailureResult("suite-under-test", "test-a")
	mWrite, _ = safeUpdate(t, mWrite, SuiteFinishedMsg{Result: result})
	titleWrite, _, shownWrite := mWrite.FailureDetail()
	if !shownWrite {
		t.Fatalf("FailureDetail() not shown after write failure; precondition not met")
	}

	if titleRun == titleWrite {
		t.Errorf("run failure and report-write failure use the same pane title %q; distinct titles are required so the user can distinguish the two failure kinds without reading the body", titleRun)
	}
}

// TestReportWriteFailure_BodyContainsWriteErrorText verifies that the pane
// body contains the write error's text and the machine-readable diagnostic
// code "report-unwritable". The error message must be present in the
// structured rendering so the cause is still visible.
// The diagnostic-code assertion fails with the old bare-error body (RED phase).
func TestReportWriteFailure_BodyContainsWriteErrorText(t *testing.T) {
	writeErr := errors.New("disk full: no space left on device — the report cannot be written")
	opts := optsWithFailingWrite(writeErr)
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a write failure; precondition not met")
	}
	if !strings.Contains(body, writeErr.Error()) {
		t.Errorf("FailureDetail() body %q does not contain the write error text %q; the error message must be visible in the structured body", body, writeErr.Error())
	}
	// The body must carry the machine-readable diagnostic code; with the old
	// bare-error body this assertion fails (TDD RED phase).
	if !strings.Contains(body, "report-unwritable") {
		t.Errorf("FailureDetail() body %q does not contain the diagnostic code \"report-unwritable\"; the body must be a structured RenderReport rendering, not a bare error string", body)
	}
}

// TestReportWriteFailure_RunResultsStillListed verifies that a write failure
// does not mask the run's own result: Result() must return the completed result
// with its test list intact.
func TestReportWriteFailure_RunResultsStillListed(t *testing.T) {
	opts := optsWithFailingWrite(errors.New("disk full"))
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	got, ok := m.Result()
	if !ok {
		t.Fatalf("Result() ok = false after a report-write failure; the run's completed result must be retained when only the write fails")
	}
	if len(got.Tests) == 0 {
		t.Errorf("Result().Tests is empty after a report-write failure; the completed test list must not be lost when writing the report file fails")
	}
}

// TestReportWriteFailure_ResultErrNotSet verifies that a write failure does not
// set resultErr. resultErr signals a run failure; a write failure is a separate
// operation and must not be conflated with one.
func TestReportWriteFailure_ResultErrNotSet(t *testing.T) {
	opts := optsWithFailingWrite(errors.New("disk full"))
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	if m.resultErr != nil {
		t.Errorf("m.resultErr = %v after a report-write failure; write failures must not set resultErr (that field signals a run failure, not a write failure)", m.resultErr)
	}
}

// TestReportWriteFailure_LandsOnResultsScreen verifies that a write failure
// still transitions to the results screen, so the user sees their test results
// even though the file could not be saved.
func TestReportWriteFailure_LandsOnResultsScreen(t *testing.T) {
	opts := optsWithFailingWrite(errors.New("disk full"))
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	if m.Screen() != ScreenResults {
		t.Errorf("Screen() after a report-write failure = %q, want %q; a write failure must still land on the results screen", m.Screen(), ScreenResults)
	}
}

// TestReportWriteFailure_StatusBarKeepsSummary verifies that the status bar
// carries a short summary naming the failed write even though the detail pane
// holds the full information.
func TestReportWriteFailure_StatusBarKeepsSummary(t *testing.T) {
	opts := optsWithFailingWrite(errors.New("disk full"))
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})
	view := safeView(t, m)

	// The status bar must name the path that failed so the one-line summary
	// is still actionable. The exact format is an implementation choice;
	// what is tested is that the report path appears in the rendered view.
	if !strings.Contains(view, "output/report.json") {
		t.Errorf("rendered view after a write failure does not contain the report path %q in the status-bar summary; the bar must name the failed file", "output/report.json")
	}
}

// TestReportWriteFailure_ViewContainsWriteDetailOnResultsScreen verifies that
// the write-failure diagnostic is present in the rendered results-screen view,
// through the embedded detail pane.
//
// The assertion targets the machine-readable diagnostic code "report-unwritable"
// (17 chars) rather than a substring of the raw error message. The structured
// rendering places this code at the start of the lead diagnostic line, so it is
// always short enough to remain on a single word-wrapped line at the default
// 80-char pane width. Checking the full raw error string would span the
// word-wrap boundary and produce a false negative.
func TestReportWriteFailure_ViewContainsWriteDetailOnResultsScreen(t *testing.T) {
	writeErr := errors.New("disk full: write failed")
	opts := optsWithFailingWrite(writeErr)
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})
	view := safeView(t, m)

	if !strings.Contains(view, "report-unwritable") {
		t.Errorf("rendered results-screen view does not contain the diagnostic code \"report-unwritable\"; the structured write-failure diagnostic must be visible through the detail pane embedded in the results screen")
	}
}

// TestReportWriteFailure_BodyContainsOverrideHint verifies that the pane body
// carries a hint naming the report-path override mechanisms. The design
// requires --report-path, --no-report, and the inline TUI editor to be
// mentioned so the user can act immediately.
// With the old bare-error body this assertion fails (RED phase).
func TestReportWriteFailure_BodyContainsOverrideHint(t *testing.T) {
	writeErr := errors.New("permission denied")
	opts := optsWithFailingWrite(writeErr)
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a write failure; precondition not met")
	}
	// The override hint must name --report-path as the primary mechanism.
	if !strings.Contains(body, "--report-path") {
		t.Errorf("FailureDetail() body %q does not contain \"--report-path\"; the write-failure diagnostic must carry a hint naming the report-path override", body)
	}
}

// TestReportWriteFailure_UnconfiguredWriter_BodyShowsDistinctCode verifies
// that when WriteFile is nil (wiring omission), the body carries the distinct
// "report-writer-unconfigured" code rather than the generic "report-unwritable"
// code. These are two separate failure conditions with different remediation.
// With the old bare-error body this assertion fails (RED phase).
func TestReportWriteFailure_UnconfiguredWriter_BodyShowsDistinctCode(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.ReportPath = "output/report.json"
	opts.WriteFile = nil // deliberate wiring omission
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false with nil WriteFile; precondition not met")
	}
	if !strings.Contains(body, "report-writer-unconfigured") {
		t.Errorf("FailureDetail() body %q does not contain \"report-writer-unconfigured\"; a nil WriteFile must produce the distinct unconfigured-writer diagnostic, not the generic write-failure diagnostic", body)
	}
	// Must not use the write-failure code for a configuration omission.
	if strings.Contains(body, "report-unwritable") {
		t.Errorf("FailureDetail() body %q contains \"report-unwritable\" for a nil WriteFile; the unconfigured-writer code must be distinct from the write-failure code", body)
	}
}

// TestReportWriteFailure_UnconfiguredWriter_OverrideHintInBody verifies that
// the hint is also present when the writer is nil, since the remediation
// (configure --report-path or use --no-report) is the same.
// With the old bare-error body this assertion fails (RED phase).
func TestReportWriteFailure_UnconfiguredWriter_OverrideHintInBody(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.ReportPath = "output/report.json"
	opts.WriteFile = nil
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	_, body, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false with nil WriteFile; precondition not met")
	}
	if !strings.Contains(body, "--report-path") {
		t.Errorf("FailureDetail() body %q does not contain \"--report-path\" hint when writer is unconfigured; the hint must be present regardless of whether a write was attempted", body)
	}
}

// ---------------------------------------------------------------------------
// T3.3 — Successful run: no failure detail shown
// ---------------------------------------------------------------------------

// TestSuccess_NoFailureDetail_NoReportPath verifies that a successful run with
// no report path configured produces no failure detail.
func TestSuccess_NoFailureDetail_NoReportPath(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	// opts.ReportPath is "" — write is skipped, not an error.
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	title, body, shown := m.FailureDetail()
	if shown {
		t.Errorf("FailureDetail() shown = true after a successful run with no report path; a successful run must show no failure detail")
	}
	if title != "" {
		t.Errorf("FailureDetail() title = %q after a successful run, want \"\"; when shown is false the title must be empty", title)
	}
	if body != "" {
		t.Errorf("FailureDetail() body is not empty after a successful run; when shown is false the body must be empty")
	}
}

// TestSuccess_NoFailureDetail_SuccessfulWrite verifies that a successful run
// with a successful report write produces no failure detail. Both operations
// succeeding is the expected happy path.
func TestSuccess_NoFailureDetail_SuccessfulWrite(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.ReportPath = "output/report.json"
	opts.WriteFile = func(path string, data []byte) error { return nil }
	m := NewModel(opts)

	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	title, body, shown := m.FailureDetail()
	if shown {
		t.Errorf("FailureDetail() shown = true after a successful run with a successful report write; no failure detail must be shown when both the run and the write succeed")
	}
	if title != "" {
		t.Errorf("FailureDetail() title = %q after a successful run and write, want \"\"; when shown is false the title must be empty", title)
	}
	if body != "" {
		t.Errorf("FailureDetail() body is not empty after a successful run and write; when shown is false the body must be empty")
	}
}

// ---------------------------------------------------------------------------
// Scroll key help entry on ScreenResults
// ---------------------------------------------------------------------------

// TestResultsScreen_ScrollHelpEntryVisible verifies that the rendered
// results screen includes the "pgup/pgdn" scroll affordance in its help bar
// when a run-failure pane is live. The design scroll key contract requires
// both keys to appear in the affected screens' help entries so the affordance
// is discoverable.
func TestResultsScreen_ScrollHelpEntryVisible(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: errors.New("run failure for help test")})

	_, _, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a run failure; precondition not met")
	}
	if m.Screen() != ScreenResults {
		t.Fatalf("Screen() = %q after SuiteFinishedMsg{Err: non-nil}, want %q; precondition not met", m.Screen(), ScreenResults)
	}

	view := safeView(t, m)
	if !strings.Contains(view, "pgup/pgdn") {
		t.Errorf("rendered results screen does not contain \"pgup/pgdn\" in the help bar when a failure pane is live; the scroll key affordance must be discoverable on ScreenResults")
	}
	if !strings.Contains(view, "scroll detail") {
		t.Errorf("rendered results screen does not contain \"scroll detail\" in the help bar when a failure pane is live; the scroll key affordance must be discoverable on ScreenResults")
	}
}

// ---------------------------------------------------------------------------
// Scroll behavior on ScreenResults
// ---------------------------------------------------------------------------

// TestResultsScreen_PgDownDoesNotAdvanceResultsCursor verifies that pressing
// PgDown on ScreenResults while a run-failure pane is live scrolls the pane
// and does not advance m.resultsCursor. On the results screen, Up/Down drive
// the results cursor while PgUp/PgDown are reserved for pane scrolling.
func TestResultsScreen_PgDownDoesNotAdvanceResultsCursor(t *testing.T) {
	// Provide multiple results so cursor movement would be observable.
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: errors.New("run failure for scroll cursor test")})

	_, _, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a run failure; precondition not met")
	}

	cursorBefore := m.resultsCursor

	// PgDown must scroll the pane, not advance the cursor.
	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.resultsCursor != cursorBefore {
		t.Errorf("PgDown moved the results cursor from %d to %d on ScreenResults; scroll keys must not affect the results cursor", cursorBefore, m.resultsCursor)
	}

	// PgUp must also not affect the cursor.
	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.resultsCursor != cursorBefore {
		t.Errorf("PgUp moved the results cursor from %d to %d on ScreenResults; scroll keys must not affect the results cursor", cursorBefore, m.resultsCursor)
	}
}

// TestResultsScreen_PgDownProducesVisiblyDifferentView verifies that
// scrolling the pane on ScreenResults actually changes the rendered view when
// a run-failure body is large enough to overflow the pane's visible height.
// A bug in the ScreenResults key handler that routed PgDown to the wrong
// handler or the wrong pane would leave the view unchanged.
func TestResultsScreen_PgDownProducesVisiblyDifferentView(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	// Build an error body large enough to overflow any reasonable pane height.
	largeBody := "start\n" + strings.Repeat("line of run error output\n", 50) + "end"
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 10})
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Err: errors.New(largeBody)})

	_, _, shown := m.FailureDetail()
	if !shown {
		t.Fatalf("FailureDetail() shown = false after a run failure with large body; precondition not met")
	}

	viewBefore := safeView(t, m)

	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	viewAfter := safeView(t, m)

	if viewBefore == viewAfter {
		t.Errorf("PgDown on ScreenResults produced no change in the rendered view; either pane scroll is not wired for this screen or all content fits the pane")
	}
}

// TestSuccess_StaleDetailClearedBySuccessfulRun verifies that failure detail
// set by a previous failure site is explicitly cleared when a successful
// SuiteFinishedMsg arrives. This is the "no leakage" invariant: a pre-flight
// failure's content must not survive onto the results screen after a
// subsequently successful run.
func TestSuccess_StaleDetailClearedBySuccessfulRun(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	m := NewModel(opts)

	// Simulate stale detail left by a previous failure (e.g. a pre-flight error).
	m = m.withFailureDetail("stale previous failure title", "stale previous failure body")
	if _, _, shown := m.FailureDetail(); !shown {
		t.Fatalf("withFailureDetail did not set shown=true; test setup is broken")
	}

	// A successful run must clear the stale detail.
	result := runFailureResult("suite-under-test", "test-a")
	m, _ = safeUpdate(t, m, SuiteFinishedMsg{Result: result})

	title, body, shown := m.FailureDetail()
	if shown {
		t.Errorf("FailureDetail() still shown after a successful SuiteFinishedMsg; stale detail from a previous failure must be cleared by a successful run")
	}
	if title != "" {
		t.Errorf("FailureDetail() title = %q after successful run (was stale), want \"\"; success must clear the title", title)
	}
	if body != "" {
		t.Errorf("FailureDetail() body not empty after successful run (was stale); success must clear the body")
	}
}
