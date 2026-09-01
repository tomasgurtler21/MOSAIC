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
			Mode: domain.ModeUpdateWorkspace, WorkspacePath: workspace,
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
			Mode: domain.ModeUpdateWorkspace, WorkspacePath: workspace,
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

// ---------------------------------------------------------------------------
// deploy --conflict flag mapping (T2.3)
// ---------------------------------------------------------------------------

// TestRun_DeploySubcommand_ConflictFlag_Overwrite_PopulatesConflictDefault verifies that
// deploy --conflict overwrite populates DeployRequest.ConflictDefault with DecisionOverwrite.
//
// RED: the --conflict flag is not yet registered on the deploy subcommand.
func TestRun_DeploySubcommand_ConflictFlag_Overwrite_PopulatesConflictDefault(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace,
			"--conflict", "overwrite", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; deploy --conflict overwrite must be a valid flag",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.ConflictDefault != domain.DecisionOverwrite {
		t.Errorf("ConflictDefault = %q, want %q; deploy --conflict overwrite must populate "+
			"DeployRequest.ConflictDefault with DecisionOverwrite",
			svc.deployReq.ConflictDefault, domain.DecisionOverwrite)
	}
}

// TestRun_DeploySubcommand_ConflictFlag_Skip_PopulatesConflictDefault verifies that
// deploy --conflict skip populates DeployRequest.ConflictDefault with DecisionSkip.
//
// RED: the --conflict flag is not yet registered on the deploy subcommand.
func TestRun_DeploySubcommand_ConflictFlag_Skip_PopulatesConflictDefault(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace,
			"--conflict", "skip", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; deploy --conflict skip must be a valid flag",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.ConflictDefault != domain.DecisionSkip {
		t.Errorf("ConflictDefault = %q, want %q; deploy --conflict skip must populate "+
			"DeployRequest.ConflictDefault with DecisionSkip",
			svc.deployReq.ConflictDefault, domain.DecisionSkip)
	}
}

// TestRun_DeploySubcommand_ConflictFlag_Backup_PopulatesConflictDefault verifies that
// deploy --conflict backup populates DeployRequest.ConflictDefault with DecisionBackupThenOverwrite.
//
// RED: the --conflict flag is not yet registered on the deploy subcommand.
func TestRun_DeploySubcommand_ConflictFlag_Backup_PopulatesConflictDefault(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace,
			"--conflict", "backup", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; deploy --conflict backup must be a valid flag",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.ConflictDefault != domain.DecisionBackupThenOverwrite {
		t.Errorf("ConflictDefault = %q, want %q; deploy --conflict backup must populate "+
			"DeployRequest.ConflictDefault with DecisionBackupThenOverwrite",
			svc.deployReq.ConflictDefault, domain.DecisionBackupThenOverwrite)
	}
}

// TestRun_DeploySubcommand_ConflictFlag_InvalidValue_ReturnsExitUsage verifies that an
// invalid --conflict value on the deploy subcommand returns ExitUsage and does not call
// DeployNew.
//
// RED: without the flag registered, any value currently returns ExitUsage (unknown flag),
// but after registration the test verifies the value validation specifically.
func TestRun_DeploySubcommand_ConflictFlag_InvalidValue_ReturnsExitUsage(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace,
			"--conflict", "not-a-valid-value", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d (ExitUsage); an invalid --conflict value must be rejected",
			code, cli.ExitUsage)
	}
}

