package cli_test

// hooks_test.go covers the CLI surface for the "hooks" subcommand: flag handling,
// dispatch to Service.DeployHooks, exit-code contract, and output rendering.
//
// All tests are RED-phase TDD tests. They assert the full intended behaviour of the
// "hooks" subcommand, which does not yet exist (I5.6 is not delivered). Every test that
// invokes cli.Run with "hooks" will receive ExitUsage (unrecognised subcommand) until the
// router is extended. Tests that assert the service IS called fail correctly in RED phase.
//
// Structural guarantee: DeployHooksRequest has no TierModels, AgentModels, or CustomTools
// fields. Tests assert on the concrete fields from deployhooks_contracts.go:
//   HarnessID, WorkspacePath, Scope, HookIDs []string, SkipAll map,
//   AutoConfirmPlan bool, DryRun bool, ConflictDefault.
//
// Verified behaviours:
//
// Required flag validation:
//   - "hooks" with no flags returns ExitUsage without calling DeployHooks.
//   - Omitting --workspace returns ExitUsage without calling DeployHooks.
//   - Omitting --harness returns ExitUsage without calling DeployHooks.
//   - With both --workspace and --harness, calls DeployHooks.
//
// Flag-to-request mapping:
//   - --workspace maps to DeployHooksRequest.WorkspacePath.
//   - --harness maps to DeployHooksRequest.HarnessID.
//   - Absent --hooks leaves HookIDs nil (hooks are asked interactively, CD-6 nil semantics).
//   - --hooks splits comma-separated values into HookIDs (non-nil).
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
//   - --output json emits valid JSON.
//
// Separation from other subcommands:
//   - "hooks" does not call svc.DeployNew.
//   - "hooks" does not call svc.Update.
//   - "hooks" does not call svc.DeployStandalone.
//   - "hooks" does not call svc.DeployAgents.
//   - "hooks" does not call svc.DeployUtilityInfrastructure.
//   - "agents" does not call svc.DeployHooks.
//   - "deploy" does not call svc.DeployHooks.

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

// TestHooksCmd_NoFlags_ReturnsExitUsage verifies that "hooks" with no flags returns
// ExitUsage without calling Service.DeployHooks.
func TestHooksCmd_NoFlags_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"hooks"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); "+
			"hooks with no flags must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.deployHooksReq != nil {
		t.Error("svc.DeployHooks was called despite no flags; "+
			"the subcommand must guard required flags before dispatching")
	}
}

// TestHooksCmd_MissingWorkspaceFlag_ReturnsExitUsage verifies that omitting --workspace
// returns ExitUsage without calling Service.DeployHooks.
func TestHooksCmd_MissingWorkspaceFlag_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); "+
			"missing --workspace must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.deployHooksReq != nil {
		t.Error("svc.DeployHooks was called despite missing --workspace")
	}
}

// TestHooksCmd_MissingHarnessFlag_ReturnsExitUsage verifies that omitting --harness
// returns ExitUsage without calling Service.DeployHooks.
func TestHooksCmd_MissingHarnessFlag_ReturnsExitUsage(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"hooks", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); "+
			"missing --harness must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.deployHooksReq != nil {
		t.Error("svc.DeployHooks was called despite missing --harness")
	}
}

// TestHooksCmd_RequiredFlagsPresent_CallsDeployHooks verifies that with both --workspace
// and --harness, the subcommand calls Service.DeployHooks.
func TestHooksCmd_RequiredFlagsPresent_CallsDeployHooks(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{
			Mode:    domain.ModeDeployHooks,
			Outcome: domain.OutcomeSuccess,
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.deployHooksReq == nil {
		t.Error("svc.DeployHooks was not called; " +
			"with both --harness and --workspace, the subcommand must dispatch to the service")
	}
}

// ---------------------------------------------------------------------------
// Flag-to-request mapping
// ---------------------------------------------------------------------------

// TestHooksCmd_WorkspaceFlag_SetsWorkspacePath verifies that --workspace maps to
// DeployHooksRequest.WorkspacePath.
func TestHooksCmd_WorkspaceFlag_SetsWorkspacePath(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq == nil {
		t.Fatal("svc.DeployHooks was not called")
	}
	if svc.deployHooksReq.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q", svc.deployHooksReq.WorkspacePath, workspace)
	}
}

