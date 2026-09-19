package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"mosaic-common/tui/widgets"
	"mosaic-run/internal/testrun"
)

// testSuiteStep identifies the current step within the suite selection screen.
type testSuiteStep int

const (
	// testSuiteStepScope is the main scope selection step: smoke, full, single, custom.
	testSuiteStepScope testSuiteStep = iota

	// testSuiteStepWorkflow is the single-workflow picker step (for ScopeSingle).
	testSuiteStepWorkflow

	// testSuiteStepMode is the mode picker step (for ScopeSingle when the selected
	// workflow declares multiple modes).
	testSuiteStepMode

	// testSuiteStepCustom is the multi-select workflow picker step (for ScopeCustom).
	testSuiteStepCustom

	testSuiteStepDone
)

// scopeOption describes one entry in the scope list.
type scopeOption struct {
	scope testrun.TestScope
	label string
}

// testSuiteScopes lists the four scope options in cursor order.
var testSuiteScopes = []scopeOption{
	{scope: testrun.ScopeSmoke, label: "Smoke Set"},
	{scope: testrun.ScopeFull, label: "Full Suite"},
	{scope: testrun.ScopeSingle, label: "Single Workflow"},
	{scope: testrun.ScopeCustom, label: "Custom Selection"},
}

// TestSuiteScreen presents the test suite/scope selection flow. The user first
// picks the scope (Smoke Set, Full Suite, Single Workflow, or Custom Selection).
// For Single Workflow the screen presents a workflow picker, then a mode picker
// if the workflow declares multiple modes. For Custom Selection the screen
// presents a multi-select workflow picker.
//
// Navigation contract:
//   - Completing all applicable steps -> Done() == true.
//   - Esc on the scope step -> Back() == true.
//   - Esc on sub-steps -> returns to the previous step.
type TestSuiteScreen struct {
	step    testSuiteStep
	back    bool
	cursor  int // used in scope and mode steps
	catalog testrun.CatalogPort
	width   int
	height  int
	styles  Styles

	// Workflow list for Single Workflow scope.
	workflowList *widgets.List

	// Mode list for Single Workflow when multiple modes are declared.
	modeList *widgets.List

	// Multi-select for Custom Selection scope.
	customSelect *widgets.MultiSelect

	// selectedScope is the scope confirmed by the user.
	selectedScope testrun.TestScope

	// selectedWorkflow is the workflow ID selected in testSuiteStepWorkflow.
	selectedWorkflow string

	// selectedMode is the mode selected in testSuiteStepMode.
	// Empty when the workflow declares exactly one mode (mode is auto-inferred).
	selectedMode string

	// selectedCustom holds the custom-selected workflow IDs.
	selectedCustom []string

	// availableModes holds the modes for the selected workflow (populated when
	// entering testSuiteStepMode).
	availableModes []string
}

// NewTestSuiteScreen creates the suite/scope selection screen.
// catalog provides workflow IDs and modes for the workflow picker sub-screens.
// Accepts testrun.CatalogPort (not *testcatalog.Catalog) so the screen can be
// constructed in tests without a filesystem-backed catalog.
func NewTestSuiteScreen(width, height int, styles Styles, catalog testrun.CatalogPort) *TestSuiteScreen {
	return &TestSuiteScreen{
		step:    testSuiteStepScope,
		cursor:  0,
		catalog: catalog,
		width:   width,
		height:  height,
		styles:  styles,
	}
}

// Update processes a key message for the current step.
func (s *TestSuiteScreen) Update(msg tea.Msg) tea.Cmd {
	switch s.step {
	case testSuiteStepScope:
		return s.updateScopeStep(msg)
	case testSuiteStepWorkflow:
		return s.updateWorkflowStep(msg)
	case testSuiteStepMode:
		return s.updateModeStep(msg)
	case testSuiteStepCustom:
		return s.updateCustomStep(msg)
	}
	return nil
}

func (s *TestSuiteScreen) updateScopeStep(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch keyMsg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(testSuiteScopes)-1 {
			s.cursor++
		}
	case "enter":
		s.selectedScope = testSuiteScopes[s.cursor].scope
		switch s.selectedScope {
		case testrun.ScopeSmoke, testrun.ScopeFull:
			// No sub-screen needed; proceed directly to done.
			s.step = testSuiteStepDone
		case testrun.ScopeSingle:
			// Show workflow picker.
			s.initWorkflowList()
			s.step = testSuiteStepWorkflow
		case testrun.ScopeCustom:
			// Show multi-select workflow picker.
			s.initCustomSelect()
			s.step = testSuiteStepCustom
		}
	case "esc":
		s.back = true
	}
	return nil
}

