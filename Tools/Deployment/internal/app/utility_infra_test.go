package app_test

// utility_infra_test.go verifies the DeployUtilityInfrastructure service method.
//
// All tests in this file are RED-phase TDD tests. They assert the full intended behaviour
// of the DeployUtilityInfrastructure method, which does not yet exist (I7.1 and I7.2 are
// not delivered). Every test that calls svc.DeployUtilityInfrastructure will receive a
// "not yet implemented" error until the implementation is delivered.
//
// Verified behaviours:
//
// Question asking (T7.1) — what the mode asks and does not ask:
//   - When UtilityAgentIDs is nil, QUtilityAgents is asked through Interaction.
//   - When UtilityAgentIDs is non-nil, QUtilityAgents is not asked (pre-answer convention).
//   - When InfrastructureAgentIDs is nil, QInfrastructureAgents is asked.
//   - When InfrastructureAgentIDs is non-nil, QInfrastructureAgents is not asked.
//   - QWorkflows is never asked, regardless of which other fields are pre-answered.
//   - QHooks is never asked, regardless of which other fields are pre-answered.
//
// Planned artifact set (T7.2) — what is planned and what is not:
//   - The planner receives Mode = ModeUtilityInfraOnly, not ModeDeployNew.
//   - The planner receives no WorkflowIDs (empty or nil).
//   - The planner receives no HookIDs (empty or nil).
//   - The planner receives the pre-answered UtilityAgentIDs.
//   - The planner receives the pre-answered InfrastructureAgentIDs.
//   - The RunSummary carries Mode = ModeUtilityInfraOnly.
//
// Infrastructure model resolution (T7.3) — same behaviour as DeployNew's infra batch:
//   - QAgentModel is asked for selected infrastructure agents.
//   - Skipping QAgentModel for an infra agent records a GapNoModel gap.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Fixtures for utility/infra tests
// ---------------------------------------------------------------------------

// minimalUtilityAgent is a stub utility agent used in utility/infra test scenarios.
var minimalUtilityAgent = domain.Agent{
	Key:             "file-manager",
	NumericID:       "20",
	Version:         "1.0",
	Name:            "File Manager",
	Description:     "Manages file operations",
	Role:            domain.RoleWorker,
	RecommendedTier: "LOW",
}

// minimalInfraAgentForUtilityTest is a stub infrastructure agent used specifically in
// utility/infra test scenarios. Differs from the one in infraagent_test.go only in its
// name, to keep the two sets of tests independent.
var minimalInfraAgentForUtilityTest = domain.Agent{
	Key:             "safety-guard",
	NumericID:       "30",
	Version:         "1.0",
	Name:            "Safety Guard",
	Description:     "Guards against unsafe operations",
	Role:            domain.RoleWorker,
	Infrastructure:  "safety",
	RecommendedTier: "HIGH",
}

// catalogWithUtilityAndInfraAgents returns a catalog with one utility agent and one
// infrastructure agent, in addition to the standard minimal catalog contents.
func catalogWithUtilityAndInfraAgents() *stubCatalog {
	cat := newMinimalCatalog()
	cat.utilityAgents = []domain.Agent{minimalUtilityAgent}
	cat.infraAgents = []domain.Agent{minimalInfraAgentForUtilityTest}
	return cat
}

// utilityInfraPlan returns a domain.Plan shaped as expected from DeployUtilityInfrastructure:
// ModeUtilityInfraOnly, no orchestrator, no workflow agents, no hooks.
func utilityInfraPlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeUtilityInfraOnly,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "file-manager"},
				TargetPath: "file-manager.md",
				Action:     domain.ActionCreate,
			},
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "safety-guard"},
				TargetPath: "safety-guard.md",
				Action:     domain.ActionCreate,
			},
		},
	}
}

// newUtilityInfraRequest returns a minimal UtilityInfraRequest for headless testing.
// UtilityAgentIDs and InfrastructureAgentIDs are set to empty non-nil slices ("none, don't ask").
func newUtilityInfraRequest(workspace, harnessID string) app.UtilityInfraRequest {
	return app.UtilityInfraRequest{
		HarnessID:              harnessID,
		WorkspacePath:          workspace,
		Scope:                  domain.ScopeProject,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		AutoConfirmPlan:        true,
	}
}

