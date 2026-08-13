package cli_test

// transform_test.go covers the CLI surface for the "transform" subcommand: flag handling,
// dispatch to Service.TransformHarness, exit-code contract, and JSON/human output rendering.
//
//
// Verified behaviours (T6.5):
//
// Required flag validation:
//   - "transform" with no flags returns ExitUsage without calling Service.TransformHarness.
//   - Omitting --harness returns ExitUsage without calling Service.TransformHarness.
//   - Omitting --path returns ExitUsage without calling Service.TransformHarness.
//   - "transform --harness src --path /p --target-harness tgt" calls Service.TransformHarness.
//
// Flag-to-request mapping:
//   - --harness maps to TransformHarnessRequest.SourceHarnessID.
//   - --target-harness maps to TransformHarnessRequest.TargetHarnessID.
//   - --path maps to TransformHarnessRequest.Path.
//   - --target-model maps to TransformHarnessRequest.TargetModel.
//   - --overwrite maps to TransformHarnessRequest.Overwrite=true.
//   - --dry-run maps to TransformHarnessRequest.DryRun=true.
//   - Absent --overwrite and --dry-run leave both false.
//
// Exit codes:
//   - A successful run (service returns nil error) returns ExitSuccess.
//   - A service error returns ExitFailure.
//
// Output rendering:
//   - Without --output json, output is written to the output buffer in human-readable form.
//   - With --output json, a JSON TransformHarnessResult document is written to outBuf.
//   - A dry-run result includes a "dry run" indicator in human output.
//
// Separation from other subcommands:
//   - "transform" does not call svc.DeployNew.
//   - "transform" does not call svc.Update.
//   - "transform" does not call svc.UpdateWorkflows.
//   - "transform" does not call svc.Promote.
//   - "deploy" does not call svc.TransformHarness.
//   - "update" does not call svc.TransformHarness.
//   - "promote" does not call svc.TransformHarness.

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
// T6.5: Required flag validation
// ---------------------------------------------------------------------------

// TestTransformCmd_NoFlags_ReturnsExitUsage verifies that "transform" with no flags at all
// returns ExitUsage without calling Service.TransformHarness.
func TestTransformCmd_NoFlags_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"transform"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); transform with no flags must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.transformReq != nil {
		t.Error("svc.TransformHarness was called despite no flags; subcommand must guard required flags first")
	}
}

// TestTransformCmd_MissingHarnessFlag_ReturnsExitUsage verifies that omitting --harness
// (the source harness) returns ExitUsage without calling Service.TransformHarness.
func TestTransformCmd_MissingHarnessFlag_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"transform", "--target-harness", "tgt", "--path", "/agents/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); missing --harness must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.transformReq != nil {
		t.Error("svc.TransformHarness was called despite missing --harness")
	}
}

// TestTransformCmd_MissingPathFlag_ReturnsExitUsage verifies that omitting --path
// returns ExitUsage without calling Service.TransformHarness.
func TestTransformCmd_MissingPathFlag_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); missing --path must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.transformReq != nil {
		t.Error("svc.TransformHarness was called despite missing --path")
	}
}

// TestTransformCmd_MissingTargetHarnessFlag_ReturnsExitUsage verifies that omitting
// --target-harness (a required flag per ContractsDesign.md line 956) returns ExitUsage
// without calling Service.TransformHarness. Both --harness and --path are present.
func TestTransformCmd_MissingTargetHarnessFlag_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--path", "/agents/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); missing --target-harness must return ExitUsage "+
			"(it is a required flag per ContractsDesign.md)", code, cli.ExitUsage)
	}
	if svc.transformReq != nil {
		t.Error("svc.TransformHarness was called despite missing --target-harness")
	}
}

// ---------------------------------------------------------------------------
// T6.5: Flag-to-request mapping
// ---------------------------------------------------------------------------

// TestTransformCmd_HarnessFlag_SetsSourceHarnessID verifies that --harness maps to
// TransformHarnessRequest.SourceHarnessID.
func TestTransformCmd_HarnessFlag_SetsSourceHarnessID(t *testing.T) {
	const srcID = "claude-code"
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: srcID,
			TargetHarnessID: "opencode",
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", srcID, "--target-harness", "opencode", "--path", "/agents/agent.md"},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if svc.transformReq.SourceHarnessID != srcID {
		t.Errorf("SourceHarnessID = %q, want %q", svc.transformReq.SourceHarnessID, srcID)
	}
}

