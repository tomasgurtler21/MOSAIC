package cli_test

// agents_test.go covers the CLI surface for the "agents" subcommand: flag handling,
// dispatch to Service.DeployAgents, exit-code contract, and output rendering.
//
// All tests are RED-phase TDD tests. They assert the full intended behaviour of the
// "agents" subcommand, which does not yet exist (I5.6 is not delivered). Every test that
// invokes cli.Run with "agents" will receive ExitUsage (unrecognised subcommand) until the
// router is extended. The tests that fail correctly in RED phase are those asserting that the
// service IS called and that flags ARE mapped to request fields.
//
// Note on usage-guard tests in RED phase: the three tests asserting ExitUsage for missing
// flags pass in RED phase because "agents" is unrecognised. They will continue to pass after
// implementation (now asserting the correct flag-validation path) and are included as
// forward-validating guards.
//
// Verified behaviours:
//
// Required flag validation:
//   - "agents" with no flags returns ExitUsage without calling DeployAgents.
//   - Omitting --workspace returns ExitUsage without calling DeployAgents.
//   - Omitting --harness returns ExitUsage without calling DeployAgents.
//   - With both --workspace and --harness, calls DeployAgents.
//
// Flag-to-request mapping:
//   - --workspace maps to DeployAgentsRequest.WorkspacePath.
//   - --harness maps to DeployAgentsRequest.HarnessID.
//   - Absent per-class flags leave all four ID slices nil (merged QDeployAgents is asked).
//   - Any per-class flag present causes the merged question to be skipped (all four treated
//     as pre-answered): the one supplied flag is non-nil, the others are non-nil empty.
//   - --subagents splits into SubagentIDs.
//   - --utility splits into UtilityAgentIDs.
//   - --infra splits into InfrastructureAgentIDs.
//   - --standalone splits into StandaloneAgentIDs.
//   - --dry-run maps to DryRun=true.
//   - --yes maps to AutoConfirmPlan=true.
//   - --workspace with surrounding quotes has them stripped.
//
// Exit codes:
//   - Service returns nil error → ExitSuccess.
//   - Service returns non-nil error → ExitFailure.
//   - OutcomeCompletedWithGaps → ExitWithSkips.
//
// Output rendering:
//   - Without --output json, output is written to the buffer in human-readable form.
//   - A dry-run result includes a "dry run" indicator in human output.
//
// Separation from other subcommands:
//   - "agents" does not call svc.DeployNew.
//   - "agents" does not call svc.Update.
//   - "agents" does not call svc.DeployStandalone.
//   - "agents" does not call svc.DeployUtilityInfrastructure.
//   - "deploy" does not call svc.DeployAgents.
//   - "update" does not call svc.DeployAgents.

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Required flag validation
// ---------------------------------------------------------------------------

// TestAgentsCmd_NoFlags_ReturnsExitUsage verifies that "agents" with no flags returns
// ExitUsage without calling Service.DeployAgents.
func TestAgentsCmd_NoFlags_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"agents"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); "+
			"agents with no flags must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.deployAgentsReq != nil {
		t.Error("svc.DeployAgents was called despite no flags; "+
			"the subcommand must guard required flags before dispatching")
	}
}

// TestAgentsCmd_MissingWorkspaceFlag_ReturnsExitUsage verifies that omitting --workspace
// returns ExitUsage without calling Service.DeployAgents.
func TestAgentsCmd_MissingWorkspaceFlag_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); "+
			"missing --workspace must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.deployAgentsReq != nil {
		t.Error("svc.DeployAgents was called despite missing --workspace")
	}
}

// TestAgentsCmd_MissingHarnessFlag_ReturnsExitUsage verifies that omitting --harness
// returns ExitUsage without calling Service.DeployAgents.
func TestAgentsCmd_MissingHarnessFlag_ReturnsExitUsage(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"agents", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); "+
			"missing --harness must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.deployAgentsReq != nil {
		t.Error("svc.DeployAgents was called despite missing --harness")
	}
}

