package screens

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-log-analyzer/internal/domain"
)

// RunDetailScreen shows the per-subagent-instance breakdown for one run with
// all token categories and money side by side. The orchestrator's own cost is
// shown as a distinct line.
//
// Navigation contract:
//   - ↑/↓  scroll actor list.
//   - Esc   -> Back() == true.
type RunDetailScreen struct {
	run    domain.RunReport
	scroll int
	width  int
	height int
	styles Styles
	back   bool
}

// NewRunDetailScreen creates the run-detail screen for the given run report.
func NewRunDetailScreen(run domain.RunReport, width, height int, styles Styles) *RunDetailScreen {
	return &RunDetailScreen{
		run:    run,
		width:  width,
		height: height,
		styles: styles,
	}
}

// ID implements Screen.
func (s *RunDetailScreen) ID() ScreenID { return ScreenRunDetail }

// Init implements Screen.
func (s *RunDetailScreen) Init() tea.Cmd { return nil }

// Update processes key messages for navigation.
func (s *RunDetailScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch key.String() {
	case "up", "k":
		if s.scroll > 0 {
			s.scroll--
		}
	case "down", "j":
		s.scroll++
	case "esc", "q":
		s.back = true
	}
	return s, nil
}

// View renders the run-detail screen.
func (s *RunDetailScreen) View(width, height int) string {
	s.width = width
	s.height = height

	var sb strings.Builder
	sep := s.styles.Border.Width(width).Render(strings.Repeat("─", width))

	// Title.
	label := s.run.Run.Label()
	if s.run.Provisional {
		label += " (" + ProvisionalLabel + ")"
	}
	sb.WriteString(s.styles.Title.Width(width).Render("Log Analyzer — Run Detail"))
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Subtitle.Width(width).Render(label))
	sb.WriteByte('\n')
	sb.WriteString(sep)
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// Run totals.
	sb.WriteString(s.styles.Subtitle.Width(width).Render("Run totals"))
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Body.Width(width).Render("  " + FormatTotalsLine(s.run.Totals)))
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// Orchestrator (distinct line, per AC10.5 and design).
	sb.WriteString(s.styles.Subtitle.Width(width).Render("Orchestrator"))
	sb.WriteByte('\n')
	s.writeActorDetail(&sb, s.run.Orchestrator, width)
	sb.WriteByte('\n')

	// Per-subagent-instance breakdown.
	if len(s.run.Agents) > 0 {
		sb.WriteString(s.styles.Subtitle.Width(width).Render(fmt.Sprintf("Agents (%d)", len(s.run.Agents))))
		sb.WriteByte('\n')
		for _, agent := range s.run.Agents {
			s.writeActorDetail(&sb, agent, width)
		}
	}

	// Unpriced models for this run.
	if len(s.run.UnpricedModels) > 0 {
		sb.WriteByte('\n')
		models := make([]string, len(s.run.UnpricedModels))
		for i, m := range s.run.UnpricedModels {
			models[i] = string(m)
		}
		sb.WriteString(s.styles.Warning.Width(width).Render(
			"Unpriced models: " + strings.Join(models, ", "),
		))
		sb.WriteByte('\n')
	}

	// Help bar.
	sb.WriteString(sep)
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Help.Width(width).Render("↑/↓ scroll  esc back  ctrl+c quit"))

	return sb.String()
}

// writeActorDetail renders one actor's row into the string builder.
func (s *RunDetailScreen) writeActorDetail(sb *strings.Builder, actor domain.ActorReport, width int) {
	label := actor.Actor.Label()
	if len(actor.UnpricedModels) > 0 {
		label += " (has unpriced models)"
	}
	sb.WriteString(s.styles.Body.Width(width).Render("  " + label))
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Muted.Width(width).Render("    " + FormatTotalsLine(actor.Totals)))
	sb.WriteByte('\n')
	if len(actor.Models) > 0 {
		models := make([]string, len(actor.Models))
		for i, m := range actor.Models {
			models[i] = string(m)
		}
		sb.WriteString(s.styles.Muted.Width(width).Render("    models: " + strings.Join(models, ", ")))
		sb.WriteByte('\n')
	}
}

// Help returns the key hints for this screen.
func (s *RunDetailScreen) Help() string {
	return "↑/↓ scroll  esc back  ctrl+c quit"
}

// Back reports whether the user pressed Esc.
func (s *RunDetailScreen) Back() bool { return s.back }

// Reset clears the back flag.
func (s *RunDetailScreen) Reset() { s.back = false }
