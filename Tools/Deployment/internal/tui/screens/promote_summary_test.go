package screens_test

// promote_summary_test.go verifies PromoteSummaryScreen: primary field rendering, dry-run
// rendering, diverted-tool and stripped-field sections, and keyboard dismissal.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// basicPromoteResult returns a minimal PromoteResult suitable for most tests.
func basicPromoteResult() app.PromoteResult {
	return app.PromoteResult{
		SourcePath:      "/workspace/.ai/Agents/my-agent.md",
		DestinationPath: "/mosaicroot/Agents/Generic/Agents/Test/my-agent.md",
		Key:             "my-agent",
		NumericID:       "42",
		Category:        "Test",
	}
}

// ---------------------------------------------------------------------------
// Primary field rendering
// ---------------------------------------------------------------------------

// TestPromoteSummaryScreen_View_ShowsSourceAndDestination verifies that the view includes
// both the source path and the destination path. These are the two paths the operator most
// needs to see after a successful promote.
func TestPromoteSummaryScreen_View_ShowsSourceAndDestination(t *testing.T) {
	result := basicPromoteResult()
	s := screens.NewPromoteSummaryScreen(result, 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, result.SourcePath) {
		t.Errorf("view does not contain source path %q:\n%s", result.SourcePath, s.View())
	}
	if !strings.Contains(view, result.DestinationPath) {
		t.Errorf("view does not contain destination path %q:\n%s", result.DestinationPath, s.View())
	}
}

// TestPromoteSummaryScreen_View_ShowsKeyAndID verifies that the view includes the agent key
// and the assigned numeric id.
func TestPromoteSummaryScreen_View_ShowsKeyAndID(t *testing.T) {
	result := basicPromoteResult()
	s := screens.NewPromoteSummaryScreen(result, 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, result.Key) {
		t.Errorf("view does not contain key %q:\n%s", result.Key, s.View())
	}
	if !strings.Contains(view, result.NumericID) {
		t.Errorf("view does not contain id %q:\n%s", result.NumericID, s.View())
	}
}

// TestPromoteSummaryScreen_View_ShowsCategory verifies that the view includes the category
// for a non-dry-run result.
func TestPromoteSummaryScreen_View_ShowsCategory(t *testing.T) {
	result := basicPromoteResult()
	s := screens.NewPromoteSummaryScreen(result, 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, result.Category) {
		t.Errorf("view does not contain category %q:\n%s", result.Category, s.View())
	}
}

// ---------------------------------------------------------------------------
// Dry-run rendering
// ---------------------------------------------------------------------------

// TestPromoteSummaryScreen_View_DryRun_IndicatesNotWritten verifies that a dry-run result
// explicitly states that the destination was not written.
func TestPromoteSummaryScreen_View_DryRun_IndicatesNotWritten(t *testing.T) {
	result := basicPromoteResult()
	result.DryRun = true
	s := screens.NewPromoteSummaryScreen(result, 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(strings.ToLower(view), "not written") && !strings.Contains(strings.ToLower(view), "dry") {
		t.Errorf("dry-run view does not indicate file was not written:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Diverted-tool and stripped-field rendering (AC2.3)
// ---------------------------------------------------------------------------

// TestPromoteSummaryScreen_View_ShowsDivertedTools verifies that diverted tools recovered
// from harness-specific frontmatter fields appear in the view. The operator needs to see
// which tools were recovered from a non-standard harness field.
func TestPromoteSummaryScreen_View_ShowsDivertedTools(t *testing.T) {
	result := basicPromoteResult()
	result.DivertedTools = []string{"Bash", "Read"}
	s := screens.NewPromoteSummaryScreen(result, 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Bash") {
		t.Errorf("view does not contain diverted tool %q:\n%s", "Bash", s.View())
	}
	if !strings.Contains(view, "Read") {
		t.Errorf("view does not contain diverted tool %q:\n%s", "Read", s.View())
	}
}

// TestPromoteSummaryScreen_View_ShowsStrippedFields verifies that stripped frontmatter
// fields appear in the view with their key and reason, so the operator can review what
// was automatically removed and re-add it by hand if needed.
func TestPromoteSummaryScreen_View_ShowsStrippedFields(t *testing.T) {
	result := basicPromoteResult()
	result.StrippedFields = []app.StrippedField{
		{Key: "customField", Values: []string{"val1"}, Reason: app.StripReasonUnknownField},
		{Key: "mcpServers", Values: []string{"unmapped-server"}, Reason: app.StripReasonUnmappedDivertedValue},
	}
	s := screens.NewPromoteSummaryScreen(result, 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "customField") {
		t.Errorf("view does not contain stripped field key %q:\n%s", "customField", s.View())
	}
	if !strings.Contains(view, "mcpServers") {
		t.Errorf("view does not contain stripped field key %q:\n%s", "mcpServers", s.View())
	}
	if !strings.Contains(view, string(app.StripReasonUnknownField)) {
		t.Errorf("view does not contain strip reason %q:\n%s", app.StripReasonUnknownField, s.View())
	}
	if !strings.Contains(view, string(app.StripReasonUnmappedDivertedValue)) {
		t.Errorf("view does not contain strip reason %q:\n%s", app.StripReasonUnmappedDivertedValue, s.View())
	}
}

// TestPromoteSummaryScreen_View_NoDivertedTools_OmitsDivertedSection verifies that when
// DivertedTools is empty, no diverted-tool line appears in the view.
func TestPromoteSummaryScreen_View_NoDivertedTools_OmitsDivertedSection(t *testing.T) {
	result := basicPromoteResult()
	result.DivertedTools = nil
	s := screens.NewPromoteSummaryScreen(result, 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if strings.Contains(strings.ToLower(view), "diverted") {
		t.Errorf("view contains 'diverted' but no diverted tools were present; empty section must be omitted:\n%s", s.View())
	}
}

// TestPromoteSummaryScreen_View_DryRun_ShowsDivertedTools verifies that diverted tools
// also appear in the dry-run view. A dry-run promote still reports what would have been
// recovered.
func TestPromoteSummaryScreen_View_DryRun_ShowsDivertedTools(t *testing.T) {
	result := basicPromoteResult()
	result.DryRun = true
	result.DivertedTools = []string{"Write"}
	s := screens.NewPromoteSummaryScreen(result, 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Write") {
		t.Errorf("dry-run view does not contain diverted tool %q; diverted tools must appear in dry-run output:\n%s", "Write", s.View())
	}
}

// ---------------------------------------------------------------------------
// Keyboard dismissal
// ---------------------------------------------------------------------------

// TestPromoteSummaryScreen_Update_QDismisses verifies that pressing 'q' marks the screen done.
func TestPromoteSummaryScreen_Update_QDismisses(t *testing.T) {
	s := screens.NewPromoteSummaryScreen(basicPromoteResult(), 80, 40, plainStyles())

	if s.Done() {
		t.Fatal("screen is already done before any key press")
	}

	done := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if !done || !s.Done() {
		t.Error("screen is not done after 'q'; 'q' must dismiss the promote summary screen")
	}
}

// TestPromoteSummaryScreen_Update_EnterDismisses verifies that pressing Enter marks the
// screen done, consistent with other summary screens.
func TestPromoteSummaryScreen_Update_EnterDismisses(t *testing.T) {
	s := screens.NewPromoteSummaryScreen(basicPromoteResult(), 80, 40, plainStyles())

	done := s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !done || !s.Done() {
		t.Error("screen is not done after Enter; Enter must dismiss the promote summary screen")
	}
}