// TestAgentsCmd_RequiredFlagsPresent_CallsDeployAgents verifies that with both --workspace
// and --harness, the subcommand calls Service.DeployAgents.
func TestAgentsCmd_RequiredFlagsPresent_CallsDeployAgents(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{
			Mode:    domain.ModeDeployAgents,
			Outcome: domain.OutcomeSuccess,
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.deployAgentsReq == nil {
		t.Error("svc.DeployAgents was not called; " +
			"with both --harness and --workspace, the subcommand must dispatch to the service")
	}
}

// ---------------------------------------------------------------------------
// Flag-to-request mapping
// ---------------------------------------------------------------------------

// TestAgentsCmd_WorkspaceFlag_SetsWorkspacePath verifies that --workspace maps to
// DeployAgentsRequest.WorkspacePath.
func TestAgentsCmd_WorkspaceFlag_SetsWorkspacePath(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	if svc.deployAgentsReq.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q", svc.deployAgentsReq.WorkspacePath, workspace)
	}
}

// TestAgentsCmd_HarnessFlag_SetsHarnessID verifies that --harness maps to
// DeployAgentsRequest.HarnessID.
func TestAgentsCmd_HarnessFlag_SetsHarnessID(t *testing.T) {
	workspace := t.TempDir()
	const harnessID = "claude-code"
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", harnessID, "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	if svc.deployAgentsReq.HarnessID != harnessID {
		t.Errorf("HarnessID = %q, want %q", svc.deployAgentsReq.HarnessID, harnessID)
	}
}

// TestAgentsCmd_NoPerClassFlags_AllIDSlicesNil verifies that when no per-class ID flag
// (--subagents, --utility, --infra, --standalone) is provided, all four ID slices in the
// request are nil, which triggers QDeployAgents interactively. This is the "nil = ask"
// half of the nil/empty convention (CD-6); all four must be nil for the merged question
// to be asked.
func TestAgentsCmd_NoPerClassFlags_AllIDSlicesNil(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	req := svc.deployAgentsReq
	if req.SubagentIDs != nil {
		t.Errorf("SubagentIDs = %v (non-nil) with no --subagents flag; want nil "+
			"(nil = ask QDeployAgents merged question, per CD-6)", req.SubagentIDs)
	}
	if req.UtilityAgentIDs != nil {
		t.Errorf("UtilityAgentIDs = %v (non-nil) with no --utility flag; want nil",
			req.UtilityAgentIDs)
	}
	if req.InfrastructureAgentIDs != nil {
		t.Errorf("InfrastructureAgentIDs = %v (non-nil) with no --infra flag; want nil",
			req.InfrastructureAgentIDs)
	}
	if req.StandaloneAgentIDs != nil {
		t.Errorf("StandaloneAgentIDs = %v (non-nil) with no --standalone flag; want nil",
			req.StandaloneAgentIDs)
	}
}

// TestAgentsCmd_SubagentsFlag_SplitsAndSetsSubagentIDs verifies that --subagents with
// comma-separated values sets SubagentIDs, with the unspecified classes set to empty
// (not nil) since any per-class flag causes the merged question to be skipped.
func TestAgentsCmd_SubagentsFlag_SplitsAndSetsSubagentIDs(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace,
			"--subagents", "agent-a,agent-b"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	req := svc.deployAgentsReq
	ids := req.SubagentIDs
	if len(ids) != 2 {
		t.Errorf("SubagentIDs has %d IDs %v; want exactly 2 ([agent-a agent-b])", len(ids), ids)
	}
	hasA, hasB := false, false
	for _, id := range ids {
		if id == "agent-a" {
			hasA = true
		}
		if id == "agent-b" {
			hasB = true
		}
	}
	if !hasA || !hasB {
		t.Errorf("SubagentIDs = %v; want [agent-a agent-b]", ids)
	}
	// When any per-class flag is set, the unspecified classes must be non-nil empty (not nil),
	// so the merged question is skipped entirely.
	if req.UtilityAgentIDs == nil {
		t.Errorf("UtilityAgentIDs is nil when --subagents is provided; want non-nil empty "+
			"(any per-class flag means the merged question is skipped: unspecified classes = explicitly empty)")
	}
	if req.InfrastructureAgentIDs == nil {
		t.Errorf("InfrastructureAgentIDs is nil when --subagents is provided; want non-nil empty")
	}
	if req.StandaloneAgentIDs == nil {
		t.Errorf("StandaloneAgentIDs is nil when --subagents is provided; want non-nil empty")
	}
}

