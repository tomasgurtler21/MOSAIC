package screens

// review.go implements the ReviewScreen overlay shown when the app calls Interaction.Review.
// The screen renders every planned action before any file is written so the user can inspect
// the full set of changes and decide whether to proceed or cancel the run.
//
// Plan items are grouped by action type (create, update, locally-modified, unchanged) and
// colour-coded. Items with version deltas show which fields drove the update. The user
// navigates with ↑/↓ to scroll, presses y/enter to proceed, or n/q/esc to cancel.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
)

// ReviewScreen renders the full deployment plan and captures the user's go/no-go decision.
// It is shown as an overlay during screenRunning; it is not a navigation step.
type ReviewScreen struct {
	plan        domain.Plan
	lines       []reviewLine // pre-rendered, scrollable content
	scrollOffset int
	visibleLines int // number of content lines visible at once
	done         bool
	answer       domain.ConfirmAnswer
	width        int
	height       int
	styles       Styles
}

// reviewLine is one pre-rendered line with an associated lipgloss style.
type reviewLine struct {
	text  string
	style string // one of: "title", "success", "warning", "error", "muted", "body", "border"
}

// NewReviewScreen creates the plan-review overlay for the given plan.
func NewReviewScreen(p domain.Plan, width, height int, styles Styles) *ReviewScreen {
	s := &ReviewScreen{
		plan:   p,
		width:  width,
		height: height,
		styles: styles,
	}
	s.lines = buildReviewLines(p)
	s.visibleLines = reviewVisibleLines(height)
	return s
}

// reviewVisibleLines computes the scrollable area height.
func reviewVisibleLines(totalHeight int) int {
	// title + subtitle + border + backup-warning + help = 6 fixed lines (was 5)
	h := totalHeight - 6
	if h < 3 {
		return 3
	}
	return h
}

// buildReviewLines constructs the pre-rendered line list from the plan, grouping items by action.
func buildReviewLines(p domain.Plan) []reviewLine {
	var lines []reviewLine

	add := func(text, style string) {
		lines = append(lines, reviewLine{text: text, style: style})
	}

	// Summary header.
	counts := p.Counts()
	mode := "Deploy new"
	if p.Mode == domain.ModeUpdate {
		mode = "Update"
	}
	add(fmt.Sprintf("Mode: %s  Harness: %s  Workspace: %s",
		mode, p.Harness.DisplayName, p.WorkspacePath), "muted")
	add("", "body")

	// Count line.
	var countParts []string
	if n := counts[domain.ActionCreate]; n > 0 {
		countParts = append(countParts, fmt.Sprintf("%d to create", n))
	}
	if n := counts[domain.ActionUpdate]; n > 0 {
		countParts = append(countParts, fmt.Sprintf("%d to update", n))
	}
	if n := counts[domain.ActionConflict]; n > 0 {
		countParts = append(countParts, fmt.Sprintf("%d locally modified", n))
	}
	if n := counts[domain.ActionUnchanged]; n > 0 {
		countParts = append(countParts, fmt.Sprintf("%d unchanged", n))
	}

	if p.Empty() {
		add("Nothing to do — all files are already up to date.", "muted")
		// Fall through to still render the unchanged items below.
	}

	add(strings.Join(countParts, "  ·  "), "body")
	add(strings.Repeat("─", 40), "border")

	// Items grouped by action priority: conflicts first, then creates, updates, unchanged.
	groups := []struct {
		action domain.PlanAction
		label  string
		style  string
	}{
		{domain.ActionConflict, "CONFLICT", "warning"},
		{domain.ActionCreate, "CREATE", "success"},
		{domain.ActionUpdate, "UPDATE", "success"},
		{domain.ActionUnchanged, "UNCHANGED", "muted"},
	}

	for _, g := range groups {
		var groupItems []domain.PlanItem
		for _, item := range p.Items {
			if item.Action == g.action {
				groupItems = append(groupItems, item)
			}
		}
		if len(groupItems) == 0 {
			continue
		}
		for _, item := range groupItems {
			badge := fmt.Sprintf("[%s]", g.label)
			line := fmt.Sprintf("%-14s %s:%s → %s",
				badge,
				item.Ref.Kind,
				item.Ref.Key,
				item.TargetPath,
			)
			add(line, g.style)

			if item.Reason != "" {
				add("               "+item.Reason, "muted")
			}

			// For updates, list each version delta that drove the update.
			for _, delta := range item.Stale {
				add(fmt.Sprintf("               %s: %s → %s",
					delta.Field, delta.Deployed, delta.Source), "muted")
			}

			// For conflicts, remind the user of the local modification.
			if item.Action == domain.ActionConflict && item.Conflict != nil {
				if item.Conflict.ManifestMissing {
					add("               (manifest missing; file was not recorded as deployed)", "warning")
				} else {
					add(fmt.Sprintf("               recorded: %s  current: %s",
						shortHash(item.Conflict.RecordedHash),
						shortHash(item.Conflict.CurrentHash)), "warning")
				}
			}
		}
	}

	// Gaps summary.
	if len(p.Gaps) > 0 {
		add("", "body")
		add(fmt.Sprintf("%d gap(s) will be recorded as follow-up TODO items:", len(p.Gaps)), "warning")
		for _, g := range p.Gaps {
			add(fmt.Sprintf("  • %s: %s", g.Kind, g.Subject), "muted")
		}
	}

	return lines
}

