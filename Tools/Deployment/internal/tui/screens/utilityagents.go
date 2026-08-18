package screens

// utilityagents.go implements the UtilityAgentScreen: a multi-select list of utility agents
// restricted to the allow-listed set, with a description shown in the detail pane whenever
// an agent is focused.
//
// Navigation contract:
//   - ↑/↓ or k/j to navigate.
//   - Space to toggle selection.
//   - Enter to confirm the selection.
//   - Esc to go back without confirming.
//
// The screen expects to receive only the agents that have already been filtered to the
// allow-list by the caller (app layer). It never accesses the tool config itself.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/widgets"
)

// UtilityAgentScreen presents the allow-listed utility agents for multi-select. An agent
// description is shown in the detail pane when that agent has focus.
type UtilityAgentScreen struct {
	agents   []domain.Agent
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

// NewUtilityAgentScreen creates the utility-agent selection screen.
// agents must already be filtered to the tool config's allow-list by the caller.
// Pass errorMsg to render an error state instead of the list.
func NewUtilityAgentScreen(
	agents []domain.Agent,
	width, height int,
	styles Styles,
	errorMsg string,
) *UtilityAgentScreen {
	lh := uaListHeight(height)
	lw := uaListWidth(width)
	dw := width - lw - 1
	if dw < 10 {
		dw = 10
	}

	items := makeAgentItems(agents)
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

	s := &UtilityAgentScreen{
		agents:   agents,
		multisel: widgets.NewMultiSelect(items, lh, lw, msStyles),
		detail:   widgets.NewDetailPane(lh, dw, detailStyles),
		hasError: errorMsg != "",
		errorMsg: errorMsg,
		width:    width,
		height:   height,
		styles:   styles,
	}
	s.updateDetail()
	return s
}

// makeAgentItems converts a slice of agents into multi-select list items.
func makeAgentItems(agents []domain.Agent) []widgets.ListItem {
	items := make([]widgets.ListItem, len(agents))
	for i, a := range agents {
		label := a.Name
		if label == "" {
			label = a.Key
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
func (s *UtilityAgentScreen) Update(msg tea.Msg) tea.Cmd {
	if s.hasError {
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
func (s *UtilityAgentScreen) updateDetail() {
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

// View renders the utility-agent selection screen.
func (s *UtilityAgentScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Select Utility Agents")
	subtitle := s.styles.Subtitle.Width(s.width).Render(
		"These agents are not deployed automatically — include only those you need.",
	)
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	var content string
	switch {
	case s.hasError:
		content = s.styles.Error.Width(s.width).Render("Error: " + s.errorMsg)
	case len(s.agents) == 0:
		content = s.styles.Muted.Width(s.width).Render(
			"No utility agents are available. Check the tool configuration allow-list.",
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
func (s *UtilityAgentScreen) Done() bool { return s.done }

// Back reports whether the user cancelled.
func (s *UtilityAgentScreen) Back() bool { return s.back }

// SelectedIDs returns the keys of the selected utility agents in declaration order.
func (s *UtilityAgentScreen) SelectedIDs() []string {
	return s.multisel.SelectedIDs()
}

// Reset clears Done and Back flags.
func (s *UtilityAgentScreen) Reset() {
	s.done = false
	s.back = false
	s.multisel.Reset()
}

// Resize updates the screen dimensions.
func (s *UtilityAgentScreen) Resize(width, height int) {
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

// uaListHeight calculates the available height for the utility-agent list.
func uaListHeight(totalHeight int) int {
	// title + subtitle + border + border + help = 5 rows reserved
	h := totalHeight - 6
	if h < 3 {
		return 3
	}
	return h
}

// uaListWidth returns the list column width (left two-thirds of the terminal).
func uaListWidth(totalWidth int) int {
	w := totalWidth * 2 / 3
	if w < 30 {
		return totalWidth
	}
	return w
}

// FormatAgentKey formats an agent key for display (unexported helper used by tests via
// the public makeAgentItems path; kept here for co-location).
func formatAgentLabel(a domain.Agent) string {
	if a.Name != "" {
		return a.Name
	}
	return fmt.Sprintf("[%s]", a.Key)
}
