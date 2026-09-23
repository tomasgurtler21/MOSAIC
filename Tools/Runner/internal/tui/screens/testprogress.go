package screens

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// testProgressState identifies the state of an individual item in the progress display.
type testProgressState int

const (
	testStatePending testProgressState = iota
	testStateRunning
	testStatePass
	testStateFail
	testStateError
)

// testProgressItem is one row in the progress display: either the deploy step
// or a single (harness, workflow, mode) test invocation.
type testProgressItem struct {
	harness  string
	workflow string
	mode     string
	state    testProgressState
	isDeploy bool // true for the deploy row; harness/workflow/mode are empty
}

// TestProgressScreen shows live progress of the test orchestration run. It
// receives state transition calls from the TUI root model (which converts
// tea.Msg values from the orchestrator goroutine into method calls) and renders
// a simple line-by-line status list.
//
// Navigation contract:
//   - 'q' / Esc once all tests are done -> Done() == true.
//   - There is no Back() while tests are running; Esc is ignored until done.
type TestProgressScreen struct {
	items         []testProgressItem
	done          bool
	allDone       bool // true once TestAllDone is called
	width         int
	height        int
	styles        Styles
	resolvedPaths map[string]string
	harnessOrder  []string
}

// NewTestProgressScreen creates the live test progress display.
func NewTestProgressScreen(width, height int, styles Styles) *TestProgressScreen {
	return &TestProgressScreen{
		width:  width,
		height: height,
		styles: styles,
	}
}

// SetDeployRunning marks the deploy step as running.
// Call when the orchestrator's OnDeployStart fires.
func (s *TestProgressScreen) SetDeployRunning() {
	// Find or add the deploy item.
	for i := range s.items {
		if s.items[i].isDeploy {
			s.items[i].state = testStateRunning
			return
		}
	}
	s.items = append(s.items, testProgressItem{
		isDeploy: true,
		state:    testStateRunning,
	})
}

// SetDeployDone marks the deploy step as done (pass or fail).
// Call when the orchestrator's OnDeployDone fires.
func (s *TestProgressScreen) SetDeployDone(err error) {
	for i := range s.items {
		if s.items[i].isDeploy {
			if err != nil {
				s.items[i].state = testStateFail
			} else {
				s.items[i].state = testStatePass
			}
			return
		}
	}
}

// AddTestRunning adds a new test item for the given (harness, workflow, mode)
// and marks it as running. Call when the orchestrator's OnTestStart fires.
func (s *TestProgressScreen) AddTestRunning(harness, workflow, mode string) {
	s.items = append(s.items, testProgressItem{
		harness:  harness,
		workflow: workflow,
		mode:     mode,
		state:    testStateRunning,
	})
}

// SetTestDone marks the most recently added test item that matches the given
// (harness, workflow, mode) as done. Call when the orchestrator's OnTestDone fires.
// pass and isError are mutually exclusive; when isError is true, pass is ignored.
func (s *TestProgressScreen) SetTestDone(harness, workflow, mode string, pass bool, isError bool) {
	// Walk in reverse to find the most recent matching running item.
	for i := len(s.items) - 1; i >= 0; i-- {
		it := &s.items[i]
		if !it.isDeploy && it.harness == harness && it.workflow == workflow && it.mode == mode {
			switch {
			case isError:
				it.state = testStateError
			case pass:
				it.state = testStatePass
			default:
				it.state = testStateFail
			}
			return
		}
	}
}

// SetAllDone marks the progress run as complete. After this call, the screen
// accepts 'q' and Esc to proceed to Done().
func (s *TestProgressScreen) SetAllDone() {
	s.allDone = true
}

// Update processes key messages. Only 'q' and Esc after all tests have
// finished trigger Done(); all other keys (and any input while running) are
// ignored so the user cannot accidentally dismiss a live run.
func (s *TestProgressScreen) Update(msg tea.Msg) tea.Cmd {
	if !s.allDone {
		return nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch keyMsg.String() {
	case "q", "esc", "enter":
		s.done = true
	}
	return nil
}

// stateLabel returns the short status badge for a progress state.
func stateLabel(state testProgressState) string {
	switch state {
	case testStatePending:
		return "PENDING"
	case testStateRunning:
		return "RUNNING"
	case testStatePass:
		return "PASS"
	case testStateFail:
		return "FAIL"
	case testStateError:
		return "ERROR"
	default:
		return "?"
	}
}

// View renders the live progress display.
func (s *TestProgressScreen) View() string {
	var sb strings.Builder

	title := s.styles.Title.Width(s.width).Render("Test Progress")
	sb.WriteString(title)
	sb.WriteString("\n")

	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	sb.WriteString(border)
	sb.WriteString("\n")

	// Resolved binaries section (shown before progress items, when populated).
	if len(s.resolvedPaths) > 0 {
		sb.WriteString(s.styles.Muted.Render("  Resolved binaries:"))
		sb.WriteString("\n")
		for _, id := range s.harnessOrder {
			path := s.resolvedPaths[id]
			sb.WriteString(s.styles.Muted.Render(fmt.Sprintf("    %s: %s", id, path)))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	for _, item := range s.items {
		var line string
		badge := stateLabel(item.state)
		if item.isDeploy {
			line = fmt.Sprintf("  [%s] Deploy", badge)
		} else {
			line = fmt.Sprintf("  [%s] %s  %s  %s", badge, item.harness, item.workflow, item.mode)
		}
		var rendered string
		switch item.state {
		case testStatePass:
			rendered = s.styles.Success.Render(line)
		case testStateFail, testStateError:
			rendered = s.styles.Error.Render(line)
		case testStateRunning:
			rendered = s.styles.Selected.Render(line)
		default:
			rendered = s.styles.Muted.Render(line)
		}
		sb.WriteString(rendered)
		sb.WriteString("\n")
	}

	sb.WriteString(border)
	sb.WriteString("\n")

	var help string
	if s.allDone {
		help = s.styles.Help.Width(s.width).Render("q/esc/enter continue")
	} else {
		help = s.styles.Help.Width(s.width).Render("running tests…  ctrl+c quit")
	}
	sb.WriteString(help)

	return sb.String()
}

// AllDone reports whether all tests have completed (SetAllDone has been called).
// Unlike Done(), this becomes true when the orchestrator finishes, regardless
// of whether the user has dismissed the screen yet.
func (s *TestProgressScreen) AllDone() bool { return s.allDone }

// Done reports whether the user has dismissed the progress screen (only
// possible once all tests have completed).
func (s *TestProgressScreen) Done() bool { return s.done }

// Back always returns false. There is no back navigation from the progress screen.
func (s *TestProgressScreen) Back() bool { return false }

// SetResolvedPaths records the resolved harness binary paths and the display
// order. When paths is non-nil and non-empty, View() renders a
// "Resolved binaries:" section above the progress rows.
func (s *TestProgressScreen) SetResolvedPaths(paths map[string]string, harnessOrder []string) {
	s.resolvedPaths = paths
	s.harnessOrder = harnessOrder
}

// Reset clears all state.
func (s *TestProgressScreen) Reset() {
	s.items = nil
	s.done = false
	s.allDone = false
	s.resolvedPaths = nil
	s.harnessOrder = nil
}

// Resize updates the screen dimensions.
func (s *TestProgressScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
