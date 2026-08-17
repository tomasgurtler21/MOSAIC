package screens

// deployagents.go implements DeployAgentScreen: a multi-level tree browser for
// the QDeployAgents question.
//
// Navigation contract (post-construction method set unchanged):
//   - At any level: ↑/↓ or k/j navigate; Enter confirms (if selection non-empty),
//     otherwise shows a transient validation message; Tab is inert.
//   - At any level: →/l enters the focused folder row; inert on an agent row.
//   - Below the top level: ←/h or Esc goes back one level, preserving selections.
//   - At the top level: Esc cancels the whole screen (sets Back()).
//   - On an agent-bearing level: Space toggles the focused agent row; Space on a
//     folder row is inert (folder rows are reachable by the cursor but not toggleable).
//   - Selection state lives in a map[string]bool keyed by agent key. Folder rows are
//     never inserted into it and never appear in SelectedIDs().

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-deploy/internal/tui/widgets"
)

// MsgNoAgentsSelected is the transient validation message rendered when the user
// attempts to confirm with an empty selection. Exported so tests assert on it
// without duplicating the literal. Mirrors MsgNoWorkflowsSelected in workflows.go.
const MsgNoAgentsSelected = "No agents selected — press space to select at least one, or esc to cancel."

// rowKind identifies whether a level row is a navigable folder or a selectable agent.
type rowKind int

const (
	rowKindFolder rowKind = iota // folder row: enter with →/l; Space is inert
	rowKindAgent                 // agent row: toggle with Space; →/l is inert
)

// levelRow is the descriptor for one row in the active level widget. Its index in
// levelRows exactly matches the corresponding item index in levelList/levelMultisel,
// allowing O(1) kind-lookup without parsing item IDs.
type levelRow struct {
	kind     rowKind
	childIdx int // index into node.Children; valid when kind == rowKindFolder
	agentIdx int // index into node.Agents;    valid when kind == rowKindAgent
}

// DeployAgentScreen presents a multi-level tree browser for QDeployAgents.
// It browses the agent catalog across an arbitrary number of folder levels,
// accumulates selections across folders, and confirms the cumulative set on Enter.
// Folder rows never appear in SelectedIDs() — only agent keys are returned.
type DeployAgentScreen struct {
	root *AgentTreeNode

	// Navigation state: path is the open folder chain; path[0] is always the root;
	// the current level is the last element. Length 1 means "top level". Depth is
	// driven by the tree data — no fixed level enum or hard-coded depth assumption.
	path []*AgentTreeNode

	// cursorStack holds the focused row index in each ancestor level, parallel to
	// path (excluding the last element, which is the current level). When navigating
	// back out of a subfolder, the saved index is restored so the user's previously
	// focused row is re-selected rather than defaulting to position 0.
	cursorStack []int

	// Row descriptors for the current level, parallel to the active widget's item slice.
	levelRows []levelRow

	// Current level widget: exactly one is non-nil during normal browsing.
	// levelList is used when the current node has no own Agents (folder-only level).
	// levelMultisel is used when the current node has own Agents (agent-bearing / mixed level).
	levelList    *widgets.List
	levelMultisel *widgets.MultiSelect

	// detail is paired with levelMultisel and shows the focused agent's description.
	// Nil when levelList is active or when in the error/empty state.
	detail *widgets.DetailPane

	// Cross-level selection map: agent key → selected. The single source of truth for
	// SelectedIDs(). Only agent keys are ever inserted — folder names never cross this boundary.
	selected map[string]bool

	// Transient empty-selection validation message; cleared on any navigation or toggle.
	validationMsg string

	// Terminal catalog-failure state.
	hasError bool
	errorMsg string

	done   bool
	back   bool
	width  int
	height int
	styles Styles
}

// NewDeployAgentScreen builds the multi-level folder browser for QDeployAgents.
// root is the tree produced by the option→tree adapter; a nil root or a tree with
// no reachable agents produces the empty state. Pass a non-empty errorMsg to render
// the error state instead of the browser. The screen hardcodes no catalog folder
// name — it renders whatever names the tree supplies.
func NewDeployAgentScreen(
	root *AgentTreeNode,
	width, height int,
	styles Styles,
	errorMsg string,
) *DeployAgentScreen {
	s := &DeployAgentScreen{
		root:     root,
		hasError: errorMsg != "",
		errorMsg: errorMsg,
		width:    width,
		height:   height,
		styles:   styles,
		selected: make(map[string]bool),
	}
	if !s.hasError && !s.isEmpty() {
		s.path = []*AgentTreeNode{root}
		s.buildLevelWidget(root)
	}
	return s
}

// isEmpty reports whether the tree has no reachable agents.
func (s *DeployAgentScreen) isEmpty() bool {
	return s.root == nil || len(s.root.AgentKeys()) == 0
}

