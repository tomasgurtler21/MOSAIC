package app_test

// deployhooks_test.go verifies the DeployHooks service method.
//
// Verified behaviours:
//
// Question asking discipline — what the mode asks and does not ask:
//   - When HookIDs is nil, QHooks is asked through Interaction.
//   - When HookIDs is non-nil, QHooks is not asked (pre-answer convention).
//   - QWorkflows is never asked, regardless of which other fields are pre-answered.
//   - QUtilityAgents is never asked.
//   - QInfrastructureAgents is never asked.
//   - QStandaloneAgents is never asked.
//   - QDeployAgents is never asked.
//   - QTierModel is never asked (no agent content is generated).
//   - QAgentModel is never asked (no agent content is generated).
//
// Hook plan and execution wiring:
//   - Selected hook bundles reach ExecRequest.Hooks in the executor call.
//   - Registration steps from the hook plan reach ExecRequest.Hooks[*].Registration.
//   - The bundle is forwarded to module.HookPlan, whose per-harness variant selection
//     is the mechanism that resolves the correct variant for the run's harness.
//
// Empty-catalog and SkipAll paths:
//   - When the catalog has no hook bundles, nothing is asked and an empty plan is produced
//     rather than an error.
//   - SkipAll[QHooks] bypasses the QHooks ask and produces an empty selection.
//   - A non-nil empty HookIDs slice means "none selected, do not ask".
//
// Orchestrator absence and nil block maps:
//   - The planner receives Mode = ModeDeployHooks.
//   - No WorkflowIDs are passed to the planner.
//   - No agent IDs of any class are passed to the planner.
//   - With the real planner, the orchestrator is absent from the plan.

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
// Fixtures for deploy-hooks tests
// ---------------------------------------------------------------------------

// minimalHookBundle is a stub hook bundle used in deploy-hooks test scenarios.
var minimalHookBundle = domain.HookBundle{
	Key:         "git-hooks",
	Version:     "1.0",
	Description: "Git lifecycle hooks for automated quality checks",
}

// hookBundleWithRegistration is a hook bundle whose harness variant has a registration step.
// Used to verify that registration steps are threaded through to the executor.
var hookBundleWithRegistration = domain.HookBundle{
	Key:         "git-hooks",
	Version:     "1.0",
	Description: "Git lifecycle hooks with registration requirement",
	Variants: map[string]domain.HookVariant{
		"stub-harness": {
			HarnessID: "stub-harness",
			Supported: true,
			Files:     []domain.HookFile{},
			Registration: []domain.RegistrationStep{
				{
					ID:          "install-pre-commit",
					TargetPath:  ".git/hooks/pre-commit",
					Fragment:    "#!/bin/sh\nexec .mosaic/hooks/pre-commit \"$@\"",
					Performable: true,
					Instruction: "Install the pre-commit hook",
				},
			},
		},
	},
}

// catalogWithHookBundles returns a catalog containing one hook bundle, used to ensure
// QHooks would be triggered if hook selection is nil.
func catalogWithHookBundles() *stubCatalog {
	cat := newMinimalCatalog()
	cat.hooks = []domain.HookBundle{minimalHookBundle}
	return cat
}

// catalogWithHookBundleAndRegistration returns a catalog with a hook bundle that has a
// registration step in its harness variant.
func catalogWithHookBundleAndRegistration() *stubCatalog {
	cat := newMinimalCatalog()
	cat.hooks = []domain.HookBundle{hookBundleWithRegistration}
	return cat
}

// hooksPlan returns a domain.Plan shaped as expected from DeployHooks: ModeDeployHooks,
// no agents, no workflows.
func hooksPlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeDeployHooks,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
	}
}

// newDeployHooksRequest returns a minimal DeployHooksRequest for headless testing.
// HookIDs is set to a non-nil empty slice ("none, don't ask").
func newDeployHooksRequest(workspace, harnessID string) app.DeployHooksRequest {
	return app.DeployHooksRequest{
		HarnessID:       harnessID,
		WorkspacePath:   workspace,
		Scope:           domain.ScopeProject,
		HookIDs:         []string{},
		AutoConfirmPlan: true,
	}
}

// ---------------------------------------------------------------------------
// Test doubles specific to deploy-hooks tests
// ---------------------------------------------------------------------------

