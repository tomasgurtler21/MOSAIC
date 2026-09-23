package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// testGHCPModeOptions lists the two choices for the test GHCP mode screen in
// cursor order.
var testGHCPModeOptions = []string{
	"blanket",
	"allowlist",
}

// TestGHCPModeScreen presents the two GHCP CLI permission-mode choices to the
// user in test mode. It is shown only when ghcp-cli is among the selected test
// harnesses. Follows the GHCPCLIModeScreen pattern.
//
// Navigation contract:
//   - Enter on "Blanket"   -> Done() == true, Value() == "blanket".
//   - Enter on "Allowlist" -> Done() == true, Value() == "allowlist".
//   - Esc                  -> Back() == true.
type TestGHCPModeScreen struct {
	cursor int
	done   bool
	back   bool
	width  int
	height int
	styles Styles
}

// NewTestGHCPModeScreen creates the GHCP permission mode picker for test mode.
func NewTestGHCPModeScreen(width, height int, styles Styles) *TestGHCPModeScreen {
	return &TestGHCPModeScreen{
		cursor: 0, // blanket is selected by default
		width:  width,
		height: height,
		styles: styles,
	}
}

// Update processes key messages for the GHCP mode selection screen.
func (s *TestGHCPModeScreen) Update(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch keyMsg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(testGHCPModeOptions)-1 {
			s.cursor++
		}
	case "enter":
		s.done = true
	case "esc":
		s.back = true
	}
	return nil
}

// View renders the GHCP mode selection screen.
func (s *TestGHCPModeScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("GHCP CLI Permission Mode")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	subtitle := s.styles.Subtitle.Width(s.width).Render(
		"Select the permission strategy for ghcp-cli test runs.",
	)

	optionLabels := map[string]string{
		"blanket":   "Blanket — grant all tool, path, and URL permissions (--yolo)",
		"allowlist": "Partial Allowlist — restrict to per-tool --allow-tool entries from the agent's deployed tools frontmatter",
	}

	var optionsBuilder strings.Builder
	for i, choice := range testGHCPModeOptions {
		label := optionLabels[choice]
		if i == s.cursor {
			optionsBuilder.WriteString("▶ " + s.styles.Selected.Render(label) + "\n")
		} else {
			optionsBuilder.WriteString("  " + s.styles.Body.Render(label) + "\n")
		}
	}

	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc back  ctrl+c quit")
	return strings.Join([]string{title, subtitle, border, optionsBuilder.String(), border, help}, "\n")
}

// Done reports whether the user confirmed a mode choice.
func (s *TestGHCPModeScreen) Done() bool { return s.done }

// Back reports whether the user pressed Esc.
func (s *TestGHCPModeScreen) Back() bool { return s.back }

// Value returns the selected permission mode: "blanket" or "allowlist".
// Only valid when Done() is true.
func (s *TestGHCPModeScreen) Value() string {
	if s.cursor >= 0 && s.cursor < len(testGHCPModeOptions) {
		return testGHCPModeOptions[s.cursor]
	}
	return "blanket"
}

// Reset clears the Done and Back flags and resets the cursor.
func (s *TestGHCPModeScreen) Reset() {
	s.done = false
	s.back = false
	s.cursor = 0
}

// Resize updates the screen dimensions.
func (s *TestGHCPModeScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