// TestHooksCmd_HarnessFlag_SetsHarnessID verifies that --harness maps to
// DeployHooksRequest.HarnessID.
func TestHooksCmd_HarnessFlag_SetsHarnessID(t *testing.T) {
	workspace := t.TempDir()
	const harnessID = "claude-code"
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", harnessID, "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq == nil {
		t.Fatal("svc.DeployHooks was not called")
	}
	if svc.deployHooksReq.HarnessID != harnessID {
		t.Errorf("HarnessID = %q, want %q", svc.deployHooksReq.HarnessID, harnessID)
	}
}

// TestHooksCmd_NoHooksFlag_HookIDsNil verifies that when --hooks is not provided,
// HookIDs is nil in the request. Nil means "ask interactively" (CD-6 nil semantics).
func TestHooksCmd_NoHooksFlag_HookIDsNil(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq == nil {
		t.Fatal("svc.DeployHooks was not called")
	}
	if svc.deployHooksReq.HookIDs != nil {
		t.Errorf("HookIDs = %v (non-nil) with no --hooks flag; want nil "+
			"(nil = ask interactively, per CD-6)", svc.deployHooksReq.HookIDs)
	}
}

// TestHooksCmd_HooksFlag_SplitsIntoHookIDs verifies that --hooks with comma-separated
// values sets HookIDs as a non-nil slice of trimmed IDs.
func TestHooksCmd_HooksFlag_SplitsIntoHookIDs(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace,
			"--hooks", "hook-alpha,hook-beta"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq == nil {
		t.Fatal("svc.DeployHooks was not called")
	}
	ids := svc.deployHooksReq.HookIDs
	if len(ids) != 2 {
		t.Errorf("HookIDs has %d IDs %v; want exactly 2 ([hook-alpha hook-beta])", len(ids), ids)
	}
	hasAlpha, hasBeta := false, false
	for _, id := range ids {
		if id == "hook-alpha" {
			hasAlpha = true
		}
		if id == "hook-beta" {
			hasBeta = true
		}
	}
	if !hasAlpha || !hasBeta {
		t.Errorf("HookIDs = %v; want [hook-alpha hook-beta]", ids)
	}
}

// TestHooksCmd_HooksFlag_TrimsWhitespace verifies that --hooks trims whitespace around
// comma-separated values.
func TestHooksCmd_HooksFlag_TrimsWhitespace(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace,
			"--hooks", "hook-alpha , hook-beta"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq == nil {
		t.Fatal("svc.DeployHooks was not called")
	}
	for _, id := range svc.deployHooksReq.HookIDs {
		if id != strings.TrimSpace(id) {
			t.Errorf("HookIDs contains %q which has surrounding whitespace; "+
				"--hooks must trim each comma-separated ID", id)
		}
	}
}

// TestHooksCmd_ConflictFlag_SetsConflictDefault verifies that --conflict maps to
// DeployHooksRequest.ConflictDefault. The flag accepts any domain.ConflictDecision string;
// the test uses "skip" (domain.DecisionSkip) as a representative value. The hooks_test.go
// field inventory lists ConflictDefault but the original verified-behaviours section omitted
// a test for it — this test fills that gap. A silent wiring bug would leave ConflictDefault
// at its zero value ("") and go undetected.
func TestHooksCmd_ConflictFlag_SetsConflictDefault(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace,
			"--conflict", string(domain.DecisionSkip)},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq == nil {
		t.Fatal("svc.DeployHooks was not called")
	}
	if svc.deployHooksReq.ConflictDefault != domain.DecisionSkip {
		t.Errorf("ConflictDefault = %q, want %q (domain.DecisionSkip);\n"+
			"--conflict must be forwarded to DeployHooksRequest.ConflictDefault",
			svc.deployHooksReq.ConflictDefault, domain.DecisionSkip)
	}
}

