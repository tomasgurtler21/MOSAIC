package screens_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Fake catalog for TestSuiteScreen tests
// ---------------------------------------------------------------------------

// fakeCatalog is a test double for testrun.CatalogPort that returns canned
// data without touching the filesystem.
type fakeCatalog struct {
	ids   []string
	modes map[string][]string // workflow ID -> modes
}

func (f *fakeCatalog) WorkflowIDs() []string { return f.ids }

func (f *fakeCatalog) WorkflowModes(id string) ([]string, error) {
	modes, ok := f.modes[id]
	if !ok {
		return nil, nil
	}
	return modes, nil
}

func (f *fakeCatalog) Workflows() []testcatalog.CatalogEntry                    { return nil }
func (f *fakeCatalog) SmokeSet() []testcatalog.CatalogEntry                     { return nil }
func (f *fakeCatalog) FullSuite() []testcatalog.CatalogEntry                    { return nil }
func (f *fakeCatalog) WorkflowByID(_ string) ([]testcatalog.CatalogEntry, error) { return nil, nil }
func (f *fakeCatalog) SidecarPath(_, _ string) string                            { return "" }
func (f *fakeCatalog) UnionInfrastructureAgentKeys() []string                    { return []string{} }

// newFakeCatalog builds a fakeCatalog for testing.
//   - ids is the ordered list of workflow IDs returned by WorkflowIDs().
//   - modes maps each workflow ID to its declared execution modes.
func newFakeCatalog(ids []string, modes map[string][]string) testrun.CatalogPort {
	return &fakeCatalog{ids: ids, modes: modes}
}

// newTestSuiteScreen is a helper that constructs a TestSuiteScreen with a
// pre-built fake catalog.
func newTestSuiteScreen(ids []string, modes map[string][]string) *screens.TestSuiteScreen {
	cat := newFakeCatalog(ids, modes)
	return screens.NewTestSuiteScreen(80, 24, screens.Styles{}, cat)
}

// pressTestSuiteKey sends a single key to the TestSuiteScreen.
func pressTestSuiteKey(s *screens.TestSuiteScreen, keyType tea.KeyType) {
	s.Update(tea.KeyMsg{Type: keyType})
}

// pressTestSuiteRune sends a rune key to the TestSuiteScreen.
func pressTestSuiteRune(s *screens.TestSuiteScreen, r rune) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// ---------------------------------------------------------------------------
// TestSuiteScreen — initial state
// ---------------------------------------------------------------------------

// TestTestSuiteScreen_InitialState verifies that a freshly created
// TestSuiteScreen starts with Done() and Back() false.
func TestTestSuiteScreen_InitialState(t *testing.T) {
	s := newTestSuiteScreen([]string{"wf-a"}, map[string][]string{"wf-a": {"auto"}})
	if s.Done() {
		t.Error("Done() is true on construction; screen must start in pending state")
	}
	if s.Back() {
		t.Error("Back() is true on construction")
	}
}

// ---------------------------------------------------------------------------
// TestSuiteScreen — scope selection: Smoke Set
// ---------------------------------------------------------------------------

// TestTestSuiteScreen_ScopeSmoke_DoneImmediately verifies that selecting
// "Smoke Set" sets Done() to true without any sub-screen navigation.
func TestTestSuiteScreen_ScopeSmoke_DoneImmediately(t *testing.T) {
	s := newTestSuiteScreen(nil, nil)
	// Smoke Set is cursor 0 (first option); press Enter to select.
	pressTestSuiteKey(s, tea.KeyEnter)
	if !s.Done() {
		t.Error("Done() is false after selecting Smoke Set; no sub-screen required")
	}
	if s.Scope() != testrun.ScopeSmoke {
		t.Errorf("Scope() = %v, want ScopeSmoke (%v)", s.Scope(), testrun.ScopeSmoke)
	}
}

// ---------------------------------------------------------------------------
// TestSuiteScreen — scope selection: Full Suite
// ---------------------------------------------------------------------------

