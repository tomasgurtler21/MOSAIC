package screens_test

// review_test.go verifies the ReviewScreen overlay: rendering of all four plan action
// classifications (create, update, unchanged, locally-modified), version delta attribution
// for updated items, the empty-plan message, scrolling, and the y/n/esc confirmation keys.

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Plan fixtures
// ---------------------------------------------------------------------------

func agentRef(key string) domain.ArtifactRef {
	return domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: key}
}

func skillRef(key string) domain.ArtifactRef {
	return domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: key}
}

// planWithItems builds a Plan containing the given items.
func planWithItems(mode domain.RunMode, items []domain.PlanItem) domain.Plan {
	return domain.Plan{
		Mode:          mode,
		WorkspacePath: "/workspace",
		Harness:       domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
		Items:         items,
	}
}

// emptyPlan returns a Plan with only unchanged items (no active work to do).
func emptyPlan() domain.Plan {
	return planWithItems(domain.ModeDeployNew, []domain.PlanItem{
		{
			Ref:    agentRef("test-runner"),
			Action: domain.ActionUnchanged,
			Reason: "already up to date",
		},
	})
}

// fullPlan returns a Plan with one item of each action classification.
func fullPlan() domain.Plan {
	return planWithItems(domain.ModeUpdate, []domain.PlanItem{
		{
			Ref:        agentRef("new-agent"),
			Action:     domain.ActionCreate,
			TargetPath: "Agents/new-agent.agent.md",
		},
		{
			Ref:        agentRef("stale-agent"),
			Action:     domain.ActionUpdate,
			TargetPath: "Agents/stale-agent.agent.md",
			Stale: []domain.VersionDelta{
				{Field: "version", Deployed: "1.0", Source: "1.1"},
			},
		},
		{
			Ref:        agentRef("conflict-agent"),
			Action:     domain.ActionConflict,
			TargetPath: "Agents/conflict-agent.agent.md",
			Conflict: &domain.LocalModification{
				RecordedHash: "sha256:aabbccdd",
				CurrentHash:  "sha256:eeff0011",
			},
		},
		{
			Ref:        skillRef("lean-tdd"),
			Action:     domain.ActionUnchanged,
			TargetPath: "Skills/lean-tdd/",
		},
	})
}

// ---------------------------------------------------------------------------
// Action classification rendering (AC22.5)
// ---------------------------------------------------------------------------

// TestReviewScreen_View_ShowsCreateItems verifies that items with ActionCreate are labelled
// with [CREATE] in the plan review so the user can see what will be written for the first time.
func TestReviewScreen_View_ShowsCreateItems(t *testing.T) {
	p := planWithItems(domain.ModeDeployNew, []domain.PlanItem{
		{Ref: agentRef("new-agent"), Action: domain.ActionCreate, TargetPath: "Agents/new-agent.agent.md"},
	})
	s := screens.NewReviewScreen(p, 80, 40, plainStyles())

	view := s.View()

	if !strings.Contains(collapseWhitespace(view), "[CREATE]") {
		t.Errorf("view does not show [CREATE] badge for a newly deployed item:\n%s", view)
	}
	if !strings.Contains(view, "new-agent") {
		t.Errorf("view does not show the agent key for the created item:\n%s", view)
	}
}

// TestReviewScreen_View_ShowsUpdateItems verifies that items with ActionUpdate are labelled
// with [UPDATE] in the plan review.
func TestReviewScreen_View_ShowsUpdateItems(t *testing.T) {
	p := planWithItems(domain.ModeUpdate, []domain.PlanItem{
		{Ref: agentRef("stale-agent"), Action: domain.ActionUpdate, TargetPath: "Agents/stale-agent.agent.md",
			Stale: []domain.VersionDelta{{Field: "version", Deployed: "1.0", Source: "1.1"}}},
	})
	s := screens.NewReviewScreen(p, 80, 40, plainStyles())

	view := s.View()

	if !strings.Contains(collapseWhitespace(view), "[UPDATE]") {
		t.Errorf("view does not show [UPDATE] badge for a stale item:\n%s", view)
	}
}

