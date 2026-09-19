package testdeploy_test

// Tests for the deploy invoker package. The test suite covers:
//   - Argument construction: correct flags for mosaic-deploy deploy subcommand
//   - Workflows flag guard: --workflows always emitted to prevent silent empty deployment
//   - Multi-harness: one subprocess invocation per harness, each with the correct harness ID
//   - Exit-code-to-error mapping: success, failure, usage error
//   - ErrDeployFailed wrapping: errors.Is and errors.As work correctly
//   - Binary-not-found handling: ErrToolUnavailable with diagnosable path
//   - Timeout and cancellation: ErrTimedOut vs caller cancellation
//
// No real mosaic-deploy binary is present in these tests. Every interaction
// with the subprocess goes through the Invoke seam. The seam returns stderr as
// well as stdout so the "tool's message reaches the caller verbatim" property
// is assertable without any process involved.
//
// These tests are written for the TDD RED phase. They compile and run, but
// fail because the stub implementation has no real logic. When the real
// implementation is written, all tests must pass.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/testdeploy"
)

// The deployment tool's stable exit codes, mirrored here as plain ints.
// This test file must not import mosaic-deploy to learn its own outcome
// contract — only the stable exit-code meanings are shared, per the
// no-import convention.
const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 3
)

// newDeployer constructs a Deployer with the given Options. Every test
// supplies its own Invoke seam; ExecutablePath is set to a stable sentinel so
// tests can assert it is passed through to the seam.
func newDeployer(opts testdeploy.Options) *testdeploy.Deployer {
	if opts.ExecutablePath == "" {
		opts.ExecutablePath = "mosaic-deploy"
	}
	return testdeploy.New(opts)
}

// minimalDeploy calls Deploy with the smallest valid argument set for a single
// harness deployment. Tests that care about specific field values call Deploy
// directly.
func minimalDeploy(d *testdeploy.Deployer) error {
	return d.Deploy(
		context.Background(),
		"/catalog",    // catalogFolder
		"/mosaic",     // mosaicRoot
		"/workspace",  // workspace
		[]string{"auto"},            // harnesses
		[]string{"smoke-single"},    // workflows
	)
}

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

	_ = d.Deploy(context.Background(), catalogPath, "/mosaic", "/workspace", []string{"auto"}, []string{"smoke-single"})

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

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", workspacePath, []string{"auto"}, []string{"smoke-single"})

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

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace", []string{"claude-code"}, []string{"smoke-single"})

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

	_ = d.Deploy(context.Background(), "/catalog", mosaicRoot, "/workspace", []string{"auto"}, []string{"smoke-single"})

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

	_ = d.Deploy(context.Background(), "/catalog", "", "/workspace", []string{"auto"}, []string{"smoke-single"})

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

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace", []string{"auto"}, []string{})

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

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace", []string{"auto"}, workflows)

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

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace", []string{"auto"}, []string{"smoke-single"})

	idx := flagIndex(capturedArgs, "--workflows")
	if idx < 0 || idx+1 >= len(capturedArgs) {
		t.Fatalf("args = %v, want --workflows <id> for single workflow", capturedArgs)
	}
	if capturedArgs[idx+1] != "smoke-single" {
		t.Errorf("--workflows value = %q, want %q (single ID, no comma)", capturedArgs[idx+1], "smoke-single")
	}
}

// =============================================================================
// Multi-harness: one invocation per harness
// =============================================================================

// TestDeploy_SingleHarness_ExactlyOneInvocation asserts that a single harness
// produces exactly one CommandRunner invocation.
func TestDeploy_SingleHarness_ExactlyOneInvocation(t *testing.T) {
	invocations := 0

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			invocations++
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace", []string{"auto"}, []string{"smoke-single"})

	if invocations != 1 {
		t.Errorf("CommandRunner invoked %d time(s), want exactly 1 for a single harness", invocations)
	}
}

// TestDeploy_TwoHarnesses_TwoInvocations asserts that two harnesses produce
// exactly two CommandRunner invocations. The deploy tool's --harness flag
// accepts a single value, so multi-harness deployment requires one call per
// harness.
func TestDeploy_TwoHarnesses_TwoInvocations(t *testing.T) {
	invocations := 0

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			invocations++
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto", "claude-code"}, []string{"smoke-single"})

	if invocations != 2 {
		t.Errorf("CommandRunner invoked %d time(s), want exactly 2 for two harnesses (one per harness)", invocations)
	}
}