// currentNode returns the node at the active path tip, or nil if the path is empty.
func (s *DeployAgentScreen) currentNode() *AgentTreeNode {
	if len(s.path) == 0 {
		return nil
	}
	return s.path[len(s.path)-1]
}

// currentCursorIndex returns the cursor index in the active level widget, or -1.
func (s *DeployAgentScreen) currentCursorIndex() int {
	if s.levelList != nil {
		return s.levelList.CursorIndex()
	}
	if s.levelMultisel != nil {
		return s.levelMultisel.CursorIndex()
	}
	return -1
}

// buildLevelWidget constructs the row descriptors and the active widget for node.
//
// Row order: all folder rows (Children order) then all agent rows (Agents order).
//
// Widget choice:
//   - len(node.Agents) == 0: widgets.List (no checkbox column — folder-only level).
//   - len(node.Agents) > 0:  widgets.MultiSelect + widgets.DetailPane (agent-bearing level).
//
// Prior selections are restored into the new widget from the selected map.
func (s *DeployAgentScreen) buildLevelWidget(node *AgentTreeNode) {
	s.levelList = nil
	s.levelMultisel = nil
	s.detail = nil

	isMixed := len(node.Agents) > 0
	rows := make([]levelRow, 0, len(node.Children)+len(node.Agents))
	items := make([]widgets.ListItem, 0, len(node.Children)+len(node.Agents))

	// Folder rows first, in Children order.
	for i, child := range node.Children {
		n := len(child.AgentKeys())
		noun := "agents"
		if n == 1 {
			noun = "agent"
		}
		label := fmt.Sprintf("%-20s  (%d %s)", child.Name, n, noun)
		if isMixed {
			// "▸ " prefix distinguishes folder rows from agent rows (which carry a checkbox).
			label = "▸ " + label
		}
		rows = append(rows, levelRow{kind: rowKindFolder, childIdx: i})
		items = append(items, widgets.ListItem{
			ID:    "dir:" + child.Name,
			Label: label,
		})
	}

	// Agent rows next, in Agents order.
	for i, agent := range node.Agents {
		label := agent.Name
		if label == "" {
			label = agent.Key
		}
		rows = append(rows, levelRow{kind: rowKindAgent, agentIdx: i})
		items = append(items, widgets.ListItem{
			ID:    agent.Key,
			Label: label,
		})
	}

	s.levelRows = rows
	lh := uaListHeight(s.height)
	lw := uaListWidth(s.width)

	if !isMixed {
		// Folder-only level: List with no checkbox column.
		listStyles := widgets.ListStyles{
			Normal:   s.styles.Body,
			Selected: s.styles.Selected,
			Disabled: s.styles.Muted,
			Cursor:   "▶",
		}
		s.levelList = widgets.NewList(items, lh, lw, listStyles)
		return
	}

	// Agent-bearing level: MultiSelect paired with a detail pane.
	msStyles := widgets.MultiSelectStyles{
		Normal:   s.styles.Body,
		Selected: s.styles.Selected,
		Checked:  s.styles.Checked,
		Disabled: s.styles.Muted,
		Cursor:   "▶",
		CheckOn:  "[x]",
		CheckOff: "[ ]",
	}
	s.levelMultisel = widgets.NewMultiSelect(items, lh, lw, msStyles)
	for _, agent := range node.Agents {
		if s.selected[agent.Key] {
			s.levelMultisel.SetChecked(agent.Key, true)
		}
	}

	dw := s.width - lw - 1
	if dw < 10 {
		dw = 10
	}
	detailStyles := widgets.DetailPaneStyles{
		Title:  s.styles.Subtitle,
		Body:   s.styles.Body,
		Border: s.styles.Border,
		Empty:  s.styles.Muted,
	}
	s.detail = widgets.NewDetailPane(lh, dw, detailStyles)
	s.updateDetail()
}

// enterLevel syncs the current level's selections, saves the current cursor index
// onto the cursor stack, pushes child onto the path, and builds the new level's widget.
func (s *DeployAgentScreen) enterLevel(child *AgentTreeNode) {
	s.syncCurrentLevel()
	s.cursorStack = append(s.cursorStack, s.currentCursorIndex())
	s.path = append(s.path, child)
	s.buildLevelWidget(child)
}

