package cli_test

// render_test.go covers the CLI surface for the "render" subcommand: flag handling,
// dispatch to Service.RenderAgent, exit-code contract, JSON/human output rendering,
// path flag quote-stripping, and workflow-set flag semantics.
//
// All tests in this file are RED-phase TDD tests. They assert the full intended behaviour
// of the "render" subcommand, which does not yet exist (I2.1 through I2.4 are not delivered).
// Every test that invokes cli.Run with "render" will receive ExitUsage (unrecognised
// subcommand) until I2.1 registers the subcommand.
//
// Verified behaviours (T2.1 through T2.5):
//
// Required flag validation (T2.1):
//   - "render" with no flags returns ExitUsage without calling Service.RenderAgent.
//   - Missing --harness returns ExitUsage without calling Service.RenderAgent.
//   - Missing both --source and --source-agent returns ExitUsage (source required).
//   - Supplying both --source and --source-agent returns ExitUsage (source ambiguous).
//   - Missing both --dest and --workspace returns ExitUsage (destination required).
//   - Supplying both --dest and --workspace returns ExitUsage (destination ambiguous).
//   - A fully-flagged invocation calls Service.RenderAgent.
//
// Non-interactive guarantee (T2.2):
//   - With every required flag supplied, Service.RenderAgent is called and completes.
//   - render does not call svc.DeployNew, svc.Update, svc.Promote, or svc.TransformHarness.
//   - Other subcommands (deploy, transform) do not call svc.RenderAgent.
//
// Output rendering and exit codes (T2.3):
//   - A successful render (service returns nil error) returns ExitSuccess.
//   - A service error returns ExitFailure.
//   - ExitWithSkips (2) is never returned; render has no partial-success state.
//   - Without --output json, the output buffer receives human-readable content.
//   - With --output json on success, a valid RenderAgentResult JSON document is written.
//   - The JSON success document contains destinationPath and sourceVersion fields.
//   - With --output json on service failure, a RenderFailure envelope is written to stdout.
//   - The RenderFailure envelope has error=true and a non-empty reason string.
//   - A dry-run human output includes a "dry run" indicator.
//   - Each ErrRender* sentinel maps to its documented stable reason code in the JSON envelope.
//
// Path flag quote-stripping (T2.4):
//   - --source with double-quoted path strips the outer quotes (pathinput.Unquote).
//   - --dest with double-quoted path strips the outer quotes.
//   - --workspace with double-quoted path strips the outer quotes.
//
// Workflow-set flag (T2.5):
//   - Absent --workflows flag leaves Workflows nil in the request.
//   - --workflows="" sets Workflows to a non-nil empty slice.
//   - --workflows=a,b,c sets Workflows to ["a", "b", "c"].

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/cli"
)

// ---------------------------------------------------------------------------
// Wire types for JSON decoding
//
// RenderFailure is not yet exported from the cli package (it will be created
// by I2.3). These local wire structs mirror the JSON shapes for decode-only use
// in tests. Once the implementation exists, the test verifies the real shape.
// ---------------------------------------------------------------------------

// wireRenderFailure mirrors RenderFailure's JSON shape for test decoding.
type wireRenderFailure struct {
	Error    bool   `json:"error"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	Detail   string `json:"detail,omitempty"`
	ExitCode int    `json:"exitCode"`
}

// wireRenderResult mirrors RenderAgentResult's minimal JSON shape for test decoding
// of the fields the consuming tool depends on (AC2.4).
type wireRenderResult struct {
	DestinationPath string `json:"destinationPath"`
	SourceVersion   string `json:"sourceVersion"`
	AgentKey        string `json:"agentKey"`
	DryRun          bool   `json:"dryRun"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// minRenderArgs returns the minimal valid "render" args: --harness, --source, --dest.
func minRenderArgs(harness, source, dest string) []string {
	return []string{"render", "--harness", harness, "--source", source, "--dest", dest}
}

// ---------------------------------------------------------------------------
// T2.1: Required flag validation
// ---------------------------------------------------------------------------

// TestRenderCmd_NoFlags_ReturnsExitUsage verifies that "render" with no flags returns
// ExitUsage without calling Service.RenderAgent.
func TestRenderCmd_NoFlags_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"render"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); render with no flags must return ExitUsage",
			code, cli.ExitUsage)
	}
	if svc.renderAgentReq != nil {
		t.Error("svc.RenderAgent was called despite no flags; subcommand must guard required flags first")
	}
}

// TestRenderCmd_MissingHarnessFlag_ReturnsExitUsage verifies that omitting --harness
// returns ExitUsage without calling Service.RenderAgent.
func TestRenderCmd_MissingHarnessFlag_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"render", "--source", "/agents/agent.md", "--dest", "/out/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); missing --harness must return ExitUsage",
			code, cli.ExitUsage)
	}
	if svc.renderAgentReq != nil {
		t.Error("svc.RenderAgent was called despite missing --harness")
	}
}

// TestRenderCmd_MissingSourceFlags_ReturnsExitUsage verifies that supplying neither
// --source nor --source-agent returns ExitUsage without calling Service.RenderAgent.
func TestRenderCmd_MissingSourceFlags_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"render", "--harness", "claude-code", "--dest", "/out/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); missing --source and --source-agent must return ExitUsage",
			code, cli.ExitUsage)
	}
	if svc.renderAgentReq != nil {
		t.Error("svc.RenderAgent was called despite missing source flag")
	}
}

