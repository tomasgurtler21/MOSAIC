package cli_test

// pathflag_test.go verifies that every path-accepting CLI flag strips one surrounding pair
// of double-quote characters from the raw flag value before the value reaches the service
// layer. A quoted path and its bare equivalent must produce an identical value in the
// service request.
//
// Flags covered:
//   --workspace  on the deploy, update, and workflows subcommands
//   --file       on the promote subcommand
//   --selections on the deploy subcommand (path to a selections YAML file)
//
// In the TDD RED phase every test in this file FAILS because the CLI passes raw flag
// values to the service without stripping. The failing tests drive the implementation
// in I1.3.

import (
	"bytes"
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/cli"
)

// ---------------------------------------------------------------------------
// --workspace on the deploy subcommand
// ---------------------------------------------------------------------------

// TestDeployCmd_QuotedWorkspaceFlag_StrippedBeforeRequest verifies that when --workspace
// receives a value surrounded by a matched pair of double quotes, those quotes are removed
// before WorkspacePath is set in the DeployRequest.
func TestDeployCmd_QuotedWorkspaceFlag_StrippedBeforeRequest(t *testing.T) {
	rawPath := "/path/to/workspace"
	quotedPath := `"` + rawPath + `"`
	svc := &spyService{deployResp: successSummary(rawPath)}

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", quotedPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq == nil {
		t.Fatal("svc.DeployNew was not called")
	}
	if svc.deployReq.WorkspacePath != rawPath {
		t.Errorf("WorkspacePath = %q, want %q; --workspace with surrounding double quotes must be stripped before the value reaches the service",
			svc.deployReq.WorkspacePath, rawPath)
	}
}

// TestDeployCmd_UnquotedWorkspaceFlag_PassedThroughUnchanged verifies that when --workspace
// receives a bare path with no quotes, the value reaches the service unchanged. This
// establishes the baseline for the paired test above.
func TestDeployCmd_UnquotedWorkspaceFlag_PassedThroughUnchanged(t *testing.T) {
	rawPath := "/path/to/workspace"
	svc := &spyService{deployResp: successSummary(rawPath)}

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", rawPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq == nil {
		t.Fatal("svc.DeployNew was not called")
	}
	if svc.deployReq.WorkspacePath != rawPath {
		t.Errorf("WorkspacePath = %q, want %q; unquoted --workspace must reach the service unchanged",
			svc.deployReq.WorkspacePath, rawPath)
	}
}

// TestDeployCmd_QuotedAndUnquotedWorkspaceFlag_ProduceIdenticalRequest verifies that a
// quoted and an unquoted --workspace value that refer to the same path produce an
// identical DeployRequest.WorkspacePath. This is the core contract for quote stripping:
// the service must not observe the difference between quoted and unquoted input.
func TestDeployCmd_QuotedAndUnquotedWorkspaceFlag_ProduceIdenticalRequest(t *testing.T) {
	rawPath := "/path/to/workspace"
	quotedPath := `"` + rawPath + `"`

	svcQuoted := &spyService{deployResp: successSummary(rawPath)}
	svcUnquoted := &spyService{deployResp: successSummary(rawPath)}

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", quotedPath, "--auto-confirm"},
		svcQuoted, &bytes.Buffer{}, &bytes.Buffer{})

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", rawPath, "--auto-confirm"},
		svcUnquoted, &bytes.Buffer{}, &bytes.Buffer{})

	if svcQuoted.deployReq == nil || svcUnquoted.deployReq == nil {
		t.Fatal("svc.DeployNew was not called for one or both runs")
	}
	if svcQuoted.deployReq.WorkspacePath != svcUnquoted.deployReq.WorkspacePath {
		t.Errorf("quoted --workspace produced WorkspacePath=%q, unquoted produced %q; both forms must yield the same value in the request",
			svcQuoted.deployReq.WorkspacePath, svcUnquoted.deployReq.WorkspacePath)
	}
}

// ---------------------------------------------------------------------------
// --workspace on the update subcommand
// ---------------------------------------------------------------------------

// TestUpdateCmd_QuotedWorkspaceFlag_StrippedBeforeRequest verifies the same quote-stripping
// contract for the update subcommand's --workspace flag.
func TestUpdateCmd_QuotedWorkspaceFlag_StrippedBeforeRequest(t *testing.T) {
	rawPath := "/path/to/workspace"
	quotedPath := `"` + rawPath + `"`
	svc := &spyService{updateResp: successSummary(rawPath)}

	cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", quotedPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.updateReq == nil {
		t.Fatal("svc.Update was not called")
	}
	if svc.updateReq.WorkspacePath != rawPath {
		t.Errorf("WorkspacePath = %q, want %q; --workspace with surrounding double quotes must be stripped before the value reaches the service",
			svc.updateReq.WorkspacePath, rawPath)
	}
}