// hooksCapturingExecutor records the Hooks slice from the ExecRequest it receives.
// It is used to verify that selected hook bundles are forwarded to the executor.
type hooksCapturingExecutor struct {
	capturedHooks []domain.HookPlan
	result        deploy.ExecResult
	called        bool
}

func (e *hooksCapturingExecutor) Execute(_ context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	e.called = true
	e.capturedHooks = req.Hooks
	return e.result, nil
}

// hookPlanRecordingModule wraps a stubHarnessModule and records the bundles that are passed
// to HookPlan. It returns a configurable HookPlan result so tests can specify registration
// steps and supported state.
type hookPlanRecordingModule struct {
	stubHarnessModule
	capturedBundles []domain.HookBundle
	hookPlanResult  domain.HookPlan
}

func (m *hookPlanRecordingModule) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	m.capturedBundles = append(m.capturedBundles, req.Bundle)
	return m.hookPlanResult, nil
}

// ---------------------------------------------------------------------------
// Question discipline — QHooks is asked when HookIDs is nil
// ---------------------------------------------------------------------------

// TestDeployHooks_HookIDsNil_QHooksAsked verifies that when HookIDs is nil, the flow asks
// QHooks through the Interaction port so the user can select which hook bundles to deploy
// (CD-6 pre-answer convention).
func TestDeployHooks_HookIDsNil_QHooksAsked(t *testing.T) {
	// Arrange — HookIDs is nil, so QHooks must be asked.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QHooks, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act — HookIDs is nil: the flow must ask QHooks.
	_, _ = svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         nil, // nil → must ask
		AutoConfirmPlan: true,
	})

	// Assert — QHooks must be asked when HookIDs is nil.
	if !stub.WasAsked(domain.QHooks, "") {
		t.Error("QHooks was not asked even though HookIDs was nil; " +
			"DeployHooks must ask QHooks through the Interaction port " +
			"when the field is not pre-answered (CD-6 pre-answer convention)")
	}
}

// ---------------------------------------------------------------------------
// Question discipline — QHooks is NOT asked when HookIDs is pre-answered
// ---------------------------------------------------------------------------

// TestDeployHooks_HookIDsPreAnswered_QHooksNotAsked verifies that when HookIDs is non-nil,
// the flow does not ask QHooks (CD-6).
func TestDeployHooks_HookIDsPreAnswered_QHooksNotAsked(t *testing.T) {
	// Arrange — HookIDs is non-nil (pre-answered).
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act — pre-answered HookIDs: QHooks must not be asked.
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         []string{"git-hooks"}, // non-nil: must not ask
		AutoConfirmPlan: true,
	})

	// Assert — the flow must complete without error when HookIDs is pre-answered.
	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when HookIDs is pre-answered", err)
	}
	// Assert — QHooks must not be asked when HookIDs is pre-answered.
	if stub.WasAsked(domain.QHooks, "") {
		t.Error("QHooks was asked even though HookIDs was pre-answered (non-nil); " +
			"DeployHooks must use the pre-answered selection directly " +
			"without asking (CD-6 pre-answer convention)")
	}
}

// ---------------------------------------------------------------------------
// Question discipline — QWorkflows is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployHooks_QWorkflowsNeverAsked verifies that QWorkflows is never asked by
// DeployHooks, regardless of request field state. Workflows are outside the scope of
// this mode; it deploys hook bundles only.
func TestDeployHooks_QWorkflowsNeverAsked(t *testing.T) {
	// Arrange — HookIDs is nil so all questions that the flow would trigger are exercised.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QHooks, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         nil,
		AutoConfirmPlan: true,
	})

	// Assert — the flow must complete without error when QHooks is scripted.
	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when all required questions are scripted", err)
	}
	// Assert — QWorkflows must never appear in the question sequence for this mode.
	if stub.WasAsked(domain.QWorkflows, "") {
		t.Error("QWorkflows was asked; DeployHooks must never ask about workflows — " +
			"this mode deploys hook bundles only; the resolver sequence must not include " +
			"any workflow resolver")
	}
}

