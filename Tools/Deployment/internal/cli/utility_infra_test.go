package cli_test

// utility_infra_test.go covers the CLI surface for the "utility-infra" subcommand: flag
// handling, dispatch to Service.DeployUtilityInfrastructure, exit-code contract, and
// output rendering.
//
// All tests in this file are RED-phase TDD tests. They assert the full intended behaviour
// of the "utility-infra" subcommand, which does not yet exist (I7.4 is not delivered).
// Every test that invokes cli.Run with "utility-infra" will receive ExitUsage (unrecognised
// subcommand) until the router is extended.
//
// Verified behaviours (T7.4):
//
// Required flag validation:
//   - "utility-infra" with no flags returns ExitUsage without calling DeployUtilityInfrastructure.
//   - Omitting --workspace returns ExitUsage without calling DeployUtilityInfrastructure.
//   - Omitting --harness returns ExitUsage without calling DeployUtilityInfrastructure.
//   - With both --workspace and --harness, calls DeployUtilityInfrastructure.
//
// Flag-to-request mapping:
//   - --workspace maps to UtilityInfraRequest.WorkspacePath.
//   - --harness maps to UtilityInfraRequest.HarnessID.
//   - --utility (absent) leaves UtilityAgentIDs nil (trigger question).
//   - --utility (present, non-empty) splits by comma into UtilityAgentIDs.
//   - --infrastructure (absent) leaves InfrastructureAgentIDs nil.
//   - --infrastructure (present, non-empty) splits by comma into InfrastructureAgentIDs.
//   - --dry-run maps to UtilityInfraRequest.DryRun=true.
//   - --auto-confirm maps to UtilityInfraRequest.AutoConfirmPlan=true.
//   - --workspace with surrounding quotes has them stripped (pathinput.Unquote).
//
// Exit codes:
//   - A successful run (service returns nil error) returns ExitSuccess.
//   - A service error returns ExitFailure.
//
// Output rendering:
//   - Without --output json, output is written to the output buffer in human-readable form.
//   - With --output json, a JSON RunSummary document is written to outBuf.
//   - A dry-run result includes a "dry run" indicator in human output.
//
// Separation from other subcommands:
//   - "utility-infra" does not call svc.DeployNew.
//   - "utility-infra" does not call svc.Update.
//   - "utility-infra" does not call svc.UpdateWorkflows.
//   - "utility-infra" does not call svc.TransformHarness.
//   - "deploy" does not call svc.DeployUtilityInfrastructure.
//   - "update" does not call svc.DeployUtilityInfrastructure.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// T7.4: Required flag validation
// ---------------------------------------------------------------------------

// TestUtilityInfraCmd_NoFlags_ReturnsExitUsage verifies that "utility-infra" with no flags
// returns ExitUsage without calling Service.DeployUtilityInfrastructure.
func TestUtilityInfraCmd_NoFlags_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"utility-infra"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); "+
			"utility-infra with no flags must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.utilityInfraReq != nil {
		t.Error("svc.DeployUtilityInfrastructure was called despite no flags; " +
			"the subcommand must guard required flags first")
	}
}

// TestUtilityInfraCmd_MissingWorkspaceFlag_ReturnsExitUsage verifies that omitting
// --workspace returns ExitUsage without calling Service.DeployUtilityInfrastructure.
func TestUtilityInfraCmd_MissingWorkspaceFlag_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); "+
			"missing --workspace must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.utilityInfraReq != nil {
		t.Error("svc.DeployUtilityInfrastructure was called despite missing --workspace")
	}
}

// TestUtilityInfraCmd_MissingHarnessFlag_ReturnsExitUsage verifies that omitting --harness
// returns ExitUsage without calling Service.DeployUtilityInfrastructure.
func TestUtilityInfraCmd_MissingHarnessFlag_ReturnsExitUsage(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"utility-infra", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); "+
			"missing --harness must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.utilityInfraReq != nil {
		t.Error("svc.DeployUtilityInfrastructure was called despite missing --harness")
	}
}

