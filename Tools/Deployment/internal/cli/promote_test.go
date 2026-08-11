package cli_test

// promote_test.go covers the CLI surface for the "promote" subcommand: flag handling,
// dispatch to Service.Promote, exit-code contract, and JSON/human output rendering.
//
// All tests in this file are RED-phase TDD tests. They assert the full intended behaviour
// of the "promote" subcommand and the renderPromoteOutput function, which is already
// implemented. The tests that drive Service.Promote through the spy will PASS (since
// renderPromoteOutput is implemented and spyService captures the call). The tests that
// verify correct promote-specific behaviour in the full flow will FAIL until I9.1-I9.6
// are implemented. Some tests in this file cover CLI wiring that may already be correct;
// those act as regression guards.
//
// Verified behaviours (T9.5):
//
// Flag dispatch:
//   - "promote --file path/to/agent.md" calls Service.Promote with FilePath set.
//   - "--category SomeCategory" maps to PromoteRequest.Category.
//   - "--overwrite" maps to PromoteRequest.Overwrite=true.
//   - "--dry-run" maps to PromoteRequest.DryRun=true.
//   - Omitting "--file" returns ExitUsage without calling Service.Promote.
//
// Exit codes:
//   - A successful promote (service returns nil error) returns ExitSuccess.
//   - A service error returns ExitFailure.
//
// Output rendering:
//   - "promote" without "--output json" writes human-readable output.
//   - "promote --output json" writes a JSON PromoteResult document.
//   - A dry-run result includes the "dry run" prefix in human output.
//
// Separation from other subcommands (T9.8 regression):
//   - The "promote" subcommand does not call svc.DeployNew.
//   - The "promote" subcommand does not call svc.Update.
//   - The "promote" subcommand does not call svc.UpdateWorkflows.
//   - The "deploy" subcommand does not call svc.Promote.
//   - The "update" subcommand does not call svc.Promote.
//   - The "workflows" subcommand does not call svc.Promote.

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
// T9.5: CLI flag dispatch
// ---------------------------------------------------------------------------

// TestPromoteCmd_FileFlagMissing_ReturnsExitUsage verifies that omitting the required
// --file flag causes Run to return ExitUsage without calling Service.Promote.
func TestPromoteCmd_FileFlagMissing_ReturnsExitUsage(t *testing.T) {
	// Arrange
	svc := &spyService{}

	// Act — promote with no --file flag.
	code := cli.Run(context.Background(),
		[]string{"promote"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); missing --file must return ExitUsage", code, cli.ExitUsage)
	}
	if svc.promoteReq != nil {
		t.Error("svc.Promote was called despite missing --file; the subcommand must guard --file first")
	}
}

// TestPromoteCmd_FileFlag_CallsPromoteWithFilePath verifies that --file sets
// PromoteRequest.FilePath and that Service.Promote is called exactly once.
func TestPromoteCmd_FileFlag_CallsPromoteWithFilePath(t *testing.T) {
	// Arrange
	filePath := "/path/to/my-harness-agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath:      filePath,
			DestinationPath: "/mosaicroot/Agents/Generic/Agents/Test/my-harness-agent.md",
			Key:             "my-harness-agent",
			NumericID:       "42",
			Category:        "Test",
		},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.promoteReq == nil {
		t.Fatal("svc.Promote was not called")
	}
	if svc.promoteReq.FilePath != filePath {
		t.Errorf("PromoteRequest.FilePath = %q, want %q", svc.promoteReq.FilePath, filePath)
	}
}

// TestPromoteCmd_CategoryFlag_SetsCategory verifies that --category maps to
// PromoteRequest.Category.
func TestPromoteCmd_CategoryFlag_SetsCategory(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	category := "Audit"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath: filePath,
			Key:        "agent",
			NumericID:  "5",
			Category:   category,
		},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--category", category, "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.promoteReq == nil {
		t.Fatal("svc.Promote was not called")
	}
	if svc.promoteReq.Category != category {
		t.Errorf("PromoteRequest.Category = %q, want %q", svc.promoteReq.Category, category)
	}
}

