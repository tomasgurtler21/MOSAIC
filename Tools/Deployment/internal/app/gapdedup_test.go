package app_test

// gapdedup_test.go verifies the gap-deduplication contract for the deploy-new flow.
//
// GapUnmappedTool deduplication (Stage 2):
//
// Two independent code paths currently emit domain.GapUnmappedTool for the same tool
// during a deploy-new run:
//
//   - Path A: resolveCustomTools() emits a gap for every tool the user skips.
//   - Path B: plan.Build() emits a gap (attributed to the owning agent) which deploy.go
//     then copies into the collector from plan.Gaps.
//
// Only Path B must survive; Path A must be removed. These tests specify the correct
// post-removal behaviour and will FAIL against the current implementation where noted.
//
// Verified invariants (GapUnmappedTool):
//
//   - A deploy-new run in which the user skips the custom-name prompt for one unmapped
//     tool produces exactly one GapUnmappedTool gap for that tool in the collector.
//   - The retained gap is the agent-attributed one from plan computation (Path B), whose
//     Detail contains the agent key.
//   - Other gap kinds collected in the same run are unaffected in count and content: a
//     GapNoModel gap added by plan.Build remains at count 1 and is not duplicated.
//   - The skipped tools map is still populated and passed to module.Tools on the
//     re-resolution pass after Path A is removed (risk-mitigation guard).
//
// GapNoModel deduplication (Stage 4):
//
// Two independent code paths currently emit domain.GapNoModel for the same agent during
// a deploy-new run in which the agent has no resolvable model:
//
//   - Path A: resolveModels() emits a gap directly for every agent left unresolved.
//   - Path B: plan.Build() emits a gap into Plan.Gaps, and deploy.go copies every plan
//     gap into the collector via the existing for _, g := range p.Gaps { s.deps.Todo.AddGap(g) }
//     loop.
//
// Only Path B must survive; Path A must be removed. Path B is the canonical source because
// plan.Gaps is also what the Review screen renders, ensuring the warning is visible to users.
// Removing Path A cannot lose any user-visible warning.
//
// Verified invariants (GapNoModel):
//
//   - A deploy-new run in which one agent has no resolvable model produces exactly one
//     GapNoModel gap for that agent in the collector.
//   - The retained gap is the plan-computation one (Path B), identifiable by the detail
//     string set in the plan stub.
//   - Other gap kinds collected in the same run are unaffected in count and content.
//   - Tier-mapping resolution and per-agent resolution still populate plan.Input.Models
//     after the gap-emission block is removed (risk-mitigation guard).

import (
	"context"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Shared scenario helper for the T2.1 pair
// ---------------------------------------------------------------------------

// runSkippedBashFlow sets up a deploy-new run where "bash" is an unmapped tool and the
// user skips the custom-name prompt (unscripted QCustomTool → SkippedOne), then executes
// the run and returns all gaps collected by the spy todo.
//
// Both T2.1 tests use the same scenario; this helper eliminates the duplicated Arrange
// and Act blocks so that future changes to the shared setup are single-site.
func runSkippedBashFlow(t *testing.T) []domain.Gap {
	t.Helper()

	mod := &unmappedToolModule{
		base:          newMinimalModule(),
		unmappedTools: []string{"bash"},
	}
	reg := newMinimalRegistry(nil)
	reg.modules = map[string]domain.HarnessModule{"stub-harness": mod}

	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build() // QCustomTool is unscripted → SkippedOne → Path A fires

	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)

	// Plan stub carries the agent-attributed Path-B gap that plan.Build would produce.
	planWithGap := newMinimalPlan(workspace)
	planWithGap.Gaps = []domain.Gap{
		{
			Kind:    domain.GapUnmappedTool,
			Subject: "bash",
			Detail:  `generic tool "bash" has no harness mapping for agent "test-runner"`,
		},
	}

	deps.Registry = reg
	deps.Planner = &stubPlanner{plan: planWithGap}
	deps.Todo = spy
	svc := app.New(deps)

	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	return spy.gaps
}

// ---------------------------------------------------------------------------
// T2.1: exactly one GapUnmappedTool per tool, and it is agent-attributed
// ---------------------------------------------------------------------------

