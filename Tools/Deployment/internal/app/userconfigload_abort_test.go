package app_test

// userconfigload_abort_test.go verifies that a config.UserConfigStore.Load failure aborts
// the run — for the new-deploy, update, and workflow-update flows — instead of the run
// silently continuing on a zero-value UserConfig.
//
// Two shapes of Load failure are covered, matching config.UserConfigStore.Load's contract:
// a raw parse error (malformed YAML) and a config.MappingDeclError (a `tool_destinations`
// declaration that fails validation). Both must reach the caller unmodified — no layer may
// replace either with a generic message, and no layer may re-wrap a MappingDeclError's
// dotted path into something less specific.
//
// Each flow has (up to) two distinct Load call sites that must independently check the
// error, per ContractsDesign.md's Integration Points:
//   - resolveModels' own Load call (resolve.go:230) — already checked as of Stage 2's
//     implementation of the shared error-return channel.
//   - the tool-mappings-version hash site (deploy.go:184, update.go:215, workflow_update.go:212)
//     — still discarded (`userCfg, _ := s.deps.UserConfig.Load()`) as of this test's writing.
//
// Update and UpdateWorkflows gate their resolveModels call on a non-empty new-agent set, so
// a run with no newly-required agents never calls resolveModels at all: the hash site is the
// *only* Load call site reachable on that run, and is exercised in isolation below.
//
// Verified invariants:
//
// Abort on Load failure (AC5.1):
//   - A raw parse error aborts every flow's run with a non-nil error.
//   - A config.MappingDeclError aborts every flow's run, and its rendered dotted path
//     survives into the error message the caller receives (errors.As matches the value type,
//     per Load returning it by value).
//
// No plan executed, no deployment writes (AC5.1, AC5.2):
//   - Planner.Build is never called.
//   - Executor.Execute is never called.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/config"
)

// sampleMappingDeclError returns a config.MappingDeclError whose rendered form carries a
// dotted path an assertion can look for verbatim.
func sampleMappingDeclError(path string) config.MappingDeclError {
	return config.MappingDeclError{
		File:        path,
		HarnessID:   "claude-code",
		MappingIdx:  0,
		Generic:     "",
		Destination: 1,
		Message:     "destinations[1]: to must be \"main\" or \"field\"",
	}
}

// ---------------------------------------------------------------------------
// DeployNew — resolveModels' own Load call site (already-reachable path)
// ---------------------------------------------------------------------------

// TestDeployNew_UserConfigLoadParseError_AbortsRun verifies that a raw parse error from
// UserConfig.Load aborts DeployNew, propagated unmodified, before any plan is built or
// executed.
func TestDeployNew_UserConfigLoadParseError_AbortsRun(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	parseErr := errors.New("/mosaic/MosaicDeploy/config/user-config.yaml: parse user config: yaml: line 3: did not find expected key")
	spy := &spyUserConfig{loadErr: parseErr}
	planner := &capturingPlanner{result: newMinimalPlan(workspace)}
	executor := &deploySpyExecutor{}
	deps.UserConfig = spy
	deps.Planner = planner
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
	if err == nil {
		t.Fatal("DeployNew returned nil error after UserConfig.Load failed; expected the run to abort")
	}
	if !errors.Is(err, parseErr) {
		t.Errorf("errors.Is(err, parseErr) = false; the Load error must propagate unmodified (AC5.1); got: %v", err)
	}
	if planner.capturedInput != nil {
		t.Error("Planner.Build was called after UserConfig.Load failed; an unusable config must abort before any plan is built (AC5.1)")
	}
	if executor.called {
		t.Error("Executor.Execute was called after UserConfig.Load failed; an unusable config must abort before any deployment write (AC5.2)")
	}
}