// TestPromoteCmd_OverwriteFlag_SetsOverwrite verifies that --overwrite maps to
// PromoteRequest.Overwrite=true.
func TestPromoteCmd_OverwriteFlag_SetsOverwrite(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath: filePath,
			Key:        "agent",
			NumericID:  "3",
		},
	}

	// Act
	cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--overwrite", "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if svc.promoteReq == nil {
		t.Fatal("svc.Promote was not called")
	}
	if !svc.promoteReq.Overwrite {
		t.Error("PromoteRequest.Overwrite is false; --overwrite flag must set it to true")
	}
}

// TestPromoteCmd_DryRunFlag_SetsDryRun verifies that --dry-run maps to
// PromoteRequest.DryRun=true.
func TestPromoteCmd_DryRunFlag_SetsDryRun(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath: filePath,
			Key:        "agent",
			NumericID:  "7",
			DryRun:     true,
		},
	}

	// Act
	cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--dry-run", "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if svc.promoteReq == nil {
		t.Fatal("svc.Promote was not called")
	}
	if !svc.promoteReq.DryRun {
		t.Error("PromoteRequest.DryRun is false; --dry-run flag must set it to true")
	}
}

// TestPromoteCmd_DefaultFlagsAbsent_OverwriteAndDryRunAreFalse verifies that when
// --overwrite and --dry-run are not given, the request has Overwrite=false and DryRun=false.
func TestPromoteCmd_DefaultFlagsAbsent_OverwriteAndDryRunAreFalse(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{SourcePath: filePath, Key: "agent", NumericID: "2"},
	}

	// Act
	cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if svc.promoteReq == nil {
		t.Fatal("svc.Promote was not called")
	}
	if svc.promoteReq.Overwrite {
		t.Error("PromoteRequest.Overwrite is true by default; it must be false unless --overwrite is given")
	}
	if svc.promoteReq.DryRun {
		t.Error("PromoteRequest.DryRun is true by default; it must be false unless --dry-run is given")
	}
}

// ---------------------------------------------------------------------------
// Exit codes
// ---------------------------------------------------------------------------

// TestPromoteCmd_ServiceError_ReturnsExitFailure verifies that a service error produces
// ExitFailure and writes the error message to errOut.
func TestPromoteCmd_ServiceError_ReturnsExitFailure(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteErr: app.ErrPromoteDestinationExists,
	}
	errBuf := &bytes.Buffer{}

	// Act
	code := cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, errBuf)

	// Assert
	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d) for service error", code, cli.ExitFailure)
	}
	if !strings.Contains(errBuf.String(), "error:") {
		t.Errorf("errOut = %q; should contain 'error:' prefix for service error", errBuf.String())
	}
}