// TestDeployNew_SkippedCustomTool_ExactlyOneGapUnmappedTool verifies that when the user
// skips the custom-name prompt for an unmapped tool, the collector receives exactly one
// GapUnmappedTool entry for that tool.
//
// This test will FAIL against the current implementation because resolveCustomTools()
// emits a second gap (Path A) on top of the one already present in plan.Gaps (Path B).
func TestDeployNew_SkippedCustomTool_ExactlyOneGapUnmappedTool(t *testing.T) {
	gaps := runSkippedBashFlow(t)

	// Assert — exactly one GapUnmappedTool for "bash", not two.
	bashGapCount := 0
	for _, g := range gaps {
		if g.Kind == domain.GapUnmappedTool && g.Subject == "bash" {
			bashGapCount++
		}
	}
	if bashGapCount != 1 {
		t.Errorf("collected %d GapUnmappedTool gaps for \"bash\", want exactly 1; "+
			"resolveCustomTools must not emit a second gap when plan.Build has already added one via plan.Gaps",
			bashGapCount)
	}
}

// TestDeployNew_SkippedCustomTool_RetainedGapIsAgentAttributed verifies that the single
// GapUnmappedTool gap kept in the collector names the owning agent in its Detail, proving
// that the retained entry is the plan-computation gap (Path B) and not the simpler
// resolveCustomTools gap (Path A) whose Detail does not mention the agent.
//
// This test will FAIL against the current implementation because two gaps are present;
// the Fatalf on count > 1 fires before the Detail check can be reached.
func TestDeployNew_SkippedCustomTool_RetainedGapIsAgentAttributed(t *testing.T) {
	gaps := runSkippedBashFlow(t)

	// Assert — gather all GapUnmappedTool gaps for "bash".
	var bashGaps []domain.Gap
	for _, g := range gaps {
		if g.Kind == domain.GapUnmappedTool && g.Subject == "bash" {
			bashGaps = append(bashGaps, g)
		}
	}
	if len(bashGaps) != 1 {
		// Fail-fast: detail assertion is only meaningful when there is exactly one gap.
		t.Fatalf("collected %d GapUnmappedTool gaps for \"bash\", want exactly 1 "+
			"(prerequisite for the agent-attribution detail check)", len(bashGaps))
	}

	// The retained gap must be the agent-attributed one whose Detail mentions the agent key.
	const agentKey = "test-runner"
	if !strings.Contains(bashGaps[0].Detail, agentKey) {
		t.Errorf("retained GapUnmappedTool Detail = %q, want it to contain the agent key %q; "+
			"the gap produced by plan.Build names the owning agent and must be the sole retained entry",
			bashGaps[0].Detail, agentKey)
	}
}

// ---------------------------------------------------------------------------
// T2.2: other gap kinds are unaffected in count and content
// ---------------------------------------------------------------------------

// TestDeployNew_SkippedCustomTool_OtherGapKindsUnaffected verifies that in a deploy-new
// run that collects both an unmapped-tool gap and a no-model gap, the no-model gap appears
// exactly once and is not duplicated by the deduplication fix.
//
// Setup: models are pre-answered for all agents so resolveModels emits zero GapNoModel
// gaps directly. The plan stub carries exactly one GapNoModel gap and one GapUnmappedTool
// gap (simulating what plan.Build would produce). Then Path A fires via unscripted
// QCustomTool, adding a second GapUnmappedTool.
//
// This test will FAIL against the current implementation because the Path-A
// GapUnmappedTool brings the total to 3 gaps instead of the expected 2, and the
// GapUnmappedTool count assertion (exactly 1) fails.
func TestDeployNew_SkippedCustomTool_OtherGapKindsUnaffected(t *testing.T) {
	// Arrange — harness reports "bash" as unmapped; plan stub carries both a
	// Path-B GapUnmappedTool gap and a GapNoModel gap.
	mod := &unmappedToolModule{
		base:          newMinimalModule(),
		unmappedTools: []string{"bash"},
	}
	reg := newMinimalRegistry(nil)
	reg.modules = map[string]domain.HarnessModule{"stub-harness": mod}

	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build() // QCustomTool unscripted → SkippedOne → Path A fires, adding a second GapUnmappedTool

	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)

	planWithGaps := newMinimalPlan(workspace)
	planWithGaps.Gaps = []domain.Gap{
		{
			Kind:    domain.GapUnmappedTool,
			Subject: "bash",
			Detail:  `generic tool "bash" has no harness mapping for agent "test-runner"`,
		},
		{
			Kind:    domain.GapNoModel,
			Subject: "test-runner",
			Detail:  "no model has been selected for agent test-runner",
		},
	}

	deps.Registry = reg
	deps.Planner = &stubPlanner{plan: planWithGaps}
	deps.Todo = spy
	svc := app.New(deps)

	// Pre-answer all agent models so resolveModels() itself adds zero GapNoModel gaps.
	// This isolates the GapNoModel count to the one carried in plan.Gaps (simulating
	// Path B), making the assertion deterministic regardless of catalog tier layout.
	// plan.ResolveArtifacts always includes the orchestrator plus every workflow agent,
	// so we cover both "orchestrator" and "test-runner".
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
		// Pre-answer the tier so test-runner gets model-a; pre-answer the orchestrator
		// directly because it declares no RecommendedTier.
		TierModels:  map[domain.Tier]string{"HIGH": "model-a"},
		AgentModels: map[string]string{"orchestrator": "model-a"},
	})

	// Count gaps by kind.
	unmappedToolCount := 0
	noModelCount := 0
	for _, g := range spy.gaps {
		switch g.Kind {
		case domain.GapUnmappedTool:
			unmappedToolCount++
		case domain.GapNoModel:
			noModelCount++
		}
	}

	// The GapNoModel gap from plan.Gaps must appear exactly once — it must not be
	// duplicated by any side-effect of removing Path A from resolveCustomTools.
	if noModelCount != 1 {
		t.Errorf("collected %d GapNoModel gaps, want exactly 1; "+
			"the deduplication fix must not affect other gap kinds", noModelCount)
	}

	// The GapUnmappedTool gap must also appear exactly once (the agent-attributed one).
	// Currently this fails because resolveCustomTools adds a second one.
	if unmappedToolCount != 1 {
		t.Errorf("collected %d GapUnmappedTool gaps for \"bash\", want exactly 1; "+
			"resolveCustomTools must not emit a second gap when plan.Build has already added one via plan.Gaps",
			unmappedToolCount)
	}
}

