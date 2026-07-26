package screens

// workflows.go implements the WorkflowBrowserScreen: a two-level navigation over workflow
// categories and the workflows within each category, with cross-folder multi-select state
// and a focused-workflow detail pane.
//
// Navigation contract:
//   - Level 0 (categories): ↑/↓ navigate; →/l opens the focused folder; Enter confirms
//     the full selection (if non-empty); Esc cancels the whole screen.
//   - Level 1 (workflows within a folder): ↑/↓ navigate; Space toggles selection;
//     ←/h or Esc returns to the category list with selections preserved; Enter confirms
//     the full selection (if non-empty) directly from this level.
//
// Tab is not a binding at either level.
//
// Selection state is stored in a persistent map keyed by workflow ID. When entering a
// folder the map is consulted to restore prior checks; when leaving it is updated so that
// deselected items are also removed. This is the mechanism that makes cross-folder
// multi-select possible without any special inter-widget protocol.
//
// Empty-selection guard: if the user presses Enter at either level with no workflows
// selected a transient validation message is displayed. The message is cleared on the
// next navigation (entering or leaving a folder) or when a workflow is toggled. Esc
// always remains a working cancel or back path regardless of validation state.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/widgets"
)

// MsgNoWorkflowsSelected is the transient validation message rendered when the user
// attempts to confirm with an empty selection. It is exported so tests can assert on
// its presence without duplicating the literal.
const MsgNoWorkflowsSelected = "No workflows selected — press space to select at least one, or esc to cancel."

// wfNavLevel identifies which navigation level the browser is showing.
type wfNavLevel int

const (
	wfNavCategories wfNavLevel = iota // category folder list
	wfNavWorkflows                    // workflow list within a selected category
)

// WorkflowBrowserScreen presents a two-level workflow browser. Categories are shown at
// the first level; opening one reveals the workflows within it for multi-select.
// Selections survive navigating between folders.
type WorkflowBrowserScreen struct {
	categories    []domain.WorkflowCategory
	level         wfNavLevel
	currentCatIdx int // index into categories; valid when level == wfNavWorkflows

	// Level 0: category list.
	catList *widgets.List

	// Level 1: workflow multi-select and detail pane (created on folder enter).
	wfMultisel *widgets.MultiSelect
	detail     *widgets.DetailPane

	// Cross-folder selection state: workflow ID -> selected.
	selected map[string]bool

	// Transient validation message shown when the user tries to confirm with no selection.
	// Separate from hasError (which is a terminal catalog-failure condition).
	validationMsg string

	// Error and empty state.
	hasError bool
	errorMsg string

	done   bool
	back   bool
	width  int
	height int
	styles Styles
}

// NewWorkflowBrowserScreen creates the workflow browser. categories is the ordered set of
// workflow categories from the catalog. Pass errorMsg to render an error state; an empty
// message with no categories produces the empty state.
func NewWorkflowBrowserScreen(
	categories []domain.WorkflowCategory,
	width, height int,
	styles Styles,
	errorMsg string,
) *WorkflowBrowserScreen {
	catItems := makeCategoryItems(categories)
	listStyles := widgets.ListStyles{
		Normal:   styles.Body,
		Selected: styles.Selected,
		Disabled: styles.Muted,
		Cursor:   "▶",
	}
	lh := wfListHeight(height)
	lw := wfListWidth(width)
	dw := width - lw - 1
	if dw < 10 {
		dw = 10
	}
	detailStyles := widgets.DetailPaneStyles{
		Title:  styles.Subtitle,
		Body:   styles.Body,
		Border: styles.Border,
		Empty:  styles.Muted,
	}
	s := &WorkflowBrowserScreen{
		categories:    categories,
		level:         wfNavCategories,
		currentCatIdx: -1,
		catList:       widgets.NewList(catItems, lh, lw, listStyles),
		detail:        widgets.NewDetailPane(lh, dw, detailStyles),
		selected:      make(map[string]bool),
		hasError:      errorMsg != "",
		errorMsg:      errorMsg,
		width:         width,
		height:        height,
		styles:        styles,
	}
	return s
}