// TestPromoteCmd_ServiceSuccess_ReturnsExitSuccess verifies that a successful promote
// returns ExitSuccess regardless of the DryRun flag.
func TestPromoteCmd_ServiceSuccess_ReturnsExitSuccess(t *testing.T) {
	filePath := "/path/to/agent.md"
	tests := []struct {
		name   string
		result app.PromoteResult
	}{
		{
			name:   "real promote",
			result: app.PromoteResult{SourcePath: filePath, Key: "agent", NumericID: "1"},
		},
		{
			name:   "dry-run promote",
			result: app.PromoteResult{SourcePath: filePath, Key: "agent", NumericID: "1", DryRun: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &spyService{promoteResp: tc.result}
			code := cli.Run(context.Background(),
				[]string{"promote", "--file", filePath, "--harness", "stub-harness"},
				svc, &bytes.Buffer{}, &bytes.Buffer{})
			if code != cli.ExitSuccess {
				t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Output rendering
// ---------------------------------------------------------------------------

// TestPromoteCmd_HumanOutput_ContainsDestinationAndKey verifies that the default (human)
// output includes the destination path and the agent key.
func TestPromoteCmd_HumanOutput_ContainsDestinationAndKey(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	dest := "/mosaicroot/Agents/Generic/Agents/Audit/agent.md"
	key := "agent"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath:      filePath,
			DestinationPath: dest,
			Key:             key,
			NumericID:       "8",
		},
	}
	outBuf := &bytes.Buffer{}

	// Act
	cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness"},
		svc, outBuf, &bytes.Buffer{})

	// Assert
	out := outBuf.String()
	if !strings.Contains(out, dest) {
		t.Errorf("output %q does not contain destination %q", out, dest)
	}
	if !strings.Contains(out, key) {
		t.Errorf("output %q does not contain key %q", out, key)
	}
}

// TestPromoteCmd_JSONOutput_DecodesAsPromoteResult verifies that --output json emits a
// JSON document that decodes as an app.PromoteResult with the correct fields.
func TestPromoteCmd_JSONOutput_DecodesAsPromoteResult(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	wantResult := app.PromoteResult{
		SourcePath:      filePath,
		DestinationPath: "/mosaicroot/Agents/Generic/Agents/Audit/agent.md",
		Key:             "agent",
		NumericID:       "10",
		Category:        "Audit",
	}
	svc := &spyService{promoteResp: wantResult}
	outBuf := &bytes.Buffer{}

	// Act
	code := cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--output", "json", "--harness", "stub-harness"},
		svc, outBuf, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}

	var gotResult app.PromoteResult
	if err := json.NewDecoder(outBuf).Decode(&gotResult); err != nil {
		t.Fatalf("JSON decode failed: %v\noutput: %q", err, outBuf.String())
	}
	if gotResult.Key != wantResult.Key {
		t.Errorf("JSON Key = %q, want %q", gotResult.Key, wantResult.Key)
	}
	if gotResult.NumericID != wantResult.NumericID {
		t.Errorf("JSON NumericID = %q, want %q", gotResult.NumericID, wantResult.NumericID)
	}
	if gotResult.DestinationPath != wantResult.DestinationPath {
		t.Errorf("JSON DestinationPath = %q, want %q", gotResult.DestinationPath, wantResult.DestinationPath)
	}
}

// TestPromoteCmd_DryRunHumanOutput_ContainsDryRunPrefix verifies that human output for a
// dry-run result contains the "dry run" prefix to clearly indicate no file was written.
func TestPromoteCmd_DryRunHumanOutput_ContainsDryRunPrefix(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath:      filePath,
			DestinationPath: "/dest/agent.md",
			Key:             "agent",
			NumericID:       "3",
			DryRun:          true,
		},
	}
	outBuf := &bytes.Buffer{}

	// Act
	cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--dry-run", "--harness", "stub-harness"},
		svc, outBuf, &bytes.Buffer{})

	// Assert
	out := outBuf.String()
	if !strings.Contains(strings.ToLower(out), "dry run") {
		t.Errorf("human output %q does not contain 'dry run'; dry-run output must signal no file was written", out)
	}
}

// ---------------------------------------------------------------------------
// T9.8: Separation from other subcommands (regression)
// ---------------------------------------------------------------------------

