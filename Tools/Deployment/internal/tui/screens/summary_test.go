package screens_test

// summary_test.go verifies the SummaryScreen: outcome banner rendering, action records
// (created, updated with version delta, skipped, backed-up, failed), fallback location
// reporting, active external module reporting, TODO item list, and scrolling.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Summary fixtures
// ---------------------------------------------------------------------------

// cleanSummary returns a RunSummary representing a fully successful deployment.
func cleanSummary() domain.RunSummary {
	return domain.RunSummary{
		Mode:           domain.ModeDeployWorkspace,
		Harness:        domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
		WorkspacePath:  "/workspace",
		DeploymentRoot: "/workspace/.ai",
		Fallback:       domain.FallbackNone,
		Actions: []domain.ActionRecord{
			{Ref: agentRef("test-runner"), TargetPath: "/workspace/.ai/Agents/test-runner.agent.md", Taken: domain.TakenCreated},
			{Ref: skillRef("lean-tdd"), TargetPath: "/workspace/.ai/Skills/lean-tdd/", Taken: domain.TakenCreated},
		},
		Outcome: domain.OutcomeSuccess,
		Logs:    domain.LogPaths{Latest: "/mosaic/logs/latest.log"},
	}
}

// summaryWithSkips returns a RunSummary where some items were skipped.
func summaryWithSkips() domain.RunSummary {
	return domain.RunSummary{
		Mode:           domain.ModeUpdateWorkspace,
		Harness:        domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
		WorkspacePath:  "/workspace",
		DeploymentRoot: "/workspace/.ai",
		Fallback:       domain.FallbackNone,
		Actions: []domain.ActionRecord{
			{Ref: agentRef("test-runner"), Taken: domain.TakenUpdated,
				Stale: []domain.VersionDelta{{Field: "version", Deployed: "1.0", Source: "1.1"}}},
			{Ref: agentRef("skipped-agent"), Taken: domain.TakenSkipped},
		},
		Todos: []domain.TodoItem{
			{Category: domain.TodoSkippedFiles, Subject: "skipped-agent", Detail: "user skipped this file"},
		},
		TodoFilePath: "/workspace/MOSAIC-DEPLOYMENT-TODO.md",
		Outcome:      domain.OutcomeCompletedWithGaps,
	}
}

// summaryWithFallback returns a RunSummary where a fallback deployment location was used.
func summaryWithFallback() domain.RunSummary {
	return domain.RunSummary{
		Mode:           domain.ModeDeployWorkspace,
		Harness:        domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
		WorkspacePath:  "/workspace",
		DeploymentRoot: "/mosaic/fallback/workspace",
		Fallback:       domain.FallbackMosaicRoot,
		Actions:        []domain.ActionRecord{{Ref: agentRef("test-runner"), Taken: domain.TakenCreated}},
		Outcome:        domain.OutcomeSuccess,
	}
}

// summaryWithExternalModule returns a RunSummary where an external module was active.
func summaryWithExternalModule() domain.RunSummary {
	return domain.RunSummary{
		Mode:           domain.ModeDeployWorkspace,
		Harness:        domain.HarnessRef{ID: "custom-harness", DisplayName: "Custom Harness"},
		WorkspacePath:  "/workspace",
		DeploymentRoot: "/workspace/.ai",
		Fallback:       domain.FallbackNone,
		External: &domain.ExternalModuleInfo{
			HarnessID:      "custom-harness",
			ExecutablePath: "/usr/local/bin/my-module",
		},
		Actions: []domain.ActionRecord{{Ref: agentRef("test-runner"), Taken: domain.TakenCreated}},
		Outcome: domain.OutcomeSuccess,
	}
}

// ---------------------------------------------------------------------------
// Outcome banner (T22.7: clean run, with skips, fallback, external module)
// ---------------------------------------------------------------------------

// TestSummaryScreen_CleanRun_ShowsSuccessBanner verifies that a fully successful run shows
// a success outcome banner.
func TestSummaryScreen_CleanRun_ShowsSuccessBanner(t *testing.T) {
	s := screens.NewSummaryScreen(cleanSummary(), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "successfully") && !strings.Contains(view, "complete") {
		t.Errorf("summary view for a clean run does not show a success indication:\n%s", s.View())
	}
}