// TestRenderCmd_BothSourceFlags_ReturnsExitUsage verifies that supplying both --source
// and --source-agent (ambiguous source) returns ExitUsage without calling Service.RenderAgent.
func TestRenderCmd_BothSourceFlags_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"render",
			"--harness", "claude-code",
			"--source", "/agents/agent.md",
			"--source-agent", "orchestrator",
			"--dest", "/out/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); supplying both --source and --source-agent must return ExitUsage",
			code, cli.ExitUsage)
	}
	if svc.renderAgentReq != nil {
		t.Error("svc.RenderAgent was called despite ambiguous source (both --source and --source-agent)")
	}
}

// TestRenderCmd_MissingDestinationFlags_ReturnsExitUsage verifies that supplying neither
// --dest nor --workspace returns ExitUsage without calling Service.RenderAgent.
func TestRenderCmd_MissingDestinationFlags_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"render", "--harness", "claude-code", "--source", "/agents/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); missing --dest and --workspace must return ExitUsage",
			code, cli.ExitUsage)
	}
	if svc.renderAgentReq != nil {
		t.Error("svc.RenderAgent was called despite missing destination flag")
	}
}

// TestRenderCmd_BothDestinationFlags_ReturnsExitUsage verifies that supplying both --dest
// and --workspace (ambiguous destination) returns ExitUsage without calling Service.RenderAgent.
func TestRenderCmd_BothDestinationFlags_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"render",
			"--harness", "claude-code",
			"--source", "/agents/agent.md",
			"--dest", "/out/agent.md",
			"--workspace", "/workspace"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); supplying both --dest and --workspace must return ExitUsage",
			code, cli.ExitUsage)
	}
	if svc.renderAgentReq != nil {
		t.Error("svc.RenderAgent was called despite ambiguous destination (both --dest and --workspace)")
	}
}

// TestRenderCmd_AllRequiredFlags_CallsRenderAgent verifies that supplying all required
// flags causes Service.RenderAgent to be called.
func TestRenderCmd_AllRequiredFlags_CallsRenderAgent(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{
			DestinationPath:       "/out/agent.md",
			DestinationResolvedBy: "explicit",
		},
	}

	code := cli.Run(context.Background(),
		minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called despite all required flags being present")
	}
}

// ---------------------------------------------------------------------------
// T2.1 & T2.3: Flag-to-request mapping
// ---------------------------------------------------------------------------

