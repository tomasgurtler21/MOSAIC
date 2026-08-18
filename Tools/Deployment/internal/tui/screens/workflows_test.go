package screens_test

// workflows_test.go verifies the WorkflowBrowserScreen:
//   - Two-level navigation: categories → workflows within a folder and back.
//   - Selection persistence: selections in a folder survive navigating to another folder
//     and back again (the central risk identified in the plan).
//   - Cross-folder multi-select: selections from different folders accumulate.
//   - Selection summary: SelectedIDs() is accurate at all navigation states.
//   - Detail pane: shows the focused workflow's name, ID, description, and hint.
//   - Empty state: rendered correctly when the catalog has no workflows.
//   - Stage 6 keybindings:
//       - Category level: Right/l opens folder; Enter confirms (with selection); Esc cancels; Tab is inert.
//       - Workflow level: Space toggles; Left/h and Esc return to categories; Enter confirms (with selection); Tab is inert.
//   - Empty-selection validation: visible transient message, Done() stays false, Esc always cancels.
//   - Help bars: list only the active bindings at each level; Tab and "enter open folder" are absent.

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

// newWFBrowser is a convenience constructor using plain styles and an 80x24 terminal.
func newWFBrowser(cats []domain.WorkflowCategory) *screens.WorkflowBrowserScreen {
	return screens.NewWorkflowBrowserScreen(cats, 80, 24, plainStyles(), "")
}

// sendWFKey drives the browser with a single special key.
func sendWFKey(s *screens.WorkflowBrowserScreen, keyType tea.KeyType) {
	s.Update(tea.KeyMsg{Type: keyType})
}

// sendWFRune drives the browser with a rune key (printable character).
func sendWFRune(s *screens.WorkflowBrowserScreen, r rune) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// sendWFTab drives the browser with the Tab key. Used in tests that assert Tab is inert.
func sendWFTab(s *screens.WorkflowBrowserScreen) {
	s.Update(tea.KeyMsg{Type: tea.KeyTab})
}

// enterFolder opens the focused category folder using the Right arrow key (Stage 6
// scheme: Right/l opens folder; Enter confirms).
func enterFolder(s *screens.WorkflowBrowserScreen) { sendWFKey(s, tea.KeyRight) }

// leaveFolder returns to the category list using Esc. Valid at the workflow level in
// both the old and the new keybinding scheme.
func leaveFolder(s *screens.WorkflowBrowserScreen) { sendWFKey(s, tea.KeyEsc) }

// toggleWorkflow toggles the focused workflow's selection with Space.
func toggleWorkflow(s *screens.WorkflowBrowserScreen) { sendWFRune(s, ' ') }

// ---------------------------------------------------------------------------
// Stage 6 — Category-level binding tests
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_CategoryLevel_Right_OpensFocusedFolder verifies that pressing the Right
// arrow key at the category level transitions the browser into the focused category's workflow
// list.
func TestWorkflowBrowser_CategoryLevel_Right_OpensFocusedFolder(t *testing.T) {
	s := newWFBrowser(wfAllCategories)
	view := s.View()
	if !strings.Contains(view, "Build") {
		t.Fatalf("initial view must show category 'Build'; got:\n%s", view)
	}

	sendWFKey(s, tea.KeyRight) // Right opens folder under new scheme

	view = s.View()
	if !strings.Contains(collapseWhitespace(view), "Greenfield TDD Workflow [greenfield-tdd]") {
		t.Errorf("after Right at category level, must show Build folder workflows; got:\n%s", view)
	}
}

// TestWorkflowBrowser_CategoryLevel_L_OpensFocusedFolder verifies that pressing 'l'
// (vim-style right) at the category level opens the focused folder.
func TestWorkflowBrowser_CategoryLevel_L_OpensFocusedFolder(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	sendWFRune(s, 'l') // 'l' is the vim-style open-folder key

	view := s.View()
	if !strings.Contains(collapseWhitespace(view), "Greenfield TDD Workflow [greenfield-tdd]") {
		t.Errorf("after 'l' at category level, must show Build folder workflows; got:\n%s", view)
	}
}