// leaveLevel syncs the current level's selections, pops the path and cursor
// stack, rebuilds the parent level's widget, and restores the previously focused
// row index so the user lands back on the folder they entered rather than row 0.
// Does nothing when already at the root.
func (s *DeployAgentScreen) leaveLevel() {
	if len(s.path) <= 1 {
		return
	}
	s.syncCurrentLevel()
	s.path = s.path[:len(s.path)-1]

	// Pop the saved cursor index for the parent level, if one was recorded.
	var savedIdx int
	hasSaved := len(s.cursorStack) > 0
	if hasSaved {
		savedIdx = s.cursorStack[len(s.cursorStack)-1]
		s.cursorStack = s.cursorStack[:len(s.cursorStack)-1]
	}

	s.buildLevelWidget(s.path[len(s.path)-1])

	// Restore the cursor to the row the user was focused on before entering the
	// subfolder. This must be done after buildLevelWidget because the widget is
	// recreated fresh by that call.
	if hasSaved {
		if s.levelList != nil {
			s.levelList.SetCursorIndex(savedIdx)
		} else if s.levelMultisel != nil {
			s.levelMultisel.SetCursorIndex(savedIdx)
		}
	}
}

// syncCurrentLevel writes the MultiSelect widget's checked state back into the
// selected map so that SelectedIDs() always reflects the live widget state.
// No-op for List levels (folder-only) since they hold no agent selections.
func (s *DeployAgentScreen) syncCurrentLevel() {
	if s.levelMultisel == nil {
		return
	}
	node := s.currentNode()
	if node == nil {
		return
	}
	for _, agent := range node.Agents {
		if s.levelMultisel.IsChecked(agent.Key) {
			s.selected[agent.Key] = true
		} else {
			delete(s.selected, agent.Key)
		}
	}
}

// tryEnterFocusedFolder enters the child folder at the cursor position.
// Does nothing when the focused row is an agent row or the cursor is out of range.
func (s *DeployAgentScreen) tryEnterFocusedFolder() {
	idx := s.currentCursorIndex()
	if idx < 0 || idx >= len(s.levelRows) {
		return
	}
	row := s.levelRows[idx]
	if row.kind != rowKindFolder {
		return // agent row: →/l is inert
	}
	node := s.currentNode()
	if node == nil || row.childIdx < 0 || row.childIdx >= len(node.Children) {
		return
	}
	s.enterLevel(node.Children[row.childIdx])
}

// updateDetail refreshes the detail pane based on the currently focused row.
// Clears the pane on a folder row; shows the agent's name and description on an
// agent row. No-op when no detail pane is active (List-level or error/empty state).
func (s *DeployAgentScreen) updateDetail() {
	if s.detail == nil {
		return
	}
	idx := s.currentCursorIndex()
	if idx < 0 || idx >= len(s.levelRows) {
		s.detail.SetContent("", "")
		return
	}
	row := s.levelRows[idx]
	if row.kind == rowKindFolder {
		s.detail.SetContent("", "")
		return
	}
	node := s.currentNode()
	if node == nil || row.agentIdx < 0 || row.agentIdx >= len(node.Agents) {
		s.detail.SetContent("", "")
		return
	}
	agent := node.Agents[row.agentIdx]
	name := agent.Name
	if name == "" {
		name = agent.Key
	}
	s.detail.SetContent(name, agent.Description)
}