// TestRenderCmd_HarnessFlag_SetsTargetHarnessID verifies that --harness maps to
// RenderAgentRequest.TargetHarnessID.
func TestRenderCmd_HarnessFlag_SetsTargetHarnessID(t *testing.T) {
	const harnessID = "claude-code"
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
	}

	cli.Run(context.Background(),
		minRenderArgs(harnessID, "/agents/agent.md", "/out/agent.md"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if svc.renderAgentReq.TargetHarnessID != harnessID {
		t.Errorf("TargetHarnessID = %q, want %q", svc.renderAgentReq.TargetHarnessID, harnessID)
	}
}

// TestRenderCmd_SourceFlag_SetsSourcePath verifies that --source maps to
// RenderAgentRequest.SourcePath.
func TestRenderCmd_SourceFlag_SetsSourcePath(t *testing.T) {
	const sourcePath = "/path/to/my-agent.md"
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
	}

	cli.Run(context.Background(),
		minRenderArgs("claude-code", sourcePath, "/out/agent.md"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if svc.renderAgentReq.SourcePath != sourcePath {
		t.Errorf("SourcePath = %q, want %q", svc.renderAgentReq.SourcePath, sourcePath)
	}
	if svc.renderAgentReq.SourceAgentKey != "" {
		t.Errorf("SourceAgentKey = %q, want empty when --source is used", svc.renderAgentReq.SourceAgentKey)
	}
}

// TestRenderCmd_SourceAgentFlag_SetsSourceAgentKey verifies that --source-agent maps to
// RenderAgentRequest.SourceAgentKey.
func TestRenderCmd_SourceAgentFlag_SetsSourceAgentKey(t *testing.T) {
	const agentKey = "orchestrator"
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
	}

	cli.Run(context.Background(),
		[]string{"render", "--harness", "claude-code", "--source-agent", agentKey, "--dest", "/out/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if svc.renderAgentReq.SourceAgentKey != agentKey {
		t.Errorf("SourceAgentKey = %q, want %q", svc.renderAgentReq.SourceAgentKey, agentKey)
	}
	if svc.renderAgentReq.SourcePath != "" {
		t.Errorf("SourcePath = %q, want empty when --source-agent is used", svc.renderAgentReq.SourcePath)
	}
}

// TestRenderCmd_DestFlag_SetsDestinationPath verifies that --dest maps to
// RenderAgentRequest.DestinationPath.
func TestRenderCmd_DestFlag_SetsDestinationPath(t *testing.T) {
	const destPath = "/out/deployed-agent.md"
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: destPath},
	}

	cli.Run(context.Background(),
		minRenderArgs("claude-code", "/agents/agent.md", destPath),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if svc.renderAgentReq.DestinationPath != destPath {
		t.Errorf("DestinationPath = %q, want %q", svc.renderAgentReq.DestinationPath, destPath)
	}
	if svc.renderAgentReq.WorkspaceRoot != "" {
		t.Errorf("WorkspaceRoot = %q, want empty when --dest is used", svc.renderAgentReq.WorkspaceRoot)
	}
}

// TestRenderCmd_WorkspaceFlag_SetsWorkspaceRoot verifies that --workspace maps to
// RenderAgentRequest.WorkspaceRoot.
func TestRenderCmd_WorkspaceFlag_SetsWorkspaceRoot(t *testing.T) {
	const workspaceRoot = "/my/workspace"
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/my/workspace/.claude/agents/agent.md"},
	}

	cli.Run(context.Background(),
		[]string{"render", "--harness", "claude-code", "--source", "/agents/agent.md", "--workspace", workspaceRoot},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if svc.renderAgentReq.WorkspaceRoot != workspaceRoot {
		t.Errorf("WorkspaceRoot = %q, want %q", svc.renderAgentReq.WorkspaceRoot, workspaceRoot)
	}
	if svc.renderAgentReq.DestinationPath != "" {
		t.Errorf("DestinationPath = %q, want empty when --workspace is used", svc.renderAgentReq.DestinationPath)
	}
}

// TestRenderCmd_ModelFlag_SetsTargetModel verifies that --model maps to
// RenderAgentRequest.TargetModel.
func TestRenderCmd_ModelFlag_SetsTargetModel(t *testing.T) {
	const modelID = "claude-sonnet-4-6"
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md", TargetModel: modelID},
	}

	cli.Run(context.Background(),
		append(minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"), "--model", modelID),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if svc.renderAgentReq.TargetModel != modelID {
		t.Errorf("TargetModel = %q, want %q", svc.renderAgentReq.TargetModel, modelID)
	}
}

// TestRenderCmd_OverwriteFlag_SetsOverwrite verifies that --overwrite maps to
// RenderAgentRequest.Overwrite=true.
func TestRenderCmd_OverwriteFlag_SetsOverwrite(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
	}

	cli.Run(context.Background(),
		append(minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"), "--overwrite"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if !svc.renderAgentReq.Overwrite {
		t.Error("Overwrite = false; --overwrite flag must set it to true")
	}
}

// TestRenderCmd_DryRunFlag_SetsDryRun verifies that --dry-run maps to
// RenderAgentRequest.DryRun=true.
func TestRenderCmd_DryRunFlag_SetsDryRun(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md", DryRun: true},
	}

	cli.Run(context.Background(),
		append(minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"), "--dry-run"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if !svc.renderAgentReq.DryRun {
		t.Error("DryRun = false; --dry-run flag must set it to true")
	}
}

// TestRenderCmd_DefaultFlags_OverwriteAndDryRunAreFalse verifies that when --overwrite
// and --dry-run are absent, both are false in the request.
func TestRenderCmd_DefaultFlags_OverwriteAndDryRunAreFalse(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
	}

	cli.Run(context.Background(),
		minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if svc.renderAgentReq.Overwrite {
		t.Error("Overwrite = true despite --overwrite flag being absent")
	}
	if svc.renderAgentReq.DryRun {
		t.Error("DryRun = true despite --dry-run flag being absent")
	}
}

// ---------------------------------------------------------------------------
// T2.2: Non-interactive guarantee
// ---------------------------------------------------------------------------

// TestRenderCmd_AllFlagsSupplied_DoesNotCallDeployNew verifies that "render" does not
// dispatch to svc.DeployNew, ensuring the subcommand is isolated to its own service method.
func TestRenderCmd_AllFlagsSupplied_DoesNotCallDeployNew(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
	}

	cli.Run(context.Background(),
		minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq != nil {
		t.Error("svc.DeployNew was called by the render subcommand; it must not be")
	}
}

// TestRenderCmd_AllFlagsSupplied_DoesNotCallTransformHarness verifies that "render" does
// not dispatch to svc.TransformHarness.
func TestRenderCmd_AllFlagsSupplied_DoesNotCallTransformHarness(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
	}

	cli.Run(context.Background(),
		minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.transformReq != nil {
		t.Error("svc.TransformHarness was called by the render subcommand; it must not be")
	}
}

// TestRenderCmd_AllFlagsSupplied_DoesNotCallPromote verifies that "render" does not
// dispatch to svc.Promote.
func TestRenderCmd_AllFlagsSupplied_DoesNotCallPromote(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
	}

	cli.Run(context.Background(),
		minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.promoteReq != nil {
		t.Error("svc.Promote was called by the render subcommand; it must not be")
	}
}

// TestDeployCmd_DoesNotCallRenderAgent verifies that the "deploy" subcommand does not
// dispatch to svc.RenderAgent.
func TestDeployCmd_DoesNotCallRenderAgent(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq != nil {
		t.Error("svc.RenderAgent was called by the deploy subcommand; it must not be")
	}
}

// TestTransformCmd_DoesNotCallRenderAgent verifies that the "transform" subcommand does
// not dispatch to svc.RenderAgent.
func TestTransformCmd_DoesNotCallRenderAgent(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{SourceHarnessID: "src", TargetHarnessID: "tgt"},
	}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq != nil {
		t.Error("svc.RenderAgent was called by the transform subcommand; it must not be")
	}
}