// ---------------------------------------------------------------------------
// Question discipline — QUtilityAgents is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployHooks_QUtilityAgentsNeverAsked verifies that QUtilityAgents is never asked by
// DeployHooks. Utility agent selection is outside the scope of this mode.
func TestDeployHooks_QUtilityAgentsNeverAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QHooks, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         nil,
		AutoConfirmPlan: true,
	})

	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when all required questions are scripted", err)
	}
	if stub.WasAsked(domain.QUtilityAgents, "") {
		t.Error("QUtilityAgents was asked; DeployHooks must never ask about utility agents — " +
			"this mode deploys hook bundles only and never involves agent selection")
	}
}

// ---------------------------------------------------------------------------
// Question discipline — QInfrastructureAgents is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployHooks_QInfrastructureAgentsNeverAsked verifies that QInfrastructureAgents is
// never asked by DeployHooks. Infrastructure agent selection is outside the scope of
// this mode.
func TestDeployHooks_QInfrastructureAgentsNeverAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QHooks, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         nil,
		AutoConfirmPlan: true,
	})

	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when all required questions are scripted", err)
	}
	if stub.WasAsked(domain.QInfrastructureAgents, "") {
		t.Error("QInfrastructureAgents was asked; DeployHooks must never ask about " +
			"infrastructure agents — this mode deploys hook bundles only")
	}
}

// ---------------------------------------------------------------------------
// Question discipline — QStandaloneAgents is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployHooks_QStandaloneAgentsNeverAsked verifies that QStandaloneAgents is never asked
// by DeployHooks. Standalone agent selection is outside the scope of this mode.
func TestDeployHooks_QStandaloneAgentsNeverAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QHooks, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         nil,
		AutoConfirmPlan: true,
	})

	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when all required questions are scripted", err)
	}
	if stub.WasAsked(domain.QStandaloneAgents, "") {
		t.Error("QStandaloneAgents was asked; DeployHooks must never ask about " +
			"standalone agents — this mode deploys hook bundles only")
	}
}

// ---------------------------------------------------------------------------
// Question discipline — QDeployAgents is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployHooks_QDeployAgentsNeverAsked verifies that QDeployAgents (the merged agent
// selection) is never asked by DeployHooks. This mode generates no agent content.
func TestDeployHooks_QDeployAgentsNeverAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QHooks, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         nil,
		AutoConfirmPlan: true,
	})

	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when all required questions are scripted", err)
	}
	if stub.WasAsked(domain.QDeployAgents, "") {
		t.Error("QDeployAgents was asked; DeployHooks must never ask for agent selection — " +
			"this mode generates no agent content; the merged agent selection question " +
			"belongs to ModeDeployAgents only")
	}
}

// ---------------------------------------------------------------------------
// Question discipline — QTierModel is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployHooks_QTierModelNeverAsked verifies that QTierModel is never asked by
// DeployHooks. This mode generates no agent content and therefore needs no model selection.
// The structural guarantee is that DeployHooksRequest has no TierModels field.
func TestDeployHooks_QTierModelNeverAsked(t *testing.T) {
	// Arrange — catalog with hooks so the question sequence is exercised fully.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QHooks, "", []string{"git-hooks"}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         nil,
		AutoConfirmPlan: true,
	})

	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when all required questions are scripted", err)
	}
	if stub.WasAsked(domain.QTierModel, "") {
		t.Error("QTierModel was asked; DeployHooks must never ask for a tier model — " +
			"this mode generates no agent content and no model resolution is needed; " +
			"the absence of TierModels in DeployHooksRequest is the structural guarantee")
	}
}

// ---------------------------------------------------------------------------
// Question discipline — QAgentModel is NEVER asked
// ---------------------------------------------------------------------------

// TestDeployHooks_QAgentModelNeverAsked verifies that QAgentModel is never asked by
// DeployHooks, for the same reason as QTierModel: no agent content is generated.
func TestDeployHooks_QAgentModelNeverAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QHooks, "", []string{"git-hooks"}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         nil,
		AutoConfirmPlan: true,
	})

	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when all required questions are scripted", err)
	}
	if stub.WasAsked(domain.QAgentModel, "") {
		t.Error("QAgentModel was asked; DeployHooks must never ask for a per-agent model — " +
			"this mode generates no agent content and involves no agent selection of any kind")
	}
}

// ---------------------------------------------------------------------------
// Hook plan and execution wiring — selected bundle reaches ExecRequest.Hooks
// ---------------------------------------------------------------------------