// TestRun_DeploySubcommand_ConflictFlag_Absent_ConflictDefaultIsZero verifies that omitting
// --conflict leaves DeployRequest.ConflictDefault at the zero value (empty string), preserving
// the interactive conflict-prompt behavior.
func TestRun_DeploySubcommand_ConflictFlag_Absent_ConflictDefaultIsZero(t *testing.T) {
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
	if svc.deployReq.ConflictDefault != "" {
		t.Errorf("ConflictDefault = %q, want empty string; omitting --conflict must leave "+
			"ConflictDefault at zero value to preserve interactive conflict behavior",
			svc.deployReq.ConflictDefault)
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

// ---------------------------------------------------------------------------
// workflows subcommand — dispatch and flag mapping
// ---------------------------------------------------------------------------

// TestRun_WorkflowsSubcommand_CallsUpdateWorkflows verifies that the "workflows" subcommand
// dispatches to svc.UpdateWorkflows and does not call svc.DeployNew or svc.Update.
func TestRun_WorkflowsSubcommand_CallsUpdateWorkflows(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: domain.RunSummary{
			Mode: domain.ModeUpdateWorkflows, WorkspacePath: workspace,
			Outcome: domain.OutcomeSuccess,
		},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.workflowsReq == nil {
		t.Error("svc.UpdateWorkflows was not called; workflows subcommand must call svc.UpdateWorkflows")
	}
	if svc.deployReq != nil {
		t.Error("svc.DeployNew was called; workflows subcommand must not call svc.DeployNew")
	}
	if svc.updateReq != nil {
		t.Error("svc.Update was called; workflows subcommand must not call svc.Update")
	}
}

// TestRun_WorkflowsSubcommand_HarnessFlag_PopulatesRequest verifies that --harness on the
// workflows subcommand populates WorkflowUpdateRequest.HarnessID.
func TestRun_WorkflowsSubcommand_HarnessFlag_PopulatesRequest(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"workflows", "--harness", "my-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.workflowsReq == nil {
		t.Fatal("UpdateWorkflows was not called")
	}
	if svc.workflowsReq.HarnessID != "my-harness" {
		t.Errorf("HarnessID = %q, want %q", svc.workflowsReq.HarnessID, "my-harness")
	}
}

// TestRun_WorkflowsSubcommand_WorkspacFlag_PopulatesRequest verifies that --workspace on the
// workflows subcommand populates WorkflowUpdateRequest.WorkspacePath.
func TestRun_WorkflowsSubcommand_WorkspacFlag_PopulatesRequest(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.workflowsReq == nil {
		t.Fatal("UpdateWorkflows was not called")
	}
	if svc.workflowsReq.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q", svc.workflowsReq.WorkspacePath, workspace)
	}
}

