package cli_test

// commands_test.go verifies command and flag parsing for the deploy and update subcommands,
// and asserts that global flags are recognised and passed through correctly.
//
// Verified invariants (T19.1, T19.8):
//
// Deploy subcommand flag mapping (T19.1):
//   - --harness maps to DeployRequest.HarnessID
//   - --workspace maps to DeployRequest.WorkspacePath
//   - --dry-run maps to DeployRequest.DryRun
//   - --auto-confirm maps to DeployRequest.AutoConfirmPlan
//   - --output json is accepted without error
//   - --scope is not a recognised flag; passing it returns ExitUsage (RED: currently accepted)
//
// Update subcommand flag mapping (T19.1):
//   - update --harness maps to UpdateRequest.HarnessID
//   - update --workspace maps to UpdateRequest.WorkspacePath
//   - update --auto-confirm maps to UpdateRequest.AutoConfirmPlan
//   - update --dry-run maps to UpdateRequest.DryRun
//
// Usage errors (T19.1):
//   - An unknown subcommand returns ExitUsage
//   - An unknown flag on a known subcommand returns ExitUsage
//
// Global flags (T19.8):
//   - --verbose is recognised on the root command and inherited by subcommands
//   - --mosaic-root is recognised on the root command and inherited by subcommands
//   - --allow-external is recognised on the root command and inherited by subcommands

import (
	"bytes"
	"context"
	"testing"

	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Deploy subcommand — harness flag (T19.1)
// ---------------------------------------------------------------------------

// TestRun_DeploySubcommand_HarnessFlag_PopulatesRequest verifies that --harness on the deploy
// subcommand populates DeployRequest.HarnessID so the app is not asked via Interaction.
func TestRun_DeploySubcommand_HarnessFlag_PopulatesRequest(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		deployResp: successSummary(workspace),
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "claude-code", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess)", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.HarnessID != "claude-code" {
		t.Errorf("HarnessID = %q, want %q", svc.deployReq.HarnessID, "claude-code")
	}
}

// TestRun_DeploySubcommand_WorkspaceFlag_PopulatesRequest verifies that --workspace on the
// deploy subcommand populates DeployRequest.WorkspacePath.
func TestRun_DeploySubcommand_WorkspaceFlag_PopulatesRequest(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		deployResp: successSummary(workspace),
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q", svc.deployReq.WorkspacePath, workspace)
	}
}

// TestRun_DeploySubcommand_ScopeProject_IsDefault verifies that when --scope is absent,
// DeployRequest.Scope defaults to ScopeProject.
func TestRun_DeploySubcommand_ScopeProject_IsDefault(t *testing.T) {
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
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.Scope != domain.ScopeProject {
		t.Errorf("Scope = %q, want %q (ScopeProject is the default)", svc.deployReq.Scope, domain.ScopeProject)
	}
}

// TestRun_DeploySubcommand_ScopeFlag_ReturnsExitUsage verifies that passing --scope on the
// deploy subcommand returns ExitUsage. MOSAIC deploys into the project workspace only; the
// --scope flag was removed when user-scope deployment support was dropped.
//
// RED: currently the flag is still registered and this test fails. It will pass after I1.5
// removes the --scope flag from the deploy subcommand.
func TestRun_DeploySubcommand_ScopeFlag_ReturnsExitUsage(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--scope", "project", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d (ExitUsage); --scope is not a recognised flag after user-scope removal", code, cli.ExitUsage)
	}
}

// TestRun_DeploySubcommand_DryRun_MapsToRequest verifies that --dry-run populates
// DeployRequest.DryRun.
func TestRun_DeploySubcommand_DryRun_MapsToRequest(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--dry-run", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if !svc.deployReq.DryRun {
		t.Error("DryRun = false, want true; --dry-run flag must set DeployRequest.DryRun")
	}
}

// TestRun_DeploySubcommand_AutoConfirm_MapsToRequest verifies that --auto-confirm sets
// DeployRequest.AutoConfirmPlan, skipping the plan review step.
func TestRun_DeploySubcommand_AutoConfirm_MapsToRequest(t *testing.T) {
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
		t.Fatal("DeployNew was not called")
	}
	if !svc.deployReq.AutoConfirmPlan {
		t.Error("AutoConfirmPlan = false, want true")
	}
}

// ---------------------------------------------------------------------------
// Update subcommand — flag mapping (T19.1)
// ---------------------------------------------------------------------------

// TestRun_UpdateSubcommand_HarnessFlag_PopulatesRequest verifies that update --harness
// populates UpdateRequest.HarnessID.
func TestRun_UpdateSubcommand_HarnessFlag_PopulatesRequest(t *testing.T) {
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
		[]string{"update", "--harness", "claude-code", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.updateReq == nil {
		t.Fatal("Update was not called; expected update subcommand to call svc.Update")
	}
	if svc.updateReq.HarnessID != "claude-code" {
		t.Errorf("HarnessID = %q, want %q", svc.updateReq.HarnessID, "claude-code")
	}
}

// TestRun_UpdateSubcommand_WorkspaceFlag_PopulatesRequest verifies update --workspace
// populates UpdateRequest.WorkspacePath.
func TestRun_UpdateSubcommand_WorkspaceFlag_PopulatesRequest(t *testing.T) {
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
		t.Fatal("Update was not called")
	}
	if svc.updateReq.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q", svc.updateReq.WorkspacePath, workspace)
	}
}