// ---------------------------------------------------------------------------
// T2.3: Exit codes
// ---------------------------------------------------------------------------

// TestRenderCmd_ServiceSuccess_ReturnsExitSuccess verifies that when Service.RenderAgent
// returns nil error, cli.Run returns ExitSuccess.
func TestRenderCmd_ServiceSuccess_ReturnsExitSuccess(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{
			DestinationPath:       "/out/agent.md",
			DestinationResolvedBy: "explicit",
			AgentKey:              "agent",
		},
		renderAgentErr: nil,
	}

	code := cli.Run(context.Background(),
		minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
}

// TestRenderCmd_ServiceError_ReturnsExitFailure verifies that when Service.RenderAgent
// returns a non-nil error, cli.Run returns ExitFailure.
func TestRenderCmd_ServiceError_ReturnsExitFailure(t *testing.T) {
	svc := &spyService{
		renderAgentErr: app.ErrRenderAgentNotInCatalog,
	}

	code := cli.Run(context.Background(),
		[]string{"render", "--harness", "claude-code", "--source-agent", "nonexistent", "--dest", "/out/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d)", code, cli.ExitFailure)
	}
}

// TestRenderCmd_NeverReturnsExitWithSkips verifies that the render subcommand never
// returns ExitWithSkips (2): it has no partial-success state.
func TestRenderCmd_NeverReturnsExitWithSkips(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
		renderAgentErr:  nil,
	}

	code := cli.Run(context.Background(),
		minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code == cli.ExitWithSkips {
		t.Errorf("exit code = ExitWithSkips (%d); render must never return ExitWithSkips — "+
			"it has no partial-success state", cli.ExitWithSkips)
	}
}

// ---------------------------------------------------------------------------
// T2.3: Output rendering
// ---------------------------------------------------------------------------

// TestRenderCmd_HumanOutput_IsNonEmpty verifies that without --output json, the output
// buffer receives human-readable content (non-empty) describing the render result.
func TestRenderCmd_HumanOutput_IsNonEmpty(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{
			SourcePath:            "/agents/agent.md",
			AgentKey:              "agent",
			TargetHarnessID:       "claude-code",
			DestinationPath:       "/out/agent.md",
			DestinationResolvedBy: "explicit",
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if outBuf.Len() == 0 {
		t.Error("output buffer is empty; want human-readable render output")
	}
}

// TestRenderCmd_JSONOutput_IsValidRenderAgentResult verifies that --output json writes a
// JSON-encoded RenderAgentResult to the output buffer on success. It asserts the two fields
// the consuming tool depends on (AC2.4): destinationPath and sourceVersion.
func TestRenderCmd_JSONOutput_IsValidRenderAgentResult(t *testing.T) {
	const destPath = "/out/my-agent.md"
	const version = "1.2.3"
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{
			SourcePath:            "/agents/my-agent.md",
			AgentKey:              "my-agent",
			SourceVersion:         version,
			TargetHarnessID:       "claude-code",
			DestinationPath:       destPath,
			DestinationResolvedBy: "explicit",
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		append(minRenderArgs("claude-code", "/agents/my-agent.md", destPath), "--output", "json"),
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}

	var got wireRenderResult
	if err := json.NewDecoder(outBuf).Decode(&got); err != nil {
		t.Fatalf("json.Decode output: %v; want a JSON RenderAgentResult document; output:\n%s",
			err, outBuf.String())
	}
	if got.DestinationPath != destPath {
		t.Errorf("JSON destinationPath = %q, want %q; "+
			"destinationPath is a required field for the consuming tool (AC2.4)",
			got.DestinationPath, destPath)
	}
	if got.SourceVersion != version {
		t.Errorf("JSON sourceVersion = %q, want %q; "+
			"sourceVersion is a required field for the consuming tool (AC2.4)",
			got.SourceVersion, version)
	}
}

// TestRenderCmd_JSONOutputOnFailure_IsRenderFailureEnvelope verifies that under
// --output json, a service failure writes a RenderFailure envelope (not a result document)
// to stdout. The envelope must have error=true and a non-empty reason.
func TestRenderCmd_JSONOutputOnFailure_IsRenderFailureEnvelope(t *testing.T) {
	svc := &spyService{
		renderAgentErr: app.ErrRenderAgentNotInCatalog,
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		append([]string{"render", "--harness", "claude-code", "--source-agent", "bad-key", "--dest", "/out/agent.md"},
			"--output", "json"),
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d)", code, cli.ExitFailure)
	}

	if outBuf.Len() == 0 {
		t.Fatal("stdout is empty under --output json on failure; want a RenderFailure envelope")
	}

	var got wireRenderFailure
	if err := json.NewDecoder(outBuf).Decode(&got); err != nil {
		t.Fatalf("json.Decode stdout: %v; want a JSON RenderFailure envelope; output:\n%s",
			err, outBuf.String())
	}
	if !got.Error {
		t.Error("RenderFailure.error = false; want true so a consumer can detect the failure shape")
	}
	if got.Reason == "" {
		t.Error("RenderFailure.reason is empty; want the stable failure code (e.g. 'agent-not-in-catalog')")
	}
}