// makeCategoryItems converts workflow categories into list items.
func makeCategoryItems(cats []domain.WorkflowCategory) []widgets.ListItem {
	items := make([]widgets.ListItem, len(cats))
	for i, cat := range cats {
		n := len(cat.Workflows)
		suffix := "workflows"
		if n == 1 {
			suffix = "workflow"
		}
		items[i] = widgets.ListItem{
			ID:    cat.Name,
			Label: fmt.Sprintf("%-20s  (%d %s)", cat.Name, n, suffix),
		}
	}
	return items
}

// makeWorkflowItems converts a slice of workflows into multi-select list items. Each item
// label shows both the workflow name and its ID.
func makeWorkflowItems(wfs []domain.Workflow) []widgets.ListItem {
	items := make([]widgets.ListItem, len(wfs))
	for i, wf := range wfs {
		label := fmt.Sprintf("%s  [%s]", wf.Name, wf.ID)
		items[i] = widgets.ListItem{
			ID:    wf.ID,
			Label: label,
		}
	}
	return items
}

// isRune reports whether the key message is for the given rune character.
func isRune(keyMsg tea.KeyMsg, r rune) bool {
	return keyMsg.Type == tea.KeyRunes && len(keyMsg.Runes) == 1 && keyMsg.Runes[0] == r
}