// ---------------------------------------------------------------------------
// dryRunRecordingExecutor — purpose-built executor for DryRun assertion
// ---------------------------------------------------------------------------

// dryRunRecordingExecutor satisfies deploy.Executor and records whether it was called
// and whether the ExecRequest carried DryRun=true. It is used solely to assert the
// DryRun field is threaded through to the executor in DeployUtilityInfrastructure.
type dryRunRecordingExecutor struct {
	called         bool
	capturedDryRun bool
	result         deploy.ExecResult
}

func (e *dryRunRecordingExecutor) Execute(_ context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	e.called = true
	e.capturedDryRun = req.DryRun
	return e.result, nil
}

// ---------------------------------------------------------------------------
// T7.1 — QUtilityAgents is asked when UtilityAgentIDs is nil
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_UtilityAgentIDsNil_QUtilityAgentsAsked verifies that when
// UtilityAgentIDs is nil, the flow asks QUtilityAgents through the Interaction port so the
// user can select which utility agents to deploy (CD-6 pre-answer convention).
func TestDeployUtilityInfrastructure_UtilityAgentIDsNil_QUtilityAgentsAsked(t *testing.T) {
	// Arrange — UtilityAgentIDs is nil, so QUtilityAgents must be asked.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QUtilityAgents, "", []string{}).
		AnswerSelectMany(domain.QInfrastructureAgents, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	deps.Planner = &stubPlanner{plan: utilityInfraPlan(workspace)}
	svc := app.New(deps)

	// Act — UtilityAgentIDs is nil: the flow must ask QUtilityAgents.
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        nil, // nil → must ask
		InfrastructureAgentIDs: []string{},
		AutoConfirmPlan:        true,
	})

	// Assert — QUtilityAgents must be asked when UtilityAgentIDs is nil.
	if !stub.WasAsked(domain.QUtilityAgents, "") {
		t.Error("QUtilityAgents was not asked even though UtilityAgentIDs was nil; " +
			"DeployUtilityInfrastructure must ask QUtilityAgents through the Interaction port " +
			"when the field is not pre-answered (CD-6 pre-answer convention)")
	}
}

// ---------------------------------------------------------------------------
// T7.1 — QUtilityAgents is NOT asked when UtilityAgentIDs is pre-answered
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_UtilityAgentIDsPreAnswered_QUtilityAgentsNotAsked verifies
// that when UtilityAgentIDs is non-nil, the flow does not ask QUtilityAgents (CD-6).
func TestDeployUtilityInfrastructure_UtilityAgentIDsPreAnswered_QUtilityAgentsNotAsked(t *testing.T) {
	// Arrange — UtilityAgentIDs is non-nil (pre-answered).
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	deps.Planner = &stubPlanner{plan: utilityInfraPlan(workspace)}
	svc := app.New(deps)

	// Act — pre-answered UtilityAgentIDs: QUtilityAgents must not be asked.
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{"file-manager"}, // non-nil: must not ask
		InfrastructureAgentIDs: []string{},
		AutoConfirmPlan:        true,
	})

	// Assert — QUtilityAgents must not be asked when UtilityAgentIDs is pre-answered.
	if stub.WasAsked(domain.QUtilityAgents, "") {
		t.Error("QUtilityAgents was asked even though UtilityAgentIDs was pre-answered (non-nil); " +
			"DeployUtilityInfrastructure must use the pre-answered selection directly " +
			"without asking (CD-6 pre-answer convention)")
	}
}