// TestHooksCmd_Scope_SetsScopeProject verifies that the hooks subcommand always populates
// DeployHooksRequest.Scope with domain.ScopeProject. Scope is a fixed wiring constant, not
// user-facing. An implementation that omits it would leave Scope at its zero value ("") and
// silently violate the deployment scope contract.
func TestHooksCmd_Scope_SetsScopeProject(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq == nil {
		t.Fatal("svc.DeployHooks was not called")
	}
	if svc.deployHooksReq.Scope != domain.ScopeProject {
		t.Errorf("Scope = %q, want %q (domain.ScopeProject);\n"+
			"the hooks subcommand must always set Scope=ScopeProject in DeployHooksRequest",
			svc.deployHooksReq.Scope, domain.ScopeProject)
	}
}

// TestHooksCmd_HooksFlag_EmptyString_NonInteractiveNone verifies that --hooks "" (empty
// value) produces non-nil empty HookIDs, meaning "explicitly none" (CD-6 empty semantics).
func TestHooksCmd_HooksFlag_EmptyString_NonInteractiveNone(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace,
			"--hooks", ""},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq == nil {
		t.Fatal("svc.DeployHooks was not called")
	}
	if svc.deployHooksReq.HookIDs == nil {
		t.Error("HookIDs is nil when --hooks=\"\" is provided; " +
			"want non-nil empty slice (explicitly none, skip interactive selection, per CD-6)")
	}
}

// TestHooksCmd_DryRunFlag_SetsDryRun verifies that --dry-run maps to
// DeployHooksRequest.DryRun=true.
func TestHooksCmd_DryRunFlag_SetsDryRun(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace, "--dry-run"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq == nil {
		t.Fatal("svc.DeployHooks was not called")
	}
	if !svc.deployHooksReq.DryRun {
		t.Error("DryRun = false; --dry-run flag must set DryRun=true in DeployHooksRequest")
	}
}

// TestHooksCmd_YesFlag_SetsAutoConfirmPlan verifies that --yes maps to
// DeployHooksRequest.AutoConfirmPlan=true.
func TestHooksCmd_YesFlag_SetsAutoConfirmPlan(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace, "--yes"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq == nil {
		t.Fatal("svc.DeployHooks was not called")
	}
	if !svc.deployHooksReq.AutoConfirmPlan {
		t.Error("AutoConfirmPlan = false; --yes flag must set AutoConfirmPlan=true in DeployHooksRequest")
	}
}

// TestHooksCmd_WorkspaceWithQuotes_StripsQuotes verifies that a quoted path supplied via
// --workspace has its surrounding double-quotes stripped.
func TestHooksCmd_WorkspaceWithQuotes_StripsQuotes(t *testing.T) {
	workspace := t.TempDir()
	quotedWorkspace := `"` + workspace + `"`
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", quotedWorkspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq == nil {
		t.Fatal("svc.DeployHooks was not called")
	}
	if svc.deployHooksReq.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q (outer quotes stripped by pathinput.Unquote)",
			svc.deployHooksReq.WorkspacePath, workspace)
	}
}

// TestHooksCmd_FullyNonInteractiveRun verifies the fully non-interactive path:
// --workspace, --harness, --hooks, and --yes all supplied; one call to DeployHooks,
// HookIDs non-nil, AutoConfirmPlan=true.
func TestHooksCmd_FullyNonInteractiveRun(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	code := cli.Run(context.Background(),
		[]string{"hooks",
			"--harness", "claude-code",
			"--workspace", workspace,
			"--hooks", "on-deploy,on-teardown",
			"--yes"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.deployHooksReq == nil {
		t.Fatal("svc.DeployHooks was not called")
	}
	req := svc.deployHooksReq
	if req.HarnessID != "claude-code" {
		t.Errorf("HarnessID = %q, want %q", req.HarnessID, "claude-code")
	}
	if req.WorkspacePath != workspace {
		t.Errorf("WorkspacePath = %q, want %q", req.WorkspacePath, workspace)
	}
	if req.HookIDs == nil {
		t.Error("HookIDs is nil; want non-nil (explicitly supplied)")
	}
	if len(req.HookIDs) != 2 {
		t.Errorf("HookIDs = %v; want [on-deploy on-teardown]", req.HookIDs)
	}
	if !req.AutoConfirmPlan {
		t.Error("AutoConfirmPlan = false; --yes must set it true")
	}
}

// ---------------------------------------------------------------------------
// Exit codes
// ---------------------------------------------------------------------------

// TestHooksCmd_ServiceSuccess_ReturnsExitSuccess verifies ExitSuccess on nil error.
func TestHooksCmd_ServiceSuccess_ReturnsExitSuccess(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	code := cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
}

// TestHooksCmd_ServiceError_ReturnsExitFailure verifies ExitFailure on non-nil error.
func TestHooksCmd_ServiceError_ReturnsExitFailure(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksErr: app.ErrRetargetMissingAgentKey,
	}

	code := cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d)", code, cli.ExitFailure)
	}
}