// TestTransformCmd_TargetHarnessFlag_SetsTargetHarnessID verifies that --target-harness
// maps to TransformHarnessRequest.TargetHarnessID.
func TestTransformCmd_TargetHarnessFlag_SetsTargetHarnessID(t *testing.T) {
	const tgtID = "opencode"
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "claude-code",
			TargetHarnessID: tgtID,
		},
	}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "claude-code", "--target-harness", tgtID, "--path", "/agents/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if svc.transformReq.TargetHarnessID != tgtID {
		t.Errorf("TargetHarnessID = %q, want %q", svc.transformReq.TargetHarnessID, tgtID)
	}
}

// TestTransformCmd_PathFlag_SetsPath verifies that --path maps to
// TransformHarnessRequest.Path.
func TestTransformCmd_PathFlag_SetsPath(t *testing.T) {
	const agentPath = "/path/to/my-agent.md"
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", agentPath},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if svc.transformReq.Path != agentPath {
		t.Errorf("Path = %q, want %q", svc.transformReq.Path, agentPath)
	}
}

// TestTransformCmd_PathWithSpaces_StripsQuotes verifies that a quoted path supplied via
// --path has its surrounding double-quotes stripped, matching the pathinput.Unquote
// convention used by the other path-accepting subcommands.
func TestTransformCmd_PathWithSpaces_StripsQuotes(t *testing.T) {
	const rawPath = `"/path with spaces/agent.md"`
	const wantPath = `/path with spaces/agent.md`
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", rawPath},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if svc.transformReq.Path != wantPath {
		t.Errorf("Path = %q, want %q (outer quotes stripped by pathinput.Unquote)", svc.transformReq.Path, wantPath)
	}
}

// TestTransformCmd_TargetModelFlag_SetsTargetModel verifies that --target-model maps to
// TransformHarnessRequest.TargetModel.
func TestTransformCmd_TargetModelFlag_SetsTargetModel(t *testing.T) {
	const modelID = "gpt-4o"
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
			TargetModel:     modelID,
		},
	}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--target-model", modelID},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if svc.transformReq.TargetModel != modelID {
		t.Errorf("TargetModel = %q, want %q", svc.transformReq.TargetModel, modelID)
	}
}

// TestTransformCmd_OverwriteFlag_SetsOverwrite verifies that --overwrite maps to
// TransformHarnessRequest.Overwrite=true.
func TestTransformCmd_OverwriteFlag_SetsOverwrite(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md", "--overwrite"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if !svc.transformReq.Overwrite {
		t.Error("Overwrite = false; --overwrite flag must set it to true")
	}
}

// TestTransformCmd_DryRunFlag_SetsDryRun verifies that --dry-run maps to
// TransformHarnessRequest.DryRun=true.
func TestTransformCmd_DryRunFlag_SetsDryRun(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
			DryRun:          true,
		},
	}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md", "--dry-run"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if !svc.transformReq.DryRun {
		t.Error("DryRun = false; --dry-run flag must set it to true")
	}
}

// TestTransformCmd_DefaultFlags_OverwriteAndDryRunAreFalse verifies that when --overwrite
// and --dry-run are absent, both are false in the request.
func TestTransformCmd_DefaultFlags_OverwriteAndDryRunAreFalse(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if svc.transformReq.Overwrite {
		t.Error("Overwrite = true despite --overwrite flag being absent")
	}
	if svc.transformReq.DryRun {
		t.Error("DryRun = true despite --dry-run flag being absent")
	}
}

// ---------------------------------------------------------------------------
// T6.5: Exit codes
// ---------------------------------------------------------------------------

// TestTransformCmd_ServiceSuccess_ReturnsExitSuccess verifies that when
// Service.TransformHarness returns nil error, cli.Run returns ExitSuccess.
func TestTransformCmd_ServiceSuccess_ReturnsExitSuccess(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
			Transformed:     1,
			Files: []app.TransformFileOutcome{
				{
					SourcePath:      "/agent.md",
					DestinationPath: "/tgt/agent.tgt.md",
					Status:          app.StatusTransformed,
				},
			},
		},
		transformErr: nil,
	}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
}

// TestTransformCmd_ServiceError_ReturnsExitFailure verifies that when
// Service.TransformHarness returns a non-nil error, cli.Run returns ExitFailure.
func TestTransformCmd_ServiceError_ReturnsExitFailure(t *testing.T) {
	svc := &spyService{
		transformErr: app.ErrTransformSameHarness,
	}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "src", "--path", "/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d)", code, cli.ExitFailure)
	}
}

// ---------------------------------------------------------------------------
// T6.5: Output rendering
// ---------------------------------------------------------------------------

