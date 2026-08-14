package cli_test

// standalone_test.go covers the CLI surface for the "standalone" subcommand: flag
// handling, dispatch to Service.DeployStandalone, exit-code contract, and output rendering.
//
// All tests are RED-phase TDD tests. They assert the full intended behaviour of the
// "standalone" subcommand, which does not yet exist (I6.4 is not delivered). Every test
// that invokes cli.Run with "standalone" will receive ExitUsage (unrecognised subcommand)
// until the router is extended. The tests that are meaningful in RED phase are those
// asserting that the service IS called and that output IS produced — these fail correctly.
//
// Note on usage-guard tests in RED phase: TestStandaloneCmd_NoFlags_ReturnsExitUsage,
// TestStandaloneCmd_MissingWorkspaceFlag_ReturnsExitUsage, and
// TestStandaloneCmd_MissingHarnessFlag_ReturnsExitUsage all assert ExitUsage, which the
// CLI router already returns for any unrecognised subcommand. These three tests therefore
// pass in RED not because flag validation is implemented, but because "standalone" is
// unrecognised. They will continue to pass after implementation (now asserting the correct
// flag-validation path) and are included as forward-validating guards. Reviewers should not
// treat their RED-phase pass as evidence that the flag-validation code is in place.
//
// Verified behaviours:
//
// Required flag validation:
//   - "standalone" with no flags returns ExitUsage without calling DeployStandalone.
//   - Omitting --workspace returns ExitUsage without calling DeployStandalone.
//   - Omitting --harness returns ExitUsage without calling DeployStandalone.
//   - With both --workspace and --harness, calls DeployStandalone.
//
// Flag-to-request mapping:
//   - --workspace maps to StandaloneRequest.WorkspacePath.
//   - --harness maps to StandaloneRequest.HarnessID.
//   - --standalone (absent) leaves StandaloneAgentIDs nil (trigger question).
//   - --standalone (present, non-empty) splits by comma into StandaloneAgentIDs.
//   - --dry-run maps to StandaloneRequest.DryRun=true.
//   - --yes maps to StandaloneRequest.AutoConfirmPlan=true.
//   - --workspace with surrounding quotes has them stripped (pathinput.Unquote).
//
// Exit codes:
//   - A successful run (service returns nil error) returns ExitSuccess.
//   - A service error returns ExitFailure.
//   - A run with OutcomeCompletedWithGaps returns ExitWithSkips.
//
// Output rendering:
//   - Without --output json, output is written to the output buffer in human-readable form.
//   - A dry-run result includes a "dry run" indicator in human output.
//
// Separation from other subcommands:
//   - "standalone" does not call svc.DeployNew.
//   - "standalone" does not call svc.Update.
//   - "standalone" does not call svc.DeployUtilityInfrastructure.
//   - "deploy" does not call svc.DeployStandalone.
//   - "update" does not call svc.DeployStandalone.

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

// TestStandaloneCmd_NoFlags_ReturnsExitUsage verifies that "standalone" with no flags
// returns ExitUsage without calling Service.DeployStandalone.
func TestStandaloneCmd_NoFlags_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"standalone"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); "+
			"standalone with no flags must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.standaloneReq != nil {
		t.Error("svc.DeployStandalone was called despite no flags; "+
			"the subcommand must guard required flags first")
	}
}

// TestStandaloneCmd_MissingWorkspaceFlag_ReturnsExitUsage verifies that omitting
// --workspace returns ExitUsage without calling Service.DeployStandalone.
func TestStandaloneCmd_MissingWorkspaceFlag_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); "+
			"missing --workspace must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.standaloneReq != nil {
		t.Error("svc.DeployStandalone was called despite missing --workspace")
	}
}

// TestStandaloneCmd_MissingHarnessFlag_ReturnsExitUsage verifies that omitting --harness
// returns ExitUsage without calling Service.DeployStandalone.
func TestStandaloneCmd_MissingHarnessFlag_ReturnsExitUsage(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"standalone", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); "+
			"missing --harness must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.standaloneReq != nil {
		t.Error("svc.DeployStandalone was called despite missing --harness")
	}
}