// TestPromoteCmd_DoesNotCallDeployNew verifies that the "promote" subcommand does not
// trigger svc.DeployNew. The two commands are independent flows.
func TestPromoteCmd_DoesNotCallDeployNew(t *testing.T) {
	svc := &spyService{
		promoteResp: app.PromoteResult{Key: "agent", NumericID: "1"},
	}
	cli.Run(context.Background(),
		[]string{"promote", "--file", "/path/agent.md", "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq != nil {
		t.Error("svc.DeployNew was called from the promote subcommand; promote and deploy must be independent")
	}
}

// TestPromoteCmd_DoesNotCallUpdate verifies that the "promote" subcommand does not
// trigger svc.Update.
func TestPromoteCmd_DoesNotCallUpdate(t *testing.T) {
	svc := &spyService{
		promoteResp: app.PromoteResult{Key: "agent", NumericID: "1"},
	}
	cli.Run(context.Background(),
		[]string{"promote", "--file", "/path/agent.md", "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.updateReq != nil {
		t.Error("svc.Update was called from the promote subcommand; promote and update must be independent")
	}
}

// TestPromoteCmd_DoesNotCallUpdateWorkflows verifies that the "promote" subcommand does not
// trigger svc.UpdateWorkflows.
func TestPromoteCmd_DoesNotCallUpdateWorkflows(t *testing.T) {
	svc := &spyService{
		promoteResp: app.PromoteResult{Key: "agent", NumericID: "1"},
	}
	cli.Run(context.Background(),
		[]string{"promote", "--file", "/path/agent.md", "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.workflowsReq != nil {
		t.Error("svc.UpdateWorkflows was called from the promote subcommand; promote and workflows must be independent")
	}
}

// TestDeployCmd_DoesNotCallPromote verifies that the "deploy" subcommand does not trigger
// svc.Promote. This is a regression guard against cross-mode leakage.
func TestDeployCmd_DoesNotCallPromote(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.promoteReq != nil {
		t.Error("svc.Promote was called from the deploy subcommand; deploy and promote must be independent")
	}
}

// TestUpdateCmd_DoesNotCallPromote verifies that the "update" subcommand does not trigger
// svc.Promote. This is a regression guard against cross-mode leakage.
func TestUpdateCmd_DoesNotCallPromote(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		updateResp: successSummary(workspace),
	}
	cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.promoteReq != nil {
		t.Error("svc.Promote was called from the update subcommand; update and promote must be independent")
	}
}

// TestWorkflowsCmd_DoesNotCallPromote verifies that the "workflows" subcommand does not
// trigger svc.Promote. This is a regression guard against cross-mode leakage.
func TestWorkflowsCmd_DoesNotCallPromote(t *testing.T) {
	workspace := t.TempDir()
	svc := &spyService{
		workflowsResp: successSummary(workspace),
	}
	cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.promoteReq != nil {
		t.Error("svc.Promote was called from the workflows subcommand; workflows and promote must be independent")
	}
}

// ---------------------------------------------------------------------------
// Stripped-field and diverted-tool rendering (AC2.3)
// ---------------------------------------------------------------------------

// TestPromoteCmd_HumanOutput_IncludesDivertedTools verifies that diverted tools recovered
// from harness-specific frontmatter fields appear in the human-readable promote output.
// A promote result with DivertedTools must list them, because the operator needs to know
// which tools were recovered from a non-standard field rather than the main tools key.
func TestPromoteCmd_HumanOutput_IncludesDivertedTools(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath:      filePath,
			DestinationPath: "/mosaicroot/Agents/Generic/Agents/Test/agent.md",
			Key:             "agent",
			NumericID:       "15",
			DivertedTools:   []string{"Bash", "Read"},
		},
	}
	outBuf := &bytes.Buffer{}

	// Act
	cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness"},
		svc, outBuf, &bytes.Buffer{})

	// Assert
	out := outBuf.String()
	if !strings.Contains(out, "Bash") {
		t.Errorf("human output %q does not contain diverted tool %q; diverted tools must appear in the promote summary", out, "Bash")
	}
	if !strings.Contains(out, "Read") {
		t.Errorf("human output %q does not contain diverted tool %q; diverted tools must appear in the promote summary", out, "Read")
	}
}