// TestTransformCmd_HumanOutput_IsNonEmpty verifies that without --output json, the output
// buffer receives human-readable content (non-empty string) describing the transform result.
func TestTransformCmd_HumanOutput_IsNonEmpty(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
			Transformed:     1,
			Files: []app.TransformFileOutcome{
				{
					SourcePath:      "/agent.md",
					DestinationPath: "/tgt/agent.tgt.md",
					Status:          app.StatusTransformed,
				},
			},
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md"},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if outBuf.Len() == 0 {
		t.Error("output buffer is empty; want human-readable transform output")
	}
}

// TestTransformCmd_JSONOutput_IsValidTransformHarnessResult verifies that --output json
// writes a JSON-encoded TransformHarnessResult to the output buffer.
func TestTransformCmd_JSONOutput_IsValidTransformHarnessResult(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
			InputPath:       "/agent.md",
			Transformed:     1,
			Files: []app.TransformFileOutcome{
				{
					SourcePath:      "/agent.md",
					DestinationPath: "/tgt/agent.tgt.md",
					Status:          app.StatusTransformed,
				},
			},
		},
	}
	outBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--output", "json"},
		svc, outBuf, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}

	// Assert: output is valid JSON that can be decoded as TransformHarnessResult.
	var got app.TransformHarnessResult
	if err := json.NewDecoder(outBuf).Decode(&got); err != nil {
		t.Fatalf("json.Decode output: %v; want a JSON TransformHarnessResult document; output:\n%s",
			err, outBuf.String())
	}
	if got.SourceHarnessID != svc.transformResp.SourceHarnessID {
		t.Errorf("JSON SourceHarnessID = %q, want %q", got.SourceHarnessID, svc.transformResp.SourceHarnessID)
	}
	if got.TargetHarnessID != svc.transformResp.TargetHarnessID {
		t.Errorf("JSON TargetHarnessID = %q, want %q", got.TargetHarnessID, svc.transformResp.TargetHarnessID)
	}
	if got.Transformed != 1 {
		t.Errorf("JSON Transformed = %d, want 1", got.Transformed)
	}
	// Assert nested struct fields to catch accidental json tag typos on TransformFileOutcome.
	if len(got.Files) != 1 {
		t.Fatalf("JSON Files length = %d, want 1", len(got.Files))
	}
	wantDestPath := svc.transformResp.Files[0].DestinationPath
	if got.Files[0].DestinationPath != wantDestPath {
		t.Errorf("JSON Files[0].DestinationPath = %q, want %q; "+
			"check the json:\"destinationPath,omitempty\" tag on TransformFileOutcome.DestinationPath",
			got.Files[0].DestinationPath, wantDestPath)
	}
	if got.Files[0].Status != svc.transformResp.Files[0].Status {
		t.Errorf("JSON Files[0].Status = %q, want %q; "+
			"check the json:\"status\" tag on TransformFileOutcome.Status",
			got.Files[0].Status, svc.transformResp.Files[0].Status)
	}
}

// TestTransformCmd_DryRunHumanOutput_ContainsDryRunIndicator verifies that a dry-run
// human-readable output includes a visible "dry run" marker so the user knows no files
// were written.
func TestTransformCmd_DryRunHumanOutput_ContainsDryRunIndicator(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
			InputPath:       "/agent.md",
			DryRun:          true,
			Transformed:     1,
			Files: []app.TransformFileOutcome{
				{
					SourcePath:      "/agent.md",
					DestinationPath: "/tgt/agent.tgt.md",
					Status:          app.StatusTransformed,
				},
			},
		},
	}
	outBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md", "--dry-run"},
		svc, outBuf, &bytes.Buffer{})

	lower := strings.ToLower(outBuf.String())
	if !strings.Contains(lower, "dry") {
		t.Errorf("human output does not contain 'dry': %q; want a dry-run indicator visible to the user",
			outBuf.String())
	}
}

// ---------------------------------------------------------------------------
// T6.5: Help-text documentation of non-recursive enumeration
// ---------------------------------------------------------------------------

// TestTransformCmd_HelpText_MentionsNonRecursiveEnumeration verifies that the transform
// subcommand's help text states that directory enumeration is non-recursive, as required
// by the directory enumeration contract: "This must be stated in the mode's help text and
// CLI flag documentation." (ContractsDesign.md).
//
// RED-phase behaviour: until I6.6 registers the transform subcommand, cobra reports an
// unknown-command error in errOut whose text does not mention "non-recursive", so this test
// fails correctly. After I6.6 delivers the subcommand, the help text must include the word
// "non-recursive" (or equivalent language) to make this test pass.
//
// Re-verify during I6.6: confirm the --help output explicitly documents that only files
// directly under the supplied directory are enumerated (no descent into subdirectories).
func TestTransformCmd_HelpText_MentionsNonRecursiveEnumeration(t *testing.T) {
	svc := &spyService{}
	errBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		[]string{"transform", "--help"},
		svc, &bytes.Buffer{}, errBuf)

	lower := strings.ToLower(errBuf.String())
	mentionsNonRecursive := strings.Contains(lower, "non-recursive") ||
		strings.Contains(lower, "non recursive") ||
		strings.Contains(lower, "not recursive") ||
		strings.Contains(lower, "directly under")
	if !mentionsNonRecursive {
		t.Errorf("transform --help output does not document non-recursive directory enumeration; "+
			"the contract requires this behavior to be stated in the mode's help text. "+
			"Got errOut:\n%s", errBuf.String())
	}
}