// TestAgentsCmd_UtilityFlag_SplitsAndSetsUtilityAgentIDs verifies that --utility with
// comma-separated values sets UtilityAgentIDs.
func TestAgentsCmd_UtilityFlag_SplitsAndSetsUtilityAgentIDs(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace,
			"--utility", "util-x"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	ids := svc.deployAgentsReq.UtilityAgentIDs
	if len(ids) != 1 || ids[0] != "util-x" {
		t.Errorf("UtilityAgentIDs = %v; want [util-x]", ids)
	}
}

// TestAgentsCmd_InfraFlag_SplitsAndSetsInfrastructureAgentIDs verifies that --infra with
// comma-separated values sets InfrastructureAgentIDs.
func TestAgentsCmd_InfraFlag_SplitsAndSetsInfrastructureAgentIDs(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace,
			"--infra", "infra-y"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	ids := svc.deployAgentsReq.InfrastructureAgentIDs
	if len(ids) != 1 || ids[0] != "infra-y" {
		t.Errorf("InfrastructureAgentIDs = %v; want [infra-y]", ids)
	}
}

// TestAgentsCmd_StandaloneFlag_SplitsAndSetsStandaloneAgentIDs verifies that --standalone
// with comma-separated values sets StandaloneAgentIDs.
func TestAgentsCmd_StandaloneFlag_SplitsAndSetsStandaloneAgentIDs(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace,
			"--standalone", "sa-z"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	ids := svc.deployAgentsReq.StandaloneAgentIDs
	if len(ids) != 1 || ids[0] != "sa-z" {
		t.Errorf("StandaloneAgentIDs = %v; want [sa-z]", ids)
	}
}

// TestAgentsCmd_AllFourFlagsPresent_AllIDSlicesNonNil verifies the "any flag specified →
// all four non-nil" rule: when all four per-class flags are given, all four slices must be
// non-nil (with their respective contents). This is the fully non-interactive path.
func TestAgentsCmd_AllFourFlagsPresent_AllIDSlicesNonNil(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace,
			"--subagents", "agent-a",
			"--utility", "util-b",
			"--infra", "infra-c",
			"--standalone", "sa-d"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	req := svc.deployAgentsReq
	if req.SubagentIDs == nil {
		t.Error("SubagentIDs is nil; want non-nil (populated with agent-a)")
	}
	if req.UtilityAgentIDs == nil {
		t.Error("UtilityAgentIDs is nil; want non-nil (populated with util-b)")
	}
	if req.InfrastructureAgentIDs == nil {
		t.Error("InfrastructureAgentIDs is nil; want non-nil (populated with infra-c)")
	}
	if req.StandaloneAgentIDs == nil {
		t.Error("StandaloneAgentIDs is nil; want non-nil (populated with sa-d)")
	}
}

// TestAgentsCmd_DryRunFlag_SetsDryRun verifies that --dry-run maps to
// DeployAgentsRequest.DryRun=true.
func TestAgentsCmd_DryRunFlag_SetsDryRun(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace, "--dry-run"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	if !svc.deployAgentsReq.DryRun {
		t.Error("DryRun = false; --dry-run flag must set DryRun=true in DeployAgentsRequest")
	}
}