// TestPromoteCmd_HumanOutput_IncludesStrippedFields verifies that stripped frontmatter
// fields appear in the human-readable promote output. A promote result with StrippedFields
// must list each field's key and reason so the operator can review what was removed.
func TestPromoteCmd_HumanOutput_IncludesStrippedFields(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath:      filePath,
			DestinationPath: "/mosaicroot/Agents/Generic/Agents/Test/agent.md",
			Key:             "agent",
			NumericID:       "16",
			StrippedFields: []app.StrippedField{
				{Key: "customField", Values: []string{"val1", "val2"}, Reason: app.StripReasonUnknownField},
				{Key: "mcpServers", Values: []string{"unmapped-server"}, Reason: app.StripReasonUnmappedDivertedValue},
			},
		},
	}
	outBuf := &bytes.Buffer{}

	// Act
	cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness"},
		svc, outBuf, &bytes.Buffer{})

	// Assert
	out := outBuf.String()
	if !strings.Contains(out, "customField") {
		t.Errorf("human output %q does not contain stripped field key %q; stripped fields must appear in the promote summary", out, "customField")
	}
	if !strings.Contains(out, "mcpServers") {
		t.Errorf("human output %q does not contain stripped field key %q; stripped fields must appear in the promote summary", out, "mcpServers")
	}
	if !strings.Contains(out, string(app.StripReasonUnknownField)) {
		t.Errorf("human output %q does not contain strip reason %q; stripped field reasons must appear in the promote summary", out, app.StripReasonUnknownField)
	}
	if !strings.Contains(out, string(app.StripReasonUnmappedDivertedValue)) {
		t.Errorf("human output %q does not contain strip reason %q; stripped field reasons must appear in the promote summary", out, app.StripReasonUnmappedDivertedValue)
	}
}

// TestPromoteCmd_HumanOutput_NoDivertedTools_OmitsDivertedSection verifies that when
// DivertedTools is empty, no diverted-tools line appears in the output. Absent diverted
// tools must not produce a visible but empty section.
func TestPromoteCmd_HumanOutput_NoDivertedTools_OmitsDivertedSection(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath:      filePath,
			DestinationPath: "/dest/agent.md",
			Key:             "agent",
			NumericID:       "17",
			DivertedTools:   nil, // no diverted tools
		},
	}
	outBuf := &bytes.Buffer{}

	// Act
	cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness"},
		svc, outBuf, &bytes.Buffer{})

	// Assert
	out := outBuf.String()
	if strings.Contains(strings.ToLower(out), "diverted") {
		t.Errorf("human output %q contains 'diverted' but no diverted tools were present; empty section must be omitted", out)
	}
}

// TestPromoteCmd_DryRunHumanOutput_IncludesStrippedFields verifies that stripped fields
// also appear in the dry-run human output. A dry-run promote computes everything but writes
// no file; the stripped-field report must still be shown.
func TestPromoteCmd_DryRunHumanOutput_IncludesStrippedFields(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath:      filePath,
			DestinationPath: "/dest/agent.md",
			Key:             "agent",
			NumericID:       "18",
			DryRun:          true,
			StrippedFields: []app.StrippedField{
				{Key: "unknownKey", Values: []string{"someValue"}, Reason: app.StripReasonUnknownField},
			},
		},
	}
	outBuf := &bytes.Buffer{}

	// Act
	cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--dry-run", "--harness", "stub-harness"},
		svc, outBuf, &bytes.Buffer{})

	// Assert
	out := outBuf.String()
	if !strings.Contains(strings.ToLower(out), "dry run") {
		t.Errorf("output %q does not contain 'dry run' marker; dry-run output must signal no file was written", out)
	}
	if !strings.Contains(out, "unknownKey") {
		t.Errorf("dry-run output %q does not contain stripped field key %q; stripped fields must appear even in dry-run output", out, "unknownKey")
	}
}

// ---------------------------------------------------------------------------
// Harness flag on promote subcommand (T1.1)
// ---------------------------------------------------------------------------

// TestPromoteCmd_HarnessFlagMissing_ReturnsExitUsage verifies that when --file is provided
// but --harness is absent, the promote subcommand returns ExitUsage without calling
// Service.Promote. The --harness flag is required; its absence is a usage error.
func TestPromoteCmd_HarnessFlagMissing_ReturnsExitUsage(t *testing.T) {
	// Arrange
	filePath := "/path/to/my-harness-agent.md"
	svc := &spyService{}
	errBuf := &bytes.Buffer{}

	// Act — provide --file but omit --harness.
	code := cli.Run(context.Background(),
		[]string{"promote", "--file", filePath},
		svc, &bytes.Buffer{}, errBuf)

	// Assert
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); missing --harness must be a usage error", code, cli.ExitUsage)
	}
	if svc.promoteReq != nil {
		t.Error("svc.Promote was called despite missing --harness; the subcommand must guard --harness before calling the service")
	}
}

