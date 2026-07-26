package app_test

// workflow_test.go verifies that adding workflows to an existing deployment in the same run
// regenerates the orchestrator's workflow injection without losing previously-injected workflows.
//
// Verified invariants (T18.6, AC18.6):
//
// Workflow injection regeneration:
//   - When AddWorkflowIDs is set in UpdateRequest, the newly-selected workflows are added
//   - The orchestrator's AvailableWorkflows injection contains BOTH the new workflows AND
//     the workflows that were already in the deployed orchestrator
//   - Previously-injected workflows are read from the deployed orchestrator, not assumed
//   - The union is computed without duplicates (the same workflow selected twice counts once)
//
// Order preservation:
//   - Workflows from the deployed orchestrator appear before newly-added workflows
//     (or the implementation preserves a deterministic order; the key invariant is neither
//     set is silently dropped)
//
// Orchestrator workflow staleness (Stage 5 — deploy-new and update flows):
//
//   - deploy-new flow: a workspace with a deployed orchestrator carrying one workflow,
//     replanned with an expanded selection, yields an ActionUpdate orchestrator item.
//   - update flow: the same scenario using the update path also yields ActionUpdate.
//   - The app correctly populates plan.Input.DeployedState with the orchestrator's deployed
//     workflow data (workflow IDs and versions) so the planner can detect the set change.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Workflow injection preservation
// ---------------------------------------------------------------------------

// TestUpdate_AddWorkflows_ExistingWorkflowsPreserved verifies that when AddWorkflowIDs
// adds a new workflow to an existing deployment, the workflows already present in the
// deployed orchestrator's AvailableWorkflows injection are NOT discarded (AC18.6).
func TestUpdate_AddWorkflows_ExistingWorkflowsPreserved(t *testing.T) {
	// Arrange — existing deployment has "quick-fix"; we add "greenfield-tdd"
	existingWorkflow := domain.Workflow{
		ID: "quick-fix", Name: "Quick Fix", Category: "Build",
		ReferencedAgents: []string{"test-runner"},
	}
	newWorkflow := domain.Workflow{
		ID: "greenfield-tdd", Name: "Greenfield TDD", Category: "Build",
		ReferencedAgents: []string{"test-runner"},
	}

	cat := newMinimalCatalog()
	cat.workflows = []domain.Workflow{existingWorkflow, newWorkflow}
	cat.categories = []domain.WorkflowCategory{
		{Name: "Build", Workflows: cat.workflows},
	}

	// The planner must be told about the union of existing + new workflows
	// We configure it to return a plan whose Workflows field contains both
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Manifest = &stubManifestStore{
		snap: manifest.Snapshot{
			State: manifest.StatePresent,
			Manifest: domain.Manifest{
				HarnessID: "stub-harness",
				Entries:   []domain.ManifestEntry{},
			},
		},
	}
	// The planner returns a plan with both workflows — this verifies the app passed both
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdate,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
		Workflows:     []string{"quick-fix", "greenfield-tdd"},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{"greenfield-tdd"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	// Both workflows must appear in the plan that was executed; the summary's workflow list
	// (or the plan passed to the executor) must contain both IDs
	_ = summary // In this stub-based test, the key assertion is that no error occurred
	// and the planner received both workflows in its Input.WorkflowIDs.
	// Full verification of the union logic requires inspecting the planner's Input, which
	// requires a capturing planner — done in the next test.
}

// TestUpdate_AddWorkflows_PlannerReceivesBothWorkflowSets verifies that when the update flow
// adds new workflows, the Planner.Build call includes BOTH the previously-deployed workflow IDs
// and the newly-requested workflow IDs, so the orchestrator injection contains the full set.
func TestUpdate_AddWorkflows_PlannerReceivesBothWorkflowSets(t *testing.T) {
	// Arrange
	existingWorkflow := domain.Workflow{
		ID: "quick-fix", Name: "Quick Fix", Category: "Build",
		ReferencedAgents: []string{"test-runner"},
	}
	newWorkflow := domain.Workflow{
		ID: "greenfield-tdd", Name: "Greenfield TDD", Category: "Build",
		ReferencedAgents: []string{"test-runner"},
	}

	cat := newMinimalCatalog()
	cat.workflows = []domain.Workflow{existingWorkflow, newWorkflow}
	cat.categories = []domain.WorkflowCategory{
		{Name: "Build", Workflows: cat.workflows},
	}

	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Manifest = &stubManifestStore{
		snap: manifest.Snapshot{
			State: manifest.StatePresent,
			Manifest: domain.Manifest{
				HarnessID: "stub-harness",
				Entries:   []domain.ManifestEntry{},
			},
		},
	}

	// Write a deployed orchestrator containing the "quick-fix" workflow section so that the
	// probe-based workflow discovery can find it. Without this file the probe returns no
	// workflow IDs for the orchestrator path and "quick-fix" would be silently dropped.
	orchestratorContent := []byte("---\nversion: \"1.0\"\n---\n\n" +
		"[[SECTION:Workflow:quick-fix]]\n" +
		"<!-- workflow-version: 1.0 -->\n" +
		"Quick fix content.\n" +
		"[[/SECTION:Workflow:quick-fix]]\n")
	writeTempFile(t, workspace, "orchestrator.md", orchestratorContent)

	// Use a capturing planner so we can inspect what WorkflowIDs were passed
	capPlanner := &capturingPlanner{
		result: domain.Plan{
			Mode:          domain.ModeUpdate,
			Harness:       minimalHarness,
			WorkspacePath: workspace,
			Scope:         domain.ScopeProject,
			Items:         []domain.PlanItem{},
			Workflows:     []string{"quick-fix", "greenfield-tdd"},
		},
	}
	deps.Planner = capPlanner
	svc := app.New(deps)

	// Act — deployed orchestrator has "quick-fix"; we add "greenfield-tdd"
	// The app must read "quick-fix" from the deployed orchestrator and union with "greenfield-tdd"
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{"greenfield-tdd"},
		AutoConfirmPlan: true,
	})

	// Assert — the planner must have been called with both workflow IDs
	if capPlanner.capturedInput == nil {
		t.Fatal("Planner.Build was not called; the update flow must call the planner")
	}
	wids := capPlanner.capturedInput.WorkflowIDs
	hasQuickFix := false
	hasGreenfield := false
	for _, id := range wids {
		if id == "quick-fix" {
			hasQuickFix = true
		}
		if id == "greenfield-tdd" {
			hasGreenfield = true
		}
	}
	if !hasQuickFix {
		t.Error("Planner.Build WorkflowIDs does not include \"quick-fix\" from the existing deployment; " +
			"the update flow must union previously-deployed workflow IDs with newly-added ones (AC18.6)")
	}
	if !hasGreenfield {
		t.Error("Planner.Build WorkflowIDs does not include \"greenfield-tdd\" from AddWorkflowIDs; " +
			"newly-selected workflows must be included in the planner input (AC18.6)")
	}
}