// TestAgentsCmd_YesFlag_SetsAutoConfirmPlan verifies that --yes maps to
// DeployAgentsRequest.AutoConfirmPlan=true.
func TestAgentsCmd_YesFlag_SetsAutoConfirmPlan(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace, "--yes"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	if !svc.deployAgentsReq.AutoConfirmPlan {
		t.Error("AutoConfirmPlan = false; --yes flag must set AutoConfirmPlan=true in DeployAgentsRequest")
	}
}

// TestAgentsCmd_WorkspaceWithQuotes_StripsQuotes verifies that a quoted path supplied via
// --workspace has its surrounding double-quotes stripped by pathinput.Unquote.
func TestAgentsCmd_WorkspaceWithQuotes_StripsQuotes(t *testing.T) {
	workspace := t.TempDir()
	quotedWorkspace := `"` + workspace + `"`
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", quotedWorkspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	if svc.deployAgentsReq.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q (outer quotes stripped by pathinput.Unquote)",
			svc.deployAgentsReq.WorkspacePath, workspace)
	}
}

// ---------------------------------------------------------------------------
// Exit codes
// ---------------------------------------------------------------------------

// TestAgentsCmd_ServiceSuccess_ReturnsExitSuccess verifies that when Service.DeployAgents
// returns nil error, cli.Run returns ExitSuccess.
func TestAgentsCmd_ServiceSuccess_ReturnsExitSuccess(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	code := cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
}

// TestAgentsCmd_ServiceError_ReturnsExitFailure verifies that when Service.DeployAgents
// returns a non-nil error, cli.Run returns ExitFailure.
func TestAgentsCmd_ServiceError_ReturnsExitFailure(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsErr: app.ErrRetargetMissingAgentKey,
	}

	code := cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d)", code, cli.ExitFailure)
	}
}

// TestAgentsCmd_ServiceCompletedWithGaps_ReturnsExitWithSkips verifies that
// OutcomeCompletedWithGaps maps to ExitWithSkips.
func TestAgentsCmd_ServiceCompletedWithGaps_ReturnsExitWithSkips(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{
			Mode:    domain.ModeDeployAgents,
			Outcome: domain.OutcomeCompletedWithGaps,
		},
	}

	code := cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitWithSkips {
		t.Errorf("exit code = %d, want ExitWithSkips (%d)", code, cli.ExitWithSkips)
	}
}

// ---------------------------------------------------------------------------
// Output rendering
// ---------------------------------------------------------------------------

// TestAgentsCmd_HumanOutput_IsNonEmpty verifies that without --output json, the output
// buffer receives human-readable content.
func TestAgentsCmd_HumanOutput_IsNonEmpty(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if outBuf.Len() == 0 {
		t.Error("output buffer is empty; want human-readable agents output")
	}
}

// TestAgentsCmd_DryRunHumanOutput_ContainsDryRunIndicator verifies that a dry-run
// human-readable output includes a visible "dry run" marker.
func TestAgentsCmd_DryRunHumanOutput_ContainsDryRunIndicator(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}
	outBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace, "--dry-run"},
		svc, outBuf, &bytes.Buffer{})

	lower := strings.ToLower(outBuf.String())
	if !strings.Contains(lower, "dry") {
		t.Errorf("human output does not contain 'dry': %q; "+
			"want a dry-run indicator", outBuf.String())
	}
}

// TestAgentsCmd_JsonOutput_EmitsValidJSON verifies that --output json causes the agents
// subcommand to write a parseable JSON document.
func TestAgentsCmd_JsonOutput_EmitsValidJSON(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"agents", "--output", "json", "--harness", "stub-harness", "--workspace", workspace},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if outBuf.Len() == 0 {
		t.Fatal("output buffer is empty with --output json; want a JSON document")
	}
	var parsed any
	if err := json.Unmarshal(outBuf.Bytes(), &parsed); err != nil {
		t.Errorf("stdout is not valid JSON with --output json: %v;\nGot: %q", err, outBuf.String())
	}
}

