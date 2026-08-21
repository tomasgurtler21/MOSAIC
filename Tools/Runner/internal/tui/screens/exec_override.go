package screens

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/widgets"
)

// ExecOverrideChoice classifies the user's response on the executable-override screen.
type ExecOverrideChoice string

const (
	// ExecOverrideChoiceRetry directs the runner to rebuild the harness adapter
	// with Path() and restart the run against the same run folder.
	ExecOverrideChoiceRetry ExecOverrideChoice = "retry"

	// ExecOverrideChoiceAbandon directs the runner to the normal error outcome
	// without retrying.
	ExecOverrideChoiceAbandon ExecOverrideChoice = "abandon"
)

// ExecOverrideScreen offers a recovery path when the selected harness's
// executable could not be launched. It names the harness and shows the path
// that was tried, then takes a replacement path.
//
// Navigation contract:
//   - Enter on a non-empty trimmed input -> Done() == true,
//     Choice() == ExecOverrideChoiceRetry, Path() == the trimmed input.
//   - Enter on an empty or whitespace-only input -> no transition; the screen
//     stays active. An empty override is never submitted, because "" means
//     "use the per-harness default" and the default is what just failed.
//   - Esc -> Back() == true, Choice() == ExecOverrideChoiceAbandon.
type ExecOverrideScreen struct {
	harnessID     string
	attemptedPath string
	attempt       int
	input         *widgets.TextInput
	width         int
	height        int
	styles        Styles
}

// NewExecOverrideScreen creates the screen. harnessID is the harness that
// failed (e.g. "ghcp-cli") and attemptedPath is the executable path that
// could not be launched; both are rendered so the user knows what to correct.
// attempt is the 1-based count of consecutive launch failures for this run,
// so a repeated failure can be shown as such rather than looking like the first.
func NewExecOverrideScreen(harnessID, attemptedPath string, attempt int, width, height int, styles Styles) *ExecOverrideScreen {
	inputStyles := widgets.TextInputStyles{
		Label:  styles.Subtitle,
		Input:  styles.Body,
		ErrMsg: styles.Error,
	}
	input := widgets.NewTextInput(
		"New executable path:",
		"/path/to/executable",
		width,
		inputStyles,
	)
	input.SetValidate(func(v string) error {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("path cannot be empty; the per-harness default was already tried")
		}
		return nil
	})
	return &ExecOverrideScreen{
		harnessID:     harnessID,
		attemptedPath: attemptedPath,
		attempt:       attempt,
		input:         input,
		width:         width,
		height:        height,
		styles:        styles,
	}
}

// Update processes a key message and delegates to the embedded text input.
func (s *ExecOverrideScreen) Update(msg tea.Msg) tea.Cmd {
	return s.input.Update(msg)
}

// View renders the executable override screen, naming the harness and the
// path that was tried and offering a text field for the replacement path.
func (s *ExecOverrideScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Executable Not Found")

	var subtitle string
	if s.attempt > 1 {
		subtitle = fmt.Sprintf("The path you supplied (%q) also failed to launch %s.", s.attemptedPath, s.harnessID)
	} else {
		subtitle = fmt.Sprintf("The %s executable could not be launched.", s.harnessID)
	}
	subtitleView := s.styles.Subtitle.Width(s.width).Render(subtitle)

	tried := s.styles.Body.Width(s.width).Render("Tried: " + s.attemptedPath)
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	inputView := s.input.View()
	help := s.styles.Help.Width(s.width).Render("enter confirm  esc abandon  ctrl+c quit")
	return strings.Join([]string{title, subtitleView, tried, border, inputView, border, help}, "\n")
}

// Done reports whether the user confirmed a non-empty executable path.
func (s *ExecOverrideScreen) Done() bool { return s.input.Done() }

// Back reports whether the user pressed Esc to abandon without retrying.
func (s *ExecOverrideScreen) Back() bool { return s.input.Back() }

// Choice returns the user's decision. Only valid when Done() or Back() is true.
//   - Done() → ExecOverrideChoiceRetry
//   - Back() → ExecOverrideChoiceAbandon
func (s *ExecOverrideScreen) Choice() ExecOverrideChoice {
	if s.input.Back() {
		return ExecOverrideChoiceAbandon
	}
	return ExecOverrideChoiceRetry
}

// Path returns the entered executable path, normalised (whitespace-trimmed,
// surrounding double quotes stripped).
// Only meaningful when Done() is true (Choice() == ExecOverrideChoiceRetry).
func (s *ExecOverrideScreen) Path() string { return normalizePath(s.input.Value()) }

// Reset clears the Done and Back flags so the screen may be re-entered.
func (s *ExecOverrideScreen) Reset() { s.input.Reset() }

// InputInit returns the command required to start cursor blinking.
// Returned by the root model when transitioning into this screen.
func (s *ExecOverrideScreen) InputInit() tea.Cmd { return s.input.Init() }

// Resize updates the screen dimensions and forwards the width to the input.
func (s *ExecOverrideScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.input.Resize(width)
}
