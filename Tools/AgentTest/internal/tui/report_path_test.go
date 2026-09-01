package tui

// report_path_test.go covers the persistent JSON report file feature for the
// interactive frontend:
//
//   T7.3 - The default report path is displayed before a run starts and can
//           be overridden, and the chosen path reaches the started run.
//
// These tests drive Model through Options.ReportPath / Options.WriteFile and
// the ScreenReportPath self-contained settings screen. No real filesystem is
// touched: WriteFile is an in-memory capture function supplied on Options.
//
// Updated for Stage 3: the inline-edit affordance moved from the old monolithic
// ScreenSettings to the dedicated ScreenReportPath in the sequential settings
// flow. Navigation: Enter(×3) from ScreenSuiteSelect → ScreenReportPath.

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// In-memory WriteFile capture (TUI-side)
// ---------------------------------------------------------------------------

// tuiCaptureWriteFile records every WriteFile call so a test can assert on
// the path and bytes without touching any real filesystem.
type tuiCaptureWriteFile struct {
	mu    sync.Mutex
	calls []tuiWriteCall
	err   error
}

type tuiWriteCall struct {
	path string
	data []byte
}

func (c *tuiCaptureWriteFile) write(path string, data []byte) error {
	c.mu.Lock()
	c.calls = append(c.calls, tuiWriteCall{path: path, data: data})
	c.mu.Unlock()
	return c.err
}

func (c *tuiCaptureWriteFile) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *tuiCaptureWriteFile) lastCall() (tuiWriteCall, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.calls) == 0 {
		return tuiWriteCall{}, false
	}
	return c.calls[len(c.calls)-1], true
}

// Compile-time check that tuiCaptureWriteFile.write satisfies WriteFileFunc.
var _ WriteFileFunc = (*tuiCaptureWriteFile)(nil).write

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newReportPathOptions extends newFixtureOptions with a report path and a
// WriteFile capture.
func newReportPathOptions(suites []string, runner *fakeSuiteRunner, reportPath string, capture *tuiCaptureWriteFile) Options {
	o := newFixtureOptions(suites, runner)
	o.ReportPath = reportPath
	if capture != nil {
		o.WriteFile = capture.write
	}
	return o
}

// navigateToScreenReportPath advances from ScreenSuiteSelect to ScreenReportPath
// by pressing Enter three times (SuiteSelect→Retention→Repetitions→ReportPath).
// It fails the test immediately if any intermediate screen is wrong.
func navigateToScreenReportPath(t *testing.T, m Model) Model {
	t.Helper()
	m = advanceToSettingsFlow(t, m) // Enter → ScreenRetention
	var cmd tea.Cmd
	m, cmd = safeUpdate(t, m, keyMsg("\r")) // ScreenRetention → ScreenRepetitions
	_ = cmd
	if m.Screen() != ScreenRepetitions {
		t.Fatalf("navigateToScreenReportPath: Screen() after Retention Enter = %q, want %q", m.Screen(), ScreenRepetitions)
	}
	m, cmd = safeUpdate(t, m, keyMsg("\r")) // ScreenRepetitions → ScreenReportPath
	_ = cmd
	if m.Screen() != ScreenReportPath {
		t.Fatalf("navigateToScreenReportPath: Screen() after Repetitions Enter = %q, want %q", m.Screen(), ScreenReportPath)
	}
	return m
}

// enterKey returns the key that commits an in-progress edit or advances a screen.
func enterKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEnter}
}

// escKey returns the key that cancels an in-progress edit or navigates backward.
func escKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEsc}
}

// ---------------------------------------------------------------------------
// T7.3: initial propagation
// ---------------------------------------------------------------------------

// TestNewModel_ReportPath_StartsAtOptionsReportPath asserts that a Model
// starts with the report path supplied by Options, before any edit —
// analogous to SelectedHarness() starting at Options.Harness.
func TestNewModel_ReportPath_StartsAtOptionsReportPath(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "run-reports/report.json", nil)

	m := NewModel(o)

	if got := m.ReportPath(); got != "run-reports/report.json" {
		t.Errorf("ReportPath() = %q, want %q (Options.ReportPath, before any edit)", got, "run-reports/report.json")
	}
}

// ---------------------------------------------------------------------------
// T7.3: display on ScreenReportPath
// ---------------------------------------------------------------------------