func (s *TestSuiteScreen) updateWorkflowStep(msg tea.Msg) tea.Cmd {
	if s.workflowList == nil {
		return nil
	}
	s.workflowList.Update(msg)
	if s.workflowList.Back() {
		// Return to scope step.
		s.workflowList.Reset()
		s.step = testSuiteStepScope
		s.cursor = indexOfScope(testrun.ScopeSingle)
		return nil
	}
	if s.workflowList.Done() {
		s.selectedWorkflow = s.workflowList.SelectedID()
		// Check if this workflow has multiple modes.
		modes, _ := s.catalog.WorkflowModes(s.selectedWorkflow)
		if len(modes) > 1 {
			// Show mode picker.
			s.availableModes = modes
			s.initModeList(modes)
			s.step = testSuiteStepMode
		} else {
			// Single mode: auto-infer, skip mode step.
			s.selectedMode = ""
			s.step = testSuiteStepDone
		}
	}
	return nil
}

func (s *TestSuiteScreen) updateModeStep(msg tea.Msg) tea.Cmd {
	if s.modeList == nil {
		return nil
	}
	s.modeList.Update(msg)
	if s.modeList.Back() {
		// Return to workflow picker.
		s.modeList.Reset()
		s.workflowList.Reset()
		s.step = testSuiteStepWorkflow
		return nil
	}
	if s.modeList.Done() {
		s.selectedMode = s.modeList.SelectedID()
		s.step = testSuiteStepDone
	}
	return nil
}

func (s *TestSuiteScreen) updateCustomStep(msg tea.Msg) tea.Cmd {
	if s.customSelect == nil {
		return nil
	}
	s.customSelect.Update(msg)
	if s.customSelect.Back() {
		// Return to scope step.
		s.customSelect.Reset()
		s.step = testSuiteStepScope
		s.cursor = indexOfScope(testrun.ScopeCustom)
		return nil
	}
	if s.customSelect.Done() {
		s.selectedCustom = s.customSelect.SelectedIDs()
		s.step = testSuiteStepDone
	}
	return nil
}

// View renders the current step.
func (s *TestSuiteScreen) View() string {
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc back  ctrl+c quit")

	switch s.step {
	case testSuiteStepScope:
		title := s.styles.Title.Width(s.width).Render("Select Test Suite")
		subtitle := s.styles.Subtitle.Width(s.width).Render("Choose the scope of tests to run.")
		var body strings.Builder
		for i, opt := range testSuiteScopes {
			if i == s.cursor {
				body.WriteString("▶ " + s.styles.Selected.Render(opt.label) + "\n")
			} else {
				body.WriteString("  " + s.styles.Body.Render(opt.label) + "\n")
			}
		}
		return strings.Join([]string{title, subtitle, border, body.String(), border, help}, "\n")

	case testSuiteStepWorkflow:
		title := s.styles.Title.Width(s.width).Render("Select Workflow")
		subtitle := s.styles.Subtitle.Width(s.width).Render("Choose the workflow to run.")
		listView := ""
		if s.workflowList != nil {
			listView = s.workflowList.View()
		}
		return strings.Join([]string{title, subtitle, border, listView, border, help}, "\n")

	case testSuiteStepMode:
		title := s.styles.Title.Width(s.width).Render("Select Mode")
		subtitle := s.styles.Subtitle.Width(s.width).Render("Choose the execution mode for this workflow.")
		listView := ""
		if s.modeList != nil {
			listView = s.modeList.View()
		}
		return strings.Join([]string{title, subtitle, border, listView, border, help}, "\n")

	case testSuiteStepCustom:
		title := s.styles.Title.Width(s.width).Render("Select Workflows")
		subtitle := s.styles.Subtitle.Width(s.width).Render("Choose one or more workflows to run.")
		customHelp := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  space toggle  enter confirm  esc back  ctrl+c quit")
		listView := ""
		if s.customSelect != nil {
			listView = s.customSelect.View()
		}
		return strings.Join([]string{title, subtitle, border, listView, border, customHelp}, "\n")
	}
	return ""
}

// Done reports whether the suite selection is complete.
func (s *TestSuiteScreen) Done() bool { return s.step == testSuiteStepDone }