// ---------------------------------------------------------------------------
// AC2.3 risk-mitigation: skipped tools still reach re-resolution after Path A removal
// ---------------------------------------------------------------------------

// TestDeployNew_SkippedCustomTool_SkippedToolReachesReResolution verifies that after the
// user skips the custom-name prompt for "bash", the skipped tools map is still populated
// and passed through to module.Tools on the re-resolution pass.
//
// This is a risk-mitigation guard for the implementation of I2.1: removing the
// gap-emitting loop from resolveCustomTools must not also remove the skipped map
// population or its downstream use. If an implementer over-deletes and stops passing
// SkippedTools to module.Tools, content generation for skipped tools would break silently
// while the gap-count tests would still pass.
//
// The test uses capturingToolsModule (defined in customtool_test.go) to record every
// Tools call, then asserts that at least one call carries SkippedTools["bash"] == true,
// confirming the skipped map survives into the re-resolution step.
//
// Currently passes; will FAIL if the implementation removes the skipped-map propagation.
func TestDeployNew_SkippedCustomTool_SkippedToolReachesReResolution(t *testing.T) {
	// Arrange — capturing module records every Tools call so we can inspect SkippedTools.
	var capturedReqs []domain.ToolRequest
	capMod := &capturingToolsModule{
		base: &unmappedToolModule{
			base:          newMinimalModule(),
			unmappedTools: []string{"bash"},
		},
		onTools: func(req domain.ToolRequest) {
			capturedReqs = append(capturedReqs, req)
		},
	}
	reg := newMinimalRegistry(nil)
	reg.modules = map[string]domain.HarnessModule{"stub-harness": capMod}

	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build() // QCustomTool is unscripted → SkippedOne → "bash" enters the skipped map

	deps, workspace := newBaseDeps(t, stub)
	deps.Registry = reg
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — at least one Tools call must carry SkippedTools["bash"] == true.
	// This confirms the skipped map is still populated and forwarded to the harness
	// on the re-resolution pass, so content generation for skipped tools is not broken.
	found := false
	for _, req := range capturedReqs {
		if req.SkippedTools != nil && req.SkippedTools["bash"] {
			found = true
			break
		}
	}
	if !found {
		t.Error("no HarnessModule.Tools call received SkippedTools[\"bash\"] == true; " +
			"the skipped map must still be populated and passed to module.Tools on the " +
			"re-resolution pass after Path A's gap-emitting loop is removed — " +
			"otherwise skipped tools will be missing from generated content")
	}
}

// ===========================================================================
// Stage 4: GapNoModel deduplication
// ===========================================================================

// ---------------------------------------------------------------------------
// Shared scenario helper for the T4.1 pair
// ---------------------------------------------------------------------------