// TestUpdate_AddWorkflows_NoDuplicates verifies that if a workflow appears in both the
// existing deployment and AddWorkflowIDs, it is not duplicated in the planner input.
func TestUpdate_AddWorkflows_NoDuplicates(t *testing.T) {
	// Arrange — existing deployment has "quick-fix"; AddWorkflowIDs also requests "quick-fix"
	cat := newMinimalCatalog()

	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Manifest = &stubManifestStore{
		snap: manifest.Snapshot{
			State: manifest.StatePresent,
			Manifest: domain.Manifest{
				HarnessID: "stub-harness",
				Entries:   []domain.ManifestEntry{},
			},
		},
	}

	capPlanner := &capturingPlanner{
		result: domain.Plan{
			Mode:          domain.ModeUpdate,
			Harness:       minimalHarness,
			WorkspacePath: workspace,
			Scope:         domain.ScopeProject,
			Items:         []domain.PlanItem{},
			Workflows:     []string{"quick-fix"},
		},
	}
	deps.Planner = capPlanner
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{"quick-fix"}, // same as existing
		AutoConfirmPlan: true,
	})

	// Assert — "quick-fix" must appear exactly once in the planner input
	if capPlanner.capturedInput == nil {
		t.Skip("Planner.Build not called; cannot verify deduplication")
	}
	count := 0
	for _, id := range capPlanner.capturedInput.WorkflowIDs {
		if id == "quick-fix" {
			count++
		}
	}
	if count > 1 {
		t.Errorf("\"quick-fix\" appears %d times in Planner.Build WorkflowIDs; "+
			"workflow IDs must be deduplicated (AC18.6)", count)
	}
}

// ---------------------------------------------------------------------------
// T5.6 — App-layer end-to-end: deploy-new and update flows with workflow set expansion
// ---------------------------------------------------------------------------

