package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/pathutil"
	"mosaic-common/tui/widgets"
)

// SourceScreen prompts the user to enter a path to a logs root or single run folder.
//
// Navigation contract:
//   - Enter on a non-empty path -> Done() == true, Path() returns the path.
//   - Esc -> Back() == true.
type SourceScreen struct {
	input  *widgets.TextInput
	width  int
	height int
	styles Styles
}

// NewSourceScreen creates the log-source path entry screen.
func NewSourceScreen(width, height int, styles Styles) *SourceScreen {
	inputStyles := widgets.TextInputStyles{
		Label:  styles.Subtitle,
		Input:  styles.Body,
		ErrMsg: styles.Error,
	}
	input := widgets.NewTextInput(
		"Log source path:",
		"/path/to/OrchestrationLogs  or  /path/to/run-folder",
		width,
		inputStyles,
	)
	input.SetValidate(validateNonEmpty)
	return &SourceScreen{
		input:  input,
		width:  width,
		height: height,
		styles: styles,
	}
}

func validateNonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return errEmptyPath
	}
	return nil
}

// errEmptyPath is a sentinel error for the empty path validation.
var errEmptyPath = errString("path cannot be empty")

type errString string

func (e errString) Error() string { return string(e) }

// ID implements Screen.
func (s *SourceScreen) ID() ScreenID { return ScreenSource }

// Init returns the command required to start cursor blinking.
func (s *SourceScreen) Init() tea.Cmd { return s.input.Init() }

// Update processes a key message and delegates to the text input widget.
func (s *SourceScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	cmd := s.input.Update(msg)
	return s, cmd
}

// View renders the source entry screen.
func (s *SourceScreen) View(width, height int) string {
	s.width = width
	s.height = height
	s.input.Resize(width)

	sep := s.styles.Border.Width(width).Render(strings.Repeat("─", width))
	title := s.styles.Title.Width(width).Render("Log Analyzer — Select Source")
	subtitle := s.styles.Subtitle.Width(width).Render("Enter the path to your logs root or a single run folder.")
	guidance := s.styles.Muted.Width(width).Render("Accepts: OrchestrationLogs/ directory  or  a single run folder (e.g. 20260101T120000Z-abcd)")
	help := s.styles.Help.Width(width).Render("enter confirm  esc skip  ctrl+c quit")

	return strings.Join([]string{title, subtitle, sep, "", guidance, s.input.View(), "", sep, help}, "\n")
}

// Help returns the key hints string for this screen.
func (s *SourceScreen) Help() string {
	return "enter confirm  esc skip  ctrl+c quit"
}

// Done reports whether the user confirmed a valid path.
func (s *SourceScreen) Done() bool { return s.input.Done() }

// Back reports whether the user pressed Esc.
func (s *SourceScreen) Back() bool { return s.input.Back() }

// Path returns the entered path, normalized. Only valid when Done() is true.
func (s *SourceScreen) Path() string { return pathutil.NormalizeInput(s.input.Value()) }

// Reset clears the done and back flags.
func (s *SourceScreen) Reset() { s.input.Reset() }