// TestUpdateCmd_QuotedAndUnquotedWorkspaceFlag_ProduceIdenticalRequest verifies that the
// update subcommand treats a quoted and an unquoted --workspace value identically.
func TestUpdateCmd_QuotedAndUnquotedWorkspaceFlag_ProduceIdenticalRequest(t *testing.T) {
	rawPath := "/path/to/workspace"
	quotedPath := `"` + rawPath + `"`

	svcQuoted := &spyService{updateResp: successSummary(rawPath)}
	svcUnquoted := &spyService{updateResp: successSummary(rawPath)}

	cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", quotedPath, "--auto-confirm"},
		svcQuoted, &bytes.Buffer{}, &bytes.Buffer{})

	cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", rawPath, "--auto-confirm"},
		svcUnquoted, &bytes.Buffer{}, &bytes.Buffer{})

	if svcQuoted.updateReq == nil || svcUnquoted.updateReq == nil {
		t.Fatal("svc.Update was not called for one or both runs")
	}
	if svcQuoted.updateReq.WorkspacePath != svcUnquoted.updateReq.WorkspacePath {
		t.Errorf("quoted --workspace produced WorkspacePath=%q, unquoted produced %q; both forms must yield the same value",
			svcQuoted.updateReq.WorkspacePath, svcUnquoted.updateReq.WorkspacePath)
	}
}

// ---------------------------------------------------------------------------
// --workspace on the workflows subcommand
// ---------------------------------------------------------------------------

// TestWorkflowsCmd_QuotedWorkspaceFlag_StrippedBeforeRequest verifies the same
// quote-stripping contract for the workflows subcommand's --workspace flag.
func TestWorkflowsCmd_QuotedWorkspaceFlag_StrippedBeforeRequest(t *testing.T) {
	rawPath := "/path/to/workspace"
	quotedPath := `"` + rawPath + `"`
	svc := &spyService{workflowsResp: successSummary(rawPath)}

	cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", quotedPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.workflowsReq == nil {
		t.Fatal("svc.UpdateWorkflows was not called")
	}
	if svc.workflowsReq.WorkspacePath != rawPath {
		t.Errorf("WorkspacePath = %q, want %q; --workspace with surrounding double quotes must be stripped before the value reaches the service",
			svc.workflowsReq.WorkspacePath, rawPath)
	}
}

// TestWorkflowsCmd_QuotedAndUnquotedWorkspaceFlag_ProduceIdenticalRequest verifies that
// the workflows subcommand treats a quoted and an unquoted --workspace value identically.
func TestWorkflowsCmd_QuotedAndUnquotedWorkspaceFlag_ProduceIdenticalRequest(t *testing.T) {
	rawPath := "/path/to/workspace"
	quotedPath := `"` + rawPath + `"`

	svcQuoted := &spyService{workflowsResp: successSummary(rawPath)}
	svcUnquoted := &spyService{workflowsResp: successSummary(rawPath)}

	cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", quotedPath, "--auto-confirm"},
		svcQuoted, &bytes.Buffer{}, &bytes.Buffer{})

	cli.Run(context.Background(),
		[]string{"workflows", "--harness", "stub-harness", "--workspace", rawPath, "--auto-confirm"},
		svcUnquoted, &bytes.Buffer{}, &bytes.Buffer{})

	if svcQuoted.workflowsReq == nil || svcUnquoted.workflowsReq == nil {
		t.Fatal("svc.UpdateWorkflows was not called for one or both runs")
	}
	if svcQuoted.workflowsReq.WorkspacePath != svcUnquoted.workflowsReq.WorkspacePath {
		t.Errorf("quoted --workspace produced WorkspacePath=%q, unquoted produced %q; both forms must yield the same value",
			svcQuoted.workflowsReq.WorkspacePath, svcUnquoted.workflowsReq.WorkspacePath)
	}
}

// ---------------------------------------------------------------------------
// --file on the promote subcommand
// ---------------------------------------------------------------------------

