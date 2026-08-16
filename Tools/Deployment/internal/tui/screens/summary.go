package screens

// summary.go implements the SummaryScreen shown after a deployment or update run completes.
// It is the TUI equivalent of the CLI's stdout summary and renders every field from
// domain.RunSummary: action records (with the driving version delta for updates), skipped
// and backed-up files, any fallback location used, any active external module, and all
// required follow-up actions from the TODO collection.
//
// The summary and the TODO checklist file are projections of one source (domain.RunSummary),
// so they cannot disagree: the checklist file is already written at this point; the screen
// renders the same data.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
)

// SummaryScreen renders the run outcome and exits when the user presses q or enter.
// It is not an overlay — it is the final screen state after screenDone is entered.
type SummaryScreen struct {
	summary      domain.RunSummary
	lines        []summaryLine
	scrollOffset int
	visibleLines int
	done         bool
	width        int
	height       int
	styles       Styles
}

// summaryLine is one pre-rendered line with a style tag.
type summaryLine struct {
	text  string
	style string
}

// NewSummaryScreen creates the post-run summary screen.
func NewSummaryScreen(summary domain.RunSummary, width, height int, styles Styles) *SummaryScreen {
	s := &SummaryScreen{
		summary: summary,
		width:   width,
		height:  height,
		styles:  styles,
	}
	s.lines = buildSummaryLines(summary)
	s.visibleLines = summaryVisibleLines(height)
	return s
}

// summaryVisibleLines computes the scrollable area height.
func summaryVisibleLines(totalHeight int) int {
	// title + border + help = 3 fixed lines
	h := totalHeight - 3
	if h < 3 {
		return 3
	}
	return h
}

// buildSummaryLines constructs the scrollable content from a RunSummary.
func buildSummaryLines(s domain.RunSummary) []summaryLine {
	var lines []summaryLine
	add := func(text, style string) {
		lines = append(lines, summaryLine{text: text, style: style})
	}

	// Outcome banner.
	switch s.Outcome {
	case domain.OutcomeSuccess:
		add("Run complete — all files deployed successfully.", "success")
	case domain.OutcomeCompletedWithGaps:
		add("Run complete with gaps — some items were skipped or require manual follow-up.", "warning")
	case domain.OutcomeFailed:
		add("Run failed — see errors below.", "error")
	default:
		add(fmt.Sprintf("Run finished (%s).", s.Outcome), "body")
	}
	add("", "body")

	// Context.
	add(fmt.Sprintf("Mode:             %s", modeDisplayName(s.Mode)), "muted")
	add(fmt.Sprintf("Harness:          %s", s.Harness.DisplayName), "muted")
	add(fmt.Sprintf("Workspace:        %s", s.WorkspacePath), "muted")
	add(fmt.Sprintf("Deployment root:  %s", s.DeploymentRoot), "muted")

	if s.Fallback != domain.FallbackNone {
		add(fmt.Sprintf("Fallback used:    %s", s.Fallback), "warning")
	}
	if s.External != nil {
		add(fmt.Sprintf("External module:  %s  (%s)", s.External.HarnessID, s.External.ExecutablePath), "body")
	}
	add("", "body")

	// Action records.
	var created, updated, unchanged, skipped, backedUp, failed []domain.ActionRecord
	for _, a := range s.Actions {
		switch a.Taken {
		case domain.TakenCreated:
			created = append(created, a)
		case domain.TakenUpdated:
			updated = append(updated, a)
		case domain.TakenUnchanged:
			unchanged = append(unchanged, a)
		case domain.TakenSkipped:
			skipped = append(skipped, a)
		case domain.TakenBackedUp:
			backedUp = append(backedUp, a)
		case domain.TakenFailed:
			failed = append(failed, a)
		}
	}

	if len(created) > 0 {
		add(fmt.Sprintf("Created (%d):", len(created)), "success")
		for _, a := range created {
			add(fmt.Sprintf("  ✓ %s:%s", a.Ref.Kind, a.Ref.Key), "success")
		}
		add("", "body")
	}

	if len(updated) > 0 {
		add(fmt.Sprintf("Updated (%d):", len(updated)), "success")
		for _, a := range updated {
			add(fmt.Sprintf("  ✓ %s:%s", a.Ref.Kind, a.Ref.Key), "success")
			for _, delta := range a.Stale {
				add(fmt.Sprintf("      %s: %s → %s", delta.Field, delta.Deployed, delta.Source), "muted")
			}
		}
		add("", "body")
	}

	if len(skipped) > 0 {
		add(fmt.Sprintf("Skipped (%d):", len(skipped)), "warning")
		for _, a := range skipped {
			add(fmt.Sprintf("  – %s:%s", a.Ref.Kind, a.Ref.Key), "warning")
		}
		add("", "body")
	}

	if len(backedUp) > 0 {
		add(fmt.Sprintf("Backed up and overwritten (%d):", len(backedUp)), "warning")
		for _, a := range backedUp {
			add(fmt.Sprintf("  ~ %s:%s", a.Ref.Kind, a.Ref.Key), "warning")
			if a.BackupPath != "" {
				add(fmt.Sprintf("      backup: %s", a.BackupPath), "muted")
			}
		}
		add("", "body")
	}

	if len(failed) > 0 {
		add(fmt.Sprintf("Failed (%d):", len(failed)), "error")
		for _, a := range failed {
			add(fmt.Sprintf("  ✗ %s:%s", a.Ref.Kind, a.Ref.Key), "error")
			if a.Err != "" {
				add(fmt.Sprintf("      %s", a.Err), "muted")
			}
		}
		add("", "body")
	}

	if len(unchanged) > 0 {
		add(fmt.Sprintf("Unchanged (%d — already up to date)", len(unchanged)), "muted")
		add("", "body")
	}

	// TODO items (follow-up actions).
	if len(s.Todos) > 0 {
		add(strings.Repeat("─", 40), "border")
		add(fmt.Sprintf("Follow-up required (%d item(s)):", len(s.Todos)), "warning")
		currentCat := domain.TodoCategory("")
		for _, item := range s.Todos {
			if item.Category != currentCat {
				currentCat = item.Category
				add(fmt.Sprintf("  [%s]", item.Category), "body")
			}
			add(formatFollowUpLine(item.Subject, item.Detail, item.Owner), "muted")
		}
		if s.TodoFilePath != "" {
			add("", "body")
			add(fmt.Sprintf("Full checklist written to: %s", s.TodoFilePath), "muted")
		}
		add("", "body")
	}

	// Log paths.
	if s.Logs.Latest != "" {
		add(fmt.Sprintf("Log: %s", s.Logs.Latest), "muted")
	}

	// Degraded logging warnings.
	for _, d := range s.LogDegraded {
		add(fmt.Sprintf("Logging warning: %s", d), "warning")
	}

	return lines
}

