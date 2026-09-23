package screens

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	lipgloss "github.com/charmbracelet/lipgloss"
	runewidth "github.com/mattn/go-runewidth"
	"mosaic-run/internal/testcheck"
	"mosaic-run/internal/testrun"
)

// ansiEscPattern matches ANSI CSI/SGR escape sequences (e.g. \x1b[31m).
var ansiEscPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// controlCharPattern matches non-printable control bytes except \t (0x09) and \n (0x0a).
var controlCharPattern = regexp.MustCompile("[\x00-\x08\x0b-\x1f\x7f]")

// logicalSection pairs unstyled raw text with the lipgloss style to apply
// after ANSI stripping, tab expansion, and hard-wrapping.
type logicalSection struct {
	text  string
	style lipgloss.Style
}

// TestResultsScreen displays the final test summary with vertical scrolling.
//
// Navigation contract:
//   - up/k, down/j: scroll one line
//   - pgup/pgdown: scroll one page
//   - mouse wheel up/down: scroll one line (when mouse enabled)
//   - q / Esc / Enter -> Done() == true
//   - Back() is always false
type TestResultsScreen struct {
	summary *testrun.TestSummary
	done    bool
	width   int
	height  int
	styles  Styles
	lines   []string // pre-built visual rows (ANSI-safe wrapped to width)
	offset  int      // index of the first visible line
}

// NewTestResultsScreen creates the results summary display.
// Lines are pre-built at construction time so scroll keys work
// before the first View() call.
func NewTestResultsScreen(width, height int, styles Styles, summary *testrun.TestSummary) *TestResultsScreen {
	s := &TestResultsScreen{
		summary: summary,
		width:   width,
		height:  height,
		styles:  styles,
	}
	s.rebuildLines()
	return s
}

// contentHeight returns the number of body lines visible in the content area:
// height - 5 (title, top border, bottom border, indicator, help).
// Clamps to a minimum of 1.
func (s *TestResultsScreen) contentHeight() int {
	h := s.height - 5
	if h < 1 {
		return 1
	}
	return h
}

// rebuildLines constructs the lines slice from s.summary using ANSI-safe
// wrapping. Must not panic for any width value.
func (s *TestResultsScreen) rebuildLines() {
	if s.summary == nil {
		s.lines = nil
		return
	}

	effectiveWidth := s.width
	if effectiveWidth <= 0 {
		effectiveWidth = 1
	}

	// Step 1: build logical sections (unstyled text + style pairs).
	var sections []logicalSection
	sections = s.buildSections(sections, effectiveWidth)

	// Process each section: normalize, strip, expand tabs, split, wrap, style.
	var lines []string
	for _, sec := range sections {
		// Step 2: strip \r
		text := strings.ReplaceAll(sec.text, "\r", "")
		// Step 3: strip ANSI escape sequences and non-printable control chars
		text = ansiEscPattern.ReplaceAllString(text, "")
		text = controlCharPattern.ReplaceAllString(text, "")
		// Step 4: expand tabs to 4 spaces
		text = strings.ReplaceAll(text, "\t", "    ")
		// Step 5: split on \n to get logical lines
		logicalLines := strings.Split(text, "\n")
		// Steps 6+7+8: hard-wrap each logical line, apply style, store
		for _, ll := range logicalLines {
			wrapped := hardWrapLine(ll, effectiveWidth)
			for _, row := range wrapped {
				lines = append(lines, sec.style.Render(row))
			}
		}
	}

	s.lines = lines
	s.clampOffset()
}

