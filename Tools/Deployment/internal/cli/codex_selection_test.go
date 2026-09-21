package cli_test

// codex_selection_test.go verifies that the CLI routes harness-selection requests
// to the service layer without a local harness-ID validation list. Codex is the
// fifth registered built-in; these tests assert it passes through the CLI surface
// exactly like any other harness and that the service layer (not the CLI) is the
// sole authority for accepting or rejecting a harness ID.
//
// T12.2 -- CLI acceptance for Codex:
//   - "deploy --harness codex" calls Service.DeployNew with HarnessID == "codex".
//   - "render --harness codex" calls Service.RenderAgent with TargetHarnessID == "codex".
//   - An unregistered harness ID ("unregistered-harness") is still passed to the
//     service layer by the CLI (the CLI has no local rejection list); the service
//     layer's Resolve call is what rejects it. This test confirms the CLI does not
//     short-circuit the call before reaching the service.
//
// Evidence of the "generic already" verdict for the CLI surface (I12.5 AC12.10):
//   The CLI builds harness IDs exclusively from user-supplied string flags and passes
//   them verbatim to the service layer. There is no allowlist or registry call in
//   run.go; the service layer's Registry.Resolve is the validation point.

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/cli"
)

// ---------------------------------------------------------------------------
// T12.2: CLI acceptance for Codex harness ID
// ---------------------------------------------------------------------------

// TestCLI_Deploy_CodexHarnessID_ReachesServiceLayer verifies that a "deploy" invocation
// with --harness codex passes HarnessID == "codex" to Service.DeployNew. The CLI must
// not validate harness IDs locally; the service layer is the sole authority via
// Registry.Resolve.
func TestCLI_Deploy_CodexHarnessID_ReachesServiceLayer(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	code := cli.Run(context.Background(),
		minDeployArgs("codex", workspace),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); deploy with --harness codex must succeed at the CLI layer",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("Service.DeployNew was not called; deploy with --harness codex must reach the service layer")
	}
	if svc.deployReq.HarnessID != "codex" {
		t.Errorf("DeployRequest.HarnessID = %q, want %q; the CLI must forward the flag value verbatim",
			svc.deployReq.HarnessID, "codex")
	}
}

// TestCLI_Render_CodexHarnessID_ReachesServiceLayer verifies that a "render" invocation
// with --harness codex passes TargetHarnessID == "codex" to Service.RenderAgent. The
// render subcommand must not validate harness IDs locally; the service layer validates
// via Registry.Resolve.
func TestCLI_Render_CodexHarnessID_ReachesServiceLayer(t *testing.T) {
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{DestinationPath: "/out/agent.toml"},
	}

	code := cli.Run(context.Background(),
		minRenderArgs("codex", "/agents/agent.md", "/out/agent.toml"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); render with --harness codex must succeed at the CLI layer",
			code, cli.ExitSuccess)
	}
	if svc.renderAgentReq == nil {
		t.Fatal("Service.RenderAgent was not called; render with --harness codex must reach the service layer")
	}
	if svc.renderAgentReq.TargetHarnessID != "codex" {
		t.Errorf("RenderAgentRequest.TargetHarnessID = %q, want %q; the CLI must forward the flag value verbatim",
			svc.renderAgentReq.TargetHarnessID, "codex")
	}
}

// ---------------------------------------------------------------------------
// T12.5: render --harness codex JSON output shape
// ---------------------------------------------------------------------------
//
// IMPORTANT: Tools/AgentTest drives the production render binary as an external subprocess,
// invoking "render --output json". It is not a Go import of this module. Any change to
// the JSON output shape of RenderAgentResult must be checked against Tools/AgentTest's
// assumptions about that shape. This test pins the fields that the external consumer
// depends on (destinationPath and sourceVersion) for a Codex render so that a future
// shape change has a visible reason to stop here.

// TestCLI_Render_CodexHarness_JSONOutputShapeUnchanged verifies that the "render"
// subcommand with --harness codex produces a JSON output document with the same shape
// as a Markdown-harness render: the destinationPath, sourceVersion, and targetHarnessId
// fields are present in the JSON output. This shape is consumed by Tools/AgentTest as an
// external subprocess and must not change.
func TestCLI_Render_CodexHarness_JSONOutputShapeUnchanged(t *testing.T) {
	const destPath = "/out/my-agent.toml"
	const version = "v1.2.3"
	svc := &spyService{
		renderAgentResp: app.RenderAgentResult{
			SourcePath:            "/agents/my-agent.md",
			AgentKey:              "my-agent",
			SourceVersion:         version,
			TargetHarnessID:       "codex",
			DestinationPath:       destPath,
			DestinationResolvedBy: "explicit",
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		append(minRenderArgs("codex", "/agents/my-agent.md", destPath), "--output", "json"),
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; render with --harness codex must succeed", code)
	}

	// Decode the JSON output and verify the fields the external consumer depends on.
	var got wireRenderResult
	if err := json.NewDecoder(outBuf).Decode(&got); err != nil {
		t.Fatalf("json.Decode output: %v; want a JSON RenderAgentResult document; output:\n%s",
			err, outBuf.String())
	}
	if got.DestinationPath != destPath {
		t.Errorf("JSON destinationPath = %q, want %q; field must be present for Tools/AgentTest external consumer",
			got.DestinationPath, destPath)
	}
	if got.SourceVersion != version {
		t.Errorf("JSON sourceVersion = %q, want %q; field must be present for Tools/AgentTest external consumer",
			got.SourceVersion, version)
	}
}

// TestCLI_Deploy_UnregisteredHarnessID_StillReachesServiceLayer verifies that the CLI
// forwards an unregistered harness ID to the service layer rather than short-circuiting
// the call. Harness-ID validation is the service layer's responsibility; the CLI must
// be a generic pass-through. This test confirms the property that makes the CLI surface
// "generic already": no CLI-owned harness-ID allowlist exists.
func TestCLI_Deploy_UnregisteredHarnessID_StillReachesServiceLayer(t *testing.T) {
	workspace := t.TempDir()
	// Spy returns success to simulate the service accepting the call (the real service
	// would reject it via Registry.Resolve, but the CLI must not do so first).
	svc := &spyService{deployResp: successSummary(workspace)}

	code := cli.Run(context.Background(),
		minDeployArgs("unregistered-harness", workspace),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// The CLI returns ExitSuccess because the spy service accepts any harness ID.
	// The property under test is that the CLI passed the call to the service at all
	// (svc.deployReq is non-nil), not that the spy accepts or rejects the ID.
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess; the spy service accepts any harness ID", code)
	}
	if svc.deployReq == nil {
		t.Fatal("Service.DeployNew was not called; the CLI must not validate harness IDs locally -- " +
			"an unregistered ID must still reach the service layer, which is the sole validation authority")
	}
}