// TestDeploy_ThreeHarnesses_ThreeInvocations asserts that three harnesses
// produce exactly three CommandRunner invocations.
func TestDeploy_ThreeHarnesses_ThreeInvocations(t *testing.T) {
	invocations := 0

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			invocations++
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto", "claude-code", "ghcp-cli"}, []string{"smoke-single"})

	if invocations != 3 {
		t.Errorf("CommandRunner invoked %d time(s), want exactly 3 for three harnesses", invocations)
	}
}

// TestDeploy_TwoHarnesses_EachInvocationGetsCorrectHarnessID asserts that each
// per-harness invocation receives its own harness ID in --harness and not the
// other harness's ID. Without this, both invocations would receive the same
// harness and one harness would go undeployed.
func TestDeploy_TwoHarnesses_EachInvocationGetsCorrectHarnessID(t *testing.T) {
	var capturedByHarness []string // harness IDs extracted from each invocation

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--harness")
			if idx >= 0 && idx+1 < len(args) {
				capturedByHarness = append(capturedByHarness, args[idx+1])
			} else {
				capturedByHarness = append(capturedByHarness, "")
			}
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto", "claude-code"}, []string{"smoke-single"})

	if len(capturedByHarness) != 2 {
		t.Fatalf("CommandRunner invoked %d time(s), want 2", len(capturedByHarness))
	}
	// Order may vary; assert that exactly the expected harness IDs appear.
	seen := map[string]bool{}
	for _, h := range capturedByHarness {
		seen[h] = true
	}
	if !seen["auto"] {
		t.Errorf("harness IDs across invocations = %v, want \"auto\" to appear", capturedByHarness)
	}
	if !seen["claude-code"] {
		t.Errorf("harness IDs across invocations = %v, want \"claude-code\" to appear", capturedByHarness)
	}
}

// TestDeploy_TwoHarnesses_SharedCatalogFolderAcrossInvocations asserts that
// all per-harness invocations share the same --catalog-folder value. The
// catalog path does not change per harness.
func TestDeploy_TwoHarnesses_SharedCatalogFolderAcrossInvocations(t *testing.T) {
	const catalog = "/opt/mosaic/Tools/Runner/TestCatalog"
	var catalogsObserved []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--catalog-folder")
			if idx >= 0 && idx+1 < len(args) {
				catalogsObserved = append(catalogsObserved, args[idx+1])
			} else {
				catalogsObserved = append(catalogsObserved, "")
			}
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), catalog, "/mosaic", "/workspace",
		[]string{"auto", "claude-code"}, []string{"smoke-single"})

	if len(catalogsObserved) != 2 {
		t.Fatalf("CommandRunner invoked %d time(s), want 2", len(catalogsObserved))
	}
	for i, c := range catalogsObserved {
		if c != catalog {
			t.Errorf("invocation %d: --catalog-folder = %q, want %q (same across all harness invocations)", i, c, catalog)
		}
	}
}

// =============================================================================
// Exit-code-to-error mapping
// =============================================================================

// TestDeploy_ExitSuccess_ReturnsNilError asserts that exit 0 from the deploy
// tool produces a nil error — the happy path.
func TestDeploy_ExitSuccess_ReturnsNilError(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, nil, exitSuccess, nil
		},
	})

	err := minimalDeploy(d)

	if err != nil {
		t.Errorf("Deploy returned error on exit 0: %v; want nil error on success", err)
	}
}

// TestDeploy_ExitFailure_ReturnsErrDeployFailed asserts that exit 1 from the
// deploy tool maps to a sentinel error wrapping ErrDeployFailed.
func TestDeploy_ExitFailure_ReturnsErrDeployFailed(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, []byte("deploy failed: workspace not found"), exitFailure, nil
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1, want an error wrapping ErrDeployFailed")
	}
	if !errors.Is(err, testdeploy.ErrDeployFailed) {
		t.Errorf("Deploy error = %v, want it to wrap ErrDeployFailed (exit 1 is a deploy failure)", err)
	}
}

// TestDeploy_ExitUsage_ReturnsErrDeployFailed asserts that exit 3 (flag/usage
// error from the deploy tool) also maps to ErrDeployFailed. From the caller's
// perspective a bad-argument error is still a deployment failure — it is not
// a distinct error class.
func TestDeploy_ExitUsage_ReturnsErrDeployFailed(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, []byte("flag --harness is required"), exitUsage, nil
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 3, want an error wrapping ErrDeployFailed")
	}
	if !errors.Is(err, testdeploy.ErrDeployFailed) {
		t.Errorf("Deploy error = %v, want it to wrap ErrDeployFailed (exit 3 is a deploy failure)", err)
	}
}

