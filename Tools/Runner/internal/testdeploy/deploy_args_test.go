package testdeploy_test

import (
	"context"
	"testing"

	"mosaic-run/internal/testdeploy"
)

// =============================================================================
// Argument construction: flags passed to mosaic-deploy deploy
// =============================================================================

// TestDeploy_SubcommandName_IsDeployNotRender asserts that Deploy invokes the
// "deploy" subcommand as the first positional argument. Using the wrong
// subcommand silently routes the call to the render handler, which has a
// completely different flag surface.
func TestDeploy_SubcommandName_IsDeployNotRender(t *testing.T) {
	var capturedArgs []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = minimalDeploy(d)

	if len(capturedArgs) == 0 {
		t.Fatal("Deploy passed no arguments to CommandRunner, want at least the subcommand name")
	}
	if capturedArgs[0] != "deploy" {
		t.Errorf("args[0] = %q, want %q (the deploy subcommand, not render or anything else)", capturedArgs[0], "deploy")
	}
}

// TestDeploy_CatalogFolder_PassedAsCatalogFolderFlag asserts that the
// catalogFolder parameter reaches the delegate as --catalog-folder. Without
// this flag the deploy tool reads from its default catalog location instead of
// the test catalog.
func TestDeploy_CatalogFolder_PassedAsCatalogFolderFlag(t *testing.T) {
	var capturedArgs []string
	const catalogPath = "/opt/mosaic/Tools/Runner/TestCatalog"

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), catalogPath, "/mosaic", "/workspace", []string{"auto"}, []string{"smoke-single"}, nil)

	if !containsFlag(capturedArgs, "--catalog-folder", catalogPath) {
		t.Errorf("args = %v, want --catalog-folder %s", capturedArgs, catalogPath)
	}
}

// TestDeploy_Workspace_PassedAsWorkspaceFlag asserts that the workspace
// parameter reaches the delegate as --workspace.
func TestDeploy_Workspace_PassedAsWorkspaceFlag(t *testing.T) {
	var capturedArgs []string
	const workspacePath = "/var/test/workspace-run-42"

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", workspacePath, []string{"auto"}, []string{"smoke-single"}, nil)

	if !containsFlag(capturedArgs, "--workspace", workspacePath) {
		t.Errorf("args = %v, want --workspace %s", capturedArgs, workspacePath)
	}
}

// TestDeploy_Harness_PassedAsHarnessFlag asserts that a harness ID from the
// harnesses slice reaches the delegate as --harness.
func TestDeploy_Harness_PassedAsHarnessFlag(t *testing.T) {
	var capturedArgs []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace", []string{"claude-code"}, []string{"smoke-single"}, nil)

	if !containsFlag(capturedArgs, "--harness", "claude-code") {
		t.Errorf("args = %v, want --harness claude-code", capturedArgs)
	}
}

// TestDeploy_MosaicRoot_PassedAsMosaicRootFlag asserts that a non-empty
// mosaicRoot parameter reaches the delegate as --mosaic-root, so the deploy
// tool resolves agent definitions, log directories, and other root-relative
// resources correctly even when --catalog-folder overrides the catalog location.
func TestDeploy_MosaicRoot_PassedAsMosaicRootFlag(t *testing.T) {
	var capturedArgs []string
	const mosaicRoot = "/home/user/mosaic"

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", mosaicRoot, "/workspace", []string{"auto"}, []string{"smoke-single"}, nil)

	if !containsFlag(capturedArgs, "--mosaic-root", mosaicRoot) {
		t.Errorf("args = %v, want --mosaic-root %s when mosaicRoot is non-empty", capturedArgs, mosaicRoot)
	}
}

// TestDeploy_MosaicRootEmpty_MosaicRootFlagAbsent asserts that when mosaicRoot
// is empty, --mosaic-root is not passed, letting the deploy tool resolve its
// own root. Empty-means-omitted follows the agentdeploy convention.
func TestDeploy_MosaicRootEmpty_MosaicRootFlagAbsent(t *testing.T) {
	var capturedArgs []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "", "/workspace", []string{"auto"}, []string{"smoke-single"}, nil)

	if len(capturedArgs) == 0 {
		t.Fatal("Deploy did not invoke CommandRunner; cannot assert argument absence")
	}
	if flagIndex(capturedArgs, "--mosaic-root") >= 0 {
		t.Errorf("args = %v, want --mosaic-root to be absent when mosaicRoot is empty", capturedArgs)
	}
}

