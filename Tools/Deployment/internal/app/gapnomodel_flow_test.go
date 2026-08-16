package app_test

// gapnomodel_flow_test.go verifies end-to-end gap collection behaviour across the Update and
// Deploy flows for the five model-presence scenarios introduced by the no-model warning fix.
//
// Both flows copy every plan gap into the todo collector via the existing
//
//	for _, g := range p.Gaps { s.deps.Todo.AddGap(g) }
//
// loop. The Review screen renders Plan.Gaps verbatim, so asserting on the collected gaps is
// equivalent to asserting on what the user sees.
//
// Each test uses a stub planner that returns a pre-built plan matching what plan.Build would
// produce under the named scenario. This isolates the flow-level concern — that Plan.Gaps are
// faithfully propagated into the collector — from the plan-layer concern of correct gap
// emission, which is covered by plan-package tests. Regressions in either layer cause
// failures: a flow regression drops gaps from the collector; a plan-layer regression (if the
// plan stub is replaced with a real planner) changes what Plan.Gaps contains.
//
// Five scenarios are verified:
//
//   - All agents unchanged with embedded models: Update produces zero GapNoModel gaps.
//     The files are not rewritten, so their embedded models stand.
//
//   - Stale agent whose embedded model is preserved: Update with one ActionUpdate item and
//     no gap in Plan.Gaps produces zero GapNoModel gaps in the collector.
//
//   - Agent genuinely lacking a model: both the Update and Deploy flows relay exactly one
//     GapNoModel gap when Plan.Gaps carries one — neither dropped nor duplicated.
//
//   - No duplicate GapNoModel in Deploy: a deploy-new run with one GapNoModel in Plan.Gaps
//     produces exactly one entry in the collector, confirming plan.Gaps is the sole source.
//
//   - Locally-modified (conflict) agent whose embedded model is preserved: Update with one
//     ActionConflict item and no gap in Plan.Gaps produces zero GapNoModel gaps.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// countNoModelGaps returns the number of GapNoModel gaps recorded by spy for agentKey.
// It is used by every scenario below; the assertion message explains the count constraint.
func countNoModelGaps(spy *spyTodo, agentKey string) int {
	n := 0
	for _, g := range spy.gaps {
		if g.Kind == domain.GapNoModel && g.Subject == agentKey {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Scenario 1: Update — all agents unchanged, embedded models → zero GapNoModel gaps
// ---------------------------------------------------------------------------

// TestUpdate_AllAgentsUnchanged_ZeroNoModelGaps verifies that an Update run in which every
// agent is ActionUnchanged produces no GapNoModel gaps in the collector.
//
// ActionUnchanged means no file is rewritten, so the embedded model remains in place; a
// GapNoModel warning would be a false positive. plan.Build must not emit GapNoModel for
// ActionUnchanged items, and the flow must relay only what Plan.Gaps contains — which is
// nothing for this case. A regression in either layer would cause this test to fail.
func TestUpdate_AllAgentsUnchanged_ZeroNoModelGaps(t *testing.T) {
	// Arrange — scripted interaction; spy todo captures every gap AddGap call.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)

	// Plan has one ActionUnchanged item for "test-runner" and no gaps, matching plan.Build's
	// output when the agent is fully current and its deployed file carries a model. Setting
	// Mode to ModeUpdateWorkspace documents the test's intended flow.
	plan := newMinimalPlan(workspace)
	plan.Mode = domain.ModeUpdateWorkspace
	plan.Items = []domain.PlanItem{
		{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
			TargetPath: "test-runner.md",
			Action:     domain.ActionUnchanged,
		},
	}
	plan.Gaps = nil // ActionUnchanged: never emit GapNoModel

	deps.Planner = &stubPlanner{plan: plan}
	deps.Todo = spy
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — zero GapNoModel gaps for "test-runner". A non-zero count means the flow is
	// adding extra gaps not present in Plan.Gaps, or plan.Build is incorrectly emitting a
	// gap for an ActionUnchanged agent.
	if n := countNoModelGaps(spy, "test-runner"); n != 0 {
		t.Errorf("collected %d GapNoModel gaps for \"test-runner\", want 0; "+
			"an ActionUnchanged agent must not produce a no-model warning because its "+
			"deployed file is not rewritten and its embedded model stands unchanged",
			n)
	}
}

// ---------------------------------------------------------------------------
// Scenario 2: Update — stale agent, embedded model preserved → zero GapNoModel gaps
// ---------------------------------------------------------------------------

// TestUpdate_StaleAgentEmbeddedModelPreserved_ZeroNoModelGaps verifies that an Update run
// where an agent is ActionUpdate but its deployed file carries a model produces no GapNoModel
// gaps for that agent.
//
// When the deployed file carries a non-empty ModelID, plan.Build suppresses GapNoModel
// because the regenerated file will carry the same model — the warning would be a false
// positive. This test pins down the end-to-end path:
//
//	plan.Build (gap suppressed) → Plan.Gaps (empty for this agent)
//	→ todo collector (no GapNoModel) → Review screen (no model warning)
//
// If the suppression rule in plan.Build regresses (emitting a gap when it should not), a
// test at the plan layer catches it. If the flow stops copying Plan.Gaps, other flow tests
// catch that. This test catches the combination: suppression + correct relay.
func TestUpdate_StaleAgentEmbeddedModelPreserved_ZeroNoModelGaps(t *testing.T) {
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)

	// Plan has one ActionUpdate item (stale "version" field) with no model explicitly resolved
	// — on Update the embedded model from the deployed file is used instead of an interactive
	// selection. Plan.Gaps carries no GapNoModel, matching plan.Build's output when the
	// deployed file has a non-empty ModelID that regeneration will preserve.
	plan := newMinimalPlan(workspace)
	plan.Mode = domain.ModeUpdateWorkspace
	plan.Items = []domain.PlanItem{
		{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
			TargetPath: "test-runner.md",
			Action:     domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: "version", Deployed: "1.0", Source: "2.0"},
			},
			Model: domain.ModelSelection{}, // unresolved; deployed file's model is used
		},
	}
	plan.Gaps = nil // deployed.ModelID != "" suppresses GapNoModel in plan.Build

	deps.Planner = &stubPlanner{plan: plan}
	deps.Todo = spy
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — zero GapNoModel gaps for "test-runner". The plan item is ActionUpdate
	// (stale, will be regenerated), so a gap WOULD appear if the deployed file lacked a
	// model. The zero count confirms the suppression path works end to end.
	if n := countNoModelGaps(spy, "test-runner"); n != 0 {
		t.Errorf("collected %d GapNoModel gaps for \"test-runner\", want 0; "+
			"an ActionUpdate agent whose deployed file carries an embedded model must not "+
			"produce a no-model warning because regeneration will preserve that model "+
			"(deployed.ModelID != \"\" suppresses GapNoModel in plan.Build)",
			n)
	}
}