// ---------------------------------------------------------------------------
// T7.1 — QInfrastructureAgents is asked when InfrastructureAgentIDs is nil
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_InfraAgentIDsNil_QInfrastructureAgentsAsked verifies that
// when InfrastructureAgentIDs is nil, the flow asks QInfrastructureAgents through the
// Interaction port (CD-6 pre-answer convention).
func TestDeployUtilityInfrastructure_InfraAgentIDsNil_QInfrastructureAgentsAsked(t *testing.T) {
	// Arrange — InfrastructureAgentIDs is nil, so QInfrastructureAgents must be asked.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QUtilityAgents, "", []string{}).
		AnswerSelectMany(domain.QInfrastructureAgents, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	deps.Planner = &stubPlanner{plan: utilityInfraPlan(workspace)}
	svc := app.New(deps)

	// Act — InfrastructureAgentIDs is nil: the flow must ask QInfrastructureAgents.
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: nil, // nil → must ask
		AutoConfirmPlan:        true,
	})

	// Assert — QInfrastructureAgents must be asked when InfrastructureAgentIDs is nil.
	if !stub.WasAsked(domain.QInfrastructureAgents, "") {
		t.Error("QInfrastructureAgents was not asked even though InfrastructureAgentIDs was nil; " +
			"DeployUtilityInfrastructure must ask QInfrastructureAgents through the Interaction port " +
			"when the field is not pre-answered (CD-6 pre-answer convention)")
	}
}

// ---------------------------------------------------------------------------
// T7.1 — QInfrastructureAgents is NOT asked when InfrastructureAgentIDs is pre-answered
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_InfraAgentIDsPreAnswered_QInfrastructureAgentsNotAsked
// verifies that when InfrastructureAgentIDs is non-nil, the flow does not ask
// QInfrastructureAgents (CD-6 pre-answer convention).
func TestDeployUtilityInfrastructure_InfraAgentIDsPreAnswered_QInfrastructureAgentsNotAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	deps.Planner = &stubPlanner{plan: utilityInfraPlan(workspace)}
	svc := app.New(deps)

	// Act — pre-answered InfrastructureAgentIDs.
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{"safety-guard"}, // non-nil: must not ask
		AutoConfirmPlan:        true,
	})

	// Assert — QInfrastructureAgents must not be asked when pre-answered.
	if stub.WasAsked(domain.QInfrastructureAgents, "") {
		t.Error("QInfrastructureAgents was asked even though InfrastructureAgentIDs was pre-answered (non-nil); " +
			"DeployUtilityInfrastructure must use the pre-answered selection directly " +
			"without asking (CD-6 pre-answer convention)")
	}
}

// ---------------------------------------------------------------------------
// T7.1 — QWorkflows is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_QWorkflowsNeverAsked verifies that QWorkflows is never
// asked by DeployUtilityInfrastructure, regardless of request field state. Workflows are
// outside the scope of this mode.
func TestDeployUtilityInfrastructure_QWorkflowsNeverAsked(t *testing.T) {
	// Arrange — both selections nil so all questions would be triggered if present.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QUtilityAgents, "", []string{}).
		AnswerSelectMany(domain.QInfrastructureAgents, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	deps.Planner = &stubPlanner{plan: utilityInfraPlan(workspace)}
	svc := app.New(deps)

	// Act — nil selections so the flow asks everything it needs; QWorkflows must not be one of them.
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        nil,
		InfrastructureAgentIDs: nil,
		AutoConfirmPlan:        true,
	})

	// Assert — QWorkflows must never appear in the question sequence for this mode.
	if stub.WasAsked(domain.QWorkflows, "") {
		t.Error("QWorkflows was asked; DeployUtilityInfrastructure must never ask about workflows — " +
			"this mode deploys only Utility and Infrastructure agents and has no workflow concept; " +
			"passing a non-nil empty WorkflowIDs to the planner or excluding the workflow question from " +
			"the resolver sequence are both acceptable implementations")
	}
}

// ---------------------------------------------------------------------------
// T7.1 — QHooks is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_QHooksNeverAsked verifies that QHooks is never asked by
// DeployUtilityInfrastructure. Hook configuration is outside the scope of this mode.
func TestDeployUtilityInfrastructure_QHooksNeverAsked(t *testing.T) {
	// Arrange — both selections nil so the flow asks everything relevant.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QUtilityAgents, "", []string{}).
		AnswerSelectMany(domain.QInfrastructureAgents, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	deps.Planner = &stubPlanner{plan: utilityInfraPlan(workspace)}
	svc := app.New(deps)

	// Act — nil selections; hook question must still be absent.
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        nil,
		InfrastructureAgentIDs: nil,
		AutoConfirmPlan:        true,
	})

	// Assert — QHooks must never appear in the question sequence for this mode.
	if stub.WasAsked(domain.QHooks, "") {
		t.Error("QHooks was asked; DeployUtilityInfrastructure must never ask about hooks — " +
			"hook configuration is outside the scope of this mode; " +
			"the resolver sequence must not include the hooks resolver")
	}
}