// TestDeployHooks_SelectedBundle_ReachesExecRequestHooks verifies that when a hook bundle is
// selected (via pre-answered HookIDs), it reaches ExecRequest.Hooks in the executor call.
// This is the primary wiring test: the service must call buildHookPlans with the selected
// bundle and forward the result to the executor.
func TestDeployHooks_SelectedBundle_ReachesExecRequestHooks(t *testing.T) {
	// Arrange — catalog with one hook bundle; capture what the executor receives.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	capExec := &hooksCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act — pre-answer the hook selection with one bundle.
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		Scope:           domain.ScopeProject,
		HookIDs:         []string{"git-hooks"},
		AutoConfirmPlan: true,
	})

	// Assert — the flow must complete without error when HookIDs is pre-answered.
	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when HookIDs is pre-answered", err)
	}
	// Assert — the executor must have been called.
	if !capExec.called {
		t.Fatal("executor was not called; DeployHooks must call the executor to apply the plan — " +
			"a hooks-only run still goes through the full plan-review-execute pipeline")
	}
	// Assert — ExecRequest.Hooks must contain one entry for the selected bundle.
	if len(capExec.capturedHooks) == 0 {
		t.Error("ExecRequest.Hooks is empty even though one hook bundle was selected; " +
			"DeployHooks must call buildHookPlans with the selected bundles and forward " +
			"the resulting HookPlans to ExecRequest.Hooks so the executor can apply " +
			"registration steps and deploy hook files")
	}
}

// ---------------------------------------------------------------------------
// Hook plan and execution wiring — registration steps reach the executor
// ---------------------------------------------------------------------------

// TestDeployHooks_RegistrationSteps_ReachExecRequestHooks verifies that when the harness
// module returns a HookPlan with registration steps for a selected bundle, those steps are
// present in the ExecRequest.Hooks entry forwarded to the executor. This tests the full
// wiring path: bundle → buildHookPlans → module.HookPlan → ExecRequest.Hooks.
func TestDeployHooks_RegistrationSteps_ReachExecRequestHooks(t *testing.T) {
	// Arrange — use a module that returns a HookPlan with registration steps.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundleAndRegistration()

	// Configure a module that returns a supported HookPlan with a registration step.
	hookMod := &hookPlanRecordingModule{
		stubHarnessModule: *newMinimalModule(),
		hookPlanResult: domain.HookPlan{
			Supported: true,
			Registration: []domain.RegistrationStep{
				{
					ID:          "install-pre-commit",
					TargetPath:  ".git/hooks/pre-commit",
					Performable: true,
					Instruction: "Install the pre-commit hook",
				},
			},
		},
	}
	deps.Registry = &stubRegistry{
		list:    []domain.HarnessRef{minimalHarness},
		modules: map[string]domain.HarnessModule{"stub-harness": hookMod},
	}
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	capExec := &hooksCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act — pre-answer with the bundle that has registration steps.
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		Scope:           domain.ScopeProject,
		HookIDs:         []string{"git-hooks"},
		AutoConfirmPlan: true,
	})

	// Assert — the flow must complete without error.
	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when HookIDs is pre-answered and "+
			"the module returns a supported hook plan", err)
	}
	// Assert — the module's HookPlan method must have been called with the bundle.
	if len(hookMod.capturedBundles) == 0 {
		t.Fatal("module.HookPlan was never called; DeployHooks must call buildHookPlans " +
			"which in turn calls module.HookPlan for each selected bundle — " +
			"this is the path that resolves the per-harness variant and produces registration steps")
	}
	if hookMod.capturedBundles[0].Key != "git-hooks" {
		t.Errorf("module.HookPlan was called with bundle key %q, want %q; "+
			"buildHookPlans must forward the catalog bundle to the module unchanged",
			hookMod.capturedBundles[0].Key, "git-hooks")
	}
	// Assert — the executor must have received the hook plan with registration steps.
	if !capExec.called {
		t.Fatal("executor was not called; DeployHooks must call the executor")
	}
	if len(capExec.capturedHooks) == 0 {
		t.Fatal("ExecRequest.Hooks is empty; the hook plan with registration steps must " +
			"reach the executor via ExecRequest.Hooks")
	}
	if len(capExec.capturedHooks[0].Registration) == 0 {
		t.Error("ExecRequest.Hooks[0].Registration is empty even though the module returned " +
			"a hook plan with registration steps; DeployHooks must wire buildHookPlans output " +
			"into ExecRequest.Hooks so registration steps are applied by the executor")
	}
}