// Update processes a tea.Msg. Returns true when the user exits.
func (s *SummaryScreen) Update(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	switch keyMsg.String() {
	case "up", "k":
		if s.scrollOffset > 0 {
			s.scrollOffset--
		}
	case "down", "j":
		maxOffset := len(s.lines) - s.visibleLines
		if maxOffset < 0 {
			maxOffset = 0
		}
		if s.scrollOffset < maxOffset {
			s.scrollOffset++
		}
	case "q", "enter", "esc":
		s.done = true
		return true
	}
	return false
}

// Done reports whether the user pressed a key to exit.
func (s *SummaryScreen) Done() bool { return s.done }

// View renders the summary screen.
func (s *SummaryScreen) View() string {
	var sb strings.Builder

	sb.WriteString(s.styles.Title.Width(s.width).Render("Deployment Summary"))
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width)))
	sb.WriteByte('\n')

	// Scrollable content window.
	start := s.scrollOffset
	end := start + s.visibleLines
	if end > len(s.lines) {
		end = len(s.lines)
	}
	for i := start; i < end; i++ {
		line := s.lines[i]
		sb.WriteString(s.renderSummaryLine(line))
		sb.WriteByte('\n')
	}

	// Scroll position indicator if needed.
	if len(s.lines) > s.visibleLines {
		total := len(s.lines)
		shown := end - start
		sb.WriteString(s.styles.Muted.Width(s.width).Render(
			fmt.Sprintf("── %d–%d of %d lines ──", start+1, start+shown, total),
		))
		sb.WriteByte('\n')
	}

	sb.WriteString(s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  q/enter exit"))
	return sb.String()
}

// renderSummaryLine applies the named style to one summary line.
func (s *SummaryScreen) renderSummaryLine(line summaryLine) string {
	switch line.style {
	case "success":
		return s.styles.Success.Width(s.width).Render(line.text)
	case "warning":
		return s.styles.Warning.Width(s.width).Render(line.text)
	case "error":
		return s.styles.Error.Width(s.width).Render(line.text)
	case "muted":
		return s.styles.Muted.Width(s.width).Render(line.text)
	case "border":
		return s.styles.Border.Width(s.width).Render(line.text)
	default:
		return s.styles.Body.Width(s.width).Render(line.text)
	}
}

// Resize updates the screen dimensions and recomputes the visible line count.
func (s *SummaryScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.visibleLines = summaryVisibleLines(height)
	maxOffset := len(s.lines) - s.visibleLines
	if maxOffset < 0 {
		maxOffset = 0
	}
	if s.scrollOffset > maxOffset {
		s.scrollOffset = maxOffset
	}
}
