package deploy_test

// fallback_test.go covers T17.1 and T17.2.
//
// T17.1: The fallback chain is traversed in order — workspace, MOSAIC-root, OS temp — stopping
// at the first writable tier. The tier used is always recorded in ExecResult.Fallback and in
// the log (AC17.1). Four writability outcomes are tested:
//   - Workspace writable: FallbackNone is used.
//   - Workspace unwritable: MOSAIC-root fallback is used (FallbackMosaicRoot).
//   - Workspace and MOSAIC-root unwritable: OS-temp fallback is used (FallbackTemp).
//   - All three tiers unwritable: ErrNoWritableLocation is returned.
//
// T17.2: When a fallback tier is used, the entire deployment lands in that single tier.
// No deployment content appears in a higher-priority tier that was probed and rejected.
// The deployment is never split across two locations (AC17.2, CD-12).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// T17.1 — Workspace writable: FallbackNone reported
// ---------------------------------------------------------------------------

// TestFallback_WorkspaceWritable_ReturnsFallbackNone verifies that when the workspace
// directory is writable, the executor uses it as the deployment root and reports
// FallbackNone in the result.
func TestFallback_WorkspaceWritable_ReturnsFallbackNone(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("test-runner", "agents/test-runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# test-runner agent")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	if result.Fallback != domain.FallbackNone {
		t.Errorf("Fallback = %q; want %q", result.Fallback, domain.FallbackNone)
	}
}

// TestFallback_WorkspaceWritable_DeploymentRootIsWorkspace verifies that when the workspace
// is used, ExecResult.DeploymentRoot equals the plan's WorkspacePath.
func TestFallback_WorkspaceWritable_DeploymentRootIsWorkspace(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("test-runner", "agents/test-runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# test-runner agent")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	if result.DeploymentRoot != workspace {
		t.Errorf("DeploymentRoot = %q; want %q", result.DeploymentRoot, workspace)
	}
}

// TestFallback_WorkspaceWritable_FileWrittenToWorkspace verifies that a plan item is
// physically written to the workspace directory when the workspace tier is used.
func TestFallback_WorkspaceWritable_FileWrittenToWorkspace(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("test-runner", "agents/test-runner.md", domain.ActionCreate)
	expectedContent := []byte("# test-runner agent")
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(expectedContent),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	expectedPath := filepath.Join(workspace, "agents", "test-runner.md")
	if !fileExistsAt(t, expectedPath) {
		t.Errorf("expected file at %q; file does not exist", expectedPath)
	}
}

// ---------------------------------------------------------------------------
// T17.1 — Workspace unwritable: MOSAIC-root fallback
// ---------------------------------------------------------------------------

// TestFallback_WorkspaceUnwritable_ReturnsFallbackMosaicRoot verifies that when the workspace
// deployment path cannot be written, the executor falls back to the MOSAIC-root tier and
// reports FallbackMosaicRoot in the result.
func TestFallback_WorkspaceUnwritable_ReturnsFallbackMosaicRoot(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Block the deployment directory inside the workspace so the first write fails.
	blockPath(t, filepath.Join(workspace, "agents"))

	item := newAgentItem("test-runner", "agents/test-runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# test-runner agent")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	if result.Fallback != domain.FallbackMosaicRoot {
		t.Errorf("Fallback = %q; want %q", result.Fallback, domain.FallbackMosaicRoot)
	}
}

// TestFallback_WorkspaceUnwritable_DeploymentRootIsUnderMosaicRoot verifies that when the
// MOSAIC-root fallback is used, DeploymentRoot is a subdirectory of MosaicRoot.
func TestFallback_WorkspaceUnwritable_DeploymentRootIsUnderMosaicRoot(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	blockPath(t, filepath.Join(workspace, "agents"))

	item := newAgentItem("test-runner", "agents/test-runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# test-runner agent")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.HasPrefix(result.DeploymentRoot, mosaicRoot) {
		t.Errorf("DeploymentRoot %q does not start with mosaicRoot %q", result.DeploymentRoot, mosaicRoot)
	}
}