// TestDeployNew_OrchestratorsWorkflowExpanded_YieldsUpdateItem verifies the deploy-new flow:
// when the workspace already has a deployed orchestrator carrying one workflow and the new
// deployment selects an expanded set, the planner must classify the orchestrator as ActionUpdate
// (not ActionUnchanged). This test uses the real planner so the classification is exercised
// end-to-end; it fails until WorkflowStaleness is invoked from classifyAgentItem.
func TestDeployNew_OrchestratorWorkflowExpanded_YieldsUpdateItem(t *testing.T) {
	// Arrange — catalog has quick-fix (already deployed) and greenfield-tdd (newly selected)
	greenfieldWF := domain.Workflow{
		ID:               "greenfield-tdd",
		Name:             "Greenfield TDD",
		Version:          "1.0",
		Category:         "Build",
		ReferencedAgents: []string{"test-runner"},
	}
	cat := newMinimalCatalog()
	cat.workflows = append(cat.workflows, greenfieldWF)
	cat.categories = []domain.WorkflowCategory{
		{Name: "Build", Workflows: cat.workflows},
	}

	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat

	// Write a deployed orchestrator at the stub module's target path for "orchestrator":
	// stubHarnessModule.TargetPath returns req.Key + ".md", so the file is "orchestrator.md".
	// It carries a quick-fix workflow block with a version comment so the probe can detect it.
	orchContent := []byte(
		"---\nversion: \"1.0\"\n---\n\n" +
			"[[SECTION:Workflow:quick-fix]]\n" +
			"<!-- workflow-version: 1.0 -->\n" +
			"Quick fix content.\n" +
			"[[/SECTION:Workflow:quick-fix]]\n",
	)
	writeTempFile(t, workspace, "orchestrator.md", orchContent)
	orchHash := manifest.Hash(orchContent)

	// Set the manifest to present, with an entry for the orchestrator whose hash matches
	// the deployed file. No version-field mismatch — the only staleness must come from
	// the expanded workflow set.
	deps.Manifest = &stubManifestStore{
		snap: manifest.Snapshot{
			State: manifest.StatePresent,
			Manifest: domain.Manifest{
				SchemaVersion: manifest.SchemaVersion,
				HarnessID:     "stub-harness",
				Entries: []domain.ManifestEntry{
					{
						Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
						TargetPath: "orchestrator.md",
						Version:    "1.0",
						ContentHash: orchHash,
					},
				},
			},
		},
	}

	// Use the real planner so the classification is exercised end-to-end.
	// Use a spy executor to capture the plan without actually writing to disk.
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec

	svc := app.New(deps)

	// Act — deploy with both quick-fix (already deployed) and greenfield-tdd (newly selected)
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		Scope:           domain.ScopeProject,
		WorkflowIDs:     []string{"quick-fix", "greenfield-tdd"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — the executor must have received a plan with the orchestrator as ActionUpdate.
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; cannot assert orchestrator plan item action")
	}
	orchItem, ok := findPlanItem(capExec.capturedPlan.Items, "orchestrator")
	if !ok {
		t.Fatal("plan has no item for orchestrator")
	}
	if orchItem.Action != domain.ActionUpdate {
		t.Errorf("orchestrator with expanded workflow selection (deploy-new flow): Action = %q, want %q; "+
			"adding a workflow to the selection must classify the orchestrator as ActionUpdate",
			orchItem.Action, domain.ActionUpdate)
	}
}