// TestSettings_ReportPath_IsDisplayedBeforeRunStarts asserts that ScreenReportPath
// renders the current report path so the user can see where the report will be
// written before confirming.
func TestSettings_ReportPath_IsDisplayedBeforeRunStarts(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "report.json", nil)
	m := NewModel(o)

	m = navigateToScreenReportPath(t, m)

	view := safeView(t, m)
	if !strings.Contains(view, "report.json") {
		t.Errorf("ScreenReportPath View() does not contain the report path %q:\n%s", "report.json", view)
	}
}

// ---------------------------------------------------------------------------
// T7.3: path editing on ScreenReportPath
// ---------------------------------------------------------------------------

// TestSettings_ReportPath_ActionKey_EntersEditMode asserts that ScreenReportPath
// shows the typed draft immediately, confirming that path input is live.
// In the new sequential flow, navigating to ScreenReportPath itself constitutes
// entering path-edit mode; there is no separate activation step.
func TestSettings_ReportPath_ActionKey_EntersEditMode(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "report.json", nil)
	m := NewModel(o)

	m = navigateToScreenReportPath(t, m)
	if m.Screen() != ScreenReportPath {
		t.Fatalf("Screen() = %q, want %q (navigateToScreenReportPath failed)", m.Screen(), ScreenReportPath)
	}

	// Type a single character; it must appear in the view immediately.
	m, _ = safeUpdate(t, m, keyMsg("x"))
	view := safeView(t, m)
	if !strings.Contains(view, "x") {
		t.Errorf("ScreenReportPath View() after typing 'x' does not contain 'x'; draft input must be reflected immediately:\n%s", view)
	}
}

// TestSettings_ReportPath_InlineEdit_Commit_UpdatesPath asserts that the user
// can type a new path on ScreenReportPath and commit it with Enter, and the
// new value is reflected in ReportPath() once the screen advances.
func TestSettings_ReportPath_InlineEdit_Commit_UpdatesPath(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "old-report.json", nil)
	m := NewModel(o)

	m = navigateToScreenReportPath(t, m)

	for _, ch := range "new-report.json" {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	// Enter commits the typed path and advances to ScreenCatalogFolder,
	// capturing the confirmed value into m.reportPath.
	m, _ = safeUpdate(t, m, enterKey())

	if m.Screen() != ScreenCatalogFolder {
		t.Errorf("Screen() after Enter on ScreenReportPath = %q, want %q", m.Screen(), ScreenCatalogFolder)
	}
	if got := m.ReportPath(); got != "new-report.json" {
		t.Errorf("ReportPath() = %q after commit, want %q", got, "new-report.json")
	}
}

// TestSettings_ReportPath_InlineEdit_Commit_ShownInView asserts that the typed
// draft appears in ScreenReportPath's rendered output before committing.
func TestSettings_ReportPath_InlineEdit_Commit_ShownInView(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "old.json", nil)
	m := NewModel(o)

	m = navigateToScreenReportPath(t, m)
	for _, ch := range "custom.json" {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}

	view := safeView(t, m)
	if !strings.Contains(view, "custom.json") {
		t.Errorf("ScreenReportPath View() while typing does not contain %q:\n%s", "custom.json", view)
	}
}

// ---------------------------------------------------------------------------
// T7.3: back navigation leaves path unchanged
// ---------------------------------------------------------------------------

// TestSettings_ReportPath_InlineEdit_Cancel_RestoresPrevious asserts that
// pressing Escape on ScreenReportPath while a draft is in progress navigates
// back to ScreenRepetitions without changing the confirmed path.
func TestSettings_ReportPath_InlineEdit_Cancel_RestoresPrevious(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "original.json", nil)
	m := NewModel(o)

	m = navigateToScreenReportPath(t, m)
	for _, ch := range "half-typed" {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, escKey())

	if m.Screen() != ScreenRepetitions {
		t.Errorf("Screen() after Esc on ScreenReportPath = %q, want %q (must go back to ScreenRepetitions)", m.Screen(), ScreenRepetitions)
	}
	// The report path must not have been changed by the abandoned draft.
	if got := m.ReportPath(); got != "original.json" {
		t.Errorf("ReportPath() = %q after Esc; want %q (original path must not be overwritten by abandoned draft)", got, "original.json")
	}
}

// ---------------------------------------------------------------------------
// T7.3: non-editing keys do not advance the screen
// ---------------------------------------------------------------------------

// TestSettings_ReportPath_EditMode_NavigationKeysAreConsumed asserts that while
// ScreenReportPath is active, a navigation key such as Down does not advance
// the flow to the next screen — only Enter and Esc drive navigation.
func TestSettings_ReportPath_EditMode_NavigationKeysAreConsumed(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "report.json", nil)
	m := NewModel(o)

	m = navigateToScreenReportPath(t, m)

	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})

	// Down must not advance to the next screen.
	if m.Screen() != ScreenReportPath {
		t.Errorf("Screen() after Down on ScreenReportPath = %q, want %q (navigation keys must not drive the sequential flow)", m.Screen(), ScreenReportPath)
	}
}

