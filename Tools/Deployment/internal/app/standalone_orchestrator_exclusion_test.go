package app_test

// standalone_orchestrator_exclusion_test.go covers the end-to-end invariant for the
// standalone deploy flow: a standalone-only run must never plan the orchestrator, even when
// the orchestrator file is locally present (or locally modified) in the workspace.
//
// These are TDD RED-phase tests. They specify the intended behaviour of the exclusion
// mechanism and will fail until I1.1–I1.3 are delivered.
//
// Verified invariants:
//
// No orchestrator plan item (AC1.3):
//   - When an orchestrator file exists at its target path in the workspace and the standalone
//     flow runs with the real planner, the resulting plan contains no orchestrator plan item.
//   - Without the fix, the orchestrator is unconditionally seeded into the probe set, its
//     on-disk file is detected, and classifyAgentItem produces an ActionConflict item for it.
//
// No orchestrator conflict question (AC1.3, indirect):
//   - The plan having no orchestrator item means no executor can ask QLocalModification for
//     the orchestrator path, so the indirect QLocalModification invariant is satisfied
//     transitively by the plan-content check above.
//
// Other modes unaffected (AC1.4):
//   - DeployNew with a locally-present orchestrator file still includes the orchestrator item
//     in its plan, confirming the exclusion is mode-gated and leaves other modes unchanged.
//
// Both paths consistent (AC1.3 — probe path and plan path):
//   - The probe set (used for deployed-state reading) and the plan artifact set (used by
//     Build) both apply OrchestratorExcludedFor so they cannot disagree. Testing the plan
//     output with the real planner exercises both paths simultaneously.

import (
	"context"
	"os"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// orchestratorFileContent is a minimal orchestrator file body. The version stamp matches
// minimalOrchestrator.Version so that, if an orchestrator is accidentally included in the
// probe set, classifyAgentItem can read a version from the file and still classify it (as
// ActionConflict via ManifestMissing rather than as "no version info"). The content itself
// is deliberately different from what the deploy tool would produce so that, if a manifest
// were present, it would be detected as locally modified.
var orchestratorFileContent = []byte("---\nversion: 1.0\n---\n# Orchestrator\nThis file was hand-placed to simulate a locally-present orchestrator.\n")

// writeOrchestratorFile writes a stub orchestrator file to the workspace at the path the
// stub harness module would target for the orchestrator agent. The stub module returns
// req.Key + ".md" for every agent, so the orchestrator's target path is "orchestrator.md"
// relative to the workspace root.
func writeOrchestratorFile(t *testing.T, workspace string) {
	t.Helper()
	path := workspace + "/orchestrator.md"
	if err := os.WriteFile(path, orchestratorFileContent, 0o644); err != nil {
		t.Fatalf("setup: could not write stub orchestrator file at %q: %v", path, err)
	}
}

// catalogWithStandaloneOnly returns a catalog that has the minimal orchestrator and a single
// standalone agent. The standalone agent is the only expected plan artifact in a standalone run.
func catalogWithStandaloneOnly() *stubCatalog {
	cat := newMinimalCatalog()
	cat.standaloneAgents = []domain.Agent{minimalStandaloneAgent}
	return cat
}

// ---------------------------------------------------------------------------
// AC1.3 — No orchestrator plan item in a standalone run with existing orchestrator file
// ---------------------------------------------------------------------------

// TestDeployStandalone_OrchestratorFilePresent_NoOrchestratorPlanItem verifies that when the
// orchestrator file already exists at its workspace target path, running DeployStandalone with
// the real planner produces a plan that contains no orchestrator plan item.
//
// Without the fix, the standalone service calls plan.ResolveArtifactsFrom without setting
// ExcludeOrchestrator=true, so the orchestrator is included in the probe set. The probe finds
// the file on disk. With no manifest, classifyAgentItem classifies the orchestrator as
// ActionConflict (ManifestMissing=true) and the planner includes it in the plan.
//
// With the fix, OrchestratorExcludedFor(ModeStandaloneOnly) returns true, the orchestrator
// is excluded from both the probe set and the plan artifact set, and the plan is clean.
func TestDeployStandalone_OrchestratorFilePresent_NoOrchestratorPlanItem(t *testing.T) {
	// Arrange — write an orchestrator file so the probe would detect it on disk.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneOnly()
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec

	// Write the orchestrator file to simulate a locally-present or locally-modified orchestrator.
	writeOrchestratorFile(t, workspace)

	svc := app.New(deps)

	// Act — run with pre-answered selections and skipped model questions to drive headlessly.
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		Scope:              domain.ScopeProject,
		StandaloneAgentIDs: []string{"session-logger"},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v; "+
			"the flow must complete without error when all required inputs are pre-answered "+
			"and SkipAll suppresses model questions", err)
	}

	// Assert — executor must have been called so there is a plan to inspect.
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; DeployStandalone must call the executor with the computed plan; " +
			"without this the plan-content check cannot run")
	}

	// Assert — no orchestrator plan item, regardless of action.
	// If the orchestrator appears here as ActionConflict, ExcludeOrchestrator is not yet wired
	// into the probe-set construction in standalone_service.go.
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "orchestrator" {
			t.Errorf("plan contains an orchestrator item (action=%v, target=%q) in a standalone-only run "+
				"where the orchestrator file is present on disk; "+
				"a standalone-only plan must never include the orchestrator (AC1.3); "+
				"the current code seeds the probe set unconditionally with c.Orchestrator() — "+
				"after the fix, OrchestratorExcludedFor(ModeStandaloneOnly)=true must suppress it from both "+
				"the probe set and the Build artifact set",
				item.Action, item.TargetPath)
		}
	}
}