// TestStandaloneCmd_RequiredFlagsPresent_CallsDeployStandalone verifies that with both
// --workspace and --harness present, the subcommand calls Service.DeployStandalone.
func TestStandaloneCmd_RequiredFlagsPresent_CallsDeployStandalone(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{
			Mode:    domain.ModeStandaloneOnly,
			Outcome: domain.OutcomeSuccess,
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.standaloneReq == nil {
		t.Error("svc.DeployStandalone was not called; " +
			"with both --harness and --workspace, the subcommand must dispatch to the service")
	}
}

// ---------------------------------------------------------------------------
// Flag-to-request mapping
// ---------------------------------------------------------------------------

// TestStandaloneCmd_WorkspaceFlag_SetsWorkspacePath verifies that --workspace maps to
// StandaloneRequest.WorkspacePath.
func TestStandaloneCmd_WorkspaceFlag_SetsWorkspacePath(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{Mode: domain.ModeStandaloneOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.standaloneReq == nil {
		t.Fatal("svc.DeployStandalone was not called")
	}
	if svc.standaloneReq.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q", svc.standaloneReq.WorkspacePath, workspace)
	}
}

// TestStandaloneCmd_HarnessFlag_SetsHarnessID verifies that --harness maps to
// StandaloneRequest.HarnessID.
func TestStandaloneCmd_HarnessFlag_SetsHarnessID(t *testing.T) {
	workspace := t.TempDir()
	const harnessID = "claude-code"
	svc := &spyService{
		standaloneResp: domain.RunSummary{Mode: domain.ModeStandaloneOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"standalone", "--harness", harnessID, "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.standaloneReq == nil {
		t.Fatal("svc.DeployStandalone was not called")
	}
	if svc.standaloneReq.HarnessID != harnessID {
		t.Errorf("HarnessID = %q, want %q", svc.standaloneReq.HarnessID, harnessID)
	}
}

// TestStandaloneCmd_AbsentStandaloneFlag_NilStandaloneAgentIDs verifies that when
// --standalone is absent, StandaloneRequest.StandaloneAgentIDs is nil, triggering the
// interactive question. This is the "nil = ask" half of the nil/empty convention (CD-6).
func TestStandaloneCmd_AbsentStandaloneFlag_NilStandaloneAgentIDs(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{Mode: domain.ModeStandaloneOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.standaloneReq == nil {
		t.Fatal("svc.DeployStandalone was not called")
	}
	if svc.standaloneReq.StandaloneAgentIDs != nil {
		t.Errorf("StandaloneAgentIDs = %v (non-nil), want nil; "+
			"an absent --standalone flag must leave StandaloneAgentIDs nil so the service asks "+
			"QStandaloneAgents interactively (nil = ask, per the CD-6 nil/empty convention)",
			svc.standaloneReq.StandaloneAgentIDs)
	}
}

// TestStandaloneCmd_StandaloneFlag_SplitsCommaSeparatedIDs verifies that --standalone with
// a comma-separated value maps each ID to StandaloneRequest.StandaloneAgentIDs.
func TestStandaloneCmd_StandaloneFlag_SplitsCommaSeparatedIDs(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{Mode: domain.ModeStandaloneOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace,
			"--standalone", "openai-adapter,claude-adapter"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.standaloneReq == nil {
		t.Fatal("svc.DeployStandalone was not called")
	}
	ids := svc.standaloneReq.StandaloneAgentIDs
	if len(ids) != 2 {
		t.Errorf("StandaloneAgentIDs has %d IDs %v; want exactly 2 ([openai-adapter claude-adapter]);\n"+
			"a buggy implementation that includes duplicates or an untrimmed entry would otherwise pass "+
			"the membership check below", len(ids), ids)
	}
	hasOpenAI := false
	hasClaude := false
	for _, id := range ids {
		if id == "openai-adapter" {
			hasOpenAI = true
		}
		if id == "claude-adapter" {
			hasClaude = true
		}
	}
	if !hasOpenAI || !hasClaude {
		t.Errorf("StandaloneAgentIDs = %v; want to contain both \"openai-adapter\" and \"claude-adapter\"; "+
			"--standalone must split its comma-separated value into StandaloneAgentIDs", ids)
	}
}

// TestStandaloneCmd_DryRunFlag_SetsDryRun verifies that --dry-run maps to
// StandaloneRequest.DryRun=true.
func TestStandaloneCmd_DryRunFlag_SetsDryRun(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{Mode: domain.ModeStandaloneOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace, "--dry-run"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.standaloneReq == nil {
		t.Fatal("svc.DeployStandalone was not called")
	}
	if !svc.standaloneReq.DryRun {
		t.Error("DryRun = false; --dry-run flag must set DryRun=true in StandaloneRequest")
	}
}

// TestStandaloneCmd_YesFlag_SetsAutoConfirmPlan verifies that --yes maps to
// StandaloneRequest.AutoConfirmPlan=true.
func TestStandaloneCmd_YesFlag_SetsAutoConfirmPlan(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{Mode: domain.ModeStandaloneOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace, "--yes"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.standaloneReq == nil {
		t.Fatal("svc.DeployStandalone was not called")
	}
	if !svc.standaloneReq.AutoConfirmPlan {
		t.Error("AutoConfirmPlan = false; --yes flag must set AutoConfirmPlan=true in StandaloneRequest")
	}
}

// TestStandaloneCmd_WorkspaceWithQuotes_StripsQuotes verifies that a quoted path supplied
// via --workspace has its surrounding double-quotes stripped by pathinput.Unquote.
func TestStandaloneCmd_WorkspaceWithQuotes_StripsQuotes(t *testing.T) {
	workspace := t.TempDir()
	quotedWorkspace := `"` + workspace + `"`
	svc := &spyService{
		standaloneResp: domain.RunSummary{Mode: domain.ModeStandaloneOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", quotedWorkspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.standaloneReq == nil {
		t.Fatal("svc.DeployStandalone was not called")
	}
	if svc.standaloneReq.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q (outer quotes stripped by pathinput.Unquote); "+
			"--workspace must strip surrounding double-quotes before passing the path to the request",
			svc.standaloneReq.WorkspacePath, workspace)
	}
}

// ---------------------------------------------------------------------------
// Exit codes
// ---------------------------------------------------------------------------

// TestStandaloneCmd_ServiceSuccess_ReturnsExitSuccess verifies that when
// Service.DeployStandalone returns nil error, cli.Run returns ExitSuccess.
func TestStandaloneCmd_ServiceSuccess_ReturnsExitSuccess(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{
			Mode:    domain.ModeStandaloneOnly,
			Outcome: domain.OutcomeSuccess,
		},
		standaloneErr: nil,
	}

	code := cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
}

// TestStandaloneCmd_ServiceError_ReturnsExitFailure verifies that when
// Service.DeployStandalone returns a non-nil error, cli.Run returns ExitFailure.
func TestStandaloneCmd_ServiceError_ReturnsExitFailure(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneErr: app.ErrRetargetMissingAgentKey, // any sentinel error
	}

	code := cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d)", code, cli.ExitFailure)
	}
}

// TestStandaloneCmd_ServiceCompletedWithGaps_ReturnsExitWithSkips verifies that when the
// RunSummary carries OutcomeCompletedWithGaps, cli.Run returns ExitWithSkips.
func TestStandaloneCmd_ServiceCompletedWithGaps_ReturnsExitWithSkips(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{
			Mode:    domain.ModeStandaloneOnly,
			Outcome: domain.OutcomeCompletedWithGaps,
		},
	}

	code := cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitWithSkips {
		t.Errorf("exit code = %d, want ExitWithSkips (%d); "+
			"a run with gaps must return ExitWithSkips following the established exit-code contract",
			code, cli.ExitWithSkips)
	}
}