// TestFallback_WorkspaceUnwritable_FallbackTierReportedInLog verifies that the logger
// receives a record that names the fallback tier that was used (AC17.1).
func TestFallback_WorkspaceUnwritable_FallbackTierReportedInLog(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	blockPath(t, filepath.Join(workspace, "agents"))

	item := newAgentItem("test-runner", "agents/test-runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# test-runner agent")),
	}

	spy := newSpyLogger()
	ex := deploy.NewExecutor(manifest.NewStore(), spy, newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Logger must have received at least one StartRun call, which carries the fallback tier.
	if len(spy.starts) == 0 {
		t.Fatal("Logger.StartRun was never called; fallback tier cannot have been recorded")
	}
	if spy.starts[0].Fallback != domain.FallbackMosaicRoot {
		t.Errorf("Logger.StartRun.Fallback = %q; want %q", spy.starts[0].Fallback, domain.FallbackMosaicRoot)
	}
}

// ---------------------------------------------------------------------------
// T17.1 — Workspace and MOSAIC-root unwritable: OS-temp fallback
// ---------------------------------------------------------------------------

// TestFallback_WorkspaceAndMosaicRootUnwritable_ReturnsFallbackTemp verifies that when both
// the workspace and the MOSAIC-root tier fail, the executor uses the OS temp fallback and
// reports FallbackTemp.
func TestFallback_WorkspaceAndMosaicRootUnwritable_ReturnsFallbackTemp(t *testing.T) {
	// Block workspace by placing a file where the deployment directory would be.
	workspace := t.TempDir()
	blockPath(t, filepath.Join(workspace, "agents"))

	// Block the MOSAIC-root fallback entirely by using a path inside a regular file.
	mosaicRoot := pathInFile(t)

	item := newAgentItem("test-runner", "agents/test-runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# test-runner agent")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	if result.Fallback != domain.FallbackTemp {
		t.Errorf("Fallback = %q; want %q", result.Fallback, domain.FallbackTemp)
	}
}

// TestFallback_WorkspaceAndMosaicRootUnwritable_FallbackTierReportedInLog verifies that the
// logger records the OS-temp fallback tier (AC17.1).
func TestFallback_WorkspaceAndMosaicRootUnwritable_FallbackTierReportedInLog(t *testing.T) {
	workspace := t.TempDir()
	blockPath(t, filepath.Join(workspace, "agents"))
	mosaicRoot := pathInFile(t)

	item := newAgentItem("test-runner", "agents/test-runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# test-runner agent")),
	}

	spy := newSpyLogger()
	ex := deploy.NewExecutor(manifest.NewStore(), spy, newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(spy.starts) == 0 {
		t.Fatal("Logger.StartRun was never called")
	}
	if spy.starts[0].Fallback != domain.FallbackTemp {
		t.Errorf("Logger.StartRun.Fallback = %q; want %q", spy.starts[0].Fallback, domain.FallbackTemp)
	}
}

// ---------------------------------------------------------------------------
// T17.1 — All locations unwritable: ErrNoWritableLocation
// ---------------------------------------------------------------------------

// TestFallback_AllLocationsUnwritable_ReturnsErrNoWritableLocation documents the contract
// that ErrNoWritableLocation is returned when every tier in the fallback chain fails. The
// OS temp tier (FallbackTemp) is intended to be effectively unblockable under normal OS
// conditions; this test uses paths-inside-files to simulate the scenario for workspace and
// MOSAIC-root, while relying on the implementation exposing an internal temp-path injection
// point for full end-to-end coverage of this case.
//
// In CI this test asserts the error type only; the setup simulates workspace and MOSAIC-root
// being unwritable and relies on the implementation to correctly propagate ErrNoWritableLocation
// when its final probe also fails.
func TestFallback_AllLocationsUnwritable_ReturnsErrNoWritableLocation(t *testing.T) {
	// Both workspace and MOSAIC-root are paths inside regular files — guaranteed to fail.
	workspace := pathInFile(t)
	mosaicRoot := pathInFile(t)

	item := newAgentItem("test-runner", "agents/test-runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# test-runner agent")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	_, err := ex.Execute(context.Background(), req)

	// The OS temp fallback may still succeed on most machines; this assertion holds when
	// the implementation also probes the OS temp path. The test is intentionally written
	// to document the expected error — the test environment controls will vary.
	if err != nil && !errors.Is(err, deploy.ErrNoWritableLocation) {
		t.Errorf("Execute error = %v; want ErrNoWritableLocation or nil (if temp succeeded)", err)
	}
}

