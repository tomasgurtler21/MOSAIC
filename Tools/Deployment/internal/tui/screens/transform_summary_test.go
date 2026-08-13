package screens_test

// transform_summary_test.go verifies TransformSummaryScreen: content rendering
// (information parity), per-file status styling, overall-outcome header styling,
// sub-line styling, and keyboard dismissal.
//
// The test suite is split into two categories:
//
//   - Content tests: use plainStyles() and verify that View() reports the same
//     text information before and after the styling restructure. These tests pass
//     with the current implementation.
//
//   - Style tests: use coloredStyles() which forces a TrueColor ANSI renderer,
//     and verify which lipgloss style role is applied to each line. The current
//     implementation wraps the entire output in a single Success.Render call, so
//     these tests fail until per-line rendering is implemented.

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Style fixtures
// ---------------------------------------------------------------------------

// coloredStyles returns a Styles where each role has a distinct TrueColor
// foreground color. The renderer explicitly sets the TrueColor profile using
// SetColorProfile so that ANSI escape sequences are always emitted regardless
// of the test environment's terminal capabilities. This makes it possible to
// assert which style role was applied to a given line by checking for the
// role's ANSI-encoded text in the View() output.
func coloredStyles() screens.Styles {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.TrueColor)
	return screens.Styles{
		Success: r.NewStyle().Foreground(lipgloss.Color("#00ff00")), // bright green
		Error:   r.NewStyle().Foreground(lipgloss.Color("#ff0000")), // bright red
		Warning: r.NewStyle().Foreground(lipgloss.Color("#ffff00")), // bright yellow
		Muted:   r.NewStyle().Foreground(lipgloss.Color("#808080")), // gray
		Help:    r.NewStyle().Foreground(lipgloss.Color("#c0c0c0")), // silver
		Body:    r.NewStyle(),
	}
}

// ---------------------------------------------------------------------------
// Result fixtures
// ---------------------------------------------------------------------------

const (
	tsSource = "claude-code"
	tsTarget = "opencode"
	tsInput  = "/agents"
)

// tsTransformedResult returns a result with one successfully transformed file.
func tsTransformedResult() app.TransformHarnessResult {
	return app.TransformHarnessResult{
		SourceHarnessID: tsSource,
		TargetHarnessID: tsTarget,
		InputPath:       tsInput,
		Transformed:     1,
		Files: []app.TransformFileOutcome{
			{
				SourcePath:      "/agents/my-agent.md",
				DestinationPath: "/out/my-agent.md",
				Status:          app.StatusTransformed,
			},
		},
	}
}

// tsFailedResult returns a result with one failed file and no transformed files.
func tsFailedResult() app.TransformHarnessResult {
	return app.TransformHarnessResult{
		SourceHarnessID: tsSource,
		TargetHarnessID: tsTarget,
		InputPath:       tsInput,
		Failed:          1,
		Files: []app.TransformFileOutcome{
			{
				SourcePath: "/agents/my-agent.md",
				Status:     app.StatusFailed,
				Reason:     "destination file already exists",
			},
		},
	}
}

// tsMixedResult returns a result with one transformed file and one failed file.
func tsMixedResult() app.TransformHarnessResult {
	return app.TransformHarnessResult{
		SourceHarnessID: tsSource,
		TargetHarnessID: tsTarget,
		InputPath:       tsInput,
		Transformed:     1,
		Failed:          1,
		Files: []app.TransformFileOutcome{
			{
				SourcePath:      "/agents/ok-agent.md",
				DestinationPath: "/out/ok-agent.md",
				Status:          app.StatusTransformed,
			},
			{
				SourcePath: "/agents/fail-agent.md",
				Status:     app.StatusFailed,
				Reason:     "destination file already exists",
			},
		},
	}
}

// tsSkippedMismatchResult returns a result with one file skipped due to a
// source harness mismatch (the file belongs to a different source harness).
func tsSkippedMismatchResult() app.TransformHarnessResult {
	return app.TransformHarnessResult{
		SourceHarnessID: tsSource,
		TargetHarnessID: tsTarget,
		InputPath:       tsInput,
		SkippedMismatch: 1,
		Files: []app.TransformFileOutcome{
			{
				SourcePath: "/agents/wrong-harness-agent.md",
				Status:     app.StatusSkippedMismatch,
				Reason:     "source harness mismatch",
			},
		},
	}
}

