package screens_test

// review_test.go verifies the ReviewScreen overlay: rendering of all four plan action
// classifications (create, update, unchanged, locally-modified), version delta attribution
// for updated items, the empty-plan message, scrolling, and the y/n/esc confirmation keys.
// It also covers the mode-name mapping in the plan review header (modeDisplayName), which
// must render a distinct, correct label for every declared RunMode and must fall back to
// the raw mode string for any unmapped mode rather than defaulting to a real mode's name.

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
	return planWithItems(domain.ModeDeployWorkspace, []domain.PlanItem{
		{
			Ref:    agentRef("test-runner"),
			Action: domain.ActionUnchanged,
			Reason: "already up to date",
		},
	})
}

// fullPlan returns a Plan with one item of each action classification.
func fullPlan() domain.Plan {
	return planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
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
	p := planWithItems(domain.ModeDeployWorkspace, []domain.PlanItem{
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
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
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
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
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
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
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
	p := planWithItems(domain.ModeDeployWorkspace, []domain.PlanItem{
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
// Workflows-only mode rendering
// ---------------------------------------------------------------------------

// TestReviewScreen_UpdateWorkflowsMode_ShowsModeHeader verifies that the ReviewScreen renders
// the correct mode name "Update workflows" in its header when given a plan whose mode
// is ModeUpdateWorkflows. The header must not fall through to the plain "Update workspace" text.
func TestReviewScreen_UpdateWorkflowsMode_ShowsModeHeader(t *testing.T) {
	// Arrange: a plan whose mode is ModeUpdateWorkflows, with one item to give the screen
	// content to render.
	p := planWithItems(domain.ModeUpdateWorkflows, []domain.PlanItem{
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

	// Assert: the mode header must say "Update workflows", not the plain "Update workspace" label.
	if !strings.Contains(view, "Update workflows") {
		t.Errorf("review view for ModeUpdateWorkflows does not show 'Update workflows' in the mode header; "+
			"the header must identify the operation clearly:\n%s", s.View())
	}
}

// TestReviewScreen_UpdateWorkflowsMode_DoesNotFallThroughToUpdateWorkspaceText verifies that
// the review screen for ModeUpdateWorkflows does not show the "Update workspace" mode label
// that is used for ModeUpdateWorkspace plans. The two modes must not be visually
// indistinguishable in the review header.
func TestReviewScreen_UpdateWorkflowsMode_DoesNotFallThroughToUpdateWorkspaceText(t *testing.T) {
	// Arrange: two plans — one update-workflows, one update-workspace — rendered side-by-side.
	workflowsPlan := planWithItems(domain.ModeUpdateWorkflows, []domain.PlanItem{
		{Ref: agentRef("orchestrator"), Action: domain.ActionUpdate},
	})
	updatePlan := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{Ref: agentRef("orchestrator"), Action: domain.ActionUpdate},
	})

	wfScreen := screens.NewReviewScreen(workflowsPlan, 80, 40, plainStyles())
	upScreen := screens.NewReviewScreen(updatePlan, 80, 40, plainStyles())

	wfView := collapseWhitespace(wfScreen.View())
	upView := collapseWhitespace(upScreen.View())

	// The mode portion of the header must differ between the two screens.
	if wfView == upView {
		t.Error("review screen for ModeUpdateWorkflows and ModeUpdateWorkspace render identically; " +
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

// ---------------------------------------------------------------------------
// Gap list owner attribution
// ---------------------------------------------------------------------------

// TestReviewScreen_GapList_InjectionGapWithOwner_ShowsAttributionInBullet verifies that a
// plan gap bearing an owner displays that owning agent on its bullet in the pre-write gap
// list, including injection gaps whose Subject is the region name and whose Owner is set.
func TestReviewScreen_GapList_InjectionGapWithOwner_ShowsAttributionInBullet(t *testing.T) {
	// Arrange
	p := planWithItems(domain.ModeDeployWorkspace, []domain.PlanItem{
		{Ref: agentRef("test-runner"), Action: domain.ActionCreate, TargetPath: "Agents/test-runner.agent.md"},
	})
	p.Gaps = []domain.Gap{
		{
			Kind:    domain.GapEmptyInjection,
			Subject: "CodebaseContext",
			Owner:   "harness-bug-hunter",
		},
	}

	// Act
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	// Assert: the owning agent must appear on the gap bullet line.
	if !strings.Contains(view, "agent: harness-bug-hunter") {
		t.Errorf("review gap list does not show owner attribution for injection gap;\n"+
			"want a line containing \"agent: harness-bug-hunter\":\n%s", view)
	}
}

// TestReviewScreen_GapList_GapSubjectAlreadyNamesOwner_NoAgentNameDuplicate verifies that
// when a gap's Subject text already names the owning agent — as tool-mapping gaps do — the
// agent name appears only once on the gap bullet line, not once in the Subject and again as
// a "(agent: …)" suffix.
func TestReviewScreen_GapList_GapSubjectAlreadyNamesOwner_NoAgentNameDuplicate(t *testing.T) {
	// Arrange
	const owner = "harness-bug-hunter"
	p := planWithItems(domain.ModeDeployWorkspace, []domain.PlanItem{
		{Ref: agentRef("test-runner"), Action: domain.ActionCreate, TargetPath: "Agents/test-runner.agent.md"},
	})
	p.Gaps = []domain.Gap{
		{
			Kind:    domain.GapUnmappedTool,
			Subject: `generic tool "bash" has no harness mapping for agent "harness-bug-hunter"`,
			Owner:   owner,
		},
	}

	// Act
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	// Assert: the owner name must appear at most once on the gap bullet line.
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "bash") && strings.Contains(line, "•") {
			count := strings.Count(line, owner)
			if count > 1 {
				t.Errorf("gap bullet for tool-mapping gap names owner %q %d times on one line; want at most 1:\n%q",
					owner, count, line)
			}
		}
	}
}

// TestReviewScreen_GapList_BlankOwner_NoAttributionSuffix verifies that a gap whose Owner is
// empty renders its bullet line without any "(agent: …)" attribution suffix.
func TestReviewScreen_GapList_BlankOwner_NoAttributionSuffix(t *testing.T) {
	// Arrange
	p := planWithItems(domain.ModeDeployWorkspace, []domain.PlanItem{
		{Ref: agentRef("test-runner"), Action: domain.ActionCreate, TargetPath: "Agents/test-runner.agent.md"},
	})
	p.Gaps = []domain.Gap{
		{
			Kind:    domain.GapNoModel,
			Subject: "fast-tier",
			Owner:   "", // run-level gap with no agent attribution
		},
	}

	// Act
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	// Assert: the gap item is visible but without any attribution suffix.
	if !strings.Contains(view, "fast-tier") {
		t.Errorf("review view does not show gap subject for blank-owner gap:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "fast-tier") {
			if strings.Contains(line, "(agent:") {
				t.Errorf("gap bullet for blank-owner gap contains attribution suffix; want none:\n%q", line)
			}
		}
	}
}

// TestReviewScreen_GapList_Attribution_DoesNotAffectPlanActionSection verifies that adding
// owner attribution to gap bullet lines does not disturb the plan action section. The [CREATE]
// badge and agent key must continue to appear correctly even when gaps carry owners.
func TestReviewScreen_GapList_Attribution_DoesNotAffectPlanActionSection(t *testing.T) {
	// Arrange: a plan with one create action and one owned gap.
	p := planWithItems(domain.ModeDeployWorkspace, []domain.PlanItem{
		{Ref: agentRef("new-agent"), Action: domain.ActionCreate, TargetPath: "Agents/new-agent.agent.md"},
	})
	p.Gaps = []domain.Gap{
		{
			Kind:    domain.GapEmptyInjection,
			Subject: "CodebaseContext",
			Owner:   "harness-bug-hunter",
		},
	}

	// Act
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := collapseWhitespace(s.View())

	// Assert: the action badge and agent key are still visible.
	if !strings.Contains(view, "[CREATE]") {
		t.Errorf("plan action section does not show [CREATE] badge when gap list carries owners;\n"+
			"attribution changes must be scoped to the gap bullet lines:\n%s", s.View())
	}
	if !strings.Contains(view, "new-agent") {
		t.Errorf("plan action section does not show agent key when gap list carries owners:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Deleted-indicator delta rendering
// ---------------------------------------------------------------------------

// TestReviewScreen_DeletedDelta_EmptySource_ShowsDeletedText verifies that a VersionDelta
// whose Source field is empty — the shared convention for a removed artifact — renders the
// word "deleted" after the arrow in the plan review. A bare trailing arrow gives the user no
// signal that the workflow has been removed, which is the user-visible bug this fixes.
func TestReviewScreen_DeletedDelta_EmptySource_ShowsDeletedText(t *testing.T) {
	// Arrange: one update item whose sole version delta has an empty Source, representing a
	// removed workflow.
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:        agentRef("orchestrator"),
			Action:     domain.ActionUpdate,
			TargetPath: "Agents/orchestrator.agent.md",
			Stale: []domain.VersionDelta{
				{Field: "workflow:removed-flow", Deployed: "1.2.0", Source: ""},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())

	// Act
	view := s.View()

	// Assert: "deleted" must appear in the rendered output so the user can see the removal.
	if !strings.Contains(view, "deleted") {
		t.Errorf("review screen does not render 'deleted' for an empty-Source VersionDelta;\n"+
			"a removed workflow (Source == \"\") must show '→ deleted' so the user is informed:\n%s", view)
	}
}

// TestReviewScreen_DeletedDelta_PopulatedSource_RendersExactlyAsToday verifies that a
// VersionDelta whose Source is non-empty continues to render the source value verbatim after
// the arrow, unchanged from the current behaviour. Only the empty-Source case changes.
func TestReviewScreen_DeletedDelta_PopulatedSource_RendersExactlyAsToday(t *testing.T) {
	// Arrange: an update item with a normal version bump (both Deployed and Source non-empty).
	const field = "workflow:existing-flow"
	const deployed = "1.0.0"
	const source = "1.1.0"

	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:        agentRef("orchestrator"),
			Action:     domain.ActionUpdate,
			TargetPath: "Agents/orchestrator.agent.md",
			Stale: []domain.VersionDelta{
				{Field: field, Deployed: deployed, Source: source},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())

	// Act
	view := s.View()

	// Assert: all three pieces of information must appear in the rendered output.
	// The raw "workflow:existing-flow" field name is translated to a user-facing label;
	// we check for the workflow ID itself ("existing-flow") which the label preserves.
	if !strings.Contains(view, "existing-flow") {
		t.Errorf("review screen does not show the workflow ID for a normal delta:\n%s", view)
	}
	if !strings.Contains(view, deployed) {
		t.Errorf("review screen does not show the deployed version for a normal delta:\n%s", view)
	}
	if !strings.Contains(view, source) {
		t.Errorf("review screen does not show the source version for a normal delta:\n%s", view)
	}
	// The word "deleted" must not appear for a non-empty Source.
	if strings.Contains(collapseWhitespace(view), "deleted") {
		t.Errorf("review screen shows 'deleted' for a non-empty Source delta; "+
			"only empty-Source deltas may use that label:\n%s", view)
	}
}

// TestReviewScreen_DeletedDelta_MixedDeltas_OnlyEmptySourceShowsDeleted verifies the mixed
// case: a single plan item carries several VersionDeltas where some have an empty Source and
// some have a populated Source. The word "deleted" must appear exactly for the empty-Source
// entries, and the populated-Source entries must still render their source values.
func TestReviewScreen_DeletedDelta_MixedDeltas_OnlyEmptySourceShowsDeleted(t *testing.T) {
	// Arrange: three deltas on one item — one removal, two normal version bumps.
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:        agentRef("orchestrator"),
			Action:     domain.ActionUpdate,
			TargetPath: "Agents/orchestrator.agent.md",
			Stale: []domain.VersionDelta{
				{Field: "workflow:removed-flow", Deployed: "2.0.0", Source: ""},    // removal
				{Field: "workflow:updated-flow", Deployed: "1.0.0", Source: "1.1.0"}, // version bump
				{Field: "workflow:another-bump", Deployed: "3.0.0", Source: "3.1.0"}, // version bump
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())

	// Act
	view := s.View()

	// Assert: "deleted" appears for the removal.
	if !strings.Contains(view, "deleted") {
		t.Errorf("review screen does not show 'deleted' for the empty-Source delta in a mixed set:\n%s", view)
	}

	// Assert: source versions for the non-removal deltas are still visible.
	if !strings.Contains(view, "1.1.0") {
		t.Errorf("review screen does not show source version '1.1.0' for a non-empty-Source delta "+
			"in the same item:\n%s", view)
	}
	if !strings.Contains(view, "3.1.0") {
		t.Errorf("review screen does not show source version '3.1.0' for a non-empty-Source delta "+
			"in the same item:\n%s", view)
	}

	// Assert: the removed workflow field name is visible alongside the "deleted" label.
	if !strings.Contains(view, "removed-flow") {
		t.Errorf("review screen does not show the field name for the removed workflow:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Mode-name mapping — all declared modes and unmapped fallback (T2.3)
// ---------------------------------------------------------------------------

// planForMode is a helper that builds a minimal one-item plan using the given mode.
// The single item is an unchanged agent so the plan is non-empty but does not exercise
// create/update/conflict rendering — only the mode header matters in these tests.
func planForMode(mode domain.RunMode) domain.Plan {
	return planWithItems(mode, []domain.PlanItem{
		{Ref: agentRef("test-agent"), Action: domain.ActionUnchanged},
	})
}

// TestReviewScreen_DeployWorkspaceMode_ShowsModeHeader verifies that the plan review header
// renders "Deploy workspace" when the plan's mode is ModeDeployWorkspace.
func TestReviewScreen_DeployWorkspaceMode_ShowsModeHeader(t *testing.T) {
	s := screens.NewReviewScreen(planForMode(domain.ModeDeployWorkspace), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Deploy workspace") {
		t.Errorf("review header for ModeDeployWorkspace does not show 'Deploy workspace':\n%s", s.View())
	}
}

// TestReviewScreen_UpdateWorkspaceMode_ShowsModeHeader verifies that the plan review header
// renders "Update workspace" when the plan's mode is ModeUpdateWorkspace.
func TestReviewScreen_UpdateWorkspaceMode_ShowsModeHeader(t *testing.T) {
	s := screens.NewReviewScreen(planForMode(domain.ModeUpdateWorkspace), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Update workspace") {
		t.Errorf("review header for ModeUpdateWorkspace does not show 'Update workspace':\n%s", s.View())
	}
}

// TestReviewScreen_PromoteToGenericMode_ShowsModeHeader verifies that the plan review header
// renders "Promote to generic" when the plan's mode is ModePromoteToGeneric.
func TestReviewScreen_PromoteToGenericMode_ShowsModeHeader(t *testing.T) {
	s := screens.NewReviewScreen(planForMode(domain.ModePromoteToGeneric), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Promote to generic") {
		t.Errorf("review header for ModePromoteToGeneric does not show 'Promote to generic':\n%s", s.View())
	}
}

// TestReviewScreen_TransformHarnessMode_ShowsModeHeader verifies that the plan review header
// renders "Transform harness" when the plan's mode is ModeTransformHarness.
func TestReviewScreen_TransformHarnessMode_ShowsModeHeader(t *testing.T) {
	s := screens.NewReviewScreen(planForMode(domain.ModeTransformHarness), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Transform harness") {
		t.Errorf("review header for ModeTransformHarness does not show 'Transform harness':\n%s", s.View())
	}
}

// TestReviewScreen_DeployAgentsMode_ShowsModeHeader verifies that the plan review header
// renders "Deploy agents" when the plan's mode is ModeDeployAgents.
func TestReviewScreen_DeployAgentsMode_ShowsModeHeader(t *testing.T) {
	s := screens.NewReviewScreen(planForMode(domain.ModeDeployAgents), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Deploy agents") {
		t.Errorf("review header for ModeDeployAgents does not show 'Deploy agents':\n%s", s.View())
	}
}

// TestReviewScreen_DeployHooksMode_ShowsModeHeader verifies that the plan review header
// renders "Deploy hooks" when the plan's mode is ModeDeployHooks.
func TestReviewScreen_DeployHooksMode_ShowsModeHeader(t *testing.T) {
	s := screens.NewReviewScreen(planForMode(domain.ModeDeployHooks), 80, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "Deploy hooks") {
		t.Errorf("review header for ModeDeployHooks does not show 'Deploy hooks':\n%s", s.View())
	}
}

// TestReviewScreen_AllSurvivingModes_TableDriven runs each of the seven declared modes
// through the review header and verifies the correct display name appears. A failure here
// means either the constant value was not updated or modeDisplayName is missing an entry.
func TestReviewScreen_AllSurvivingModes_TableDriven(t *testing.T) {
	cases := []struct {
		mode        domain.RunMode
		wantLabel   string
	}{
		{domain.ModeDeployWorkspace, "Deploy workspace"},
		{domain.ModeUpdateWorkspace, "Update workspace"},
		{domain.ModeUpdateWorkflows, "Update workflows"},
		{domain.ModeDeployAgents, "Deploy agents"},
		{domain.ModeDeployHooks, "Deploy hooks"},
		{domain.ModePromoteToGeneric, "Promote to generic"},
		{domain.ModeTransformHarness, "Transform harness"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.mode), func(t *testing.T) {
			s := screens.NewReviewScreen(planForMode(tc.mode), 80, 40, plainStyles())
			view := collapseWhitespace(s.View())
			if !strings.Contains(view, tc.wantLabel) {
				t.Errorf("review header for mode %q does not contain %q:\n%s",
					tc.mode, tc.wantLabel, s.View())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Version-delta label rendering (T3.1)
// ---------------------------------------------------------------------------

// TestReviewScreen_VersionDelta_NoDuplicateBetweenReasonAndDeltaLines verifies that when a
// stale plan item carries both a Reason string (as the plan builder populates it from version
// deltas) and per-field Stale deltas, each version change appears exactly once in the review
// output. The current bug renders the same change twice — once as a Reason line and once as
// a per-delta line — which confuses users who see duplicate information.
func TestReviewScreen_VersionDelta_NoDuplicateBetweenReasonAndDeltaLines(t *testing.T) {
	// Use version strings that are unique enough to count occurrences reliably.
	const deployed = "9.8.7"
	const source = "9.9.0"

	// Reason mimics what buildAgentUpdateReasons/formatVersionDeltas generates for standard
	// version fields, which is the duplicated content the fix must suppress.
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:    agentRef("stale-agent"),
			Action: domain.ActionUpdate,
			Reason: fmt.Sprintf("version changed from %q to %q", deployed, source),
			Stale: []domain.VersionDelta{
				{Field: "version", Deployed: deployed, Source: source},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	// The deployed version string must appear at most once: the per-delta line is the correct
	// rendering; the Reason line repeats it and must be suppressed for stale items that have
	// per-delta lines to display.
	deployedCount := strings.Count(view, deployed)
	if deployedCount > 1 {
		t.Errorf("deployed version %q appears %d times in the plan review (want at most 1); "+
			"the Reason line and per-delta lines are duplicating the same version change — "+
			"only the per-delta lines should be shown:\n%s",
			deployed, deployedCount, view)
	}
}

// TestReviewScreen_VersionDelta_ToolMappingsVersionUsesUserFacingLabel verifies that the
// raw internal field name "tool_mappings_version" is replaced by the user-facing label
// "tool mappings hash" in the plan review. Raw field names are implementation details that
// should not be exposed to the user.
func TestReviewScreen_VersionDelta_ToolMappingsVersionUsesUserFacingLabel(t *testing.T) {
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:    agentRef("some-agent"),
			Action: domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: "tool_mappings_version", Deployed: "aaaa1111", Source: "bbbb2222"},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	if !strings.Contains(view, "tool mappings hash") {
		t.Errorf("plan review does not show user-facing label \"tool mappings hash\" for "+
			"\"tool_mappings_version\" delta; raw internal field names must not be shown to users:\n%s", view)
	}
}

// TestReviewScreen_VersionDelta_HarnessVersionUsesUserFacingLabel verifies that the raw
// internal field name "harness_version" is replaced by the user-facing label "harness version".
func TestReviewScreen_VersionDelta_HarnessVersionUsesUserFacingLabel(t *testing.T) {
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:    agentRef("some-agent"),
			Action: domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: "harness_version", Deployed: "1.0", Source: "1.1"},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	if !strings.Contains(view, "harness version") {
		t.Errorf("plan review does not show user-facing label \"harness version\" for "+
			"\"harness_version\" delta; raw internal field names must not be shown to users:\n%s", view)
	}
}

// TestReviewScreen_VersionDelta_InjectionsVersionUsesUserFacingLabel verifies that the raw
// internal field name "injections_version" is replaced by the user-facing label
// "harness injection version (body tag)". The explanation is necessary because the field
// name "injections_version" is ambiguous — it actually refers to a body-tag version
// attribute, not a frontmatter key, which the label makes clear.
func TestReviewScreen_VersionDelta_InjectionsVersionUsesUserFacingLabel(t *testing.T) {
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:    agentRef("some-agent"),
			Action: domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: "injections_version", Deployed: "2.0", Source: "2.1"},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	if !strings.Contains(view, "harness injection version") {
		t.Errorf("plan review does not show user-facing label containing \"harness injection version\" for "+
			"\"injections_version\" delta; raw field names must not be exposed to users:\n%s", view)
	}
}

// TestReviewScreen_VersionDelta_CatalogVersionFieldUsesUserFacingLabel verifies that the raw
// internal field name "version" (the catalog version stamp) is replaced by the user-facing
// label "catalog version". The bare word "version" is too ambiguous in context; the label
// must clarify that it is the catalog-level version field.
func TestReviewScreen_VersionDelta_CatalogVersionFieldUsesUserFacingLabel(t *testing.T) {
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:    agentRef("some-agent"),
			Action: domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: "version", Deployed: "3.0", Source: "3.1"},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	if !strings.Contains(view, "catalog version") {
		t.Errorf("plan review does not show user-facing label \"catalog version\" for "+
			"the \"version\" delta field; the bare word \"version\" is too ambiguous in context:\n%s", view)
	}
}

// TestReviewScreen_VersionDelta_BundleVersionUsesUserFacingLabel verifies that the raw
// internal field name "bundle_version" is replaced by the user-facing label "bundle version".
func TestReviewScreen_VersionDelta_BundleVersionUsesUserFacingLabel(t *testing.T) {
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:    agentRef("some-agent"),
			Action: domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: "bundle_version", Deployed: "5.0", Source: "5.1"},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	if !strings.Contains(view, "bundle version") {
		t.Errorf("plan review does not show user-facing label \"bundle version\" for "+
			"\"bundle_version\" delta; raw internal field names must not be shown to users:\n%s", view)
	}
}

// TestReviewScreen_VersionDelta_OrchestratorInjectionsVersionUsesUserFacingLabel verifies
// that the raw internal field name "orchestrator_injections_version" is replaced by the
// user-facing label "orchestrator injection version".
func TestReviewScreen_VersionDelta_OrchestratorInjectionsVersionUsesUserFacingLabel(t *testing.T) {
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:    agentRef("some-agent"),
			Action: domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: "orchestrator_injections_version", Deployed: "4.0", Source: "4.1"},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	if !strings.Contains(view, "orchestrator injection version") {
		t.Errorf("plan review does not show user-facing label \"orchestrator injection version\" for "+
			"\"orchestrator_injections_version\" delta; raw field names must not be exposed:\n%s", view)
	}
}

// TestReviewScreen_VersionDelta_ProtocolVersionUsesUserFacingLabel verifies that the raw
// internal field name "protocol_version" (the ProtocolDeltaField constant from the plan
// package) is replaced by the user-facing label "harness protocol version". This delta
// reaches the review screen via item.Stale and must be labelled like every other delta.
func TestReviewScreen_VersionDelta_ProtocolVersionUsesUserFacingLabel(t *testing.T) {
	// "protocol_version" is the value of plan.ProtocolDeltaField. Use it directly here so
	// the test remains accurate without importing the plan package.
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:    agentRef("some-agent"),
			Action: domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: "protocol_version", Deployed: "1.9", Source: "1.10"},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	if !strings.Contains(view, "harness protocol version") {
		t.Errorf("plan review does not show user-facing label \"harness protocol version\" for "+
			"\"protocol_version\" delta; the raw field name must be translated for users:\n%s", view)
	}
}

// TestReviewScreen_VersionDelta_WorkflowPrefixedFieldUsesReadableLabel verifies that a
// VersionDelta whose field follows the "workflow:<id>" convention renders with a readable
// label that includes the workflow ID but strips the raw "workflow:" prefix. A rendered
// label of "workflow my-workflow version" (or similar) is user-facing; the raw
// "workflow:my-workflow" prefix is not.
func TestReviewScreen_VersionDelta_WorkflowPrefixedFieldUsesReadableLabel(t *testing.T) {
	const workflowID = "my-workflow"
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:    agentRef("orchestrator"),
			Action: domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: "workflow:" + workflowID, Deployed: "1.0.0", Source: "1.1.0"},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	// The workflow ID must appear so the user knows which workflow changed.
	if !strings.Contains(view, workflowID) {
		t.Errorf("plan review does not show workflow ID %q in the delta label; "+
			"the workflow ID must be preserved so the user can identify which workflow changed:\n%s",
			workflowID, view)
	}

	// The raw "workflow:" prefix must not appear as the label delimiter; the colon-separated
	// form is an internal naming convention that is not meaningful to users.
	if strings.Contains(view, "workflow:"+workflowID) {
		t.Errorf("plan review shows raw \"workflow:%s\" field name in the output; "+
			"the \"workflow:\" prefix must be translated to a readable label:\n%s",
			workflowID, view)
	}
}

// ---------------------------------------------------------------------------
// Version-delta rendering — regression guards (T3.1)
// ---------------------------------------------------------------------------

// TestReviewScreen_VersionDelta_ActionCreateWithNoDeltas_RendersCorrectly is a regression
// guard verifying that ActionCreate items — which never carry version deltas — continue to
// render their [CREATE] badge and agent key correctly after the delta-label changes. The
// label mapping must not disturb items that have an empty Stale slice.
func TestReviewScreen_VersionDelta_ActionCreateWithNoDeltas_RendersCorrectly(t *testing.T) {
	p := planWithItems(domain.ModeDeployWorkspace, []domain.PlanItem{
		{
			Ref:        agentRef("brand-new-agent"),
			Action:     domain.ActionCreate,
			TargetPath: "Agents/brand-new-agent.agent.md",
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "[CREATE]") {
		t.Errorf("regression: ActionCreate item no longer shows [CREATE] badge after delta-label changes:\n%s",
			s.View())
	}
	if !strings.Contains(view, "brand-new-agent") {
		t.Errorf("regression: ActionCreate item no longer shows agent key after delta-label changes:\n%s",
			s.View())
	}
}

// TestReviewScreen_VersionDelta_UnmappedFieldFallsBackToRawName is a regression guard
// verifying that a VersionDelta whose field name has no entry in the label map renders the
// raw field name verbatim rather than crashing or emitting an empty label. New, previously
// unseen field names must degrade gracefully so the user still sees something meaningful.
func TestReviewScreen_VersionDelta_UnmappedFieldFallsBackToRawName(t *testing.T) {
	const unknownField = "some_future_field_xyz"
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:    agentRef("some-agent"),
			Action: domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: unknownField, Deployed: "1.0", Source: "2.0"},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	// The raw field name must appear as a fallback so the user has some indicator of what changed.
	if !strings.Contains(view, unknownField) {
		t.Errorf("plan review does not show raw field name %q for an unmapped delta field; "+
			"unmapped fields must fall back to the raw name rather than silently omitting the label:\n%s",
			unknownField, view)
	}
}

// TestReviewScreen_VersionDelta_MultipleDeltas_AllRendered is a regression guard verifying
// that when a single plan item carries multiple version deltas, every delta is rendered in
// the review output. The label-mapping changes must not drop or skip any delta entries.
func TestReviewScreen_VersionDelta_MultipleDeltas_AllRendered(t *testing.T) {
	p := planWithItems(domain.ModeUpdateWorkspace, []domain.PlanItem{
		{
			Ref:    agentRef("multi-delta-agent"),
			Action: domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: "version", Deployed: "1.0", Source: "1.1"},
				{Field: "harness_version", Deployed: "2.0", Source: "2.1"},
				{Field: "tool_mappings_version", Deployed: "aaa", Source: "bbb"},
			},
		},
	})
	s := screens.NewReviewScreen(p, 120, 80, plainStyles())
	view := s.View()

	// All three source versions must appear: if any delta is dropped, its source version is absent.
	for _, src := range []string{"1.1", "2.1", "bbb"} {
		if !strings.Contains(view, src) {
			t.Errorf("plan review does not show source version %q; "+
				"all version deltas must be rendered when a plan item carries multiple deltas:\n%s",
				src, view)
		}
	}
}

// ---------------------------------------------------------------------------
// Physical-row scroll behavior with line wrapping
// ---------------------------------------------------------------------------

// longItemPlan builds a plan with n create items whose rendered review lines (badge +
// ref path) exceed 40 terminal columns. At width=40, each such item's badge line wraps
// to at least two physical terminal rows, making physical row count greater than logical
// line count. This fixture is the basis for all physical-row scroll tests below.
func longItemPlan(n int) domain.Plan {
	items := make([]domain.PlanItem, n)
	for i := range items {
		items[i] = domain.PlanItem{
			Ref:        agentRef(fmt.Sprintf("some-long-agent-name-%02d", i)),
			Action:     domain.ActionCreate,
			TargetPath: fmt.Sprintf("Agents/a-very-long-subdirectory-path/some-long-agent-name-%02d.agent.md", i),
		}
	}
	return planWithItems(domain.ModeDeployWorkspace, items)
}

// scrollIndicatorTotal parses the scroll position indicator line in a rendered view and
// returns the total row count it reports. The indicator format is "── A–B of C lines ──";
// this function extracts and returns C. Returns 0, false if no indicator line is found.
func scrollIndicatorTotal(view string) (int, bool) {
	for _, line := range strings.Split(view, "\n") {
		if !strings.Contains(line, " of ") || !strings.Contains(line, " lines") {
			continue
		}
		ofIdx := strings.Index(line, " of ")
		if ofIdx < 0 {
			continue
		}
		rest := line[ofIdx+4:] // characters after " of "
		linesIdx := strings.Index(rest, " lines")
		if linesIdx < 0 {
			continue
		}
		numStr := strings.TrimSpace(rest[:linesIdx])
		var n int
		if _, err := fmt.Sscanf(numStr, "%d", &n); err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}

// physicalRowCount returns the number of physical terminal rows a rendered view string
// occupies, by counting newline separators. A view that does not end in a newline has
// its final line counted as a row. This matches how a terminal emulator consumes the string.
func physicalRowCount(view string) int {
	if view == "" {
		return 0
	}
	n := strings.Count(view, "\n")
	if !strings.HasSuffix(view, "\n") {
		n++
	}
	return n
}

// TestReviewScreen_PhysicalRows_ViewAtOffset0DoesNotExceedTerminalHeight verifies that
// when a ReviewScreen is created with a narrow terminal width that forces long review lines
// to wrap (40 columns here), the View() output at scroll offset 0 does not exceed the
// configured terminal height in physical rows. The current implementation sizes the scroll
// window by counting logical review-line entries, not physical rows, so this test fails
// until the scroll window computation is rewritten to accumulate physical rows.
func TestReviewScreen_PhysicalRows_ViewAtOffset0DoesNotExceedTerminalHeight(t *testing.T) {
	// Arrange: 40-column terminal forces each long item line to wrap to multiple physical
	// rows. 10 items is enough to fill and exceed the scroll window when wrapping is counted.
	const width = 40
	const height = 15

	s := screens.NewReviewScreen(longItemPlan(10), width, height, plainStyles())

	// Act: render at scroll offset 0 (the default initial position).
	view := s.View()

	// Assert: physical rows must fit within the configured terminal height.
	rows := physicalRowCount(view)
	if rows > height {
		t.Errorf("View() at scroll offset 0 occupies %d physical rows but terminal height is %d; "+
			"the scroll window must be computed in physical rows (accounting for lipgloss line "+
			"wrapping) not in logical review-line count:\n%s",
			rows, height, view)
	}
}

// TestReviewScreen_PhysicalRows_ScrollIndicator_ReflectsPhysicalRowCount verifies that the
// scroll-position indicator ("── A–B of C lines ──") reports a total based on physical
// terminal rows, not the count of logical review-line entries. When line wrapping is in
// effect at a narrow width, physical rows exceed logical lines; the indicator must report
// the larger physical total so the user knows how much content remains to scroll through.
func TestReviewScreen_PhysicalRows_ScrollIndicator_ReflectsPhysicalRowCount(t *testing.T) {
	// Arrange: narrow terminal so all long lines wrap; small height to force the indicator.
	// 12 create items → 4 header lines + 12 item lines = 16 logical lines.
	// At width=40, each line wraps to multiple physical rows → physical total is well above 16.
	const width = 40
	const height = 8
	const itemCount = 12
	// 4 header lines (mode, blank, count, separator) + 1 badge line per item.
	const logicalLineCount = 4 + itemCount

	s := screens.NewReviewScreen(longItemPlan(itemCount), width, height, plainStyles())

	// Act
	view := s.View()

	// Assert: the scroll indicator must appear (content exceeds visible window).
	total, ok := scrollIndicatorTotal(view)
	if !ok {
		t.Fatalf("scroll position indicator not found in view; "+
			"with %d items at height %d, scrolling must be active and the indicator visible:\n%s",
			itemCount, height, view)
	}

	// Assert: indicator total must exceed the logical line count, proving it uses physical rows.
	if total <= logicalLineCount {
		t.Errorf("scroll indicator total=%d is not greater than logical line count %d; "+
			"the indicator must report physical rows (which exceed logical lines when wrapping "+
			"is in effect), not the count of pre-rendered review-line entries:\n%s",
			total, logicalLineCount, view)
	}
}

// TestReviewScreen_PhysicalRows_NoWrapping_BehavesIdentically is a regression guard that
// verifies on a wide terminal where no review lines wrap, the physical-row scroll logic
// produces the same visible result as the original logical-line logic. When each logical
// line occupies exactly one physical row the two approaches are equivalent; the fix must
// not change behaviour for plans that fit without wrapping.
func TestReviewScreen_PhysicalRows_NoWrapping_BehavesIdentically(t *testing.T) {
	// Arrange: 200-column terminal — all review lines are well under 200 columns, no wrapping.
	const width = 200
	const height = 40

	s := screens.NewReviewScreen(longItemPlan(10), width, height, plainStyles())

	// Act
	view := s.View()

	// Assert: physical rows must fit within terminal height (regression guard).
	rows := physicalRowCount(view)
	if rows > height {
		t.Errorf("View() on a wide terminal (width=%d, no wrapping) occupies %d physical rows "+
			"but terminal height is %d; plans with no line-wrapping must render identically "+
			"after the physical-row scroll fix is applied",
			width, rows, height)
	}

	// Assert: the view must not be empty.
	if view == "" {
		t.Error("View() returned empty string for a valid plan at wide terminal width")
	}
}

// TestReviewScreen_PhysicalRows_AfterResizeNarrow_ViewDoesNotExceedNewTerminalHeight verifies
// that after Resize() to a narrower terminal width, the scroll window accounts for the
// additional physical rows introduced by line wrapping. View() must fit within the new
// terminal height even though the same logical lines now occupy more physical rows than
// they did at the original width.
func TestReviewScreen_PhysicalRows_AfterResizeNarrow_ViewDoesNotExceedNewTerminalHeight(t *testing.T) {
	// Arrange: start wide (no wrapping), then resize to narrow (wrapping forces more rows).
	const wideWidth = 200
	const narrowWidth = 40
	const height = 15

	s := screens.NewReviewScreen(longItemPlan(10), wideWidth, height, plainStyles())

	// Act: resize to a narrow terminal where all long item lines wrap to multiple rows.
	s.Resize(narrowWidth, height)
	view := s.View()

	// Assert: physical rows must not exceed the (unchanged) terminal height.
	rows := physicalRowCount(view)
	if rows > height {
		t.Errorf("View() after Resize to width=%d occupies %d physical rows but terminal height is %d; "+
			"Resize must recompute the scroll window in physical rows so the view does not "+
			"overflow the terminal when the width narrows and line wrapping increases:\n%s",
			narrowWidth, rows, height, view)
	}
}

// TestReviewScreen_UnmappedMode_ShowsRawModeStringNotAKnownModeName verifies that when a
// plan carries a RunMode value that has no entry in the mode-name mapping, the review
// header renders the raw mode string rather than silently falling back to a real mode's
// display name (e.g., "Deploy workspace"). A raw-string fallback makes the problem
// immediately visible; a real-name fallback would masquerade as a normal deployment.
func TestReviewScreen_UnmappedMode_ShowsRawModeStringNotAKnownModeName(t *testing.T) {
	// Arrange: a plan whose mode is not in any mapping table.
	const unmappedMode = domain.RunMode("fictional-unknown-mode")
	s := screens.NewReviewScreen(planForMode(unmappedMode), 80, 40, plainStyles())

	// Act
	view := collapseWhitespace(s.View())

	// Assert: the raw mode string must be visible in the header.
	if !strings.Contains(view, "fictional-unknown-mode") {
		t.Errorf("review header for an unmapped mode does not show the raw mode string %q; "+
			"an unhandled mode must be visibly wrong rather than defaulting to a known mode name:\n%s",
			unmappedMode, s.View())
	}

	// Assert: the view must not show any known real mode name for an unmapped mode.
	// The most dangerous false fallback is "Deploy workspace" (the old hardcoded default).
	knownModeNames := []string{
		"Deploy workspace",
		"Update workspace",
		"Update workflows",
		"Deploy agents",
		"Deploy hooks",
		"Promote to generic",
		"Transform harness",
	}
	for _, name := range knownModeNames {
		if strings.Contains(view, name) {
			t.Errorf("review header for an unmapped mode shows real mode name %q; "+
				"an unmapped mode must not masquerade as a known operation:\n%s",
				name, s.View())
		}
	}
}
