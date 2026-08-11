package cli_test

// dispatch_test.go verifies that cli.Run correctly dispatches to the appropriate service
// method based on the subcommand, and that the adapter receives its dependencies injected
// rather than constructing them internally.
//
// Verified invariants (T19.7):
//
// Operation dispatch:
//   - The "deploy" subcommand causes Run to call svc.DeployNew exactly once.
//   - The "update" subcommand causes Run to call svc.Update exactly once.
//   - Neither "deploy" nor "update" calls both service methods in a single invocation.
//
// Dependency injection (no internal construction):
//   - Run accepts an already-constructed app.Service and delegates to it; it never calls
//     constructors for catalog, registry, manifest, or any other core package. This is
//     enforced structurally by the Run signature — app.Service is injected, not built.
//   - A stub service that satisfies app.Service is sufficient for Run to operate; no real
//     infrastructure is needed, proving dependencies are external to the adapter.
//
// Single service usage:
//   - The same svc instance is used for the full invocation; svc is not replaced mid-call.

import (
	"bytes"
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Subcommand dispatch
// ---------------------------------------------------------------------------

// TestRun_DeploySubcommand_CallsDeployNew verifies that the "deploy" subcommand dispatches
// to svc.DeployNew and does not call svc.Update.
func TestRun_DeploySubcommand_CallsDeployNew(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Error("svc.DeployNew was not called; deploy subcommand must call svc.DeployNew")
	}
	if svc.updateReq != nil {
		t.Error("svc.Update was called; deploy subcommand must not call svc.Update")
	}
}

// TestRun_UpdateSubcommand_CallsUpdate verifies that the "update" subcommand dispatches
// to svc.Update and does not call svc.DeployNew.
func TestRun_UpdateSubcommand_CallsUpdate(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		updateResp: domain.RunSummary{
			Mode: domain.ModeUpdate, WorkspacePath: workspace,
			Outcome: domain.OutcomeSuccess,
		},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.updateReq == nil {
		t.Error("svc.Update was not called; update subcommand must call svc.Update")
	}
	if svc.deployReq != nil {
		t.Error("svc.DeployNew was called; update subcommand must not call svc.DeployNew")
	}
}

// TestRun_DeploySubcommand_CallsDeployNewExactlyOnce verifies that the "deploy" subcommand
// causes exactly one call to svc.DeployNew. Multiple calls would indicate the adapter is
// re-running the use case internally.
func TestRun_DeploySubcommand_CallsDeployNewExactlyOnce(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	callCount := 0
	svc := &countingService{
		onDeploy: func() domain.RunSummary {
			callCount++
			return successSummary(workspace)
		},
	}

	// Act
	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if callCount != 1 {
		t.Errorf("DeployNew called %d times, want exactly 1; the adapter must not re-invoke the use case", callCount)
	}
}

// TestRun_UpdateSubcommand_CallsUpdateExactlyOnce verifies that the "update" subcommand
// causes exactly one call to svc.Update.
func TestRun_UpdateSubcommand_CallsUpdateExactlyOnce(t *testing.T) {
	// Arrange
	callCount := 0
	svc := &countingService{
		onUpdate: func() domain.RunSummary {
			callCount++
			return domain.RunSummary{Outcome: domain.OutcomeSuccess}
		},
	}

	// Act
	cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", t.TempDir(), "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if callCount != 1 {
		t.Errorf("Update called %d times, want exactly 1", callCount)
	}
}

// ---------------------------------------------------------------------------
// Dependency injection — structural proof
// ---------------------------------------------------------------------------

// TestRun_AcceptsStubService_WithoutRealInfrastructure verifies that Run operates correctly
// when given a stub that satisfies app.Service, with no real catalog, manifest, registry,
// or other infrastructure. This proves that Run delegates entirely to the injected service
// rather than constructing dependencies internally.
func TestRun_AcceptsStubService_WithoutRealInfrastructure(t *testing.T) {
	// Arrange — a pure stub service with no backing files or processes
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "any-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code == cli.ExitUsage {
		t.Fatal("exit code = ExitUsage with a valid stub service; Run must not require real infrastructure")
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called; the stub service must be sufficient for the adapter to operate")
	}
}

// ---------------------------------------------------------------------------
// countingService — counts calls for dispatch assertions
// ---------------------------------------------------------------------------

// countingService implements app.Service and counts method calls via injected callbacks.
type countingService struct {
	onDeploy func() domain.RunSummary
	onUpdate func() domain.RunSummary
}

func (s *countingService) ListHarnesses() []domain.HarnessRef { return nil }

func (s *countingService) DeployNew(ctx context.Context, req app.DeployRequest) (domain.RunSummary, error) {
	if s.onDeploy != nil {
		return s.onDeploy(), nil
	}
	return domain.RunSummary{Outcome: domain.OutcomeSuccess}, nil
}

func (s *countingService) Update(ctx context.Context, req app.UpdateRequest) (domain.RunSummary, error) {
	if s.onUpdate != nil {
		return s.onUpdate(), nil
	}
	return domain.RunSummary{Outcome: domain.OutcomeSuccess}, nil
}

func (s *countingService) UpdateWorkflows(ctx context.Context, req app.WorkflowUpdateRequest) (domain.RunSummary, error) {
	return domain.RunSummary{Outcome: domain.OutcomeSuccess}, nil
}

func (s *countingService) Promote(ctx context.Context, req app.PromoteRequest) (app.PromoteResult, error) {
	return app.PromoteResult{}, nil
}

func (s *countingService) TransformHarness(ctx context.Context, req app.TransformHarnessRequest) (app.TransformHarnessResult, error) {
	return app.TransformHarnessResult{}, nil
}

func (s *countingService) DeployUtilityInfrastructure(ctx context.Context, req app.UtilityInfraRequest) (domain.RunSummary, error) {
	return domain.RunSummary{Outcome: domain.OutcomeSuccess}, nil
}