// TestRun_UpdateSubcommand_AutoConfirm_MapsToRequest verifies update --auto-confirm sets
// UpdateRequest.AutoConfirmPlan.
func TestRun_UpdateSubcommand_AutoConfirm_MapsToRequest(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		updateResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
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
		t.Fatal("Update was not called")
	}
	if !svc.updateReq.AutoConfirmPlan {
		t.Error("AutoConfirmPlan = false, want true")
	}
}

// TestRun_UpdateSubcommand_DryRun_MapsToRequest verifies update --dry-run sets
// UpdateRequest.DryRun.
func TestRun_UpdateSubcommand_DryRun_MapsToRequest(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		updateResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", workspace, "--dry-run", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.updateReq == nil {
		t.Fatal("Update was not called")
	}
	if !svc.updateReq.DryRun {
		t.Error("DryRun = false, want true")
	}
}

// ---------------------------------------------------------------------------
// Usage errors (T19.1)
// ---------------------------------------------------------------------------

// TestRun_UnknownSubcommand_ReturnsExitUsage verifies that an unrecognised subcommand
// name returns ExitUsage without calling any service method.
func TestRun_UnknownSubcommand_ReturnsExitUsage(t *testing.T) {
	// Arrange
	svc := &spyService{}

	// Act
	code := cli.Run(context.Background(),
		[]string{"explode"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d (ExitUsage) for unknown subcommand", code, cli.ExitUsage)
	}
	if svc.deployReq != nil || svc.updateReq != nil {
		t.Error("service method was called for an unknown subcommand; expected no call")
	}
}

// TestRun_UnknownFlag_ReturnsExitUsage verifies that an unrecognised flag on a known
// subcommand returns ExitUsage.
func TestRun_UnknownFlag_ReturnsExitUsage(t *testing.T) {
	// Arrange
	svc := &spyService{}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--does-not-exist"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d (ExitUsage) for unknown flag", code, cli.ExitUsage)
	}
}

// TestRun_NoSubcommand_ReturnsExitUsage verifies that calling Run with no subcommand
// returns ExitUsage (the TUI is selected by the entry point, not by cli.Run).
func TestRun_NoSubcommand_ReturnsExitUsage(t *testing.T) {
	// Arrange
	svc := &spyService{}

	// Act
	code := cli.Run(context.Background(),
		[]string{},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d (ExitUsage) when no subcommand is given", code, cli.ExitUsage)
	}
}

// ---------------------------------------------------------------------------
// Global flags (T19.8)
// ---------------------------------------------------------------------------

// TestRun_VerboseFlag_IsRecognised verifies that --verbose on a subcommand does not
// produce a usage error; it is a valid global flag.
func TestRun_VerboseFlag_IsRecognised(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--verbose", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert: must not be a usage error
	if code == cli.ExitUsage {
		t.Errorf("exit code = ExitUsage; --verbose must be a recognised global flag, not an unknown flag")
	}
}

// TestRun_MosaicRootFlag_IsRecognised verifies that --mosaic-root is a recognised global
// flag and does not produce a usage error.
func TestRun_MosaicRootFlag_IsRecognised(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	root := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--mosaic-root", root, "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code == cli.ExitUsage {
		t.Errorf("exit code = ExitUsage; --mosaic-root must be a recognised global flag")
	}
}

// TestRun_MosaicRootFlag_ServiceIsCalled verifies that providing --mosaic-root with a
// non-default path does not prevent the service from being called. A flag implementation
// that silently drops --mosaic-root (e.g. due to shadowing or parse order) might still call
// the service without the flag value; this test catches the case where the flag causes the
// service call to be dropped entirely.
func TestRun_MosaicRootFlag_ServiceIsCalled(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	root := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--mosaic-root", root, "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; --mosaic-root must not cause the run to fail", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Error("DeployNew was not called; --mosaic-root must not prevent the service from being invoked")
	}
}

// TestRun_AllowExternalFlag_IsRecognised verifies that --allow-external is a recognised
// global flag that does not produce a usage error.
func TestRun_AllowExternalFlag_IsRecognised(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--allow-external", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code == cli.ExitUsage {
		t.Errorf("exit code = ExitUsage; --allow-external must be a recognised global flag")
	}
}

// TestRun_AllowExternalFlag_Inherited_ByUpdateSubcommand verifies that --allow-external
// is also recognised when placed before the update subcommand.
func TestRun_AllowExternalFlag_Inherited_ByUpdateSubcommand(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		updateResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"--allow-external", "update", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code == cli.ExitUsage {
		t.Errorf("exit code = ExitUsage; --allow-external must be inherited by the update subcommand")
	}
}

// TestRun_OutputFlag_Json_IsRecognised verifies that --output json is accepted on the deploy
// subcommand without producing a usage error.
func TestRun_OutputFlag_Json_IsRecognised(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--output", "json", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert: any non-usage exit is acceptable
	if code == cli.ExitUsage {
		t.Errorf("exit code = ExitUsage; --output json must be a valid flag value")
	}
}