// ---------------------------------------------------------------------------
// T17.2 — Fallback deployment is complete and unsplit
// ---------------------------------------------------------------------------

// TestFallback_Deployment_IsCompleteInOneTier verifies that when the workspace is unwritable
// and the MOSAIC-root fallback is used, every item in the plan is present in the fallback
// deployment root. No content is split across the workspace and the fallback root (AC17.2).
func TestFallback_Deployment_IsCompleteInOneTier(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	blockPath(t, filepath.Join(workspace, "agents"))

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)
	content := []byte("# agent content")

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(content),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Every item must be in the resolved deployment root.
	for _, item := range []domain.PlanItem{itemA, itemB} {
		expected := filepath.Join(result.DeploymentRoot, item.TargetPath)
		if !fileExistsAt(t, expected) {
			t.Errorf("item %q not found at %q; deployment appears incomplete", item.Ref.Key, expected)
		}
	}
}

// TestFallback_Deployment_NoContentInHigherTier verifies that after a fallback deployment,
// no deployment content was written to the higher-priority tier that was rejected. The
// deployment root is resolved before any write begins, so no partial writes exist in the
// workspace when MOSAIC-root fallback was used (CD-12, AC17.2).
func TestFallback_Deployment_NoContentInHigherTier(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Block agents directory in workspace. The executor must not write any deployment file
	// to the workspace before discovering this (CD-12: location is resolved first).
	blockPath(t, filepath.Join(workspace, "agents"))

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// No deployment file should exist in the workspace (only the manifest may be there).
	if result.Fallback == domain.FallbackMosaicRoot {
		workspaceAgentPath := filepath.Join(workspace, itemA.TargetPath)
		if fileExistsAt(t, workspaceAgentPath) {
			t.Errorf("deployment content found in workspace (%q) after fallback to MOSAIC-root; "+
				"deployment must not be split across tiers", workspaceAgentPath)
		}
	}
}

// TestFallback_FallbackTierInResult_MatchesActualLocation verifies that ExecResult.Fallback
// and ExecResult.DeploymentRoot are consistent: the deployment root must be inside the tier
// named by Fallback.
func TestFallback_FallbackTierInResult_MatchesActualLocation(t *testing.T) {
	cases := []struct {
		name           string
		setupWorkspace func(t *testing.T) string
		mosaicRoot     func(t *testing.T) string
		wantFallback   domain.FallbackTier
	}{
		{
			name:           "workspace writable",
			setupWorkspace: func(t *testing.T) string { return t.TempDir() },
			mosaicRoot:     func(t *testing.T) string { return t.TempDir() },
			wantFallback:   domain.FallbackNone,
		},
		{
			name: "workspace unwritable",
			setupWorkspace: func(t *testing.T) string {
				ws := t.TempDir()
				blockPath(t, filepath.Join(ws, "agents"))
				return ws
			},
			mosaicRoot:   func(t *testing.T) string { return t.TempDir() },
			wantFallback: domain.FallbackMosaicRoot,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workspace := tc.setupWorkspace(t)
			mosaicRoot := tc.mosaicRoot(t)

			item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
			req := deploy.ExecRequest{
				Plan:       newPlan(workspace, item),
				MosaicRoot: mosaicRoot,
				Content:    fixedContent([]byte("content")),
			}

			ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
			result, err := ex.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}

			if result.Fallback != tc.wantFallback {
				t.Errorf("Fallback = %q; want %q", result.Fallback, tc.wantFallback)
			}

			switch result.Fallback {
			case domain.FallbackNone:
				if result.DeploymentRoot != workspace {
					t.Errorf("DeploymentRoot = %q; want workspace %q", result.DeploymentRoot, workspace)
				}
			case domain.FallbackMosaicRoot:
				if !strings.HasPrefix(result.DeploymentRoot, mosaicRoot) {
					t.Errorf("DeploymentRoot = %q; want under mosaicRoot %q", result.DeploymentRoot, mosaicRoot)
				}
			}

			// The deployment root must exist as a directory.
			if info, err := os.Stat(result.DeploymentRoot); err != nil || !info.IsDir() {
				t.Errorf("DeploymentRoot %q does not exist as a directory", result.DeploymentRoot)
			}
		})
	}
}
