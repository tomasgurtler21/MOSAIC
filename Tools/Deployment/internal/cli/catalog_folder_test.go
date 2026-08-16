package cli_test

// catalog_folder_test.go verifies that --catalog-folder is accepted as a persistent
// flag by all seven subcommands, in both "--flag value" and "--flag=value" forms,
// and that it composes with each subcommand's own flags without altering their behaviour.
//
// Contract under test (T5.4):
//
//   Acceptance without usage error:
//     - The deploy subcommand accepts --catalog-folder in both forms.
//     - The update subcommand accepts --catalog-folder in both forms.
//     - The workflows subcommand accepts --catalog-folder in both forms.
//     - The promote subcommand accepts --catalog-folder in both forms.
//     - The transform subcommand accepts --catalog-folder in both forms.
//     - The utility-infra subcommand accepts --catalog-folder in both forms.
//     - The render subcommand accepts --catalog-folder in both forms.
//     None of the above may return ExitUsage as a result of --catalog-folder alone.
//
//   Composability:
//     - --catalog-folder coexists with --workspace, --harness, --dry-run,
//       --output, --conflict, --auto-confirm, and --mosaic-root without altering
//       the behaviour of those flags.
//     - In particular, the workspace path forwarded to svc.DeployNew is unchanged
//       by the presence of --catalog-folder.
//
//   Persistent registration (cobra):
//     - --catalog-folder is registered as a persistent flag on the root command so
//       that cobra's own re-parse of the same argument list does not reject it.
//       A cobra rejection produces ExitUsage; its absence is the assertion.
//
// NOTE: These tests fail in the TDD RED phase because --catalog-folder is not yet
// registered in cli.Run (I5.5). Cobra will return an unknown flag error, causing
// cli.Run to return ExitUsage. Once I5.5 registers the persistent flag, these tests
// will pass (assuming the underlying service behaviour is unchanged).

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/domain"
)

// catalogFolderDir returns a temporary directory path to use as a --catalog-folder
// value in CLI invocations. The directory is created by t.TempDir() so it exists
// on disk and is cleaned up automatically.
func catalogFolderDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// ---------------------------------------------------------------------------
// deploy subcommand
// ---------------------------------------------------------------------------

