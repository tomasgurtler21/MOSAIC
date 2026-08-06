package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/widgets"
)

// SeedInputScreen prompts the user for an optional seed-input path (a file or a
// directory) to copy into the run folder before the first dispatch.
//
// Navigation contract:
//   - Enter -> Done() == true. SeedInput() returns the trimmed entered value,
//     which may be the empty string: the field is optional and blank is a
//     legal confirmation.
//   - Esc -> Back() == true.
//
// The screen performs no filesystem validation. Existence, collision, and
// destination refusals are decided at session start.
type SeedInputScreen struct {
	input  *widgets.TextInput
	width  int
	height int
	styles Styles
}

// NewSeedInputScreen creates the seed-input entry screen.
func NewSeedInputScreen(width, height int, styles Styles) *SeedInputScreen {
	inputStyles := widgets.TextInputStyles{
		Label:  styles.Subtitle,
		Input:  styles.Body,
		ErrMsg: styles.Error,
	}
	input := widgets.NewTextInput(
		"Seed input path (optional):",
		"/path/to/seed/file-or-directory",
		width,
		inputStyles,
	)
	// No SetValidate call: blank is a legal confirmation for this optional field.
	return &SeedInputScreen{
		input:  input,
		width:  width,
		height: height,
		styles: styles,
	}
}

// Update processes a key message and delegates to the embedded text input.
func (s *SeedInputScreen) Update(msg tea.Msg) tea.Cmd {
	return s.input.Update(msg)
}

// View renders the seed-input screen. Returns a non-empty string containing
// the screen title, subtitle, input field, and help line.
func (s *SeedInputScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Seed Input")
	subtitle := s.styles.Subtitle.Width(s.width).Render("Enter an optional path to a file or directory to copy into the run folder before the first dispatch. Leave blank to start with no seeded artifacts.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	inputView := s.input.View()
	help := s.styles.Help.Width(s.width).Render("enter confirm  esc back  ctrl+c quit")
	return strings.Join([]string{title, subtitle, border, inputView, border, help}, "\n")
}

// Done reports whether the user pressed Enter to confirm the entered path
// (including an empty path, which is a legal confirmation for this optional field).
func (s *SeedInputScreen) Done() bool { return s.input.Done() }

// Back reports whether the user pressed Esc.
func (s *SeedInputScreen) Back() bool { return s.input.Back() }

// SeedInput returns the entered path, whitespace-trimmed. Empty string means the
// user chose not to seed. Only meaningful when Done() is true.
func (s *SeedInputScreen) SeedInput() string { return strings.TrimSpace(s.input.Value()) }

// Reset clears both the Done and Back flags before the screen is re-entered.
func (s *SeedInputScreen) Reset() { s.input.Reset() }

// InputInit returns the command required to start cursor blinking.
// Returned by the root model when transitioning into this screen.
func (s *SeedInputScreen) InputInit() tea.Cmd { return s.input.Init() }

// Resize updates the screen dimensions and forwards the width to the text input.
func (s *SeedInputScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.input.Resize(width)
}