// runUnresolvedAgentFlow sets up a deploy-new run where "test-runner" has no resolvable
// model (no tier mapping, QTierModel and QAgentModel are unscripted → SkippedOne by
// default), then executes the run and returns all gaps collected by the spy todo.
//
// The plan stub carries exactly one GapNoModel for "test-runner" with a distinctive detail
// string, simulating the plan-computation gap that plan.Build would produce (Path B). Both
// T4.1 tests use this helper so that future changes to the shared setup are single-site.
func runUnresolvedAgentFlow(t *testing.T) []domain.Gap {
	t.Helper()

	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build() // QTierModel and QAgentModel are unscripted → SkippedOne → Path A fires

	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)

	// Plan stub carries the plan-computation GapNoModel that plan.Build would produce for
	// an agent with no resolved model. The detail is distinctive so it can be compared
	// against the different string emitted by resolveModels (Path A).
	planWithGap := newMinimalPlan(workspace)
	planWithGap.Gaps = []domain.Gap{
		{
			Kind:    domain.GapNoModel,
			Subject: "test-runner",
			Detail:  `no model has been selected for agent "test-runner"`,
		},
	}
	deps.Planner = &stubPlanner{plan: planWithGap}
	deps.Todo = spy
	svc := app.New(deps)

	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
		// No TierModels, no AgentModels → test-runner is left unresolved → Path A fires
	})

	return spy.gaps
}

// ---------------------------------------------------------------------------
// T4.1: exactly one GapNoModel per unresolved agent
// ---------------------------------------------------------------------------

// TestDeployNew_UnresolvedAgent_ExactlyOneGapNoModel verifies that when an agent has no
// resolvable model, the collector receives exactly one GapNoModel gap for that agent.
//
// This test will FAIL against the current implementation because resolveModels adds a
// second GapNoModel gap (Path A) on top of the one already present in plan.Gaps (Path B),
// yielding two gaps for "test-runner" instead of the specified one.
func TestDeployNew_UnresolvedAgent_ExactlyOneGapNoModel(t *testing.T) {
	gaps := runUnresolvedAgentFlow(t)

	// Assert — exactly one GapNoModel for "test-runner", not two.
	count := 0
	for _, g := range gaps {
		if g.Kind == domain.GapNoModel && g.Subject == "test-runner" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("collected %d GapNoModel gaps for \"test-runner\", want exactly 1; "+
			"resolveModels must not emit a second gap when plan.Build has already added one via plan.Gaps",
			count)
	}
}

// TestDeployNew_UnresolvedAgent_RetainedGapIsPlanComputation verifies that the single
// GapNoModel gap kept in the collector is the one produced by plan computation (Path B),
// not the simpler one emitted by resolveModels (Path A).
//
// The plan-computation gap's Detail is set in the plan stub (distinctive string). The
// resolveModels gap carries Detail == "no model selected for agent test-runner". After the
// fix, only the plan gap must be present, and its Detail must match the plan stub's value.
//
// This test will FAIL against the current implementation because two gaps are present;
// the Fatalf on count != 1 fires before the Detail check can be reached.
func TestDeployNew_UnresolvedAgent_RetainedGapIsPlanComputation(t *testing.T) {
	gaps := runUnresolvedAgentFlow(t)

	// Gather all GapNoModel gaps for "test-runner".
	var testRunnerGaps []domain.Gap
	for _, g := range gaps {
		if g.Kind == domain.GapNoModel && g.Subject == "test-runner" {
			testRunnerGaps = append(testRunnerGaps, g)
		}
	}
	if len(testRunnerGaps) != 1 {
		// Fail-fast: the Detail check is only meaningful when there is exactly one gap.
		t.Fatalf("collected %d GapNoModel gaps for \"test-runner\", want exactly 1 "+
			"(prerequisite for the plan-computation detail check)", len(testRunnerGaps))
	}

	// The retained gap must be the plan-computation one whose Detail matches the stub value.
	// resolveModels (Path A) emits Detail = "no model selected for agent test-runner" (no quotes
	// around the key). The plan stub carries a different string, making the two distinguishable.
	const planDetail = `no model has been selected for agent "test-runner"`
	if testRunnerGaps[0].Detail != planDetail {
		t.Errorf("retained GapNoModel Detail = %q, want %q; "+
			"the gap produced by plan.Build must be the sole retained entry — "+
			"resolveModels must no longer add its own gap to the collector",
			testRunnerGaps[0].Detail, planDetail)
	}
}