// ---------------------------------------------------------------------------
// T7.3: chosen path reaches the started run
// ---------------------------------------------------------------------------

// TestSuiteSelect_InitialReportPath_ReachesRun asserts that the initial
// Options.ReportPath is the path WriteFile is called with when a run
// completes, if the user never edits it.
func TestSuiteSelect_InitialReportPath_ReachesRun(t *testing.T) {
	runner := newFakeSuiteRunner()
	capture := &tuiCaptureWriteFile{}
	o := newReportPathOptions([]string{"suite-a.yaml"}, runner, "my-report.json", capture)
	m := NewModel(o)

	m, cmd := startSuiteFromSuiteSelect(t, m)
	msg := runCmd(t, cmd)
	if msg == nil {
		t.Fatalf("no tea.Cmd returned when starting the suite")
	}
	safeUpdate(t, m, msg)

	if n := capture.callCount(); n != 1 {
		t.Fatalf("WriteFile called %d times, want 1", n)
	}
	call, _ := capture.lastCall()
	if call.path != "my-report.json" {
		t.Errorf("WriteFile path = %q, want %q (Options.ReportPath)", call.path, "my-report.json")
	}
}

// TestSettings_EditedReportPath_ReachesRun asserts that when the user types a
// new path on ScreenReportPath and confirms it, that value — not the original
// Options.ReportPath — is the path WriteFile receives.
func TestSettings_EditedReportPath_ReachesRun(t *testing.T) {
	runner := newFakeSuiteRunner()
	capture := &tuiCaptureWriteFile{}
	o := newReportPathOptions([]string{"suite-a.yaml"}, runner, "default.json", capture)
	m := NewModel(o)

	// Navigate to ScreenReportPath and type an override.
	m = navigateToScreenReportPath(t, m)
	for _, ch := range "override.json" {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	// Enter commits the path and advances to ScreenCatalogFolder.
	m, _ = safeUpdate(t, m, enterKey())
	if m.Screen() != ScreenCatalogFolder {
		t.Fatalf("Screen() after Enter on ScreenReportPath = %q, want %q", m.Screen(), ScreenCatalogFolder)
	}
	if got := m.ReportPath(); got != "override.json" {
		t.Fatalf("ReportPath() = %q after commit, want %q", got, "override.json")
	}

	// Complete the remaining settings screens to start the suite.
	m, _ = safeUpdate(t, m, keyMsg("\r")) // → ScreenMaxConcurrentRuns
	m, cmd := safeUpdate(t, m, keyMsg("\r"))
	// Drive through the pre-flight notice screen if the implementation introduces
	// it before ScreenProgress (queue-wide pre-flight sequence).
	if m.Screen() == ScreenPreflightNotice && cmd != nil {
		msg := runCmd(t, cmd)
		m, cmd = safeUpdate(t, m, msg)
	}
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() = %q, want %q after settings", m.Screen(), ScreenProgress)
	}
	if cmd == nil {
		t.Fatalf("no tea.Cmd returned when starting the suite")
	}
	msg := runCmd(t, cmd)
	if msg == nil {
		t.Fatalf("no message returned from suite-start Cmd")
	}
	m, _ = safeUpdate(t, m, msg)
	_ = m

	if n := capture.callCount(); n != 1 {
		t.Fatalf("WriteFile called %d times, want 1", n)
	}
	call, _ := capture.lastCall()
	if call.path != "override.json" {
		t.Errorf("WriteFile path = %q, want %q (the committed override path, not Options.ReportPath)", call.path, "override.json")
	}
}

// TestSettings_EmptyReportPath_SuppressesWrite asserts that when Options.ReportPath
// is empty, WriteFile is not called after a run — an empty path is the TUI's
// equivalent of --no-report.
func TestSettings_EmptyReportPath_SuppressesWrite(t *testing.T) {
	runner := newFakeSuiteRunner()
	capture := &tuiCaptureWriteFile{}
	// Set the initial path to "" so ScreenReportPath is initialized with an
	// empty confirmed path. The user pressing Enter on ScreenReportPath without
	// typing keeps it empty, and startSelectedSuite skips WriteFile for "".
	o := newReportPathOptions([]string{"suite-a.yaml"}, runner, "", capture)
	m := NewModel(o)

	m, cmd := startSuiteFromSuiteSelect(t, m)
	msg := runCmd(t, cmd)
	if msg == nil {
		t.Fatalf("no tea.Cmd returned when starting the suite")
	}
	safeUpdate(t, m, msg)

	if n := capture.callCount(); n != 0 {
		t.Errorf("WriteFile called %d times with an empty report path; want 0 (write suppressed)", n)
	}
}

