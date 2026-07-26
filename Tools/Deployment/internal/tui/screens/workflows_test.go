package screens_test

// workflows_test.go verifies the WorkflowBrowserScreen:
//   - Two-level navigation: categories → workflows within a folder and back.
//   - Selection persistence: selections in a folder survive navigating to another folder
//     and back again (the central risk identified in the plan).
//   - Cross-folder multi-select: selections from different folders accumulate.
//   - Selection summary: SelectedIDs() is accurate at all navigation states.
//   - Detail pane: shows the focused workflow's name, ID, description, and hint.
//   - Empty state: rendered correctly when the catalog has no workflows.
//   - Keyboard-only operability: every interaction is driven through key messages.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

var wfBuild = domain.WorkflowCategory{
	Name: "Build",
	Workflows: []domain.Workflow{
		{ID: "greenfield-tdd", Name: "Greenfield TDD Workflow", Description: "Building a new project from scratch.", Hint: "Full greenfield"},
		{ID: "quick-fix", Name: "Quick Fix Workflow", Description: "Small bug fixes.", Hint: "Small fixes"},
	},
}

var wfAudit = domain.WorkflowCategory{
	Name: "Audit",
	Workflows: []domain.Workflow{
		{ID: "brownfield-pr-audit", Name: "Brownfield PR Audit Workflow", Description: "Audit code for PR review.", Hint: "PR audit"},
	},
}

var wfAllCategories = []domain.WorkflowCategory{wfBuild, wfAudit}

// newWFBrowser is a convenience constructor using plain styles and an 80×24 terminal.
func newWFBrowser(cats []domain.WorkflowCategory) *screens.WorkflowBrowserScreen {
	return screens.NewWorkflowBrowserScreen(cats, 80, 24, plainStyles(), "")
}

// sendWFKey drives the browser with a single key.
func sendWFKey(s *screens.WorkflowBrowserScreen, keyType tea.KeyType) {
	s.Update(tea.KeyMsg{Type: keyType})
}

// sendWFRune drives the browser with a rune key.
func sendWFRune(s *screens.WorkflowBrowserScreen, r rune) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// sendWFTab drives the browser with the Tab key.
func sendWFTab(s *screens.WorkflowBrowserScreen) {
	s.Update(tea.KeyMsg{Type: tea.KeyTab})
}

// enterFolder opens the folder at the current cursor position using Enter.
func enterFolder(s *screens.WorkflowBrowserScreen) { sendWFKey(s, tea.KeyEnter) }

// leaveFolder returns to the category list using Esc.
func leaveFolder(s *screens.WorkflowBrowserScreen) { sendWFKey(s, tea.KeyEsc) }

// toggleWorkflow toggles the focused workflow's selection with Space.
func toggleWorkflow(s *screens.WorkflowBrowserScreen) { sendWFRune(s, ' ') }

// ---------------------------------------------------------------------------
// Two-level navigation
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_Enter_OpensCategoryFolder verifies that pressing Enter on a category
// transitions the browser from category list to workflow list.
func TestWorkflowBrowser_Enter_OpensCategoryFolder(t *testing.T) {
	s := newWFBrowser(wfAllCategories)
	view := s.View()
	if !strings.Contains(view, "Build") {
		t.Fatalf("initial view must show the category 'Build'; got:\n%s", view)
	}

	// Enter the first category (Build).
	enterFolder(s)

	view = s.View()
	// The workflow list should now show workflows from the Build category.
	if !strings.Contains(collapseWhitespace(view), "Greenfield TDD Workflow [greenfield-tdd]") {
		t.Errorf("after entering Build folder, view must list workflows with name and id; got:\n%s", view)
	}
}

// TestWorkflowBrowser_Esc_FromWorkflowLevel_ReturnsToCategories verifies that pressing Esc
// while in a workflow list returns the browser to the category list.
func TestWorkflowBrowser_Esc_FromWorkflowLevel_ReturnsToCategories(t *testing.T) {
	s := newWFBrowser(wfAllCategories)
	enterFolder(s)

	leaveFolder(s)

	view := s.View()
	// Must show the category list again.
	if !strings.Contains(view, "Build") {
		t.Errorf("after Esc from workflow level, category 'Build' must be visible; got:\n%s", view)
	}
	if !strings.Contains(view, "Audit") {
		t.Errorf("after Esc from workflow level, category 'Audit' must be visible; got:\n%s", view)
	}
}

// TestWorkflowBrowser_Esc_FromCategoryLevel_SetsBack verifies that pressing Esc at the top
// category level signals Back().
func TestWorkflowBrowser_Esc_FromCategoryLevel_SetsBack(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	sendWFKey(s, tea.KeyEsc)

	if !s.Back() {
		t.Error("Back() = false after Esc at category level; want true")
	}
}