// TestRenderCmd_JSONFailureEnvelope_HasMatchingExitCode verifies that the exitCode field
// in the RenderFailure envelope equals the process exit code, so a captured stdout is
// self-describing when exit status was not retained.
func TestRenderCmd_JSONFailureEnvelope_HasMatchingExitCode(t *testing.T) {
	svc := &spyService{
		renderAgentErr: app.ErrRenderAgentNotInCatalog,
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		append([]string{"render", "--harness", "claude-code", "--source-agent", "bad-key", "--dest", "/out/agent.md"},
			"--output", "json"),
		svc, outBuf, &bytes.Buffer{})

	var got wireRenderFailure
	if err := json.NewDecoder(outBuf).Decode(&got); err != nil {
		t.Fatalf("json.Decode stdout: %v; output:\n%s", err, outBuf.String())
	}
	if got.ExitCode != code {
		t.Errorf("envelope exitCode = %d, actual exit code = %d; they must match", got.ExitCode, code)
	}
}

// TestRenderCmd_DryRunHumanOutput_ContainsDryRunIndicator verifies that a dry-run
// human-readable output includes a visible "dry run" marker so the user knows no files
// were written.
func TestRenderCmd_DryRunHumanOutput_ContainsDryRunIndicator(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{
			SourcePath:            "/agents/agent.md",
			AgentKey:              "agent",
			TargetHarnessID:       "claude-code",
			DestinationPath:       "/out/agent.md",
			DestinationResolvedBy: "explicit",
			DryRun:                true,
		},
	}
	outBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		append(minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"), "--dry-run"),
		svc, outBuf, &bytes.Buffer{})

	lower := strings.ToLower(outBuf.String())
	if !strings.Contains(lower, "dry") {
		t.Errorf("human output does not contain 'dry': %q; want a dry-run indicator visible to the user",
			outBuf.String())
	}
}

// TestRenderCmd_JSONSentinelReasonCodes verifies that every ErrRender* sentinel is mapped
// to its documented stable reason code in the JSON failure envelope. This is the table test
// the design specifies: a code added on one side without the other turns this test red.
func TestRenderCmd_JSONSentinelReasonCodes(t *testing.T) {
	cases := []struct {
		name       string
		serviceErr error
		wantReason string
		args       []string
	}{
		{
			name:       "source required",
			serviceErr: app.ErrRenderSourceRequired,
			wantReason: "source-required",
			// This error is caught by CLI flag validation before the service is called,
			// so we use a service that returns it directly for the reason-code test.
			args: minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		},
		{
			name:       "source ambiguous",
			serviceErr: app.ErrRenderSourceAmbiguous,
			wantReason: "source-ambiguous",
			args:       minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		},
		{
			name:       "source not found",
			serviceErr: app.ErrRenderSourceNotFound,
			wantReason: "source-not-found",
			args:       minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		},
		{
			name:       "source not file",
			serviceErr: app.ErrRenderSourceNotFile,
			wantReason: "source-not-file",
			args:       minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		},
		{
			name:       "agent not in catalog",
			serviceErr: app.ErrRenderAgentNotInCatalog,
			wantReason: "agent-not-in-catalog",
			args:       []string{"render", "--harness", "claude-code", "--source-agent", "bad-key", "--dest", "/out/agent.md"},
		},
		{
			name:       "source not generic",
			serviceErr: app.ErrRenderSourceNotGeneric,
			wantReason: "source-not-generic",
			args:       minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		},
		{
			name:       "invalid role",
			serviceErr: app.ErrRenderInvalidRole,
			wantReason: "invalid-role",
			args:       minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		},
		{
			name:       "harness required",
			serviceErr: app.ErrRenderHarnessRequired,
			wantReason: "harness-required",
			args:       minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		},
		{
			name:       "harness unresolvable",
			serviceErr: app.ErrRenderHarnessUnresolvable,
			wantReason: "harness-unresolvable",
			args:       minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		},
		{
			name:       "destination required",
			serviceErr: app.ErrRenderDestinationRequired,
			wantReason: "destination-required",
			args:       minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		},
		{
			name:       "destination ambiguous",
			serviceErr: app.ErrRenderDestinationAmbiguous,
			wantReason: "destination-ambiguous",
			args:       minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		},
		{
			name:       "destination exists",
			serviceErr: app.ErrRenderDestinationExists,
			wantReason: "destination-exists",
			args:       minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		},
		{
			name:       "unknown workflow",
			serviceErr: app.ErrRenderUnknownWorkflow,
			wantReason: "unknown-workflow",
			args: append(minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
				"--workflows", "nonexistent-wf"),
		},
		{
			name:       "workflows not applicable",
			serviceErr: app.ErrRenderWorkflowsNotApplicable,
			wantReason: "workflows-not-applicable",
			args: append(minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
				"--workflows", "some-wf"),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc := &spyService{renderAgentErr: tc.serviceErr}
			outBuf := &bytes.Buffer{}

			args := append(tc.args, "--output", "json")
			cli.Run(context.Background(), args, svc, outBuf, &bytes.Buffer{})

			if outBuf.Len() == 0 {
				t.Fatalf("stdout empty for error %q; want RenderFailure envelope under --output json", tc.name)
			}
			var got wireRenderFailure
			if err := json.NewDecoder(outBuf).Decode(&got); err != nil {
				t.Fatalf("json.Decode: %v; output:\n%s", err, outBuf.String())
			}
			if got.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q for error %q; "+
					"update the sentinel-to-reason mapping in renderRenderAgentOutput",
					got.Reason, tc.wantReason, tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AD-8: Usage-failure JSON envelope (exit 3 under --output json)
// ---------------------------------------------------------------------------

// TestRenderCmd_UsageFailureJSON_EmitsRenderFailureEnvelope verifies that when the render
// subcommand detects a CLI usage failure (missing required flag) under --output json, it
// writes a RenderFailure envelope to stdout with Error == true, a non-empty Reason, and
// ExitCode == 3. AD-8 states the envelope is populated for usage failures (exit 3) too,
// so a consumer sees the same shape whether the CLI or the service rejected the request.
func TestRenderCmd_UsageFailureJSON_EmitsRenderFailureEnvelope(t *testing.T) {
	svc := &spyService{}
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}

	// Missing --harness is a CLI-detected usage error (exit 3).
	code := cli.Run(context.Background(),
		[]string{"render", "--source", "/agents/agent.md", "--dest", "/out/agent.md", "--output", "json"},
		svc, outBuf, errBuf)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); missing --harness must return ExitUsage",
			code, cli.ExitUsage)
	}
	if svc.renderAgentReq != nil {
		t.Error("svc.RenderAgent was called despite missing --harness; CLI must guard required flags first")
	}
	if outBuf.Len() == 0 {
		t.Fatal("stdout is empty under --output json for a usage failure; " +
			"AD-8 requires the RenderFailure envelope on every non-zero exit, including exit 3")
	}
	var got wireRenderFailure
	if err := json.NewDecoder(outBuf).Decode(&got); err != nil {
		t.Fatalf("json.Decode stdout: %v; want a JSON RenderFailure envelope; output:\n%s",
			err, outBuf.String())
	}
	if !got.Error {
		t.Error("RenderFailure.error = false; want true so a consumer can immediately identify the failure shape")
	}
	if got.Reason == "" {
		t.Error("RenderFailure.reason is empty; want a non-empty stable reason code (e.g. 'harness-required'); " +
			"this is the stable code a consuming tool branches on (AD-8)")
	}
	if got.ExitCode != cli.ExitUsage {
		t.Errorf("RenderFailure.exitCode = %d, want ExitUsage (%d); "+
			"the envelope must be self-describing when the caller does not retain exit status",
			got.ExitCode, cli.ExitUsage)
	}
}

