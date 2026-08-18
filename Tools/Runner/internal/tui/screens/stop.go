package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// StopChoice classifies the user's response on the stop recovery screen.
type StopChoice string

const (
	// StopChoiceRetry directs the runner to re-invoke the routing consultant
	// with the same ConsultationRequest, for when the user has fixed something
	// externally and wants the run to continue.
	StopChoiceRetry StopChoice = "retry"

	// StopChoiceManualDispatch directs the runner to invoke the ManualResolver
	// for a single routing decision, after which the run continues normally
	// with the configured resolver.
	StopChoiceManualDispatch StopChoice = "manual-dispatch"
)

// stopOptions lists the available recovery actions in cursor order.
var stopOptions = []StopChoice{
	StopChoiceRetry,
	StopChoiceManualDispatch,
}

// StopScreen presents the stop reason and offers recovery actions when the
// routing consultant has stopped the run.
//
// Navigation contract:
//   - Enter on "Retry"           -> Done() == true, Choice() == StopChoiceRetry.
//   - Enter on "Manual dispatch" -> Done() == true, Choice() == StopChoiceManualDispatch.
//   - Esc                        -> Back() == true.
type StopScreen struct {
	reason string
	cursor int
	done   bool
	back   bool
	width  int
	height int
	styles Styles
}

// NewStopScreen creates a stop recovery screen displaying the given stop reason.
func NewStopScreen(reason string, width, height int, styles Styles) *StopScreen {
	return &StopScreen{
		reason: reason,
		cursor: 0, // Retry is selected by default
		width:  width,
		height: height,
		styles: styles,
	}
}

// Update processes key messages for the stop recovery screen.
func (s *StopScreen) Update(msg tea.Msg) tea.Cmd {
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
		if s.cursor < len(stopOptions)-1 {
			s.cursor++
		}
	case "enter":
		s.done = true
	case "esc":
		s.back = true
	}
	return nil
}

// View renders the stop recovery screen showing the reason and recovery options.
func (s *StopScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Run Stopped")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	reason := s.styles.Body.Width(s.width).Render("Reason: " + s.reason)

	optionLabels := map[StopChoice]string{
		StopChoiceRetry:          "Retry — re-invoke the routing decision with the same state",
		StopChoiceManualDispatch: "Manual dispatch — take over as resolver for this one decision",
	}

	var optionsBuilder strings.Builder
	optionsBuilder.WriteString(s.styles.Body.Width(s.width).Render("Recovery options:") + "\n")
	for i, choice := range stopOptions {
		label := optionLabels[choice]
		if i == s.cursor {
			optionsBuilder.WriteString("▶ " + s.styles.Selected.Render(label) + "\n")
		} else {
			optionsBuilder.WriteString("  " + s.styles.Body.Render(label) + "\n")
		}
	}

	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc quit  ctrl+c quit")
	return strings.Join([]string{title, border, reason, "", optionsBuilder.String(), border, help}, "\n")
}

// Done reports whether the user confirmed a recovery choice.
func (s *StopScreen) Done() bool { return s.done }

// Back reports whether the user pressed Esc.
func (s *StopScreen) Back() bool { return s.back }

// Choice returns the selected recovery action. Only valid when Done() is true.
func (s *StopScreen) Choice() StopChoice {
	if s.cursor >= 0 && s.cursor < len(stopOptions) {
		return stopOptions[s.cursor]
	}
	return ""
}

// Resize updates the screen dimensions.
func (s *StopScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
