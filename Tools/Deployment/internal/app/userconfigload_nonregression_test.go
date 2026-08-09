package app_test

// userconfigload_nonregression_test.go pins the two paths Stage 5's abort behavior must
// leave untouched, exercised through the real config.userConfigStore (not the in-memory
// spyUserConfig double used elsewhere), so the wiring between app.Deps.UserConfig and the
// on-disk file is itself covered, not only the app-layer error-handling logic.
//
// Verified invariants:
//
// Absent config (AC5.4):
//   - When MosaicDeploy/config/user-config.yaml does not exist, DeployNew proceeds to
//     execution with no error — the ordinary first-run path is unaffected by every abort
//     path Stage 5 adds elsewhere.
//
// Well-formed config (AC5.5):
//   - A persisted tier mapping in user-config.yaml still pre-answers QTierModel — no
//     QTierModel question is asked for a tier the file already maps.
//   - A tool_destinations declaration in user-config.yaml still contributes to the
//     ToolMappingsVersion stamp passed to the planner, exactly as it does today.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// realUserConfigRoot creates a temporary MOSAIC-root-shaped directory (with the
// MosaicDeploy/config subdirectory config.NewUserConfigStore expects) and returns its path.
func realUserConfigRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "MosaicDeploy", "config"), 0o755); err != nil {
		t.Fatalf("realUserConfigRoot: %v", err)
	}
	return root
}

// TestDeployNew_RealUserConfigStore_AbsentFile_ProceedsWithoutError verifies that DeployNew,
// wired to the real config.userConfigStore, still completes successfully when
// user-config.yaml does not exist on disk (AC5.4, non-regression).
func TestDeployNew_RealUserConfigStore_AbsentFile_ProceedsWithoutError(t *testing.T) {
	// Arrange — a MOSAIC root with no user-config.yaml written.
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	configRoot := realUserConfigRoot(t)
	deps.UserConfig = config.NewUserConfigStore(configRoot)
	executor := &deploySpyExecutor{}
	deps.Executor = executor
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err != nil {
		t.Fatalf("DeployNew returned an error with an absent user-config.yaml: %v; "+
			"an absent file is the ordinary first-run path and must not be treated as an error (AC5.4)", err)
	}
	if !executor.called {
		t.Error("Executor.Execute was not called with an absent user-config.yaml; " +
			"the first-run flow must proceed to execution unchanged (AC5.4)")
	}
}

// TestDeployNew_RealUserConfigStore_WellFormedConfig_SuppliesTierPreAnswer verifies that a
// persisted tier mapping in a well-formed user-config.yaml still pre-answers QTierModel, so
// the question is not asked (AC5.5, non-regression).
func TestDeployNew_RealUserConfigStore_WellFormedConfig_SuppliesTierPreAnswer(t *testing.T) {
	// Arrange — a well-formed user-config.yaml with a HIGH tier mapping for stub-harness.
	configRoot := realUserConfigRoot(t)
	content := []byte("schema_version: \"1\"\n" +
		"tier_models:\n" +
		"  stub-harness:\n" +
		"    HIGH: model-b\n")
	if err := os.WriteFile(filepath.Join(configRoot, "MosaicDeploy", "config", "user-config.yaml"), content, 0o644); err != nil {
		t.Fatalf("write user-config.yaml: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.UserConfig = config.NewUserConfigStore(configRoot)
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

	// Assert
	if stub.WasAsked(domain.QTierModel, "HIGH") {
		t.Error("QTierModel was asked for tier \"HIGH\" despite a persisted mapping in user-config.yaml; " +
			"a well-formed config must still supply pre-answers, unchanged by Stage 5 (AC5.5)")
	}
}

// TestDeployNew_RealUserConfigStore_ToolDestinations_ContributeToStamp verifies that a
// tool_destinations declaration in a well-formed user-config.yaml still contributes to the
// ToolMappingsVersion passed to the planner (AC5.5, non-regression).
func TestDeployNew_RealUserConfigStore_ToolDestinations_ContributeToStamp(t *testing.T) {
	// Arrange — a well-formed user-config.yaml declaring one tool_destinations mapping.
	configRoot := realUserConfigRoot(t)
	content := []byte("schema_version: \"1\"\n" +
		"tool_destinations:\n" +
		"  stub-harness:\n" +
		"    - generic: some_tool\n" +
		"      destinations:\n" +
		"        - to: main\n" +
		"          names: [\"SomeTool\"]\n")
	if err := os.WriteFile(filepath.Join(configRoot, "MosaicDeploy", "config", "user-config.yaml"), content, 0o644); err != nil {
		t.Fatalf("write user-config.yaml: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.UserConfig = config.NewUserConfigStore(configRoot)
	planner := &capturingPlanner{result: newMinimalPlan(workspace)}
	deps.Planner = planner
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

	// Assert
	if planner.capturedInput == nil {
		t.Fatal("Planner.Build was never called; cannot verify ToolMappingsVersion")
	}
	if planner.capturedInput.ToolMappingsVersion == "" {
		t.Error("plan.Input.ToolMappingsVersion is empty despite a tool_destinations declaration in " +
			"user-config.yaml; a well-formed config must still contribute to the stamp (AC5.5)")
	}
}
