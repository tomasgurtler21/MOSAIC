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
	plan         domain.Plan
	lines        []reviewLine // pre-rendered, scrollable content
	physHeights  []int        // physical terminal row height of each reviewLine at current width
	scrollOffset int
	visibleLines int // fallback line count; physical-row logic uses computeContentBudget instead
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
	s.recomputePhysHeights()
	return s
}

// reviewVisibleLines computes the scrollable area height using a fixed chrome estimate.
// It is kept for backward compatibility; View() uses computeContentBudget() for accuracy.
func reviewVisibleLines(totalHeight int) int {
	// title + subtitle + border + indicator + help = 5 fixed lines (assumes no chrome wrapping)
	h := totalHeight - 5
	if h < 3 {
		return 3
	}
	return h
}

// physicalHeight returns the number of physical terminal rows a rendered string occupies.
// lipgloss Width(w).Render(text) may insert newline characters when text exceeds w columns;
// counting those internal newlines gives the true row count.
func physicalHeight(rendered string) int {
	n := strings.Count(rendered, "\n") + 1
	if n < 1 {
		return 1
	}
	return n
}

// recomputePhysHeights re-renders every content line at the current width and records
// how many physical terminal rows each occupies. This must be called whenever the width
// changes so that scroll bounds and the content budget remain accurate.
func (s *ReviewScreen) recomputePhysHeights() {
	s.physHeights = make([]int, len(s.lines))
	for i, line := range s.lines {
		s.physHeights[i] = physicalHeight(s.renderLine(line))
	}
}

// computeContentBudget returns the number of physical terminal rows available for
// scrollable content. It renders each fixed chrome element at the current width to
// measure its true height (accounting for wrapping), then subtracts all chrome rows
// and one reserved row for the scroll-position indicator from the terminal height.
func (s *ReviewScreen) computeContentBudget() int {
	titleH := physicalHeight(s.styles.Title.Width(s.width).Render(reviewTitle))
	subtitleH := physicalHeight(s.styles.Subtitle.Width(s.width).Render(reviewSubtitle))
	borderH := physicalHeight(s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width)))
	helpH := physicalHeight(s.styles.Help.Width(s.width).Render(reviewHelpText))
	const indicatorRows = 1
	budget := s.height - titleH - subtitleH - borderH - helpH - indicatorRows
	if budget < 1 {
		return 1
	}
	return budget
}

// computeMaxScrollOffset returns the largest scroll offset at which there is still
// enough content below to fill the visible content area. Prevents the user from
// scrolling past the last meaningful content.
func (s *ReviewScreen) computeMaxScrollOffset() int {
	budget := s.computeContentBudget()
	accum := 0
	for i := len(s.physHeights) - 1; i >= 0; i-- {
		accum += s.physHeights[i]
		if accum >= budget {
			return i
		}
	}
	return 0
}

// totalContentPhysicalRows returns the sum of physical row heights for all content lines.
func (s *ReviewScreen) totalContentPhysicalRows() int {
	total := 0
	for _, h := range s.physHeights {
		total += h
	}
	return total
}

// reviewTitle, reviewSubtitle, and reviewHelpText are the fixed strings rendered as chrome
// outside the scrollable content area. They are defined here so computeContentBudget and
// View use identical text when measuring chrome heights.
const reviewTitle = "Review Deployment Plan"
const reviewSubtitle = "Review all planned actions before any file is written."
const reviewHelpText = "↑/k up  ↓/j down  y/enter proceed  n/q/esc cancel"

// buildReviewLines constructs the pre-rendered line list from the plan, grouping items by action.
func buildReviewLines(p domain.Plan) []reviewLine {
	var lines []reviewLine

	add := func(text, style string) {
		lines = append(lines, reviewLine{text: text, style: style})
	}

	// Summary header.
	counts := p.Counts()
	add(fmt.Sprintf("Mode: %s  Harness: %s  Workspace: %s",
		modeDisplayName(p.Mode), p.Harness.DisplayName, p.WorkspacePath), "muted")
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

			// Show the Reason line only when there are no per-field delta lines to display.
			// When Stale deltas are present, the per-delta loop below already shows the
			// version changes with before/after values; the Reason string would duplicate that.
			if item.Reason != "" && len(item.Stale) == 0 {
				add("               "+item.Reason, "muted")
			}

			// For updates, list each version delta that drove the update.
			for _, delta := range item.Stale {
				add("               "+formatVersionDelta(delta), "muted")
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
			add(formatFollowUpLine(string(g.Kind), g.Subject, g.Owner), "muted")
		}
	}

	return lines
}