// Update processes a key message. At the category level: →/l opens a folder, Enter
// confirms the selection (if non-empty), Esc cancels. At the workflow level: Space
// toggles, ←/h and Esc return to the category list, Enter confirms the selection (if
// non-empty). Tab is inert at both levels.
func (s *WorkflowBrowserScreen) Update(msg tea.Msg) tea.Cmd {
	if s.hasError {
		// In error state only Esc is recognised.
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEsc {
			s.back = true
		}
		return nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	switch s.level {
	case wfNavCategories:
		return s.updateCategories(keyMsg)
	case wfNavWorkflows:
		return s.updateWorkflows(keyMsg)
	}
	return nil
}

// updateCategories handles key events at the category-list level.
func (s *WorkflowBrowserScreen) updateCategories(keyMsg tea.KeyMsg) tea.Cmd {
	switch {
	case keyMsg.Type == tea.KeyEsc:
		// Esc at category level cancels the whole screen.
		s.back = true
		return nil

	case keyMsg.Type == tea.KeyEnter:
		// Enter confirms if at least one workflow is selected; otherwise shows the
		// transient validation message.
		if len(s.SelectedIDs()) > 0 {
			s.done = true
		} else {
			s.validationMsg = MsgNoWorkflowsSelected
		}
		return nil

	case keyMsg.Type == tea.KeyRight || isRune(keyMsg, 'l'):
		// Right or l opens the focused category folder.
		s.validationMsg = ""
		idx := s.catList.CursorIndex()
		if idx >= 0 && idx < len(s.categories) {
			s.enterCategory(idx)
		}
		return nil

	case keyMsg.Type == tea.KeyTab:
		// Tab is not a binding at this level; consume and ignore.
		return nil
	}

	// Delegate navigation (Up/Down/j/k) to the category list widget. Enter and Esc
	// are already intercepted above so the widget's Done/Back flags will not be set,
	// but we reset them defensively.
	cmd := s.catList.Update(keyMsg)
	if s.catList.Done() || s.catList.Back() {
		s.catList.Reset()
	}
	return cmd
}

// updateWorkflows handles key events at the workflow-list level.
func (s *WorkflowBrowserScreen) updateWorkflows(keyMsg tea.KeyMsg) tea.Cmd {
	switch {
	case keyMsg.Type == tea.KeyEsc || keyMsg.Type == tea.KeyLeft || isRune(keyMsg, 'h'):
		// Esc, Left, or h returns to the category list, preserving selections.
		s.validationMsg = ""
		s.leaveCategory()
		return nil

	case keyMsg.Type == tea.KeyEnter:
		// Enter confirms if at least one workflow is selected; otherwise shows the
		// transient validation message.
		if len(s.SelectedIDs()) > 0 {
			s.done = true
		} else {
			s.validationMsg = MsgNoWorkflowsSelected
		}
		return nil

	case keyMsg.Type == tea.KeyTab:
		// Tab is not a binding at this level; consume and ignore.
		return nil
	}

	// Delegate Space (toggle) and navigation (Up/Down/j/k) to the multi-select widget.
	// Enter and Esc are already intercepted above so the widget's Done/Back flags will
	// not be set, but we reset them defensively.
	cmd := s.wfMultisel.Update(keyMsg)
	if s.wfMultisel.Done() || s.wfMultisel.Back() {
		s.wfMultisel.Reset()
	} else {
		// Clear the validation message whenever the user interacts with the list
		// (covers Space toggle as well as cursor movement).
		s.validationMsg = ""
		s.syncCurrentFolder()
		s.updateDetail()
	}
	return cmd
}

// enterCategory transitions to the workflow-list view for the category at idx.
func (s *WorkflowBrowserScreen) enterCategory(idx int) {
	s.currentCatIdx = idx
	cat := s.categories[idx]

	items := makeWorkflowItems(cat.Workflows)
	msStyles := widgets.MultiSelectStyles{
		Normal:   s.styles.Body,
		Selected: s.styles.Selected,
		Checked:  s.styles.Checked,
		Disabled: s.styles.Muted,
		Cursor:   "▶",
		CheckOn:  "[x]",
		CheckOff: "[ ]",
	}
	lh := wfListHeight(s.height)
	lw := wfListWidth(s.width)
	s.wfMultisel = widgets.NewMultiSelect(items, lh, lw, msStyles)

	// Restore prior selections for this folder.
	for _, wf := range cat.Workflows {
		if s.selected[wf.ID] {
			s.wfMultisel.SetChecked(wf.ID, true)
		}
	}

	s.level = wfNavWorkflows
	s.updateDetail()
}

// leaveCategory syncs checked state back to the selected map and returns to the category list.
func (s *WorkflowBrowserScreen) leaveCategory() {
	if s.currentCatIdx >= 0 && s.currentCatIdx < len(s.categories) {
		cat := s.categories[s.currentCatIdx]
		for _, wf := range cat.Workflows {
			if s.wfMultisel.IsChecked(wf.ID) {
				s.selected[wf.ID] = true
			} else {
				delete(s.selected, wf.ID)
			}
		}
	}
	s.level = wfNavCategories
	s.wfMultisel = nil
	s.detail.SetContent("", "")
}

// syncCurrentFolder eagerly updates the selected map from the current workflow multi-select
// state so the selection summary is accurate while the user is inside a folder.
func (s *WorkflowBrowserScreen) syncCurrentFolder() {
	if s.level != wfNavWorkflows || s.currentCatIdx < 0 || s.currentCatIdx >= len(s.categories) {
		return
	}
	cat := s.categories[s.currentCatIdx]
	for _, wf := range cat.Workflows {
		if s.wfMultisel.IsChecked(wf.ID) {
			s.selected[wf.ID] = true
		} else {
			delete(s.selected, wf.ID)
		}
	}
}

// updateDetail refreshes the detail pane to show the focused workflow's name, ID, description
// and hint.
func (s *WorkflowBrowserScreen) updateDetail() {
	if s.level != wfNavWorkflows || s.wfMultisel == nil || s.currentCatIdx < 0 {
		s.detail.SetContent("", "")
		return
	}
	cat := s.categories[s.currentCatIdx]
	idx := s.wfMultisel.CursorIndex()
	if idx < 0 || idx >= len(cat.Workflows) {
		s.detail.SetContent("", "")
		return
	}
	wf := cat.Workflows[idx]
	title := fmt.Sprintf("%s [%s]", wf.Name, wf.ID)
	var body strings.Builder
	if wf.Description != "" {
		fmt.Fprintf(&body, "%s\n\n", wf.Description)
	}
	if wf.Hint != "" {
		fmt.Fprintf(&body, "Hint: %s", wf.Hint)
	}
	s.detail.SetContent(title, body.String())
}

// View renders the browser. At the category level the category list is shown with a summary
// bar. At the workflow level the multi-select and detail pane are shown side by side. A
// transient validation message is shown when the user attempts to confirm with no selection.
func (s *WorkflowBrowserScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Select Workflows")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	var content string
	switch {
	case s.hasError:
		content = s.styles.Error.Width(s.width).Render("Error: " + s.errorMsg)

	case len(s.categories) == 0:
		content = s.styles.Muted.Width(s.width).Render("No workflows found in the catalog.")

	case s.level == wfNavCategories:
		content = s.catList.View()

	case s.level == wfNavWorkflows:
		lw := wfListWidth(s.width)
		sep := s.styles.Border.Render("│")
		content = lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Width(lw).Render(s.wfMultisel.View()),
			sep,
			s.detail.View(),
		)
	}

	summary := s.renderSummary()
	help := s.renderHelp()

	parts := []string{title, border, content, border, summary}
	if s.validationMsg != "" {
		parts = append(parts, s.styles.Error.Width(s.width).Render(s.validationMsg))
	}
	parts = append(parts, help)
	return strings.Join(parts, "\n")
}