// ---------------------------------------------------------------------------
// Output rendering
// ---------------------------------------------------------------------------

// TestStandaloneCmd_HumanOutput_IsNonEmpty verifies that without --output json, the output
// buffer receives human-readable content describing the result.
func TestStandaloneCmd_HumanOutput_IsNonEmpty(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{
			Mode:    domain.ModeStandaloneOnly,
			Outcome: domain.OutcomeSuccess,
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if outBuf.Len() == 0 {
		t.Error("output buffer is empty; want human-readable standalone output")
	}
}

// TestStandaloneCmd_DryRunHumanOutput_ContainsDryRunIndicator verifies that a dry-run
// human-readable output includes a visible "dry run" marker.
func TestStandaloneCmd_DryRunHumanOutput_ContainsDryRunIndicator(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{
			Mode:    domain.ModeStandaloneOnly,
			Outcome: domain.OutcomeSuccess,
		},
	}
	outBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace, "--dry-run"},
		svc, outBuf, &bytes.Buffer{})

	lower := strings.ToLower(outBuf.String())
	if !strings.Contains(lower, "dry") {
		t.Errorf("human output does not contain 'dry': %q; "+
			"want a dry-run indicator visible to the user", outBuf.String())
	}
}

// ---------------------------------------------------------------------------
// Separation from other subcommands
// ---------------------------------------------------------------------------