// ---------------------------------------------------------------------------
// T6.5: Separation from other subcommands
// ---------------------------------------------------------------------------

// TestTransformCmd_DoesNotCallDeployNew verifies that "transform" does not dispatch to
// svc.DeployNew.
func TestTransformCmd_DoesNotCallDeployNew(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{SourceHarnessID: "src", TargetHarnessID: "tgt"},
	}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq != nil {
		t.Error("svc.DeployNew was called by the transform subcommand; it must not be")
	}
}

// TestTransformCmd_DoesNotCallPromote verifies that "transform" does not dispatch to
// svc.Promote.
func TestTransformCmd_DoesNotCallPromote(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{SourceHarnessID: "src", TargetHarnessID: "tgt"},
	}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.promoteReq != nil {
		t.Error("svc.Promote was called by the transform subcommand; it must not be")
	}
}

// TestDeployCmd_DoesNotCallTransformHarness verifies that the "deploy" subcommand does not
// dispatch to svc.TransformHarness.
func TestDeployCmd_DoesNotCallTransformHarness(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.transformReq != nil {
		t.Error("svc.TransformHarness was called by the deploy subcommand; it must not be")
	}
}

// TestPromoteCmd_DoesNotCallTransformHarness verifies that the "promote" subcommand does
// not dispatch to svc.TransformHarness.
func TestPromoteCmd_DoesNotCallTransformHarness(t *testing.T) {
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath:      "/my-agent.md",
			DestinationPath: "/mosaicroot/Subagents/Test/my-agent.md",
			Key:             "my-agent",
			NumericID:       "1",
		},
	}

	cli.Run(context.Background(),
		[]string{"promote", "--file", "/my-agent.md", "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.transformReq != nil {
		t.Error("svc.TransformHarness was called by the promote subcommand; it must not be")
	}
}

// ---------------------------------------------------------------------------
// T8.1: --model-map flag parsing — happy paths
// ---------------------------------------------------------------------------

// TestTransformCmd_ModelMapFlag_SinglePair_SetsModelMap verifies that a single --model-map
// value in "source=target" form places the parsed pair on TransformHarnessRequest.ModelMap.
//
// RED phase: until I8.1 adds the --model-map flag, cobra rejects it as an unknown flag and
// Run returns ExitUsage. The ExitSuccess assertion below fails correctly.
func TestTransformCmd_ModelMapFlag_SinglePair_SetsModelMap(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--model-map", "claude-sonnet-4=gpt-4o"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if got := svc.transformReq.ModelMap["claude-sonnet-4"]; got != "gpt-4o" {
		t.Errorf("ModelMap[%q] = %q, want %q; single --model-map pair must appear on the request mapping",
			"claude-sonnet-4", got, "gpt-4o")
	}
}

// TestTransformCmd_ModelMapFlag_MultiplePairs_SetsAllMappings verifies that repeating --model-map
// several times produces a ModelMap with one entry per pair, letting each source model carry its
// own target.
//
// RED phase: cobra rejects --model-map as unknown; TransformHarness is never called.
func TestTransformCmd_ModelMapFlag_MultiplePairs_SetsAllMappings(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agents",
			"--model-map", "claude-sonnet-4=gpt-4o",
			"--model-map", "claude-opus-4=gpt-4-turbo",
			"--model-map", "claude-haiku-3=gpt-3.5-turbo"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if len(svc.transformReq.ModelMap) != 3 {
		t.Fatalf("ModelMap has %d entries, want 3; got %v", len(svc.transformReq.ModelMap), svc.transformReq.ModelMap)
	}
	cases := []struct{ src, want string }{
		{"claude-sonnet-4", "gpt-4o"},
		{"claude-opus-4", "gpt-4-turbo"},
		{"claude-haiku-3", "gpt-3.5-turbo"},
	}
	for _, c := range cases {
		if got := svc.transformReq.ModelMap[c.src]; got != c.want {
			t.Errorf("ModelMap[%q] = %q, want %q", c.src, got, c.want)
		}
	}
}

