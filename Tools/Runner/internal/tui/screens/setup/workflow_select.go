package setup

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/widgets"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// WorkflowSelectScreen
// ---------------------------------------------------------------------------

// WorkflowSelectScreen lets the user select a workflow from enumerated regions.
//
// Navigation contract:
//   - Enter on a workflow -> Done() == true, SelectedID() returns the workflow ID.
//   - Esc -> Back() == true.
type WorkflowSelectScreen struct {
	list   *widgets.List
	width  int
	height int
	styles screens.Styles
}

// NewWorkflowSelectScreen creates the workflow selection screen.
func NewWorkflowSelectScreen(workflows []domain.WorkflowRegion, width, height int, styles screens.Styles) *WorkflowSelectScreen {
	items := make([]widgets.ListItem, len(workflows))
	for i, wf := range workflows {
		items[i] = widgets.ListItem{
			ID:     string(wf.Info.ID),
			Label:  string(wf.Info.ID),
			Detail: fmt.Sprintf("version: %s", wf.Info.Version),
		}
	}
	listStyles := widgets.ListStyles{
		Normal:   styles.Body,
		Selected: styles.Selected,
		Disabled: styles.Muted,
		Cursor:   "▶",
	}
	contentH := height - 6 // reserve title + border + help
	if contentH < 1 {
		contentH = 1
	}
	list := widgets.NewList(items, contentH, width, listStyles)
	return &WorkflowSelectScreen{
		list:   list,
		width:  width,
		height: height,
		styles: styles,
	}
}

// Update processes a key message.
func (s *WorkflowSelectScreen) Update(msg tea.Msg) tea.Cmd {
	s.list.Update(msg)
	return nil
}

// View renders the workflow selection screen.
func (s *WorkflowSelectScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Select Workflow")
	subtitle := s.styles.Subtitle.Width(s.width).Render("Choose the workflow to run.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	listView := s.list.View()
	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc back  ctrl+c quit")
	return strings.Join([]string{title, subtitle, border, listView, border, help}, "\n")
}

// Done reports whether the user selected a workflow.
func (s *WorkflowSelectScreen) Done() bool { return s.list.Done() }

// Back reports whether the user pressed Esc.
func (s *WorkflowSelectScreen) Back() bool { return s.list.Back() }

// SelectedID returns the selected workflow ID. Only valid when Done() is true.
func (s *WorkflowSelectScreen) SelectedID() string { return s.list.SelectedID() }

// Reset clears the done and back flags.
func (s *WorkflowSelectScreen) Reset() { s.list.Reset() }

// Resize updates the screen dimensions.
func (s *WorkflowSelectScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	contentH := height - 6
	if contentH < 1 {
		contentH = 1
	}
	s.list.Resize(contentH, width)
}