// ---------------------------------------------------------------------------
// T7.2 — Planner receives Mode = ModeUtilityInfraOnly
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_PlannerInput_ModeIsUtilityInfraOnly verifies that the
// planner receives Mode = ModeUtilityInfraOnly in its Input, so the planner produces a plan
// scoped to utility and infrastructure agents — not a full deploy-new plan.
func TestDeployUtilityInfrastructure_PlannerInput_ModeIsUtilityInfraOnly(t *testing.T) {
	// Arrange — capturing planner records the Input it receives.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	capPlan := &capturingPlanner{result: utilityInfraPlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), newUtilityInfraRequest(workspace, "stub-harness"))

	// Assert — planner must have been called.
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployUtilityInfrastructure must call the planner " +
			"to compute a deployment plan")
	}
	if capPlan.capturedInput.Mode != domain.ModeUtilityInfraOnly {
		t.Errorf("Planner.Build received Mode = %q, want %q; "+
			"DeployUtilityInfrastructure must pass ModeUtilityInfraOnly to the planner "+
			"so the planner knows to scope the artifact set to utility and infrastructure agents only",
			capPlan.capturedInput.Mode, domain.ModeUtilityInfraOnly)
	}
}

// ---------------------------------------------------------------------------
// T7.2 — Planner receives no WorkflowIDs
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_PlannerInput_WorkflowIDsIsEmpty verifies that the planner
// receives an empty or nil WorkflowIDs slice. This ensures no workflow-driven agent or
// orchestrator rewrite is planned.
func TestDeployUtilityInfrastructure_PlannerInput_WorkflowIDsIsEmpty(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	capPlan := &capturingPlanner{result: utilityInfraPlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), newUtilityInfraRequest(workspace, "stub-harness"))

	// Assert
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployUtilityInfrastructure must call the planner")
	}
	if len(capPlan.capturedInput.WorkflowIDs) > 0 {
		t.Errorf("Planner.Build received WorkflowIDs = %v; want empty; "+
			"DeployUtilityInfrastructure must pass no workflow IDs to the planner — "+
			"workflows are outside the scope of this mode and must not cause workflow agents or "+
			"orchestrator rewrites to be planned",
			capPlan.capturedInput.WorkflowIDs)
	}
}

// ---------------------------------------------------------------------------
// T7.2 — Planner receives no HookIDs
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_PlannerInput_HookIDsIsEmpty verifies that the planner
// receives an empty or nil HookIDs slice. Hook artifacts must never be planned in this mode.
func TestDeployUtilityInfrastructure_PlannerInput_HookIDsIsEmpty(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	capPlan := &capturingPlanner{result: utilityInfraPlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), newUtilityInfraRequest(workspace, "stub-harness"))

	// Assert
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployUtilityInfrastructure must call the planner")
	}
	if len(capPlan.capturedInput.HookIDs) > 0 {
		t.Errorf("Planner.Build received HookIDs = %v; want empty; "+
			"DeployUtilityInfrastructure must not pass any hook IDs to the planner — "+
			"hooks are outside the scope of this mode",
			capPlan.capturedInput.HookIDs)
	}
}

