package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
)

// DoneScreen shows the final run outcome after the session completes or is stopped.
//
// Navigation contract:
//   - 'q', Enter, or Esc -> Done() == true.
type DoneScreen struct {
	outcome domain.RunOutcome
	errMsg  string
	done    bool
	width   int
	height  int
	styles  Styles
}

// NewDoneScreen creates a done/error summary screen.
func NewDoneScreen(outcome domain.RunOutcome, errMsg string, width, height int, styles Styles) *DoneScreen {
	return &DoneScreen{
		outcome: outcome,
		errMsg:  errMsg,
		width:   width,
		height:  height,
		styles:  styles,
	}
}

// Update processes a key message.
func (s *DoneScreen) Update(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch keyMsg.String() {
	case "q", "enter", "esc":
		s.done = true
	}
	return nil
}

// View renders the completion/error summary screen.
func (s *DoneScreen) View() string {
	var lines []string

	if s.errMsg != "" {
		title := s.styles.Title.Width(s.width).Render("Run Failed")
		border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
		msg := s.styles.Error.Width(s.width).Render(s.errMsg)
		lines = []string{title, border, msg}
	} else {
		var titleText string
		var titleStyle = s.styles.Title
		switch s.outcome.Status {
		case domain.RunCompleted:
			titleText = "Run Completed"
			titleStyle = s.styles.Success
		case domain.RunStopped:
			titleText = "Run Stopped (resumable)"
			titleStyle = s.styles.Warning
		case domain.RunRefused:
			titleText = "Run Refused"
			titleStyle = s.styles.Error
		case domain.RunDeviationUnresolved:
			titleText = "Deviation Unresolved"
			titleStyle = s.styles.Error
		case domain.RunFailed:
			titleText = "Run Failed"
			titleStyle = s.styles.Error
		default:
			titleText = "Run Complete"
		}

		title := titleStyle.Width(s.width).Render(titleText)
		border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
		var msgStyle = s.styles.Body
		if s.outcome.Status != domain.RunCompleted {
			msgStyle = s.styles.Warning
		}
		msg := msgStyle.Width(s.width).Render(s.outcome.Message)
		lines = []string{title, border, msg}
	}

	help := s.styles.Help.Width(s.width).Render("q/enter/esc  exit")
	lines = append(lines, "", help)
	return strings.Join(lines, "\n")
}

// Done reports whether the user has dismissed the screen.
func (s *DoneScreen) Done() bool { return s.done }

// Resize updates the screen dimensions.
func (s *DoneScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
