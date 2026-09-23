package screens

import (
	"strings"

	commonharness "mosaic-common/harness"
	tea "github.com/charmbracelet/bubbletea"
	"mosaic-common/tui/widgets"
	"mosaic-run/internal/harness"
)

// TestHarnessScreen presents a multi-select list of CLI harnesses for the user
// to choose which harnesses to test against. Uses harness.CLISelections() as
// the selection set. When ghcp-cli is among the selected harnesses, the caller
// should present the TestGHCPModeScreen afterward.
//
// Navigation contract:
//   - Space toggles the item under the cursor.
//   - Enter confirms the selection -> Done() == true.
//   - Esc -> Back() == true.
type TestHarnessScreen struct {
	multiselect *widgets.MultiSelect
	width       int
	height      int
	styles      Styles
}

// NewTestHarnessScreen creates the harness multi-select screen.
// Populates the selection set from harness.CLISelections().
func NewTestHarnessScreen(width, height int, styles Styles) *TestHarnessScreen {
	sels := harness.CLISelections()
	items := make([]widgets.ListItem, len(sels))
	for i, s := range sels {
		items[i] = widgets.ListItem{
			ID:    s.ID,
			Label: s.Label,
		}
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
	contentH := height - 6
	if contentH < 1 {
		contentH = 1
	}
	ms := widgets.NewMultiSelect(items, contentH, width, msStyles)
	return &TestHarnessScreen{
		multiselect: ms,
		width:       width,
		height:      height,
		styles:      styles,
	}
}

// Update processes a key message and delegates to the multi-select widget.
func (s *TestHarnessScreen) Update(msg tea.Msg) tea.Cmd {
	return s.multiselect.Update(msg)
}

// View renders the harness multi-select screen.
func (s *TestHarnessScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Select Test Harnesses")
	subtitle := s.styles.Subtitle.Width(s.width).Render("Choose one or more harnesses to test against.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	listView := s.multiselect.View()
	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  space toggle  enter confirm  esc back  ctrl+c quit")
	return strings.Join([]string{title, subtitle, border, listView, border, help}, "\n")
}

// Done reports whether the user confirmed the harness selection.
func (s *TestHarnessScreen) Done() bool { return s.multiselect.Done() }

// Back reports whether the user pressed Esc.
func (s *TestHarnessScreen) Back() bool { return s.multiselect.Back() }

// SelectedHarnesses returns the IDs of all toggled-on harnesses, in declaration
// order. Only valid when Done() is true.
func (s *TestHarnessScreen) SelectedHarnesses() []string {
	return s.multiselect.SelectedIDs()
}

// HasGHCPCLI reports whether ghcp-cli is among the selected harnesses.
// Convenience method for the caller to decide whether to show TestGHCPModeScreen.
// Only meaningful when Done() is true.
func (s *TestHarnessScreen) HasGHCPCLI() bool {
	for _, id := range s.multiselect.SelectedIDs() {
		if id == commonharness.HarnessIDGHCPCLI {
			return true
		}
	}
	return false
}

// Reset clears the done and back flags.
func (s *TestHarnessScreen) Reset() { s.multiselect.Reset() }

// Resize updates the screen dimensions and reflows the list.
func (s *TestHarnessScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	contentH := height - 6
	if contentH < 1 {
		contentH = 1
	}
	s.multiselect.Resize(contentH, width)
}