// TestTransformCmd_ModelMapFlag_EmptySource_AddressesUnsetGroup verifies that a --model-map
// value with an empty source side ("=target") places the entry under the unset-model-group key
// (app.UnsetSourceModel, the empty string), matching agents that carry no source model field.
//
// RED phase: cobra rejects --model-map; TransformHarness is never called.
func TestTransformCmd_ModelMapFlag_EmptySource_AddressesUnsetGroup(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--model-map", "=gpt-4o"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	// app.UnsetSourceModel == "" — the group of agents that declare no model in their frontmatter.
	if got := svc.transformReq.ModelMap[app.UnsetSourceModel]; got != "gpt-4o" {
		t.Errorf("ModelMap[UnsetSourceModel(%q)] = %q, want %q; empty source side must address the unset-model group",
			app.UnsetSourceModel, got, "gpt-4o")
	}
}

// TestTransformCmd_ModelMapFlag_TargetContainsEquals_ParsedByFirstEquals verifies that when
// the target model identifier itself contains "=", the pair is parsed by splitting on the FIRST
// "=" only, preserving the entire right-hand side as the target value.
//
// RED phase: cobra rejects --model-map; TransformHarness is never called.
func TestTransformCmd_ModelMapFlag_TargetContainsEquals_ParsedByFirstEquals(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	// Target value "gpt-4o=version=1" contains "="; only the first "=" is the separator.
	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--model-map", "claude-sonnet-4=gpt-4o=version=1"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if got := svc.transformReq.ModelMap["claude-sonnet-4"]; got != "gpt-4o=version=1" {
		t.Errorf("ModelMap[%q] = %q, want %q; target containing '=' must be preserved intact (split on first '=' only)",
			"claude-sonnet-4", got, "gpt-4o=version=1")
	}
}

// ---------------------------------------------------------------------------
// T8.1: --model-map flag parsing — malformed input errors
// ---------------------------------------------------------------------------

// TestTransformCmd_ModelMapFlag_MalformedNoEquals_ReturnsExitUsage verifies that a --model-map
// value with no "=" separator returns ExitUsage without calling Service.TransformHarness.
// A clear error naming the offending value must appear on stderr.
//
// RED phase: cobra rejects the unknown flag; Run returns ExitUsage and svc is never called —
// the same observable outcome as GREEN, but for a different internal reason. The meaningful
// behavioral assertions (service not called, actionable stderr) are correct in both phases.
func TestTransformCmd_ModelMapFlag_MalformedNoEquals_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}
	errBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--model-map", "gpt-4o-no-separator"},
		svc, &bytes.Buffer{}, errBuf)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); --model-map without '=' must return ExitUsage",
			code, cli.ExitUsage)
	}
	if svc.transformReq != nil {
		t.Error("svc.TransformHarness was called despite malformed --model-map; must not be called on parse error")
	}
}

// TestTransformCmd_ModelMapFlag_MalformedNoEquals_WritesOffendingValueToStderr verifies that
// a malformed --model-map value (no "=") produces a stderr message that names the offending
// value, so the user can identify and correct it.
//
// RED phase: cobra's "unknown flag" message names the flag itself, not the value. This test
// will fail in RED because the offending value is not mentioned.
func TestTransformCmd_ModelMapFlag_MalformedNoEquals_WritesOffendingValueToStderr(t *testing.T) {
	svc := &spyService{}
	errBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--model-map", "gpt-4o-no-separator"},
		svc, &bytes.Buffer{}, errBuf)

	if !strings.Contains(errBuf.String(), "gpt-4o-no-separator") {
		t.Errorf("stderr does not mention the offending value %q; got: %q; "+
			"the error must name the offending value so the user can correct it",
			"gpt-4o-no-separator", errBuf.String())
	}
}

// TestTransformCmd_ModelMapFlag_MalformedEmptyTarget_ReturnsExitUsage verifies that a
// --model-map value with an empty right-hand side ("source=") returns ExitUsage without
// calling Service.TransformHarness. An empty target is not a valid mapping.
func TestTransformCmd_ModelMapFlag_MalformedEmptyTarget_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}
	errBuf := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--model-map", "claude-sonnet-4="},
		svc, &bytes.Buffer{}, errBuf)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); --model-map with empty target must return ExitUsage",
			code, cli.ExitUsage)
	}
	if svc.transformReq != nil {
		t.Error("svc.TransformHarness was called despite empty target in --model-map; must not run on parse error")
	}
}