// ---------------------------------------------------------------------------
// Selection persistence across folders
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_SelectionSurvivesNavigation verifies the primary risk: selections made
// in one folder survive navigating to a different folder and back.
func TestWorkflowBrowser_SelectionSurvivesNavigation(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Step 1: Enter Build folder.
	enterFolder(s)

	// Step 2: Toggle "greenfield-tdd" (cursor starts on first item).
	toggleWorkflow(s)

	// Step 3: Leave Build folder.
	leaveFolder(s)

	// Step 4: Navigate to the second category (Audit) and enter it.
	sendWFKey(s, tea.KeyDown)
	enterFolder(s)

	// Step 5: Leave Audit without selecting anything.
	leaveFolder(s)

	// Step 6: Re-enter Build folder and verify the prior selection is restored.
	sendWFKey(s, tea.KeyUp) // cursor back on Build
	enterFolder(s)

	// The "greenfield-tdd" selection must still be present after the round trip.
	ids := s.SelectedIDs()
	if !containsStr(ids, "greenfield-tdd") {
		t.Errorf("SelectedIDs() = %v after round-trip navigation; want 'greenfield-tdd' to survive (cross-folder selection must persist)", ids)
	}
}

// TestWorkflowBrowser_SelectionPersists_WhileInsideOtherFolder verifies that SelectedIDs()
// returns all selections from all folders while the user is browsing inside one of them.
func TestWorkflowBrowser_SelectionPersists_WhileInsideOtherFolder(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Toggle greenfield-tdd inside Build.
	enterFolder(s)
	toggleWorkflow(s)
	leaveFolder(s)

	// Enter Audit folder and select brownfield-pr-audit.
	sendWFKey(s, tea.KeyDown)
	enterFolder(s)
	toggleWorkflow(s)

	// While still inside Audit, both selections must be visible via SelectedIDs().
	ids := s.SelectedIDs()
	if !containsStr(ids, "greenfield-tdd") {
		t.Errorf("SelectedIDs() = %v while inside Audit; want 'greenfield-tdd' from Build to still appear", ids)
	}
	if !containsStr(ids, "brownfield-pr-audit") {
		t.Errorf("SelectedIDs() = %v while inside Audit; want 'brownfield-pr-audit' to appear", ids)
	}
}

// TestWorkflowBrowser_Deselect_RemovesFromSelectedIDs verifies that toggling an already-selected
// workflow off removes it from SelectedIDs(). This guards against the bug where selected items
// can only be added, never removed.
func TestWorkflowBrowser_Deselect_RemovesFromSelectedIDs(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Select greenfield-tdd.
	enterFolder(s)
	toggleWorkflow(s)

	if !containsStr(s.SelectedIDs(), "greenfield-tdd") {
		t.Fatal("precondition: greenfield-tdd must be selected after first toggle")
	}

	// Deselect greenfield-tdd.
	toggleWorkflow(s)
	leaveFolder(s)

	ids := s.SelectedIDs()
	if containsStr(ids, "greenfield-tdd") {
		t.Errorf("SelectedIDs() = %v after deselecting greenfield-tdd; want it removed", ids)
	}
}

// ---------------------------------------------------------------------------
// Cross-folder multi-select
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_CrossFolderMultiSelect verifies that workflows from different category
// folders can be selected in a single session and all appear in SelectedIDs().
func TestWorkflowBrowser_CrossFolderMultiSelect(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Select greenfield-tdd from Build.
	enterFolder(s)
	toggleWorkflow(s)
	leaveFolder(s)

	// Select brownfield-pr-audit from Audit.
	sendWFKey(s, tea.KeyDown)
	enterFolder(s)
	toggleWorkflow(s)
	leaveFolder(s)

	ids := s.SelectedIDs()
	if !containsStr(ids, "greenfield-tdd") {
		t.Errorf("SelectedIDs() = %v; want 'greenfield-tdd' selected from Build folder", ids)
	}
	if !containsStr(ids, "brownfield-pr-audit") {
		t.Errorf("SelectedIDs() = %v; want 'brownfield-pr-audit' selected from Audit folder", ids)
	}
}

// TestWorkflowBrowser_Tab_ConfirmsEntireSelection verifies that pressing Tab at the category
// level signals Done() so the root model can proceed with the collected workflow IDs.
func TestWorkflowBrowser_Tab_ConfirmsEntireSelection(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Select a workflow, return to category level, then confirm.
	enterFolder(s)
	toggleWorkflow(s)
	leaveFolder(s)
	sendWFTab(s)

	if !s.Done() {
		t.Error("Done() = false after Tab at category level; want true")
	}
}