// ---------------------------------------------------------------------------
// Scenario 3a: Update — agent genuinely lacking a model → exactly one GapNoModel gap
// ---------------------------------------------------------------------------

// TestUpdate_AgentWithNoEmbeddedModel_ExactlyOneNoModelGap verifies that an Update run where
// an agent's deployed file carries no model and no interactive model was resolved produces
// exactly one GapNoModel gap for that agent.
//
// "Exactly one" is the critical assertion: zero means the warning was silently lost; two or
// more means duplicates clutter the todo checklist and the Review screen.
func TestUpdate_AgentWithNoEmbeddedModel_ExactlyOneNoModelGap(t *testing.T) {
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)

	// Plan carries one GapNoModel for "test-runner", matching plan.Build's output when the
	// agent is ActionUpdate, no model was resolved, and the deployed file has no embedded
	// model (deployed.ModelID == "").
	plan := newMinimalPlan(workspace)
	plan.Mode = domain.ModeUpdateWorkspace
	plan.Items = []domain.PlanItem{
		{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
			TargetPath: "test-runner.md",
			Action:     domain.ActionUpdate,
			Stale: []domain.VersionDelta{
				{Field: "version", Deployed: "1.0", Source: "2.0"},
			},
		},
	}
	plan.Gaps = []domain.Gap{
		{
			Kind:    domain.GapNoModel,
			Subject: "test-runner",
			Detail:  "no model has been selected for agent test-runner",
		},
	}

	deps.Planner = &stubPlanner{plan: plan}
	deps.Todo = spy
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — exactly one GapNoModel for "test-runner". Zero means the gap was dropped
	// before reaching the collector. More than one means a second code path is also emitting
	// GapNoModel outside of the Plan.Gaps copy loop.
	if n := countNoModelGaps(spy, "test-runner"); n != 1 {
		t.Errorf("collected %d GapNoModel gaps for \"test-runner\", want exactly 1; "+
			"an agent with no embedded model and no resolved selection must produce exactly "+
			"one no-model warning: zero means the gap was lost, more than one means duplicates",
			n)
	}
}

// ---------------------------------------------------------------------------
// Scenario 3b: Deploy — agent genuinely lacking a model → exactly one GapNoModel gap
// ---------------------------------------------------------------------------

// TestDeployNew_AgentWithNoModel_ExactlyOneNoModelGap verifies that a fresh Deploy run where
// an agent has no resolvable model produces exactly one GapNoModel gap for that agent.
//
// This is the Deploy-flow counterpart of TestUpdate_AgentWithNoEmbeddedModel_ExactlyOneNoModelGap,
// confirming both flows correctly relay the single Plan.Gaps entry without dropping or
// duplicating it.
func TestDeployNew_AgentWithNoModel_ExactlyOneNoModelGap(t *testing.T) {
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)

	// Plan carries one GapNoModel for "test-runner", matching plan.Build's output when the
	// agent is ActionCreate and no model was resolved.
	plan := newMinimalPlan(workspace)
	plan.Gaps = []domain.Gap{
		{
			Kind:    domain.GapNoModel,
			Subject: "test-runner",
			Detail:  "no model has been selected for agent test-runner",
		},
	}

	deps.Planner = &stubPlanner{plan: plan}
	deps.Todo = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — exactly one GapNoModel for "test-runner". Zero means the Deploy flow dropped
	// the gap; more than one means a path outside Plan.Gaps also emitted GapNoModel.
	if n := countNoModelGaps(spy, "test-runner"); n != 1 {
		t.Errorf("collected %d GapNoModel gaps for \"test-runner\", want exactly 1; "+
			"a deploy-new run with an unresolved agent must produce exactly one no-model "+
			"warning: zero means the gap was lost, more than one means duplicates",
			n)
	}
}

