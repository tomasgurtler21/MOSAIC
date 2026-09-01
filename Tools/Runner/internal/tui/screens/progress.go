package screens

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ProgressRow represents a single completed or in-progress step shown on the progress screen.
type ProgressRow struct {
	AgentInstance string
	Phase         string
	Stage         string
	Status        string // "running", "SUCCESS", "BLOCKED", etc.
	Elapsed       time.Duration
}

// tickMsg is sent periodically to update elapsed time.
type tickMsg time.Time

// ProgressScreen shows live per-step progress during workflow execution.
//
// Key bindings:
//   - 's' -> requests graceful stop (GracefulStop() becomes true)
//   - 'a' -> requests artifact inspection (ArtifactView() becomes true)
//   - ctrl+c -> forced cancel (handled at root level)
type ProgressScreen struct {
	rows           []ProgressRow
	runningIdx     int // index of currently running row, or -1
	startTime      time.Time
	stopRequest    bool
	confirmPending bool
	artifactView   bool
	status         string
	statusErr      bool
	width          int
	height         int
	styles         Styles
}

// NewProgressScreen creates a new progress screen.
func NewProgressScreen(width, height int, styles Styles) *ProgressScreen {
	return &ProgressScreen{
		runningIdx: -1,
		startTime:  time.Now(),
		width:      width,
		height:     height,
		styles:     styles,
	}
}

// Init returns a tick command so the elapsed time updates every second.
func (s *ProgressScreen) Init() tea.Cmd {
	return tea.Every(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// AppendRow adds a new step row. Call when a new step begins.
func (s *ProgressScreen) AppendRow(row ProgressRow) {
	s.rows = append(s.rows, row)
	s.runningIdx = len(s.rows) - 1
}

// CompleteRow marks the current running row as complete with the given status.
func (s *ProgressScreen) CompleteRow(status string) {
	if s.runningIdx >= 0 && s.runningIdx < len(s.rows) {
		s.rows[s.runningIdx].Status = status
	}
	s.runningIdx = -1
}

// SetStatus sets the status message shown at the top of the screen.
func (s *ProgressScreen) SetStatus(msg string, isError bool) {
	s.status = msg
	s.statusErr = isError
}

// Update processes a key message.
func (s *ProgressScreen) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.confirmPending {
			switch msg.String() {
			case "y", "Y":
				s.stopRequest = true
			}
			s.confirmPending = false
		} else {
			switch msg.String() {
			case "s", "S":
				if !s.stopRequest {
					s.confirmPending = true
				}
			case "a", "A", "i", "I":
				s.artifactView = true
			}
		}
	case tickMsg:
		return tea.Every(time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}
	return nil
}

// View renders the progress screen.
func (s *ProgressScreen) View() string {
	elapsed := time.Since(s.startTime).Round(time.Second)
	titleText := fmt.Sprintf("Running  (%s elapsed)", elapsed)
	title := s.styles.Title.Width(s.width).Render(titleText)
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	var statusLine string
	if s.status != "" {
		if s.statusErr {
			statusLine = s.styles.Error.Width(s.width).Render(s.status)
		} else {
			statusLine = s.styles.Body.Width(s.width).Render(s.status)
		}
	} else {
		statusLine = strings.Repeat(" ", s.width)
	}

	// Rows section.
	var rowsBuilder strings.Builder
	contentH := s.height - 6
	if contentH < 1 {
		contentH = 1
	}
	start := 0
	if len(s.rows) > contentH {
		start = len(s.rows) - contentH
	}
	for i := start; i < len(s.rows); i++ {
		row := s.rows[i]
		indicator := "  "
		var line string
		if row.Status == "running" || (s.runningIdx == i) {
			indicator = "▶ "
			label := fmt.Sprintf("%s  phase=%s", row.AgentInstance, row.Phase)
			if row.Stage != "" {
				label += fmt.Sprintf(" stage=%s", row.Stage)
			}
			line = indicator + s.styles.Selected.Render(label)
		} else if row.Status == "SUCCESS" {
			label := fmt.Sprintf("%s  %s", row.AgentInstance, row.Status)
			line = indicator + s.styles.Success.Render(label)
		} else if row.Status == "" {
			label := fmt.Sprintf("%s  phase=%s", row.AgentInstance, row.Phase)
			line = indicator + s.styles.Body.Render(label)
		} else if row.Status == "BLOCKED" || row.Status == "CAPABILITY_EXCEEDED" {
			label := fmt.Sprintf("%s  %s", row.AgentInstance, row.Status)
			line = indicator + s.styles.Error.Render(label)
		} else {
			label := fmt.Sprintf("%s  %s", row.AgentInstance, row.Status)
			line = indicator + s.styles.Warning.Render(label)
		}
		if i > start {
			rowsBuilder.WriteByte('\n')
		}
		rowsBuilder.WriteString(line)
	}

	help := s.styles.Help.Width(s.width).Render("s graceful-stop  a artifact-view  ctrl+c force-quit")

	parts := []string{title, statusLine, border, rowsBuilder.String()}
	if s.confirmPending {
		confirmPrompt := s.styles.Warning.Width(s.width).Render("Stop after current step? (y/n)")
		parts = append(parts, confirmPrompt)
	} else if s.stopRequest {
		stopNotice := s.styles.Warning.Width(s.width).Render("Stopping after current step completes…")
		parts = append(parts, stopNotice)
	}
	parts = append(parts, border, help)
	return strings.Join(parts, "\n")
}

// GracefulStop reports whether the user requested a graceful stop.
func (s *ProgressScreen) GracefulStop() bool { return s.stopRequest }

// ConfirmPending reports whether the screen is currently showing the
// "stop after current step?" confirmation prompt (i.e. 's'/'S' was pressed
// and no response has been given yet). Distinct from GracefulStop(): this is
// true only in the intermediate state, before either an affirmative or a
// cancelling key is pressed.
func (s *ProgressScreen) ConfirmPending() bool { return s.confirmPending }

// ArtifactViewRequested reports whether the user requested artifact inspection.
func (s *ProgressScreen) ArtifactViewRequested() bool { return s.artifactView }

// ClearArtifactViewRequest resets the artifact view flag.
func (s *ProgressScreen) ClearArtifactViewRequest() { s.artifactView = false }

// Resize updates the screen dimensions.
func (s *ProgressScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