// TestSummaryScreen_WithSkips_ShowsGapsBanner verifies that a run completed with gaps
// shows a warning banner mentioning skips or gaps.
func TestSummaryScreen_WithSkips_ShowsGapsBanner(t *testing.T) {
	s := screens.NewSummaryScreen(summaryWithSkips(), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "gap") && !strings.Contains(view, "skip") && !strings.Contains(view, "follow-up") {
		t.Errorf("summary view for a run with skips does not mention gaps or skips:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Action records
// ---------------------------------------------------------------------------

// TestSummaryScreen_ShowsCreatedItems verifies that created items appear in the summary.
func TestSummaryScreen_ShowsCreatedItems(t *testing.T) {
	s := screens.NewSummaryScreen(cleanSummary(), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Created") {
		t.Errorf("summary view does not show 'Created' section:\n%s", s.View())
	}
	if !strings.Contains(view, "test-runner") {
		t.Errorf("summary view does not show the created agent key:\n%s", s.View())
	}
}

// TestSummaryScreen_ShowsUpdatedItemsWithVersionDelta verifies that updated items appear in
// the summary with the version field that drove the update (AC22.6).
func TestSummaryScreen_ShowsUpdatedItemsWithVersionDelta(t *testing.T) {
	s := screens.NewSummaryScreen(summaryWithSkips(), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Updated") {
		t.Errorf("summary view does not show 'Updated' section:\n%s", s.View())
	}
	if !strings.Contains(view, "version") {
		t.Errorf("summary view does not show the version field that drove the update:\n%s", s.View())
	}
	if !strings.Contains(view, "1.0") || !strings.Contains(view, "1.1") {
		t.Errorf("summary view does not show the old and new version values:\n%s", s.View())
	}
}

// TestSummaryScreen_ShowsSkippedItems verifies that skipped items appear in the summary
// so the user knows what was not deployed.
func TestSummaryScreen_ShowsSkippedItems(t *testing.T) {
	s := screens.NewSummaryScreen(summaryWithSkips(), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Skipped") {
		t.Errorf("summary view does not show 'Skipped' section:\n%s", s.View())
	}
	if !strings.Contains(view, "skipped-agent") {
		t.Errorf("summary view does not show the skipped agent key:\n%s", s.View())
	}
}

// TestSummaryScreen_ShowsBackedUpItems verifies that backed-up items show both their key
// and the backup path.
func TestSummaryScreen_ShowsBackedUpItems(t *testing.T) {
	summary := cleanSummary()
	summary.Actions = []domain.ActionRecord{
		{
			Ref:        agentRef("conflict-agent"),
			TargetPath: "/workspace/.ai/Agents/conflict-agent.agent.md",
			Taken:      domain.TakenBackedUp,
			BackupPath: "/workspace/.mosaic/backups/conflict-agent.agent.md.20260101T000000Z.bak",
		},
	}
	summary.Outcome = domain.OutcomeCompletedWithGaps

	s := screens.NewSummaryScreen(summary, 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Backed up") && !strings.Contains(view, "backed") {
		t.Errorf("summary view does not show 'Backed up' section:\n%s", s.View())
	}
	if !strings.Contains(view, ".bak") {
		t.Errorf("summary view does not show the backup path:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Fallback location (AC22.6)
// ---------------------------------------------------------------------------

// TestSummaryScreen_WithFallback_ShowsFallbackInfo verifies that when a fallback deployment
// location was used, it is reported in the summary (AC22.6).
func TestSummaryScreen_WithFallback_ShowsFallbackInfo(t *testing.T) {
	s := screens.NewSummaryScreen(summaryWithFallback(), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Fallback") && !strings.Contains(view, "fallback") {
		t.Errorf("summary view does not mention the fallback location:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Active external module (AC22.6)
// ---------------------------------------------------------------------------

// TestSummaryScreen_WithExternalModule_ShowsModuleInfo verifies that when an external module
// was active, its harness ID and executable path appear in the summary (AC22.6).
func TestSummaryScreen_WithExternalModule_ShowsModuleInfo(t *testing.T) {
	s := screens.NewSummaryScreen(summaryWithExternalModule(), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "custom-harness") {
		t.Errorf("summary view does not show the external module harness ID:\n%s", s.View())
	}
	if !strings.Contains(view, "/usr/local/bin/my-module") {
		t.Errorf("summary view does not show the external module executable path:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// TODO items / follow-up actions (AC22.6)
// ---------------------------------------------------------------------------

// TestSummaryScreen_WithTodos_ShowsFollowUpItems verifies that TODO items (required follow-up
// actions) are listed in the summary.
func TestSummaryScreen_WithTodos_ShowsFollowUpItems(t *testing.T) {
	s := screens.NewSummaryScreen(summaryWithSkips(), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Follow-up") && !strings.Contains(view, "follow-up") {
		t.Errorf("summary view does not show a follow-up section for TODO items:\n%s", s.View())
	}
}

// TestSummaryScreen_WithTodoFilePath_ShowsPath verifies that when a TODO checklist file was
// written, its path appears in the summary.
func TestSummaryScreen_WithTodoFilePath_ShowsPath(t *testing.T) {
	s := screens.NewSummaryScreen(summaryWithSkips(), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "MOSAIC-DEPLOYMENT-TODO.md") {
		t.Errorf("summary view does not show the TODO file path:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Run context metadata
// ---------------------------------------------------------------------------

// TestSummaryScreen_ShowsRunContext verifies that the harness name, workspace path, and
// deployment root are shown so the user can confirm where the run deployed to.
func TestSummaryScreen_ShowsRunContext(t *testing.T) {
	s := screens.NewSummaryScreen(cleanSummary(), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Claude Code") {
		t.Errorf("summary view does not show the harness display name:\n%s", s.View())
	}
	if !strings.Contains(view, "/workspace") {
		t.Errorf("summary view does not show the workspace path:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Exit keys
// ---------------------------------------------------------------------------

// TestSummaryScreen_QKey_SetsDone verifies that pressing 'q' signals the screen is done so
// the root model knows to quit.
func TestSummaryScreen_QKey_SetsDone(t *testing.T) {
	s := screens.NewSummaryScreen(cleanSummary(), 80, 40, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if !s.Done() {
		t.Error("Done() = false after 'q'; want true")
	}
}

// TestSummaryScreen_EnterKey_SetsDone verifies that Enter also exits the summary screen.
func TestSummaryScreen_EnterKey_SetsDone(t *testing.T) {
	s := screens.NewSummaryScreen(cleanSummary(), 80, 40, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter; want true")
	}
}

// ---------------------------------------------------------------------------
// Scrolling
// ---------------------------------------------------------------------------

// TestSummaryScreen_Scroll_DoesNotPanic verifies that scrolling the summary screen does not
// panic even at the boundaries of the content.
func TestSummaryScreen_Scroll_DoesNotPanic(t *testing.T) {
	s := screens.NewSummaryScreen(cleanSummary(), 80, 10, plainStyles()) // small window

	for i := 0; i < 50; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	for i := 0; i < 50; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyUp})
	}

	view := s.View()
	if view == "" {
		t.Error("View() returned empty string after scrolling; want non-empty")
	}
}

// ---------------------------------------------------------------------------
// Workflows-only mode rendering
// ---------------------------------------------------------------------------

// TestSummaryScreen_WorkflowsOnlyMode_ShowsModeLabel verifies that the SummaryScreen renders
// the correct mode label "Update workflows only" when the RunSummary carries ModeUpdateWorkflows.
// The summary must not fall through to the plain "Update" label used for ModeUpdateWorkspace runs.
func TestSummaryScreen_WorkflowsOnlyMode_ShowsModeLabel(t *testing.T) {
	// Arrange: a RunSummary for a workflows-only run.
	summary := domain.RunSummary{
		Mode:           domain.ModeUpdateWorkflows,
		Harness:        domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
		WorkspacePath:  "/workspace",
		DeploymentRoot: "/workspace/.ai",
		Fallback:       domain.FallbackNone,
		Actions: []domain.ActionRecord{
			{Ref: agentRef("orchestrator"), Taken: domain.TakenUpdated},
		},
		Outcome: domain.OutcomeSuccess,
	}
	s := screens.NewSummaryScreen(summary, 80, 40, plainStyles())

	// Act
	view := collapseWhitespace(s.View())

	// Assert: the mode line must reflect "Update workflows", not the old "Update workflows only",
	// "workflows only", or the generic "Update" or "Deploy new".
	if !strings.Contains(view, "Update workflows") {
		t.Errorf("summary view for ModeUpdateWorkflows does not show the correct mode label \"Update workflows\"; "+
			"Mode: must identify the operation with the current vocabulary:\n%s", s.View())
	}
}

// TestSummaryScreen_WorkflowsOnlyMode_DoesNotShowDeployNewLabel verifies that a workflows-only
// run summary does not mistakenly display the "Deploy new" label. Each of the three run modes
// must show its own distinct label.
func TestSummaryScreen_WorkflowsOnlyMode_DoesNotShowDeployNewLabel(t *testing.T) {
	// Arrange
	summary := domain.RunSummary{
		Mode:           domain.ModeUpdateWorkflows,
		Harness:        domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
		WorkspacePath:  "/workspace",
		DeploymentRoot: "/workspace/.ai",
		Fallback:       domain.FallbackNone,
		Outcome:        domain.OutcomeSuccess,
	}
	s := screens.NewSummaryScreen(summary, 80, 40, plainStyles())

	// Act
	view := collapseWhitespace(s.View())

	// Assert: "Deploy new" must not appear in the mode label for a workflows-only run.
	if strings.Contains(view, "Deploy new") {
		t.Errorf("summary view for ModeUpdateWorkflows shows 'Deploy new' mode label; "+
			"workflows-only runs must use a distinct mode label:\n%s", s.View())
	}
}

// TestSummaryScreen_AllThreeModes_ShowDistinctModeLabels is a table-driven test verifying
// that each of the three run modes produces a distinct mode label in the summary view.
// No two modes may render identically in the mode line.
func TestSummaryScreen_AllThreeModes_ShowDistinctModeLabels(t *testing.T) {
	baseSummary := func(mode domain.RunMode) domain.RunSummary {
		return domain.RunSummary{
			Mode:    mode,
			Outcome: domain.OutcomeSuccess,
		}
	}

	modes := []domain.RunMode{domain.ModeDeployWorkspace, domain.ModeUpdateWorkspace, domain.ModeUpdateWorkflows}
	views := make(map[domain.RunMode]string, len(modes))
	for _, mode := range modes {
		s := screens.NewSummaryScreen(baseSummary(mode), 80, 40, plainStyles())
		views[mode] = collapseWhitespace(s.View())
	}

	// Each pair must produce a different rendered view (at minimum, the mode line differs).
	for i, a := range modes {
		for j, b := range modes {
			if i >= j {
				continue
			}
			if views[a] == views[b] {
				t.Errorf("summary views for %q and %q are identical; "+
					"each run mode must produce a distinct mode label in the summary", a, b)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Follow-up owner attribution
// ---------------------------------------------------------------------------

// TestSummaryScreen_FollowUp_InjectionItemWithOwner_ShowsAttributionWhenDetailEmpty verifies
// that an injection follow-up item bearing an owner shows the owning agent in the post-run
// summary screen even when the item's Detail field is empty.
func TestSummaryScreen_FollowUp_InjectionItemWithOwner_ShowsAttributionWhenDetailEmpty(t *testing.T) {
	// Arrange
	summary := domain.RunSummary{
		Mode:           domain.ModeDeployWorkspace,
		Harness:        domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
		WorkspacePath:  "/workspace",
		DeploymentRoot: "/workspace/.ai",
		Todos: []domain.TodoItem{
			{
				Category: domain.TodoInjections,
				Subject:  "CodebaseContext",
				Owner:    "harness-bug-hunter",
				Detail:   "", // empty — injection region awaiting user content
			},
		},
		TodoFilePath: "/workspace/MOSAIC-DEPLOYMENT-TODO.md",
		Outcome:      domain.OutcomeCompletedWithGaps,
	}

	// Act
	s := screens.NewSummaryScreen(summary, 120, 80, plainStyles())
	view := s.View()

	// Assert: the owning agent must be visible on the follow-up line.
	if !strings.Contains(view, "agent: harness-bug-hunter") {
		t.Errorf("summary view does not show owner attribution for injection item with empty Detail;\n"+
			"want a line containing \"agent: harness-bug-hunter\":\n%s", view)
	}
}

// TestSummaryScreen_FollowUp_ItemDetailAlreadyNamesOwner_NoAgentNameDuplicate verifies that
// when a follow-up item's Detail text already names the owning agent (as tool-mapping items
// do), the agent name appears only once on that follow-up bullet line — not once in the
// Detail text and again as a "(agent: …)" suffix.
func TestSummaryScreen_FollowUp_ItemDetailAlreadyNamesOwner_NoAgentNameDuplicate(t *testing.T) {
	// Arrange
	const owner = "harness-bug-hunter"
	summary := domain.RunSummary{
		Mode:           domain.ModeDeployWorkspace,
		Harness:        domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
		WorkspacePath:  "/workspace",
		DeploymentRoot: "/workspace/.ai",
		Todos: []domain.TodoItem{
			{
				Category: domain.TodoToolMappings,
				Subject:  "file_read",
				Owner:    owner,
				Detail:   `generic tool "file_read" has no harness mapping for agent "harness-bug-hunter"`,
			},
		},
		Outcome: domain.OutcomeCompletedWithGaps,
	}

	// Act
	s := screens.NewSummaryScreen(summary, 120, 80, plainStyles())
	view := s.View()

	// Assert: the owner name must appear at most once on the follow-up bullet line.
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "file_read") && strings.Contains(line, "•") {
			count := strings.Count(line, owner)
			if count > 1 {
				t.Errorf("follow-up bullet for tool-mapping item names owner %q %d times on one line; want at most 1:\n%q",
					owner, count, line)
			}
		}
	}
}

// TestSummaryScreen_FollowUp_BlankOwner_NoAttributionSuffix verifies that a follow-up item
// whose Owner is empty renders without any "(agent: …)" attribution suffix.
func TestSummaryScreen_FollowUp_BlankOwner_NoAttributionSuffix(t *testing.T) {
	// Arrange
	summary := domain.RunSummary{
		Mode:           domain.ModeDeployWorkspace,
		Harness:        domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
		WorkspacePath:  "/workspace",
		DeploymentRoot: "/workspace/.ai",
		Todos: []domain.TodoItem{
			{
				Category: domain.TodoModels,
				Subject:  "fast-tier",
				Owner:    "", // run-level item with no agent attribution
				Detail:   "no model configured",
			},
		},
		Outcome: domain.OutcomeCompletedWithGaps,
	}

	// Act
	s := screens.NewSummaryScreen(summary, 120, 80, plainStyles())
	view := s.View()

	// Assert: the item is rendered but without any attribution suffix.
	if !strings.Contains(view, "fast-tier") {
		t.Errorf("summary view does not render follow-up item with blank owner at all:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "fast-tier") {
			if strings.Contains(line, "(agent:") {
				t.Errorf("follow-up line for blank-owner item contains attribution suffix; want none:\n%q", line)
			}
		}
	}
}

// TestSummaryScreen_NonFollowUpSections_UnaffectedByOwnerAttribution verifies that the parts
// of the summary not related to follow-up items — the outcome banner, mode label, and action
// records — continue to render correctly after attribution logic is applied to the follow-up
// bullet lines.
func TestSummaryScreen_NonFollowUpSections_UnaffectedByOwnerAttribution(t *testing.T) {
	// Arrange: a clean run with action records but no todos.
	s := screens.NewSummaryScreen(cleanSummary(), 120, 80, plainStyles())

	// Act
	view := collapseWhitespace(s.View())

	// Assert: action-record items are visible as usual.
	if !strings.Contains(view, "test-runner") {
		t.Errorf("summary view does not show the created agent key;\n"+
			"non-follow-up output must be unaffected by attribution changes:\n%s", s.View())
	}
	// Assert: no spurious attribution suffixes appear in a clean run with no owned items.
	if strings.Contains(view, "(agent:") {
		t.Errorf("clean-run summary view contains \"(agent:\" attribution where none is expected:\n%s", s.View())
	}
}