// TestPromoteCmd_HarnessFlagMissing_ErrorMessageNamesHarness verifies that when --harness is
// absent, the error written to errOut names the --harness flag so the user knows which flag
// to add. The message must reference --harness specifically.
func TestPromoteCmd_HarnessFlagMissing_ErrorMessageNamesHarness(t *testing.T) {
	// Arrange
	filePath := "/path/to/my-harness-agent.md"
	svc := &spyService{}
	errBuf := &bytes.Buffer{}

	// Act
	cli.Run(context.Background(),
		[]string{"promote", "--file", filePath},
		svc, &bytes.Buffer{}, errBuf)

	// Assert — the error output must name --harness so the user knows what to add.
	errOut := errBuf.String()
	if !strings.Contains(errOut, "--harness") {
		t.Errorf("errOut = %q; does not contain '--harness'; the usage error message must name the missing flag", errOut)
	}
}

// TestPromoteCmd_FileMissingAndHarnessMissing_ReportsFileFlagError verifies that when both
// --file and --harness are absent, the error names --file rather than --harness. The --file
// check must run before the --harness check so that the caller fixes the most fundamental
// argument first, matching the source-path-before-harness ordering in Service.Promote.
func TestPromoteCmd_FileMissingAndHarnessMissing_ReportsFileFlagError(t *testing.T) {
	// Arrange — neither --file nor --harness provided.
	svc := &spyService{}
	errBuf := &bytes.Buffer{}

	// Act
	code := cli.Run(context.Background(),
		[]string{"promote"},
		svc, &bytes.Buffer{}, errBuf)

	// Assert — usage error is expected.
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}

	// The error message must reference --file, not --harness, because --file is checked first.
	errOut := errBuf.String()
	if !strings.Contains(errOut, "--file") {
		t.Errorf("errOut = %q; does not contain '--file'; when both flags are absent the --file error must be reported first", errOut)
	}
	// --harness must NOT be the reported error when --file is also missing.
	if strings.Contains(errOut, "--harness is required") {
		t.Errorf("errOut = %q; reports --harness error when --file is also missing; --file must be checked and reported first", errOut)
	}
}

// TestPromoteCmd_HarnessFlag_SetsHarnessIDInRequest verifies that when --harness is provided,
// its value is set as PromoteRequest.HarnessID when calling Service.Promote.
func TestPromoteCmd_HarnessFlag_SetsHarnessIDInRequest(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	harnessID := "stub-harness"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath: filePath,
			Key:        "agent",
			NumericID:  "1",
		},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", harnessID},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}
	if svc.promoteReq == nil {
		t.Fatal("svc.Promote was not called")
	}
	if svc.promoteReq.HarnessID != harnessID {
		t.Errorf("PromoteRequest.HarnessID = %q, want %q; --harness flag must set HarnessID in the request", svc.promoteReq.HarnessID, harnessID)
	}
}

// TestPromoteCmd_HarnessAndFileProvided_CallsPromoteOnce verifies that when both --file and
// --harness are present, the promote subcommand calls Service.Promote exactly once.
func TestPromoteCmd_HarnessAndFileProvided_CallsPromoteOnce(t *testing.T) {
	// Arrange
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath: filePath,
			Key:        "agent",
			NumericID:  "2",
		},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); promote with both --file and --harness must succeed", code, cli.ExitSuccess)
	}
	if svc.promoteReq == nil {
		t.Error("svc.Promote was not called; both --file and --harness are present so the service must be invoked")
	}
}