// TestUtilityInfraCmd_RequiredFlagsPresent_CallsDeployUtilityInfrastructure verifies that
// with both --workspace and --harness present, the subcommand calls
// Service.DeployUtilityInfrastructure.
func TestUtilityInfraCmd_RequiredFlagsPresent_CallsDeployUtilityInfrastructure(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{
			Mode:    domain.ModeUtilityInfraOnly,
			Outcome: domain.OutcomeSuccess,
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.utilityInfraReq == nil {
		t.Error("svc.DeployUtilityInfrastructure was not called; " +
			"with both --harness and --workspace, the subcommand must dispatch to the service")
	}
}

// ---------------------------------------------------------------------------
// T7.4: Flag-to-request mapping
// ---------------------------------------------------------------------------

// TestUtilityInfraCmd_WorkspaceFlag_SetsWorkspacePath verifies that --workspace maps to
// UtilityInfraRequest.WorkspacePath.
func TestUtilityInfraCmd_WorkspaceFlag_SetsWorkspacePath(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{Mode: domain.ModeUtilityInfraOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.utilityInfraReq == nil {
		t.Fatal("svc.DeployUtilityInfrastructure was not called")
	}
	if svc.utilityInfraReq.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q", svc.utilityInfraReq.WorkspacePath, workspace)
	}
}

// TestUtilityInfraCmd_HarnessFlag_SetsHarnessID verifies that --harness maps to
// UtilityInfraRequest.HarnessID.
func TestUtilityInfraCmd_HarnessFlag_SetsHarnessID(t *testing.T) {
	workspace := t.TempDir()
	const harnessID = "claude-code"
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{Mode: domain.ModeUtilityInfraOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", harnessID, "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.utilityInfraReq == nil {
		t.Fatal("svc.DeployUtilityInfrastructure was not called")
	}
	if svc.utilityInfraReq.HarnessID != harnessID {
		t.Errorf("HarnessID = %q, want %q", svc.utilityInfraReq.HarnessID, harnessID)
	}
}

// TestUtilityInfraCmd_AbsentUtilityFlag_NilUtilityAgentIDs verifies that when --utility is
// absent, UtilityInfraRequest.UtilityAgentIDs is nil, triggering the interactive question.
// This is the "nil = ask" half of the nil/empty semantics.
func TestUtilityInfraCmd_AbsentUtilityFlag_NilUtilityAgentIDs(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{Mode: domain.ModeUtilityInfraOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.utilityInfraReq == nil {
		t.Fatal("svc.DeployUtilityInfrastructure was not called")
	}
	if svc.utilityInfraReq.UtilityAgentIDs != nil {
		t.Errorf("UtilityAgentIDs = %v (non-nil), want nil; "+
			"an absent --utility flag must leave UtilityAgentIDs nil so the service asks "+
			"QUtilityAgents interactively (nil = ask, per the CD-6 nil/empty convention)",
			svc.utilityInfraReq.UtilityAgentIDs)
	}
}

// TestUtilityInfraCmd_UtilityFlag_SplitsCommaSeparatedIDs verifies that --utility with a
// comma-separated value maps each ID to UtilityInfraRequest.UtilityAgentIDs.
func TestUtilityInfraCmd_UtilityFlag_SplitsCommaSeparatedIDs(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{Mode: domain.ModeUtilityInfraOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace,
			"--utility", "file-manager,backup-agent"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.utilityInfraReq == nil {
		t.Fatal("svc.DeployUtilityInfrastructure was not called")
	}
	ids := svc.utilityInfraReq.UtilityAgentIDs
	hasFileManager := false
	hasBackupAgent := false
	for _, id := range ids {
		if id == "file-manager" {
			hasFileManager = true
		}
		if id == "backup-agent" {
			hasBackupAgent = true
		}
	}
	if !hasFileManager || !hasBackupAgent {
		t.Errorf("UtilityAgentIDs = %v; want to contain both \"file-manager\" and \"backup-agent\"; "+
			"--utility must split its comma-separated value into UtilityAgentIDs", ids)
	}
}

// TestUtilityInfraCmd_AbsentInfrastructureFlag_NilInfrastructureAgentIDs verifies that when
// --infrastructure is absent, UtilityInfraRequest.InfrastructureAgentIDs is nil.
func TestUtilityInfraCmd_AbsentInfrastructureFlag_NilInfrastructureAgentIDs(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{Mode: domain.ModeUtilityInfraOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.utilityInfraReq == nil {
		t.Fatal("svc.DeployUtilityInfrastructure was not called")
	}
	if svc.utilityInfraReq.InfrastructureAgentIDs != nil {
		t.Errorf("InfrastructureAgentIDs = %v (non-nil), want nil; "+
			"an absent --infrastructure flag must leave InfrastructureAgentIDs nil so the "+
			"service asks QInfrastructureAgents interactively (nil = ask)",
			svc.utilityInfraReq.InfrastructureAgentIDs)
	}
}

// TestUtilityInfraCmd_InfrastructureFlag_SplitsCommaSeparatedIDs verifies that
// --infrastructure with a comma-separated value maps to InfrastructureAgentIDs.
func TestUtilityInfraCmd_InfrastructureFlag_SplitsCommaSeparatedIDs(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{Mode: domain.ModeUtilityInfraOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace,
			"--infrastructure", "checkpoint-agent,commit-agent"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.utilityInfraReq == nil {
		t.Fatal("svc.DeployUtilityInfrastructure was not called")
	}
	ids := svc.utilityInfraReq.InfrastructureAgentIDs
	hasCheckpoint := false
	hasCommit := false
	for _, id := range ids {
		if id == "checkpoint-agent" {
			hasCheckpoint = true
		}
		if id == "commit-agent" {
			hasCommit = true
		}
	}
	if !hasCheckpoint || !hasCommit {
		t.Errorf("InfrastructureAgentIDs = %v; want to contain both \"checkpoint-agent\" and \"commit-agent\"; "+
			"--infrastructure must split its comma-separated value into InfrastructureAgentIDs", ids)
	}
}

// TestUtilityInfraCmd_DryRunFlag_SetsDryRun verifies that --dry-run maps to
// UtilityInfraRequest.DryRun=true.
func TestUtilityInfraCmd_DryRunFlag_SetsDryRun(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{Mode: domain.ModeUtilityInfraOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace, "--dry-run"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.utilityInfraReq == nil {
		t.Fatal("svc.DeployUtilityInfrastructure was not called")
	}
	if !svc.utilityInfraReq.DryRun {
		t.Error("DryRun = false; --dry-run flag must set DryRun=true in UtilityInfraRequest")
	}
}

// TestUtilityInfraCmd_WorkspaceWithQuotes_StripsQuotes verifies that a quoted path supplied
// via --workspace has its surrounding double-quotes stripped by pathinput.Unquote.
func TestUtilityInfraCmd_WorkspaceWithQuotes_StripsQuotes(t *testing.T) {
	workspace := t.TempDir()
	quotedWorkspace := `"` + workspace + `"`
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{Mode: domain.ModeUtilityInfraOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", quotedWorkspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.utilityInfraReq == nil {
		t.Fatal("svc.DeployUtilityInfrastructure was not called")
	}
	if svc.utilityInfraReq.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q (outer quotes stripped by pathinput.Unquote); "+
			"--workspace must strip surrounding double-quotes before passing the path to the request",
			svc.utilityInfraReq.WorkspacePath, workspace)
	}
}

// ---------------------------------------------------------------------------
// T7.4: Exit codes
// ---------------------------------------------------------------------------

// TestUtilityInfraCmd_ServiceSuccess_ReturnsExitSuccess verifies that when
// Service.DeployUtilityInfrastructure returns nil error, cli.Run returns ExitSuccess.
func TestUtilityInfraCmd_ServiceSuccess_ReturnsExitSuccess(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{
			Mode:    domain.ModeUtilityInfraOnly,
			Outcome: domain.OutcomeSuccess,
		},
		utilityInfraErr: nil,
	}

	code := cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
}

// TestUtilityInfraCmd_ServiceError_ReturnsExitFailure verifies that when
// Service.DeployUtilityInfrastructure returns a non-nil error, cli.Run returns ExitFailure.
func TestUtilityInfraCmd_ServiceError_ReturnsExitFailure(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraErr: app.ErrRetargetMissingAgentKey, // any sentinel error
	}

	code := cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d)", code, cli.ExitFailure)
	}
}

// TestUtilityInfraCmd_ServiceCompletedWithGaps_ReturnsExitWithSkips verifies that when the
// RunSummary carries OutcomeCompletedWithGaps, cli.Run returns ExitWithSkips, following the
// same exit-code contract as the deploy and update subcommands.
func TestUtilityInfraCmd_ServiceCompletedWithGaps_ReturnsExitWithSkips(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{
			Mode:    domain.ModeUtilityInfraOnly,
			Outcome: domain.OutcomeCompletedWithGaps,
		},
	}

	code := cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitWithSkips {
		t.Errorf("exit code = %d, want ExitWithSkips (%d); "+
			"a run with gaps must return ExitWithSkips following the established exit-code contract",
			code, cli.ExitWithSkips)
	}
}

