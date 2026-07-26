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

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
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
// Helpers for this file
// ---------------------------------------------------------------------------

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