// TestReviewScreen_View_ShowsVersionDelta verifies that for updated items, the review screen
// shows which version field drove the update and the old/new values.
func TestReviewScreen_View_ShowsVersionDelta(t *testing.T) {
	p := planWithItems(domain.ModeUpdate, []domain.PlanItem{
		{
			Ref:    agentRef("stale-agent"),
			Action: domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: "transform_version", Deployed: "2.0", Source: "2.1"},
			},
		},
	})
	s := screens.NewReviewScreen(p, 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "transform_version") {
		t.Errorf("view does not show the version field name for updated item:\n%s", s.View())
	}
	if !strings.Contains(view, "2.0") || !strings.Contains(view, "2.1") {
		t.Errorf("view does not show old/new version values for updated item:\n%s", s.View())
	}
}

// TestReviewScreen_View_ShowsConflictItems verifies that locally-modified items are labelled
// with [CONFLICT] and mention the modification.
func TestReviewScreen_View_ShowsConflictItems(t *testing.T) {
	p := planWithItems(domain.ModeUpdate, []domain.PlanItem{
		{
			Ref:    agentRef("conflict-agent"),
			Action: domain.ActionConflict,
			Conflict: &domain.LocalModification{
				RecordedHash: "sha256:aabbccdd",
				CurrentHash:  "sha256:eeff0011",
			},
		},
	})
	s := screens.NewReviewScreen(p, 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "[CONFLICT]") {
		t.Errorf("view does not show [CONFLICT] badge for a locally-modified item:\n%s", s.View())
	}
}

// TestReviewScreen_View_ShowsUnchangedItems verifies that unchanged items are labelled with
// [UNCHANGED] so the user can confirm which files are already up to date.
func TestReviewScreen_View_ShowsUnchangedItems(t *testing.T) {
	p := planWithItems(domain.ModeDeployNew, []domain.PlanItem{
		{Ref: skillRef("lean-tdd"), Action: domain.ActionUnchanged},
	})
	s := screens.NewReviewScreen(p, 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "[UNCHANGED]") {
		t.Errorf("view does not show [UNCHANGED] badge for an unchanged item:\n%s", s.View())
	}
}

// TestReviewScreen_View_FullPlan_ShowsAllClassifications verifies that when a plan contains
// all four action types, the review renders all four badges.
func TestReviewScreen_View_FullPlan_ShowsAllClassifications(t *testing.T) {
	s := screens.NewReviewScreen(fullPlan(), 80, 60, plainStyles())

	view := collapseWhitespace(s.View())

	for _, badge := range []string{"[CREATE]", "[UPDATE]", "[CONFLICT]", "[UNCHANGED]"} {
		if !strings.Contains(view, badge) {
			t.Errorf("view does not contain %s badge for full plan:\n%s", badge, s.View())
		}
	}
}

