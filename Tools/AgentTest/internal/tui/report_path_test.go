package tui

// report_path_test.go covers the persistent JSON report file feature for the
// interactive frontend:
//
//   T7.3 - The default report path is displayed before a run starts and can
//           be overridden, and the chosen path reaches the started run.
//
// These tests drive Model through Options.ReportPath / Options.WriteFile and
// the suite-select screen's inline edit affordance. No real filesystem is
// touched: WriteFile is an in-memory capture function supplied on Options.
//
// The inline-edit affordance lives on the suite-select screen alongside the
// retention toggle. Pressing 'e' enters edit mode; while in edit mode, key
// presses are text input (not navigation); Enter commits the typed value;
// Escape restores the previous value.

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

// navigateToReportPathEdit navigates from ScreenSuiteSelect to the settings
// screen, positions the cursor on the report-path entry, and opens the inline
// editor by pressing Enter. On return, the model is on ScreenSettings with
// the report-path inline editor active (EditingReportPath() == true).
func navigateToReportPathEdit(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() after Tab = %q, want ScreenSettings (prerequisite failed)", m.Screen())
	}
	entries := m.SettingsEntries()
	for i, e := range entries {
		if e.Kind == SettingReportPath {
			for j := 0; j < i; j++ {
				m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
			}
			m, _ = safeUpdate(t, m, keyType(tea.KeyEnter)) // open inline editor
			return m
		}
	}
	t.Fatalf("SettingsEntries() has no SettingReportPath entry")
	return m
}

// enterKey returns the key that commits the in-progress edit.
func enterKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEnter}
}

// escKey returns the key that cancels the in-progress edit.
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
// T7.3: display on suite-select screen
// ---------------------------------------------------------------------------

// TestSettings_ReportPath_IsDisplayedBeforeRunStarts asserts that the settings
// screen's rendered output includes the current report path so a user can see
// where the report will be written before starting a run.
func TestSettings_ReportPath_IsDisplayedBeforeRunStarts(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "report.json", nil)
	m := NewModel(o)

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() after Tab = %q, want %q", m.Screen(), ScreenSettings)
	}

	view := safeView(t, m)
	if !strings.Contains(view, "report.json") {
		t.Errorf("settings View() does not contain the report path %q:\n%s", "report.json", view)
	}
}

// ---------------------------------------------------------------------------
// T7.3: inline edit — commit
// ---------------------------------------------------------------------------

// TestSettings_ReportPath_ActionKey_EntersEditMode asserts that pressing Enter
// on the report-path entry of the settings screen activates the inline editor.
func TestSettings_ReportPath_ActionKey_EntersEditMode(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "report.json", nil)
	m := NewModel(o)

	m = navigateToReportPathEdit(t, m)

	if !m.EditingReportPath() {
		t.Error("EditingReportPath() = false after pressing Enter on the report-path entry; want true (inline editor active)")
	}
}

// TestSettings_ReportPath_InlineEdit_Commit_UpdatesPath asserts that the user
// can type a new path on the settings screen and commit it with Enter, and the
// new value is reflected in ReportPath().
func TestSettings_ReportPath_InlineEdit_Commit_UpdatesPath(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "old-report.json", nil)
	m := NewModel(o)

	m = navigateToReportPathEdit(t, m)
	if !m.EditingReportPath() {
		t.Fatalf("EditingReportPath() = false after opening the inline editor via settings screen")
	}

	for _, ch := range "new-report.json" {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, enterKey())

	if m.EditingReportPath() {
		t.Error("EditingReportPath() = true after Enter; want false (edit mode exited)")
	}
	if got := m.ReportPath(); got != "new-report.json" {
		t.Errorf("ReportPath() = %q after commit, want %q", got, "new-report.json")
	}
}

// TestSettings_ReportPath_InlineEdit_Commit_ShownInView asserts that the
// committed path appears in the settings screen's View() output.
func TestSettings_ReportPath_InlineEdit_Commit_ShownInView(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "old.json", nil)
	m := NewModel(o)

	m = navigateToReportPathEdit(t, m)
	for _, ch := range "custom.json" {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, enterKey())

	view := safeView(t, m)
	if !strings.Contains(view, "custom.json") {
		t.Errorf("settings View() after commit does not contain %q:\n%s", "custom.json", view)
	}
}

// ---------------------------------------------------------------------------
// T7.3: inline edit — cancel
// ---------------------------------------------------------------------------

