package screens

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// Text shown by the stop-confirmation gate. The prompt is rendered inside a
// bordered banner rather than as a bare line, and each resolution leaves an
// acknowledgement in the status slot so neither outcome is silent.
const (
	confirmPromptLabel = "Stop after current step? (y/n)"
	confirmAckText     = "Stop confirmed - the run will finish the current step, then stop."
	cancelAckText      = "Stop cancelled - the run continues."
)

// ProgressScreen shows live per-step progress during workflow execution.
//
// Key bindings:
//   - 's' -> opens the graceful-stop confirmation gate (ConfirmPending())
//   - 'y' -> confirms the pending stop (GracefulStop() becomes true)
//   - 'n' / esc -> cancels the pending stop
//   - 'a' -> requests artifact inspection (ArtifactView() becomes true)
//   - ctrl+c -> forced cancel (handled at root level)
//
// While the gate is pending it is the only binding in effect: keys other than
// the confirm and cancel keys are ignored and leave it open. ctrl+c is handled
// by the root model ahead of screen delegation, so force-quit stays available
// and the user is never trapped in an unresolved confirmation.
type ProgressScreen struct {
	rows           []ProgressRow
	runningIdx     int // index of currently running row, or -1
	startTime      time.Time
	stopRequest    bool
	confirmPending bool
	gateEvents     []StopGateEvent
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
			// The gate is sticky: only an explicit confirm or cancel key
			// resolves it. Every other key is ignored so the prompt cannot be
			// dismissed by a stray press, and in particular does not fall
			// through to the artifact-view binding below.
			switch key := msg.String(); key {
			case "y", "Y":
				s.stopRequest = true
				s.confirmPending = false
				s.SetStatus(confirmAckText, false)
				s.gateEvents = append(s.gateEvents,
					StopGateEvent{Kind: StopGateResolved, Key: key, Confirmed: true})
			case "n", "N", "esc":
				s.confirmPending = false
				s.SetStatus(cancelAckText, false)
				s.gateEvents = append(s.gateEvents,
					StopGateEvent{Kind: StopGateResolved, Key: key})
			}
		} else {
			switch key := msg.String(); key {
			case "s", "S":
				if !s.stopRequest {
					s.confirmPending = true
					s.gateEvents = append(s.gateEvents,
						StopGateEvent{Kind: StopGateEntered, Key: key})
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
		parts = append(parts, s.confirmBanner())
	} else if s.stopRequest {
		stopNotice := s.styles.Warning.Width(s.width).Render("Stopping after current step completes…")
		parts = append(parts, stopNotice)
	}
	parts = append(parts, border, help)
	return strings.Join(parts, "\n")
}

// confirmBanner renders the pending stop prompt as a bordered box, so it reads
// as a distinct interruption rather than as one more line in the agent-row
// stack it sits below.
//
// The body is styled two columns narrower than the screen because the border
// adds one column on each side; the finished box therefore totals exactly the
// screen width, like every other line in the view, and does not wrap in a
// terminal of that width.
func (s *ProgressScreen) confirmBanner() string {
	bodyWidth := s.width - 2
	if bodyWidth < 1 {
		bodyWidth = 1
	}
	return s.styles.Warning.
		Width(bodyWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.styles.Warning.GetForeground()).
		Render(confirmPromptLabel)
}

// GracefulStop reports whether the user requested a graceful stop.
func (s *ProgressScreen) GracefulStop() bool { return s.stopRequest }

// ConfirmPending reports whether the screen is currently showing the
// "stop after current step?" confirmation prompt (i.e. 's'/'S' was pressed
// and no response has been given yet). Distinct from GracefulStop(): this is
// true only in the intermediate state, before either an affirmative or a
// cancelling key is pressed.
func (s *ProgressScreen) ConfirmPending() bool { return s.confirmPending }

// ResetStopState clears the graceful-stop latch, the pending confirmation, any
// gate transition recorded but not yet drained, and the status slot, so a reused
// screen presents a live stop affordance and no carried-over notice on a resumed
// run.
//
// Undrained transitions are discarded because they belong to the run that just
// ended; logging one against the resumed run would misattribute it.
//
// The status slot is included because the two restart paths that reuse the
// screen would otherwise carry the previous run's stop acknowledgement into the
// resumed run, asserting a stop that has just been cancelled. Any status held
// at a restart boundary is stale by definition.
func (s *ProgressScreen) ResetStopState() {
	s.stopRequest = false
	s.confirmPending = false
	s.gateEvents = nil
	s.status = ""
	s.statusErr = false
}

// StopGateEventKind names a transition of the graceful-stop confirmation gate.
//
// A closed two-value set rather than a string, so an unhandled kind is a
// compile-time concern and the "an ignored key is not a resolution" rule is
// structural: there is no third value to misuse.
type StopGateEventKind int

const (
	// StopGateEntered is the gate opening: 's'/'S' on an unstopped run.
	StopGateEntered StopGateEventKind = iota

	// StopGateResolved is the gate closing on an explicit answer, confirming
	// or cancelling the stop.
	StopGateResolved
)

// StopGateEvent records one transition of the graceful-stop confirmation gate,
// for the root model to log. Only genuine transitions are recorded: entering
// the gate, and resolving it. A key ignored while the gate is pending records
// nothing, by design -- the gate stays pending, so an entry would misname what
// happened, and the volume is unbounded (a held key or a terminal escape
// sequence arrives as a burst of key messages).
type StopGateEvent struct {
	// Kind is which transition occurred.
	Kind StopGateEventKind

	// Key is the resolving key exactly as bubbletea rendered it ("s", "y",
	// "esc"), recorded verbatim so the log states which key the user actually
	// pressed and 'y' stays distinguishable from 'Y'.
	Key string

	// Confirmed is true for a 'y'/'Y' resolution and false for 'n'/'N'/'esc'.
	// Meaningful only when Kind == StopGateResolved.
	Confirmed bool
}

// TakeStopGateEvents returns the gate transitions recorded since the last call
// and clears them. At most one event is recorded per Update, and only for a
// genuine transition: entering the gate, and resolving it. Keys ignored while
// the gate is pending record nothing. Returns nil or empty when there is
// nothing pending.
//
// Drain semantics -- rather than a peek accessor -- so that each transition is
// reported exactly once and cannot be logged twice by the repeated polling the
// root model performs on every repaint.
func (s *ProgressScreen) TakeStopGateEvents() []StopGateEvent {
	events := s.gateEvents
	s.gateEvents = nil
	return events
}

// ArtifactViewRequested reports whether the user requested artifact inspection.
func (s *ProgressScreen) ArtifactViewRequested() bool { return s.artifactView }

// ClearArtifactViewRequest resets the artifact view flag.
func (s *ProgressScreen) ClearArtifactViewRequest() { s.artifactView = false }

// Resize updates the screen dimensions.
func (s *ProgressScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