// ---------------------------------------------------------------------------
// AC2.3: Error stream (errOut) content
// ---------------------------------------------------------------------------

// TestRenderCmd_UsageFailure_WritesMessageToErrOut verifies that a CLI-detected usage
// failure (missing required flag) writes a diagnostic message to the error stream. This
// covers the "clear message on the error stream" half of AC2.3 — the exit code half is
// already covered by TestRenderCmd_MissingHarnessFlag_ReturnsExitUsage.
func TestRenderCmd_UsageFailure_WritesMessageToErrOut(t *testing.T) {
	svc := &spyService{}
	errBuf := &bytes.Buffer{}

	// Missing --harness.
	cli.Run(context.Background(),
		[]string{"render", "--source", "/agents/agent.md", "--dest", "/out/agent.md"},
		svc, &bytes.Buffer{}, errBuf)

	if errBuf.Len() == 0 {
		t.Error("errOut is empty for a usage failure; " +
			"AC2.3 requires a clear message on the error stream for every missing required flag")
	}
	// The diagnostic should name the offending flag so the user knows what to fix.
	if !strings.Contains(errBuf.String(), "harness") {
		t.Errorf("errOut %q does not mention 'harness'; "+
			"the diagnostic must name the missing flag so the user knows what to supply",
			errBuf.String())
	}
}

// TestRenderCmd_ServiceError_WritesToErrOut verifies that a service failure writes a
// diagnostic message to the error stream. The design states the diagnostic is written to
// stderr as "error: %v" on both usage and service failure paths.
func TestRenderCmd_ServiceError_WritesToErrOut(t *testing.T) {
	svc := &spyService{
		renderAgentErr: app.ErrRenderAgentNotInCatalog,
	}
	errBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		[]string{"render", "--harness", "claude-code", "--source-agent", "bad-key", "--dest", "/out/agent.md"},
		svc, &bytes.Buffer{}, errBuf)

	if errBuf.Len() == 0 {
		t.Error("errOut is empty for a service failure; " +
			"the implementation must write the diagnostic to stderr (e.g. 'error: <message>')")
	}
}

// TestRenderCmd_ServiceErrorJSON_EnvelopeMessageAgreesWithErrOut verifies that the
// RenderFailure envelope's Message field and the error stream both derive from the same
// service error text, so the two writers cannot disagree. This is the "two writers must
// not disagree" property from AD-8: the design states Message is "the same text written
// to stderr."
func TestRenderCmd_ServiceErrorJSON_EnvelopeMessageAgreesWithErrOut(t *testing.T) {
	svc := &spyService{
		renderAgentErr: app.ErrRenderAgentNotInCatalog,
	}
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		append([]string{"render", "--harness", "claude-code", "--source-agent", "bad-key", "--dest", "/out/agent.md"},
			"--output", "json"),
		svc, outBuf, errBuf)

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d)", code, cli.ExitFailure)
	}
	if outBuf.Len() == 0 {
		t.Fatal("stdout empty; want a RenderFailure envelope under --output json")
	}
	var got wireRenderFailure
	if err := json.NewDecoder(outBuf).Decode(&got); err != nil {
		t.Fatalf("json.Decode stdout: %v; output:\n%s", err, outBuf.String())
	}
	if got.Message == "" {
		t.Error("RenderFailure.message is empty; the envelope must carry the same human diagnostic as stderr")
	}
	if errBuf.Len() == 0 {
		t.Error("errOut is empty; a service failure must write the diagnostic to stderr")
	}
	// Both must derive from the same source: errOut must contain envelope.Message (the design
	// states the two are the same text; stderr may carry an "error: " prefix).
	if !strings.Contains(errBuf.String(), got.Message) {
		t.Errorf("errOut does not contain envelope.Message: "+
			"envelope.Message=%q; errOut=%q; "+
			"AD-8 requires the two writers draw from the same error in one place",
			got.Message, errBuf.String())
	}
}