// TestDeploy_DeployFailed_StderrInToolMessage asserts that the deploy tool's
// stderr text is preserved in DeployError.ToolMessage so that diagnostics can
// quote the tool's own message rather than paraphrasing it.
func TestDeploy_DeployFailed_StderrInToolMessage(t *testing.T) {
	const stderrText = "error: workspace /nonexistent does not exist"

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, []byte(stderrText), exitFailure, nil
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1, want ErrDeployFailed")
	}
	var de *testdeploy.DeployError
	if !errors.As(err, &de) {
		t.Fatalf("Deploy error = %v, want a *DeployError carrying the tool's stderr", err)
	}
	if !strings.Contains(de.ToolMessage, stderrText) {
		t.Errorf("DeployError.ToolMessage = %q, want it to contain the stderr text %q", de.ToolMessage, stderrText)
	}
}

// TestDeploy_DeployFailed_ExitCodeInDeployError asserts that the actual exit
// code from the deploy tool is preserved in DeployError.ExitCode so that
// diagnostics can report the exact code.
func TestDeploy_DeployFailed_ExitCodeInDeployError(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, []byte("something went wrong"), exitFailure, nil
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1, want ErrDeployFailed")
	}
	var de *testdeploy.DeployError
	if !errors.As(err, &de) {
		t.Fatalf("Deploy error = %v, want a *DeployError", err)
	}
	if de.ExitCode != exitFailure {
		t.Errorf("DeployError.ExitCode = %d, want %d", de.ExitCode, exitFailure)
	}
}

// =============================================================================
// ErrDeployFailed wrapping contract
// =============================================================================

// TestDeploy_DeployError_UnwrapsToErrDeployFailed asserts that errors.Is
// returns true for ErrDeployFailed on a *DeployError. This is the standard
// Go error sentinel pattern — callers should be able to branch on the class
// without knowing the concrete type.
func TestDeploy_DeployError_UnwrapsToErrDeployFailed(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, []byte("deploy failed"), exitFailure, nil
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1")
	}
	if !errors.Is(err, testdeploy.ErrDeployFailed) {
		t.Errorf("errors.Is(err, ErrDeployFailed) = false, want true; error = %v", err)
	}
}

// TestDeploy_DeployError_ErrorsAsDeployError asserts that errors.As unwraps
// a *DeployError from a deploy failure. Callers need this to read ToolMessage
// and ExitCode for diagnostic output.
func TestDeploy_DeployError_ErrorsAsDeployError(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, []byte("deploy failed"), exitFailure, nil
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1")
	}
	var de *testdeploy.DeployError
	if !errors.As(err, &de) {
		t.Errorf("errors.As(err, &DeployError) = false, want true; error = %v", err)
	}
}

// =============================================================================
// ErrToolUnavailable: binary not found or not executable
// =============================================================================

// TestDeploy_ToolUnavailable_ReturnsErrToolUnavailable asserts that an
// invocation failure (the binary is absent, not executable, or killed before
// the exit code is known) maps to ErrToolUnavailable, not to ErrDeployFailed.
// Binary-not-found is an infrastructure problem, not a deploy failure.
func TestDeploy_ToolUnavailable_ReturnsErrToolUnavailable(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, nil, 0, errors.New("exec: executable file not found in $PATH")
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error when the tool cannot be invoked, want ErrToolUnavailable")
	}
	if !errors.Is(err, testdeploy.ErrToolUnavailable) {
		t.Errorf("Deploy error = %v, want it to wrap ErrToolUnavailable (not ErrDeployFailed)", err)
	}
}