// Update processes key input.
// In the error state and empty state only Esc is handled (sets Back()).
// Otherwise full tree navigation is dispatched.
func (s *DeployAgentScreen) Update(msg tea.Msg) tea.Cmd {
	if s.hasError || s.isEmpty() {
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEsc {
			s.back = true
		}
		return nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	return s.updateNormal(keyMsg)
}

// updateNormal dispatches key events during normal (non-error, non-empty) browsing.
func (s *DeployAgentScreen) updateNormal(keyMsg tea.KeyMsg) tea.Cmd {
	isTopLevel := len(s.path) == 1

	switch {
	case keyMsg.Type == tea.KeyEsc:
		if isTopLevel {
			s.back = true
		} else {
			s.validationMsg = ""
			s.leaveLevel()
		}
		return nil

	case keyMsg.Type == tea.KeyLeft || isRune(keyMsg, 'h'):
		if !isTopLevel {
			s.validationMsg = ""
			s.leaveLevel()
		}
		return nil

	case keyMsg.Type == tea.KeyEnter:
		if len(s.SelectedIDs()) > 0 {
			s.done = true
		} else {
			s.validationMsg = MsgNoAgentsSelected
		}
		return nil

	case keyMsg.Type == tea.KeyTab:
		// Tab is inert at every level.
		return nil

	case keyMsg.Type == tea.KeyRight || isRune(keyMsg, 'l'):
		s.validationMsg = ""
		s.tryEnterFocusedFolder()
		return nil
	}

	// Space on a folder row in a mixed level is inert — intercepted before the widget
	// can toggle the folder row's ID into the MultiSelect's checked set.
	if isRune(keyMsg, ' ') && s.levelMultisel != nil {
		idx := s.levelMultisel.CursorIndex()
		if idx >= 0 && idx < len(s.levelRows) && s.levelRows[idx].kind == rowKindFolder {
			return nil
		}
	}

	// Delegate navigation (Up/Down/j/k) and Space toggle to the active level widget.
	// Enter and Esc are already intercepted above; reset Done/Back flags defensively.
	if s.levelList != nil {
		cmd := s.levelList.Update(keyMsg)
		if s.levelList.Done() || s.levelList.Back() {
			s.levelList.Reset()
		} else {
			s.validationMsg = ""
			s.updateDetail()
		}
		return cmd
	}
	if s.levelMultisel != nil {
		cmd := s.levelMultisel.Update(keyMsg)
		if s.levelMultisel.Done() || s.levelMultisel.Back() {
			s.levelMultisel.Reset()
		} else {
			s.validationMsg = ""
			s.syncCurrentLevel()
			s.updateDetail()
		}
		return cmd
	}
	return nil
}

// View renders the agent-selection screen.
// Title, a border, the current level content, a border, the selection summary,
// an optional transient validation message, and the help bar are joined vertically.
func (s *DeployAgentScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Select Agents")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	if s.hasError {
		errLine := s.styles.Error.Width(s.width).Render("Error: " + s.errorMsg)
		return strings.Join([]string{title, border, errLine}, "\n")
	}

	if s.isEmpty() {
		emptyLine := s.styles.Muted.Width(s.width).Render("No agents are available in the catalog.")
		return strings.Join([]string{title, border, emptyLine}, "\n")
	}

	var content string
	switch {
	case s.levelList != nil:
		content = s.levelList.View()
	case s.levelMultisel != nil:
		lw := uaListWidth(s.width)
		sep := s.styles.Border.Render("│")
		content = lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Width(lw).Render(s.levelMultisel.View()),
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

// renderSummary produces the persistent selection summary line below the content area.
func (s *DeployAgentScreen) renderSummary() string {
	ids := s.SelectedIDs()
	if len(ids) == 0 {
		return s.styles.Muted.Width(s.width).Render("No agents selected.")
	}
	noun := "agent"
	if len(ids) > 1 {
		noun = "agents"
	}
	label := fmt.Sprintf("Selected %d %s: %s", len(ids), noun, strings.Join(ids, ", "))
	return s.styles.Success.Width(s.width).Render(label)
}

// renderHelp returns the context-sensitive help bar matching the active bindings at
// the current navigation level.
func (s *DeployAgentScreen) renderHelp() string {
	if len(s.path) > 1 {
		// Below the top level: ←/h/esc goes back one level. Space is only meaningful
		// when the current node has own agents (i.e. a MultiSelect widget is active).
		if s.levelMultisel != nil {
			return s.styles.Help.Width(s.width).Render(
				"↑/k up  ↓/j down  space toggle  ←/h/esc back  enter confirm  ctrl+c quit",
			)
		}
		return s.styles.Help.Width(s.width).Render(
			"↑/k up  ↓/j down  →/l open folder  ←/h/esc back  enter confirm  ctrl+c quit",
		)
	}
	return s.styles.Help.Width(s.width).Render(
		"↑/k up  ↓/j down  →/l open folder  enter confirm  esc cancel  ctrl+c quit",
	)
}

// Done reports whether the user confirmed the selection.
func (s *DeployAgentScreen) Done() bool { return s.done }

// Back reports whether the user cancelled (Esc at the top level, or Esc in an
// error/empty state).
func (s *DeployAgentScreen) Back() bool { return s.back }

// SelectedIDs returns the keys of the selected agents in root.AgentKeys() traversal
// order. Only agent keys are returned — folder names never appear here. Returns nil
// when nothing is selected. Safe to call at any level and at any time.
func (s *DeployAgentScreen) SelectedIDs() []string {
	if s.root == nil {
		return nil
	}
	allKeys := s.root.AgentKeys()
	var result []string
	for _, key := range allKeys {
		if s.selected[key] {
			result = append(result, key)
		}
	}
	return result
}

// Reset clears the Done and Back flags. Does not clear the selection map or the
// navigation path, matching WorkflowBrowserScreen.Reset semantics.
func (s *DeployAgentScreen) Reset() {
	s.done = false
	s.back = false
}

// Resize updates the screen dimensions and resizes any active level widget and
// detail pane. Does not panic at any depth, in the empty state, or in the error state.
func (s *DeployAgentScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	lh := uaListHeight(height)
	lw := uaListWidth(width)
	dw := width - lw - 1
	if dw < 10 {
		dw = 10
	}
	if s.levelList != nil {
		s.levelList.Resize(lh, lw)
	}
	if s.levelMultisel != nil {
		s.levelMultisel.Resize(lh, lw)
	}
	if s.detail != nil {
		s.detail.Resize(lh, dw)
	}
}