// ---------------------------------------------------------------------------
// Minor: Human output workflow ids for an orchestrator subject
// ---------------------------------------------------------------------------

// TestRenderCmd_HumanOutput_OrchestratorIncludesWorkflowIDs verifies that when the service
// returns a result with Role "orchestrator" and a non-empty Workflows list, the human-readable
// output includes those workflow ids. The design specifies renderRenderAgentHuman shows
// "the workflow ids written when the agent is an orchestrator."
func TestRenderCmd_HumanOutput_OrchestratorIncludesWorkflowIDs(t *testing.T) {
	wantWorkflows := []string{"tdd-soft", "code-review"}
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{
			SourcePath:            "/agents/orchestrator.md",
			AgentKey:              "orchestrator",
			Role:                  "orchestrator",
			TargetHarnessID:       "claude-code",
			DestinationPath:       "/out/orchestrator.md",
			DestinationResolvedBy: "explicit",
			Workflows:             wantWorkflows,
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		append(minRenderArgs("claude-code", "/agents/orchestrator.md", "/out/orchestrator.md"),
			"--workflows", "tdd-soft,code-review"),
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	output := outBuf.String()
	for _, wf := range wantWorkflows {
		if !strings.Contains(output, wf) {
			t.Errorf("human output does not contain workflow id %q; "+
				"renderRenderAgentHuman must include the workflow ids for an orchestrator subject. "+
				"Output:\n%s", wf, output)
		}
	}
}

// ---------------------------------------------------------------------------
// Minor: Non-JSON failure mode leaves stdout free of the envelope shape
// ---------------------------------------------------------------------------

// TestRenderCmd_ServiceFailureWithoutJSON_StdoutIsEmpty verifies that when a service
// failure occurs without --output json, stdout receives no content. The RenderFailure
// envelope exists only in JSON mode; human-facing output on failure is stderr-only.
func TestRenderCmd_ServiceFailureWithoutJSON_StdoutIsEmpty(t *testing.T) {
	svc := &spyService{
		renderAgentErr: app.ErrRenderAgentNotInCatalog,
	}
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"render", "--harness", "claude-code", "--source-agent", "bad-key", "--dest", "/out/agent.md"},
		svc, outBuf, errBuf)

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d)", code, cli.ExitFailure)
	}
	if outBuf.Len() != 0 {
		t.Errorf("stdout is non-empty without --output json on service failure: %q; "+
			"the RenderFailure envelope must only appear under --output json, never in human mode",
			outBuf.String())
	}
	if errBuf.Len() == 0 {
		t.Error("errOut is empty; a service failure must write the diagnostic to stderr")
	}
}

// ---------------------------------------------------------------------------
// T2.4: Path flag quote-stripping
// ---------------------------------------------------------------------------

// TestRenderCmd_SourceQuotedPath_StripsQuotes verifies that a quoted path supplied via
// --source has its surrounding double-quotes stripped (pathinput.Unquote convention).
func TestRenderCmd_SourceQuotedPath_StripsQuotes(t *testing.T) {
	const rawPath = `"/path with spaces/my-agent.md"`
	const wantPath = `/path with spaces/my-agent.md`
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
	}

	cli.Run(context.Background(),
		[]string{"render", "--harness", "claude-code", "--source", rawPath, "--dest", "/out/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if svc.renderAgentReq.SourcePath != wantPath {
		t.Errorf("SourcePath = %q, want %q (outer quotes stripped by pathinput.Unquote)",
			svc.renderAgentReq.SourcePath, wantPath)
	}
}

// TestRenderCmd_DestQuotedPath_StripsQuotes verifies that a quoted path supplied via
// --dest has its surrounding double-quotes stripped (pathinput.Unquote convention).
func TestRenderCmd_DestQuotedPath_StripsQuotes(t *testing.T) {
	const rawDest = `"/out path/deployed agent.md"`
	const wantDest = `/out path/deployed agent.md`
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: wantDest},
	}

	cli.Run(context.Background(),
		[]string{"render", "--harness", "claude-code", "--source", "/agents/agent.md", "--dest", rawDest},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if svc.renderAgentReq.DestinationPath != wantDest {
		t.Errorf("DestinationPath = %q, want %q (outer quotes stripped by pathinput.Unquote)",
			svc.renderAgentReq.DestinationPath, wantDest)
	}
}