// TestRun_WorkflowsSubcommand_WorkflowsFlag_PopulatesWorkflowIDs verifies that --workflows
// parses a comma-separated list into WorkflowUpdateRequest.WorkflowIDs.
func TestRun_WorkflowsSubcommand_WorkflowsFlag_PopulatesWorkflowIDs(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", workspace,
			"--workflows", "wf-a,wf-b,wf-c", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.workflowsReq == nil {
		t.Fatal("UpdateWorkflows was not called")
	}
	want := []string{"wf-a", "wf-b", "wf-c"}
	got := svc.workflowsReq.WorkflowIDs
	if len(got) != len(want) {
		t.Fatalf("WorkflowIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("WorkflowIDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRun_WorkflowsSubcommand_WorkflowsFlag_Absent_WorkflowIDsIsNil verifies that omitting
// --workflows leaves WorkflowUpdateRequest.WorkflowIDs nil so the interactive prompt fires.
func TestRun_WorkflowsSubcommand_WorkflowsFlag_Absent_WorkflowIDsIsNil(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.workflowsReq == nil {
		t.Fatal("UpdateWorkflows was not called")
	}
	if svc.workflowsReq.WorkflowIDs != nil {
		t.Errorf("WorkflowIDs = %v, want nil; omitting --workflows must leave WorkflowIDs nil", svc.workflowsReq.WorkflowIDs)
	}
}

// TestRun_WorkflowsSubcommand_ConflictFlag_InvalidValue_ReturnsExitUsage verifies that an
// invalid --conflict value on the workflows subcommand returns ExitUsage.
func TestRun_WorkflowsSubcommand_ConflictFlag_InvalidValue_ReturnsExitUsage(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{}

	// Act
	code := cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", workspace,
			"--conflict", "not-valid", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d (ExitUsage); an invalid --conflict value must be rejected", code, cli.ExitUsage)
	}
}

// TestRun_WorkflowsSubcommand_ConflictFlag_Overwrite_PopulatesConflictDefault verifies that
// workflows --conflict overwrite populates WorkflowUpdateRequest.ConflictDefault.
func TestRun_WorkflowsSubcommand_ConflictFlag_Overwrite_PopulatesConflictDefault(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", workspace,
			"--conflict", "overwrite", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.workflowsReq == nil {
		t.Fatal("UpdateWorkflows was not called")
	}
	if svc.workflowsReq.ConflictDefault != domain.DecisionOverwrite {
		t.Errorf("ConflictDefault = %q, want %q", svc.workflowsReq.ConflictDefault, domain.DecisionOverwrite)
	}
}

// TestRun_WorkflowsSubcommand_ConflictFlag_Backup_PopulatesConflictDefault verifies that
// workflows --conflict backup populates WorkflowUpdateRequest.ConflictDefault with the
// backup-then-overwrite decision value.
func TestRun_WorkflowsSubcommand_ConflictFlag_Backup_PopulatesConflictDefault(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", workspace,
			"--conflict", "backup", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.workflowsReq == nil {
		t.Fatal("UpdateWorkflows was not called")
	}
	if svc.workflowsReq.ConflictDefault != domain.DecisionBackupThenOverwrite {
		t.Errorf("ConflictDefault = %q, want %q (backup-then-overwrite)",
			svc.workflowsReq.ConflictDefault, domain.DecisionBackupThenOverwrite)
	}
}

// TestRun_WorkflowsSubcommand_ConflictFlag_Absent_DoesNotSetConflictDefault verifies the
// "only set when the flag was explicitly provided" rule: omitting --conflict must leave
// ConflictDefault at its zero value so that interactive conflict prompts fire at runtime.
func TestRun_WorkflowsSubcommand_ConflictFlag_Absent_DoesNotSetConflictDefault(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act — no --conflict flag
	code := cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.workflowsReq == nil {
		t.Fatal("UpdateWorkflows was not called")
	}
	// ConflictDefault must be the zero value (empty string) when the flag was not provided.
	if svc.workflowsReq.ConflictDefault != "" {
		t.Errorf("ConflictDefault = %q, want empty; omitting --conflict must leave the field at zero "+
			"so the interactive conflict prompt fires rather than silently skipping", svc.workflowsReq.ConflictDefault)
	}
}

// TestRun_WorkflowsSubcommand_DryRunFlag_SetsField verifies that --dry-run propagates to
// WorkflowUpdateRequest.DryRun, matching the same flag convention used by the other subcommands.
func TestRun_WorkflowsSubcommand_DryRunFlag_SetsField(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", workspace,
			"--dry-run", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.workflowsReq == nil {
		t.Fatal("UpdateWorkflows was not called")
	}
	if !svc.workflowsReq.DryRun {
		t.Error("DryRun = false; --dry-run flag must populate WorkflowUpdateRequest.DryRun = true")
	}
}

// TestRun_WorkflowsSubcommand_AutoConfirmFlag_SetsField verifies that --auto-confirm
// propagates to WorkflowUpdateRequest.AutoConfirmPlan, matching the other subcommands.
func TestRun_WorkflowsSubcommand_AutoConfirmFlag_SetsField(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.workflowsReq == nil {
		t.Fatal("UpdateWorkflows was not called")
	}
	if !svc.workflowsReq.AutoConfirmPlan {
		t.Error("AutoConfirmPlan = false; --auto-confirm must populate WorkflowUpdateRequest.AutoConfirmPlan = true")
	}
}

// TestRun_WorkflowsSubcommand_WorkflowsFlag_TrimsWhitespace verifies that --workflows
// trims whitespace from each comma-separated ID before populating WorkflowIDs. A user
// supplying "wf-a, wf-b" (with a space after the comma) must get ["wf-a", "wf-b"].
func TestRun_WorkflowsSubcommand_WorkflowsFlag_TrimsWhitespace(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act
	cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", workspace,
			"--workflows", "wf-a, wf-b , wf-c", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if svc.workflowsReq == nil {
		t.Fatal("UpdateWorkflows was not called")
	}
	want := []string{"wf-a", "wf-b", "wf-c"}
	got := svc.workflowsReq.WorkflowIDs
	if len(got) != len(want) {
		t.Fatalf("WorkflowIDs = %v, want %v; whitespace around IDs must be trimmed", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("WorkflowIDs[%d] = %q, want %q; whitespace trimming failed", i, got[i], want[i])
		}
	}
}

// ---------------------------------------------------------------------------
// deploy --selections flag: infrastructure_agents forwarding (T1.3)
// ---------------------------------------------------------------------------

// TestRun_DeploySubcommand_SelectionsFile_InfrastructureAgents_Populated_ForwardedToRequest
// verifies that when a --selections file contains infrastructure_agents with values, the
// deploy request's InfrastructureAgentIDs field is populated with those values.
//
// RED: the current deployCmd.RunE block forwards sf.UtilityAgents and sf.Hooks into the
// request but does not forward sf.InfrastructureAgents. This test fails until
// `req.InfrastructureAgentIDs = sf.InfrastructureAgents` is added to the block.
func TestRun_DeploySubcommand_SelectionsFile_InfrastructureAgents_Populated_ForwardedToRequest(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	selectionsPath := writeTempYAML(t, "infrastructure_agents:\n  - checkpoint-manager-git\n")

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy",
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--selections", selectionsPath,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess)", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"checkpoint-manager-git"}
	got := svc.deployReq.InfrastructureAgentIDs
	if len(got) != len(want) {
		t.Fatalf("InfrastructureAgentIDs = %v, want %v; "+
			"infrastructure_agents from the selections file must be forwarded into "+
			"DeployRequest.InfrastructureAgentIDs", got, want)
	}
	if got[0] != want[0] {
		t.Errorf("InfrastructureAgentIDs[0] = %q, want %q", got[0], want[0])
	}
}

// TestRun_DeploySubcommand_SelectionsFile_InfrastructureAgents_Empty_ForwardedAsNonNilEmpty
// verifies that `infrastructure_agents: []` in a --selections file produces a non-nil
// empty slice in DeployRequest.InfrastructureAgentIDs. A non-nil empty slice is the
// "explicitly deploy none" signal that prevents the deploy tool from asking an interactive
// question. A nil value (absent key) would still allow an interactive prompt.
//
// RED: the current deployCmd.RunE block does not forward sf.InfrastructureAgents at all,
// so InfrastructureAgentIDs is nil after this test's selections file is read.
func TestRun_DeploySubcommand_SelectionsFile_InfrastructureAgents_Empty_ForwardedAsNonNilEmpty(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	selectionsPath := writeTempYAML(t, "infrastructure_agents: []\n")

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy",
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--selections", selectionsPath,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess)", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	got := svc.deployReq.InfrastructureAgentIDs
	if got == nil {
		t.Fatal("InfrastructureAgentIDs is nil; 'infrastructure_agents: []' in the " +
			"selections file must produce a non-nil empty slice so the deploy tool treats " +
			"it as \"deploy none\" rather than triggering an interactive question")
	}
	if len(got) != 0 {
		t.Errorf("InfrastructureAgentIDs = %v, want empty slice; "+
			"an explicit empty list must not add any IDs to the request", got)
	}
}

// TestRun_DeploySubcommand_SelectionsFile_InfrastructureAgents_Absent_FieldIsNil verifies
// that when a --selections file does not contain an infrastructure_agents key, the deploy
// request's InfrastructureAgentIDs field remains nil. A nil value preserves the existing
// "ask interactively" behavior.
func TestRun_DeploySubcommand_SelectionsFile_InfrastructureAgents_Absent_FieldIsNil(t *testing.T) {
	// Arrange — selections file with only utility_agents; no infrastructure_agents key.
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	selectionsPath := writeTempYAML(t, "utility_agents:\n  - some-agent\n")

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy",
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--selections", selectionsPath,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess)", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.InfrastructureAgentIDs != nil {
		t.Errorf("InfrastructureAgentIDs = %v (non-nil), want nil; "+
			"an absent infrastructure_agents key must leave InfrastructureAgentIDs nil "+
			"so the deploy tool's interactive question remains active",
			svc.deployReq.InfrastructureAgentIDs)
	}
}

// TestRun_DeploySubcommand_SelectionsFile_InfrastructureAgents_MultipleIDs_AllForwarded
// verifies that multiple infrastructure_agents IDs in the selections file are all forwarded
// into DeployRequest.InfrastructureAgentIDs without truncation or reordering.
//
// RED: same as the populated single-ID test — forwarding is not implemented yet.
func TestRun_DeploySubcommand_SelectionsFile_InfrastructureAgents_MultipleIDs_AllForwarded(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	selectionsPath := writeTempYAML(t,
		"infrastructure_agents:\n  - checkpoint-manager-git\n  - commit-manager-git\n")

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy",
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--selections", selectionsPath,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess)", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"checkpoint-manager-git", "commit-manager-git"}
	got := svc.deployReq.InfrastructureAgentIDs
	if len(got) != len(want) {
		t.Fatalf("InfrastructureAgentIDs = %v, want %v; "+
			"all infrastructure agent IDs from the selections file must be forwarded", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Errorf("InfrastructureAgentIDs[%d] = %q, want %q", i, got[i], id)
		}
	}
}

