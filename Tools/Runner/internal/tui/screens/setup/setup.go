package setup

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/widgets"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// HarnessSelectScreen
// ---------------------------------------------------------------------------

// HarnessSelectScreen lets the user select the AI harness adapter before the
// rest of the setup sequence proceeds.
//
// Navigation contract:
//   - Enter on a harness -> Done() == true, SelectedID() returns its ID.
//   - Esc -> Back() == true (quit; this is the first setup screen).
type HarnessSelectScreen struct {
	list        *widgets.List
	width       int
	height      int
	styles      screens.Styles
	toolVersion string
}

// NewHarnessSelectScreen creates the harness selection screen.
func NewHarnessSelectScreen(width, height int, styles screens.Styles) *HarnessSelectScreen {
	sels := harness.CLISelections()
	items := make([]widgets.ListItem, len(sels))
	for i, s := range sels {
		items[i] = widgets.ListItem{
			ID:    s.ID,
			Label: s.Label,
		}
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
	return &HarnessSelectScreen{
		list:   list,
		width:  width,
		height: height,
		styles: styles,
	}
}

// SetToolVersion sets the tool version string displayed in the screen title.
// Call before the first View() for the version to appear on entry.
func (s *HarnessSelectScreen) SetToolVersion(v string) {
	s.toolVersion = v
}

// Update processes a key message and delegates to the list widget.
func (s *HarnessSelectScreen) Update(msg tea.Msg) tea.Cmd {
	s.list.Update(msg)
	return nil
}

// View renders the harness selection screen.
func (s *HarnessSelectScreen) View() string {
	titleText := "Select Harness"
	if s.toolVersion != "" {
		titleText = "MOSAIC Runner v" + s.toolVersion + " -- Select Harness"
	}
	title := s.styles.Title.Width(s.width).Render(titleText)
	subtitle := s.styles.Subtitle.Width(s.width).Render("Choose the AI harness to use for this run.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	listView := s.list.View()
	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc quit  ctrl+c quit")
	return strings.Join([]string{title, subtitle, border, listView, border, help}, "\n")
}

// Done reports whether the user selected a harness.
func (s *HarnessSelectScreen) Done() bool { return s.list.Done() }

// Back reports whether the user pressed Esc.
func (s *HarnessSelectScreen) Back() bool { return s.list.Back() }

// SelectedID returns the selected harness ID. Only valid when Done() is true.
func (s *HarnessSelectScreen) SelectedID() string { return s.list.SelectedID() }

// Reset clears the done and back flags.
func (s *HarnessSelectScreen) Reset() { s.list.Reset() }

// Resize updates the screen dimensions and reflows the list.
func (s *HarnessSelectScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	contentH := height - 6
	if contentH < 1 {
		contentH = 1
	}
	s.list.Resize(contentH, width)
}

// RunTestsChoiceID is the sentinel harness ID placed at the top of the
// dev-mode harness selection screen to let the user enter the test flow.
// When the user selects this ID, the app transitions to the test catalog
// screen instead of the normal run setup flow.
const RunTestsChoiceID = "run-tests"

// NewHarnessSelectScreenDevMode creates the harness selection screen with an
// additional "Run Tests" option at the top. It is used in place of
// NewHarnessSelectScreen when DevMode is enabled.
func NewHarnessSelectScreenDevMode(width, height int, styles screens.Styles) *HarnessSelectScreen {
	sels := harness.CLISelections()
	items := make([]widgets.ListItem, 0, len(sels)+1)
	items = append(items, widgets.ListItem{
		ID:    RunTestsChoiceID,
		Label: "Run Tests (test mode)",
	})
	for _, s := range sels {
		items = append(items, widgets.ListItem{
			ID:    s.ID,
			Label: s.Label,
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
	return &HarnessSelectScreen{
		list:   list,
		width:  width,
		height: height,
		styles: styles,
	}
}
