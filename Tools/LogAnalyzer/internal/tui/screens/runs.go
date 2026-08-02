package screens

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-log-analyzer/internal/domain"
)

// RunsScreen shows all-runs totals plus one row per named run with tokens
// and money side by side. Provisional runs are labelled. The unattributable
// bucket is in its own section, excluded from the named-run total. Data-quality
// findings are summarised at the bottom.
//
// Navigation contract:
//   - ↑/↓  navigate runs.
//   - Enter -> Done() == true, SelectedRunID() returns the chosen run id.
//   - Esc   -> Back() == true.
type RunsScreen struct {
	report domain.Report
	cursor int
	width  int
	height int
	styles Styles
	done   bool
	back   bool
}

// NewRunsScreen creates the run-list screen for the given report.
func NewRunsScreen(report domain.Report, width, height int, styles Styles) *RunsScreen {
	return &RunsScreen{
		report: report,
		width:  width,
		height: height,
		styles: styles,
	}
}

// SetReport replaces the report (used after a reprice without re-navigating).
func (s *RunsScreen) SetReport(r domain.Report) {
	s.report = r
	s.done = false
	s.back = false
	if s.cursor >= len(r.Runs) && len(r.Runs) > 0 {
		s.cursor = len(r.Runs) - 1
	}
}

// ID implements Screen.
func (s *RunsScreen) ID() ScreenID { return ScreenRuns }

// Init implements Screen.
func (s *RunsScreen) Init() tea.Cmd { return nil }

// Update processes key messages for navigation.
func (s *RunsScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	n := len(s.report.Runs)
	switch key.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < n-1 {
			s.cursor++
		}
	case "enter":
		if n > 0 {
			s.done = true
		}
	case "esc", "q":
		s.back = true
	}
	return s, nil
}

// View renders the run-list screen.
func (s *RunsScreen) View(width, height int) string {
	s.width = width
	s.height = height

	var sb strings.Builder
	sep := s.styles.Border.Width(width).Render(strings.Repeat("─", width))

	// Title.
	sb.WriteString(s.styles.Title.Width(width).Render("Log Analyzer — Runs"))
	sb.WriteByte('\n')
	sb.WriteString(sep)
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// All-runs totals (named runs only, per contract).
	sb.WriteString(s.styles.Subtitle.Width(width).Render("All runs"))
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Body.Width(width).Render("  " + FormatTotalsLine(s.report.AllRuns)))
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// Per-run list.
	if len(s.report.Runs) == 0 {
		sb.WriteString(s.styles.Muted.Width(width).Render("  (no named runs)"))
		sb.WriteByte('\n')
	} else {
		sb.WriteString(s.styles.Subtitle.Width(width).Render(fmt.Sprintf("Runs (%d)", len(s.report.Runs))))
		sb.WriteByte('\n')
		for i, run := range s.report.Runs {
			s.writeRunRow(&sb, i, run, width)
		}
	}

	// Unattributable bucket (separate section, not in named-run total).
	if s.report.Unattributable != nil {
		sb.WriteByte('\n')
		sb.WriteString(sep)
		sb.WriteByte('\n')
		label := s.styles.Warning.Width(width).Render(UnattributableLabel)
		sb.WriteString(label)
		sb.WriteByte('\n')
		sb.WriteString(s.styles.Body.Width(width).Render("  " + FormatTotalsLine(s.report.Unattributable.Totals)))
		sb.WriteByte('\n')
	}

	// Data-quality findings summary.
	if !s.report.Quality.IsClean() {
		sb.WriteByte('\n')
		counts := s.report.Quality.Counts
		total := 0
		for _, c := range counts {
			total += c
		}
		incomplete := ""
		if s.report.Quality.Incomplete() {
			incomplete = " — data is incomplete"
		}
		line := fmt.Sprintf("⚠ %d data-quality finding(s)%s", total, incomplete)
		sb.WriteString(s.styles.Warning.Width(width).Render(line))
		sb.WriteByte('\n')
	}

	// Unpriced models notice.
	if len(s.report.UnpricedModels) > 0 {
		line := fmt.Sprintf("Some models have no pricing (%d model(s) unpriced). Press 'p' to enter prices.", len(s.report.UnpricedModels))
		sb.WriteString(s.styles.Warning.Width(width).Render(line))
		sb.WriteByte('\n')
	}

	// Help bar.
	sb.WriteString(sep)
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Help.Width(width).Render("↑/↓ navigate  enter open  p pricing  esc quit  ctrl+c quit"))

	return sb.String()
}

// writeRunRow renders one run row into the string builder.
func (s *RunsScreen) writeRunRow(sb *strings.Builder, idx int, run domain.RunReport, width int) {
	cursor := "  "
	style := s.styles.Body
	if idx == s.cursor {
		cursor = "> "
		style = s.styles.Selected
	}

	label := run.Run.Label()
	if run.Provisional {
		label += " (" + ProvisionalLabel + ")"
	}
	sb.WriteString(style.Width(width).Render(cursor + label))
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Muted.Width(width).Render("    " + FormatTotalsLine(run.Totals)))
	sb.WriteByte('\n')
}

// Help returns the key hints for this screen.
func (s *RunsScreen) Help() string {
	return "↑/↓ navigate  enter open  p pricing  esc quit  ctrl+c quit"
}

// Done reports whether the user selected a run.
func (s *RunsScreen) Done() bool { return s.done }

// Back reports whether the user pressed Esc.
func (s *RunsScreen) Back() bool { return s.back }

// SelectedRunID returns the id of the currently highlighted run.
// Only meaningful when Done() is true.
func (s *RunsScreen) SelectedRunID() string {
	if len(s.report.Runs) == 0 || s.cursor >= len(s.report.Runs) {
		return ""
	}
	return s.report.Runs[s.cursor].Run.ID
}

// Reset clears the done and back flags.
func (s *RunsScreen) Reset() {
	s.done = false
	s.back = false
}