// ---------------------------------------------------------------------------
// T7.2 — Planner receives the pre-answered UtilityAgentIDs
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_PlannerInput_IncludesPreAnsweredUtilityAgents verifies
// that when UtilityAgentIDs is pre-answered, the planner receives those IDs. The planner
// uses them to determine which utility agents and their required skills to include in the plan.
func TestDeployUtilityInfrastructure_PlannerInput_IncludesPreAnsweredUtilityAgents(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	capPlan := &capturingPlanner{result: utilityInfraPlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act — pre-answer utility agent selection.
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{"file-manager"}, // pre-answered
		InfrastructureAgentIDs: []string{},
		AutoConfirmPlan:        true,
	})

	// Assert — planner must have received "file-manager" in its UtilityAgentIDs.
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployUtilityInfrastructure must call the planner")
	}
	found := false
	for _, id := range capPlan.capturedInput.UtilityAgentIDs {
		if id == "file-manager" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Planner.Build UtilityAgentIDs = %v; want to contain \"file-manager\"; "+
			"the pre-answered UtilityAgentIDs must be threaded through to the planner input",
			capPlan.capturedInput.UtilityAgentIDs)
	}
}

// ---------------------------------------------------------------------------
// T7.2 — Planner receives the pre-answered InfrastructureAgentIDs
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_PlannerInput_IncludesPreAnsweredInfraAgents verifies that
// when InfrastructureAgentIDs is pre-answered, the planner receives those IDs.
func TestDeployUtilityInfrastructure_PlannerInput_IncludesPreAnsweredInfraAgents(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	capPlan := &capturingPlanner{result: utilityInfraPlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act — pre-answer infrastructure agent selection.
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{"safety-guard"}, // pre-answered
		AutoConfirmPlan:        true,
	})

	// Assert — planner must have received "safety-guard" in its InfrastructureAgentIDs.
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployUtilityInfrastructure must call the planner")
	}
	found := false
	for _, id := range capPlan.capturedInput.InfrastructureAgentIDs {
		if id == "safety-guard" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Planner.Build InfrastructureAgentIDs = %v; want to contain \"safety-guard\"; "+
			"the pre-answered InfrastructureAgentIDs must be threaded through to the planner input",
			capPlan.capturedInput.InfrastructureAgentIDs)
	}
}

// ---------------------------------------------------------------------------
// T7.2 — RunSummary.Mode is ModeUtilityInfraOnly
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_RunSummaryModeIsUtilityInfraOnly verifies that the
// RunSummary returned by DeployUtilityInfrastructure carries ModeUtilityInfraOnly. This
// distinguishes the mode in run records, the todo file metadata, and the CLI output.
func TestDeployUtilityInfrastructure_RunSummaryModeIsUtilityInfraOnly(t *testing.T) {
	// Arrange — stub planner returns a utility-infra plan with ModeUtilityInfraOnly.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	deps.Planner = &stubPlanner{plan: utilityInfraPlan(workspace)}
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployUtilityInfrastructure(context.Background(), newUtilityInfraRequest(workspace, "stub-harness"))

	// Assert — a nil error means the flow ran; check the mode.
	if err != nil {
		t.Fatalf("DeployUtilityInfrastructure returned error %v; " +
			"the flow must complete successfully when all required fields are pre-answered", err)
	}
	if summary.Mode != domain.ModeUtilityInfraOnly {
		t.Errorf("RunSummary.Mode = %q, want %q; "+
			"DeployUtilityInfrastructure must report ModeUtilityInfraOnly in the summary "+
			"so the mode is distinguishable from DeployNew in run records",
			summary.Mode, domain.ModeUtilityInfraOnly)
	}
}