// TestTestSuiteScreen_ScopeFull_DoneImmediately verifies that selecting
// "Full Suite" sets Done() to true without any sub-screen navigation.
func TestTestSuiteScreen_ScopeFull_DoneImmediately(t *testing.T) {
	s := newTestSuiteScreen(nil, nil)
	// Full Suite is cursor 1; move down once.
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyEnter)
	if !s.Done() {
		t.Error("Done() is false after selecting Full Suite; no sub-screen required")
	}
	if s.Scope() != testrun.ScopeFull {
		t.Errorf("Scope() = %v, want ScopeFull (%v)", s.Scope(), testrun.ScopeFull)
	}
}

// ---------------------------------------------------------------------------
// TestSuiteScreen — scope selection: Single Workflow (single mode)
// ---------------------------------------------------------------------------

// TestTestSuiteScreen_ScopeSingle_SingleModeWorkflow_SkipsModeStep verifies
// that when a single-mode workflow is selected, Done() is reached without a
// mode picker step, and SelectedMode() is empty (auto-inferred).
func TestTestSuiteScreen_ScopeSingle_SingleModeWorkflow_SkipsModeStep(t *testing.T) {
	ids := []string{"wf-single"}
	modes := map[string][]string{"wf-single": {"auto"}}
	s := newTestSuiteScreen(ids, modes)

	// Navigate to Single Workflow (cursor 2).
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyEnter) // select scope

	if s.Done() {
		t.Fatal("Done() is true immediately after selecting Single Workflow scope; should show workflow picker")
	}

	// The workflow picker should now be active. Press Enter to select the only workflow.
	pressTestSuiteKey(s, tea.KeyEnter)

	if !s.Done() {
		t.Error("Done() is false after selecting a single-mode workflow; mode step should be skipped")
	}
	if s.Scope() != testrun.ScopeSingle {
		t.Errorf("Scope() = %v, want ScopeSingle", s.Scope())
	}
	workflows := s.SelectedWorkflows()
	if len(workflows) != 1 || workflows[0] != "wf-single" {
		t.Errorf("SelectedWorkflows() = %v, want [wf-single]", workflows)
	}
	if mode := s.SelectedMode(); mode != "" {
		t.Errorf("SelectedMode() = %q, want empty (single-mode auto-inferred)", mode)
	}
}

// TestTestSuiteScreen_ScopeSingle_MultiModeWorkflow_ShowsModeStep verifies
// that when a multi-mode workflow is selected, the mode picker appears and
// Done() is not set until a mode is chosen.
func TestTestSuiteScreen_ScopeSingle_MultiModeWorkflow_ShowsModeStep(t *testing.T) {
	ids := []string{"wf-multi"}
	modes := map[string][]string{"wf-multi": {"auto", "auto-review"}}
	s := newTestSuiteScreen(ids, modes)

	// Select Single Workflow scope (cursor 2).
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyEnter)

	// Workflow picker: select the only workflow.
	pressTestSuiteKey(s, tea.KeyEnter)

	// Screen must not be done yet — mode picker should appear.
	if s.Done() {
		t.Fatal("Done() is true before mode is selected; multi-mode workflow must show mode picker")
	}

	// Mode picker: select the first mode.
	pressTestSuiteKey(s, tea.KeyEnter)

	if !s.Done() {
		t.Error("Done() is false after selecting a mode from the mode picker")
	}
	if s.Scope() != testrun.ScopeSingle {
		t.Errorf("Scope() = %v, want ScopeSingle", s.Scope())
	}
	if got := s.SelectedMode(); got != "auto" {
		t.Errorf("SelectedMode() = %q, want %q (first declared mode)", got, "auto")
	}
}