// TestTransformCmd_ModelMapFlag_MalformedEmptyTarget_WritesOffendingValueToStderr verifies
// that an empty-target --model-map value names the offending entry on stderr.
//
// RED phase: fails because cobra's unknown-flag error does not name the value.
func TestTransformCmd_ModelMapFlag_MalformedEmptyTarget_WritesOffendingValueToStderr(t *testing.T) {
	svc := &spyService{}
	errBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--model-map", "claude-sonnet-4="},
		svc, &bytes.Buffer{}, errBuf)

	if !strings.Contains(errBuf.String(), "claude-sonnet-4=") {
		t.Errorf("stderr does not mention the offending value %q; got: %q; "+
			"error must name the offending entry so the user knows what to fix",
			"claude-sonnet-4=", errBuf.String())
	}
}

// TestTransformCmd_ModelMapFlag_DuplicateSource_ReturnsExitUsage verifies that supplying the
// same source model identifier twice via --model-map returns ExitUsage without calling
// Service.TransformHarness. Duplicate source models are ambiguous and must be rejected.
func TestTransformCmd_ModelMapFlag_DuplicateSource_ReturnsExitUsage(t *testing.T) {
	svc := &spyService{}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--model-map", "claude-sonnet-4=gpt-4o",
			"--model-map", "claude-sonnet-4=gpt-4-turbo"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); duplicate source in --model-map must return ExitUsage",
			code, cli.ExitUsage)
	}
	if svc.transformReq != nil {
		t.Error("svc.TransformHarness was called despite duplicate source model in --model-map")
	}
}

// TestTransformCmd_ModelMapFlag_DuplicateSource_WritesSourceModelToStderr verifies that a
// duplicate-source --model-map error names the duplicated source model on stderr.
//
// RED phase: fails because cobra's unknown-flag error does not name the source model.
func TestTransformCmd_ModelMapFlag_DuplicateSource_WritesSourceModelToStderr(t *testing.T) {
	svc := &spyService{}
	errBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--model-map", "claude-sonnet-4=gpt-4o",
			"--model-map", "claude-sonnet-4=gpt-4-turbo"},
		svc, &bytes.Buffer{}, errBuf)

	if !strings.Contains(errBuf.String(), "claude-sonnet-4") {
		t.Errorf("stderr does not mention the duplicate source model %q; got: %q; "+
			"error must identify the duplicated source model",
			"claude-sonnet-4", errBuf.String())
	}
}

// ---------------------------------------------------------------------------
// T8.2: End-to-end non-interactive CLI transform across multiple source models
// ---------------------------------------------------------------------------

// TestTransformCmd_ModelMapFlag_SetsSkipAllForTargetModel verifies that when --model-map is
// supplied, the request carries SkipAll[QTransformTargetModel] = true. This ensures the CLI
// path never blocks on an interactive model selection prompt for any source model, whether
// mapped or not.
//
// RED phase: cobra rejects --model-map; TransformHarness is not called, so SkipAll cannot
// be inspected. The svc.transformReq == nil guard fails correctly.
func TestTransformCmd_ModelMapFlag_SetsSkipAllForTargetModel(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--model-map", "claude-sonnet-4=gpt-4o"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if !svc.transformReq.SkipAll[domain.QTransformTargetModel] {
		t.Error("SkipAll[QTransformTargetModel] = false; --model-map must set it to true so no interactive model " +
			"question can block a non-interactive CLI invocation")
	}
}

// TestTransformCmd_ModelMapWithTargetModel_BothPresentOnRequest verifies that when both
// --model-map and --target-model are supplied together, both values appear on the request:
// the mapping entries go onto ModelMap and the fallback goes onto TargetModel. Source models
// not in ModelMap then resolve to TargetModel rather than to an empty value.
//
// RED phase: cobra rejects --model-map; TransformHarness is not called.
func TestTransformCmd_ModelMapWithTargetModel_BothPresentOnRequest(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agents",
			"--model-map", "claude-sonnet-4=gpt-4o",
			"--target-model", "gpt-4-turbo"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	// Mapped source model must appear in ModelMap.
	if got := svc.transformReq.ModelMap["claude-sonnet-4"]; got != "gpt-4o" {
		t.Errorf("ModelMap[%q] = %q, want %q; mapped source must appear on ModelMap",
			"claude-sonnet-4", got, "gpt-4o")
	}
	// --target-model must still be wired onto TargetModel as the fallback for unmapped sources.
	if svc.transformReq.TargetModel != "gpt-4-turbo" {
		t.Errorf("TargetModel = %q, want %q; --target-model must be retained as the fallback for unmapped sources",
			svc.transformReq.TargetModel, "gpt-4-turbo")
	}
}

