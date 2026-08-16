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

// editKey returns the key that activates the report-path inline editor on
// the suite-select screen.
func editKey() tea.KeyMsg {
	return keyMsg("e")
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

// TestSuiteSelect_ReportPath_IsDisplayedBeforeRunStarts asserts that the
// suite-select screen's rendered output includes the current report path so a
// user can see where the report will be written before starting a run.
func TestSuiteSelect_ReportPath_IsDisplayedBeforeRunStarts(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "report.json", nil)
	m := NewModel(o)

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	view := safeView(t, m)
	if !strings.Contains(view, "report.json") {
		t.Errorf("suite-select View() does not contain the report path %q:\n%s", "report.json", view)
	}
}

// ---------------------------------------------------------------------------
// T7.3: inline edit — commit
// ---------------------------------------------------------------------------

// TestSuiteSelect_ReportPath_EditKey_EntersEditMode asserts that pressing the
// edit key on the suite-select screen activates the inline editor, so a user
// can type a new path.
func TestSuiteSelect_ReportPath_EditKey_EntersEditMode(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "report.json", nil)
	m := NewModel(o)

	m, _ = safeUpdate(t, m, editKey())

	if !m.EditingReportPath() {
		t.Error("EditingReportPath() = false after pressing the edit key; want true (inline editor active)")
	}
}

// TestSuiteSelect_ReportPath_InlineEdit_Commit_UpdatesPath asserts that the
// user can type a new path and commit it with Enter, and the new value is
// reflected in ReportPath().
func TestSuiteSelect_ReportPath_InlineEdit_Commit_UpdatesPath(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "old-report.json", nil)
	m := NewModel(o)

	m, _ = safeUpdate(t, m, editKey())
	if !m.EditingReportPath() {
		t.Fatalf("EditingReportPath() = false after pressing the edit key")
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

// TestSuiteSelect_ReportPath_InlineEdit_Commit_ShownInView asserts that the
// committed path appears in the suite-select View() output.
func TestSuiteSelect_ReportPath_InlineEdit_Commit_ShownInView(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "old.json", nil)
	m := NewModel(o)

	m, _ = safeUpdate(t, m, editKey())
	for _, ch := range "custom.json" {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, enterKey())

	view := safeView(t, m)
	if !strings.Contains(view, "custom.json") {
		t.Errorf("suite-select View() after commit does not contain %q:\n%s", "custom.json", view)
	}
}

// ---------------------------------------------------------------------------
// T7.3: inline edit — cancel
// ---------------------------------------------------------------------------

// TestSuiteSelect_ReportPath_InlineEdit_Cancel_RestoresPrevious asserts that
// pressing Escape while editing cancels the edit and restores the previous
// path value.
func TestSuiteSelect_ReportPath_InlineEdit_Cancel_RestoresPrevious(t *testing.T) {
	o := newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "original.json", nil)
	m := NewModel(o)

	m, _ = safeUpdate(t, m, editKey())
	// Prerequisite: the edit key must activate the inline editor. Without this
	// guard the cancel path is never reached and the assertions below pass
	// vacuously — the test would not catch a broken cancel implementation.
	if !m.EditingReportPath() {
		t.Fatalf("EditingReportPath() = false after pressing the edit key; inline editor did not activate — cancel test cannot run (prerequisite missing)")
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

// TestSuiteSelect_ReportPath_EditMode_NavigationKeysAreText asserts that while
// the inline editor is active, the Down key is treated as text input rather
// than cursor movement — the one state in which that is true.
func TestSuiteSelect_ReportPath_EditMode_NavigationKeysAreText(t *testing.T) {
	suites := []string{"suite-a.yaml", "suite-b.yaml"}
	o := newReportPathOptions(suites, newFakeSuiteRunner(), "report.json", nil)
	m := NewModel(o)

	cursorBefore := m.suiteCursor

	m, _ = safeUpdate(t, m, editKey())
	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})

	if m.suiteCursor != cursorBefore {
		t.Errorf("suiteCursor changed from %d to %d while editing: Down key moved the cursor instead of being treated as text", cursorBefore, m.suiteCursor)
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

// TestSuiteSelect_EditedReportPath_ReachesRun asserts that when the user edits
// the report path before starting, the committed value — not the original
// Options.ReportPath — is the path WriteFile receives.
func TestSuiteSelect_EditedReportPath_ReachesRun(t *testing.T) {
	runner := newFakeSuiteRunner()
	capture := &tuiCaptureWriteFile{}
	o := newReportPathOptions([]string{"suite-a.yaml"}, runner, "default.json", capture)
	m := NewModel(o)

	m, _ = safeUpdate(t, m, editKey())
	for _, ch := range "override.json" {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, enterKey())

	if got := m.ReportPath(); got != "override.json" {
		t.Fatalf("ReportPath() = %q after commit, want %q", got, "override.json")
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

// TestSuiteSelect_EmptyReportPath_SuppressesWrite asserts that when the user
// clears the path to an empty string, WriteFile is not called after a run —
// an empty path is the TUI's equivalent of the CLI's --no-report flag.
func TestSuiteSelect_EmptyReportPath_SuppressesWrite(t *testing.T) {
	runner := newFakeSuiteRunner()
	capture := &tuiCaptureWriteFile{}
	o := newReportPathOptions([]string{"suite-a.yaml"}, runner, "report.json", capture)
	m := NewModel(o)

	// Clear the path by entering edit mode and committing with nothing typed.
	m, _ = safeUpdate(t, m, editKey())
	m, _ = safeUpdate(t, m, enterKey())

	if got := m.ReportPath(); got != "" {
		t.Fatalf("ReportPath() = %q after committing empty; want empty string", got)
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
