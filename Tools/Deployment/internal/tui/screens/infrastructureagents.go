package screens

// infrastructureagents.go implements InfrastructureAgentScreen: a multi-select list of
// infrastructure agents from the catalog, with a description shown in the detail pane
// whenever an agent is focused.
//
// Infrastructure agents are distinguished from ordinary worker agents by a non-empty
// Infrastructure field. This screen presents all of them; no allow-list gating is applied.
// The agent's infrastructure class is shown in the list item label as a suffix
// (e.g., "checkpoint-manager-git [checkpoint]") so the user can distinguish agents by class.
//
// Navigation contract:
//   - ↑/↓ or k/j to navigate.
//   - Space to toggle selection.
//   - Enter to confirm the selection.
//   - Esc to go back without confirming.
//
// Multiple agents of the same class may be selected without restriction. The "at most one
// active per class" rule is enforced at run start, not deploy time.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/widgets"
)

// InfrastructureAgentScreen presents infrastructure agents for multi-select. An agent
// description is shown in the detail pane when that agent has focus. The agent's
// infrastructure class is shown as a label suffix so agents of the same class are
// visually distinguishable.
type InfrastructureAgentScreen struct {
	agents   []domain.Agent
	multisel *widgets.MultiSelect
	detail   *widgets.DetailPane

	done   bool
	back   bool
	width  int
	height int
	styles Styles
}

// NewInfrastructureAgentScreen creates the infrastructure-agent selection screen.
// agents must be the full set of infrastructure agents from the catalog (already filtered
// to non-empty Infrastructure field by the caller).
func NewInfrastructureAgentScreen(
	agents []domain.Agent,
	width, height int,
	styles Styles,
) *InfrastructureAgentScreen {
	lh := iaListHeight(height)
	lw := uaListWidth(width) // reuse same column proportions as utility agents
	dw := width - lw - 1
	if dw < 10 {
		dw = 10
	}

	items := makeInfraAgentItems(agents)
	msStyles := widgets.MultiSelectStyles{
		Normal:   styles.Body,
		Selected: styles.Selected,
		Checked:  styles.Checked,
		Disabled: styles.Muted,
		Cursor:   "▶",
		CheckOn:  "[x]",
		CheckOff: "[ ]",
	}
	detailStyles := widgets.DetailPaneStyles{
		Title:  styles.Subtitle,
		Body:   styles.Body,
		Border: styles.Border,
		Empty:  styles.Muted,
	}

	s := &InfrastructureAgentScreen{
		agents:   agents,
		multisel: widgets.NewMultiSelect(items, lh, lw, msStyles),
		detail:   widgets.NewDetailPane(lh, dw, detailStyles),
		width:    width,
		height:   height,
		styles:   styles,
	}
	s.updateDetail()
	return s
}

// makeInfraAgentItems converts a slice of infrastructure agents into multi-select list items.
// Each item's label is suffixed with " [class]" where class is agent.Infrastructure.
func makeInfraAgentItems(agents []domain.Agent) []widgets.ListItem {
	items := make([]widgets.ListItem, len(agents))
	for i, a := range agents {
		label := a.Name
		if label == "" {
			label = a.Key
		}
		if a.Infrastructure != "" {
			label = label + " [" + a.Infrastructure + "]"
		}
		items[i] = widgets.ListItem{
			ID:          a.Key,
			Label:       label,
			Description: a.Description,
			Detail:      a.Description,
		}
	}
	return items
}

// Update processes key input and delegates to the multi-select widget.
func (s *InfrastructureAgentScreen) Update(msg tea.Msg) tea.Cmd {
	cmd := s.multisel.Update(msg)
	if s.multisel.Done() {
		s.done = true
	} else if s.multisel.Back() {
		s.back = true
	} else {
		s.updateDetail()
	}
	return cmd
}

// updateDetail refreshes the detail pane to show the focused agent's description.
func (s *InfrastructureAgentScreen) updateDetail() {
	idx := s.multisel.CursorIndex()
	if idx < 0 || idx >= len(s.agents) {
		s.detail.SetContent("", "")
		return
	}
	a := s.agents[idx]
	name := a.Name
	if name == "" {
		name = a.Key
	}
	s.detail.SetContent(name, a.Description)
}

// View renders the infrastructure-agent selection screen.
func (s *InfrastructureAgentScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Select Infrastructure Agents")
	subtitle := s.styles.Subtitle.Width(s.width).Render(
		"Choose which infrastructure agents to include in this deployment.",
	)
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	var content string
	if len(s.agents) == 0 {
		content = s.styles.Muted.Width(s.width).Render(
			"No infrastructure agents are available in the catalog.",
		)
	} else {
		lw := uaListWidth(s.width)
		sep := s.styles.Border.Render("│")
		content = lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Width(lw).Render(s.multisel.View()),
			sep,
			s.detail.View(),
		)
	}

	help := s.styles.Help.Width(s.width).Render(
		"↑/k up  ↓/j down  space toggle  enter confirm  esc back  ctrl+c quit",
	)
	return strings.Join([]string{title, subtitle, border, content, border, help}, "\n")
}

// Done reports whether the user confirmed the selection.
func (s *InfrastructureAgentScreen) Done() bool { return s.done }

// Back reports whether the user cancelled.
func (s *InfrastructureAgentScreen) Back() bool { return s.back }

// SelectedIDs returns the keys of the selected infrastructure agents in declaration order.
func (s *InfrastructureAgentScreen) SelectedIDs() []string {
	return s.multisel.SelectedIDs()
}

// Reset clears Done and Back flags.
func (s *InfrastructureAgentScreen) Reset() {
	s.done = false
	s.back = false
	s.multisel.Reset()
}

// Resize updates the screen dimensions.
func (s *InfrastructureAgentScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	lh := iaListHeight(height)
	lw := uaListWidth(width)
	dw := width - lw - 1
	if dw < 10 {
		dw = 10
	}
	s.multisel.Resize(lh, lw)
	s.detail.Resize(lh, dw)
}

// iaListHeight calculates the available height for the infrastructure-agent list.
func iaListHeight(totalHeight int) int {
	// title + subtitle + border + border + help = 5 structural rows + 1 separator overhead = 6 total overhead
	h := totalHeight - 6
	if h < 3 {
		return 3
	}
	return h
}