// ---------------------------------------------------------------------------
// T7.2 — DryRun field is honoured
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_DryRun_ExecutorCalledWithDryRunTrue verifies that when
// DryRun is true, the executor receives an ExecRequest with DryRun=true so that no files
// are written. This matches the DryRun contract established by DeployNew, where the executor
// is always called (using simulateAction per plan item) and DryRun=true suppresses all writes.
func TestDeployUtilityInfrastructure_DryRun_ExecutorCalledWithDryRunTrue(t *testing.T) {
	// Arrange — use a purpose-built executor that records DryRun.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	deps.Planner = &stubPlanner{plan: utilityInfraPlan(workspace)}
	dryExec := &dryRunRecordingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = dryExec
	svc := app.New(deps)

	// Act — DryRun = true.
	_, err := svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		DryRun:                 true,
		AutoConfirmPlan:        true,
	})

	// Assert — dry-run must succeed without aborting.
	if err != nil {
		t.Fatalf("DeployUtilityInfrastructure DryRun=true returned error %v; "+
			"dry-run must compute the plan and return a summary without writing files", err)
	}
	// Assert — the executor must be called (DryRun uses simulateAction, not a no-op).
	if !dryExec.called {
		t.Fatal("executor was not called; DeployUtilityInfrastructure must call the executor even " +
			"for DryRun=true so the plan is simulated and a meaningful summary is returned — " +
			"the executor's DryRun flag suppresses writes, not the call itself")
	}
	// Assert — the executor must receive DryRun=true so it skips all file writes.
	if !dryExec.capturedDryRun {
		t.Error("executor was called but ExecRequest.DryRun was false; " +
			"DeployUtilityInfrastructure must thread DryRun=true from its request into ExecRequest.DryRun — " +
			"without this the executor will write files even when the caller requested a dry-run; " +
			"this matches the same DryRun threading contract as DeployNew")
	}
}

// ---------------------------------------------------------------------------
// T7.3 — QAgentModel is asked for infrastructure agents
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_InfraAgent_QAgentModelAsked verifies that when
// infrastructure agents are selected, QAgentModel is asked for each of them. Infrastructure
// model resolution must behave as it does in DeployNew: the same two-batch pattern, with
// infra agents in the second batch.
func TestDeployUtilityInfrastructure_InfraAgent_QAgentModelAsked(t *testing.T) {
	// Arrange — one infra agent selected; tier question skipped so QAgentModel is reached.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "safety-guard", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	deps.Planner = &stubPlanner{plan: utilityInfraPlan(workspace)}
	svc := app.New(deps)

	// Act — pre-answer utility agents; infra agent selected to trigger model resolution.
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{"safety-guard"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert — QAgentModel must have been asked for the infrastructure agent.
	if !stub.WasAsked(domain.QAgentModel, "safety-guard") {
		t.Error("QAgentModel was not asked for \"safety-guard\"; " +
			"infrastructure agents selected in DeployUtilityInfrastructure must participate " +
			"in model resolution, exactly as they do in DeployNew — " +
			"the second resolveModels call (infra batch) must exist in this flow too")
	}
}

// ---------------------------------------------------------------------------
// T7.3 — Skipped infra model produces GapNoModel
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_InfraAgent_SkippedModel_RecordsGapNoModel verifies that
// when the user skips or does not answer QAgentModel for an infrastructure agent, a GapNoModel
// gap is recorded, matching the behaviour established by DeployNew. The run must still complete.
func TestDeployUtilityInfrastructure_InfraAgent_SkippedModel_RecordsGapNoModel(t *testing.T) {
	// Arrange — skip the infra agent model question; the todo collector must record GapNoModel.
	spy := &spyTodo{}
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	deps.Planner = &stubPlanner{plan: utilityInfraPlan(workspace)}
	deps.Todo = spy
	svc := app.New(deps)

	// Act — SkipAll[QAgentModel] suppresses the question; skip answer produces GapNoModel.
	_, err := svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{"safety-guard"},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})

	// Assert — run must complete despite the skipped model.
	if err != nil {
		t.Fatalf("DeployUtilityInfrastructure returned error %v; "+
			"a skipped or unanswered model question must not abort the run — "+
			"the run must complete and record a GapNoModel gap, matching DeployNew behaviour", err)
	}
	// Assert — a GapNoModel gap must be recorded for the infra agent.
	if !spy.hasGapKind(domain.GapNoModel) {
		t.Error("no GapNoModel gap was recorded after skipping QAgentModel for \"safety-guard\"; " +
			"DeployUtilityInfrastructure must forward GapNoModel gaps to the todo collector " +
			"when an infrastructure agent's model question is skipped, " +
			"matching the same-skip-path contract as DeployNew")
	}
}