// TestReviewScreen_View_EmptyPlan_ShowsNothingToDoMessage verifies that when the plan has
// no active work (all items are unchanged), the review shows an appropriate message rather
// than an empty list.
func TestReviewScreen_View_EmptyPlan_ShowsNothingToDoMessage(t *testing.T) {
	s := screens.NewReviewScreen(emptyPlan(), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Nothing to do") && !strings.Contains(view, "nothing to do") &&
		!strings.Contains(view, "up to date") {
		t.Errorf("view for an empty plan does not indicate nothing needs to be done:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Confirmation keys
// ---------------------------------------------------------------------------

// TestReviewScreen_YKey_ReturnsProceed verifies that pressing 'y' confirms the plan and
// returns Answered{Confirm: true}.
func TestReviewScreen_YKey_ReturnsProceed(t *testing.T) {
	s := screens.NewReviewScreen(fullPlan(), 80, 40, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if !s.Done() {
		t.Error("Done() = false after 'y'; want true")
	}
	ans := s.Answer()
	if ans.Status != domain.Answered || !ans.Confirm {
		t.Errorf("Answer() = {%v, %v}, want {Answered, true}", ans.Status, ans.Confirm)
	}
}

// TestReviewScreen_EnterKey_ReturnsProceed verifies that Enter also confirms the plan.
func TestReviewScreen_EnterKey_ReturnsProceed(t *testing.T) {
	s := screens.NewReviewScreen(fullPlan(), 80, 40, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter; want true")
	}
	if !s.Answer().Confirm {
		t.Error("Answer().Confirm = false after Enter; want true (Enter should proceed)")
	}
}

// TestReviewScreen_NKey_ReturnsCancel verifies that pressing 'n' declines the plan.
func TestReviewScreen_NKey_ReturnsCancel(t *testing.T) {
	s := screens.NewReviewScreen(fullPlan(), 80, 40, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	if !s.Done() {
		t.Error("Done() = false after 'n'; want true")
	}
	if s.Answer().Confirm {
		t.Error("Answer().Confirm = true after 'n'; want false (n should decline)")
	}
}

// TestReviewScreen_QKey_ReturnsCancel verifies that 'q' also declines.
func TestReviewScreen_QKey_ReturnsCancel(t *testing.T) {
	s := screens.NewReviewScreen(fullPlan(), 80, 40, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if !s.Done() {
		t.Error("Done() = false after 'q'; want true")
	}
	if s.Answer().Confirm {
		t.Error("Answer().Confirm = true after 'q'; want false")
	}
}

// TestReviewScreen_EscKey_ReturnsCancel verifies that Esc also declines the plan.
func TestReviewScreen_EscKey_ReturnsCancel(t *testing.T) {
	s := screens.NewReviewScreen(fullPlan(), 80, 40, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Done() {
		t.Error("Done() = false after Esc; want true")
	}
	if s.Answer().Confirm {
		t.Error("Answer().Confirm = true after Esc; want false")
	}
}

// ---------------------------------------------------------------------------
// Scrolling
// ---------------------------------------------------------------------------

// TestReviewScreen_Scroll_DoesNotPanic verifies that scrolling does not panic even when the
// offset is already at the start or would exceed the content length.
func TestReviewScreen_Scroll_DoesNotPanic(t *testing.T) {
	s := screens.NewReviewScreen(fullPlan(), 80, 20, plainStyles()) // narrow window to force scroll

	// Scroll down many times.
	for i := 0; i < 100; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	// Scroll up many times.
	for i := 0; i < 100; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyUp})
	}

	// Should still be alive and renderable.
	view := s.View()
	if view == "" {
		t.Error("View() returned an empty string after scrolling; want non-empty")
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestReviewScreen_Reset_ClearsDone verifies that Reset restores the screen to an idle state.
func TestReviewScreen_Reset_ClearsDone(t *testing.T) {
	s := screens.NewReviewScreen(fullPlan(), 80, 40, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !s.Done() {
		t.Fatal("precondition: Done() must be true before Reset")
	}

	s.Reset()

	if s.Done() {
		t.Error("Done() = true after Reset; want false")
	}
}

// ---------------------------------------------------------------------------
// Backup hint (FR-4)
// ---------------------------------------------------------------------------

// TestReviewScreen_View_ShowsBackupHint verifies that the review screen displays a visible
// warning reminding the user to back up existing workspace files before proceeding. Files may
// be overwritten by the deployment, so users must be warned before they answer the prompt.
func TestReviewScreen_View_ShowsBackupHint(t *testing.T) {
	s := screens.NewReviewScreen(fullPlan(), 80, 40, plainStyles())

	view := collapseWhitespace(strings.ToLower(s.View()))

	if !strings.Contains(view, "back up") &&
		!strings.Contains(view, "backup") &&
		!strings.Contains(view, "overwrite") {
		t.Errorf("review screen view does not show a backup warning; "+
			"users must be warned about potential file overwrites before they confirm:\n%s", s.View())
	}
}

// TestReviewScreen_View_BackupHint_VisibleAtNonZeroScrollOffset verifies that the backup hint
// cannot be scrolled out of view. The hint must reside in the non-scrollable header or footer
// region so the user always sees it regardless of how far they have scrolled through the plan.
func TestReviewScreen_View_BackupHint_VisibleAtNonZeroScrollOffset(t *testing.T) {
	// Build a plan with enough items to make the content taller than the visible window.
	var items []domain.PlanItem
	for i := 0; i < 30; i++ {
		items = append(items, domain.PlanItem{
			Ref:        agentRef(fmt.Sprintf("agent-%02d", i)),
			Action:     domain.ActionCreate,
			TargetPath: fmt.Sprintf("Agents/agent-%02d.md", i),
		})
	}
	p := planWithItems(domain.ModeDeployNew, items)
	// A small height forces the content to overflow the visible window.
	s := screens.NewReviewScreen(p, 80, 10, plainStyles())

	// Scroll down enough that the early content lines are no longer visible.
	for i := 0; i < 20; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	view := collapseWhitespace(strings.ToLower(s.View()))

	if !strings.Contains(view, "back up") &&
		!strings.Contains(view, "backup") &&
		!strings.Contains(view, "overwrite") {
		t.Errorf("backup hint disappeared after scrolling; "+
			"the hint must be placed in the non-scrollable header/footer region so it is always visible:\n%s",
			s.View())
	}
}

// ---------------------------------------------------------------------------
// Workflows-only mode rendering
// ---------------------------------------------------------------------------

// TestReviewScreen_WorkflowsOnlyMode_ShowsModeHeader verifies that the ReviewScreen renders
// the correct mode name "Update workflows only" in its header when given a plan whose mode
// is ModeWorkflowsOnly. The header must not fall through to the update-mode text.
func TestReviewScreen_WorkflowsOnlyMode_ShowsModeHeader(t *testing.T) {
	// Arrange: a plan whose mode is ModeWorkflowsOnly, with one item to give the screen
	// content to render.
	p := planWithItems(domain.ModeWorkflowsOnly, []domain.PlanItem{
		{
			Ref:        agentRef("orchestrator"),
			Action:     domain.ActionUpdate,
			TargetPath: "Agents/orchestrator.agent.md",
			Stale:      []domain.VersionDelta{{Field: "workflows", Deployed: "a", Source: "b"}},
		},
	})
	s := screens.NewReviewScreen(p, 80, 40, plainStyles())

	// Act
	view := collapseWhitespace(s.View())

	// Assert: the mode header must say "workflows only", not the plain "Update" label.
	if !strings.Contains(view, "workflows only") && !strings.Contains(view, "Workflows only") &&
		!strings.Contains(view, "workflows-only") && !strings.Contains(view, "Update workflows") {
		t.Errorf("review view for ModeWorkflowsOnly does not show the workflows-only mode name; "+
			"the mode header must identify the operation clearly:\n%s", s.View())
	}
}

// TestReviewScreen_WorkflowsOnlyMode_DoesNotFallThroughToUpdateText verifies that the review
// screen for ModeWorkflowsOnly does not show the plain "Update" mode label that is used for
// ModeUpdate plans. The two modes must not be visually indistinguishable.
func TestReviewScreen_WorkflowsOnlyMode_DoesNotFallThroughToUpdateText(t *testing.T) {
	// Arrange: two plans — one workflows-only, one plain update — rendered side-by-side.
	workflowsPlan := planWithItems(domain.ModeWorkflowsOnly, []domain.PlanItem{
		{Ref: agentRef("orchestrator"), Action: domain.ActionUpdate},
	})
	updatePlan := planWithItems(domain.ModeUpdate, []domain.PlanItem{
		{Ref: agentRef("orchestrator"), Action: domain.ActionUpdate},
	})

	wfScreen := screens.NewReviewScreen(workflowsPlan, 80, 40, plainStyles())
	upScreen := screens.NewReviewScreen(updatePlan, 80, 40, plainStyles())

	wfView := collapseWhitespace(wfScreen.View())
	upView := collapseWhitespace(upScreen.View())

	// The mode portion of the header must differ between the two screens.
	// If both say "Mode: Update" they are indistinguishable, which is wrong.
	if wfView == upView {
		t.Error("review screen for ModeWorkflowsOnly and ModeUpdate render identically; " +
			"the two modes must produce visually distinct mode headers so the user knows which operation is planned")
	}
}

// TestReviewScreen_View_BackupHint_FitsWithinConfiguredWidth verifies that the backup hint
// renders within the configured terminal width, consistent with every other line on the screen.
// Oversized lines would corrupt the layout on narrow terminals.
func TestReviewScreen_View_BackupHint_FitsWithinConfiguredWidth(t *testing.T) {
	const width = 60
	s := screens.NewReviewScreen(fullPlan(), width, 40, plainStyles())

	view := s.View()

	for i, line := range strings.Split(view, "\n") {
		// Trim trailing spaces that lipgloss Width() padding may add; the visible content
		// must still fit within the configured width.
		// Use rune count (not byte count) since box-drawing and Unicode characters occupy
		// one terminal column per rune, matching how lipgloss measures width.
		trimmed := strings.TrimRight(line, " ")
		if len([]rune(trimmed)) > width {
			t.Errorf("line %d exceeds configured width %d (rune count=%d): %q",
				i+1, width, len([]rune(trimmed)), trimmed)
		}
	}
}