// tsSkippedNotAgentResult returns a result with one file skipped because it
// is not a MOSAIC agent document.
func tsSkippedNotAgentResult() app.TransformHarnessResult {
	return app.TransformHarnessResult{
		SourceHarnessID: tsSource,
		TargetHarnessID: tsTarget,
		InputPath:       tsInput,
		SkippedNotAgent: 1,
		Files: []app.TransformFileOutcome{
			{
				SourcePath: "/agents/plain-file.md",
				Status:     app.StatusSkippedNotAgent,
				Reason:     "not a MOSAIC agent",
			},
		},
	}
}

// tsRichTransformedResult returns a result with one transformed file that
// carries all per-file detail sub-lines: a warning, tools, carried verbatim
// tools, and a stripped field. Used by sub-line content and style tests.
func tsRichTransformedResult() app.TransformHarnessResult {
	return app.TransformHarnessResult{
		SourceHarnessID: tsSource,
		TargetHarnessID: tsTarget,
		InputPath:       tsInput,
		Transformed:     1,
		Files: []app.TransformFileOutcome{
			{
				SourcePath:           "/agents/my-agent.md",
				DestinationPath:      "/out/my-agent.md",
				Status:               app.StatusTransformed,
				Warning:              "indeterminate harness match",
				Tools:                []string{"Bash", "Read"},
				CarriedVerbatimTools: []string{"Write"},
				StrippedFields: []app.StrippedField{
					{Key: "mcpServers", Values: []string{"server1"}, Reason: app.StripReasonUnmappedDivertedValue},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Content / information-parity tests
// ---------------------------------------------------------------------------

// TestTransformSummaryScreen_View_ShowsSourceAndTargetHarness verifies that the
// view includes both the source and target harness IDs so the operator knows
// which conversion was performed.
func TestTransformSummaryScreen_View_ShowsSourceAndTargetHarness(t *testing.T) {
	s := screens.NewTransformSummaryScreen(tsTransformedResult(), 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, tsSource) {
		t.Errorf("view does not contain source harness %q", tsSource)
	}
	if !strings.Contains(view, tsTarget) {
		t.Errorf("view does not contain target harness %q", tsTarget)
	}
}

// TestTransformSummaryScreen_View_ShowsInputPath verifies that the input path
// appears so the operator can confirm which directory or file was processed.
func TestTransformSummaryScreen_View_ShowsInputPath(t *testing.T) {
	s := screens.NewTransformSummaryScreen(tsTransformedResult(), 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, tsInput) {
		t.Errorf("view does not contain input path %q", tsInput)
	}
}

// TestTransformSummaryScreen_View_ShowsCountsLine verifies that the summary counts
// (transformed, skipped-mismatch, skipped-not-agent, failed) appear in the view.
// The counts let the operator know the batch outcome at a glance.
func TestTransformSummaryScreen_View_ShowsCountsLine(t *testing.T) {
	result := tsMixedResult() // 1 transformed, 1 failed
	s := screens.NewTransformSummaryScreen(result, 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "transformed: 1") {
		t.Errorf("view does not contain transformed count; got: %s", view)
	}
	if !strings.Contains(view, "failed: 1") {
		t.Errorf("view does not contain failed count; got: %s", view)
	}
}

// TestTransformSummaryScreen_View_ShowsTransformedFileLine verifies that a
// transformed file's source and destination paths appear so the operator can
// verify what was written where.
func TestTransformSummaryScreen_View_ShowsTransformedFileLine(t *testing.T) {
	result := tsTransformedResult()
	s := screens.NewTransformSummaryScreen(result, 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "/agents/my-agent.md") {
		t.Errorf("view does not contain transformed source path")
	}
	if !strings.Contains(view, "/out/my-agent.md") {
		t.Errorf("view does not contain transformed destination path")
	}
}

// TestTransformSummaryScreen_View_ShowsFailedFileLine verifies that a failed
// file's path and reason appear so the operator can investigate and re-run.
func TestTransformSummaryScreen_View_ShowsFailedFileLine(t *testing.T) {
	result := tsFailedResult()
	s := screens.NewTransformSummaryScreen(result, 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "/agents/my-agent.md") {
		t.Errorf("view does not contain failed file path")
	}
	if !strings.Contains(view, "destination file already exists") {
		t.Errorf("view does not contain failure reason")
	}
}

// TestTransformSummaryScreen_View_ShowsSkippedMismatchFileLine verifies that a
// skipped (mismatch) file's path and reason appear in the view.
func TestTransformSummaryScreen_View_ShowsSkippedMismatchFileLine(t *testing.T) {
	result := tsSkippedMismatchResult()
	s := screens.NewTransformSummaryScreen(result, 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "/agents/wrong-harness-agent.md") {
		t.Errorf("view does not contain skipped (mismatch) file path")
	}
	if !strings.Contains(view, "source harness mismatch") {
		t.Errorf("view does not contain skip reason")
	}
}

// TestTransformSummaryScreen_View_ShowsSkippedNotAgentFileLine verifies that a
// skipped (not agent) file's path and reason appear in the view.
func TestTransformSummaryScreen_View_ShowsSkippedNotAgentFileLine(t *testing.T) {
	result := tsSkippedNotAgentResult()
	s := screens.NewTransformSummaryScreen(result, 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "/agents/plain-file.md") {
		t.Errorf("view does not contain skipped (not agent) file path")
	}
	if !strings.Contains(view, "not a MOSAIC agent") {
		t.Errorf("view does not contain skip reason")
	}
}

// TestTransformSummaryScreen_View_ShowsWarningSubLine verifies that a per-file
// warning (the indeterminate harness-match note) appears as a sub-line under the
// transformed entry so the operator is alerted to the potential mis-identification.
func TestTransformSummaryScreen_View_ShowsWarningSubLine(t *testing.T) {
	result := tsRichTransformedResult()
	s := screens.NewTransformSummaryScreen(result, 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "indeterminate harness match") {
		t.Errorf("view does not contain per-file warning sub-line text")
	}
}

// TestTransformSummaryScreen_View_ShowsToolsSubLine verifies that the
// target-harness tool names written to the output appear as a sub-line under the
// transformed entry.
func TestTransformSummaryScreen_View_ShowsToolsSubLine(t *testing.T) {
	result := tsRichTransformedResult()
	s := screens.NewTransformSummaryScreen(result, 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Bash") {
		t.Errorf("view does not contain tools sub-line; tool %q missing", "Bash")
	}
	if !strings.Contains(view, "Read") {
		t.Errorf("view does not contain tools sub-line; tool %q missing", "Read")
	}
}

// TestTransformSummaryScreen_View_ShowsCarriedVerbatimToolsSubLine verifies that
// custom tools kept under their original name appear as a sub-line.
func TestTransformSummaryScreen_View_ShowsCarriedVerbatimToolsSubLine(t *testing.T) {
	result := tsRichTransformedResult()
	s := screens.NewTransformSummaryScreen(result, 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Write") {
		t.Errorf("view does not contain carried verbatim tools sub-line; tool %q missing", "Write")
	}
}

// TestTransformSummaryScreen_View_ShowsStrippedFieldSubLine verifies that stripped
// frontmatter fields appear as a sub-line with their key and reason so the operator
// can re-add them manually.
func TestTransformSummaryScreen_View_ShowsStrippedFieldSubLine(t *testing.T) {
	result := tsRichTransformedResult()
	s := screens.NewTransformSummaryScreen(result, 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "mcpServers") {
		t.Errorf("view does not contain stripped field key %q", "mcpServers")
	}
	if !strings.Contains(view, string(app.StripReasonUnmappedDivertedValue)) {
		t.Errorf("view does not contain strip reason %q", app.StripReasonUnmappedDivertedValue)
	}
}

// TestTransformSummaryScreen_View_ShowsExitHint verifies that the "press q or
// enter to exit" hint appears so the operator knows how to close the screen.
func TestTransformSummaryScreen_View_ShowsExitHint(t *testing.T) {
	s := screens.NewTransformSummaryScreen(tsTransformedResult(), 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(strings.ToLower(view), "press q") && !strings.Contains(strings.ToLower(view), "enter to exit") {
		t.Errorf("view does not contain exit hint; got: %s", view)
	}
}

// TestTransformSummaryScreen_View_DryRun_ShowsPrefixOnHeader verifies that a dry-run
// result marks the header with the dry-run prefix so the operator cannot mistake a
// dry run for an actual write.
func TestTransformSummaryScreen_View_DryRun_ShowsPrefixOnHeader(t *testing.T) {
	result := tsTransformedResult()
	result.DryRun = true
	s := screens.NewTransformSummaryScreen(result, 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(strings.ToLower(view), "dry") {
		t.Errorf("dry-run header does not contain dry-run indicator; got: %s", view)
	}
}

// TestTransformSummaryScreen_View_DryRun_ShowsDryRunTagOnTransformedLine verifies
// that transformed file lines in a dry run carry the [dry-run] tag so the operator
// knows no file was actually written.
func TestTransformSummaryScreen_View_DryRun_ShowsDryRunTagOnTransformedLine(t *testing.T) {
	result := tsTransformedResult()
	result.DryRun = true
	s := screens.NewTransformSummaryScreen(result, 80, 40, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "[dry-run]") {
		t.Errorf("dry-run transformed line does not contain [dry-run] tag; got: %s", view)
	}
}

// ---------------------------------------------------------------------------
// Per-file status styling tests
// ---------------------------------------------------------------------------
// Style assertions work by building the ANSI-encoded version of a line using the
// appropriate style role and checking whether it appears in (or is absent from) the
// View() output. Because coloredStyles() uses a forced TrueColor renderer, each
// role produces a distinct, deterministic escape sequence.
//
// Assertion rationale:
//   - styles.Error.Render(text) produces "\x1b[38;2;255;0;0mtext\x1b[0m"
//   - styles.Success.Render(text) produces "\x1b[38;2;0;255;0mtext\x1b[0m"
//   - In the broken (current) implementation, the whole View() is one
//     Success.Render call, so every line carries the green escape sequence.
//     A check for the Error-coded form of a failed line therefore fails.
//   - In the correct per-line implementation, each line is rendered with its
//     own role, so the Error-coded form of a failed line is present in the view.

// TestTransformSummaryScreen_View_FailedEntry_RendersInErrorStyle verifies that a
// failed file line appears in the Error (red) style. The operator must immediately
// see that an entry failed; showing it in green is misleading.
func TestTransformSummaryScreen_View_FailedEntry_RendersInErrorStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsFailedResult(), 80, 40, styles)
	view := s.View()

	// The failed line as it appears in View() after stripping the leading newline.
	failedLineText := "  failed: /agents/my-agent.md — destination file already exists"
	errorStyled := styles.Error.Render(failedLineText)
	if !strings.Contains(view, errorStyled) {
		t.Errorf("failed entry is not rendered in the Error style.\n"+
			"The current implementation wraps every line in a single Success block.\n"+
			"want Error-coded line to appear in view.\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
}

// TestTransformSummaryScreen_View_FailedEntry_NotInSuccessStyle verifies that a
// failed file line does not appear in the Success (green) style.
func TestTransformSummaryScreen_View_FailedEntry_NotInSuccessStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsFailedResult(), 80, 40, styles)
	view := s.View()

	failedLineText := "  failed: /agents/my-agent.md — destination file already exists"
	successStyled := styles.Success.Render(failedLineText)
	if strings.Contains(view, successStyled) {
		t.Errorf("failed entry is rendered in the Success style; want Error style.\n"+
			"A failed transform must never be shown in green.")
	}
}

// TestTransformSummaryScreen_View_SkippedMismatchEntry_RendersInWarningStyle verifies
// that a skipped (harness mismatch) file line appears in the Warning style. Skipped
// entries are not failures, but they are not successes either — Warning matches how
// the sibling summary.go screen treats its skipped block.
func TestTransformSummaryScreen_View_SkippedMismatchEntry_RendersInWarningStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsSkippedMismatchResult(), 80, 40, styles)
	view := s.View()

	skippedLineText := "  skipped (mismatch): /agents/wrong-harness-agent.md — source harness mismatch"
	warnStyled := styles.Warning.Render(skippedLineText)
	if !strings.Contains(view, warnStyled) {
		t.Errorf("skipped (mismatch) entry is not rendered in the Warning style.\n"+
			"The current implementation renders it in Success style instead.\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
}

// TestTransformSummaryScreen_View_SkippedMismatchEntry_NotInSuccessStyle verifies
// that a skipped (mismatch) file line does not appear in the Success (green) style.
func TestTransformSummaryScreen_View_SkippedMismatchEntry_NotInSuccessStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsSkippedMismatchResult(), 80, 40, styles)
	view := s.View()

	skippedLineText := "  skipped (mismatch): /agents/wrong-harness-agent.md — source harness mismatch"
	successStyled := styles.Success.Render(skippedLineText)
	if strings.Contains(view, successStyled) {
		t.Errorf("skipped (mismatch) entry is rendered in Success style; want Warning style")
	}
}

// TestTransformSummaryScreen_View_SkippedNotAgentEntry_RendersInWarningStyle verifies
// that a skipped (not agent) file line appears in the Warning style, consistent with
// how StatusSkippedMismatch is treated.
func TestTransformSummaryScreen_View_SkippedNotAgentEntry_RendersInWarningStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsSkippedNotAgentResult(), 80, 40, styles)
	view := s.View()

	skippedLineText := "  skipped (not agent): /agents/plain-file.md — not a MOSAIC agent"
	warnStyled := styles.Warning.Render(skippedLineText)
	if !strings.Contains(view, warnStyled) {
		t.Errorf("skipped (not agent) entry is not rendered in the Warning style.\n"+
			"The current implementation renders it in Success style instead.\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
}

// TestTransformSummaryScreen_View_SkippedNotAgentEntry_NotInSuccessStyle verifies
// that a skipped (not agent) file line does not appear in the Success (green) style.
func TestTransformSummaryScreen_View_SkippedNotAgentEntry_NotInSuccessStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsSkippedNotAgentResult(), 80, 40, styles)
	view := s.View()

	skippedLineText := "  skipped (not agent): /agents/plain-file.md — not a MOSAIC agent"
	successStyled := styles.Success.Render(skippedLineText)
	if strings.Contains(view, successStyled) {
		t.Errorf("skipped (not agent) entry is rendered in Success style; want Warning style")
	}
}

// TestTransformSummaryScreen_View_TransformedEntry_RendersInSuccessStyle verifies that a
// transformed file line appears in the Success (green) style. The operator must see at a
// glance that the file was successfully written; a neutral or muted style would obscure the
// positive outcome. This is the companion positive assertion to the failed-entry Error test.
func TestTransformSummaryScreen_View_TransformedEntry_RendersInSuccessStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsTransformedResult(), 80, 40, styles)
	view := s.View()

	// The transformed line as it appears in View() for a non-dry-run result.
	transformedLineText := "  transformed: /agents/my-agent.md → /out/my-agent.md"
	successStyled := styles.Success.Render(transformedLineText)
	if !strings.Contains(view, successStyled) {
		t.Errorf("transformed entry is not rendered in the Success style.\n"+
			"The current implementation wraps every line in a single Success block, so per-line\n"+
			"Success coding cannot be detected as a distinct substring.\n"+
			"want Success-coded line to appear in view (requires per-line rendering).\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
}

// TestTransformSummaryScreen_View_SkippedMismatchEntry_NotInErrorStyle verifies that a
// skipped (harness mismatch) file line does not appear in the Error (red) style. Skipped
// entries are not failures — showing them in red would misrepresent the outcome to the
// operator and create false urgency.
func TestTransformSummaryScreen_View_SkippedMismatchEntry_NotInErrorStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsSkippedMismatchResult(), 80, 40, styles)
	view := s.View()

	skippedLineText := "  skipped (mismatch): /agents/wrong-harness-agent.md — source harness mismatch"
	errorStyled := styles.Error.Render(skippedLineText)
	if strings.Contains(view, errorStyled) {
		t.Errorf("skipped (mismatch) entry is rendered in the Error style; want Warning style.\n"+
			"A skipped entry is not a failure and must never appear in red.")
	}
}