// TestTestSuiteScreen_ScopeSingle_MultiModeWorkflow_SecondModeSelectable verifies
// that the user can select the second mode in the mode picker.
func TestTestSuiteScreen_ScopeSingle_MultiModeWorkflow_SecondModeSelectable(t *testing.T) {
	ids := []string{"wf-multi"}
	modes := map[string][]string{"wf-multi": {"auto", "auto-review"}}
	s := newTestSuiteScreen(ids, modes)

	// Select Single Workflow scope.
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyEnter)

	// Select the workflow.
	pressTestSuiteKey(s, tea.KeyEnter)

	// Mode picker: move to second mode and select.
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyEnter)

	if !s.Done() {
		t.Fatal("Done() is false after selecting second mode")
	}
	if got := s.SelectedMode(); got != "auto-review" {
		t.Errorf("SelectedMode() = %q, want %q", got, "auto-review")
	}
}

// ---------------------------------------------------------------------------
// TestSuiteScreen — scope selection: Custom Selection
// ---------------------------------------------------------------------------

// TestTestSuiteScreen_ScopeCustom_ShowsMultiSelect verifies that selecting
// "Custom Selection" shows a multi-select workflow picker and Done() is not
// set until the user confirms.
func TestTestSuiteScreen_ScopeCustom_ShowsMultiSelect(t *testing.T) {
	ids := []string{"wf-a", "wf-b"}
	modes := map[string][]string{"wf-a": {"auto"}, "wf-b": {"auto"}}
	s := newTestSuiteScreen(ids, modes)

	// Custom Selection is cursor 3; move down three times.
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyEnter) // select scope

	if s.Done() {
		t.Fatal("Done() is true immediately after selecting Custom Selection; should show multi-select")
	}

	// Toggle first workflow and confirm.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // space to toggle
	pressTestSuiteKey(s, tea.KeyEnter)

	if !s.Done() {
		t.Fatal("Done() is false after confirming custom selection")
	}
	if s.Scope() != testrun.ScopeCustom {
		t.Errorf("Scope() = %v, want ScopeCustom", s.Scope())
	}
	got := s.SelectedWorkflows()
	if len(got) != 1 || got[0] != "wf-a" {
		t.Errorf("SelectedWorkflows() = %v, want [wf-a]", got)
	}
}

// TestTestSuiteScreen_ScopeCustom_MultipleSelections verifies that multiple
// workflows can be selected in the custom selection step.
func TestTestSuiteScreen_ScopeCustom_MultipleSelections(t *testing.T) {
	ids := []string{"wf-a", "wf-b", "wf-c"}
	modes := map[string][]string{}
	s := newTestSuiteScreen(ids, modes)

	// Navigate to Custom Selection (cursor 3).
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyEnter)

	// Toggle wf-a, move to wf-b, toggle wf-b, then confirm.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // toggle wf-a
	pressTestSuiteKey(s, tea.KeyDown)
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // toggle wf-b
	pressTestSuiteKey(s, tea.KeyEnter)

	if !s.Done() {
		t.Fatal("Done() is false after confirming")
	}
	got := s.SelectedWorkflows()
	if len(got) != 2 {
		t.Fatalf("SelectedWorkflows() = %v, want 2 entries", got)
	}
	wfMap := map[string]bool{}
	for _, id := range got {
		wfMap[id] = true
	}
	if !wfMap["wf-a"] {
		t.Error("wf-a not in SelectedWorkflows()")
	}
	if !wfMap["wf-b"] {
		t.Error("wf-b not in SelectedWorkflows()")
	}
}

// ---------------------------------------------------------------------------
// TestSuiteScreen — backward navigation
// ---------------------------------------------------------------------------

// TestTestSuiteScreen_EscOnScopeStep_SetsBack verifies that pressing Esc on the
// scope step sets Back() to true.
func TestTestSuiteScreen_EscOnScopeStep_SetsBack(t *testing.T) {
	s := newTestSuiteScreen(nil, nil)
	pressTestSuiteKey(s, tea.KeyEsc)
	if !s.Back() {
		t.Error("Back() is false after Esc on scope step; expected true")
	}
	if s.Done() {
		t.Error("Done() is true after Esc; expected false")
	}
}