// shortHash abbreviates a "sha256:..." hash to "sha256:xxxxxxxx…" for display.
func shortHash(h string) string {
	if len(h) <= 16 {
		return h
	}
	return h[:16] + "…"
}

// Update processes a tea.Msg. Returns true when the screen has a final answer.
func (s *ReviewScreen) Update(msg tea.Msg) bool {
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
	case "y", "enter":
		s.answer = domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}
		s.done = true
		return true
	case "n", "q", "esc":
		s.answer = domain.ConfirmAnswer{Status: domain.Answered, Confirm: false}
		s.done = true
		return true
	}
	return false
}

// Done reports whether the screen has produced a final answer.
func (s *ReviewScreen) Done() bool { return s.done }

// Answer returns the final ConfirmAnswer. Only valid when Done() is true.
func (s *ReviewScreen) Answer() domain.ConfirmAnswer { return s.answer }

// View renders the plan review overlay.
func (s *ReviewScreen) View() string {
	var sb strings.Builder

	sb.WriteString(s.styles.Title.Width(s.width).Render("Review Deployment Plan"))
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Subtitle.Width(s.width).Render("Review all planned actions before any file is written."))
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
		rendered := s.renderLine(line)
		sb.WriteString(rendered)
		sb.WriteByte('\n')
	}

	// Scroll position indicator.
	if len(s.lines) > s.visibleLines {
		total := len(s.lines)
		shown := end - start
		sb.WriteString(s.styles.Muted.Width(s.width).Render(
			fmt.Sprintf("── %d–%d of %d lines ──", start+1, start+shown, total),
		))
		sb.WriteByte('\n')
	}

	sb.WriteString(s.styles.Warning.Width(s.width).Render(
		"WARNING: Proceeding will overwrite existing workspace files. Back up any important changes first.",
	))
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Help.Width(s.width).Render(
		"↑/k up  ↓/j down  y/enter proceed  n/q/esc cancel",
	))
	return sb.String()
}

// renderLine applies the appropriate style from the Styles set to one review line.
func (s *ReviewScreen) renderLine(line reviewLine) string {
	switch line.style {
	case "title":
		return s.styles.Title.Width(s.width).Render(line.text)
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

// Resize updates the overlay dimensions and recomputes the visible line count.
func (s *ReviewScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.visibleLines = reviewVisibleLines(height)
	// Clamp scroll offset to the new bounds.
	maxOffset := len(s.lines) - s.visibleLines
	if maxOffset < 0 {
		maxOffset = 0
	}
	if s.scrollOffset > maxOffset {
		s.scrollOffset = maxOffset
	}
}

// Reset clears the done/answer and resets scroll to the top.
func (s *ReviewScreen) Reset() {
	s.done = false
	s.answer = domain.ConfirmAnswer{}
	s.scrollOffset = 0
}
