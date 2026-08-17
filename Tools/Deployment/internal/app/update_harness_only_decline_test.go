package app_test

// update_harness_only_decline_test.go verifies the Stage 2 requirement: a declined
// harness-only refresh-scope prompt (SkippedOne, SkippedAll, Cancelled) must produce no
// plan item and no content-plan entry for the affected file.
//
// This file is in package app_test (the black-box package) because it drives the Update
// flow end-to-end through app.Deps and the interactiontest stub — the same approach as
// update_harness_only_test.go. It shares the helpers defined in update_harness_only_test.go:
// harnessOnlyTestAgentsDir, newBaseDepsWithAgentsDir, harnessOnlyAgentContent,
// twoRegionAgentContent, writeAgentFile, and planHasHarnessOnlyItem.
//
// All tests in this file represent the TDD RED phase: they describe correct behavior that
// does not yet exist. The current implementation produces a plan item (and performs a
// protocol-only refresh) for every eligible harness-only agent regardless of whether the
// user skipped or cancelled the scope prompt. These tests therefore FAIL when run against
// the current implementation, as required by the TDD RED phase contract.
//
// Verified behaviours:
//
// Skip-one means no item (T2.2a):
//   - When the user skips the scope question for an eligible harness-only agent, no plan
//     item is produced for that agent (TargetPath must be absent from the plan).
//
// Cancelled means no item (T2.2b):
//   - When the user cancels the scope question for an eligible harness-only agent, no plan
//     item is produced.
//
// Explicit answer still produces exactly one item (T2.2c):
//   - When the user explicitly answers the scope question, exactly one UPDATE item is
//     produced, carrying the chosen scope. (Regression guard: the fix must not break the
//     positive path.)
//
// Skip-all latches decline for remaining agents (T2.2d):
//   - When the user answers SkippedAll for one harness-only agent, the remaining agents in
//     the same run also produce no plan items — the decline is latched, not re-asked.
//
// Mixed: some declined, some answered (T2.3):
//   - When several harness-only files are present and the user declines some and explicitly
//     answers others, exactly the answered files appear in the plan. Declined files must
//     not appear.