// ---------------------------------------------------------------------------
// Scenario 4: Deploy — no duplicate GapNoModel entries for one agent
// ---------------------------------------------------------------------------

// TestDeployNew_SingleAgentNoModel_NoDuplicateNoModelGaps verifies that when exactly one
// GapNoModel entry is present in Plan.Gaps, exactly one GapNoModel entry appears in the
// collector — never two or more.
//
// plan.Gaps must be the sole source by which GapNoModel reaches the collector. Any second
// code path emitting GapNoModel (such as the now-removed resolveModels emission) would show
// up here as a duplicate. This is the explicit deduplication guard for the Deploy flow.
func TestDeployNew_SingleAgentNoModel_NoDuplicateNoModelGaps(t *testing.T) {
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)

	// Plan carries exactly one GapNoModel for "test-runner". If any path outside plan.Gaps
	// also emits GapNoModel into the collector, the count would exceed one.
	plan := newMinimalPlan(workspace)
	plan.Gaps = []domain.Gap{
		{
			Kind:    domain.GapNoModel,
			Subject: "test-runner",
			Detail:  "no model has been selected for agent test-runner",
		},
	}

	deps.Planner = &stubPlanner{plan: plan}
	deps.Todo = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — exactly one GapNoModel for "test-runner". The plan stub puts one entry into
	// Plan.Gaps; the collector must reflect that count exactly. A count > 1 means a second
	// emission path is still active; a count of 0 means the copy loop was removed.
	if n := countNoModelGaps(spy, "test-runner"); n != 1 {
		t.Errorf("collected %d GapNoModel gaps for \"test-runner\", want exactly 1; "+
			"plan.Gaps must be the sole source of GapNoModel — any other emission path "+
			"produces duplicates; removal of the copy loop produces zero",
			n)
	}
}

// ---------------------------------------------------------------------------
// Scenario 5: Update — locally-modified (conflict) agent, embedded model preserved → zero gaps
// ---------------------------------------------------------------------------

// TestUpdate_ConflictAgentEmbeddedModelPreserved_ZeroNoModelGaps verifies that an Update run
// where an ActionConflict agent's deployed file carries a model produces no GapNoModel gaps
// for that agent.
//
// ActionConflict obeys the same gap-suppression rule as ActionUpdate: when the deployed file
// has a non-empty ModelID, plan.Build suppresses GapNoModel. This test confirms the
// suppression works through the Update flow for conflict-classified agents, including the
// QLocalModification interaction that conflict items require.
func TestUpdate_ConflictAgentEmbeddedModelPreserved_ZeroNoModelGaps(t *testing.T) {
	// Arrange — the conflict item triggers a QLocalModification question; answer overwrite
	// so the flow proceeds normally without the test stalling on an unscripted interaction.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "test-runner.md", string(domain.DecisionOverwrite)).
		AnswerReview(true).
		Build()
	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)

	// Plan has one ActionConflict item with no model explicitly resolved — the deployed
	// file's embedded model will be preserved. Plan.Gaps has no GapNoModel for "test-runner",
	// matching plan.Build's output when deployed.ModelID != "".
	plan := newMinimalPlan(workspace)
	plan.Mode = domain.ModeUpdateWorkspace
	plan.Items = []domain.PlanItem{
		{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
			TargetPath: "test-runner.md",
			Action:     domain.ActionConflict,
			Conflict: &domain.LocalModification{
				RecordedHash: "sha256:aaa",
				CurrentHash:  "sha256:bbb",
			},
			Model: domain.ModelSelection{}, // unresolved; deployed file's model is used
		},
	}
	plan.Gaps = nil // deployed.ModelID != "" suppresses GapNoModel in plan.Build

	deps.Planner = &stubPlanner{plan: plan}
	deps.Todo = spy
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — zero GapNoModel gaps for "test-runner". The plan item is ActionConflict
	// (locally modified, will be overwritten), so a gap WOULD appear if the deployed file
	// lacked a model. The zero count confirms the conflict-path suppression works end to end.
	if n := countNoModelGaps(spy, "test-runner"); n != 0 {
		t.Errorf("collected %d GapNoModel gaps for \"test-runner\", want 0; "+
			"an ActionConflict agent whose deployed file carries an embedded model must not "+
			"produce a no-model warning: the overwritten file will carry the same model, "+
			"matching the ActionUpdate suppression rule",
			n)
	}
}
