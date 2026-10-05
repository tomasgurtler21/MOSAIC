package app_test

// deployhooks_registration_regression_test.go pins the existing deploy-hooks behaviour for an
// already-deployed hook that is at the catalog version: its registration gap is still reported
// in the plan and in the TODO when the registration target exists. The Update hook refresh
// filters registration artefacts of hooks it does not write; deploy-hooks must not.

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

func TestDeployHooks_UnchangedDeployedHookWithExistingTarget_RegistrationGapStillReported(t *testing.T) {
	module := newTestHookModule(uhHarnessID, newTestHookPlan(t, uhFlat))
	review := &uhReviewSpy{Stub: interactiontest.NewBuilder().AnswerReview(true).Build()}
	deps, workspace := newBaseDeps(t, review.Stub)
	deps.Interaction = review
	cat := newMinimalCatalog()
	cat.hooks = []domain.HookBundle{{Key: uhHookKey, Version: uhCatalogVersion}}
	deps.Catalog = cat
	deps.Planner = &uhPlannerSpy{inner: plan.New(), workspace: workspace}
	deps.Registry = &stubRegistry{
		list:    []domain.HarnessRef{module.Ref()},
		modules: map[string]domain.HarnessModule{uhHarnessID: module},
	}
	deps.Manifest = &stubManifestStore{snap: manifest.Snapshot{State: manifest.StatePresent, Manifest: domain.Manifest{
		SchemaVersion: manifest.SchemaVersion, HarnessID: uhHarnessID,
		Entries: []domain.ManifestEntry{{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactHook, Key: uhHookKey},
			TargetPath: uhHookTarget, Version: uhCatalogVersion, ContentHash: manifest.Hash(nil),
		}},
	}}}
	deps.Executor = deploy.NewExecutor(deps.Manifest, deps.Logger, deps.Todo)
	uhSeedFiles(t, workspace, map[string]string{
		uhHooksDir + "/logger.sh": "deployed earlier\n",
		uhRegTarget:               uhExistingSettings,
	})

	_, err := app.New(deps).DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       uhHarnessID,
		WorkspacePath:   workspace,
		Scope:           domain.ScopeProject,
		HookIDs:         []string{uhHookKey},
		AutoConfirmPlan: true,
	})

	if err != nil {
		t.Fatalf("DeployHooks: %v", err)
	}
	if review.reviewed == nil {
		t.Fatal("no plan was reviewed")
	}
	item, ok := findPlanItem(review.reviewed.Items, uhHookKey)
	if !ok || item.Action != domain.ActionUnchanged {
		t.Fatalf("hook plan item = %+v (found %v), want ActionUnchanged", item, ok)
	}
	if !uhHasRegistrationGap(review.reviewed.Gaps) {
		t.Errorf("deploy-hooks plan lacks the registration gap of an unchanged hook: %+v", review.reviewed.Gaps)
	}
	if !uhHasRegistrationStep(review.reviewed.Registrations) {
		t.Errorf("deploy-hooks plan lacks the registration step of an unchanged hook: %+v", review.reviewed.Registrations)
	}
	if !deps.Todo.(*spyTodo).hasGapKind(domain.GapHookRegistration) {
		t.Errorf("deploy-hooks TODO lacks the hook-registration gap: %+v", deps.Todo.(*spyTodo).gaps)
	}
}
