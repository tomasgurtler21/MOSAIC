package plan_test

// deployhooks_exclusion_test.go verifies the orchestrator-exclusion behaviour for the
// deploy-hooks mode.
//
// OrchestratorExcludedFor(ModeDeployHooks) must return true so that:
//   - The built plan contains no orchestrator plan item.
//   - Deploying hook bundles never rewrites the orchestrator file.
//
// These tests exercise the plan layer in isolation. App-level wiring (DeployHooks passing
// ModeDeployHooks to the planner) is covered in deployhooks_test.go in the app package.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// plan.Build with ModeDeployHooks — orchestrator absent from plan items
// ---------------------------------------------------------------------------

// TestBuild_ModeDeployHooks_OrchestratorAbsentFromPlan verifies that plan.Build with
// Mode = ModeDeployHooks produces a plan that contains no plan item with key "orchestrator".
// This exercises the full plan-build path to confirm that the mode-to-exclusion wiring is
// active at the Build layer.
func TestBuild_ModeDeployHooks_OrchestratorAbsentFromPlan(t *testing.T) {
	// Arrange — catalog with an orchestrator but no hook bundles or agents selected.
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
	}
	mod := newFakeModule()

	planner := plan.New()
	built, err := planner.Build(context.Background(), plan.Input{
		Mode:          domain.ModeDeployHooks,
		Catalog:       cat,
		Module:        mod,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		// No agent IDs, workflow IDs, or hook IDs — empty hooks-only run.
	})
	if err != nil {
		t.Fatalf("plan.Build with ModeDeployHooks returned error %v; "+
			"a hooks-only plan with no agents selected must succeed with an empty plan", err)
	}

	// Assert — no orchestrator item in the plan.
	for _, item := range built.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "orchestrator" {
			t.Errorf("plan.Build(ModeDeployHooks) produced an orchestrator plan item "+
				"(action=%v, target=%q); a deploy-hooks plan must not include the orchestrator — "+
				"OrchestratorExcludedFor(ModeDeployHooks) must return true so Build suppresses it; "+
				"if the orchestrator appears here, the mode is not being forwarded to the exclusion check",
				item.Action, item.TargetPath)
		}
	}
}
