package screens

// deployagents.go implements DeployAgentScreen: a browsable, grouped multi-select
// for the QDeployAgents question.
//
// It is a sibling of StandaloneAgentScreen, not a modification of it, so the
// still-live legacy standalone mode is untouched.
//
// Navigation contract (identical post-construction method set to StandaloneAgentScreen):
//   - ↑/↓ or k/j to navigate.
//   - Space to toggle selection (agent rows only; heading rows are not selectable).
//   - Enter to confirm the selection.
//   - Esc to go back without confirming.
//
// Category rendering contract:
//   - Each category contributes a non-selectable heading row followed by its agents.
//   - The empty-name category is rendered under the heading "(uncategorized)" and is always last.
//   - The screen hardcodes no catalog category name; it renders whatever groups it is given.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/widgets"
)

// DeployAgentScreen presents a browsable, grouped multi-select for QDeployAgents.
// It renders exactly the categories it is handed, in the order it is handed them.
// Heading rows are non-selectable; the empty-Name category renders last under "(uncategorized)".
type DeployAgentScreen struct {
	categories []AgentCategory
	// agents is a parallel slice to the multiselect items: nil for heading rows,
	// non-nil for agent rows. Used to resolve the focused agent for the detail pane.
	agents   []*domain.Agent
	multisel *widgets.MultiSelect
	detail   *widgets.DetailPane

	// Error / empty state.
	hasError bool
	errorMsg string

	done   bool
	back   bool
	width  int
	height int
	styles Styles
}

// NewDeployAgentScreen builds the browsable, grouped multi-select for QDeployAgents.
// categories must be pre-ordered: named categories in preferred order, empty-Name last.
// The screen renders whatever category names it is given — no catalog name is hardcoded.
// Pass errorMsg to render an error state instead of the list.
func NewDeployAgentScreen(
	categories []AgentCategory,
	width, height int,
	styles Styles,
	errorMsg string,
) *DeployAgentScreen {
	lh := uaListHeight(height)
	lw := uaListWidth(width)
	dw := width - lw - 1
	if dw < 10 {
		dw = 10
	}

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

	items, agentPtrs := buildDeployAgentItems(categories)

	s := &DeployAgentScreen{
		categories: categories,
		agents:     agentPtrs,
		multisel:   widgets.NewMultiSelect(items, lh, lw, msStyles),
		detail:     widgets.NewDetailPane(lh, dw, detailStyles),
		hasError:   errorMsg != "",
		errorMsg:   errorMsg,
		width:      width,
		height:     height,
		styles:     styles,
	}
	s.updateDetail()
	return s
}

// buildDeployAgentItems builds the flat list items and a parallel agent pointer slice from
// the ordered categories. Each category contributes a disabled heading row followed by
// selectable agent rows. The parallel agentPtrs slice is nil for heading rows and points
// to the agent for agent rows.
func buildDeployAgentItems(categories []AgentCategory) ([]widgets.ListItem, []*domain.Agent) {
	var items []widgets.ListItem
	var agentPtrs []*domain.Agent

	for _, cat := range categories {
		headingLabel := cat.Name
		if headingLabel == "" {
			headingLabel = "(uncategorized)"
		}
		// Heading row: non-selectable, unique ID derived from category name.
		items = append(items, widgets.ListItem{
			ID:       "heading:" + cat.Name,
			Label:    headingLabel,
			Disabled: true,
		})
		agentPtrs = append(agentPtrs, nil)

		for i := range cat.Agents {
			a := &cat.Agents[i]
			label := a.Name
			if label == "" {
				label = a.Key
			}
			items = append(items, widgets.ListItem{
				ID:          a.Key,
				Label:       label,
				Description: a.Description,
				Detail:      a.Description,
			})
			agentPtrs = append(agentPtrs, a)
		}
	}
	return items, agentPtrs
}

// Update processes key input. In the error state only Esc is handled. Otherwise the
// multi-select widget handles all key events.
func (s *DeployAgentScreen) Update(msg tea.Msg) tea.Cmd {
	if s.hasError {
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEsc {
			s.back = true
		}
		return nil
	}

	// Empty state: only Esc goes back.
	if totalAgentCount(s.categories) == 0 {
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEsc {
			s.back = true
		}
		return nil
	}

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
func (s *DeployAgentScreen) updateDetail() {
	idx := s.multisel.CursorIndex()
	if idx < 0 || idx >= len(s.agents) || s.agents[idx] == nil {
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

// View renders the agent-selection screen.
func (s *DeployAgentScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Select Agents")
	subtitle := s.styles.Subtitle.Width(s.width).Render(
		"Choose which agents to deploy.",
	)
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	var content string
	switch {
	case s.hasError:
		content = s.styles.Error.Width(s.width).Render("Error: " + s.errorMsg)
	case totalAgentCount(s.categories) == 0:
		content = s.styles.Muted.Width(s.width).Render(
			"No agents are available in the catalog.",
		)
	default:
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
func (s *DeployAgentScreen) Done() bool { return s.done }

// Back reports whether the user cancelled.
func (s *DeployAgentScreen) Back() bool { return s.back }

// SelectedIDs returns the keys of the selected agents.
// Heading rows are never included; only agent keys are returned.
func (s *DeployAgentScreen) SelectedIDs() []string {
	return s.multisel.SelectedIDs()
}

// Reset clears Done and Back flags.
func (s *DeployAgentScreen) Reset() {
	s.done = false
	s.back = false
	s.multisel.Reset()
}

// Resize updates the screen dimensions.
func (s *DeployAgentScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	lh := uaListHeight(height)
	lw := uaListWidth(width)
	dw := width - lw - 1
	if dw < 10 {
		dw = 10
	}
	s.multisel.Resize(lh, lw)
	s.detail.Resize(lh, dw)
}