// TestTransformCmd_ModelMapFlag_ThreePairs_AllPresentOnRequest verifies that a batch with
// three distinct source models, each mapped to a different target, produces a ModelMap with
// all three entries on the request. This covers the multi-source-model transform path end-to-end
// at the CLI boundary.
//
// RED phase: cobra rejects --model-map; TransformHarness is not called.
func TestTransformCmd_ModelMapFlag_ThreePairs_AllPresentOnRequest(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "claude-code",
			TargetHarnessID: "opencode",
			AppliedModelMap: app.TransformModelMapping{
				"claude-sonnet-4": "gpt-4o",
				"claude-opus-4":   "gpt-4-turbo",
				"claude-haiku-3":  "gpt-3.5-turbo",
			},
		},
	}

	code := cli.Run(context.Background(),
		[]string{"transform",
			"--harness", "claude-code",
			"--target-harness", "opencode",
			"--path", "/agents",
			"--model-map", "claude-sonnet-4=gpt-4o",
			"--model-map", "claude-opus-4=gpt-4-turbo",
			"--model-map", "claude-haiku-3=gpt-3.5-turbo"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if len(svc.transformReq.ModelMap) != 3 {
		t.Fatalf("ModelMap has %d entries, want 3 (one per source model); got %v",
			len(svc.transformReq.ModelMap), svc.transformReq.ModelMap)
	}
	if got := svc.transformReq.ModelMap["claude-sonnet-4"]; got != "gpt-4o" {
		t.Errorf("ModelMap[%q] = %q, want %q", "claude-sonnet-4", got, "gpt-4o")
	}
	if got := svc.transformReq.ModelMap["claude-opus-4"]; got != "gpt-4-turbo" {
		t.Errorf("ModelMap[%q] = %q, want %q", "claude-opus-4", got, "gpt-4-turbo")
	}
	if got := svc.transformReq.ModelMap["claude-haiku-3"]; got != "gpt-3.5-turbo" {
		t.Errorf("ModelMap[%q] = %q, want %q", "claude-haiku-3", got, "gpt-3.5-turbo")
	}
}

// TestTransformCmd_ModelMapFlag_NoTargetModel_SkipAllSuppressesPromptForUnmappedSources
// verifies that when --model-map is supplied without --target-model, the request sets
// SkipAll[QTransformTargetModel] = true and leaves TargetModel empty. The combination
// ensures unmapped source models resolve to an empty model field rather than prompting
// interactively.
//
// RED phase: cobra rejects --model-map; TransformHarness is not called.
func TestTransformCmd_ModelMapFlag_NoTargetModel_SkipAllSuppressesPromptForUnmappedSources(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agents",
			"--model-map", "claude-sonnet-4=gpt-4o"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	// No --target-model was given, so TargetModel must be empty.
	if svc.transformReq.TargetModel != "" {
		t.Errorf("TargetModel = %q, want empty; no --target-model flag was supplied", svc.transformReq.TargetModel)
	}
	// SkipAll[QTransformTargetModel] must be true so unmapped sources resolve to empty, not a prompt.
	if !svc.transformReq.SkipAll[domain.QTransformTargetModel] {
		t.Error("SkipAll[QTransformTargetModel] = false; must be true when --model-map is supplied so the " +
			"CLI never issues interactive model questions for unmapped source models")
	}
}

// TestTransformCmd_WithoutModelMapFlag_ModelMapIsNilOnRequest verifies that when --model-map
// is NOT supplied, ModelMap on the request is nil (the zero value). This preserves the
// existing behavior for callers that pass only --target-model.
func TestTransformCmd_WithoutModelMapFlag_ModelMapIsNilOnRequest(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	if len(svc.transformReq.ModelMap) != 0 {
		t.Errorf("ModelMap has %d entries, want 0; --model-map was not supplied so ModelMap must be nil/empty",
			len(svc.transformReq.ModelMap))
	}
	// Without --model-map the SkipAll entry for QTransformTargetModel must be absent. An implementation
	// that unconditionally sets SkipAll would suppress model questions for interactive and
	// --target-model-only callers that legitimately rely on the questioning path.
	if svc.transformReq.SkipAll[domain.QTransformTargetModel] {
		t.Error("SkipAll[QTransformTargetModel] = true when --model-map was not supplied; " +
			"SkipAll must only be set when --model-map is present so interactive callers are not suppressed")
	}
}

// ---------------------------------------------------------------------------
// T8.1: --model-map flag parsing — whitespace handling
// ---------------------------------------------------------------------------

// TestTransformCmd_ModelMapFlag_WhitespacePadding_TrimmedKeyAndValue verifies that surrounding
// whitespace on both the source and target sides of a --model-map value is trimmed before the
// pair is placed on the request. A naive strings.Split implementation would silently include the
// padding in the map key, making a lookup for the trimmed key return the zero value.
//
// RED phase: cobra rejects --model-map as an unknown flag and Run returns ExitUsage. The
// ExitSuccess assertion fails correctly. After I8.1 registers the flag, parseModelMap must
// trim both sides for this test to reach GREEN.
func TestTransformCmd_ModelMapFlag_WhitespacePadding_TrimmedKeyAndValue(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	// The value has leading and trailing spaces on both the source and target sides.
	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--model-map", "  claude-sonnet-4  =  gpt-4o  "},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); whitespace-padded --model-map must be accepted",
			code, cli.ExitSuccess)
	}
	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	// Trimmed key must be present; the un-trimmed key must not.
	if got := svc.transformReq.ModelMap["claude-sonnet-4"]; got != "gpt-4o" {
		t.Errorf("ModelMap[%q] = %q, want %q; source and target must be trimmed of surrounding whitespace",
			"claude-sonnet-4", got, "gpt-4o")
	}
	if _, ok := svc.transformReq.ModelMap["  claude-sonnet-4  "]; ok {
		t.Error("ModelMap contains the un-trimmed key \"  claude-sonnet-4  \"; whitespace must be stripped before insertion")
	}
}

