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

// View renders the per-file transform report. It always shows the source/target harness,
// the summary counts, and each file's status. A dry-run prefix makes non-writes explicit.
func (s *TransformSummaryScreen) View() string {
	var sb strings.Builder
	r := s.result

	dryPrefix := ""
	if r.DryRun {
		dryPrefix = "dry run: "
	}

	header := fmt.Sprintf("%stransform complete\nsource: %s → %s\ninput: %s",
		dryPrefix, r.SourceHarnessID, r.TargetHarnessID, r.InputPath)
	sb.WriteString(header)

	sb.WriteString(fmt.Sprintf("\ntransformed: %d  skipped-mismatch: %d  skipped-not-agent: %d  failed: %d",
		r.Transformed, r.SkippedMismatch, r.SkippedNotAgent, r.Failed))

	for _, f := range r.Files {
		switch f.Status {
		case app.StatusTransformed:
			if r.DryRun {
				sb.WriteString(fmt.Sprintf("\n  [dry-run] %s → %s", f.SourcePath, f.DestinationPath))
			} else {
				sb.WriteString(fmt.Sprintf("\n  transformed: %s → %s", f.SourcePath, f.DestinationPath))
			}
			if f.Warning != "" {
				sb.WriteString(fmt.Sprintf("\n    warning: %s", f.Warning))
			}
			if len(f.Tools) > 0 {
				sb.WriteString(fmt.Sprintf("\n    tools: %s", strings.Join(f.Tools, ", ")))
			}
			if len(f.CarriedVerbatimTools) > 0 {
				sb.WriteString(fmt.Sprintf("\n    carried verbatim tools: %s", strings.Join(f.CarriedVerbatimTools, ", ")))
			}
			for _, sf := range f.StrippedFields {
				if len(sf.Values) > 0 {
					sb.WriteString(fmt.Sprintf("\n    stripped field: %s (%s): %s", sf.Key, string(sf.Reason), strings.Join(sf.Values, ", ")))
				} else {
					sb.WriteString(fmt.Sprintf("\n    stripped field: %s (%s)", sf.Key, string(sf.Reason)))
				}
			}
		case app.StatusSkippedMismatch:
			sb.WriteString(fmt.Sprintf("\n  skipped (mismatch): %s — %s", f.SourcePath, f.Reason))
		case app.StatusSkippedNotAgent:
			sb.WriteString(fmt.Sprintf("\n  skipped (not agent): %s — %s", f.SourcePath, f.Reason))
		case app.StatusFailed:
			sb.WriteString(fmt.Sprintf("\n  failed: %s — %s", f.SourcePath, f.Reason))
		}
	}

	sb.WriteString("\npress q or enter to exit")

	return s.styles.Success.Width(s.width).Render(sb.String())
}

// Resize adjusts the screen to new terminal dimensions.
func (s *TransformSummaryScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