// TestWorkflowBrowser_CategoryLevel_Enter_WithSelection_ConfirmsAndSetsDone verifies that
// pressing Enter at the category level when at least one workflow is selected confirms the
// whole selection and sets Done() to true.
func TestWorkflowBrowser_CategoryLevel_Enter_WithSelection_ConfirmsAndSetsDone(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Open the first folder, select a workflow, return to the category list.
	enterFolder(s)
	toggleWorkflow(s) // select greenfield-tdd
	leaveFolder(s)    // Esc returns to categories

	// At category level with one workflow selected, Enter must confirm.
	sendWFKey(s, tea.KeyEnter)

	if !s.Done() {
		t.Error("Done() = false after Enter at category level with one workflow selected; want true (Enter must confirm when selection is non-empty)")
	}
}

// TestWorkflowBrowser_CategoryLevel_Enter_WithNoSelection_DoesNotSetDone verifies that
// pressing Enter at the category level with no workflows selected does not confirm the run
// and shows the validation message to inform the user.
func TestWorkflowBrowser_CategoryLevel_Enter_WithNoSelection_DoesNotSetDone(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	sendWFKey(s, tea.KeyEnter) // Enter with zero selections

	if s.Done() {
		t.Error("Done() = true after Enter at category level with no selection; want false (empty selection must be blocked)")
	}
	if !strings.Contains(s.View(), screens.MsgNoWorkflowsSelected) {
		t.Errorf("view after Enter with no selection at category level must contain the validation message;\nwant substring: %q\ngot view:\n%s",
			screens.MsgNoWorkflowsSelected, s.View())
	}
}

// TestWorkflowBrowser_CategoryLevel_Esc_SetsBack verifies that pressing Esc at the
// category level signals Back(), providing a genuine cancel path.
func TestWorkflowBrowser_CategoryLevel_Esc_SetsBack(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	sendWFKey(s, tea.KeyEsc)

	if !s.Back() {
		t.Error("Back() = false after Esc at category level; want true")
	}
	if s.Done() {
		t.Error("Done() = true after Esc at category level; want false")
	}
}