// TestDeployStandalone_OrchestratorFilePresent_StandaloneAgentIsPlanned verifies that even
// when the orchestrator is excluded from the plan, the selected standalone agent still appears.
// This confirms that the exclusion suppresses only the orchestrator and leaves the standalone
// agent unaffected.
func TestDeployStandalone_OrchestratorFilePresent_StandaloneAgentIsPlanned(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneOnly()
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec

	writeOrchestratorFile(t, workspace)

	svc := app.New(deps)

	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		Scope:              domain.ScopeProject,
		StandaloneAgentIDs: []string{"session-logger"},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v", err)
	}

	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; cannot inspect the plan")
	}

	foundStandalone := false
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "session-logger" {
			foundStandalone = true
		}
	}
	if !foundStandalone {
		t.Error("plan does not contain standalone agent \"session-logger\" even though it was selected; " +
			"orchestrator exclusion must suppress only the orchestrator, not the selected standalone agents")
	}
}

// TestDeployStandalone_NoOrchestratorFile_NoOrchestratorPlanItem verifies the base case:
// even without any orchestrator file on disk, a standalone-only plan contains no orchestrator
// item. This ensures the exclusion is not just a consequence of probe behaviour for an absent
// file (ActionCreate is still a plan item) but is enforced at the artifact-set level.
func TestDeployStandalone_NoOrchestratorFile_NoOrchestratorPlanItem(t *testing.T) {
	// Arrange — no orchestrator file written to workspace (file is absent from disk).
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithStandaloneOnly()
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, err := svc.DeployStandalone(context.Background(), app.StandaloneRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		Scope:              domain.ScopeProject,
		StandaloneAgentIDs: []string{"session-logger"},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployStandalone returned error %v", err)
	}

	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; cannot inspect the plan")
	}

	// Even when the orchestrator file is absent, if the orchestrator is in the artifact set,
	// classifyAgentItem will classify it as ActionCreate — which is still an unwanted item.
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "orchestrator" {
			t.Errorf("plan contains orchestrator item (action=%v) even though the orchestrator file is absent; "+
				"the orchestrator must not appear in any standalone-only plan regardless of on-disk state (AC1.3); "+
				"without ExcludeOrchestrator=true the orchestrator would appear as ActionCreate here",
				item.Action)
		}
	}
}

// ---------------------------------------------------------------------------
// AC1.4 — Other modes still include the orchestrator with an existing orchestrator file
// ---------------------------------------------------------------------------

// TestDeployNew_OrchestratorFilePresent_OrchestratorInPlan verifies that when the
// orchestrator file exists at its workspace path and DeployNew is used (not DeployStandalone),
// the orchestrator still appears in the plan. This pins AC1.4: the exclusion is strictly
// mode-gated and must not affect any non-standalone-only mode.
func TestDeployNew_OrchestratorFilePresent_OrchestratorInPlan(t *testing.T) {
	// Arrange — write an orchestrator file so the planner can detect it.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	// Standard catalog with orchestrator and quick-fix workflow.
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec

	writeOrchestratorFile(t, workspace)

	svc := app.New(deps)

	// Act — use DeployNew with pre-answered selections.
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		Scope:           domain.ScopeProject,
		WorkflowIDs:     []string{"quick-fix"},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew returned error %v; "+
			"the flow must complete without error when all required inputs are pre-answered", err)
	}

	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; cannot inspect the plan")
	}

	// Assert — the orchestrator must appear in the plan for a deploy-new run, even though the
	// file exists on disk (it will be classified as ActionConflict with ManifestMissing=true).
	found := false
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "orchestrator" {
			found = true
		}
	}
	if !found {
		t.Error("orchestrator does not appear in the plan for a DeployNew run with an existing orchestrator file; " +
			"OrchestratorExcludedFor must return false for ModeDeployNew so the orchestrator is always included in " +
			"deploy-new plans (AC1.4); if this fails, the exclusion is incorrectly applied to non-standalone modes")
	}
}
