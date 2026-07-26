package screens

// workflows.go implements the WorkflowBrowserScreen: a two-level navigation over workflow
// categories and the workflows within each category, with cross-folder multi-select state
// and a focused-workflow detail pane.
//
// Navigation contract:
//   - Level 0 (categories): ↑/↓ navigate, Enter opens a folder, Tab confirms the full
//     selection, Esc sets Back().
//   - Level 1 (workflows within a folder): ↑/↓ navigate, Space toggles selection, Enter
//     or Esc returns to the category list with selections intact.
//
// Selection state is stored in a persistent map keyed by workflow ID. When entering a
// folder the map is consulted to restore prior checks; when leaving it is updated so that
// deselected items are also removed. This is the mechanism that makes cross-folder
// multi-select possible without any special inter-widget protocol.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/widgets"
)

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

	// Error and empty state.
	hasError bool
	errorMsg string

	done   bool
	back   bool
	width  int
	height int
	styles Styles

	// Keybindings that the category list does not handle (Tab to confirm).
	confirmKey key.Binding
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
		confirmKey:    key.NewBinding(key.WithKeys("tab")),
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

// Update processes a key message. Tab at the category level confirms the whole selection.
func (s *WorkflowBrowserScreen) Update(msg tea.Msg) tea.Cmd {
	if s.hasError {
		// In error state only Esc is recognised.
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEsc {
			s.back = true
		}
		return nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// Tab at category level confirms the selection from any folder state.
		if s.level == wfNavCategories && key.Matches(keyMsg, s.confirmKey) {
			s.done = true
			return nil
		}
		// Esc at workflow level returns to category list without consuming the Esc.
		if s.level == wfNavWorkflows && keyMsg.Type == tea.KeyEsc {
			s.leaveCategory()
			return nil
		}
	}

	switch s.level {
	case wfNavCategories:
		cmd := s.catList.Update(msg)
		if s.catList.Back() {
			s.catList.Reset()
			s.back = true
		} else if s.catList.Done() {
			idx := s.catList.CursorIndex()
			s.catList.Reset()
			if idx >= 0 && idx < len(s.categories) {
				s.enterCategory(idx)
			}
		}
		return cmd

	case wfNavWorkflows:
		cmd := s.wfMultisel.Update(msg)
		if s.wfMultisel.Done() || s.wfMultisel.Back() {
			s.wfMultisel.Reset()
			s.leaveCategory()
		} else {
			s.syncCurrentFolder()
			s.updateDetail()
		}
		return cmd
	}
	return nil
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
// bar. At the workflow level the multi-select and detail pane are shown side by side.
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

	return strings.Join([]string{title, border, content, border, summary, help}, "\n")
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

// renderHelp returns the context-sensitive help bar.
func (s *WorkflowBrowserScreen) renderHelp() string {
	if s.level == wfNavWorkflows {
		return s.styles.Help.Width(s.width).Render(
			"↑/k up  ↓/j down  space toggle  enter/esc back to folders  ctrl+c quit",
		)
	}
	return s.styles.Help.Width(s.width).Render(
		"↑/k up  ↓/j down  enter open folder  tab confirm  esc cancel  ctrl+c quit",
	)
}

// Done reports whether the user confirmed the selection (Tab at category level).
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