// modeDisplayName maps a RunMode to the label shown in the plan review's summary header.
// It covers every declared mode. An unmapped mode returns string(mode) verbatim, so an
// unhandled mode is visibly wrong rather than silently masquerading as a real one.
func modeDisplayName(mode domain.RunMode) string {
	switch mode {
	case domain.ModeDeployWorkspace:
		return "Deploy workspace"
	case domain.ModeUpdateWorkspace:
		return "Update workspace"
	case domain.ModeUpdateWorkflows:
		return "Update workflows"
	case domain.ModeDeployAgents:
		return "Deploy agents"
	case domain.ModeDeployHooks:
		return "Deploy hooks"
	case domain.ModePromoteToGeneric:
		return "Promote to generic"
	case domain.ModeTransformHarness:
		return "Transform harness"
	default:
		return string(mode)
	}
}

// versionDeltaLabel returns the user-facing label for a VersionDelta.Field value.
// Fields with a well-known internal name are translated to a descriptive label.
// Fields with the "workflow:" prefix are translated to "workflow <id> version".
// Any other field falls back to the raw field name so new fields degrade gracefully.
func versionDeltaLabel(field string) string {
	switch field {
	case "version":
		return "catalog version"
	case "harness_version":
		return "harness version"
	case "injections_version":
		return "harness injection version (body tag)"
	case "tool_mappings_version":
		return "tool mappings hash"
	case "bundle_version":
		return "bundle version"
	case "orchestrator_injections_version":
		return "orchestrator injection version"
	case "protocol_version":
		return "harness protocol version"
	}
	const workflowPrefix = "workflow:"
	if strings.HasPrefix(field, workflowPrefix) {
		id := strings.TrimPrefix(field, workflowPrefix)
		return "workflow " + id + " version"
	}
	return field
}

// formatVersionDelta renders one staleness comparison for the plan review.
//
// An empty Source is the shared, documented representation of a removal. It renders as the
// word "deleted" rather than a bare trailing arrow. Every other delta renders verbatim.
//
//	{Label}: {Deployed} → {Source}     when Source != ""
//	{Label}: {Deployed} → deleted      when Source == ""
func formatVersionDelta(delta domain.VersionDelta) string {
	source := delta.Source
	if source == "" {
		source = "deleted"
	}
	return fmt.Sprintf("%s: %s → %s", versionDeltaLabel(delta.Field), delta.Deployed, source)
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
		maxOffset := s.computeMaxScrollOffset()
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
//
// The scrollable content area is sized by counting physical terminal rows (accounting for
// lipgloss line-wrapping of long lines at the current width), not by counting logical
// reviewLine entries. Chrome elements outside the content area — title, subtitle, border,
// indicator, and help — are rendered first so their physical heights can be measured and
// subtracted from the terminal height to yield the correct content budget.
func (s *ReviewScreen) View() string {
	var sb strings.Builder

	sb.WriteString(s.styles.Title.Width(s.width).Render(reviewTitle))
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Subtitle.Width(s.width).Render(reviewSubtitle))
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width)))
	sb.WriteByte('\n')

	// Scrollable content window: accumulate physical rows until the budget is exhausted.
	contentBudget := s.computeContentBudget()
	start := s.scrollOffset
	rowsUsed := 0
	for i := start; i < len(s.lines); i++ {
		h := s.physHeights[i]
		if rowsUsed+h > contentBudget {
			break
		}
		sb.WriteString(s.renderLine(s.lines[i]))
		sb.WriteByte('\n')
		rowsUsed += h
	}

	// Scroll position indicator: shown when total physical content exceeds the budget,
	// meaning some lines are hidden above or below. Reports physical row positions so the
	// user knows how much content remains to scroll through.
	totalPhys := s.totalContentPhysicalRows()
	if totalPhys > contentBudget {
		physBefore := 0
		for i := 0; i < start && i < len(s.physHeights); i++ {
			physBefore += s.physHeights[i]
		}
		physStart := physBefore + 1
		physEnd := physBefore + rowsUsed
		if physEnd < physStart {
			physEnd = physStart
		}
		sb.WriteString(s.styles.Muted.Width(s.width).Render(
			fmt.Sprintf("── %d–%d of %d lines ──", physStart, physEnd, totalPhys),
		))
		sb.WriteByte('\n')
	}

	sb.WriteString(s.styles.Help.Width(s.width).Render(reviewHelpText))
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

// Resize updates the overlay dimensions and recomputes physical line heights.
// After a resize, line wrapping changes (narrower widths wrap more), so physical heights
// for all content lines are recomputed before clamping the scroll offset.
func (s *ReviewScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.visibleLines = reviewVisibleLines(height)
	s.recomputePhysHeights()
	// Clamp scroll offset to the new physical-row-based bounds.
	maxOffset := s.computeMaxScrollOffset()
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