// ---------------------------------------------------------------------------
// Hook plan and execution wiring — bundle is forwarded to module.HookPlan
// ---------------------------------------------------------------------------

// TestDeployHooks_Bundle_ForwardedToModuleHookPlan verifies that the selected bundle is
// passed to module.HookPlan. The harness module is responsible for selecting the correct
// per-harness variant from the bundle's Variants map; the service's contract is to supply
// the full bundle so the module can perform that selection.
func TestDeployHooks_Bundle_ForwardedToModuleHookPlan(t *testing.T) {
	// Arrange — use the recording module to capture what bundle it receives.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	hookMod := &hookPlanRecordingModule{
		stubHarnessModule: *newMinimalModule(),
		hookPlanResult:    domain.HookPlan{Supported: false, Reason: "stub"},
	}
	deps.Registry = &stubRegistry{
		list:    []domain.HarnessRef{minimalHarness},
		modules: map[string]domain.HarnessModule{"stub-harness": hookMod},
	}
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	deps.Executor = &stubExecutor{result: newMinimalExecResult(workspace)}
	svc := app.New(deps)

	// Act — pre-answer with the hook bundle key.
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		Scope:           domain.ScopeProject,
		HookIDs:         []string{"git-hooks"},
		AutoConfirmPlan: true,
	})

	// Assert — the flow must complete without error.
	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when HookIDs is pre-answered", err)
	}
	// Assert — module.HookPlan must have been called with the selected bundle.
	if len(hookMod.capturedBundles) == 0 {
		t.Error("module.HookPlan was never called; DeployHooks must call buildHookPlans " +
			"which passes each selected bundle to module.HookPlan — the module uses the " +
			"bundle's Variants map to select the per-harness variant and produce " +
			"harness-specific files and registration steps")
	}
}

// ---------------------------------------------------------------------------
// Empty-catalog path — asks nothing, produces no error
// ---------------------------------------------------------------------------

// TestDeployHooks_EmptyHookCatalog_QHooksNotAsked verifies that when the catalog has no hook
// bundles, QHooks is not asked. There is nothing to offer, so presenting the question would
// be a no-op. The flow must detect the empty catalog and skip the ask.
func TestDeployHooks_EmptyHookCatalog_QHooksNotAsked(t *testing.T) {
	// Arrange — catalog with no hook bundles.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	// Minimal catalog has no hook bundles by default.
	deps.Catalog = newMinimalCatalog()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act — HookIDs nil; but the catalog is empty so the question must be skipped.
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         nil,
		AutoConfirmPlan: true,
	})

	// Assert — the flow must complete without error when the catalog has no hooks.
	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"an empty hook catalog must produce an empty plan (not an error) — "+
			"the flow must skip the QHooks ask and proceed normally", err)
	}
	// Assert — QHooks must not be asked when the catalog has no hook bundles.
	if stub.WasAsked(domain.QHooks, "") {
		t.Error("QHooks was asked even though the catalog has no hook bundles; " +
			"DeployHooks must skip the question when the catalog is empty — " +
			"there is nothing to offer, so asking would be a no-op")
	}
}

// ---------------------------------------------------------------------------
// Empty-catalog path — produces an empty plan, not an error
// ---------------------------------------------------------------------------

// TestDeployHooks_EmptyHookCatalog_ReturnsNoError verifies that when the catalog has no hook
// bundles, DeployHooks completes successfully with an empty plan rather than returning an
// error. This is a successful no-op that must not be treated as a failure.
func TestDeployHooks_EmptyHookCatalog_ReturnsNoError(t *testing.T) {
	// Arrange — catalog with no hook bundles.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newMinimalCatalog()
	capExec := &hooksCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, err := svc.DeployHooks(context.Background(), newDeployHooksRequest(workspace, "stub-harness"))

	// Assert — the flow must complete without error for an empty catalog.
	if err != nil {
		t.Errorf("DeployHooks returned error %v for an empty hook catalog; "+
			"an empty catalog is not an error condition — the flow must produce an "+
			"empty plan and return a successful RunSummary, not abort with an error", err)
	}
}

