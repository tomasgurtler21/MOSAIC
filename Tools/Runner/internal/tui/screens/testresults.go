package screens

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"mosaic-run/internal/testcheck"
	"mosaic-run/internal/testrun"
)

// TestResultsScreen displays the final test summary: per-harness pass/fail
// counts and first-mismatch details for any failures. The user presses q/esc
// to proceed to Done().
//
// Navigation contract:
//   - 'q' / Esc / Enter -> Done() == true.
//   - Back() is always false (there is no back navigation from the results screen).
type TestResultsScreen struct {
	summary *testrun.TestSummary
	done    bool
	width   int
	height  int
	styles  Styles
}

// NewTestResultsScreen creates the results summary display.
func NewTestResultsScreen(width, height int, styles Styles, summary *testrun.TestSummary) *TestResultsScreen {
	return &TestResultsScreen{
		summary: summary,
		width:   width,
		height:  height,
		styles:  styles,
	}
}

// Update processes key messages. 'q', Esc, and Enter set Done.
func (s *TestResultsScreen) Update(msg tea.Msg) tea.Cmd {
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

// View renders the results summary.
func (s *TestResultsScreen) View() string {
	if s.summary == nil {
		return s.styles.Error.Render("No test results available.")
	}

	var sb strings.Builder

	title := s.styles.Title.Width(s.width).Render("Test Results")
	sb.WriteString(title)
	sb.WriteString("\n")

	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	sb.WriteString(border)
	sb.WriteString("\n")

	// Deploy error short-circuits: no tests were run.
	if s.summary.DeployError != nil {
		errLine := s.styles.Error.Render(fmt.Sprintf("  Deploy failed: %v", s.summary.DeployError))
		sb.WriteString(errLine)
		sb.WriteString("\n")
		sb.WriteString(border)
		sb.WriteString("\n")
		// Show resolved binaries when available (resolution succeeded but a later
		// step failed; the fallback summary carries ResolvedPaths per AC4.11).
		if len(s.summary.ResolvedPaths) > 0 {
			sb.WriteString(s.styles.Subtitle.Render("  Resolved binaries:"))
			sb.WriteString("\n")
			keys := sortedKeys(s.summary.ResolvedPaths)
			for _, id := range keys {
				sb.WriteString(s.styles.Muted.Render(fmt.Sprintf("    %s: %s", id, s.summary.ResolvedPaths[id])))
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
		sb.WriteString(s.styles.Help.Width(s.width).Render("q/esc/enter quit"))
		return sb.String()
	}

	// Overall summary line.
	overall := fmt.Sprintf("  Total: %d pass  %d fail  %d error",
		s.summary.TotalPass, s.summary.TotalFail, s.summary.TotalError)
	if s.summary.AllPass {
		sb.WriteString(s.styles.Success.Render(overall))
	} else {
		sb.WriteString(s.styles.Error.Render(overall))
	}
	sb.WriteString("\n\n")

	// Resolved binaries section (after overall summary, before per-harness breakdown).
	if len(s.summary.ResolvedPaths) > 0 {
		sb.WriteString(s.styles.Subtitle.Render("  Resolved binaries:"))
		sb.WriteString("\n")
		keys := sortedKeys(s.summary.ResolvedPaths)
		for _, id := range keys {
			sb.WriteString(s.styles.Muted.Render(fmt.Sprintf("    %s: %s", id, s.summary.ResolvedPaths[id])))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Per-harness breakdown.
	for _, hr := range s.summary.HarnessResults {
		harnessLine := fmt.Sprintf("  Harness: %s  —  %d pass  %d fail  %d error",
			hr.Harness, hr.PassCount, hr.FailCount, hr.ErrorCount)
		sb.WriteString(s.styles.Subtitle.Render(harnessLine))
		sb.WriteString("\n")

		// For each failing or erroring test, show the first mismatch / error.
		for _, r := range hr.Results {
			if r.Pass {
				continue
			}
			testLine := fmt.Sprintf("    FAIL  %s / %s", r.WorkflowID, r.Mode)
			sb.WriteString(s.styles.Error.Render(testLine))
			sb.WriteString("\n")

			isError := r.Error != nil

			if r.Error != nil {
				sb.WriteString(s.styles.Muted.Render(fmt.Sprintf("      error: %v", r.Error)))
				sb.WriteString("\n")
			} else if r.Mismatch != nil {
				sb.WriteString(renderMismatch(r.Mismatch, s.styles))
				sb.WriteString("\n")
			}

			// Show exit code: ERROR=always (even 0), FAIL=non-zero only.
			if isError || r.ActualExitCode != 0 {
				sb.WriteString(s.styles.Muted.Render(fmt.Sprintf("      exit code: %d", r.ActualExitCode)))
				sb.WriteString("\n")
			}

			// Show stderr block when non-empty (independent of exit code).
			if r.ChildStderr != "" {
				sb.WriteString(s.styles.Muted.Render("      stderr:"))
				sb.WriteString("\n")
				content := testrun.TruncateTail(r.ChildStderr, testrun.MaxStderrDisplayBytes)
				sb.WriteString(s.styles.Muted.Render(content))
				sb.WriteString("\n")
			}
		}
	}

	// Show log file path when available (non-empty).
	if s.summary.LogPath != "" {
		sb.WriteString(s.styles.Muted.Render(fmt.Sprintf("  Log file: %s", s.summary.LogPath)))
		sb.WriteString("\n")
	}

	sb.WriteString(border)
	sb.WriteString("\n")
	sb.WriteString(s.styles.Help.Width(s.width).Render("q/esc/enter quit"))

	return sb.String()
}

// sortedKeys returns the keys of a map[string]string in lexicographic order.
// Used to produce deterministic output for the "Resolved binaries:" section.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// renderMismatch formats a single Mismatch for display.
func renderMismatch(m *testcheck.Mismatch, styles Styles) string {
	if m == nil {
		return ""
	}
	return styles.Muted.Render(fmt.Sprintf("      %s", m.Message))
}

// Done reports whether the user dismissed the results screen.
func (s *TestResultsScreen) Done() bool { return s.done }

// Back always returns false.
func (s *TestResultsScreen) Back() bool { return false }

// Reset clears the done flag (but keeps the summary so it can be re-displayed).
func (s *TestResultsScreen) Reset() {
	s.done = false
}

// Resize updates the screen dimensions.
func (s *TestResultsScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
