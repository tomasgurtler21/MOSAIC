package cli_test

// transform_test.go covers the CLI surface for the "transform" subcommand: flag handling,
// dispatch to Service.TransformHarness, exit-code contract, and JSON/human output rendering.
//
// All tests in this file are RED-phase TDD tests. They assert the full intended behaviour
// of the "transform" subcommand, which does not yet exist (I6.3 and I6.4 are not delivered).
// Every test that invokes cli.Run with "transform" will receive ExitUsage (unrecognised
// subcommand) until the router is extended.
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
			DestinationPath: "/mosaicroot/Agents/Generic/Agents/Test/my-agent.md",
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
