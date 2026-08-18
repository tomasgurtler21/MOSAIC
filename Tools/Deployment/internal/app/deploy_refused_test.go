package app_test

// deploy_refused_test.go verifies that a deployment attempted against a catalogue carrying
// no orchestrator definition is refused before any workspace files are written.
//
// The refusal originates in plan.ResolveArtifactsFrom, propagates through plan.Build, and
// surfaces as a non-zero exit (error return) from the service layer. The executor must
// never be invoked for a refused deployment, so no workspace writes can occur.
//
// Covered behaviors (T2.4):
//
//   - DeployNew returns an error wrapping plan.ErrMissingOrchestrator when the catalogue
//     has no orchestrator agent definition and the mode requires one.
//   - The deployment executor is not invoked for a refused deployment.
//   - The workspace filesystem state is unchanged after a refused deployment.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Filesystem capture helpers (for no-write verification)
// ---------------------------------------------------------------------------

// refusedFileState records the modification time and size of one file for the no-write check.
type refusedFileState struct {
	modTime time.Time
	size    int64
	isDir   bool
}

// captureFS walks dir and returns a map from relative path to refusedFileState.
// Used to detect file system writes by comparing before/after snapshots.
func captureFS(t *testing.T, dir string) map[string]refusedFileState {
	t.Helper()
	state := make(map[string]refusedFileState)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return relErr
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		state[rel] = refusedFileState{
			modTime: info.ModTime(),
			size:    info.Size(),
			isDir:   d.IsDir(),
		}
		return nil
	})
	if err != nil {
		t.Fatalf("captureFS(%q): %v", dir, err)
	}
	return state
}

// fsChanged reports whether the two file system states differ, and describes the first
// difference found. Returns false (no change) if the states are identical.
func fsChanged(before, after map[string]refusedFileState) (changed bool, description string) {
	for path, bSt := range before {
		aSt, ok := after[path]
		if !ok {
			return true, fmt.Sprintf("file removed: %s", path)
		}
		if !bSt.isDir && (bSt.size != aSt.size || !bSt.modTime.Equal(aSt.modTime)) {
			return true, fmt.Sprintf("file modified: %s (size %d→%d)", path, bSt.size, aSt.size)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			return true, fmt.Sprintf("file created: %s", path)
		}
	}
	return false, ""
}


// ---------------------------------------------------------------------------
// spyExecutor — records whether Execute was called; used in refused-deployment tests
// ---------------------------------------------------------------------------

// spyExecutor is an executor spy that records whether Execute was called. It returns an
// empty ExecResult with no error by default.
type spyExecutor struct {
	called bool
}

func (e *spyExecutor) Execute(_ context.Context, _ deploy.ExecRequest) (deploy.ExecResult, error) {
	e.called = true
	return deploy.ExecResult{}, nil
}

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// noOrchestratorCatalog returns a stubCatalog with no orchestrator agent defined.
// The orchestrator field is left at its zero value (domain.Agent{} with Key == "").
func noOrchestratorCatalog(root string) *stubCatalog {
	return &stubCatalog{
		root:      root,
		agents:    []domain.Agent{},
		workflows: []domain.Workflow{},
		// orchestrator: zero value — this is the invariant being tested.
	}
}

// ---------------------------------------------------------------------------
// T2.4 — Command-level refusal for orchestrator-less catalogue
// ---------------------------------------------------------------------------

// TestDeployNew_OrchestratorlessCatalogue_ReturnsErrMissingOrchestrator verifies that
// DeployNew returns an error wrapping plan.ErrMissingOrchestrator when the catalogue has
// no orchestrator agent definition. This corresponds to a non-zero process exit at the
// command layer. The refusal must be raised during plan computation, before any workspace
// file is written.
func TestDeployNew_OrchestratorlessCatalogue_ReturnsErrMissingOrchestrator(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	spy := &spyExecutor{}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)

	// Replace catalog with one that has no orchestrator definition.
	deps.Catalog = noOrchestratorCatalog(workspace)
	// Use the real planner so the refusal is raised from ResolveArtifactsFrom, not a stub.
	deps.Planner = plan.New()
	deps.Executor = spy

	svc := app.New(deps)

	// Pre-answer all interaction fields to reach the planner without interaction prompts.
	req := app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{},
		UtilityAgentIDs: []string{},
		HookIDs:         []string{},
		AutoConfirmPlan: true,
		// Pre-answer the model for the empty-key orchestrator to avoid interaction in
		// the RED phase, where ResolveArtifacts succeeds and model resolution is reached.
		AgentModels: map[string]string{"": "model-a"},
	}

	// Act
	_, err := svc.DeployNew(context.Background(), req)

	// Assert
	if err == nil {
		t.Fatal("DeployNew with orchestrator-less catalogue: expected error, got nil; " +
			"a deployment must be refused and report a non-zero exit when no orchestrator is defined — " +
			"proceeding to execution would produce a deployment action with an empty agent key")
	}
	if !errors.Is(err, plan.ErrMissingOrchestrator) {
		t.Errorf("DeployNew returned error %v; want errors.Is(err, plan.ErrMissingOrchestrator); " +
			"the error must wrap ErrMissingOrchestrator so the CLI can report a meaningful message "+
			"naming what is missing and where it was sought",
			err)
	}
}

// TestDeployNew_OrchestratorlessCatalogue_ExecutorNotCalled verifies that the deployment
// executor is never invoked when the catalogue has no orchestrator. An invoked executor
// could write workspace files; the refusal must prevent that.
func TestDeployNew_OrchestratorlessCatalogue_ExecutorNotCalled(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	spy := &spyExecutor{}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)

	deps.Catalog = noOrchestratorCatalog(workspace)
	deps.Planner = plan.New()
	deps.Executor = spy

	svc := app.New(deps)

	req := app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{},
		UtilityAgentIDs: []string{},
		HookIDs:         []string{},
		AutoConfirmPlan: true,
		AgentModels:     map[string]string{"": "model-a"},
	}

	// Act
	svc.DeployNew(context.Background(), req) //nolint:errcheck

	// Assert
	if spy.called {
		t.Error("deployment executor was called for an orchestrator-less catalogue; " +
			"no workspace files must be written before the refusal — " +
			"the executor must never be reached when plan.ResolveArtifactsFrom returns ErrMissingOrchestrator")
	}
}

// TestDeployNew_OrchestratorlessCatalogue_NoWorkspaceWrites verifies that no files are
// created or modified in the workspace directory when the deployment is refused. This is
// the filesystem-level assertion for the no-writes requirement.
func TestDeployNew_OrchestratorlessCatalogue_NoWorkspaceWrites(t *testing.T) {
	// Arrange — capture workspace state before the refused deployment.
	workspace := t.TempDir()
	before := captureFS(t, workspace)

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)

	deps.Catalog = noOrchestratorCatalog(workspace)
	deps.Planner = plan.New()
	deps.Executor = &spyExecutor{}

	svc := app.New(deps)

	req := app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{},
		UtilityAgentIDs: []string{},
		HookIDs:         []string{},
		AutoConfirmPlan: true,
		AgentModels:     map[string]string{"": "model-a"},
	}

	// Act
	svc.DeployNew(context.Background(), req) //nolint:errcheck

	// Assert
	after := captureFS(t, workspace)
	if changed, desc := fsChanged(before, after); changed {
		t.Errorf("workspace was modified after a refused deployment (%s); "+
			"no files must be written when the deployment is refused for a missing orchestrator",
			desc)
	}
}
