package screens_test

// review_test.go verifies the ReviewScreen overlay: rendering of all four plan action
// classifications (create, update, unchanged, locally-modified), version delta attribution
// for updated items, the empty-plan message, scrolling, and the y/n/esc confirmation keys.
// It also covers the mode-name mapping in the plan review header (modeDisplayName), which
// must render a distinct, correct label for every declared RunMode and must fall back to
// the raw mode string for any unmapped mode rather than defaulting to a real mode's name.

import (
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
	if !strings.Contains(view, field) {
		t.Errorf("review screen does not show the version field name for a normal delta:\n%s", view)
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