// Back reports whether the user pressed Esc on the scope step.
func (s *TestSuiteScreen) Back() bool { return s.back }

// Scope returns the selected test scope. Only valid when Done() is true.
func (s *TestSuiteScreen) Scope() testrun.TestScope { return s.selectedScope }

// SelectedWorkflows returns the selected workflow IDs. Populated for
// ScopeSingle (one entry) and ScopeCustom (zero or more entries).
// Only valid when Done() is true.
func (s *TestSuiteScreen) SelectedWorkflows() []string {
	switch s.selectedScope {
	case testrun.ScopeSingle:
		if s.selectedWorkflow != "" {
			return []string{s.selectedWorkflow}
		}
		return nil
	case testrun.ScopeCustom:
		return s.selectedCustom
	}
	return nil
}

// SelectedMode returns the mode selected for Single Workflow scope.
// Empty when the scope is not ScopeSingle or when the workflow declares only
// one mode (mode is auto-inferred from the catalog).
// Only valid when Done() is true.
func (s *TestSuiteScreen) SelectedMode() string { return s.selectedMode }

// Reset clears all state and returns the screen to the initial scope step.
func (s *TestSuiteScreen) Reset() {
	s.step = testSuiteStepScope
	s.back = false
	s.cursor = 0
	s.workflowList = nil
	s.modeList = nil
	s.customSelect = nil
	s.selectedScope = testrun.ScopeSmoke
	s.selectedWorkflow = ""
	s.selectedMode = ""
	s.selectedCustom = nil
	s.availableModes = nil
}

// Resize updates the screen dimensions and reflows any active sub-widget.
func (s *TestSuiteScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	contentH := height - 6
	if contentH < 1 {
		contentH = 1
	}
	if s.workflowList != nil {
		s.workflowList.Resize(contentH, width)
	}
	if s.modeList != nil {
		s.modeList.Resize(contentH, width)
	}
	if s.customSelect != nil {
		s.customSelect.Resize(contentH, width)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// initWorkflowList builds the workflow list widget from the catalog.
func (s *TestSuiteScreen) initWorkflowList() {
	ids := s.catalog.WorkflowIDs()
	items := make([]widgets.ListItem, len(ids))
	for i, id := range ids {
		items[i] = widgets.ListItem{ID: id, Label: id}
	}
	listStyles := widgets.ListStyles{
		Normal:   s.styles.Body,
		Selected: s.styles.Selected,
		Disabled: s.styles.Muted,
		Cursor:   "▶",
	}
	contentH := s.height - 6
	if contentH < 1 {
		contentH = 1
	}
	s.workflowList = widgets.NewList(items, contentH, s.width, listStyles)
}

// initModeList builds the mode list widget from the given mode slice.
func (s *TestSuiteScreen) initModeList(modes []string) {
	items := make([]widgets.ListItem, len(modes))
	for i, m := range modes {
		items[i] = widgets.ListItem{ID: m, Label: m}
	}
	listStyles := widgets.ListStyles{
		Normal:   s.styles.Body,
		Selected: s.styles.Selected,
		Disabled: s.styles.Muted,
		Cursor:   "▶",
	}
	contentH := s.height - 6
	if contentH < 1 {
		contentH = 1
	}
	s.modeList = widgets.NewList(items, contentH, s.width, listStyles)
}

// initCustomSelect builds the multi-select widget from the catalog.
func (s *TestSuiteScreen) initCustomSelect() {
	ids := s.catalog.WorkflowIDs()
	items := make([]widgets.ListItem, len(ids))
	for i, id := range ids {
		items[i] = widgets.ListItem{ID: id, Label: id}
	}
	msStyles := widgets.MultiSelectStyles{
		Normal:   s.styles.Body,
		Selected: s.styles.Selected,
		Checked:  s.styles.Checked,
		Disabled: s.styles.Muted,
		Cursor:   "▶",
		CheckOn:  "[x]",
		CheckOff: "[ ]",
	}
	contentH := s.height - 6
	if contentH < 1 {
		contentH = 1
	}
	s.customSelect = widgets.NewMultiSelect(items, contentH, s.width, msStyles)
}

// indexOfScope returns the cursor index of the given scope in testSuiteScopes.
func indexOfScope(scope testrun.TestScope) int {
	for i, opt := range testSuiteScopes {
		if opt.scope == scope {
			return i
		}
	}
	return 0
}