// TestWorkflowBrowser_CategoryLevel_Tab_IsInert verifies that pressing Tab at the category
// level has no effect: Done() stays false, Back() stays false, and the view still shows the
// category list.
func TestWorkflowBrowser_CategoryLevel_Tab_IsInert(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	sendWFTab(s)

	if s.Done() {
		t.Error("Done() = true after Tab at category level; Tab must not confirm (it is not a binding on this screen)")
	}
	if s.Back() {
		t.Error("Back() = true after Tab at category level; Tab must not cancel")
	}
	view := s.View()
	if !strings.Contains(view, "Build") {
		t.Errorf("after Tab at category level, view must still show category list; got:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Stage 6 — Workflow-level binding tests
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_WorkflowLevel_Space_TogglesSelection verifies that Space toggles the
// focused workflow's selection while inside a folder.
func TestWorkflowBrowser_WorkflowLevel_Space_TogglesSelection(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	enterFolder(s) // open Build folder

	if containsStr(s.SelectedIDs(), "greenfield-tdd") {
		t.Fatal("precondition: greenfield-tdd must not be selected before first toggle")
	}

	toggleWorkflow(s) // Space: select greenfield-tdd

	if !containsStr(s.SelectedIDs(), "greenfield-tdd") {
		t.Errorf("SelectedIDs() = %v after Space; want 'greenfield-tdd' selected", s.SelectedIDs())
	}
}

// TestWorkflowBrowser_WorkflowLevel_Left_ReturnsToCategoriesPreservingSelection verifies that
// pressing Left at the workflow level returns to the category list with selections preserved.
func TestWorkflowBrowser_WorkflowLevel_Left_ReturnsToCategoriesPreservingSelection(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	enterFolder(s)    // open Build folder
	toggleWorkflow(s) // select greenfield-tdd

	sendWFKey(s, tea.KeyLeft) // Left returns to category list

	// Must be back at the category level.
	view := s.View()
	if !strings.Contains(view, "Build") || !strings.Contains(view, "Audit") {
		t.Errorf("after Left at workflow level, must show category list with both categories; got:\n%s", view)
	}
	// Selection must be preserved across the navigation.
	if !containsStr(s.SelectedIDs(), "greenfield-tdd") {
		t.Errorf("SelectedIDs() = %v after Left; want 'greenfield-tdd' to survive navigation", s.SelectedIDs())
	}
}

// TestWorkflowBrowser_WorkflowLevel_H_ReturnsToCategoriesPreservingSelection verifies that
// pressing 'h' (vim-style left) at the workflow level returns to the category list with
// selections preserved.
func TestWorkflowBrowser_WorkflowLevel_H_ReturnsToCategoriesPreservingSelection(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	enterFolder(s)    // open Build folder
	toggleWorkflow(s) // select greenfield-tdd

	sendWFRune(s, 'h') // 'h' is the vim-style leave-folder key

	view := s.View()
	if !strings.Contains(view, "Build") {
		t.Errorf("after 'h' at workflow level, must show category list; got:\n%s", view)
	}
	if !containsStr(s.SelectedIDs(), "greenfield-tdd") {
		t.Errorf("SelectedIDs() = %v after 'h'; want 'greenfield-tdd' to survive navigation", s.SelectedIDs())
	}
}

// TestWorkflowBrowser_WorkflowLevel_Esc_ReturnsToCategoriesPreservingSelection verifies that
// pressing Esc at the workflow level returns to the category list with selections preserved and
// does NOT trigger Back() (Esc at workflow level is "back one level", not "cancel screen").
func TestWorkflowBrowser_WorkflowLevel_Esc_ReturnsToCategoriesPreservingSelection(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	enterFolder(s)    // open Build folder
	toggleWorkflow(s) // select greenfield-tdd
	leaveFolder(s)    // Esc at workflow level returns to categories

	if s.Back() {
		t.Error("Back() = true after Esc at workflow level; Esc must return to the category list, not cancel the whole screen")
	}
	view := s.View()
	if !strings.Contains(view, "Build") {
		t.Errorf("after Esc at workflow level, must show category list; got:\n%s", view)
	}
	if !containsStr(s.SelectedIDs(), "greenfield-tdd") {
		t.Errorf("SelectedIDs() = %v after Esc at workflow level; want 'greenfield-tdd' to survive navigation", s.SelectedIDs())
	}
}

// TestWorkflowBrowser_WorkflowLevel_Enter_WithSelection_ConfirmsAndSetsDone verifies that
// pressing Enter at the workflow level when at least one workflow is selected confirms the
// whole selection and sets Done() to true without requiring a return to the category level first.
func TestWorkflowBrowser_WorkflowLevel_Enter_WithSelection_ConfirmsAndSetsDone(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	enterFolder(s)    // open Build folder
	toggleWorkflow(s) // select greenfield-tdd

	// Enter at the workflow level with a selection must confirm the run directly.
	sendWFKey(s, tea.KeyEnter)

	if !s.Done() {
		t.Error("Done() = false after Enter at workflow level with one workflow selected; want true (Enter must confirm at both levels)")
	}
	if !containsStr(s.SelectedIDs(), "greenfield-tdd") {
		t.Errorf("SelectedIDs() = %v after Enter confirmation from workflow level; want 'greenfield-tdd'", s.SelectedIDs())
	}
}

// TestWorkflowBrowser_WorkflowLevel_Enter_WithNoSelection_DoesNotSetDone verifies that
// pressing Enter at the workflow level with no workflows selected does not confirm the run
// and shows the validation message to inform the user.
func TestWorkflowBrowser_WorkflowLevel_Enter_WithNoSelection_DoesNotSetDone(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	enterFolder(s) // open Build folder — no workflow selected yet

	sendWFKey(s, tea.KeyEnter)

	if s.Done() {
		t.Error("Done() = true after Enter at workflow level with no selection; want false (empty selection must be blocked at both levels)")
	}
	if !strings.Contains(s.View(), screens.MsgNoWorkflowsSelected) {
		t.Errorf("view after Enter with no selection at workflow level must contain the validation message;\nwant substring: %q\ngot view:\n%s",
			screens.MsgNoWorkflowsSelected, s.View())
	}
}

// TestWorkflowBrowser_WorkflowLevel_Tab_IsInert verifies that pressing Tab while inside a
// workflow folder has no effect — Tab is not a binding at the workflow level either.
func TestWorkflowBrowser_WorkflowLevel_Tab_IsInert(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	enterFolder(s) // open Build folder
	sendWFTab(s)

	if s.Done() {
		t.Error("Done() = true after Tab inside a workflow folder; Tab must not confirm at the workflow level")
	}
}

// ---------------------------------------------------------------------------
// Stage 6 — Empty-selection validation
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_CategoryLevel_Enter_WithNoSelection_ShowsValidationMessage verifies that
// pressing Enter at the category level with no selection renders the transient validation
// message so the user understands why the screen did not advance.
func TestWorkflowBrowser_CategoryLevel_Enter_WithNoSelection_ShowsValidationMessage(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	sendWFKey(s, tea.KeyEnter) // Enter with no selection

	view := s.View()
	if !strings.Contains(view, screens.MsgNoWorkflowsSelected) {
		t.Errorf("view after Enter with no selection at category level must contain the validation message;\nwant substring: %q\ngot view:\n%s",
			screens.MsgNoWorkflowsSelected, view)
	}
}

// TestWorkflowBrowser_WorkflowLevel_Enter_WithNoSelection_ShowsValidationMessage verifies that
// pressing Enter at the workflow level with no selection also renders the transient validation
// message.
func TestWorkflowBrowser_WorkflowLevel_Enter_WithNoSelection_ShowsValidationMessage(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	enterFolder(s) // open Build folder — no workflow selected yet
	sendWFKey(s, tea.KeyEnter)

	view := s.View()
	if !strings.Contains(view, screens.MsgNoWorkflowsSelected) {
		t.Errorf("view after Enter with no selection at workflow level must contain the validation message;\nwant substring: %q\ngot view:\n%s",
			screens.MsgNoWorkflowsSelected, view)
	}
}

// TestWorkflowBrowser_ValidationMessage_ClearsAfterWorkflowSelected verifies that the
// transient validation message disappears once the user selects at least one workflow.
func TestWorkflowBrowser_ValidationMessage_ClearsAfterWorkflowSelected(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Trigger the validation message at the workflow level.
	enterFolder(s)
	sendWFKey(s, tea.KeyEnter) // Enter with no selection → shows message

	if !strings.Contains(s.View(), screens.MsgNoWorkflowsSelected) {
		t.Fatal("precondition: validation message must be visible before workflow is selected; test is set up incorrectly")
	}

	// Selecting a workflow must clear the message.
	toggleWorkflow(s)

	if strings.Contains(s.View(), screens.MsgNoWorkflowsSelected) {
		t.Errorf("validation message must clear once a workflow is selected; still present in view:\n%s", s.View())
	}
}

// TestWorkflowBrowser_ValidationMessage_ClearsOnNavigation verifies that the transient
// validation message is cleared when the user navigates into a folder.
func TestWorkflowBrowser_ValidationMessage_ClearsOnNavigation(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Trigger the validation message at the category level.
	sendWFKey(s, tea.KeyEnter) // Enter with no selection

	if !strings.Contains(s.View(), screens.MsgNoWorkflowsSelected) {
		t.Fatal("precondition: validation message must be visible after Enter with no selection at category level; test is set up incorrectly")
	}

	// Navigating into a folder must clear the message.
	enterFolder(s) // Right

	if strings.Contains(s.View(), screens.MsgNoWorkflowsSelected) {
		t.Errorf("validation message must clear when user navigates into a folder; still present in view:\n%s", s.View())
	}
}

// TestWorkflowBrowser_ValidationMessage_ClearsOnReturnToCategories verifies that the
// transient validation message is cleared when the user presses Esc (or Left/h) at the
// workflow level to return to the category list. The design specifies that all navigation-
// back keys (Left, h, Esc at workflow level) clear the message.
func TestWorkflowBrowser_ValidationMessage_ClearsOnReturnToCategories(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Enter a folder and trigger the validation message at the workflow level.
	enterFolder(s)
	sendWFKey(s, tea.KeyEnter) // Enter with no selection → shows message

	if !strings.Contains(s.View(), screens.MsgNoWorkflowsSelected) {
		t.Fatal("precondition: validation message must be visible at the workflow level before navigating back; test is set up incorrectly")
	}

	// Return to the category list with Esc at the workflow level.
	leaveFolder(s) // Esc

	if strings.Contains(s.View(), screens.MsgNoWorkflowsSelected) {
		t.Errorf("validation message must be cleared when returning from workflow level to category level via Esc; still present in view:\n%s", s.View())
	}
}

// TestWorkflowBrowser_ValidationMessage_EscCancelsAtCategoryLevel verifies that Esc at the
// category level sets Back() even when the validation message is visible. The user must never
// be trapped on the screen.
func TestWorkflowBrowser_ValidationMessage_EscCancelsAtCategoryLevel(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Trigger the validation message.
	sendWFKey(s, tea.KeyEnter) // Enter with no selection

	// Esc must still cancel regardless of validation state.
	sendWFKey(s, tea.KeyEsc)

	if !s.Back() {
		t.Error("Back() = false after Esc with validation message visible; Esc must always provide a cancel path at the category level")
	}
	if s.Done() {
		t.Error("Done() = true after Esc at category level with validation message; want false")
	}
}

// TestWorkflowBrowser_ValidationMessage_EscLeavesWorkflowLevel verifies that Esc at the
// workflow level returns to the category list — not to Back() — even when the validation
// message is visible. The user is not trapped.
func TestWorkflowBrowser_ValidationMessage_EscLeavesWorkflowLevel(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Open a folder and trigger the validation message at the workflow level.
	enterFolder(s)
	sendWFKey(s, tea.KeyEnter) // Enter with no selection

	if !strings.Contains(s.View(), screens.MsgNoWorkflowsSelected) {
		t.Fatal("precondition: validation message must be visible at the workflow level before pressing Esc; test is set up incorrectly")
	}

	// Esc at workflow level must return to the category list, not trigger Back().
	leaveFolder(s) // Esc

	if s.Back() {
		t.Error("Back() = true after Esc at workflow level with validation message; Esc at workflow level must return to category list, not cancel the screen")
	}
	if s.Done() {
		t.Error("Done() = true after Esc at workflow level; want false")
	}
	view := s.View()
	if !strings.Contains(view, "Build") {
		t.Errorf("after Esc at workflow level, view must show the category list; got:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Stage 6 — Help bar accuracy
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_CategoryLevel_HelpBar_ListsRightAndConfirm verifies that the category-
// level help bar advertises the Right/l open-folder binding and the Enter confirm binding.
func TestWorkflowBrowser_CategoryLevel_HelpBar_ListsRightAndConfirm(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	view := collapseWhitespace(s.View())

	// The help bar must mention the Right/l open-folder binding.
	if !strings.Contains(view, "→/l") {
		t.Errorf("category-level help bar must mention '→/l open folder'; got:\n%s", s.View())
	}
	// It must mention open folder in conjunction with that key.
	if !strings.Contains(strings.ToLower(view), "open folder") {
		t.Errorf("category-level help bar must mention 'open folder'; got:\n%s", s.View())
	}
	// It must mention enter confirm.
	if !strings.Contains(strings.ToLower(view), "enter confirm") {
		t.Errorf("category-level help bar must mention 'enter confirm'; got:\n%s", s.View())
	}
}

// TestWorkflowBrowser_CategoryLevel_HelpBar_DoesNotMentionTab verifies that the category-
// level help bar does not mention Tab, which is no longer a binding on this screen.
func TestWorkflowBrowser_CategoryLevel_HelpBar_DoesNotMentionTab(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	view := strings.ToLower(s.View())

	if strings.Contains(view, "tab") {
		t.Errorf("category-level help bar must not mention 'tab' (Tab is not a binding in Stage 6);\ngot view:\n%s", s.View())
	}
}

// TestWorkflowBrowser_CategoryLevel_HelpBar_DoesNotSayEnterOpenFolder verifies that the
// category-level help bar does not describe Enter as "open folder" (Enter now confirms).
func TestWorkflowBrowser_CategoryLevel_HelpBar_DoesNotSayEnterOpenFolder(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	view := strings.ToLower(collapseWhitespace(s.View()))

	if strings.Contains(view, "enter open folder") {
		t.Errorf("category-level help bar must not say 'enter open folder'; Enter now confirms, not opens:\n%s", s.View())
	}
}

// TestWorkflowBrowser_WorkflowLevel_HelpBar_ListsLeftAndConfirm verifies that the workflow-
// level help bar advertises the Left/h back-to-folders binding and the Enter confirm binding.
func TestWorkflowBrowser_WorkflowLevel_HelpBar_ListsLeftAndConfirm(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	enterFolder(s) // open Build folder → workflow level

	view := collapseWhitespace(s.View())
	lower := strings.ToLower(view)

	// Must mention the Left/h back-to-folders binding.
	if !strings.Contains(view, "←/h") {
		t.Errorf("workflow-level help bar must mention '←/h back to folders'; got:\n%s", s.View())
	}
	if !strings.Contains(lower, "back to folders") {
		t.Errorf("workflow-level help bar must mention 'back to folders'; got:\n%s", s.View())
	}
	// Must mention enter confirm.
	if !strings.Contains(lower, "enter confirm") {
		t.Errorf("workflow-level help bar must mention 'enter confirm'; got:\n%s", s.View())
	}
	// Must mention space toggle.
	if !strings.Contains(lower, "space toggle") {
		t.Errorf("workflow-level help bar must mention 'space toggle'; got:\n%s", s.View())
	}
}

// TestWorkflowBrowser_WorkflowLevel_HelpBar_DoesNotMentionTab verifies that the workflow-
// level help bar does not advertise Tab.
func TestWorkflowBrowser_WorkflowLevel_HelpBar_DoesNotMentionTab(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	enterFolder(s) // open Build folder

	view := strings.ToLower(s.View())

	if strings.Contains(view, "tab") {
		t.Errorf("workflow-level help bar must not mention 'tab';\ngot view:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Two-level navigation (updated for Stage 6: Right opens folder)
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_Right_OpensCategoryFolder verifies that pressing Right on a category
// transitions the browser from the category list to the workflow list for that category.
func TestWorkflowBrowser_Right_OpensCategoryFolder(t *testing.T) {
	s := newWFBrowser(wfAllCategories)
	view := s.View()
	if !strings.Contains(view, "Build") {
		t.Fatalf("initial view must show the category 'Build'; got:\n%s", view)
	}

	// Open the first category (Build) with Right.
	enterFolder(s)

	view = s.View()
	if !strings.Contains(collapseWhitespace(view), "Greenfield TDD Workflow [greenfield-tdd]") {
		t.Errorf("after entering Build folder (Right), view must list workflows with name and id; got:\n%s", view)
	}
}

// TestWorkflowBrowser_Esc_FromWorkflowLevel_ReturnsToCategories verifies that pressing Esc
// while in a workflow list returns the browser to the category list without triggering Back().
func TestWorkflowBrowser_Esc_FromWorkflowLevel_ReturnsToCategories(t *testing.T) {
	s := newWFBrowser(wfAllCategories)
	enterFolder(s)

	leaveFolder(s)

	if s.Back() {
		t.Error("Back() = true after Esc from workflow level; Esc at workflow level must return to the category list, not cancel the whole screen")
	}
	view := s.View()
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

// TestWorkflowBrowser_Deselect_RemovesFromSelectedIDs verifies that toggling an already-
// selected workflow off removes it from SelectedIDs(). This guards against the bug where
// selected items can only be added, never removed.
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

// TestWorkflowBrowser_Enter_ConfirmsEntireSelection verifies that pressing Enter at the
// category level with at least one workflow selected signals Done() so the root model can
// proceed with the collected workflow IDs.
func TestWorkflowBrowser_Enter_ConfirmsEntireSelection(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Select a workflow, return to the category level, then confirm with Enter.
	enterFolder(s)
	toggleWorkflow(s)
	leaveFolder(s)
	sendWFKey(s, tea.KeyEnter)

	if !s.Done() {
		t.Error("Done() = false after Enter at category level with a selection; want true")
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
// the human-readable name and the machine id.
func TestWorkflowBrowser_List_ShowsNameAndID(t *testing.T) {
	s := newWFBrowser(wfAllCategories)
	enterFolder(s)

	view := s.View()
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

	// Set up a selection and confirm with Enter (Stage 6 scheme).
	enterFolder(s)
	toggleWorkflow(s) // select greenfield-tdd
	leaveFolder(s)
	sendWFKey(s, tea.KeyEnter) // Enter at category level with selection → Done()

	if !s.Done() {
		t.Fatal("precondition: Done() must be true after Enter with selection at category level")
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
// Tab is inert at both levels (updated for Stage 6: Tab is never a confirm key)
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_Tab_IsInertAtBothLevels verifies that pressing Tab neither confirms
// nor cancels at either navigation level. Tab is not a binding on this screen.
func TestWorkflowBrowser_Tab_IsInertAtBothLevels(t *testing.T) {
	t.Run("category level", func(t *testing.T) {
		s := newWFBrowser(wfAllCategories)
		sendWFTab(s)
		if s.Done() {
			t.Error("Done() = true after Tab at category level; Tab must not confirm at any level")
		}
		if s.Back() {
			t.Error("Back() = true after Tab at category level; Tab must not cancel")
		}
	})

	t.Run("workflow level", func(t *testing.T) {
		s := newWFBrowser(wfAllCategories)
		enterFolder(s) // open Build folder
		sendWFTab(s)
		if s.Done() {
			t.Error("Done() = true after Tab inside a workflow folder; Tab must not confirm at any level")
		}
	})
}

// ---------------------------------------------------------------------------
// Summary content
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_SummaryView_ContainsSelectedID verifies that the selection summary line
// in View() includes the actual workflow ID, not just the count.
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
// Keyboard-only operability (updated for Stage 6 keybinding scheme)
// ---------------------------------------------------------------------------

// TestWorkflowBrowser_KeyboardOnly_FullFlow verifies that a complete workflow selection
// session is achievable using only keyboard input under the Stage 6 scheme:
// Right to open folder, Space to toggle, Esc to return to categories, Enter to confirm.
func TestWorkflowBrowser_KeyboardOnly_FullFlow(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Open Build folder with Right.
	s.Update(tea.KeyMsg{Type: tea.KeyRight})

	// Toggle the first workflow (greenfield-tdd) with Space.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	// Return to categories with Esc.
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Confirm with Enter (Stage 6: Enter at category level with selection confirms).
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after complete keyboard-only flow (Right→Space→Esc→Enter); want true")
	}
	ids := s.SelectedIDs()
	if !containsStr(ids, "greenfield-tdd") {
		t.Errorf("SelectedIDs() = %v after keyboard-only flow; want 'greenfield-tdd'", ids)
	}
}

// TestWorkflowBrowser_KeyboardOnly_FullFlow_ConfirmFromWorkflowLevel verifies that a
// complete session can also be confirmed directly from the workflow level without returning
// to the category list first.
func TestWorkflowBrowser_KeyboardOnly_FullFlow_ConfirmFromWorkflowLevel(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Open Build folder with Right.
	s.Update(tea.KeyMsg{Type: tea.KeyRight})

	// Toggle the first workflow with Space.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	// Confirm directly from the workflow level with Enter.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Right→Space→Enter (confirm from workflow level); want true")
	}
	ids := s.SelectedIDs()
	if !containsStr(ids, "greenfield-tdd") {
		t.Errorf("SelectedIDs() = %v after confirming from workflow level; want 'greenfield-tdd'", ids)
	}
}

// TestWorkflowBrowser_KeyboardOnly_VimKeys verifies that vim-style navigation (j/k) and the
// vim-style open-folder key (l) work for fully keyboard-driven use.
func TestWorkflowBrowser_KeyboardOnly_VimKeys(t *testing.T) {
	s := newWFBrowser(wfAllCategories)

	// Navigate to the second category using 'j'.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})

	// Open the second category (Audit) with 'l' (vim-style Right).
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	// The view should show Audit workflows.
	view := s.View()
	if !strings.Contains(view, "Brownfield PR Audit") {
		t.Errorf("vim 'j'+'l' keys must navigate into Audit category and show its workflows; got:\n%s", view)
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