// TestDeploy_ToolUnavailable_ErrorNamesExecutablePath asserts that the
// ErrToolUnavailable error names the executable path that was searched, so
// the operator knows what binary to install or what path to override.
func TestDeploy_ToolUnavailable_ErrorNamesExecutablePath(t *testing.T) {
	const binaryPath = "/usr/local/bin/mosaic-deploy"

	d := testdeploy.New(testdeploy.Options{
		ExecutablePath: binaryPath,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, nil, 0, errors.New("exec: no such file or directory")
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error when the tool cannot be invoked, want ErrToolUnavailable")
	}
	if !strings.Contains(err.Error(), binaryPath) {
		t.Errorf("error = %q, want it to name the executable path %q so the operator knows what was searched", err.Error(), binaryPath)
	}
}

// TestDeploy_ToolUnavailable_IsDistinctFromDeployFailed asserts that
// ErrToolUnavailable and ErrDeployFailed are distinct error classes — they
// must not satisfy each other's errors.Is check.
func TestDeploy_ToolUnavailable_IsDistinctFromDeployFailed(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, nil, 0, errors.New("exec: no such file or directory")
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error when the tool cannot be invoked")
	}
	if errors.Is(err, testdeploy.ErrDeployFailed) {
		t.Errorf("errors.Is(err, ErrDeployFailed) = true for a binary-not-found error, want false; "+
			"ErrToolUnavailable and ErrDeployFailed must be distinct sentinel classes (err = %v)", err)
	}
}

// =============================================================================
// Timeout and caller cancellation
// =============================================================================

// TestDeploy_TimeoutExceeded_ReturnsErrTimedOut asserts that when the deploy
// tool exceeds the configured timeout, the call returns ErrTimedOut and does
// not hang. The timeout applied by the deployer bounds the delegate's runtime,
// so a stalled binary degrades a run rather than hanging it.
func TestDeploy_TimeoutExceeded_ReturnsErrTimedOut(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Timeout: 20 * time.Millisecond,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			// Block until the context the deployer applied expires.
			<-ctx.Done()
			return nil, nil, 0, ctx.Err()
		},
	})

	// The outer context is generous; the deployer's own timeout fires first.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := d.Deploy(ctx, "/catalog", "/mosaic", "/workspace", []string{"auto"}, []string{"smoke-single"})

	if err == nil {
		t.Fatal("Deploy returned nil error when the tool timed out, want ErrTimedOut")
	}
	if !errors.Is(err, testdeploy.ErrTimedOut) {
		t.Errorf("Deploy error = %v, want it to wrap ErrTimedOut (the deployer's own timeout, not the caller's context)", err)
	}
}

// TestDeploy_CallerContextCancelled_ReturnsError asserts that if the caller's
// own context is cancelled, the call returns promptly with a non-nil error
// rather than continuing. The error must not wrap ErrTimedOut — a caller
// cancellation is distinct from the deployer's internal timeout.
func TestDeploy_CallerContextCancelled_ReturnsError(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Timeout: 10 * time.Second, // much longer than the test will run
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			<-ctx.Done()
			return nil, nil, 0, ctx.Err()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled immediately before Deploy is called

	err := d.Deploy(ctx, "/catalog", "/mosaic", "/workspace", []string{"auto"}, []string{"smoke-single"})

	if err == nil {
		t.Fatal("Deploy returned nil error when the caller's context was cancelled, want a non-nil error")
	}
	if errors.Is(err, testdeploy.ErrTimedOut) {
		t.Errorf("Deploy error = %v, want it NOT to wrap ErrTimedOut for a caller cancellation "+
			"(ErrTimedOut is reserved for the deployer's own internal timeout)", err)
	}
}

// =============================================================================
// Multi-harness failure: stops on first error
// =============================================================================

// TestDeploy_MultipleHarnesses_FirstHarnessFailure_StopsEarly asserts that
// if the first harness invocation fails (non-zero exit), the deployer returns
// the error immediately without invoking the runner for subsequent harnesses.
func TestDeploy_MultipleHarnesses_FirstHarnessFailure_StopsEarly(t *testing.T) {
	invocations := 0

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			invocations++
			// First invocation fails; second should never be reached.
			return nil, []byte("deploy failed"), exitFailure, nil
		},
	})

	err := d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto", "claude-code"}, []string{"smoke-single"})

	if err == nil {
		t.Fatal("Deploy returned nil error after first harness failed, want ErrDeployFailed")
	}
	if !errors.Is(err, testdeploy.ErrDeployFailed) {
		t.Errorf("Deploy error = %v, want ErrDeployFailed", err)
	}
	if invocations != 1 {
		t.Errorf("CommandRunner invoked %d time(s), want exactly 1 "+
			"(deployer must stop on first harness failure, not continue to remaining harnesses)", invocations)
	}
}

// =============================================================================
// Helpers
// =============================================================================

// containsFlag reports whether args contains the flag followed by value as
// consecutive elements.
func containsFlag(args []string, flag, value string) bool {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

// flagIndex returns the index of flag in args, or -1 if not found.
func flagIndex(args []string, flag string) int {
	for i, arg := range args {
		if arg == flag {
			return i
		}
	}
	return -1
}