// ---------------------------------------------------------------------------
// T4.2: surviving gap is from plan; other gap kinds are unaffected in count and content
// ---------------------------------------------------------------------------

// TestDeployNew_UnresolvedAgent_OtherGapKindsUnaffected verifies that in a deploy-new run
// that collects both a no-model gap and an unmapped-tool gap, both appear exactly once and
// neither is duplicated by the no-model deduplication fix.
//
// Setup: test-runner has no resolvable model (no tier mapping, QAgentModel unscripted →
// SkippedOne), so resolveModels emits a GapNoModel via Path A in the current
// implementation. The plan stub carries exactly one GapNoModel and one GapUnmappedTool
// for test-runner (simulating plan.Build under Path B). The unmapped-tool dedup (Stage 2)
// is already in place, so GapUnmappedTool is sourced only from plan.Gaps.
//
// This test will FAIL against the current implementation because the Path-A GapNoModel
// brings the total no-model count for "test-runner" to 2 instead of the specified 1.
func TestDeployNew_UnresolvedAgent_OtherGapKindsUnaffected(t *testing.T) {
	// Arrange — plan stub carries both gap kinds; test-runner is unresolved so Path A fires.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build() // QTierModel and QAgentModel unscripted → SkippedOne → Path A fires

	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)

	planWithGaps := newMinimalPlan(workspace)
	planWithGaps.Gaps = []domain.Gap{
		{
			Kind:    domain.GapNoModel,
			Subject: "test-runner",
			Detail:  `no model has been selected for agent "test-runner"`,
		},
		{
			Kind:    domain.GapUnmappedTool,
			Subject: "bash",
			Detail:  `generic tool "bash" has no harness mapping for agent "test-runner"`,
		},
	}
	deps.Planner = &stubPlanner{plan: planWithGaps}
	deps.Todo = spy
	svc := app.New(deps)

	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
		// No TierModels, no AgentModels → test-runner is unresolved → Path A fires
	})

	// Count gaps by kind and subject.
	noModelCount := 0
	for _, g := range spy.gaps {
		if g.Kind == domain.GapNoModel && g.Subject == "test-runner" {
			noModelCount++
		}
	}
	unmappedToolCount := 0
	for _, g := range spy.gaps {
		if g.Kind == domain.GapUnmappedTool && g.Subject == "bash" {
			unmappedToolCount++
		}
	}

	// The GapNoModel gap from plan.Gaps must appear exactly once.
	// Currently fails because resolveModels adds a second one via Path A.
	if noModelCount != 1 {
		t.Errorf("collected %d GapNoModel gaps for \"test-runner\", want exactly 1; "+
			"resolveModels must not emit a second gap when plan.Build has already added one via plan.Gaps",
			noModelCount)
	}

	// The GapUnmappedTool gap must appear exactly once.
	// The unmapped-tool dedup (Stage 2) already guarantees this; this assertion confirms
	// the no-model dedup fix does not disturb other gap kinds in count or content.
	if unmappedToolCount != 1 {
		t.Errorf("collected %d GapUnmappedTool gaps for \"bash\", want exactly 1; "+
			"the no-model deduplication fix must not affect other gap kinds",
			unmappedToolCount)
	}
}

// ---------------------------------------------------------------------------
// T4.3 risk-mitigation: model resolution still works after Path A removal
// ---------------------------------------------------------------------------

// capturingPlannerT4 records the plan.Input passed to Build so tests can inspect the
// resolved models map without exercising real plan logic.
type capturingPlannerT4 struct {
	captured  plan.Input
	fixedPlan domain.Plan
}

func (p *capturingPlannerT4) Build(ctx context.Context, in plan.Input) (domain.Plan, error) {
	p.captured = in
	return p.fixedPlan, nil
}