// TestDeployNew_UserConfigLoadMappingDeclError_DottedPathSurvives verifies that a
// config.MappingDeclError from UserConfig.Load aborts DeployNew and that its dotted path
// reaches the caller intact rather than being flattened to a generic message.
func TestDeployNew_UserConfigLoadMappingDeclError_DottedPathSurvives(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	declErr := sampleMappingDeclError("/mosaic/MosaicDeploy/config/user-config.yaml")
	spy := &spyUserConfig{loadErr: declErr}
	executor := &deploySpyExecutor{}
	deps.UserConfig = spy
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
	if err == nil {
		t.Fatal("DeployNew returned nil error after UserConfig.Load returned a MappingDeclError; expected the run to abort")
	}
	var got config.MappingDeclError
	if !errors.As(err, &got) {
		t.Fatalf("errors.As(err, *config.MappingDeclError) = false; expected the MappingDeclError to survive unwrapped/rewrapped, got: %v (%T)", err, err)
	}
	wantPath := declErr.Error()
	if !strings.Contains(err.Error(), "tool_destinations.claude-code[0].destinations[1]") {
		t.Errorf("error message %q does not carry the dotted path from %q; "+
			"every layer must propagate a MappingDeclError with %%w or return it unmodified (AC5.1)", err.Error(), wantPath)
	}
	if executor.called {
		t.Error("Executor.Execute was called after UserConfig.Load returned a MappingDeclError; the run must abort before any deployment write (AC5.2)")
	}
}

// ---------------------------------------------------------------------------
// DeployNew — the tool-mappings-version hash site (deploy.go:184), isolated
// ---------------------------------------------------------------------------

// TestDeployNew_UserConfigLoadFails_AtHashSite_AbortsRun verifies that deploy.go's own
// Load call, made to compute the tool-mappings-version hash, is itself checked — not only
// resolveModels' earlier call. The double succeeds on its first Load call (the one
// resolveModels makes) and fails from the second call onward (the hash site), isolating the
// hash site's own check from resolveModels'.
func TestDeployNew_UserConfigLoadFails_AtHashSite_AbortsRun(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	parseErr := errors.New("/mosaic/MosaicDeploy/config/user-config.yaml: parse user config: yaml: unexpected token")
	spy := &spyUserConfig{loadErr: parseErr, failLoadFrom: 2}
	planner := &capturingPlanner{result: newMinimalPlan(workspace)}
	executor := &deploySpyExecutor{}
	deps.UserConfig = spy
	deps.Planner = planner
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
	if err == nil {
		t.Fatal("DeployNew returned nil error after the hash-site UserConfig.Load call failed; expected the run to abort")
	}
	if !errors.Is(err, parseErr) {
		t.Errorf("errors.Is(err, parseErr) = false; got: %v", err)
	}
	if planner.capturedInput != nil {
		t.Error("Planner.Build was called after the hash-site UserConfig.Load call failed; " +
			"no run may compute or stamp tool_mappings_version from a config whose load failed (AC5.2), " +
			"and no plan may be built past that point (AC5.1)")
	}
	if executor.called {
		t.Error("Executor.Execute was called after the hash-site UserConfig.Load call failed (AC5.2)")
	}
}

// ---------------------------------------------------------------------------
// Update — no new agents, so the hash site is the only reachable Load call
// ---------------------------------------------------------------------------

// TestUpdate_UserConfigLoadFails_NoNewAgents_HashSiteAborts verifies update.go:215's own
// Load call is checked, in a run where every workflow-required agent is already deployed:
// resolveModels is never called (it is gated on len(newAgents) > 0), so the hash site is the
// sole Load call site this run reaches.
func TestUpdate_UserConfigLoadFails_NoNewAgents_HashSiteAborts(t *testing.T) {
	// Arrange — write both the orchestrator and "test-runner" as already deployed so
	// newAgents is empty and resolveModels is never called.
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	writeTempFile(t, workspace, "orchestrator.md", []byte("---\nversion: \"1.0\"\n---\nOrchestrator body.\n"))
	writeTempFile(t, workspace, "test-runner.md", []byte("---\nversion: \"1.0\"\n---\nAgent body.\n"))

	parseErr := errors.New("/mosaic/MosaicDeploy/config/user-config.yaml: parse user config: yaml: unexpected token")
	spy := &spyUserConfig{loadErr: parseErr}
	planner := &capturingPlanner{result: newMinimalPlan(workspace)}
	executor := &deploySpyExecutor{}
	deps.UserConfig = spy
	deps.Planner = planner
	deps.Executor = executor
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err == nil {
		t.Fatal("Update returned nil error after the hash-site UserConfig.Load call failed; expected the run to abort")
	}
	if !errors.Is(err, parseErr) {
		t.Errorf("errors.Is(err, parseErr) = false; got: %v", err)
	}
	if planner.capturedInput != nil {
		t.Error("Planner.Build was called after Update's hash-site UserConfig.Load call failed (AC5.1, AC5.2)")
	}
	if executor.called {
		t.Error("Executor.Execute was called after Update's hash-site UserConfig.Load call failed (AC5.2)")
	}
}