// ---------------------------------------------------------------------------
// Stage 5: Recovered generic-field rendering — CLI surface tests
//
// These tests verify how the CLI adapter surfaces PromoteResult.RecoveredFields to callers
// via the JSON and human output paths. They use the spy service to inject a pre-built
// PromoteResult and assert on the rendered output bytes.
//
// Verified behaviours:
//
//   JSON output:
//     - When PromoteResult.RecoveredFields is non-empty, the JSON document includes a
//       "recoveredFields" array with the expected entries.
//     - When PromoteResult.RecoveredFields is empty, the JSON document omits the
//       "recoveredFields" key entirely (omitempty).
//
//   Human output:
//     - The human rendering path does not crash or panic when PromoteResult.RecoveredFields
//       is non-empty; it produces at least a non-empty stdout.
// ---------------------------------------------------------------------------

// TestPromoteCmd_JSONOutput_RecoveredFields_IncludedWhenNonEmpty verifies that when the
// service returns a PromoteResult with a non-empty RecoveredFields slice, the "--output json"
// rendering includes a "recoveredFields" array with the expected entries.
func TestPromoteCmd_JSONOutput_RecoveredFields_IncludedWhenNonEmpty(t *testing.T) {
	// Arrange — service returns a result with all three recoverable fields.
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath:      filePath,
			DestinationPath: "/generic/Agents/Generic/Agents/Test/agent.md",
			Key:             "agent",
			NumericID:       "1",
			Category:        "Test",
			RecoveredFields: []string{"recommended_tier", "tier_rationale", "required_skills"},
		},
	}
	stdout := &bytes.Buffer{}

	// Act
	code := cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness", "--output", "json"},
		svc, stdout, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, stdout.String())
	}

	rawFields, ok := decoded["recoveredFields"]
	if !ok {
		t.Fatalf("JSON output is missing %q; RecoveredFields must be serialised when non-empty.\nFull output: %s", "recoveredFields", stdout.String())
	}

	var gotFields []string
	if err := json.Unmarshal(rawFields, &gotFields); err != nil {
		t.Fatalf("could not unmarshal recoveredFields as []string: %v", err)
	}

	wantFields := []string{"recommended_tier", "tier_rationale", "required_skills"}
	if len(gotFields) != len(wantFields) {
		t.Errorf("recoveredFields = %v, want %v", gotFields, wantFields)
		return
	}
	for i, want := range wantFields {
		if gotFields[i] != want {
			t.Errorf("recoveredFields[%d] = %q, want %q", i, gotFields[i], want)
		}
	}
}

// TestPromoteCmd_JSONOutput_RecoveredFields_OmittedWhenEmpty verifies that when the
// service returns a PromoteResult with an empty (nil) RecoveredFields slice, the JSON
// document does not include a "recoveredFields" key (omitempty contract).
func TestPromoteCmd_JSONOutput_RecoveredFields_OmittedWhenEmpty(t *testing.T) {
	// Arrange — service returns a result with no recovered fields (all were already present
	// or the user skipped all questions).
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath: filePath,
			Key:        "agent",
			NumericID:  "1",
			Category:   "Test",
			// RecoveredFields intentionally nil/absent
		},
	}
	stdout := &bytes.Buffer{}

	// Act
	code := cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness", "--output", "json"},
		svc, stdout, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, stdout.String())
	}

	if _, ok := decoded["recoveredFields"]; ok {
		t.Errorf("JSON output contains %q even though RecoveredFields is empty; the omitempty tag must suppress the key.\nFull output: %s", "recoveredFields", stdout.String())
	}
}