// ---------------------------------------------------------------------------
// T7.4: Output rendering
// ---------------------------------------------------------------------------

// TestUtilityInfraCmd_HumanOutput_IsNonEmpty verifies that without --output json, the output
// buffer receives human-readable content describing the result.
func TestUtilityInfraCmd_HumanOutput_IsNonEmpty(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{
			Mode:    domain.ModeUtilityInfraOnly,
			Outcome: domain.OutcomeSuccess,
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if outBuf.Len() == 0 {
		t.Error("output buffer is empty; want human-readable utility-infra output")
	}
}

// TestUtilityInfraCmd_DryRunHumanOutput_ContainsDryRunIndicator verifies that a dry-run
// human-readable output includes a visible "dry run" marker.
func TestUtilityInfraCmd_DryRunHumanOutput_ContainsDryRunIndicator(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{
			Mode:    domain.ModeUtilityInfraOnly,
			Outcome: domain.OutcomeSuccess,
		},
	}
	outBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace, "--dry-run"},
		svc, outBuf, &bytes.Buffer{})

	lower := strings.ToLower(outBuf.String())
	if !strings.Contains(lower, "dry") {
		t.Errorf("human output does not contain 'dry': %q; "+
			"want a dry-run indicator visible to the user", outBuf.String())
	}
}