// ---------------------------------------------------------------------------
// SkipAll path — bypasses QHooks ask
// ---------------------------------------------------------------------------

// TestDeployHooks_SkipAllQHooks_QHooksNotAsked verifies that when SkipAll[QHooks] is true
// and HookIDs is nil, the flow yields an empty selection without presenting QHooks.
func TestDeployHooks_SkipAllQHooks_QHooksNotAsked(t *testing.T) {
	// Arrange — no answer scripted for QHooks; SkipAll must prevent the ask.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act — HookIDs nil but SkipAll suppresses the question.
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         nil,
		SkipAll:         map[domain.QuestionID]bool{domain.QHooks: true},
		AutoConfirmPlan: true,
	})

	// Assert — the flow must complete without error when SkipAll suppresses the only question.
	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when SkipAll suppresses QHooks", err)
	}
	// Assert — QHooks must not be asked when skipped via SkipAll.
	if stub.WasAsked(domain.QHooks, "") {
		t.Error("QHooks was asked even though SkipAll[QHooks] was true; " +
			"the skip-all gate must suppress the question and yield an empty selection " +
			"without presenting QHooks through the Interaction port")
	}
}

// ---------------------------------------------------------------------------
// SkipAll path — produces an empty plan (not an error)
// ---------------------------------------------------------------------------

// TestDeployHooks_SkipAllQHooks_ReturnsNoError verifies that SkipAll[QHooks] combined with
// a nil HookIDs produces a successful empty-selection run rather than an error.
func TestDeployHooks_SkipAllQHooks_ReturnsNoError(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         nil,
		SkipAll:         map[domain.QuestionID]bool{domain.QHooks: true},
		AutoConfirmPlan: true,
	})

	// Assert — SkipAll must not cause an error; empty selection is a valid no-op.
	if err != nil {
		t.Errorf("DeployHooks returned error %v when SkipAll[QHooks] was true; "+
			"SkipAll must suppress the question and yield an empty selection; "+
			"an empty selection must produce an empty plan, not an error", err)
	}
}

// ---------------------------------------------------------------------------
// Non-nil empty HookIDs — means "none selected, do not ask"
// ---------------------------------------------------------------------------

// TestDeployHooks_NonNilEmptyHookIDs_QHooksNotAsked verifies that when HookIDs is an
// explicitly empty non-nil slice, the flow does not ask QHooks. This is the CD-6 "do not
// ask" sentinel: non-nil means pre-answered, empty means zero selection.
func TestDeployHooks_NonNilEmptyHookIDs_QHooksNotAsked(t *testing.T) {
	// Arrange — explicitly empty slice, not nil.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act — empty non-nil slice: pre-answered as "none".
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         []string{}, // non-nil empty → "none, do not ask"
		AutoConfirmPlan: true,
	})

	// Assert — the flow must complete without error for a non-nil empty pre-answer.
	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete without error when HookIDs is a non-nil empty slice", err)
	}
	// Assert — QHooks must not be asked for a non-nil empty pre-answer.
	if stub.WasAsked(domain.QHooks, "") {
		t.Error("QHooks was asked even though HookIDs was a non-nil empty slice; " +
			"a non-nil HookIDs always means pre-answered (CD-6 convention), " +
			"including the empty-slice case which means 'deploy no hooks'")
	}
}

// ---------------------------------------------------------------------------
// Orchestrator absence — planner receives Mode = ModeDeployHooks
// ---------------------------------------------------------------------------