// TestDeployNew_TierModel_PreAnswered_PopulatesModelSelection verifies that a pre-answered
// tier model is carried through to plan.Input.Models as a resolved ModelSelection for every
// agent on that tier.
//
// This is a risk-mitigation guard for I4.1: removing the gap-emission block from
// resolveModels must not remove the tier-mapping lookup or the models-map population that
// precedes it. If an implementer over-deletes and the tier resolution path stops writing to
// models, plan.Input.Models would be empty and content generation would silently receive
// an unresolved model while the gap-count tests would still pass (both paths would be gone).
//
// Currently passes; will FAIL if implementation of I4.1 removes tier-resolution logic.
func TestDeployNew_TierModel_PreAnswered_PopulatesModelSelection(t *testing.T) {
	// Arrange — pre-answer tier "HIGH" so test-runner (RecommendedTier:"HIGH") resolves
	// without an interactive QAgentModel question. Pre-answer orchestrator directly because
	// it declares no RecommendedTier and would otherwise go through QAgentModel unscripted.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()

	cap := &capturingPlannerT4{}
	deps, workspace := newBaseDeps(t, stub)
	cap.fixedPlan = newMinimalPlan(workspace)
	deps.Planner = cap
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
		TierModels:      map[domain.Tier]string{"HIGH": "model-a"},
		AgentModels:     map[string]string{"orchestrator": "model-a"},
	})

	// Assert — test-runner must appear in plan.Input.Models with a resolved selection whose
	// ModelID and Origin match what the tier mapping produced.
	sel, ok := cap.captured.Models["test-runner"]
	if !ok {
		t.Fatal("plan.Input.Models does not contain an entry for \"test-runner\"; " +
			"tier-mapping resolution in resolveModels must still populate the models map " +
			"after the gap-emission block is removed (I4.1 over-deletion guard)")
	}
	if !sel.Resolved() {
		t.Errorf("plan.Input.Models[\"test-runner\"].Resolved() == false (Origin=%q, ModelID=%q); "+
			"a pre-answered tier mapping must yield a resolved ModelSelection",
			sel.Origin, sel.ModelID)
	}
	if sel.ModelID != "model-a" {
		t.Errorf("plan.Input.Models[\"test-runner\"].ModelID = %q, want \"model-a\"; "+
			"the tier-mapped model ID must be forwarded unchanged into plan.Input",
			sel.ModelID)
	}
	if sel.Origin != domain.OriginTierMapping {
		t.Errorf("plan.Input.Models[\"test-runner\"].Origin = %q, want %q; "+
			"a model resolved through tier mapping must carry OriginTierMapping",
			sel.Origin, domain.OriginTierMapping)
	}
}

// TestDeployNew_AgentModel_Answered_PopulatesModelSelection verifies that a per-agent
// interactive model answer (QAgentModel answered with an explicit choice) is carried through
// to plan.Input.Models as a resolved ModelSelection for that agent.
//
// This is a risk-mitigation guard for I4.1: removing the gap-emission block from
// resolveModels must not remove the per-agent answer recording. If an implementer
// over-deletes and the answer-handling branch stops writing to models, the models map
// would be empty for that agent and content generation would silently receive an unresolved
// model while the gap-count tests would still pass.
//
// Currently passes; will FAIL if implementation of I4.1 removes per-agent resolution logic.
func TestDeployNew_AgentModel_Answered_PopulatesModelSelection(t *testing.T) {
	// Arrange — no tier mappings so test-runner goes through QAgentModel. Script that
	// question to return "model-b". Pre-answer orchestrator via AgentModels because it
	// declares no RecommendedTier and would otherwise receive an unscripted SkippedOne,
	// keeping the assertion focused on test-runner alone.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "test-runner", "model-b").
		AnswerReview(true).
		Build()

	cap := &capturingPlannerT4{}
	deps, workspace := newBaseDeps(t, stub)
	cap.fixedPlan = newMinimalPlan(workspace)
	deps.Planner = cap
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
		AgentModels:     map[string]string{"orchestrator": "model-a"},
		// No TierModels → test-runner falls through to QAgentModel, answered above
	})

	// Assert — test-runner must appear in plan.Input.Models with the interactively answered
	// model ID and a resolved origin.
	sel, ok := cap.captured.Models["test-runner"]
	if !ok {
		t.Fatal("plan.Input.Models does not contain an entry for \"test-runner\"; " +
			"per-agent answer recording in resolveModels must still populate the models map " +
			"after the gap-emission block is removed (I4.1 over-deletion guard)")
	}
	if !sel.Resolved() {
		t.Errorf("plan.Input.Models[\"test-runner\"].Resolved() == false (Origin=%q, ModelID=%q); "+
			"answering QAgentModel with an explicit choice must yield a resolved ModelSelection",
			sel.Origin, sel.ModelID)
	}
	if sel.ModelID != "model-b" {
		t.Errorf("plan.Input.Models[\"test-runner\"].ModelID = %q, want \"model-b\"; "+
			"the QAgentModel answer must be forwarded unchanged into plan.Input",
			sel.ModelID)
	}
}