// TestPromoteCmd_QuotedFileFlag_StrippedBeforeRequest verifies that when --file receives
// a path surrounded by a matched pair of double quotes, those quotes are removed before
// FilePath is set in the PromoteRequest.
func TestPromoteCmd_QuotedFileFlag_StrippedBeforeRequest(t *testing.T) {
	rawPath := "/path/to/agent.md"
	quotedPath := `"` + rawPath + `"`
	svc := &spyService{promoteResp: app.PromoteResult{Key: "agent", NumericID: "1"}}

	cli.Run(context.Background(),
		[]string{"promote", "--file", quotedPath, "--harness", "stub-harness"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.promoteReq == nil {
		t.Fatal("svc.Promote was not called")
	}
	if svc.promoteReq.FilePath != rawPath {
		t.Errorf("FilePath = %q, want %q; --file with surrounding double quotes must be stripped before the value reaches the service",
			svc.promoteReq.FilePath, rawPath)
	}
}

// TestPromoteCmd_QuotedAndUnquotedFileFlag_ProduceIdenticalRequest verifies that a quoted
// and an unquoted --file value that name the same path produce an identical
// PromoteRequest.FilePath. The service must not observe the difference.
func TestPromoteCmd_QuotedAndUnquotedFileFlag_ProduceIdenticalRequest(t *testing.T) {
	rawPath := "/path/to/agent.md"
	quotedPath := `"` + rawPath + `"`

	svcQuoted := &spyService{promoteResp: app.PromoteResult{Key: "agent", NumericID: "1"}}
	svcUnquoted := &spyService{promoteResp: app.PromoteResult{Key: "agent", NumericID: "1"}}

	cli.Run(context.Background(),
		[]string{"promote", "--file", quotedPath, "--harness", "stub-harness"},
		svcQuoted, &bytes.Buffer{}, &bytes.Buffer{})

	cli.Run(context.Background(),
		[]string{"promote", "--file", rawPath, "--harness", "stub-harness"},
		svcUnquoted, &bytes.Buffer{}, &bytes.Buffer{})

	if svcQuoted.promoteReq == nil || svcUnquoted.promoteReq == nil {
		t.Fatal("svc.Promote was not called for one or both runs")
	}
	if svcQuoted.promoteReq.FilePath != svcUnquoted.promoteReq.FilePath {
		t.Errorf("quoted --file produced FilePath=%q, unquoted produced %q; both forms must yield the same value",
			svcQuoted.promoteReq.FilePath, svcUnquoted.promoteReq.FilePath)
	}
}

// ---------------------------------------------------------------------------
// --selections on the deploy subcommand
// ---------------------------------------------------------------------------

// TestDeployCmd_QuotedSelectionsFlag_FindsSelectionsFile verifies that when --selections
// receives a file path surrounded by a matched pair of double quotes, the quotes are
// stripped before the file is opened. If the quotes are not stripped, the open call fails
// with a "not found" error and svc.DeployNew is never reached. The test asserts the
// service was called, which is only possible if the file was successfully opened.
func TestDeployCmd_QuotedSelectionsFlag_FindsSelectionsFile(t *testing.T) {
	content := "workflows:\n  - quick-fix\n"
	rawPath := writeTempYAML(t, content)
	quotedPath := `"` + rawPath + `"`
	svc := &spyService{deployResp: successSummary(t.TempDir())}
	errBuf := &bytes.Buffer{}

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--selections", quotedPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, errBuf)

	// When the path is not stripped, parseSelectionsFile receives a path with literal
	// quote characters, which does not exist, so the handler returns before calling
	// svc.DeployNew. The only way for svc.deployReq to be non-nil is if the quotes
	// were successfully stripped and the file was opened.
	if svc.deployReq == nil {
		t.Errorf("svc.DeployNew was not called; this means parseSelectionsFile could not open the file — "+
			"the quoted --selections path must be stripped before the file is opened; errOut: %s",
			errBuf.String())
	}
}

// TestDeployCmd_QuotedSelectionsFlag_ProducesSameWorkflowsAsUnquoted verifies that a
// quoted and an unquoted --selections path to the same file both deliver the same
// workflow selections to the service.
func TestDeployCmd_QuotedSelectionsFlag_ProducesSameWorkflowsAsUnquoted(t *testing.T) {
	content := "workflows:\n  - quick-fix\n"
	rawPath := writeTempYAML(t, content)
	quotedPath := `"` + rawPath + `"`

	svcQuoted := &spyService{deployResp: successSummary(t.TempDir())}
	svcUnquoted := &spyService{deployResp: successSummary(t.TempDir())}

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--selections", quotedPath, "--auto-confirm"},
		svcQuoted, &bytes.Buffer{}, &bytes.Buffer{})

	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--selections", rawPath, "--auto-confirm"},
		svcUnquoted, &bytes.Buffer{}, &bytes.Buffer{})

	if svcQuoted.deployReq == nil {
		t.Fatal("svc.DeployNew was not called for the quoted --selections run; quotes were not stripped")
	}
	if svcUnquoted.deployReq == nil {
		t.Fatal("svc.DeployNew was not called for the unquoted --selections run")
	}

	quotedWFs := svcQuoted.deployReq.WorkflowIDs
	unquotedWFs := svcUnquoted.deployReq.WorkflowIDs
	if len(quotedWFs) != len(unquotedWFs) {
		t.Errorf("quoted --selections produced %d workflow(s) %v, unquoted produced %d %v; both must yield the same selections",
			len(quotedWFs), quotedWFs, len(unquotedWFs), unquotedWFs)
		return
	}
	for i := range quotedWFs {
		if quotedWFs[i] != unquotedWFs[i] {
			t.Errorf("workflow[%d]: quoted=%q, unquoted=%q; selections must be identical regardless of quoting",
				i, quotedWFs[i], unquotedWFs[i])
		}
	}
}