// buildSections constructs the logical sections for the body content.
func (s *TestResultsScreen) buildSections(sections []logicalSection, effectiveWidth int) []logicalSection {
	if s.summary.DeployError != nil {
		// Deploy-error path
		sections = append(sections, logicalSection{
			text:  fmt.Sprintf("  Deploy failed: %v", s.summary.DeployError),
			style: s.styles.Error,
		})
		// Mid-body separator (scrollable border line)
		sections = append(sections, logicalSection{
			text:  strings.Repeat("─", effectiveWidth),
			style: s.styles.Border,
		})
		if len(s.summary.ResolvedPaths) > 0 {
			sections = append(sections, logicalSection{
				text:  "  Resolved binaries:",
				style: s.styles.Subtitle,
			})
			keys := sortedKeys(s.summary.ResolvedPaths)
			for _, id := range keys {
				sections = append(sections, logicalSection{
					text:  fmt.Sprintf("    %s: %s", id, s.summary.ResolvedPaths[id]),
					style: s.styles.Muted,
				})
			}
		}
		return sections
	}

	// Normal path
	overall := fmt.Sprintf("  Total: %d pass  %d fail  %d error",
		s.summary.TotalPass, s.summary.TotalFail, s.summary.TotalError)
	overallStyle := s.styles.Error
	if s.summary.AllPass {
		overallStyle = s.styles.Success
	}
	sections = append(sections, logicalSection{text: overall, style: overallStyle})

	if len(s.summary.ResolvedPaths) > 0 {
		sections = append(sections, logicalSection{
			text:  "  Resolved binaries:",
			style: s.styles.Subtitle,
		})
		keys := sortedKeys(s.summary.ResolvedPaths)
		for _, id := range keys {
			sections = append(sections, logicalSection{
				text:  fmt.Sprintf("    %s: %s", id, s.summary.ResolvedPaths[id]),
				style: s.styles.Muted,
			})
		}
		// Blank line after resolved paths
		sections = append(sections, logicalSection{text: "", style: s.styles.Body})
	}

	for _, hr := range s.summary.HarnessResults {
		harnessLine := fmt.Sprintf("  Harness: %s  —  %d pass  %d fail  %d error",
			hr.Harness, hr.PassCount, hr.FailCount, hr.ErrorCount)
		sections = append(sections, logicalSection{text: harnessLine, style: s.styles.Subtitle})

		for _, r := range hr.Results {
			if r.Pass {
				continue
			}
			testLine := fmt.Sprintf("    FAIL  %s / %s", r.WorkflowID, r.Mode)
			sections = append(sections, logicalSection{text: testLine, style: s.styles.Error})

			isError := r.Error != nil

			if r.Error != nil {
				sections = append(sections, logicalSection{
					text:  fmt.Sprintf("      error: %v", r.Error),
					style: s.styles.Muted,
				})
			} else if r.Mismatch != nil {
				sections = append(sections, logicalSection{
					text:  fmt.Sprintf("      %s", r.Mismatch.Message),
					style: s.styles.Muted,
				})
			}

			if isError || r.ActualExitCode != 0 {
				sections = append(sections, logicalSection{
					text:  fmt.Sprintf("      exit code: %d", r.ActualExitCode),
					style: s.styles.Muted,
				})
			}

			if r.ChildStderr != "" {
				sections = append(sections, logicalSection{
					text:  "      stderr:",
					style: s.styles.Muted,
				})
				content := testrun.TruncateTail(r.ChildStderr, testrun.MaxStderrDisplayBytes)
				sections = append(sections, logicalSection{text: content, style: s.styles.Muted})
			}
		}
	}

	if s.summary.LogPath != "" {
		sections = append(sections, logicalSection{
			text:  fmt.Sprintf("  Log file: %s", s.summary.LogPath),
			style: s.styles.Muted,
		})
	}

	return sections
}

// hardWrapLine wraps a single logical line (no embedded newlines) to width
// display cells. Returns at least one row (empty string for empty input).
// Never loops or panics for any width >= 1.
func hardWrapLine(s string, width int) []string {
	if width <= 0 {
		width = 1
	}
	if s == "" {
		return []string{""}
	}
	var result []string
	for len(s) > 0 {
		var rowWidth int
		cutIdx := -1
		for i, r := range s {
			rw := runewidth.RuneWidth(r)
			if rowWidth+rw > width {
				cutIdx = i
				break
			}
			rowWidth += rw
		}
		if cutIdx == -1 {
			// Remaining string fits entirely.
			result = append(result, s)
			break
		}
		if cutIdx == 0 {
			// Even the first rune doesn't fit; take it anyway to avoid infinite loop.
			_, sz := utf8.DecodeRuneInString(s)
			result = append(result, s[:sz])
			s = s[sz:]
		} else {
			result = append(result, s[:cutIdx])
			s = s[cutIdx:]
		}
	}
	return result
}