// TestPromoteCmd_HumanOutput_RecoveredFields_DoesNotCrash verifies that the human-readable
// rendering path does not panic or return a non-zero exit code when PromoteResult.RecoveredFields
// is non-empty. The test does not assert the exact wording of the output, only that the
// adapter handles a non-nil RecoveredFields slice gracefully.
func TestPromoteCmd_HumanOutput_RecoveredFields_DoesNotCrash(t *testing.T) {
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath:      filePath,
			DestinationPath: "/generic/Agents/Generic/Agents/Test/agent.md",
			Key:             "agent",
			NumericID:       "1",
			Category:        "Test",
			RecoveredFields: []string{"recommended_tier", "tier_rationale", "required_skills"},
		},
	}
	stdout := &bytes.Buffer{}

	// Act — human output (no --output json flag)
	code := cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness"},
		svc, stdout, &bytes.Buffer{})

	// Assert — must not crash and must produce some output.
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); human output with RecoveredFields must not crash", code, cli.ExitSuccess)
	}
	if stdout.Len() == 0 {
		t.Error("stdout is empty; promote human output must produce at least some text")
	}
}

// TestPromoteCmd_JSONOutput_SingleRecoveredField_IncludedCorrectly verifies the JSON
// rendering when only one recoverable field was supplied (the most common case when the
// source already carries two of the three fields).
func TestPromoteCmd_JSONOutput_SingleRecoveredField_IncludedCorrectly(t *testing.T) {
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath:      filePath,
			Key:             "agent",
			NumericID:       "1",
			Category:        "Test",
			RecoveredFields: []string{"tier_rationale"}, // only one was newly supplied
		},
	}
	stdout := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness", "--output", "json"},
		svc, stdout, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, stdout.String())
	}

	rawFields, ok := decoded["recoveredFields"]
	if !ok {
		t.Fatalf("JSON output is missing %q for a single-field RecoveredFields.\nFull output: %s", "recoveredFields", stdout.String())
	}

	var gotFields []string
	if err := json.Unmarshal(rawFields, &gotFields); err != nil {
		t.Fatalf("could not unmarshal recoveredFields: %v", err)
	}

	if len(gotFields) != 1 || gotFields[0] != "tier_rationale" {
		t.Errorf("recoveredFields = %v, want [\"tier_rationale\"]", gotFields)
	}
}

// TestPromoteCmd_JSONOutput_RecoveredFields_DoesNotContainSkippedFields verifies that a
// PromoteResult.RecoveredFields containing specific entries does not inadvertently include
// field names that were not answered. This protects the CLI serialisation contract — it
// must pass through exactly what the service reports, nothing more.
func TestPromoteCmd_JSONOutput_RecoveredFields_MatchesServiceResult(t *testing.T) {
	// Service reports only "recommended_tier" was recovered; the other two were skipped.
	filePath := "/path/to/agent.md"
	svc := &spyService{
		promoteResp: app.PromoteResult{
			SourcePath:      filePath,
			Key:             "agent",
			NumericID:       "1",
			Category:        "Test",
			RecoveredFields: []string{"recommended_tier"},
		},
	}
	stdout := &bytes.Buffer{}

	code := cli.Run(context.Background(),
		[]string{"promote", "--file", filePath, "--harness", "stub-harness", "--output", "json"},
		svc, stdout, &bytes.Buffer{})

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d)", code, cli.ExitSuccess)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, stdout.String())
	}

	rawFields, ok := decoded["recoveredFields"]
	if !ok {
		t.Fatalf("JSON output is missing %q.\nFull output: %s", "recoveredFields", stdout.String())
	}

	var gotFields []string
	if err := json.Unmarshal(rawFields, &gotFields); err != nil {
		t.Fatalf("could not unmarshal recoveredFields: %v", err)
	}

	// Must contain exactly one entry — "recommended_tier" — not the other two.
	if len(gotFields) != 1 {
		t.Errorf("recoveredFields length = %d, want 1; the CLI must pass through exactly what the service returned", len(gotFields))
	}
	if len(gotFields) > 0 && gotFields[0] != "recommended_tier" {
		t.Errorf("recoveredFields[0] = %q, want %q", gotFields[0], "recommended_tier")
	}

	for _, unexpected := range []string{"tier_rationale", "required_skills"} {
		for _, f := range gotFields {
			if f == unexpected {
				t.Errorf("recoveredFields contains %q but the service did not report it as recovered; the CLI must not add field names", unexpected)
			}
		}
	}

	_ = strings.Contains // prevent unused-import error
}