// ---------------------------------------------------------------------------
// T7.3: report content is JSON
// ---------------------------------------------------------------------------

// TestSuiteSelect_ReportContent_IsJSON asserts that the bytes WriteFile
// receives are valid JSON — the machine-readable rendering — rather than
// the text progress output the screen showed during the run.
func TestSuiteSelect_ReportContent_IsJSON(t *testing.T) {
	runner := newFakeSuiteRunner()
	capture := &tuiCaptureWriteFile{}
	o := newReportPathOptions([]string{"suite-a.yaml"}, runner, "report.json", capture)
	m := NewModel(o)

	m, cmd := startSuiteFromSuiteSelect(t, m)
	msg := runCmd(t, cmd)
	if msg == nil {
		t.Fatalf("no cmd from starting the suite")
	}
	safeUpdate(t, m, msg)

	call, ok := capture.lastCall()
	if !ok {
		t.Fatal("WriteFile was never called")
	}
	if !json.Valid(call.data) {
		t.Errorf("WriteFile bytes are not valid JSON:\n%s", call.data)
	}
	// Verify the bytes carry at least one semantically meaningful field rather
	// than an empty object ({}), null, or an empty array — all of which satisfy
	// json.Valid but indicate a wiring or rendering fault.
	if !strings.Contains(string(call.data), `"schema_version"`) {
		t.Errorf("WriteFile bytes are valid JSON but do not contain the expected %q field; the content may be empty or zero-valued:\n%s", "schema_version", call.data)
	}
}

// ---------------------------------------------------------------------------
// T7.3 (AC7.6): write-failure and nil-WriteFile surfacing
// ---------------------------------------------------------------------------

// TestSuiteSelect_WriteFileFailure_IsVisibleInView asserts that when the
// injected WriteFile returns a non-nil error after a run completes, the
// error is surfaced in the TUI's rendered view so the user is not silently
// left with a missing report file.
//
// The error text from the injected function ("permission denied") must
// appear somewhere in the view, naming the cause so the user can act on it.
func TestSuiteSelect_WriteFileFailure_IsVisibleInView(t *testing.T) {
	runner := newFakeSuiteRunner()
	capture := &tuiCaptureWriteFile{err: errors.New("permission denied")}
	o := newReportPathOptions([]string{"suite-a.yaml"}, runner, "report.json", capture)
	m := NewModel(o)

	m, cmd := startSuiteFromSuiteSelect(t, m)
	msg := runCmd(t, cmd)
	if msg == nil {
		t.Fatalf("no tea.Cmd returned when starting the suite")
	}
	m, _ = safeUpdate(t, m, msg)

	// A WriteFile error must be surfaced to the user. The rendered view must
	// contain the error cause so the user knows why the report was not written.
	view := safeView(t, m)
	if !strings.Contains(view, "permission denied") {
		t.Errorf("view after WriteFile failure does not surface the error cause %q;\nview:\n%s", "permission denied", view)
	}
}

// ---------------------------------------------------------------------------
// Suite-selection recomputation: derived path shown in settings flow
// ---------------------------------------------------------------------------

// TestSuiteSelect_WithReportPathFor_ShowsDerivedPathInSettingsFlow asserts
// that when Options.ReportPathFor is supplied, ScreenReportPath displays the
// suite-derived path — not the initial placeholder — when the settings flow
// opens after the user confirms their suite selection with Enter.
//
// This specifies that the report-path recomputation must happen at
// suite-selection time (inside updateSuiteSelect, before initSettingScreens
// is called), so the user sees the correct output location during the settings
// flow, not only after completing all settings screens.
func TestSuiteSelect_WithReportPathFor_ShowsDerivedPathInSettingsFlow(t *testing.T) {
	const placeholder = "placeholder.json"
	const suiteName = "suite-a.yaml"
	const derivedPath = "suite-a-derived-report.json"

	o := newFixtureOptions([]string{suiteName}, newFakeSuiteRunner())
	o.ReportPath = placeholder
	o.ReportPathFor = func(_ string) string {
		return derivedPath
	}
	m := NewModel(o)

	// Confirm suite selection (Enter) then navigate through the settings flow
	// to ScreenReportPath.
	m = navigateToScreenReportPath(t, m)

	// Act
	view := safeView(t, m)

	// Assert: the derived path must appear; the placeholder must not.
	if !strings.Contains(view, derivedPath) {
		t.Errorf("ScreenReportPath view after suite selection does not contain the suite-derived path %q;\n"+
			"recomputation must occur at selection time (before initSettingScreens) so the settings flow "+
			"shows the real output path:\n%s", derivedPath, view)
	}
}