// clampOffset ensures offset stays within [0, max(0, len(lines)-contentHeight)].
func (s *TestResultsScreen) clampOffset() {
	maxOffset := len(s.lines) - s.contentHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if s.offset > maxOffset {
		s.offset = maxOffset
	}
	if s.offset < 0 {
		s.offset = 0
	}
}

// Update processes key and mouse messages.
func (s *TestResultsScreen) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "enter":
			s.done = true
		case "up", "k":
			if s.offset > 0 {
				s.offset--
			}
		case "down", "j":
			maxOffset := len(s.lines) - s.contentHeight()
			if maxOffset < 0 {
				maxOffset = 0
			}
			if s.offset < maxOffset {
				s.offset++
			}
		case "pgup":
			s.offset -= s.contentHeight()
			if s.offset < 0 {
				s.offset = 0
			}
		case "pgdown":
			s.offset += s.contentHeight()
			maxOffset := len(s.lines) - s.contentHeight()
			if maxOffset < 0 {
				maxOffset = 0
			}
			if s.offset > maxOffset {
				s.offset = maxOffset
			}
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				if s.offset > 0 {
					s.offset--
				}
			case tea.MouseButtonWheelDown:
				maxOffset := len(s.lines) - s.contentHeight()
				if maxOffset < 0 {
					maxOffset = 0
				}
				if s.offset < maxOffset {
					s.offset++
				}
			}
		}
	}
	return nil
}

// View renders only the visible window of the results summary.
func (s *TestResultsScreen) View() string {
	if s.summary == nil {
		return s.styles.Error.Render("No test results available.")
	}

	effectiveWidth := s.width
	if effectiveWidth <= 0 {
		effectiveWidth = 1
	}

	title := s.styles.Title.Width(effectiveWidth).Render("Test Results")
	border := s.styles.Border.Width(effectiveWidth).Render(strings.Repeat("─", effectiveWidth))

	contentH := s.contentHeight()
	end := s.offset + contentH
	if end > len(s.lines) {
		end = len(s.lines)
	}

	// Build body rows, padded to contentH so chrome is pinned.
	bodyRows := make([]string, contentH)
	for i := 0; i < contentH; i++ {
		lineIdx := s.offset + i
		if lineIdx < len(s.lines) {
			bodyRows[i] = s.lines[lineIdx]
		}
		// else: leave as "" (padding)
	}

	// Position indicator.
	total := len(s.lines)
	var statusText string
	if total == 0 {
		statusText = strings.Repeat(" ", effectiveWidth)
	} else {
		pos := s.offset + 1
		statusText = s.styles.Muted.Width(effectiveWidth).Render(
			"lines " + strconv.Itoa(pos) + "-" + strconv.Itoa(end) + " of " + strconv.Itoa(total))
	}

	help := s.styles.Help.Width(effectiveWidth).Render("up/k up  down/j down  pgup/pgdn  q/esc/enter quit")

	parts := make([]string, 0, 5+contentH)
	parts = append(parts, title)
	parts = append(parts, border)
	parts = append(parts, strings.Join(bodyRows, "\n"))
	parts = append(parts, border)
	parts = append(parts, statusText)
	parts = append(parts, help)

	return strings.Join(parts, "\n")
}

// sortedKeys returns the keys of a map[string]string in lexicographic order.
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

// Reset clears the done flag and resets the scroll offset to zero.
// Does NOT rebuild lines because the summary is immutable after construction.
func (s *TestResultsScreen) Reset() {
	s.done = false
	s.offset = 0
}

// Resize updates the screen dimensions, rebuilds wrapped lines
// (width changes affect wrapping), and clamps the offset.
func (s *TestResultsScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.rebuildLines()
	s.clampOffset()
}