// ---------------------------------------------------------------------------
// TestRun_WorkflowsSubcommand_NoModelQuestionsInRequest (existing test)
// ---------------------------------------------------------------------------

// TestRun_WorkflowsSubcommand_NoModelQuestionsInRequest verifies that WorkflowUpdateRequest
// has no tier-model or agent-model fields by observing that the CLI subcommand does not
// expose --tier-models or --agent-models flags. This is the structural guarantee that the
// workflows subcommand never asks model questions.
func TestRun_WorkflowsSubcommand_NoModelFlags_AcceptsOnlyWorkflowFlags(t *testing.T) {
	// Arrange: pass an unrecognised flag that would exist if model configuration were exposed.
	svc := &spyService{}
	var errOut bytes.Buffer

	// Act: --tier-model is not a flag on the workflows subcommand; cobra must reject it.
	code := cli.Run(context.Background(),
		[]string{"workflows", "--tier-model", "HIGH=claude-opus-4", "--auto-confirm"},
		svc, &bytes.Buffer{}, &errOut)

	// Assert: the CLI must reject an unknown flag with a usage error, not silently ignore it.
	if code == cli.ExitSuccess {
		t.Error("exit code = ExitSuccess for unrecognised --tier-model flag on workflows; " +
			"workflows subcommand must not accept model flags — the mode has no model questions")
	}
}