// TestUpdate_UserConfigLoadFails_NewAgent_AbortsViaResolveModels verifies that, on a run with
// a newly-required agent, the failure is caught at resolveModels' own Load call (the first
// Load call this run makes) and the run aborts identically.
func TestUpdate_UserConfigLoadFails_NewAgent_AbortsViaResolveModels(t *testing.T) {
	// Arrange — "test-runner.md" is NOT written, so it is a newly-required agent and
	// resolveModels is called.
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	writeTempFile(t, workspace, "orchestrator.md", []byte("---\nversion: \"1.0\"\n---\nOrchestrator body.\n"))

	parseErr := errors.New("/mosaic/MosaicDeploy/config/user-config.yaml: parse user config: yaml: unexpected token")
	spy := &spyUserConfig{loadErr: parseErr}
	executor := &deploySpyExecutor{}
	deps.UserConfig = spy
	deps.Executor = executor
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err == nil {
		t.Fatal("Update returned nil error after UserConfig.Load failed for a newly-required agent; expected the run to abort")
	}
	if !errors.Is(err, parseErr) {
		t.Errorf("errors.Is(err, parseErr) = false; got: %v", err)
	}
	if executor.called {
		t.Error("Executor.Execute was called after UserConfig.Load failed (AC5.2)")
	}
}

// ---------------------------------------------------------------------------
// UpdateWorkflows — no new agents, so the hash site is the only reachable Load call
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_UserConfigLoadFails_NoNewAgents_HashSiteAborts verifies
// workflow_update.go:212's own Load call is checked, in a run where every workflow-required
// agent is already deployed and resolveModels is never called.
func TestUpdateWorkflows_UserConfigLoadFails_NoNewAgents_HashSiteAborts(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	writeTempFile(t, workspace, "orchestrator.md", []byte("---\nversion: \"1.0\"\n---\nOrchestrator body.\n"))
	writeTempFile(t, workspace, "test-runner.md", []byte("---\nversion: \"1.0\"\n---\nAgent body.\n"))

	parseErr := errors.New("/mosaic/MosaicDeploy/config/user-config.yaml: parse user config: yaml: unexpected token")
	spy := &spyUserConfig{loadErr: parseErr}
	planner := &capturingPlanner{result: newMinimalPlan(workspace)}
	executor := &deploySpyExecutor{}
	deps.UserConfig = spy
	deps.Planner = planner
	deps.Executor = executor
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err == nil {
		t.Fatal("UpdateWorkflows returned nil error after the hash-site UserConfig.Load call failed; expected the run to abort")
	}
	if !errors.Is(err, parseErr) {
		t.Errorf("errors.Is(err, parseErr) = false; got: %v", err)
	}
	if planner.capturedInput != nil {
		t.Error("Planner.Build was called after UpdateWorkflows' hash-site UserConfig.Load call failed (AC5.1, AC5.2)")
	}
	if executor.called {
		t.Error("Executor.Execute was called after UpdateWorkflows' hash-site UserConfig.Load call failed (AC5.2)")
	}
}

// TestUpdateWorkflows_UserConfigLoadFails_NewAgent_AbortsViaResolveModels verifies that, on a
// run with a newly-required agent, the failure is caught at resolveModels' own Load call and
// the run aborts before resolveCustomTools is reached (Integration Points: "a cancelled run
// asks no further questions of any kind, not just no further model questions" — the same
// ordering requirement applies to this abort).
func TestUpdateWorkflows_UserConfigLoadFails_NewAgent_AbortsViaResolveModels(t *testing.T) {
	// Arrange — no test-runner.md written, so it is newly-required.
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}

	parseErr := errors.New("/mosaic/MosaicDeploy/config/user-config.yaml: parse user config: yaml: unexpected token")
	spy := &spyUserConfig{loadErr: parseErr}
	executor := &deploySpyExecutor{}
	deps.UserConfig = spy
	deps.Executor = executor
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err == nil {
		t.Fatal("UpdateWorkflows returned nil error after UserConfig.Load failed for a newly-required agent; expected the run to abort")
	}
	if !errors.Is(err, parseErr) {
		t.Errorf("errors.Is(err, parseErr) = false; got: %v", err)
	}
	if executor.called {
		t.Error("Executor.Execute was called after UserConfig.Load failed (AC5.2)")
	}
}