// ---------------------------------------------------------------------------
// Detail pane tracking
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_DetailPane_ShowsNameIDDescriptionHint verifies that the detail pane
// content contains all four fields (name, id, description, hint) for the focused workflow.
func TestWorkflowBrowser_DetailPane_ShowsNameIDDescriptionHint(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Enter Build folder and check detail pane for the first workflow.
	enterFolder(s)

	view := collapseWhitespace(s.View())

	// The workflow's name and ID should both appear.
	if !strings.Contains(view, "Greenfield TDD Workflow") {
		t.Errorf("detail pane does not show workflow name 'Greenfield TDD Workflow'; view:\n%s", s.View())
	}
	if !strings.Contains(view, "greenfield-tdd") {
		t.Errorf("detail pane does not show workflow id 'greenfield-tdd'; view:\n%s", s.View())
	}
	if !strings.Contains(view, "Building a new project from scratch") {
		t.Errorf("detail pane does not show workflow description; view:\n%s", s.View())
	}
	if !strings.Contains(view, "Full greenfield") {
		t.Errorf("detail pane does not show workflow hint; view:\n%s", s.View())
	}
}

// TestWorkflowBrowser_DetailPane_TracksNavigation verifies that the detail pane updates as
// the cursor moves between workflows within a folder.
func TestWorkflowBrowser_DetailPane_TracksNavigation(t *testing.T) {
	s := newWFBrowser(wfAllCategories)
	enterFolder(s) // Build folder: greenfield-tdd is focused first.

	view1 := s.View()
	if !strings.Contains(collapseWhitespace(view1), "Greenfield TDD Workflow") {
		t.Errorf("first focused workflow detail must show Greenfield TDD Workflow; got:\n%s", view1)
	}

	// Move cursor to the next workflow.
	sendWFKey(s, tea.KeyDown)

	view2 := s.View()
	if !strings.Contains(collapseWhitespace(view2), "Quick Fix Workflow") {
		t.Errorf("detail pane must update to Quick Fix Workflow after moving cursor down; got:\n%s", view2)
	}
}