// TestSuiteSelect_WithReportPathFor_PreservesManualEdit asserts that when the
// user has manually changed the report path (so it differs from the initial
// Options.ReportPath), re-selecting the suite does not overwrite the edited
// value with the suite-derived one.
//
// The guard is: recompute only when m.reportPath == m.opts.ReportPath.
// Once the user edits the path the values diverge, and the guard must suppress
// further recomputation on subsequent suite selections.
func TestSuiteSelect_WithReportPathFor_PreservesManualEdit(t *testing.T) {
	const placeholder = "placeholder.json"
	const suiteName = "suite-a.yaml"
	const customPath = "my-custom-report.json"

	o := newFixtureOptions([]string{suiteName}, newFakeSuiteRunner())
	o.ReportPath = placeholder
	o.ReportPathFor = func(suitePath string) string {
		return "derived-" + suitePath + ".json"
	}
	m := NewModel(o)

	// Step 1: Enter the settings flow and manually edit the report path.
	m = navigateToScreenReportPath(t, m)
	for _, ch := range customPath {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	// Commit; model advances to ScreenCatalogFolder and m.reportPath = customPath.
	m, _ = safeUpdate(t, m, enterKey())
	if m.Screen() != ScreenCatalogFolder {
		t.Fatalf("after committing report path, Screen() = %q, want %q", m.Screen(), ScreenCatalogFolder)
	}
	if got := m.ReportPath(); got != customPath {
		t.Fatalf("ReportPath() = %q after manual edit; want %q", got, customPath)
	}

	// Step 2: Navigate back to ScreenSuiteSelect via Esc.
	m, _ = safeUpdate(t, m, escKey()) // CatalogFolder → ReportPath
	m, _ = safeUpdate(t, m, escKey()) // ReportPath → Repetitions
	m, _ = safeUpdate(t, m, escKey()) // Repetitions → Retention
	m, _ = safeUpdate(t, m, escKey()) // Retention → SuiteSelect
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("after Esc back through settings, Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	// Step 3: Re-select the suite. The guard must prevent overwriting customPath.
	m = navigateToScreenReportPath(t, m)

	// Assert: the manually-edited path must be preserved.
	if got := m.ReportPath(); got != customPath {
		t.Errorf("ReportPath() = %q after re-selection; want %q (manual edit must not be overwritten by suite-derived recomputation)", got, customPath)
	}
	view := safeView(t, m)
	if !strings.Contains(view, customPath) {
		t.Errorf("ScreenReportPath view after re-selection does not contain the manually-edited path %q;\nuser edits must be preserved:\n%s", customPath, view)
	}
}

// TestSuiteSelect_NilWriteFile_IsReportedInView asserts that when
// Options.WriteFile is nil, the TUI surfaces a report-write failure in the
// rendered view rather than silently skipping the write. A nil WriteFile is a
// wiring omission — a distinct failure mode from a runtime write error — and
// must be visible to the user.
func TestSuiteSelect_NilWriteFile_IsReportedInView(t *testing.T) {
	runner := newFakeSuiteRunner()
	// WriteFile is deliberately nil: wiring omission.
	o := newReportPathOptions([]string{"suite-a.yaml"}, runner, "report.json", nil)
	m := NewModel(o)

	m, cmd := startSuiteFromSuiteSelect(t, m)
	msg := runCmd(t, cmd)
	if msg == nil {
		t.Fatalf("no tea.Cmd returned when starting the suite")
	}
	m, _ = safeUpdate(t, m, msg)

	// A nil WriteFile must be treated as a report-write failure and surfaced to
	// the user, not silently swallowed. The view must mention both the report
	// (so the user knows which file was not written) and an error indicator.
	view := safeView(t, m)
	viewLower := strings.ToLower(view)
	hasReport := strings.Contains(viewLower, "report")
	hasError := strings.Contains(viewLower, "error") || strings.Contains(viewLower, "cannot") || strings.Contains(viewLower, "failed")
	if !hasReport || !hasError {
		t.Errorf("view after completing a run with nil WriteFile does not surface a report-write failure (hasReport=%v, hasError=%v);\nview:\n%s", hasReport, hasError, view)
	}
}