// renderSummary produces the persistent selection summary line.
func (s *WorkflowBrowserScreen) renderSummary() string {
	ids := s.SelectedIDs()
	if len(ids) == 0 {
		return s.styles.Muted.Width(s.width).Render("No workflows selected.")
	}
	noun := "workflow"
	if len(ids) > 1 {
		noun = "workflows"
	}
	label := fmt.Sprintf("Selected %d %s: %s", len(ids), noun, strings.Join(ids, ", "))
	return s.styles.Success.Width(s.width).Render(label)
}

// renderHelp returns the context-sensitive help bar matching the active bindings at the
// current navigation level.
func (s *WorkflowBrowserScreen) renderHelp() string {
	if s.level == wfNavWorkflows {
		return s.styles.Help.Width(s.width).Render(
			"↑/k up  ↓/j down  space toggle  ←/h back to folders  enter confirm  ctrl+c quit",
		)
	}
	return s.styles.Help.Width(s.width).Render(
		"↑/k up  ↓/j down  →/l open folder  enter confirm  esc cancel  ctrl+c quit",
	)
}

// Done reports whether the user confirmed the selection.
func (s *WorkflowBrowserScreen) Done() bool { return s.done }

// Back reports whether the user cancelled (Esc at category level).
func (s *WorkflowBrowserScreen) Back() bool { return s.back }

// SelectedIDs returns the workflow IDs selected across all folders, in category-declaration
// order. This is safe to call at any time including while inside a folder.
func (s *WorkflowBrowserScreen) SelectedIDs() []string {
	var ids []string
	seen := make(map[string]bool, len(s.selected))
	for _, cat := range s.categories {
		for _, wf := range cat.Workflows {
			if s.selected[wf.ID] && !seen[wf.ID] {
				seen[wf.ID] = true
				ids = append(ids, wf.ID)
			}
		}
	}
	return ids
}

// Reset clears Done and Back flags.
func (s *WorkflowBrowserScreen) Reset() {
	s.done = false
	s.back = false
}

// Resize updates the screen dimensions.
func (s *WorkflowBrowserScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	lh := wfListHeight(height)
	lw := wfListWidth(width)
	dw := width - lw - 1
	if dw < 10 {
		dw = 10
	}
	s.catList.Resize(lh, lw)
	s.detail.Resize(lh, dw)
	if s.wfMultisel != nil {
		s.wfMultisel.Resize(lh, lw)
	}
}

// wfListHeight calculates the content area height for the workflow browser.
func wfListHeight(totalHeight int) int {
	// title + border + summary + help + border = 5 rows reserved
	h := totalHeight - 6
	if h < 3 {
		return 3
	}
	return h
}

// wfListWidth calculates the list column width (left two-thirds of the screen).
func wfListWidth(totalWidth int) int {
	w := totalWidth * 2 / 3
	if w < 30 {
		return totalWidth
	}
	return w
}