// TestDeploy_AutoConfirmFlag_AlwaysPresent asserts that --auto-confirm is
// always passed. Without it the deployment tool halts on a plan-review gate
// and the non-interactive call fails before writing anything.
func TestDeploy_AutoConfirmFlag_AlwaysPresent(t *testing.T) {
	var capturedArgs []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = minimalDeploy(d)

	found := false
	for _, arg := range capturedArgs {
		if arg == "--auto-confirm" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("args = %v, want --auto-confirm to always be present (plan-review gate fires on every call)", capturedArgs)
	}
}

// TestDeploy_ExecutablePath_PassedToCommandRunner asserts that the configured
// ExecutablePath is what the seam receives — not a hardcoded constant. The
// seam is what makes testing work; the correct path is what makes a real
// invocation reach the right binary.
func TestDeploy_ExecutablePath_PassedToCommandRunner(t *testing.T) {
	const binaryPath = "/usr/local/bin/mosaic-deploy"
	var gotPath string

	d := testdeploy.New(testdeploy.Options{
		ExecutablePath: binaryPath,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			gotPath = path
			return nil, nil, exitSuccess, nil
		},
	})

	_ = minimalDeploy(d)

	if gotPath != binaryPath {
		t.Errorf("CommandRunner received path %q, want the configured ExecutablePath %q", gotPath, binaryPath)
	}
}

// =============================================================================
// Workflows flag guard: --workflows is always emitted
// =============================================================================

// TestDeploy_WorkflowsFlag_AlwaysEmitted_EvenWhenEmpty asserts the critical
// guard: --workflows must be present on the wire even when the workflows list
// is empty. An omitted --workflows flag causes the deploy tool to silently
// resolve to an empty deployment, deploying only the orchestrator while
// reporting success. Always emitting the flag prevents silent empty deployments.
func TestDeploy_WorkflowsFlag_AlwaysEmitted_EvenWhenEmpty(t *testing.T) {
	var capturedArgs []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace", []string{"auto"}, []string{}, nil)

	if flagIndex(capturedArgs, "--workflows") < 0 {
		t.Errorf("args = %v, want --workflows to be present even when the workflows list is empty "+
			"(omitting --workflows causes the deploy tool to silently deploy only the orchestrator)", capturedArgs)
	}
}

// TestDeploy_WorkflowsFlag_PopulatedList_CommaJoinedValues asserts that a
// populated workflows slice reaches the delegate as --workflows with all IDs
// comma-joined in order.
func TestDeploy_WorkflowsFlag_PopulatedList_CommaJoinedValues(t *testing.T) {
	var capturedArgs []string
	workflows := []string{"smoke-single", "findings-loop", "orchestrated-linear"}

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace", []string{"auto"}, workflows, nil)

	idx := flagIndex(capturedArgs, "--workflows")
	if idx < 0 || idx+1 >= len(capturedArgs) {
		t.Fatalf("args = %v, want --workflows <ids> when workflows list is populated", capturedArgs)
	}
	const want = "smoke-single,findings-loop,orchestrated-linear"
	if capturedArgs[idx+1] != want {
		t.Errorf("--workflows value = %q, want %q (comma-joined, order preserved)", capturedArgs[idx+1], want)
	}
}

// TestDeploy_WorkflowsFlag_SingleWorkflow_NoTrailingComma asserts that a
// single-element workflows list produces a plain ID with no comma.
func TestDeploy_WorkflowsFlag_SingleWorkflow_NoTrailingComma(t *testing.T) {
	var capturedArgs []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace", []string{"auto"}, []string{"smoke-single"}, nil)

	idx := flagIndex(capturedArgs, "--workflows")
	if idx < 0 || idx+1 >= len(capturedArgs) {
		t.Fatalf("args = %v, want --workflows <id> for single workflow", capturedArgs)
	}
	if capturedArgs[idx+1] != "smoke-single" {
		t.Errorf("--workflows value = %q, want %q (single ID, no comma)", capturedArgs[idx+1], "smoke-single")
	}
}