// ---------------------------------------------------------------------------
// T7.3 — EmptyNonNil InfrastructureAgentIDs → no infra model questions asked
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_NoInfraAgents_NoQAgentModelForInfra verifies that when
// InfrastructureAgentIDs is an empty non-nil slice (no agents selected), no QAgentModel is
// asked for the infra batch. The model-resolution gate (non-empty agent set) must apply.
func TestDeployUtilityInfrastructure_NoInfraAgents_NoQAgentModelForInfra(t *testing.T) {
	// Arrange — empty infra selection.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	deps.Planner = &stubPlanner{plan: utilityInfraPlan(workspace)}
	svc := app.New(deps)

	// Act — no infra agents selected.
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{}, // explicitly empty — none selected
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert — no infra-batch model question must be presented when no infra agents are selected.
	if stub.WasAsked(domain.QAgentModel, "safety-guard") {
		t.Error("QAgentModel was asked for \"safety-guard\" even though InfrastructureAgentIDs was empty; " +
			"when no infrastructure agents are selected, the infra model-resolution batch is empty " +
			"and no QAgentModel must be asked for any infrastructure agent")
	}
}

// ---------------------------------------------------------------------------
// T7.2 (real-planner) — Planned artifact set excludes workflow-driven agents
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_RealPlanner_PlanExcludesWorkflowDrivenAgents verifies
// that, with the real planner wired in, DeployUtilityInfrastructure produces a plan that
// contains the selected utility and infrastructure agents and does NOT contain any
// workflow-driven agent. This is the direct regression test for Plan.md's Risk row:
// "Reusing the deploy-new flow silently pulls in workflow-driven agents."
//
// Using a stub planner (as the other T7.2 tests do) only asserts which values are passed
// INTO Build; this test asserts what comes OUT of the real planner so that an implementation
// that accidentally passes non-empty WorkflowIDs to the planner is caught by the output-side
// check — the most valuable property the existing T7.2 tests cannot provide.
func TestDeployUtilityInfrastructure_RealPlanner_PlanExcludesWorkflowDrivenAgents(t *testing.T) {
	// Arrange — real planner; capturing executor records the computed plan without writing.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithUtilityAndInfraAgents()
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec
	svc := app.New(deps)

	// Act — pre-answer all selections and skip model questions so the flow runs headlessly.
	// "file-manager" is the utility agent; "safety-guard" is the infrastructure agent.
	// "test-runner" is a workflow-driven agent referenced by minimalWorkflow — it must not appear.
	_, err := svc.DeployUtilityInfrastructure(context.Background(), app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		Scope:                  domain.ScopeProject,
		UtilityAgentIDs:        []string{"file-manager"},
		InfrastructureAgentIDs: []string{"safety-guard"},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployUtilityInfrastructure returned error %v; "+
			"with pre-answered selections the flow must complete successfully", err)
	}

	// Assert — executor must have been called (the full planning pipeline must have run).
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; DeployUtilityInfrastructure must invoke the executor " +
			"via the planner — this test uses the real planner to exercise the output-side invariant")
	}

	// Assert — plan must include the selected utility agent.
	foundUtility := false
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "file-manager" {
			foundUtility = true
		}
	}
	if !foundUtility {
		t.Error("plan does not contain utility agent \"file-manager\"; " +
			"DeployUtilityInfrastructure must plan the selected utility agents")
	}

	// Assert — plan must include the selected infrastructure agent.
	foundInfra := false
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "safety-guard" {
			foundInfra = true
		}
	}
	if !foundInfra {
		t.Error("plan does not contain infrastructure agent \"safety-guard\"; " +
			"DeployUtilityInfrastructure must plan the selected infrastructure agents")
	}

	// Assert — plan must NOT include workflow-driven agents.
	// "test-runner" is referenced by minimalWorkflow but must be absent because no workflows
	// are selected in this mode. Its presence would prove the implementation passed non-empty
	// WorkflowIDs to the planner — exactly the failure mode Plan.md's Risk row warns against.
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "test-runner" {
			t.Errorf("plan contains workflow-driven agent %q; "+
				"DeployUtilityInfrastructure must not pass any workflow IDs to the planner — "+
				"if WorkflowIDs is non-empty, the planner will include every workflow-referenced "+
				"agent; ensure WorkflowIDs is nil or empty in the planner input for this mode",
				item.Ref.Key)
		}
	}
}
