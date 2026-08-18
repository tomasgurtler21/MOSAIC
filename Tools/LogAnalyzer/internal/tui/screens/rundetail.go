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
// down/j does not advance past the last actor row; clamping also happens in
// View so that terminal resizes are handled on the next render.
func (s *RunDetailScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	actorCount := 1 + len(s.run.Agents)
	switch key.String() {
	case "up", "k":
		if s.scroll > 0 {
			s.scroll--
		}
	case "down", "j":
		if actorCount > 0 && s.scroll < actorCount-1 {
			s.scroll++
		}
	case "esc", "q":
		s.back = true
	}
	return s, nil
}

// View renders the run-detail screen, clipped to the given height.
// Actor rows (orchestrator + agents) form one scrollable list.  The scroll
// offset is clamped against the passed-in height on every render so that
// terminal resizes are handled implicitly on the next call.
func (s *RunDetailScreen) View(width, height int) string {
	s.width = width
	s.height = height

	sep := s.styles.Border.Width(width).Render(strings.Repeat("─", width))

	// Build the combined actor list: orchestrator first, then agents.
	allActors := make([]domain.ActorReport, 0, 1+len(s.run.Agents))
	allActors = append(allActors, s.run.Orchestrator)
	allActors = append(allActors, s.run.Agents...)

	// Determine the worst-case lines per actor item (2 lines; 3 when the actor
	// has a models line).  Using the maximum across all actors ensures that a
	// mixed list cannot overflow the allocated line budget.
	linesPerItem := 2
	for _, a := range allActors {
		if len(a.Models) > 0 {
			linesPerItem = 3
			break
		}
	}

	// --- Fixed header (7 lines) ---
	var headerSB strings.Builder
	label := s.run.Run.Label()
	if s.run.Provisional {
		label += " (" + ProvisionalLabel + ")"
	}
	headerSB.WriteString(s.styles.Title.Width(width).Render("Log Analyzer — Run Detail"))
	headerSB.WriteByte('\n')
	headerSB.WriteString(s.styles.Subtitle.Width(width).Render(label))
	headerSB.WriteByte('\n')
	headerSB.WriteString(sep)
	headerSB.WriteByte('\n')
	headerSB.WriteByte('\n')
	headerSB.WriteString(s.styles.Subtitle.Width(width).Render("Run totals"))
	headerSB.WriteByte('\n')
	headerSB.WriteString(s.styles.Body.Width(width).Render("  " + FormatTotalsLine(s.run.Totals)))
	headerSB.WriteByte('\n')
	headerSB.WriteByte('\n')
	headerStr := headerSB.String()
	headerLines := strings.Count(headerStr, "\n")

	// --- Variable footer (unpriced models notice) ---
	// Rendered before the actor list so its line count feeds the available calculation.
	var varFooterSB strings.Builder
	if len(s.run.UnpricedModels) > 0 {
		varFooterSB.WriteByte('\n')
		models := make([]string, len(s.run.UnpricedModels))
		for i, m := range s.run.UnpricedModels {
			models[i] = string(m)
		}
		varFooterSB.WriteString(s.styles.Warning.Width(width).Render(
			"Unpriced models: " + strings.Join(models, ", "),
		))
		varFooterSB.WriteByte('\n')
	}
	varFooterStr := varFooterSB.String()
	varFooterLines := strings.Count(varFooterStr, "\n")

	// --- Scroll arithmetic ---
	const fixedFooterLines = 2 // sep + help bar
	overhead := headerLines + varFooterLines + fixedFooterLines
	available := height - overhead
	actorCount := len(allActors)
	visibleItems := VisibleItems(available, linesPerItem)
	// No-cursor mode: scroll is a plain offset; ScrollWindow clamps it.
	w := ScrollWindow(s.scroll, -1, actorCount, visibleItems)
	s.scroll = w.Offset

	// --- Scroll hint for the help bar ---
	hint := FormatScrollHint(w, actorCount)
	helpText := "↑/↓ scroll  esc back  ctrl+c quit"
	if hint != "" {
		helpText += "  " + hint
	}

	// --- Visible actor rows ---
	// Section headings ("Orchestrator", "Agents (N)") are rendered inline with
	// the first item of each section when that item is in the visible window.
	// This keeps them out of the fixed overhead so the line budget is derived
	// purely from actor detail rows (2 or 3 lines each).
	var actorsSB strings.Builder
	for i := w.Offset; i < w.End() && i < actorCount; i++ {
		if i == 0 {
			actorsSB.WriteString(s.styles.Subtitle.Width(width).Render("Orchestrator"))
			actorsSB.WriteByte('\n')
		} else if i == 1 && len(s.run.Agents) > 0 {
			actorsSB.WriteString(s.styles.Subtitle.Width(width).Render(fmt.Sprintf("Agents (%d)", len(s.run.Agents))))
			actorsSB.WriteByte('\n')
		}
		s.writeActorDetail(&actorsSB, allActors[i], width)
	}

	// --- Assemble ---
	var sb strings.Builder
	sb.WriteString(headerStr)
	sb.WriteString(actorsSB.String())
	sb.WriteString(varFooterStr)
	sb.WriteString(sep)
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Help.Width(width).Render(helpText))

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