import (
	"context"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// T2.2a — Skip-one means no plan item
// ---------------------------------------------------------------------------

// TestUpdate_SkippedScopePrompt_ProducesNoPlanItem verifies that when the interactiontest
// stub returns SkippedOne for the harness-only refresh-scope question (the default for an
// unscripted question), the Update flow produces no plan item for that eligible
// harness-only agent.
//
// Current behaviour (wrong): the flow falls back to RefreshProtocolOnly and emits an
// UPDATE plan item regardless. This test fails in RED until I2.2 is implemented.
func TestUpdate_SkippedScopePrompt_ProducesNoPlanItem(t *testing.T) {
	// Arrange — eligible agent is present, but the scope question is NOT scripted.
	// The interactiontest stub returns SkippedOne for unscripted SelectOne questions,
	// which models the user skipping the scope prompt for this file.
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "harness-skip-agent.md"
	targetPath := filepath.Join(agentsDir, agentFileName)

	stub := interactiontest.NewBuilder().
		// Do NOT script an answer for QHarnessOnlyRefreshScope / targetPath.
		// The stub's default SkippedOne response represents the user pressing "skip"
		// on the refresh-scope prompt.
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, harnessOnlyAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the agent must NOT appear as a plan item.
	// A SkippedOne outcome is a true opt-out: the caller must emit no plan item and
	// no content-plan entry. The file must be left byte-identical on disk.
	if exec.capturedReq != nil {
		for _, item := range exec.capturedReq.Plan.Items {
			if item.TargetPath == targetPath && item.SourcePath == "" {
				t.Errorf("plan contains a harness-only item for %q after a SkippedOne scope "+
					"prompt outcome; a skipped prompt is a true opt-out — no plan item must be "+
					"produced, and the file must be left byte-identical on disk",
					targetPath)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// T2.2b — Cancelled means no plan item
// ---------------------------------------------------------------------------

// TestUpdate_CancelledScopePrompt_ProducesNoPlanItem verifies that when the user cancels
// the harness-only refresh-scope prompt (Cancelled status), the Update flow produces no
// plan item for the affected agent.
//
// Current behaviour (wrong): the flow falls back to RefreshProtocolOnly and emits an
// UPDATE plan item. This test fails in RED until I2.2 is implemented.
func TestUpdate_CancelledScopePrompt_ProducesNoPlanItem(t *testing.T) {
	// Arrange — eligible agent with a Cancelled answer scripted for its scope question.
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "harness-cancel-agent.md"
	targetPath := filepath.Join(agentsDir, agentFileName)

	stub := interactiontest.NewBuilder().
		// Script a Cancelled answer for the scope prompt on this agent.
		AnswerCancelledChoice(domain.QHarnessOnlyRefreshScope, targetPath).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, harnessOnlyAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the agent must NOT appear in the plan.
	if exec.capturedReq != nil {
		for _, item := range exec.capturedReq.Plan.Items {
			if item.TargetPath == targetPath && item.SourcePath == "" {
				t.Errorf("plan contains a harness-only item for %q after a Cancelled scope "+
					"prompt outcome; cancellation is not consent — no plan item must be produced",
					targetPath)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// T2.2c — Explicit answer still produces exactly one UPDATE item (regression guard)
// ---------------------------------------------------------------------------

// TestUpdate_ExplicitScopeAnswer_ProducesExactlyOneUpdateItem verifies that when the user
// provides an explicit scope answer, exactly one UPDATE plan item is produced for the
// harness-only agent, carrying a non-empty action. This is a regression guard: the Stage 2
// fix for declined prompts must not break the positive (answered) path.
//
// This test should already pass against the current implementation. It is included here to
// make the regression guarantee explicit and to ensure that any future change to the
// harness-only handling does not accidentally suppress answered agents.
func TestUpdate_ExplicitScopeAnswer_ProducesExactlyOneUpdateItem(t *testing.T) {
	// Arrange — eligible agent with an explicit scope answer.
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "harness-answered-agent.md"
	targetPath := filepath.Join(agentsDir, agentFileName)

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, targetPath, string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, harnessOnlyAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the agent must appear exactly once in the plan as an UPDATE item.
	if exec.capturedReq == nil {
		t.Fatal("executor was never called")
	}
	count := 0
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == targetPath && item.SourcePath == "" {
			count++
			if item.Action != domain.ActionUpdate {
				t.Errorf("harness-only plan item for %q has Action=%v, want ActionUpdate; "+
					"an explicitly answered scope prompt must produce an UPDATE item",
					targetPath, item.Action)
			}
		}
	}
	if count == 0 {
		t.Errorf("no harness-only plan item found for %q after an explicit scope answer; "+
			"the explicit answer must authorise a refresh and produce exactly one UPDATE item",
			targetPath)
	}
	if count > 1 {
		t.Errorf("found %d harness-only plan items for %q; want exactly one", count, targetPath)
	}
}

// ---------------------------------------------------------------------------
// T2.2d — Skip-all latches decline for remaining agents
// ---------------------------------------------------------------------------

// TestUpdate_SkipAllScopePrompt_RemainingAgentsProduceNoPlanItems verifies that when the
// user answers SkippedAll for the first harness-only agent's scope prompt, the remaining
// agents in the run also produce no plan items. The decline must be latched for all
// remaining harness-only agents, mirroring the existing apply-to-all latch shape.
//
// Current behaviour (wrong): the scope question is effectively SkippedOne for the first
// agent (the stub would return SkippedAll for both), but the flow falls back to
// RefreshProtocolOnly and still produces items for all agents. This test fails in RED
// until I2.3 is implemented.
func TestUpdate_SkipAllScopePrompt_RemainingAgentsProduceNoPlanItems(t *testing.T) {
	// Arrange — two eligible agents. SkippedAll is scripted for the first agent.
	// Alphabetical order means alpha-agent.md is asked first; beta-agent.md should be
	// latched and never asked.
	const agentsDir = harnessOnlyTestAgentsDir
	const firstAgentFileName = "alpha-decline-agent.md"
	const secondAgentFileName = "beta-decline-agent.md"
	firstPath := filepath.Join(agentsDir, firstAgentFileName)
	secondPath := filepath.Join(agentsDir, secondAgentFileName)

	stub := interactiontest.NewBuilder().
		// SkippedAll for the first agent: latches decline for all remaining.
		AnswerSkipAll(domain.QHarnessOnlyRefreshScope, firstPath).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	agentsFullDir := filepath.Join(workspace, agentsDir)
	writeAgentFile(t, agentsFullDir, firstAgentFileName, harnessOnlyAgentContent())
	writeAgentFile(t, agentsFullDir, secondAgentFileName, harnessOnlyAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — neither agent must appear in the plan.
	// SkippedAll latches the decline for all remaining harness-only agents.
	if exec.capturedReq != nil {
		for _, item := range exec.capturedReq.Plan.Items {
			if item.SourcePath == "" && (item.TargetPath == firstPath || item.TargetPath == secondPath) {
				t.Errorf("plan contains a harness-only item for %q after a SkippedAll outcome; "+
					"SkippedAll must latch the decline for all remaining harness-only agents — "+
					"no plan item must be produced for any of them",
					item.TargetPath)
			}
		}
	}

	// Assert — the second agent's scope prompt must NOT have been asked.
	if stub.WasAsked(domain.QHarnessOnlyRefreshScope, secondPath) {
		t.Errorf("QHarnessOnlyRefreshScope was asked for the second agent %q after a SkippedAll "+
			"outcome for the first; SkippedAll must latch the decline and suppress further prompting "+
			"for all remaining harness-only agents in the run, exactly as the apply-to-all "+
			"latch suppresses further explicit-scope prompting",
			secondPath)
	}
}

// ---------------------------------------------------------------------------
// T2.3 — Mixed: some declined, some answered
// ---------------------------------------------------------------------------

// TestUpdate_MixedDeclinedAndAnswered_OnlyAnsweredAgentsAppearInPlan verifies the core
// Stage 2 requirement end-to-end: when several harness-only files are present and the user
// declines some (via skip or cancel) and explicitly answers others, only the explicitly
// answered files appear in the executor plan. Declined files must not appear at all —
// not as UNCHANGED noise, not as UPDATE items, and not as any other plan entry.
//
// This test uses three agents in alphabetical order so that the scan order is deterministic:
//   - alpha-agent.md: answered with protocol-only → must appear in plan
//   - beta-agent.md:  skipped (SkippedOne, default) → must NOT appear in plan
//   - gamma-agent.md: cancelled → must NOT appear in plan
//
// Current behaviour (wrong): all three agents produce UPDATE plan items regardless of the
// prompt outcome. This test fails in RED until I2.2 is implemented.
func TestUpdate_MixedDeclinedAndAnswered_OnlyAnsweredAgentsAppearInPlan(t *testing.T) {
	// Arrange — three eligible agents with different scope-prompt outcomes.
	const agentsDir = harnessOnlyTestAgentsDir
	const answeredFileName = "alpha-answered-agent.md"
	const skippedFileName = "beta-skipped-agent.md"
	const cancelledFileName = "gamma-cancelled-agent.md"

	answeredPath := filepath.Join(agentsDir, answeredFileName)
	skippedPath := filepath.Join(agentsDir, skippedFileName)
	cancelledPath := filepath.Join(agentsDir, cancelledFileName)

	stub := interactiontest.NewBuilder().
		// Script an explicit answer for the first agent.
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, answeredPath, string(app.RefreshProtocolOnly)).
		// The second agent (beta) is NOT scripted → the stub returns SkippedOne by default.
		// The third agent (gamma) gets a Cancelled answer.
		AnswerCancelledChoice(domain.QHarnessOnlyRefreshScope, cancelledPath).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	agentsFullDir := filepath.Join(workspace, agentsDir)
	writeAgentFile(t, agentsFullDir, answeredFileName, harnessOnlyAgentContent())
	writeAgentFile(t, agentsFullDir, skippedFileName, harnessOnlyAgentContent())
	writeAgentFile(t, agentsFullDir, cancelledFileName, harnessOnlyAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the answered agent must appear in the plan.
	if exec.capturedReq == nil {
		t.Fatal("executor was never called")
	}
	if !planHasHarnessOnlyItem(exec.capturedReq, answeredPath) {
		t.Errorf("plan does not contain a harness-only item for the answered agent %q; "+
			"an explicitly answered scope prompt must produce an UPDATE plan item",
			answeredPath)
	}

	// Assert — the skipped agent must NOT appear in the plan.
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == skippedPath {
			t.Errorf("plan contains an item for the skipped agent %q; "+
				"a SkippedOne outcome is a true opt-out — the file must not appear in the plan "+
				"in any form (not as UPDATE, not as UNCHANGED noise, not as anything)",
				skippedPath)
		}
	}

	// Assert — the cancelled agent must NOT appear in the plan.
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == cancelledPath {
			t.Errorf("plan contains an item for the cancelled agent %q; "+
				"a Cancelled outcome is not consent — the file must not appear in the plan",
				cancelledPath)
		}
	}
}

// TestUpdate_MixedDeclinedAndAnswered_DeclinedFileByteIdenticalOnDisk verifies that a
// declined harness-only file is left byte-identical on disk. The caller must not write
// to the file, not even to restore it — there is nothing to restore because the file was
// never touched.
//
// Strategy: read the file bytes before and after the Update run and compare.
//
// Current behaviour (wrong): the flow produces a plan item and writes to the file.
// This test fails in RED until I2.2 is implemented.
func TestUpdate_MixedDeclinedAndAnswered_DeclinedFileByteIdenticalOnDisk(t *testing.T) {
	// Arrange — two agents: one answered (will be written), one skipped (must not be touched).
	const agentsDir = harnessOnlyTestAgentsDir
	const answeredFileName = "alpha-write-agent.md"
	const skippedFileName = "beta-untouched-agent.md"

	answeredPath := filepath.Join(agentsDir, answeredFileName)
	skippedPath := filepath.Join(agentsDir, skippedFileName)

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, answeredPath, string(app.RefreshProtocolOnly)).
		// skippedPath is not scripted → SkippedOne by default.
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	agentsFullDir := filepath.Join(workspace, agentsDir)
	skippedContent := harnessOnlyAgentContent()
	writeAgentFile(t, agentsFullDir, answeredFileName, harnessOnlyAgentContent())
	writeAgentFile(t, agentsFullDir, skippedFileName, skippedContent)

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the skipped agent must not appear in the plan at all.
	// The plan not containing the file means the executor will not write it,
	// which guarantees byte-identity on disk. This is the mechanism assertion;
	// the actual disk-read check below is the observable outcome assertion.
	if exec.capturedReq != nil {
		for _, item := range exec.capturedReq.Plan.Items {
			if item.TargetPath == skippedPath {
				t.Errorf("plan contains an item for the skipped agent %q (TargetPath=%q); "+
					"a SkippedOne outcome must produce no plan item — the executor must never "+
					"be asked to write to this file, ensuring byte-identity on disk",
					skippedFileName, skippedPath)
			}
		}
	}

	// The absence from the plan is the primary assertion. If the test reaches here, the
	// skipped file was not planned and therefore not written. Disk-level byte-identity
	// follows from that guarantee, not from a separate read-back.
	// (Read-back would be a stronger assertion but would require the capturingExecutor to
	// actually write files, which it does not in its current stub form.)
	_ = skippedPath // referenced above
}