// TestStandaloneCmd_DoesNotCallDeployNew verifies that "standalone" does not dispatch
// to svc.DeployNew.
func TestStandaloneCmd_DoesNotCallDeployNew(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{Mode: domain.ModeStandaloneOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq != nil {
		t.Error("svc.DeployNew was called by the standalone subcommand; it must not be")
	}
}

// TestStandaloneCmd_DoesNotCallUpdate verifies that "standalone" does not dispatch to
// svc.Update.
func TestStandaloneCmd_DoesNotCallUpdate(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{Mode: domain.ModeStandaloneOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.updateReq != nil {
		t.Error("svc.Update was called by the standalone subcommand; it must not be")
	}
}

// TestStandaloneCmd_DoesNotCallDeployUtilityInfrastructure verifies that "standalone"
// does not dispatch to svc.DeployUtilityInfrastructure.
func TestStandaloneCmd_DoesNotCallDeployUtilityInfrastructure(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{Mode: domain.ModeStandaloneOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.utilityInfraReq != nil {
		t.Error("svc.DeployUtilityInfrastructure was called by the standalone subcommand; it must not be")
	}
}

// TestDeployCmd_DoesNotCallDeployStandalone verifies that the "deploy" subcommand does
// not dispatch to svc.DeployStandalone.
func TestDeployCmd_DoesNotCallDeployStandalone(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.standaloneReq != nil {
		t.Error("svc.DeployStandalone was called by the deploy subcommand; it must not be")
	}
}

// TestUpdateCmd_DoesNotCallDeployStandalone verifies that the "update" subcommand does
// not dispatch to svc.DeployStandalone.
func TestUpdateCmd_DoesNotCallDeployStandalone(t *testing.T) {
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

	if svc.standaloneReq != nil {
		t.Error("svc.DeployStandalone was called by the update subcommand; it must not be")
	}
}

// TestUtilityInfraCmd_DoesNotCallDeployStandalone verifies that the "utility-infra"
// subcommand does not dispatch to svc.DeployStandalone.
func TestUtilityInfraCmd_DoesNotCallDeployStandalone(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		utilityInfraResp: domain.RunSummary{Mode: domain.ModeUtilityInfraOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"utility-infra", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.standaloneReq != nil {
		t.Error("svc.DeployStandalone was called by the utility-infra subcommand; it must not be")
	}
}

// ---------------------------------------------------------------------------
// Whitespace trimming in --standalone flag
// ---------------------------------------------------------------------------

// TestStandaloneCmd_StandaloneFlag_TrimsWhitespaceAroundComma verifies that
// --standalone with whitespace around the comma (e.g. "openai-adapter , claude-adapter")
// trims each ID to its trimmed form, as required by the splitTrimmedParts contract.
// A naive strings.Split would leave leading/trailing spaces in the IDs.
func TestStandaloneCmd_StandaloneFlag_TrimsWhitespaceAroundComma(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{Mode: domain.ModeStandaloneOnly, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"standalone", "--harness", "stub-harness", "--workspace", workspace,
			"--standalone", "openai-adapter , claude-adapter"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.standaloneReq == nil {
		t.Fatal("svc.DeployStandalone was not called")
	}
	ids := svc.standaloneReq.StandaloneAgentIDs
	if len(ids) != 2 {
		t.Fatalf("StandaloneAgentIDs has %d IDs %v; want exactly 2", len(ids), ids)
	}
	for _, id := range ids {
		if id != strings.TrimSpace(id) {
			t.Errorf("StandaloneAgentIDs contains %q which has surrounding whitespace;\n"+
				"--standalone must trim each comma-separated ID (splitTrimmedParts contract)", id)
		}
	}
	// Verify the specific trimmed values are present.
	wantIDs := map[string]bool{"openai-adapter": false, "claude-adapter": false}
	for _, id := range ids {
		wantIDs[id] = true
	}
	for want, found := range wantIDs {
		if !found {
			t.Errorf("trimmed ID %q not found in StandaloneAgentIDs %v;\n"+
				"--standalone must trim whitespace around commas and split into the bare IDs", want, ids)
		}
	}
}

// ---------------------------------------------------------------------------
// JSON output rendering
// ---------------------------------------------------------------------------

// TestStandaloneCmd_JsonOutput_EmitsValidJSON verifies that --output json causes the
// standalone subcommand to write a parseable JSON document to the output buffer, consistent
// with the behaviour of the deploy and utility-infra subcommands.
func TestStandaloneCmd_JsonOutput_EmitsValidJSON(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		standaloneResp: domain.RunSummary{
			Mode:    domain.ModeStandaloneOnly,
			Outcome: domain.OutcomeSuccess,
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"standalone", "--output", "json", "--harness", "stub-harness", "--workspace", workspace},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if outBuf.Len() == 0 {
		t.Fatal("output buffer is empty with --output json; want a JSON document on stdout")
	}
	var parsed any
	if err := json.Unmarshal(outBuf.Bytes(), &parsed); err != nil {
		t.Errorf("stdout is not valid JSON with --output json: %v;\n"+
			"want a parseable JSON RunSummary document.\nGot: %q", err, outBuf.String())
	}
}