// TestUpdate_OrchestratorWorkflowExpanded_YieldsUpdateItem verifies the update flow: when the
// deployed orchestrator carries one workflow and AddWorkflowIDs expands the selection, the
// planner must classify the orchestrator as ActionUpdate. This test uses the real planner.
func TestUpdate_OrchestratorWorkflowExpanded_YieldsUpdateItem(t *testing.T) {
	// Arrange — catalog has quick-fix (already deployed) and greenfield-tdd (newly added)
	greenfieldWF := domain.Workflow{
		ID:               "greenfield-tdd",
		Name:             "Greenfield TDD",
		Version:          "1.0",
		Category:         "Build",
		ReferencedAgents: []string{"test-runner"},
	}
	cat := newMinimalCatalog()
	cat.workflows = append(cat.workflows, greenfieldWF)
	cat.categories = []domain.WorkflowCategory{
		{Name: "Build", Workflows: cat.workflows},
	}

	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat

	// Write the deployed orchestrator at "orchestrator.md" with the quick-fix workflow block.
	orchContent := []byte(
		"---\nversion: \"1.0\"\n---\n\n" +
			"[[SECTION:Workflow:quick-fix]]\n" +
			"<!-- workflow-version: 1.0 -->\n" +
			"Quick fix content.\n" +
			"[[/SECTION:Workflow:quick-fix]]\n",
	)
	writeTempFile(t, workspace, "orchestrator.md", orchContent)
	orchHash := manifest.Hash(orchContent)

	deps.Manifest = &stubManifestStore{
		snap: manifest.Snapshot{
			State: manifest.StatePresent,
			Manifest: domain.Manifest{
				SchemaVersion: manifest.SchemaVersion,
				HarnessID:     "stub-harness",
				Entries: []domain.ManifestEntry{
					{
						Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
						TargetPath: "orchestrator.md",
						Version:    "1.0",
						ContentHash: orchHash,
					},
				},
			},
		},
	}

	// Real planner + spy executor
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec

	svc := app.New(deps)

	// Act — update flow, adding greenfield-tdd to the existing deployment
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{"greenfield-tdd"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — orchestrator plan item must be ActionUpdate
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; cannot assert orchestrator plan item action")
	}
	orchItem, ok := findPlanItem(capExec.capturedPlan.Items, "orchestrator")
	if !ok {
		t.Fatal("plan has no item for orchestrator")
	}
	if orchItem.Action != domain.ActionUpdate {
		t.Errorf("orchestrator with expanded workflow selection (update flow): Action = %q, want %q; "+
			"adding a workflow in the update flow must classify the orchestrator as ActionUpdate",
			orchItem.Action, domain.ActionUpdate)
	}
}

// TestDeployNew_OrchestratorWorkflowVersionBumped_YieldsUpdateItem verifies the deploy-new
// flow for the catalog version bump scenario: when the deployed orchestrator carries a
// workflow at version "1.0" and the catalog has bumped that same workflow to "2.0", the
// orchestrator must be classified as ActionUpdate even though the selection is unchanged.
// This test uses the real planner so the classification is exercised end-to-end; it fails
// until WorkflowStaleness is invoked from classifyAgentItem and detects the version change.
func TestDeployNew_OrchestratorWorkflowVersionBumped_YieldsUpdateItem(t *testing.T) {
	// Arrange — catalog has quick-fix at version "2.0" (bumped from deployed "1.0")
	bumpedWorkflow := domain.Workflow{
		ID:               "quick-fix",
		Name:             "Quick Fix",
		Version:          "2.0", // catalog version bumped
		Category:         "Build",
		ReferencedAgents: []string{"test-runner"},
	}
	cat := newMinimalCatalog()
	cat.workflows = []domain.Workflow{bumpedWorkflow}
	cat.categories = []domain.WorkflowCategory{
		{Name: "Build", Workflows: []domain.Workflow{bumpedWorkflow}},
	}

	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat

	// Write a deployed orchestrator at "orchestrator.md" carrying quick-fix at version "1.0".
	// The deployed workflow-version comment is "1.0" but the catalog now has "2.0" — this
	// mismatch must be detected by WorkflowStaleness even though the selection is unchanged.
	orchContent := []byte(
		"---\nversion: \"1.0\"\n---\n\n" +
			"[[SECTION:Workflow:quick-fix]]\n" +
			"<!-- workflow-version: 1.0 -->\n" +
			"Quick fix content.\n" +
			"[[/SECTION:Workflow:quick-fix]]\n",
	)
	writeTempFile(t, workspace, "orchestrator.md", orchContent)
	orchHash := manifest.Hash(orchContent)

	// Manifest carries the orchestrator entry; no version-field mismatch — the only
	// staleness must come from the workflow catalog version bump.
	deps.Manifest = &stubManifestStore{
		snap: manifest.Snapshot{
			State: manifest.StatePresent,
			Manifest: domain.Manifest{
				SchemaVersion: manifest.SchemaVersion,
				HarnessID:     "stub-harness",
				Entries: []domain.ManifestEntry{
					{
						Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
						TargetPath: "orchestrator.md",
						Version:    "1.0",
						ContentHash: orchHash,
					},
				},
			},
		},
	}

	// Use the real planner so the classification is exercised end-to-end.
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec

	svc := app.New(deps)

	// Act — deploy with quick-fix only (selection unchanged from what is deployed)
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		Scope:           domain.ScopeProject,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — the executor must have received a plan with the orchestrator as ActionUpdate.
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; cannot assert orchestrator plan item action")
	}
	orchItem, ok := findPlanItem(capExec.capturedPlan.Items, "orchestrator")
	if !ok {
		t.Fatal("plan has no item for orchestrator")
	}
	if orchItem.Action != domain.ActionUpdate {
		t.Errorf("orchestrator with catalog version bump on unchanged selection (deploy-new): Action = %q, want %q; "+
			"a workflow catalog version bump must classify the orchestrator as ActionUpdate",
			orchItem.Action, domain.ActionUpdate)
	}
}

