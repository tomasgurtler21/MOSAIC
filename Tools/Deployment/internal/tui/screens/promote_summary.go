package screens

// promote_summary.go declares PromoteSummaryScreen, the completion screen shown after a
// successful promote run. It is the promote-mode counterpart of SummaryScreen, which remains
// RunSummary-only.

import (
	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/app"
)

// PromoteSummaryScreen renders the outcome of a promote run. It mirrors SummaryScreen's
// method set so rootModel drives both the same way, but reads a PromoteResult rather than a
// domain.RunSummary.
type PromoteSummaryScreen struct {
	result app.PromoteResult
	done   bool
	width  int
	height int
	styles Styles
}

// NewPromoteSummaryScreen builds the screen from a completed promote result.
func NewPromoteSummaryScreen(result app.PromoteResult, width, height int, styles Styles) *PromoteSummaryScreen {
	return &PromoteSummaryScreen{
		result: result,
		width:  width,
		height: height,
		styles: styles,
	}
}

// Update handles key input and reports whether the user dismissed the screen.
func (s *PromoteSummaryScreen) Update(msg tea.Msg) bool {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "q", "enter", "esc":
			s.done = true
		}
	}
	return s.done
}

// Done reports that the user dismissed the screen.
func (s *PromoteSummaryScreen) Done() bool { return s.done }

// View renders the screen. It always shows the source path, the destination path, the agent
// key, the assigned numeric id, and the category, and states explicitly that nothing was
// written when the result is a dry run.
func (s *PromoteSummaryScreen) View() string {
	if s.result.DryRun {
		return s.styles.Success.Width(s.width).Render(
			"Promote (dry run)\nSource: " + s.result.SourcePath +
				"\nDestination: " + s.result.DestinationPath + " (not written)" +
				"\nKey: " + s.result.Key +
				"\nID: " + s.result.NumericID,
		)
	}
	return s.styles.Success.Width(s.width).Render(
		"Promote complete\nSource: " + s.result.SourcePath +
			"\nDestination: " + s.result.DestinationPath +
			"\nKey: " + s.result.Key +
			"\nID: " + s.result.NumericID +
			"\nCategory: " + s.result.Category,
	)
}

// Resize adjusts the screen to new terminal dimensions.
func (s *PromoteSummaryScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