// TestRenderCmd_WorkspaceQuotedPath_StripsQuotes verifies that a quoted path supplied via
// --workspace has its surrounding double-quotes stripped (pathinput.Unquote convention).
func TestRenderCmd_WorkspaceQuotedPath_StripsQuotes(t *testing.T) {
	const rawWorkspace = `"/my workspace root"`
	const wantWorkspace = `/my workspace root`
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/my workspace root/.claude/agents/agent.md"},
	}

	cli.Run(context.Background(),
		[]string{"render", "--harness", "claude-code", "--source", "/agents/agent.md", "--workspace", rawWorkspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if svc.renderAgentReq.WorkspaceRoot != wantWorkspace {
		t.Errorf("WorkspaceRoot = %q, want %q (outer quotes stripped by pathinput.Unquote)",
			svc.renderAgentReq.WorkspaceRoot, wantWorkspace)
	}
}

// ---------------------------------------------------------------------------
// T2.5: Workflow-set flag
// ---------------------------------------------------------------------------

// TestRenderCmd_WorkflowsFlagAbsent_WorkflowsIsNil verifies that when --workflows is not
// supplied, RenderAgentRequest.Workflows is nil (meaning "not specified", distinct from
// "explicitly empty").
func TestRenderCmd_WorkflowsFlagAbsent_WorkflowsIsNil(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
	}

	cli.Run(context.Background(),
		minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if svc.renderAgentReq.Workflows != nil {
		t.Errorf("Workflows = %v, want nil; absent --workflows must produce nil, not an empty slice, "+
			"so the service can distinguish 'not specified' from 'explicitly empty'",
			svc.renderAgentReq.Workflows)
	}
}

// TestRenderCmd_WorkflowsFlagExplicitlyEmpty_WorkflowsIsNonNilEmpty verifies that
// --workflows="" sets RenderAgentRequest.Workflows to a non-nil empty slice (meaning
// "explicitly none", distinct from nil/"not specified").
func TestRenderCmd_WorkflowsFlagExplicitlyEmpty_WorkflowsIsNonNilEmpty(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
	}

	cli.Run(context.Background(),
		append(minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"), "--workflows="),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	if svc.renderAgentReq.Workflows == nil {
		t.Error("Workflows = nil; --workflows='' must produce a non-nil empty slice, "+
			"so the service can distinguish 'explicitly empty' from 'not specified'")
	}
	if len(svc.renderAgentReq.Workflows) != 0 {
		t.Errorf("Workflows = %v, want non-nil empty slice; --workflows='' must produce zero ids",
			svc.renderAgentReq.Workflows)
	}
}

// TestRenderCmd_WorkflowsFlagPopulated_WorkflowsIsIDList verifies that
// --workflows=a,b,c sets RenderAgentRequest.Workflows to ["a", "b", "c"].
func TestRenderCmd_WorkflowsFlagPopulated_WorkflowsIsIDList(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"},
	}

	cli.Run(context.Background(),
		append(minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"), "--workflows=tdd-soft,code-review"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called")
	}
	wantWorkflows := []string{"tdd-soft", "code-review"}
	if len(svc.renderAgentReq.Workflows) != len(wantWorkflows) {
		t.Fatalf("Workflows = %v, want %v", svc.renderAgentReq.Workflows, wantWorkflows)
	}
	for i, w := range wantWorkflows {
		if svc.renderAgentReq.Workflows[i] != w {
			t.Errorf("Workflows[%d] = %q, want %q", i, svc.renderAgentReq.Workflows[i], w)
		}
	}
}

// TestRenderCmd_WorkflowsFlagAbsentVsEmpty_DistinguishableInRequest verifies the critical
// nil/non-nil distinction at the request level: an absent flag and an explicitly empty flag
// must produce different values so the service layer can honour the "not specified" vs
// "explicitly none" contract.
func TestRenderCmd_WorkflowsFlagAbsentVsEmpty_DistinguishableInRequest(t *testing.T) {
	// Absent.
	svcAbsent := &spyService{renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"}}
	cli.Run(context.Background(),
		minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"),
		svcAbsent, &bytes.Buffer{}, &bytes.Buffer{})

	// Explicitly empty.
	svcEmpty := &spyService{renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.md"}}
	cli.Run(context.Background(),
		append(minRenderArgs("claude-code", "/agents/agent.md", "/out/agent.md"), "--workflows="),
		svcEmpty, &bytes.Buffer{}, &bytes.Buffer{})

	if svcAbsent.renderAgentReq == nil || svcEmpty.renderAgentReq == nil {
		t.Fatal("svc.RenderAgent was not called in one or both cases")
	}
	if svcAbsent.renderAgentReq.Workflows != nil {
		t.Errorf("absent flag: Workflows = %v, want nil", svcAbsent.renderAgentReq.Workflows)
	}
	if svcEmpty.renderAgentReq.Workflows == nil {
		t.Error("empty flag: Workflows = nil, want non-nil empty slice")
	}
	// Ensure the two are distinguishable.
	absentIsNil := svcAbsent.renderAgentReq.Workflows == nil
	emptyIsNonNil := svcEmpty.renderAgentReq.Workflows != nil
	if !absentIsNil || !emptyIsNonNil {
		t.Errorf("absent/empty distinction not preserved: absent.Workflows=%v emptyWorkflows=%v",
			svcAbsent.renderAgentReq.Workflows, svcEmpty.renderAgentReq.Workflows)
	}
}