// TestSettings_ReportPath_InlineEdit_Cancel_RestoresPrevious asserts that
// pressing Escape while editing on the settings screen cancels the edit and
// restores the previous path value.
func TestSettings_ReportPath_InlineEdit_Cancel_RestoresPrevious(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "original.json", nil)
	m := NewModel(o)

	m = navigateToReportPathEdit(t, m)
	if !m.EditingReportPath() {
		t.Fatalf("EditingReportPath() = false after opening the inline editor via settings screen — cancel test cannot run (prerequisite missing)")
	}
	for _, ch := range "half-typed" {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, escKey())

	if m.EditingReportPath() {
		t.Error("EditingReportPath() = true after Escape; want false (edit mode cancelled)")
	}
	if got := m.ReportPath(); got != "original.json" {
		t.Errorf("ReportPath() = %q after cancel, want %q (original path restored)", got, "original.json")
	}
}

// ---------------------------------------------------------------------------
// T7.3: edit mode suspends list navigation
// ---------------------------------------------------------------------------

// TestSettings_ReportPath_EditMode_NavigationKeysAreConsumed asserts that while
// the inline editor is active on the settings screen, the Down key does not
// move the settings cursor — navigation keys are consumed without effect during
// editing.
func TestSettings_ReportPath_EditMode_NavigationKeysAreConsumed(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "report.json", nil)
	m := NewModel(o)

	m = navigateToReportPathEdit(t, m)
	cursorBefore := m.settingsCursor

	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))

	if m.settingsCursor != cursorBefore {
		t.Errorf("settingsCursor changed from %d to %d while editing: Down key must not move the cursor during inline editing", cursorBefore, m.settingsCursor)
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

	m, cmd := safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() after selecting suite = %q, want %q", m.Screen(), ScreenProgress)
	}
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

// TestSettings_EditedReportPath_ReachesRun asserts that when the user edits
// the report path via the settings screen before starting, the committed value
// — not the original Options.ReportPath — is the path WriteFile receives.
func TestSettings_EditedReportPath_ReachesRun(t *testing.T) {
	runner := newFakeSuiteRunner()
	capture := &tuiCaptureWriteFile{}
	o := newReportPathOptions([]string{"suite-a.yaml"}, runner, "default.json", capture)
	m := NewModel(o)

	m = navigateToReportPathEdit(t, m)
	for _, ch := range "override.json" {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, enterKey())

	if got := m.ReportPath(); got != "override.json" {
		t.Fatalf("ReportPath() = %q after commit, want %q", got, "override.json")
	}

	// Return to suite-select and start the run.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after Esc from settings = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
	m, cmd := safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() after selecting suite = %q, want %q", m.Screen(), ScreenProgress)
	}
	msg := runCmd(t, cmd)
	if msg == nil {
		t.Fatalf("no tea.Cmd returned when starting the suite")
	}
	safeUpdate(t, m, msg)

	if n := capture.callCount(); n != 1 {
		t.Fatalf("WriteFile called %d times, want 1", n)
	}
	call, _ := capture.lastCall()
	if call.path != "override.json" {
		t.Errorf("WriteFile path = %q, want %q (the committed override path, not Options.ReportPath)", call.path, "override.json")
	}
}

// TestSettings_EmptyReportPath_SuppressesWrite asserts that when the user
// clears the path to an empty string via the settings screen, WriteFile is not
// called after a run — an empty path is the TUI's equivalent of --no-report.
func TestSettings_EmptyReportPath_SuppressesWrite(t *testing.T) {
	runner := newFakeSuiteRunner()
	capture := &tuiCaptureWriteFile{}
	o := newReportPathOptions([]string{"suite-a.yaml"}, runner, "report.json", capture)
	m := NewModel(o)

	// Clear the path by opening the editor on the settings screen and committing
	// with nothing typed.
	m = navigateToReportPathEdit(t, m)
	m, _ = safeUpdate(t, m, enterKey())

	if got := m.ReportPath(); got != "" {
		t.Fatalf("ReportPath() = %q after committing empty; want empty string", got)
	}

	// Return to suite-select and start the run.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after Esc from settings = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
	m, cmd := safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() after selecting suite = %q, want %q", m.Screen(), ScreenProgress)
	}
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

	m, cmd := safeUpdate(t, m, keyMsg("\r"))
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

	m, cmd := safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() after selecting suite = %q, want %q", m.Screen(), ScreenProgress)
	}
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

	m, cmd := safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() after selecting suite = %q, want %q", m.Screen(), ScreenProgress)
	}
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