// TestWorkflowBrowser_List_ShowsNameAndID verifies that each workflow list row displays both
// the human-readable name and the machine id (the picker must show both per AC21.2).
func TestWorkflowBrowser_List_ShowsNameAndID(t *testing.T) {
	s := newWFBrowser(wfAllCategories)
	enterFolder(s)

	view := s.View()
	// The row should contain both the name and the id in the list column.
	if !strings.Contains(collapseWhitespace(view), "Quick Fix Workflow [quick-fix]") {
		t.Errorf("workflow list row must show both name and id; got:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Selection summary
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_SelectionSummary_ShowsCount verifies that the selection summary
// line reflects the number of selected workflows.
func TestWorkflowBrowser_SelectionSummary_ShowsCount(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Initially no workflows selected.
	if !strings.Contains(s.View(), "No workflows selected") {
		t.Error("initial summary must report no workflows selected")
	}

	// Select one workflow.
	enterFolder(s)
	toggleWorkflow(s)
	leaveFolder(s)

	view := s.View()
	if !strings.Contains(collapseWhitespace(view), "Selected 1 workflow") {
		t.Errorf("summary must show '1 workflow' after one selection; got:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_EmptyState_RendersMessage verifies that a browser constructed with an
// empty category slice shows an informational empty-state message rather than blank output.
func TestWorkflowBrowser_EmptyState_RendersMessage(t *testing.T) {
	s := screens.NewWorkflowBrowserScreen(nil, 80, 24, plainStyles(), "")

	view := s.View()
	if strings.TrimSpace(view) == "" {
		t.Error("empty-state view must not be blank")
	}
	if !strings.Contains(strings.ToLower(view), "no workflow") {
		t.Errorf("empty-state view must contain a 'no workflow' message; got:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Error state
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_ErrorState_RendersMessage verifies that constructing the screen with a
// non-empty errorMsg renders the error message rather than the list.
func TestWorkflowBrowser_ErrorState_RendersMessage(t *testing.T) {
	s := screens.NewWorkflowBrowserScreen(wfAllCategories, 80, 24, plainStyles(), "catalog read failure: disk full")

	view := s.View()
	if !strings.Contains(view, "catalog read failure: disk full") {
		t.Errorf("error-state view must contain the error message; got:\n%s", view)
	}
}

// TestWorkflowBrowser_ErrorState_EscSetsBack verifies that Esc still works in the error state.
func TestWorkflowBrowser_ErrorState_EscSetsBack(t *testing.T) {
	s := screens.NewWorkflowBrowserScreen(wfAllCategories, 80, 24, plainStyles(), "some error")
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc in error state; want true")
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_Reset_ClearsDoneAndBack verifies that Reset restores the screen to an
// idle state.
func TestWorkflowBrowser_Reset_ClearsDoneAndBack(t *testing.T) {
	s := newWFBrowser(wfAllCategories)
	sendWFTab(s)
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after Tab")
	}

	s.Reset()

	if s.Done() {
		t.Error("Done() = true after Reset; want false")
	}
	if s.Back() {
		t.Error("Back() = true after Reset; want false")
	}
}

// ---------------------------------------------------------------------------
// SelectedIDs ordering
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_SelectedIDs_CategoryDeclarationOrder verifies that SelectedIDs()
// returns IDs in the order they appear in the category/workflow declarations, not in the
// order they were toggled by the user.
func TestWorkflowBrowser_SelectedIDs_CategoryDeclarationOrder(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Select brownfield-pr-audit (Audit) first, then greenfield-tdd (Build).
	// Audit is the second category, Build is the first.
	sendWFKey(s, tea.KeyDown) // move cursor to Audit
	enterFolder(s)
	toggleWorkflow(s) // select brownfield-pr-audit
	leaveFolder(s)

	sendWFKey(s, tea.KeyUp) // back to Build
	enterFolder(s)
	toggleWorkflow(s) // select greenfield-tdd
	leaveFolder(s)

	ids := s.SelectedIDs()
	if len(ids) != 2 {
		t.Fatalf("SelectedIDs() = %v; want exactly 2 IDs", ids)
	}
	// greenfield-tdd is in Build (first category) and must appear before brownfield-pr-audit.
	if ids[0] != "greenfield-tdd" {
		t.Errorf("SelectedIDs()[0] = %q; want 'greenfield-tdd' (Build comes before Audit)", ids[0])
	}
	if ids[1] != "brownfield-pr-audit" {
		t.Errorf("SelectedIDs()[1] = %q; want 'brownfield-pr-audit'", ids[1])
	}
}

// ---------------------------------------------------------------------------
// Tab key ignored inside a workflow folder
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_Tab_IgnoredAtWorkflowLevel verifies that pressing Tab while inside a
// workflow folder does not trigger Done(). Tab is only meaningful at the category level.
func TestWorkflowBrowser_Tab_IgnoredAtWorkflowLevel(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Enter a folder then send Tab.
	enterFolder(s)
	sendWFTab(s)

	if s.Done() {
		t.Error("Done() = true after Tab inside a workflow folder; Tab must only confirm at the category level")
	}
}

// ---------------------------------------------------------------------------
// Summary content
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_SummaryView_ContainsSelectedID verifies that the selection summary line
// in View() includes the actual workflow ID, not just the count, so the user can see at a
// glance which workflows are selected without leaving the screen.
func TestWorkflowBrowser_SummaryView_ContainsSelectedID(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Select one workflow.
	enterFolder(s)
	toggleWorkflow(s) // select greenfield-tdd
	leaveFolder(s)

	view := collapseWhitespace(s.View())
	if !strings.Contains(view, "greenfield-tdd") {
		t.Errorf("summary line must contain the selected workflow ID 'greenfield-tdd'; got:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Keyboard-only operability
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_KeyboardOnly_FullFlow verifies that a complete workflow selection
// session is achievable using only keyboard input (no mouse events required).
func TestWorkflowBrowser_KeyboardOnly_FullFlow(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Open Build folder with Enter.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Toggle the first workflow (greenfield-tdd) with Space.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	// Return to categories with Esc.
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Confirm with Tab.
	s.Update(tea.KeyMsg{Type: tea.KeyTab})

	if !s.Done() {
		t.Error("Done() = false after complete keyboard-only flow; want true")
	}
	ids := s.SelectedIDs()
	if !containsStr(ids, "greenfield-tdd") {
		t.Errorf("SelectedIDs() = %v after keyboard-only flow; want 'greenfield-tdd'", ids)
	}
}

// TestWorkflowBrowser_KeyboardOnly_VimKeys verifies that vim-style navigation (j/k) works.
func TestWorkflowBrowser_KeyboardOnly_VimKeys(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Navigate to the second category using 'j'.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})

	// Enter the second category (Audit).
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// The view should show Audit workflows. The name "Brownfield PR Audit" is unique to the
	// Audit folder and short enough to fit on one rendered line without wrapping.
	view := s.View()
	if !strings.Contains(view, "Brownfield PR Audit") {
		t.Errorf("vim 'j' key must move cursor to Audit category; want Audit workflows in view:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