// ---------------------------------------------------------------------------
// Separation from other subcommands
// ---------------------------------------------------------------------------

// TestAgentsCmd_DoesNotCallDeployNew verifies that "agents" does not dispatch to
// svc.DeployNew.
func TestAgentsCmd_DoesNotCallDeployNew(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq != nil {
		t.Error("svc.DeployNew was called by the agents subcommand; it must not be")
	}
}

// TestAgentsCmd_DoesNotCallUpdate verifies that "agents" does not dispatch to svc.Update.
func TestAgentsCmd_DoesNotCallUpdate(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.updateReq != nil {
		t.Error("svc.Update was called by the agents subcommand; it must not be")
	}
}

// TestDeployCmd_DoesNotCallDeployAgents verifies that the "deploy" subcommand does not
// dispatch to svc.DeployAgents.
func TestDeployCmd_DoesNotCallDeployAgents(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq != nil {
		t.Error("svc.DeployAgents was called by the deploy subcommand; it must not be")
	}
}

// TestUpdateCmd_DoesNotCallDeployAgents verifies that the "update" subcommand does not
// dispatch to svc.DeployAgents.
func TestUpdateCmd_DoesNotCallDeployAgents(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		updateResp: domain.RunSummary{Mode: domain.ModeUpdateWorkspace, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq != nil {
		t.Error("svc.DeployAgents was called by the update subcommand; it must not be")
	}
}

// ---------------------------------------------------------------------------
// Whitespace trimming in per-class flags
// ---------------------------------------------------------------------------

// TestAgentsCmd_ConflictFlag_SetsConflictDefault verifies that --conflict maps to
// DeployAgentsRequest.ConflictDefault. The flag accepts any domain.ConflictDecision string;
// the test uses "skip" (domain.DecisionSkip) as a representative value. A silent wiring bug —
// flag parsed but not forwarded — would leave ConflictDefault at its zero value ("") and go
// undetected without this test.
func TestAgentsCmd_ConflictFlag_SetsConflictDefault(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace,
			"--conflict", string(domain.DecisionSkip)},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	if svc.deployAgentsReq.ConflictDefault != domain.DecisionSkip {
		t.Errorf("ConflictDefault = %q, want %q (domain.DecisionSkip);\n"+
			"--conflict must be forwarded to DeployAgentsRequest.ConflictDefault",
			svc.deployAgentsReq.ConflictDefault, domain.DecisionSkip)
	}
}

// TestAgentsCmd_Scope_SetsScopeProject verifies that the agents subcommand always populates
// DeployAgentsRequest.Scope with domain.ScopeProject. Scope is not user-facing (no flag);
// it is a fixed wiring constant from the CLI contract. An implementation that omits it would
// leave Scope at its zero value ("") and silently violate the deployment scope contract.
func TestAgentsCmd_Scope_SetsScopeProject(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	if svc.deployAgentsReq.Scope != domain.ScopeProject {
		t.Errorf("Scope = %q, want %q (domain.ScopeProject);\n"+
			"the agents subcommand must always set Scope=ScopeProject in DeployAgentsRequest",
			svc.deployAgentsReq.Scope, domain.ScopeProject)
	}
}

// TestAgentsCmd_SubagentsFlag_TrimsWhitespace verifies that --subagents with whitespace
// around commas produces trimmed IDs.
func TestAgentsCmd_SubagentsFlag_TrimsWhitespace(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace,
			"--subagents", "agent-a , agent-b"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq == nil {
		t.Fatal("svc.DeployAgents was not called")
	}
	for _, id := range svc.deployAgentsReq.SubagentIDs {
		if id != strings.TrimSpace(id) {
			t.Errorf("SubagentIDs contains %q which has surrounding whitespace; "+
				"--subagents must trim each comma-separated ID", id)
		}
	}
}