// TestDeployHooks_PlannerInput_ModeIsDeployHooks verifies that the planner receives
// Mode = ModeDeployHooks in its Input. The planner uses OrchestratorExcludedFor(mode) to
// decide whether to include the orchestrator in the artifact set, so the mode must be
// correct for the orchestrator to be excluded.
func TestDeployHooks_PlannerInput_ModeIsDeployHooks(t *testing.T) {
	// Arrange — capturing planner records the Input it receives.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	capPlan := &capturingPlanner{result: hooksPlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployHooks(context.Background(), newDeployHooksRequest(workspace, "stub-harness"))

	// Assert — planner must have been called.
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployHooks must call the planner " +
			"to compute a deployment plan before the executor writes anything")
	}
	if capPlan.capturedInput.Mode != domain.ModeDeployHooks {
		t.Errorf("Planner.Build received Mode = %q, want %q; "+
			"DeployHooks must pass ModeDeployHooks to the planner so that "+
			"OrchestratorExcludedFor(mode) returns true and the orchestrator is excluded "+
			"from the artifact set",
			capPlan.capturedInput.Mode, domain.ModeDeployHooks)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator absence — planner receives no WorkflowIDs
// ---------------------------------------------------------------------------

// TestDeployHooks_PlannerInput_WorkflowIDsIsEmpty verifies that the planner receives an
// empty or nil WorkflowIDs slice. Workflows are entirely out of scope for this mode.
func TestDeployHooks_PlannerInput_WorkflowIDsIsEmpty(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	capPlan := &capturingPlanner{result: hooksPlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployHooks(context.Background(), newDeployHooksRequest(workspace, "stub-harness"))

	// Assert
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployHooks must call the planner")
	}
	if len(capPlan.capturedInput.WorkflowIDs) > 0 {
		t.Errorf("Planner.Build received WorkflowIDs = %v; want empty; "+
			"DeployHooks must not pass any workflow IDs to the planner — "+
			"workflows are outside the scope of this mode and must never cause "+
			"workflow agents or orchestrator rewrites to be planned",
			capPlan.capturedInput.WorkflowIDs)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator absence — planner receives no agent IDs of any class
// ---------------------------------------------------------------------------

// TestDeployHooks_PlannerInput_AllAgentIDsAreEmpty verifies that the planner receives no
// agent IDs of any class (SubagentIDs, UtilityAgentIDs, InfrastructureAgentIDs,
// StandaloneAgentIDs). This mode generates no agent content.
func TestDeployHooks_PlannerInput_AllAgentIDsAreEmpty(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	capPlan := &capturingPlanner{result: hooksPlan(workspace)}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployHooks(context.Background(), newDeployHooksRequest(workspace, "stub-harness"))

	// Assert
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; DeployHooks must call the planner")
	}
	if len(capPlan.capturedInput.UtilityAgentIDs) > 0 {
		t.Errorf("Planner.Build received UtilityAgentIDs = %v; want empty; "+
			"this mode must not pass any utility agent IDs to the planner",
			capPlan.capturedInput.UtilityAgentIDs)
	}
	if len(capPlan.capturedInput.InfrastructureAgentIDs) > 0 {
		t.Errorf("Planner.Build received InfrastructureAgentIDs = %v; want empty; "+
			"this mode must not pass any infrastructure agent IDs to the planner",
			capPlan.capturedInput.InfrastructureAgentIDs)
	}
	if len(capPlan.capturedInput.StandaloneAgentIDs) > 0 {
		t.Errorf("Planner.Build received StandaloneAgentIDs = %v; want empty; "+
			"this mode must not pass any standalone agent IDs to the planner",
			capPlan.capturedInput.StandaloneAgentIDs)
	}
	if len(capPlan.capturedInput.SubagentIDs) > 0 {
		t.Errorf("Planner.Build received SubagentIDs = %v; want empty; "+
			"this mode must not pass any subagent IDs to the planner",
			capPlan.capturedInput.SubagentIDs)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator absence — real planner produces no orchestrator plan item
// ---------------------------------------------------------------------------

// TestDeployHooks_RealPlanner_OrchestratorAbsentFromPlan verifies that with the real planner
// wired in, a hooks-only run produces a plan that contains no orchestrator plan item.
//
// Without OrchestratorExcludedFor(ModeDeployHooks) returning true (already implemented in
// plan.OrchestratorExcludedFor), the planner would include the orchestrator unconditionally.
// This test exercises the full plan-build path to confirm that mode → exclusion → plan is
// correctly wired in the service.
func TestDeployHooks_RealPlanner_OrchestratorAbsentFromPlan(t *testing.T) {
	// Arrange — real planner; capturing executor records the computed plan.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec
	svc := app.New(deps)

	// Act — pre-answer all selections so the flow runs headlessly; no hooks selected
	// so the plan is trivially empty but the orchestrator exclusion is still applied.
	_, err := svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		Scope:           domain.ScopeProject,
		HookIDs:         []string{},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"with pre-answered selections the flow must complete successfully", err)
	}

	// Assert — executor must have been called (the planning pipeline must have run).
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; DeployHooks must invoke the executor via the planner — " +
			"this test uses the real planner to verify the orchestrator-exclusion path is " +
			"wired through the full pipeline")
	}

	// Assert — no orchestrator plan item, regardless of action.
	// If the orchestrator appears here, ModeDeployHooks is not being forwarded correctly to
	// the planner, or OrchestratorExcludedFor is not consulted.
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "orchestrator" {
			t.Errorf("plan contains an orchestrator item (action=%v, target=%q) in a hooks-only run; "+
				"a deploy-hooks plan must never include the orchestrator (the orchestrator is "+
				"excluded because OrchestratorExcludedFor(ModeDeployHooks)=true); "+
				"if the planner receives a mode other than ModeDeployHooks, "+
				"OrchestratorExcludedFor returns false and the orchestrator enters the plan",
				item.Action, item.TargetPath)
		}
	}
}