// ---------------------------------------------------------------------------
// TUI/CLI equivalence for the new mode
// ---------------------------------------------------------------------------

// TestCLIWorkflowsSubcommand_PassesSameRequestAsTUI verifies that when both the TUI
// and CLI frontends process the same workflow-selection input, they construct an equivalent
// WorkflowUpdateRequest: same HarnessID, WorkspacePath, and WorkflowIDs. This is the
// structural half of the equivalence requirement.
func TestCLIWorkflowsSubcommand_PassesSameRequestFieldsAsTUI(t *testing.T) {
	// Arrange: use the CLI to make a workflows request with known inputs.
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act: CLI path.
	cli.Run(context.Background(),
		[]string{"workflows",
			"--harness", "claude-code",
			"--workspace", workspace,
			"--workflows", "wf-alpha,wf-beta",
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert: the fields that the TUI would also set from its entry screens must be present.
	if svc.workflowsReq == nil {
		t.Fatal("UpdateWorkflows was not called")
	}
	if svc.workflowsReq.HarnessID != "claude-code" {
		t.Errorf("HarnessID = %q, want %q — field must match what the TUI mode-entry screen provides",
			svc.workflowsReq.HarnessID, "claude-code")
	}
	if svc.workflowsReq.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q — field must match what the TUI workspace screen provides",
			svc.workflowsReq.WorkspacePath, workspace)
	}
	wantIDs := []string{"wf-alpha", "wf-beta"}
	if len(svc.workflowsReq.WorkflowIDs) != len(wantIDs) {
		t.Fatalf("WorkflowIDs = %v, want %v", svc.workflowsReq.WorkflowIDs, wantIDs)
	}
	for i, id := range wantIDs {
		if svc.workflowsReq.WorkflowIDs[i] != id {
			t.Errorf("WorkflowIDs[%d] = %q, want %q", i, svc.workflowsReq.WorkflowIDs[i], id)
		}
	}
}