// TestTestSuiteScreen_EscOnWorkflowStep_ReturnsToScope verifies that pressing
// Esc on the workflow picker returns to the scope step without setting Back().
func TestTestSuiteScreen_EscOnWorkflowStep_ReturnsToScope(t *testing.T) {
	ids := []string{"wf-a"}
	modes := map[string][]string{"wf-a": {"auto"}}
	s := newTestSuiteScreen(ids, modes)

	// Navigate to Single Workflow scope.
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyEnter)

	// We should now be on the workflow picker. Press Esc.
	pressTestSuiteKey(s, tea.KeyEsc)

	if s.Back() {
		t.Error("Back() is true after Esc on workflow step; should return to scope step, not back out")
	}
	if s.Done() {
		t.Error("Done() is true after Esc on workflow step")
	}

	// After returning to scope, pressing Esc should now set Back().
	pressTestSuiteKey(s, tea.KeyEsc)
	if !s.Back() {
		t.Error("Back() is false after Esc on scope step (after returning from workflow step)")
	}
}

// TestTestSuiteScreen_EscOnModeStep_ReturnsToWorkflow verifies that pressing
// Esc on the mode picker returns to the workflow picker.
func TestTestSuiteScreen_EscOnModeStep_ReturnsToWorkflow(t *testing.T) {
	ids := []string{"wf-multi"}
	modes := map[string][]string{"wf-multi": {"auto", "auto-review"}}
	s := newTestSuiteScreen(ids, modes)

	// Select Single Workflow scope.
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyDown)
	pressTestSuiteKey(s, tea.KeyEnter)

	// Select the workflow (enters mode picker).
	pressTestSuiteKey(s, tea.KeyEnter)

	if s.Done() {
		t.Fatal("Done() is true after selecting multi-mode workflow; mode picker must appear")
	}

	// Press Esc on mode step — should return to workflow picker.
	pressTestSuiteKey(s, tea.KeyEsc)

	if s.Back() {
		t.Error("Back() is true after Esc on mode step; should return to workflow picker")
	}
	if s.Done() {
		t.Error("Done() is true after Esc on mode step")
	}
}

// ---------------------------------------------------------------------------
// TestSuiteScreen — View rendering
// ---------------------------------------------------------------------------

// TestTestSuiteScreen_View_ScopeStep_ShowsAllFourOptions verifies that the
// scope step renders all four scope labels.
func TestTestSuiteScreen_View_ScopeStep_ShowsAllFourOptions(t *testing.T) {
	s := newTestSuiteScreen(nil, nil)
	view := s.View()
	for _, label := range []string{"Smoke Set", "Full Suite", "Single Workflow", "Custom Selection"} {
		if !strings.Contains(view, label) {
			t.Errorf("View() on scope step does not contain %q; all four options must be visible", label)
		}
	}
}

// TestTestSuiteScreen_View_IsNonEmpty verifies that View() returns a non-empty
// string.
func TestTestSuiteScreen_View_IsNonEmpty(t *testing.T) {
	s := newTestSuiteScreen(nil, nil)
	if v := s.View(); v == "" {
		t.Error("View() returned empty string")
	}
}

// ---------------------------------------------------------------------------
// TestSuiteScreen — Reset
// ---------------------------------------------------------------------------

// TestTestSuiteScreen_Reset_ClearsState verifies that Reset() returns the
// screen to its initial state.
func TestTestSuiteScreen_Reset_ClearsState(t *testing.T) {
	s := newTestSuiteScreen(nil, nil)
	// Select Smoke Set to set Done().
	pressTestSuiteKey(s, tea.KeyEnter)
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after Smoke Set selection")
	}
	s.Reset()
	if s.Done() {
		t.Error("Done() is true after Reset; expected false")
	}
	if s.Back() {
		t.Error("Back() is true after Reset; expected false")
	}
}

// ---------------------------------------------------------------------------
// TestSuiteScreen — Resize
// ---------------------------------------------------------------------------

// TestTestSuiteScreen_Resize_DoesNotPanic verifies that Resize does not panic.
func TestTestSuiteScreen_Resize_DoesNotPanic(t *testing.T) {
	s := newTestSuiteScreen([]string{"wf"}, map[string][]string{"wf": {"auto"}})
	s.Resize(120, 40)
}
