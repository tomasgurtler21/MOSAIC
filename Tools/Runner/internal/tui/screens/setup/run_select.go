package setup

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/widgets"
	"mosaic-run/internal/runselect"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// RunSelectScreen
// ---------------------------------------------------------------------------

// NewRunSentinelID is the list-item ID used for the synthetic "Start new run"
// entry. It is deliberately not a valid run_id (contains no timestamp or hex
// suffix) so it can never collide with a real run_id. Equal to
// runselect.NewRunChoiceID.
const NewRunSentinelID = runselect.NewRunChoiceID

// RunSelectScreen lets the user select a resumable run or start a new one.
//
// Navigation contract:
//   - Enter on a selectable choice -> Done() == true, SelectedChoiceID() returns its ID.
//   - Enter on the "Start new run" item -> Done() == true, IsNewRun() == true.
//   - Esc -> Back() == true.
type RunSelectScreen struct {
	list        *widgets.List
	choices     []runselect.Choice
	width       int
	height      int
	styles      screens.Styles
	toolVersion string
}

// NewRunSelectScreen creates the run selection screen from a selection
// question.
//
// The list is: the "Start a new run" item first, then every selectable
// choice in question order, then every non-selectable choice, each rendered
// with the reason it cannot be resumed. Non-selectable items are skipped by
// cursor movement and cannot be activated.
//
// question.Choices may contain zero selectable entries; the screen is still
// valid and shows only "Start a new run" plus any unresumable entries.
func NewRunSelectScreen(question runselect.Question, width, height int, styles screens.Styles) *RunSelectScreen {
	var selectable []runselect.Choice
	var unresumable []runselect.Choice
	for _, c := range question.Choices {
		switch c.Kind {
		case runselect.ChoiceResume:
			selectable = append(selectable, c)
		case runselect.ChoiceUnresumable:
			unresumable = append(unresumable, c)
		}
	}

	items := make([]widgets.ListItem, 0, len(selectable)+1)

	// Prepend the "Start new run" entry.
	items = append(items, widgets.ListItem{
		ID:    NewRunSentinelID,
		Label: "Start a new run",
	})

	// Append each resumable choice.
	for _, c := range selectable {
		label := c.Run.RunID
		detail := ""
		if c.Run.Workflow != "" {
			detail += c.Run.Workflow
		}
		if c.Run.Task != "" {
			if detail != "" {
				detail += " — "
			}
			detail += c.Run.Task
		}
		if !c.Run.LastUpdated.IsZero() {
			if detail != "" {
				detail += "  "
			}
			detail += c.Run.LastUpdated.Format("2006-01-02 15:04:05 UTC")
		}
		items = append(items, widgets.ListItem{
			ID:     c.ID,
			Label:  label,
			Detail: detail,
		})
	}

	// Append each unresumable choice, disabled, with its reason shown.
	for _, c := range unresumable {
		why := c.Reason.Description()
		if c.Detail != "" {
			why += ": " + c.Detail
		}
		items = append(items, widgets.ListItem{
			ID:             c.ID,
			Label:          c.Run.RunID,
			Disabled:       true,
			DisabledReason: why,
		})
	}

	listStyles := widgets.ListStyles{
		Normal:   styles.Body,
		Selected: styles.Selected,
		Disabled: styles.Muted,
		Cursor:   "▶",
	}
	contentH := height - 6
	if contentH < 1 {
		contentH = 1
	}
	list := widgets.NewList(items, contentH, width, listStyles)

	return &RunSelectScreen{
		list:    list,
		choices: question.Choices,
		width:   width,
		height:  height,
		styles:  styles,
	}
}

// SetToolVersion sets the tool version string displayed in the screen title.
// Call before the first View() for the version to appear on entry.
func (s *RunSelectScreen) SetToolVersion(v string) {
	s.toolVersion = v
}

// Update processes a key message and delegates to the list widget.
func (s *RunSelectScreen) Update(msg tea.Msg) tea.Cmd {
	s.list.Update(msg)
	return nil
}

// View renders the run selection screen with title, candidate list, and help bar.
func (s *RunSelectScreen) View() string {
	titleText := "Select Run"
	if s.toolVersion != "" {
		titleText = "MOSAIC Runner v" + s.toolVersion + " -- Select Run"
	}
	title := s.styles.Title.Width(s.width).Render(titleText)
	subtitle := s.styles.Subtitle.Width(s.width).Render("Choose a resumable run or start a new one.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	listView := s.list.View()
	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc quit  ctrl+c quit")
	return strings.Join([]string{title, subtitle, border, listView, border, help}, "\n")
}

// Done reports whether the user selected an item.
func (s *RunSelectScreen) Done() bool { return s.list.Done() }

// Back reports whether the user pressed Esc.
func (s *RunSelectScreen) Back() bool { return s.list.Back() }

// SelectedChoiceID returns the ID of the activated choice. Valid only when
// Done() is true. Equals NewRunSentinelID when the user chose a new run.
func (s *RunSelectScreen) SelectedChoiceID() string { return s.list.SelectedID() }

// IsNewRun reports whether the user chose the "Start new run" option.
// Only valid when Done() == true.
func (s *RunSelectScreen) IsNewRun() bool {
	return s.list.SelectedID() == NewRunSentinelID
}

// Reset clears the done and back flags.
func (s *RunSelectScreen) Reset() { s.list.Reset() }

// Resize updates the screen dimensions and reflows the list.
func (s *RunSelectScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	contentH := height - 6
	if contentH < 1 {
		contentH = 1
	}
	s.list.Resize(contentH, width)
}