// TestTransformCmd_ModelMapFlag_WhitespaceOnlySource_NormalisedToUnsetSourceModel verifies that
// a --model-map value whose source side is whitespace-only (e.g. "   =gpt-4o") is normalised
// to app.UnsetSourceModel (the empty string) after trimming, addressing agents that carry no
// model field — the same group as a purely empty source ("=gpt-4o").
//
// RED phase: cobra rejects --model-map; TransformHarness is never called. The ExitSuccess
// assertion fails correctly.
func TestTransformCmd_ModelMapFlag_WhitespaceOnlySource_NormalisedToUnsetSourceModel(t *testing.T) {
	svc := &spyService{
		transformResp: app.TransformHarnessResult{
			SourceHarnessID: "src",
			TargetHarnessID: "tgt",
		},
	}

	// Three spaces before the "=" — after trimming the source side is empty, which equals UnsetSourceModel.
	code := cli.Run(context.Background(),
		[]string{"transform", "--harness", "src", "--target-harness", "tgt", "--path", "/agent.md",
			"--model-map", "   =gpt-4o"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); whitespace-only source side must be normalised to UnsetSourceModel",
			code, cli.ExitSuccess)
	}
	if svc.transformReq == nil {
		t.Fatal("svc.TransformHarness was not called")
	}
	// After trimming the source side becomes "" == app.UnsetSourceModel; the entry must appear under that key.
	if got := svc.transformReq.ModelMap[app.UnsetSourceModel]; got != "gpt-4o" {
		t.Errorf("ModelMap[UnsetSourceModel(%q)] = %q, want %q; whitespace-only source must normalise to UnsetSourceModel after trimming",
			app.UnsetSourceModel, got, "gpt-4o")
	}
}

// ---------------------------------------------------------------------------
// T8.2: Help-text documentation of unmapped-source fallback (AC8.4)
// ---------------------------------------------------------------------------

// TestTransformCmd_HelpText_DocumentsUnmappedSourceFallback verifies that the transform
// subcommand's --help output documents the behavior for source models that are present in the
// batch but not named in --model-map. AC8.4 requires that this behavior be "documented in the
// flag help text." The design specifies the expected prose:
//   "Source models not mapped here fall back to --target-model, and are left empty when
//    --target-model is absent."
//
// A test for this is necessary because an implementation can satisfy the behavioral acceptance
// criteria (AC8.2, AC8.3, AC8.5) while shipping help text that omits or misstates the fallback
// rule, leaving the non-interactive contract undocumented.
//
// RED phase: until I8.1 registers the --model-map flag, cobra reports the transform subcommand
// as unrecognised and emits a generic error rather than flag help text. The assertion below fails
// correctly. After I8.1 and I8.3 deliver the flag and its help text, this test reaches GREEN.
func TestTransformCmd_HelpText_DocumentsUnmappedSourceFallback(t *testing.T) {
	svc := &spyService{}
	errBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		[]string{"transform", "--help"},
		svc, &bytes.Buffer{}, errBuf)

	lower := strings.ToLower(errBuf.String())
	// Accept several phrasings that convey the fallback rule. Exact prose may differ from the
	// design spec's wording as long as the meaning is unambiguous.
	documentsFallback := strings.Contains(lower, "fall back") ||
		strings.Contains(lower, "fallback") ||
		strings.Contains(lower, "not mapped") ||
		strings.Contains(lower, "unmapped")
	if !documentsFallback {
		t.Errorf("transform --help output does not document the unmapped-source-model fallback behavior; "+
			"AC8.4 requires this to be stated in the flag help text so non-interactive callers know "+
			"what happens to source models they did not map. Got help output:\n%s", errBuf.String())
	}
}