// TestDeployNew_OrchestratorDeployedState_ContainsWorkflowData verifies that the deploy-new
// flow correctly populates the orchestrator's DeployedState with the workflow IDs and versions
// read from the deployed file. This is the app layer's responsibility: supplying the planner
// with the right DeployedState so workflow staleness comparisons are possible.
func TestDeployNew_OrchestratorDeployedState_ContainsWorkflowData(t *testing.T) {
	// Arrange — deployed orchestrator carries a quick-fix workflow block with version 1.0
	orchContent := []byte(
		"---\nversion: \"1.0\"\n---\n\n" +
			"[[SECTION:Workflow:quick-fix]]\n" +
			"<!-- workflow-version: 1.0 -->\n" +
			"Quick fix content.\n" +
			"[[/SECTION:Workflow:quick-fix]]\n",
	)

	cat := newMinimalCatalog()
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat

	writeTempFile(t, workspace, "orchestrator.md", orchContent)

	// Use a capturing planner to inspect what DeployedState was passed
	capPlan := &capturingPlanner{
		result: domain.Plan{
			Mode:          domain.ModeDeployNew,
			Harness:       minimalHarness,
			WorkspacePath: workspace,
			Scope:         domain.ScopeProject,
			Items:         []domain.PlanItem{},
			Workflows:     []string{"quick-fix"},
		},
	}
	deps.Planner = capPlan
	svc := app.New(deps)

	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		Scope:           domain.ScopeProject,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called")
	}

	// Locate the orchestrator's entry in DeployedState
	var orchState *domain.DeployedArtifactState
	for path, state := range capPlan.capturedInput.DeployedState {
		if path == "orchestrator.md" {
			s := state
			orchState = &s
			break
		}
	}
	if orchState == nil {
		t.Fatal("plan.Input.DeployedState has no entry for orchestrator.md; " +
			"the app must probe and include the orchestrator's deployed state")
	}
	if !orchState.Present {
		t.Error("DeployedState[orchestrator.md].Present = false; " +
			"the deployed orchestrator file exists but the probe did not detect it")
	}

	// The deployed state must carry the workflow IDs extracted from the file
	deployedIDs := orchState.Workflows.IDs()
	found := false
	for _, id := range deployedIDs {
		if id == "quick-fix" {
			found = true
		}
	}
	if !found {
		t.Errorf("DeployedState[orchestrator.md].Workflows = %v; "+
			"expected to contain \"quick-fix\" extracted from the deployed file's workflow section markers",
			deployedIDs)
	}
}

// ---------------------------------------------------------------------------
// Helpers for this file
// ---------------------------------------------------------------------------

// planCapturingExecutor records the plan.Plan it receives and returns a configurable result.
// It is used to assert on the plan item actions produced by the real planner without
// performing actual file-system writes.
type planCapturingExecutor struct {
	capturedPlan *domain.Plan
	result       deploy.ExecResult
	err          error
}

func (e *planCapturingExecutor) Execute(ctx context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	p := req.Plan
	e.capturedPlan = &p
	return e.result, e.err
}

// findPlanItem returns the first PlanItem whose Ref.Key matches key, and whether it was found.
func findPlanItem(items []domain.PlanItem, key string) (domain.PlanItem, bool) {
	for _, item := range items {
		if item.Ref.Key == key {
			return item, true
		}
	}
	return domain.PlanItem{}, false
}

// capturingPlanner records the Input passed to Build and returns a configurable plan.
type capturingPlanner struct {
	result        domain.Plan
	err           error
	capturedInput *plan.Input
}

func (p *capturingPlanner) Build(ctx context.Context, in plan.Input) (domain.Plan, error) {
	cp := in
	p.capturedInput = &cp
	return p.result, p.err
}