// ---------------------------------------------------------------------------
// T7.4: Separation from other subcommands
// ---------------------------------------------------------------------------

// TestUtilityInfraCmd_DoesNotCallDeployNew verifies that "utility-infra" does not dispatch
// to svc.DeployNew.
func TestUtilityInfraCmd_DoesNotCallDeployNew(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{Mode: domain.ModeUtilityInfraOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq != nil {
		t.Error("svc.DeployNew was called by the utility-infra subcommand; it must not be")
	}
}

// TestUtilityInfraCmd_DoesNotCallUpdate verifies that "utility-infra" does not dispatch to
// svc.Update.
func TestUtilityInfraCmd_DoesNotCallUpdate(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{Mode: domain.ModeUtilityInfraOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.updateReq != nil {
		t.Error("svc.Update was called by the utility-infra subcommand; it must not be")
	}
}

// TestUtilityInfraCmd_DoesNotCallTransformHarness verifies that "utility-infra" does not
// dispatch to svc.TransformHarness.
func TestUtilityInfraCmd_DoesNotCallTransformHarness(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{Mode: domain.ModeUtilityInfraOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.transformReq != nil {
		t.Error("svc.TransformHarness was called by the utility-infra subcommand; it must not be")
	}
}

// TestDeployCmd_DoesNotCallDeployUtilityInfrastructure verifies that the "deploy" subcommand
// does not dispatch to svc.DeployUtilityInfrastructure.
func TestDeployCmd_DoesNotCallDeployUtilityInfrastructure(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.utilityInfraReq != nil {
		t.Error("svc.DeployUtilityInfrastructure was called by the deploy subcommand; it must not be")
	}
}

// TestUpdateCmd_DoesNotCallDeployUtilityInfrastructure verifies that the "update" subcommand
// does not dispatch to svc.DeployUtilityInfrastructure.
func TestUpdateCmd_DoesNotCallDeployUtilityInfrastructure(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		updateResp: domain.RunSummary{
			Mode:    domain.ModeUpdate,
			Outcome: domain.OutcomeSuccess,
		},
	}

	cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.utilityInfraReq != nil {
		t.Error("svc.DeployUtilityInfrastructure was called by the update subcommand; it must not be")
	}
}