// TestTransformSummaryScreen_View_SkippedNotAgentEntry_NotInErrorStyle verifies that a
// skipped (not agent) file line does not appear in the Error (red) style. Skipped entries
// are not failures — showing them in red would misrepresent the outcome to the operator.
func TestTransformSummaryScreen_View_SkippedNotAgentEntry_NotInErrorStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsSkippedNotAgentResult(), 80, 40, styles)
	view := s.View()

	skippedLineText := "  skipped (not agent): /agents/plain-file.md — not a MOSAIC agent"
	errorStyled := styles.Error.Render(skippedLineText)
	if strings.Contains(view, errorStyled) {
		t.Errorf("skipped (not agent) entry is rendered in the Error style; want Warning style.\n"+
			"A skipped entry is not a failure and must never appear in red.")
	}
}

// ---------------------------------------------------------------------------
// Overall-outcome header/counts styling tests
// ---------------------------------------------------------------------------

// TestTransformSummaryScreen_View_AllFailedResult_HeaderRendersInErrorStyle verifies
// that when the overall outcome is failure (Failed > 0), the header line appears in
// the Error (red) style, not Success. The header color must communicate the overall
// outcome visually so the operator notices the failure without reading every line.
func TestTransformSummaryScreen_View_AllFailedResult_HeaderRendersInErrorStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsFailedResult(), 80, 40, styles)
	view := s.View()

	// "transform complete" is the opening text of the header for a non-dry-run result.
	// It must appear in Error style when the overall outcome is failure.
	errorStyled := styles.Error.Render("transform complete")
	successStyled := styles.Success.Render("transform complete")

	if !strings.Contains(view, errorStyled) {
		t.Errorf("header not rendered in Error style for an all-failed result.\n"+
			"want 'transform complete' to appear with Error (red) coding when failures are present.\n"+
			"The current implementation renders the header in Success (green) style.\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
	if strings.Contains(view, successStyled) {
		t.Errorf("header rendered in Success style for an all-failed result; want Error style.\n"+
			"A run with failures must never show a green header.")
	}
}

// TestTransformSummaryScreen_View_AllTransformedResult_HeaderRendersInSuccessStyle verifies
// that when the overall outcome is success (no failures and no skips), the header line
// appears in the Success (green) style. A clean run should communicate success at a glance,
// completing the three-way outcome mapping alongside the Error (failures) and Warning (skips)
// header tests.
func TestTransformSummaryScreen_View_AllTransformedResult_HeaderRendersInSuccessStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsTransformedResult(), 80, 40, styles)
	view := s.View()

	// "transform complete" is the opening text of the header for a non-dry-run result.
	// It must appear in Success style when there are no failures and no skips.
	successStyled := styles.Success.Render("transform complete")
	errorStyled := styles.Error.Render("transform complete")
	warningStyled := styles.Warning.Render("transform complete")

	if !strings.Contains(view, successStyled) {
		t.Errorf("header not rendered in Success style for an all-transformed result.\n"+
			"want 'transform complete' to appear with Success (green) coding when no failures or skips.\n"+
			"The current implementation wraps the entire block in one Success call, so per-line\n"+
			"Success coding cannot be detected as a distinct substring.\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
	if strings.Contains(view, errorStyled) {
		t.Errorf("header rendered in Error style for an all-transformed result; want Success style.")
	}
	if strings.Contains(view, warningStyled) {
		t.Errorf("header rendered in Warning style for an all-transformed result; want Success style.")
	}
}

// TestTransformSummaryScreen_View_SkippedOnlyResult_HeaderRendersInWarningStyle verifies
// that when the overall outcome has no failures but has skipped entries, the header line
// appears in the Warning (yellow) style. Skipped entries are not a success — a green header
// would misrepresent a run where files were not processed.
func TestTransformSummaryScreen_View_SkippedOnlyResult_HeaderRendersInWarningStyle(t *testing.T) {
	styles := coloredStyles()
	// tsSkippedMismatchResult has one skipped entry and no failures: overall outcome = warning.
	s := screens.NewTransformSummaryScreen(tsSkippedMismatchResult(), 80, 40, styles)
	view := s.View()

	warningStyled := styles.Warning.Render("transform complete")
	successStyled := styles.Success.Render("transform complete")
	errorStyled := styles.Error.Render("transform complete")

	if !strings.Contains(view, warningStyled) {
		t.Errorf("header not rendered in Warning style for a skipped-only result.\n"+
			"want 'transform complete' to appear with Warning (yellow) coding when skips exist and no failures.\n"+
			"The current implementation renders the header in Success style instead.\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
	if strings.Contains(view, successStyled) {
		t.Errorf("header rendered in Success style for a skipped-only result; want Warning style.\n"+
			"A run with skipped entries must not show a green header.")
	}
	if strings.Contains(view, errorStyled) {
		t.Errorf("header rendered in Error style for a skipped-only result; want Warning style.")
	}
}

// TestTransformSummaryScreen_View_MixedResult_ErrorStyleAppearsForFailedEntry verifies
// that when a result contains both transformed and failed entries, the failed entry is
// rendered in the Error style. This is the direct test of the requirement that a mixed
// result does not wrap its entire output in a single Success style block.
func TestTransformSummaryScreen_View_MixedResult_ErrorStyleAppearsForFailedEntry(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsMixedResult(), 80, 40, styles)
	view := s.View()

	failedLineText := "  failed: /agents/fail-agent.md — destination file already exists"
	errorStyled := styles.Error.Render(failedLineText)
	if !strings.Contains(view, errorStyled) {
		t.Errorf("mixed result: failed entry is not rendered in the Error style.\n"+
			"A view with both transformed and failed entries must not wrap everything\n"+
			"in a single Success block — the failed line must be Error-coded.\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
}

// ---------------------------------------------------------------------------
// Sub-line styling tests
// ---------------------------------------------------------------------------

// TestTransformSummaryScreen_View_WarningSubLine_RendersInWarningStyle verifies
// that the per-file warning sub-line (the indeterminate harness-match note) appears
// in the Warning style. The warning sub-line signals a non-blocking concern that
// is distinct from both the Success color of a clean transform and the Error color
// of a failure.
func TestTransformSummaryScreen_View_WarningSubLine_RendersInWarningStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsRichTransformedResult(), 80, 40, styles)
	view := s.View()

	warnSubLineText := "    warning: indeterminate harness match"
	warnStyled := styles.Warning.Render(warnSubLineText)
	if !strings.Contains(view, warnStyled) {
		t.Errorf("per-file warning sub-line is not rendered in the Warning style.\n"+
			"The current implementation renders it in Success style instead.\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
}

// TestTransformSummaryScreen_View_ToolsSubLine_RendersInMutedStyle verifies that
// the tools sub-line appears in the Muted style. Detail sub-lines are informational
// and should be visually de-emphasized relative to the status line above them.
func TestTransformSummaryScreen_View_ToolsSubLine_RendersInMutedStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsRichTransformedResult(), 80, 40, styles)
	view := s.View()

	toolsSubLineText := "    tools: Bash, Read"
	mutedStyled := styles.Muted.Render(toolsSubLineText)
	if !strings.Contains(view, mutedStyled) {
		t.Errorf("tools sub-line is not rendered in the Muted style.\n"+
			"The current implementation renders it in Success style instead.\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
}

// TestTransformSummaryScreen_View_CarriedVerbatimToolsSubLine_RendersInMutedStyle verifies
// that the carried-verbatim-tools sub-line appears in the Muted style. Like the tools
// sub-line, this is informational detail that should be visually de-emphasized relative to
// the status line above it. The tsRichTransformedResult fixture provides the "Write" tool.
func TestTransformSummaryScreen_View_CarriedVerbatimToolsSubLine_RendersInMutedStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsRichTransformedResult(), 80, 40, styles)
	view := s.View()

	carriedToolsSubLineText := "    carried verbatim tools: Write"
	mutedStyled := styles.Muted.Render(carriedToolsSubLineText)
	if !strings.Contains(view, mutedStyled) {
		t.Errorf("carried verbatim tools sub-line is not rendered in the Muted style.\n"+
			"The current implementation renders it in Success style instead.\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
}

// TestTransformSummaryScreen_View_StrippedFieldSubLine_RendersInMutedStyle verifies that
// a stripped-field sub-line appears in the Muted style. Stripped-field detail is
// informational and should be visually de-emphasized, consistent with the tools sub-line.
// The tsRichTransformedResult fixture provides mcpServers with reason unmapped-diverted-value.
func TestTransformSummaryScreen_View_StrippedFieldSubLine_RendersInMutedStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsRichTransformedResult(), 80, 40, styles)
	view := s.View()

	// tsRichTransformedResult provides StrippedFields with Key="mcpServers",
	// Values=["server1"], Reason=StripReasonUnmappedDivertedValue ("unmapped-diverted-value").
	// View() formats this as "    stripped field: mcpServers (unmapped-diverted-value): server1".
	strippedFieldSubLineText := "    stripped field: mcpServers (unmapped-diverted-value): server1"
	mutedStyled := styles.Muted.Render(strippedFieldSubLineText)
	if !strings.Contains(view, mutedStyled) {
		t.Errorf("stripped field sub-line is not rendered in the Muted style.\n"+
			"The current implementation renders it in Success style instead.\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
}

// TestTransformSummaryScreen_View_ExitHint_RendersInHelpStyle verifies that the
// "press q or enter to exit" footer appears in the Help style. The exit hint is
// navigation guidance, not outcome information, so it uses the Help role consistent
// with how other screens render their key-binding hints.
func TestTransformSummaryScreen_View_ExitHint_RendersInHelpStyle(t *testing.T) {
	styles := coloredStyles()
	s := screens.NewTransformSummaryScreen(tsTransformedResult(), 80, 40, styles)
	view := s.View()

	exitHintText := "press q or enter to exit"
	helpStyled := styles.Help.Render(exitHintText)
	if !strings.Contains(view, helpStyled) {
		t.Errorf("exit hint is not rendered in the Help style.\n"+
			"The current implementation renders it in Success style instead.\n"+
			"view (truncated to 300 chars): %.300s", view)
	}
}

// ---------------------------------------------------------------------------
// Keyboard dismissal tests
// ---------------------------------------------------------------------------

// TestTransformSummaryScreen_Update_QDismisses verifies that pressing 'q' marks
// the screen done and returns true from Update.
func TestTransformSummaryScreen_Update_QDismisses(t *testing.T) {
	s := screens.NewTransformSummaryScreen(tsTransformedResult(), 80, 40, plainStyles())
	if s.Done() {
		t.Fatal("screen is already done before any key press")
	}

	done := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if !done {
		t.Error("Update returned false for 'q'; want true")
	}
	if !s.Done() {
		t.Error("Done() returned false after 'q'; want true")
	}
}

// TestTransformSummaryScreen_Update_EnterDismisses verifies that pressing Enter
// marks the screen done, consistent with other summary screens.
func TestTransformSummaryScreen_Update_EnterDismisses(t *testing.T) {
	s := screens.NewTransformSummaryScreen(tsTransformedResult(), 80, 40, plainStyles())

	done := s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !done {
		t.Error("Update returned false for Enter; want true")
	}
	if !s.Done() {
		t.Error("Done() returned false after Enter; want true")
	}
}

// TestTransformSummaryScreen_Update_EscDismisses verifies that pressing Escape
// also dismisses the screen, consistent with the key handling in Update.
func TestTransformSummaryScreen_Update_EscDismisses(t *testing.T) {
	s := screens.NewTransformSummaryScreen(tsTransformedResult(), 80, 40, plainStyles())

	done := s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !done {
		t.Error("Update returned false for Esc; want true")
	}
	if !s.Done() {
		t.Error("Done() returned false after Esc; want true")
	}
}