// TestHooksCmd_ServiceCompletedWithGaps_ReturnsExitWithSkips verifies that
// OutcomeCompletedWithGaps maps to ExitWithSkips.
func TestHooksCmd_ServiceCompletedWithGaps_ReturnsExitWithSkips(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{
			Mode:    domain.ModeDeployHooks,
			Outcome: domain.OutcomeCompletedWithGaps,
		},
	}

	code := cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitWithSkips {
		t.Errorf("exit code = %d, want ExitWithSkips (%d)", code, cli.ExitWithSkips)
	}
}

// ---------------------------------------------------------------------------
// Output rendering
// ---------------------------------------------------------------------------

// TestHooksCmd_HumanOutput_IsNonEmpty verifies human-readable output is written.
func TestHooksCmd_HumanOutput_IsNonEmpty(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if outBuf.Len() == 0 {
		t.Error("output buffer is empty; want human-readable hooks output")
	}
}

// TestHooksCmd_DryRunHumanOutput_ContainsDryRunIndicator verifies a "dry run" marker
// appears in human-readable output when --dry-run is used.
func TestHooksCmd_DryRunHumanOutput_ContainsDryRunIndicator(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}
	outBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace, "--dry-run"},
		svc, outBuf, &bytes.Buffer{})

	lower := strings.ToLower(outBuf.String())
	if !strings.Contains(lower, "dry") {
		t.Errorf("human output does not contain 'dry': %q; "+
			"want a dry-run indicator", outBuf.String())
	}
}

// TestHooksCmd_JsonOutput_EmitsValidJSON verifies --output json writes parseable JSON.
func TestHooksCmd_JsonOutput_EmitsValidJSON(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"hooks", "--output", "json", "--harness", "stub-harness", "--workspace", workspace},
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

// TestHooksCmd_DoesNotCallDeployNew verifies "hooks" does not dispatch to svc.DeployNew.
func TestHooksCmd_DoesNotCallDeployNew(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq != nil {
		t.Error("svc.DeployNew was called by the hooks subcommand; it must not be")
	}
}

// TestHooksCmd_DoesNotCallUpdate verifies "hooks" does not dispatch to svc.Update.
func TestHooksCmd_DoesNotCallUpdate(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.updateReq != nil {
		t.Error("svc.Update was called by the hooks subcommand; it must not be")
	}
}

// TestHooksCmd_DoesNotCallDeployAgents verifies "hooks" does not dispatch to
// svc.DeployAgents.
func TestHooksCmd_DoesNotCallDeployAgents(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployHooksResp: domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"hooks", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployAgentsReq != nil {
		t.Error("svc.DeployAgents was called by the hooks subcommand; it must not be")
	}
}

// TestAgentsCmd_DoesNotCallDeployHooks verifies "agents" does not call svc.DeployHooks.
func TestAgentsCmd_DoesNotCallDeployHooks(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		deployAgentsResp: domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess},
	}

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq != nil {
		t.Error("svc.DeployHooks was called by the agents subcommand; it must not be")
	}
}

// TestDeployCmd_DoesNotCallDeployHooks verifies the "deploy" subcommand does not call
// svc.DeployHooks.
func TestDeployCmd_DoesNotCallDeployHooks(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployHooksReq != nil {
		t.Error("svc.DeployHooks was called by the deploy subcommand; it must not be")
	}
}