// ---------------------------------------------------------------------------
// RunSummary.Mode is ModeDeployHooks
// ---------------------------------------------------------------------------

// TestDeployHooks_RunSummaryModeIsDeployHooks verifies that the RunSummary returned by
// DeployHooks carries ModeDeployHooks. This distinguishes the mode in run records, the
// todo file metadata, and the CLI output.
func TestDeployHooks_RunSummaryModeIsDeployHooks(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHookBundles()
	deps.Planner = &stubPlanner{plan: hooksPlan(workspace)}
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployHooks(context.Background(), newDeployHooksRequest(workspace, "stub-harness"))

	// Assert — a nil error means the flow ran; check the mode.
	if err != nil {
		t.Fatalf("DeployHooks returned error %v; "+
			"the flow must complete successfully when all required fields are pre-answered", err)
	}
	if summary.Mode != domain.ModeDeployHooks {
		t.Errorf("RunSummary.Mode = %q, want %q; "+
			"DeployHooks must report ModeDeployHooks in the summary "+
			"so the mode is distinguishable in run records and CLI output",
			summary.Mode, domain.ModeDeployHooks)
	}
}

// ---------------------------------------------------------------------------
// Compile-time interface checks
// ---------------------------------------------------------------------------

// Ensure the necessary domain constants exist and are the expected types.
var (
	_ domain.RunMode    = domain.ModeDeployHooks
	_ domain.QuestionID = domain.QHooks
	_ app.Service       = (*hooksPlannerSmokeCheck)(nil) // compile-time check that Service includes DeployHooks
)

// hooksPlannerSmokeCheck is a minimal struct that must satisfy app.Service. It exists only
// to verify at compile time that app.Service includes the DeployHooks method. It is never
// constructed or used at runtime.
type hooksPlannerSmokeCheck struct{}

func (h *hooksPlannerSmokeCheck) ListHarnesses() []domain.HarnessRef { return nil }
func (h *hooksPlannerSmokeCheck) DeployNew(_ context.Context, _ app.DeployRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (h *hooksPlannerSmokeCheck) Update(_ context.Context, _ app.UpdateRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (h *hooksPlannerSmokeCheck) UpdateWorkflows(_ context.Context, _ app.WorkflowUpdateRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (h *hooksPlannerSmokeCheck) TransformHarness(_ context.Context, _ app.TransformHarnessRequest) (app.TransformHarnessResult, error) {
	return app.TransformHarnessResult{}, nil
}
func (h *hooksPlannerSmokeCheck) DeployAgents(_ context.Context, _ app.DeployAgentsRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (h *hooksPlannerSmokeCheck) DeployHooks(_ context.Context, _ app.DeployHooksRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (h *hooksPlannerSmokeCheck) RenderAgent(_ context.Context, _ app.RenderAgentRequest) (app.RenderAgentResult, error) {
	return app.RenderAgentResult{}, nil
}
func (h *hooksPlannerSmokeCheck) CheckWorkflowIndex(_ context.Context) (app.IndexCheckResult, error) {
	return app.IndexCheckResult{}, nil
}
func (h *hooksPlannerSmokeCheck) Promote(_ context.Context, _ app.PromoteRequest) (app.PromoteResult, error) {
	return app.PromoteResult{}, nil
}
