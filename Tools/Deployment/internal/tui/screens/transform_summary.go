package screens

// transform_summary.go declares TransformSummaryScreen, the completion screen shown after
// a transform run. It mirrors PromoteSummaryScreen's method set so rootModel drives both
// the same way.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/app"
)

// transformLine is one rendered line of the transform summary together with the
// style role it must be drawn in. Mirrors summaryLine in summary.go.
type transformLine struct {
	text  string
	style string // "success" | "warning" | "error" | "muted" | "help" | "" (body)
}

// TransformSummaryScreen renders the outcome of a transform run. It mirrors
// PromoteSummaryScreen's method set so rootModel drives both the same way, but reads a
// TransformHarnessResult rather than a PromoteResult.
type TransformSummaryScreen struct {
	result app.TransformHarnessResult
	done   bool
	width  int
	height int
	styles Styles
}

// NewTransformSummaryScreen builds the screen from a completed transform result.
func NewTransformSummaryScreen(result app.TransformHarnessResult, width, height int, styles Styles) *TransformSummaryScreen {
	return &TransformSummaryScreen{
		result: result,
		width:  width,
		height: height,
		styles: styles,
	}
}

// Update handles key input and reports whether the user dismissed the screen.
func (s *TransformSummaryScreen) Update(msg tea.Msg) bool {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "q", "enter", "esc":
			s.done = true
		}
	}
	return s.done
}

// Done reports that the user dismissed the screen.
func (s *TransformSummaryScreen) Done() bool { return s.done }

// overallOutcomeStyle returns the style tag for the overall run outcome: "error" when any
// file failed, "warning" when files were skipped but none failed, "success" otherwise.
func (s *TransformSummaryScreen) overallOutcomeStyle() string {
	r := s.result
	if r.Failed > 0 {
		return "error"
	}
	if r.SkippedMismatch+r.SkippedNotAgent > 0 {
		return "warning"
	}
	return "success"
}

// View renders the per-file transform report. Each line is rendered individually in
// the style appropriate to its status, following the sibling renderSummaryLine pattern
// from summary.go. A dry-run prefix makes non-writes explicit.
func (s *TransformSummaryScreen) View() string {
	r := s.result
	outcome := s.overallOutcomeStyle()

	dryPrefix := ""
	if r.DryRun {
		dryPrefix = "dry run: "
	}

	var lines []transformLine

	// Header lines styled by overall outcome.
	lines = append(lines, transformLine{
		text:  fmt.Sprintf("%stransform complete", dryPrefix),
		style: outcome,
	})
	lines = append(lines, transformLine{
		text:  fmt.Sprintf("source: %s → %s", r.SourceHarnessID, r.TargetHarnessID),
		style: outcome,
	})
	lines = append(lines, transformLine{
		text:  fmt.Sprintf("input: %s", r.InputPath),
		style: outcome,
	})

	// Counts line styled by overall outcome.
	lines = append(lines, transformLine{
		text: fmt.Sprintf("transformed: %d  skipped-mismatch: %d  skipped-not-agent: %d  failed: %d",
			r.Transformed, r.SkippedMismatch, r.SkippedNotAgent, r.Failed),
		style: outcome,
	})

	// Per-file lines styled by individual file status.
	for _, f := range r.Files {
		switch f.Status {
		case app.StatusTransformed:
			var fileText string
			if r.DryRun {
				fileText = fmt.Sprintf("  [dry-run] %s → %s", f.SourcePath, f.DestinationPath)
			} else {
				fileText = fmt.Sprintf("  transformed: %s → %s", f.SourcePath, f.DestinationPath)
			}
			lines = append(lines, transformLine{text: fileText, style: "success"})
			if f.Warning != "" {
				lines = append(lines, transformLine{
					text:  fmt.Sprintf("    warning: %s", f.Warning),
					style: "warning",
				})
			}
			if len(f.Tools) > 0 {
				lines = append(lines, transformLine{
					text:  fmt.Sprintf("    tools: %s", strings.Join(f.Tools, ", ")),
					style: "muted",
				})
			}
			if len(f.CarriedVerbatimTools) > 0 {
				lines = append(lines, transformLine{
					text:  fmt.Sprintf("    carried verbatim tools: %s", strings.Join(f.CarriedVerbatimTools, ", ")),
					style: "muted",
				})
			}
			for _, sf := range f.StrippedFields {
				if len(sf.Values) > 0 {
					lines = append(lines, transformLine{
						text:  fmt.Sprintf("    stripped field: %s (%s): %s", sf.Key, string(sf.Reason), strings.Join(sf.Values, ", ")),
						style: "muted",
					})
				} else {
					lines = append(lines, transformLine{
						text:  fmt.Sprintf("    stripped field: %s (%s)", sf.Key, string(sf.Reason)),
						style: "muted",
					})
				}
			}
		case app.StatusSkippedMismatch:
			lines = append(lines, transformLine{
				text:  fmt.Sprintf("  skipped (mismatch): %s — %s", f.SourcePath, f.Reason),
				style: "warning",
			})
		case app.StatusSkippedNotAgent:
			lines = append(lines, transformLine{
				text:  fmt.Sprintf("  skipped (not agent): %s — %s", f.SourcePath, f.Reason),
				style: "warning",
			})
		case app.StatusFailed:
			lines = append(lines, transformLine{
				text:  fmt.Sprintf("  failed: %s — %s", f.SourcePath, f.Reason),
				style: "error",
			})
		}
	}

	// Exit hint in the help style.
	lines = append(lines, transformLine{
		text:  "press q or enter to exit",
		style: "help",
	})

	// Render each line individually with its own style role and join with newlines.
	var sb strings.Builder
	for i, line := range lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(s.renderTransformLine(line))
	}
	return sb.String()
}

// renderTransformLine applies the named style role to one transform summary line,
// following the sibling renderSummaryLine pattern from summary.go.
func (s *TransformSummaryScreen) renderTransformLine(line transformLine) string {
	switch line.style {
	case "success":
		return s.styles.Success.Width(s.width).Render(line.text)
	case "warning":
		return s.styles.Warning.Width(s.width).Render(line.text)
	case "error":
		return s.styles.Error.Width(s.width).Render(line.text)
	case "muted":
		return s.styles.Muted.Width(s.width).Render(line.text)
	case "help":
		return s.styles.Help.Width(s.width).Render(line.text)
	default:
		return s.styles.Body.Width(s.width).Render(line.text)
	}
}

// Resize adjusts the screen to new terminal dimensions.
func (s *TransformSummaryScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