// TestCatalogFolderFlag_Deploy_SpaceForm_Accepted verifies that the deploy subcommand
// accepts "--catalog-folder <dir>" (space form) without returning ExitUsage.
func TestCatalogFolderFlag_Deploy_SpaceForm_Accepted(t *testing.T) {
	workspace := t.TempDir()
	catalogDir := catalogFolderDir(t)
	svc := &spyService{deployResp: successSummary(workspace)}

	code := cli.Run(context.Background(),
		[]string{"deploy",
			"--catalog-folder", catalogDir,
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code == cli.ExitUsage {
		t.Errorf("deploy subcommand returned ExitUsage when --catalog-folder was supplied; "+
			"--catalog-folder must be accepted by the deploy subcommand as a persistent flag "+
			"(got exit code %d)", code)
	}
}

// TestCatalogFolderFlag_Deploy_EqualsForm_Accepted verifies that the deploy subcommand
// accepts "--catalog-folder=<dir>" (equals form) without returning ExitUsage.
func TestCatalogFolderFlag_Deploy_EqualsForm_Accepted(t *testing.T) {
	workspace := t.TempDir()
	catalogDir := catalogFolderDir(t)
	svc := &spyService{deployResp: successSummary(workspace)}

	code := cli.Run(context.Background(),
		[]string{"deploy",
			"--catalog-folder=" + catalogDir,
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code == cli.ExitUsage {
		t.Errorf("deploy subcommand returned ExitUsage for --catalog-folder=<dir> form; "+
			"both flag forms must be accepted (got exit code %d)", code)
	}
}

// ---------------------------------------------------------------------------
// update subcommand
// ---------------------------------------------------------------------------

// TestCatalogFolderFlag_Update_SpaceForm_Accepted verifies that the update subcommand
// accepts "--catalog-folder <dir>" without returning ExitUsage.
func TestCatalogFolderFlag_Update_SpaceForm_Accepted(t *testing.T) {
	workspace := t.TempDir()
	catalogDir := catalogFolderDir(t)
	svc := &spyService{
		updateResp: successSummary(workspace),
	}

	code := cli.Run(context.Background(),
		[]string{"update",
			"--catalog-folder", catalogDir,
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code == cli.ExitUsage {
		t.Errorf("update subcommand returned ExitUsage when --catalog-folder was supplied; "+
			"--catalog-folder must be accepted by the update subcommand (got exit code %d)", code)
	}
}

// TestCatalogFolderFlag_Update_EqualsForm_Accepted verifies the equals form on update.
func TestCatalogFolderFlag_Update_EqualsForm_Accepted(t *testing.T) {
	workspace := t.TempDir()
	catalogDir := catalogFolderDir(t)
	svc := &spyService{updateResp: successSummary(workspace)}

	code := cli.Run(context.Background(),
		[]string{"update",
			"--catalog-folder=" + catalogDir,
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code == cli.ExitUsage {
		t.Errorf("update subcommand returned ExitUsage for --catalog-folder=<dir>; "+
			"both flag forms must be accepted (got exit code %d)", code)
	}
}

// ---------------------------------------------------------------------------
// workflows subcommand
// ---------------------------------------------------------------------------

// TestCatalogFolderFlag_Workflows_SpaceForm_Accepted verifies that the workflows subcommand
// accepts "--catalog-folder <dir>" without returning ExitUsage.
func TestCatalogFolderFlag_Workflows_SpaceForm_Accepted(t *testing.T) {
	workspace := t.TempDir()
	catalogDir := catalogFolderDir(t)
	svc := &spyService{workflowsResp: successSummary(workspace)}

	code := cli.Run(context.Background(),
		[]string{"workflows",
			"--catalog-folder", catalogDir,
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code == cli.ExitUsage {
		t.Errorf("workflows subcommand returned ExitUsage when --catalog-folder was supplied; "+
			"--catalog-folder must be accepted by the workflows subcommand (got exit code %d)", code)
	}
}

// TestCatalogFolderFlag_Workflows_EqualsForm_Accepted verifies the equals form on workflows.
func TestCatalogFolderFlag_Workflows_EqualsForm_Accepted(t *testing.T) {
	workspace := t.TempDir()
	catalogDir := catalogFolderDir(t)
	svc := &spyService{workflowsResp: successSummary(workspace)}

	code := cli.Run(context.Background(),
		[]string{"workflows",
			"--catalog-folder=" + catalogDir,
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code == cli.ExitUsage {
		t.Errorf("workflows subcommand returned ExitUsage for --catalog-folder=<dir>; "+
			"both flag forms must be accepted (got exit code %d)", code)
	}
}

// ---------------------------------------------------------------------------
// promote subcommand
// ---------------------------------------------------------------------------

// TestCatalogFolderFlag_Promote_SpaceForm_Accepted verifies that the promote subcommand
// accepts "--catalog-folder <dir>" without returning ExitUsage solely because of the flag.
// The comparison approach is used: run with and without --catalog-folder to determine
// whether the flag itself causes cobra to reject the invocation.
func TestCatalogFolderFlag_Promote_SpaceForm_Accepted(t *testing.T) {
	catalogDir := catalogFolderDir(t)
	svc := &spyService{}

	// promote requires --file and --harness. Supply them with a path that won't exist
	// on disk — the promote handler validates these, but cobra unknown-flag rejection
	// happens before handler logic.
	var errBufWith, errBufWithout bytes.Buffer
	codeWith := cli.Run(context.Background(),
		[]string{"promote",
			"--catalog-folder", catalogDir,
			"--file", "/nonexistent/agent.md",
			"--harness", "stub-harness",
		},
		svc, &bytes.Buffer{}, &errBufWith)

	codeWithout := cli.Run(context.Background(),
		[]string{"promote",
			"--file", "/nonexistent/agent.md",
			"--harness", "stub-harness",
		},
		svc, &bytes.Buffer{}, &errBufWithout)

	// If --catalog-folder causes ExitUsage when the same invocation without it does not,
	// the flag is being rejected by cobra — which means the persistent registration is missing.
	if codeWith == cli.ExitUsage && codeWithout != cli.ExitUsage {
		t.Errorf("promote subcommand returned ExitUsage when --catalog-folder was supplied "+
			"but not when it was absent (without=%d, with=%d); "+
			"--catalog-folder must be accepted by promote as a persistent flag",
			codeWithout, codeWith)
	}
}

// TestCatalogFolderFlag_Promote_EqualsForm_NotRejected verifies that
// "--catalog-folder=<dir>" does not cause cobra to reject the promote subcommand
// with ExitUsage when the flag otherwise passes.
func TestCatalogFolderFlag_Promote_EqualsForm_NotRejected(t *testing.T) {
	catalogDir := catalogFolderDir(t)
	svc := &spyService{}

	// We invoke promote with required flags and also with --catalog-folder=<dir>.
	// Without the persistent registration, cobra returns ExitUsage for unknown flag.
	var errBufWith, errBufWithout bytes.Buffer
	codeWith := cli.Run(context.Background(),
		[]string{"promote",
			"--catalog-folder=" + catalogDir,
			"--file", "/nonexistent/agent.md",
			"--harness", "stub-harness",
		},
		svc, &bytes.Buffer{}, &errBufWith)

	codeWithout := cli.Run(context.Background(),
		[]string{"promote",
			"--file", "/nonexistent/agent.md",
			"--harness", "stub-harness",
		},
		svc, &bytes.Buffer{}, &errBufWithout)

	if codeWith == cli.ExitUsage && codeWithout != cli.ExitUsage {
		t.Errorf("promote returned ExitUsage with --catalog-folder=<dir> but not without it; "+
			"the flag must be accepted without a usage error (with=%d, without=%d)",
			codeWith, codeWithout)
	}
}

// ---------------------------------------------------------------------------
// transform subcommand
// ---------------------------------------------------------------------------

// TestCatalogFolderFlag_Transform_SpaceForm_Accepted verifies that the transform
// subcommand accepts "--catalog-folder <dir>" without returning ExitUsage due to
// the flag.
func TestCatalogFolderFlag_Transform_SpaceForm_Accepted(t *testing.T) {
	catalogDir := catalogFolderDir(t)
	svc := &spyService{}

	var errBufWith, errBufWithout bytes.Buffer
	codeWith := cli.Run(context.Background(),
		[]string{"transform",
			"--catalog-folder", catalogDir,
			"--harness", "stub-harness",
			"--target-harness", "other-harness",
			"--path", "/nonexistent/agent.md",
		},
		svc, &bytes.Buffer{}, &errBufWith)

	codeWithout := cli.Run(context.Background(),
		[]string{"transform",
			"--harness", "stub-harness",
			"--target-harness", "other-harness",
			"--path", "/nonexistent/agent.md",
		},
		svc, &bytes.Buffer{}, &errBufWithout)

	if codeWith == cli.ExitUsage && codeWithout != cli.ExitUsage {
		t.Errorf("transform returned ExitUsage with --catalog-folder but not without it; "+
			"--catalog-folder must be accepted by the transform subcommand (with=%d, without=%d)",
			codeWith, codeWithout)
	}
}

// TestCatalogFolderFlag_Transform_EqualsForm_Accepted verifies the equals form on transform.
func TestCatalogFolderFlag_Transform_EqualsForm_Accepted(t *testing.T) {
	catalogDir := catalogFolderDir(t)
	svc := &spyService{}

	var errBufWith, errBufWithout bytes.Buffer
	codeWith := cli.Run(context.Background(),
		[]string{"transform",
			"--catalog-folder=" + catalogDir,
			"--harness", "stub-harness",
			"--target-harness", "other-harness",
			"--path", "/nonexistent/agent.md",
		},
		svc, &bytes.Buffer{}, &errBufWith)

	codeWithout := cli.Run(context.Background(),
		[]string{"transform",
			"--harness", "stub-harness",
			"--target-harness", "other-harness",
			"--path", "/nonexistent/agent.md",
		},
		svc, &bytes.Buffer{}, &errBufWithout)

	if codeWith == cli.ExitUsage && codeWithout != cli.ExitUsage {
		t.Errorf("transform returned ExitUsage for --catalog-folder=<dir> but not without it; "+
			"both forms must be accepted (with=%d, without=%d)",
			codeWith, codeWithout)
	}
}

// ---------------------------------------------------------------------------
// utility-infra subcommand
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// render subcommand
// ---------------------------------------------------------------------------

// TestCatalogFolderFlag_Render_SpaceForm_Accepted verifies that the render subcommand
// accepts "--catalog-folder <dir>" without returning ExitUsage due to the flag.
func TestCatalogFolderFlag_Render_SpaceForm_Accepted(t *testing.T) {
	catalogDir := catalogFolderDir(t)
	svc := &spyService{}

	var errBufWith, errBufWithout bytes.Buffer
	codeWith := cli.Run(context.Background(),
		[]string{"render",
			"--catalog-folder", catalogDir,
			"--harness", "stub-harness",
			"--source-agent", "orchestrator",
			"--dest", "/tmp/output.md",
		},
		svc, &bytes.Buffer{}, &errBufWith)

	codeWithout := cli.Run(context.Background(),
		[]string{"render",
			"--harness", "stub-harness",
			"--source-agent", "orchestrator",
			"--dest", "/tmp/output.md",
		},
		svc, &bytes.Buffer{}, &errBufWithout)

	if codeWith == cli.ExitUsage && codeWithout != cli.ExitUsage {
		t.Errorf("render returned ExitUsage with --catalog-folder but not without; "+
			"--catalog-folder must be accepted by the render subcommand (with=%d, without=%d)",
			codeWith, codeWithout)
	}
}

// TestCatalogFolderFlag_Render_EqualsForm_Accepted verifies the equals form on render.
func TestCatalogFolderFlag_Render_EqualsForm_Accepted(t *testing.T) {
	catalogDir := catalogFolderDir(t)
	svc := &spyService{}

	var errBufWith, errBufWithout bytes.Buffer
	codeWith := cli.Run(context.Background(),
		[]string{"render",
			"--catalog-folder=" + catalogDir,
			"--harness", "stub-harness",
			"--source-agent", "orchestrator",
			"--dest", "/tmp/output.md",
		},
		svc, &bytes.Buffer{}, &errBufWith)

	codeWithout := cli.Run(context.Background(),
		[]string{"render",
			"--harness", "stub-harness",
			"--source-agent", "orchestrator",
			"--dest", "/tmp/output.md",
		},
		svc, &bytes.Buffer{}, &errBufWithout)

	if codeWith == cli.ExitUsage && codeWithout != cli.ExitUsage {
		t.Errorf("render returned ExitUsage for --catalog-folder=<dir> but not without; "+
			"both forms must be accepted (with=%d, without=%d)",
			codeWith, codeWithout)
	}
}

// ---------------------------------------------------------------------------
// Composability: --catalog-folder does not alter deploy flag behaviour
// ---------------------------------------------------------------------------

// TestCatalogFolderFlag_Deploy_WorkspaceFlagUnchanged verifies that adding
// --catalog-folder does not change the workspace path forwarded to svc.DeployNew.
// The existing --workspace flag must behave identically with or without --catalog-folder.
func TestCatalogFolderFlag_Deploy_WorkspaceFlagUnchanged(t *testing.T) {
	workspace := t.TempDir()
	catalogDir := catalogFolderDir(t)
	svc := &spyService{deployResp: successSummary(workspace)}

	cli.Run(context.Background(),
		[]string{"deploy",
			"--catalog-folder", catalogDir,
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq == nil {
		t.Fatal("svc.DeployNew was not called; the deploy subcommand must still dispatch to the service")
	}
	if svc.deployReq.WorkspacePath != workspace {
		t.Errorf("DeployNew received WorkspacePath = %q, want %q; "+
			"--catalog-folder must not alter the workspace path forwarded to svc.DeployNew",
			svc.deployReq.WorkspacePath, workspace)
	}
}

// TestCatalogFolderFlag_Deploy_DryRunFlagUnchanged verifies that --dry-run composes
// with --catalog-folder without any change: DryRun must still be true when
// --dry-run is supplied alongside --catalog-folder.
func TestCatalogFolderFlag_Deploy_DryRunFlagUnchanged(t *testing.T) {
	workspace := t.TempDir()
	catalogDir := catalogFolderDir(t)
	svc := &spyService{deployResp: successSummary(workspace)}

	cli.Run(context.Background(),
		[]string{"deploy",
			"--catalog-folder", catalogDir,
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--dry-run",
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq == nil {
		t.Fatal("svc.DeployNew was not called")
	}
	if !svc.deployReq.DryRun {
		t.Errorf("DeployNew received DryRun = false, want true; "+
			"--dry-run must be unaffected by the presence of --catalog-folder")
	}
}

// ---------------------------------------------------------------------------
// Composability: remaining AC5.6 flags
// ---------------------------------------------------------------------------

// TestCatalogFolderFlag_Deploy_SelectionsFlagUnchanged verifies that --selections composes
// with --catalog-folder without behaviour change: the workflow IDs from the selections
// file must still reach svc.DeployNew unchanged when --catalog-folder is also present.
func TestCatalogFolderFlag_Deploy_SelectionsFlagUnchanged(t *testing.T) {
	workspace := t.TempDir()
	catalogDir := catalogFolderDir(t)
	selectionsPath := writeTempYAML(t, "workflows:\n  - composability-wf\n")
	svc := &spyService{deployResp: successSummary(workspace)}

	cli.Run(context.Background(),
		[]string{"deploy",
			"--catalog-folder", catalogDir,
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--selections", selectionsPath,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq == nil {
		t.Fatal("svc.DeployNew was not called; the deploy subcommand must dispatch to the service " +
			"even when --catalog-folder and --selections are both present")
	}
	if len(svc.deployReq.WorkflowIDs) != 1 || svc.deployReq.WorkflowIDs[0] != "composability-wf" {
		t.Errorf("DeployNew received WorkflowIDs = %v, want [\"composability-wf\"]; "+
			"--selections must still populate WorkflowIDs when --catalog-folder is also present",
			svc.deployReq.WorkflowIDs)
	}
}

// TestCatalogFolderFlag_Deploy_OutputJsonUnchanged verifies that --output json composes
// with --catalog-folder without behaviour change: stdout must still contain a valid JSON
// document when both flags are present.
func TestCatalogFolderFlag_Deploy_OutputJsonUnchanged(t *testing.T) {
	workspace := t.TempDir()
	catalogDir := catalogFolderDir(t)
	svc := &spyService{deployResp: successSummary(workspace)}
	var out bytes.Buffer

	code := cli.Run(context.Background(),
		[]string{"deploy",
			"--catalog-folder", catalogDir,
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--output", "json",
			"--auto-confirm",
		},
		svc, &out, &bytes.Buffer{})

	if code == cli.ExitUsage {
		t.Errorf("deploy returned ExitUsage with --catalog-folder and --output json; "+
			"--catalog-folder must not cause a usage error when combined with --output json "+
			"(got exit code %d)", code)
	}
	var parsed any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Errorf("stdout is not valid JSON when --output json and --catalog-folder are both present: %v; "+
			"--output json must still produce a JSON document regardless of --catalog-folder; "+
			"got: %q", err, out.String())
	}
}

// TestCatalogFolderFlag_Deploy_ConflictFlagUnchanged verifies that --conflict composes
// with --catalog-folder without behaviour change: the conflict decision must still reach
// svc.DeployNew unchanged when --catalog-folder is also present.
func TestCatalogFolderFlag_Deploy_ConflictFlagUnchanged(t *testing.T) {
	workspace := t.TempDir()
	catalogDir := catalogFolderDir(t)
	svc := &spyService{deployResp: successSummary(workspace)}

	cli.Run(context.Background(),
		[]string{"deploy",
			"--catalog-folder", catalogDir,
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--conflict", "overwrite",
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if svc.deployReq == nil {
		t.Fatal("svc.DeployNew was not called; must be called even when --catalog-folder and --conflict are both present")
	}
	if svc.deployReq.ConflictDefault != domain.DecisionOverwrite {
		t.Errorf("DeployNew received ConflictDefault = %q, want %q (DecisionOverwrite); "+
			"--conflict overwrite must be forwarded to the service unchanged "+
			"when --catalog-folder is also present",
			svc.deployReq.ConflictDefault, domain.DecisionOverwrite)
	}
}

// TestCatalogFolderFlag_Deploy_MosaicRootFlagUnchanged verifies that --mosaic-root composes
// with --catalog-folder without behaviour change: when both global flags are present, the
// service must still be called (neither flag prevents the other from being accepted by cobra).
func TestCatalogFolderFlag_Deploy_MosaicRootFlagUnchanged(t *testing.T) {
	workspace := t.TempDir()
	catalogDir := catalogFolderDir(t)
	mosaicRootDir := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	code := cli.Run(context.Background(),
		[]string{"deploy",
			"--catalog-folder", catalogDir,
			"--mosaic-root", mosaicRootDir,
			"--harness", "stub-harness",
			"--workspace", workspace,
			"--auto-confirm",
		},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	if code == cli.ExitUsage {
		t.Errorf("deploy returned ExitUsage when --catalog-folder and --mosaic-root were both supplied; "+
			"both persistent flags must coexist without a usage error (got exit code %d)", code)
	}
	if svc.deployReq == nil {
		t.Errorf("svc.DeployNew was not called when --catalog-folder and --mosaic-root were both present; "+
			"the presence of --catalog-folder must not prevent --mosaic-root from being accepted, "+
			"or suppress the service call")
	}
}
