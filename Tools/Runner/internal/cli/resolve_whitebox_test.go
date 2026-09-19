// resolve_whitebox_test.go provides whitebox tests for the CLI fail-fast
// behavior when harness binary resolution fails (T4.1). These tests live in
// package cli (not cli_test) so they can:
//
//   1. Define TestMain, which installs a synthetic-success resolveAndAnnounce
//      stub for the whole test binary before any test runs. This makes the
//      existing tests in test_test.go (package cli_test) deterministic on
//      machines where harness binaries are not installed.
//   2. Override the resolveAndAnnounce package-level variable to inject
//      failures via setResolveAndAnnounce (unexported helper).
//
// Tests that use setResolveAndAnnounce must NOT call t.Parallel(): the variable
// is shared and not goroutine-safe.
package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// TestMain installs a synthetic-success resolveAndAnnounce stub before running
// the test binary. This ensures that all tests in both package cli and package
// cli_test that reach the resolution call site (which is every test that calls
// RunTestCommand past flag validation) are deterministic across environments
// where harness binaries may not be installed.
//
// Individual tests that need to test resolution failure override the variable
// via setResolveAndAnnounce, which restores it to this synthetic stub via
// t.Cleanup.
func TestMain(m *testing.M) {
	resolveAndAnnounce = syntheticSuccessResolve
	os.Exit(m.Run())
}

// syntheticSuccessResolve is the synthetic-success stub installed by TestMain.
// It returns an empty map for the "fake" harness (which has no binary to
// resolve) and a deterministic fake path for every other harness ID.
// The fake paths follow /synthetic/bin/<harnessID> so tests can assert on
// them if needed.
func syntheticSuccessResolve(
	harnesses []string,
	lookPath func(string) (string, error),
	cliOutput io.Writer,
	logger domain.DebugLogger,
) (map[string]string, error) {
	result := make(map[string]string)
	for _, h := range harnesses {
		if h != "fake" {
			result[h] = "/synthetic/bin/" + h
		}
	}
	return result, nil
}

// setResolveAndAnnounce overrides the package-level resolveAndAnnounce
// variable for the duration of a single test and restores the previous value
// via t.Cleanup. Tests that call this helper must NOT use t.Parallel() because
// resolveAndAnnounce is a shared package-level variable.
func setResolveAndAnnounce(t *testing.T, fn func(
	harnesses []string,
	lookPath func(string) (string, error),
	cliOutput io.Writer,
	logger domain.DebugLogger,
) (map[string]string, error)) {
	t.Helper()
	orig := resolveAndAnnounce
	resolveAndAnnounce = fn
	t.Cleanup(func() { resolveAndAnnounce = orig })
}

// makeTempCatalogRoot creates a minimal catalog directory structure under a
// fresh temporary directory and returns the MOSAIC root path (the value for
// the --catalog flag). It mirrors the pattern in test_test.go's makeCatalogRoot.
//
// The created structure is:
//
//	<root>/
//	  Tools/Runner/TestCatalog/Workflows/MosaicTest/
//	    <workflowID>.md (with frontmatter)
//	    Fixtures/<workflowID>/   (empty fixture directory)
func makeTempCatalogRoot(t *testing.T, workflowID string, modes []string, smokeSet []string) string {
	t.Helper()
	root := t.TempDir()

	wfDir := filepath.Join(root, "Tools", "Runner", "TestCatalog", "Workflows", "MosaicTest")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", wfDir, err)
	}

	fixDir := filepath.Join(wfDir, "Fixtures", workflowID)
	if err := os.MkdirAll(fixDir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", fixDir, err)
	}

	modesYAML := "  - " + strings.Join(modes, "\n  - ")
	smokeYAML := ""
	if len(smokeSet) > 0 {
		smokeYAML = "\nsmoke_set:\n  - " + strings.Join(smokeSet, "\n  - ")
	}
	content := fmt.Sprintf("---\nid: %s\nname: Test Workflow\nmodes:\n%s%s\n---\n",
		workflowID, modesYAML, smokeYAML)

	mdPath := filepath.Join(wfDir, workflowID+".md")
	if err := os.WriteFile(mdPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", mdPath, err)
	}

	return root
}

// =============================================================================
// T4.1: CLI fail-fast on resolution failure
// =============================================================================

// TestRunTestSubcmd_ResolutionFailure_ReturnsExitFailure verifies that when
// resolveAndAnnounce returns an error, RunTestCommand exits with ExitFailure
// (not ExitUsage, not ExitSuccess). Resolution sits after catalog load and
// before orchestrator construction; the early return must propagate to the
// caller as exit code 1.
//
// The corroborating assertion (stdout must be empty) ensures this test fails
// in TDD RED for the correct reason: a coincidental ExitFailure from deploy
// failure is distinguishable because deploy writes to stdout, whereas a proper
// resolution-failure fast-exit produces no stdout output.
func TestRunTestSubcmd_ResolutionFailure_ReturnsExitFailure(t *testing.T) {
	// Must not call t.Parallel(): overrides shared resolveAndAnnounce.
	root := makeTempCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	injectedErr := errors.New("test: harness binary not found on PATH")
	setResolveAndAnnounce(t, func(_ []string, _ func(string) (string, error), _ io.Writer, _ domain.DebugLogger) (map[string]string, error) {
		return nil, injectedErr
	})

	var out, errOut bytes.Buffer
	code := RunTestCommand(
		context.Background(),
		[]string{"test", "--catalog", root, "--suite", "smoke", "--harness", "claude-code"},
		t.TempDir(),
		&out, &errOut,
	)

	if code != ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d); stderr: %q", code, ExitFailure, errOut.String())
	}
	// Corroborating assertion: resolution fail-fast must produce no stdout output.
	// Without this, a coincidental ExitFailure from a missing deploy binary would
	// pass the exit-code check even though resolution was never checked.
	if out.Len() != 0 {
		t.Errorf("stdout must be empty on resolution failure (fail-fast before orchestrator), got: %q", out.String())
	}
}

// TestRunTestSubcmd_ResolutionFailure_NotExitUsage verifies that resolution
// failure returns ExitFailure, not ExitUsage. The distinction matters because
// ExitUsage is reserved for flag-validation errors; resolution is a runtime
// failure (harness binary absent from PATH), not a usage error.
//
// The corroborating assertion (stderr must name the resolution call site) ensures
// this test fails in TDD RED for the correct reason: a coincidental non-ExitUsage
// from a failed deploy is distinguishable because the error message will name the
// orchestration step rather than the resolution step.
func TestRunTestSubcmd_ResolutionFailure_NotExitUsage(t *testing.T) {
	// Must not call t.Parallel(): overrides shared resolveAndAnnounce.
	root := makeTempCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	setResolveAndAnnounce(t, func(_ []string, _ func(string) (string, error), _ io.Writer, _ domain.DebugLogger) (map[string]string, error) {
		return nil, errors.New("test: injected resolution failure")
	})

	var out, errOut bytes.Buffer
	code := RunTestCommand(
		context.Background(),
		[]string{"test", "--catalog", root, "--suite", "smoke", "--harness", "claude-code"},
		t.TempDir(),
		&out, &errOut,
	)

	if code == ExitUsage {
		t.Errorf("resolution failure must not return ExitUsage (%d); got ExitUsage; stderr: %q", ExitUsage, errOut.String())
	}
	// Corroborating assertion: the resolution error must name the resolution call
	// site. This fails in RED (when resolution wiring is absent) because the error
	// message comes from the orchestrator/deploy step, not the resolution step.
	if !strings.Contains(errOut.String(), "resolving harness binaries") {
		t.Errorf("stderr %q must contain \"resolving harness binaries\" to confirm it is a resolution failure, not a downstream failure", errOut.String())
	}
}

// TestRunTestSubcmd_ResolutionFailure_WritesErrorToStderr verifies that on
// resolution failure the error message appears on stderr in the format:
//
//	error: resolving harness binaries: <reason>
//
// This format is consistent with the existing error-print convention in test.go.
func TestRunTestSubcmd_ResolutionFailure_WritesErrorToStderr(t *testing.T) {
	// Must not call t.Parallel(): overrides shared resolveAndAnnounce.
	root := makeTempCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	const errReason = "test: harness binary not found on PATH"
	setResolveAndAnnounce(t, func(_ []string, _ func(string) (string, error), _ io.Writer, _ domain.DebugLogger) (map[string]string, error) {
		return nil, errors.New(errReason)
	})

	var out, errOut bytes.Buffer
	RunTestCommand(
		context.Background(),
		[]string{"test", "--catalog", root, "--suite", "smoke", "--harness", "claude-code"},
		t.TempDir(),
		&out, &errOut,
	)

	got := errOut.String()
	if !strings.Contains(got, "resolving harness binaries") {
		t.Errorf("stderr %q does not contain \"resolving harness binaries\"", got)
	}
	if !strings.Contains(got, errReason) {
		t.Errorf("stderr %q does not contain the error reason %q", got, errReason)
	}
}

// TestRunTestSubcmd_ResolutionFailure_StderrStartsWithErrorPrefix verifies
// that the error line is prefixed with "error:" following the existing
// convention for actionable error messages in the CLI test path.
func TestRunTestSubcmd_ResolutionFailure_StderrStartsWithErrorPrefix(t *testing.T) {
	// Must not call t.Parallel(): overrides shared resolveAndAnnounce.
	root := makeTempCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	setResolveAndAnnounce(t, func(_ []string, _ func(string) (string, error), _ io.Writer, _ domain.DebugLogger) (map[string]string, error) {
		return nil, errors.New("test: binary absent")
	})

	var out, errOut bytes.Buffer
	RunTestCommand(
		context.Background(),
		[]string{"test", "--catalog", root, "--suite", "smoke", "--harness", "claude-code"},
		t.TempDir(),
		&out, &errOut,
	)

	got := errOut.String()
	if !strings.Contains(got, "error:") {
		t.Errorf("stderr %q does not contain \"error:\" prefix", got)
	}
}

// TestRunTestSubcmd_ResolutionFailure_WithFakeHarness_OverrideStillFails
// verifies that when the resolveAndAnnounce override returns an error, even
// a "fake" harness (which normally skips resolution) triggers the failure
// because the override controls the entire resolution call site, not just the
// per-harness lookup logic.
//
// The corroborating assertion (stdout must be empty) distinguishes a proper
// resolution fail-fast (no orchestrator output) from a coincidental ExitFailure
// caused by a missing deploy binary (which produces stdout output before failing).
func TestRunTestSubcmd_ResolutionFailure_WithFakeHarness_OverrideStillFails(t *testing.T) {
	// Must not call t.Parallel(): overrides shared resolveAndAnnounce.
	root := makeTempCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	setResolveAndAnnounce(t, func(_ []string, _ func(string) (string, error), _ io.Writer, _ domain.DebugLogger) (map[string]string, error) {
		return nil, errors.New("test: always fail")
	})

	var out, errOut bytes.Buffer
	code := RunTestCommand(
		context.Background(),
		[]string{"test", "--catalog", root, "--suite", "smoke", "--harness", "fake"},
		t.TempDir(),
		&out, &errOut,
	)

	if code != ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d) when override injects error; stderr: %q",
			code, ExitFailure, errOut.String())
	}
	// Corroborating assertion: resolution fail-fast must produce no stdout output.
	if out.Len() != 0 {
		t.Errorf("stdout must be empty on resolution failure (fail-fast before orchestrator), got: %q", out.String())
	}
}

// TestRunTestSubcmd_ResolutionFailure_StdoutIsEmpty verifies that when
// resolution fails, no orchestration output is written to stdout. The fail-fast
// return happens before the orchestrator is constructed, so no test progress
// or summary output must appear on stdout. Only stderr carries the error message.
func TestRunTestSubcmd_ResolutionFailure_StdoutIsEmpty(t *testing.T) {
	// Must not call t.Parallel(): overrides shared resolveAndAnnounce.
	root := makeTempCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	setResolveAndAnnounce(t, func(_ []string, _ func(string) (string, error), _ io.Writer, _ domain.DebugLogger) (map[string]string, error) {
		return nil, errors.New("test: injected resolution failure")
	})

	var out, errOut bytes.Buffer
	RunTestCommand(
		context.Background(),
		[]string{"test", "--catalog", root, "--suite", "smoke", "--harness", "claude-code"},
		t.TempDir(),
		&out, &errOut,
	)

	if out.Len() != 0 {
		t.Errorf("stdout must be empty on resolution failure (no tests ran), got: %q", out.String())
	}
}

// TestRunTestSubcmd_SyntheticSuccessOverride_DoesNotReturnUsageError verifies
// that the TestMain-installed synthetic success stub does not cause usage
// errors for standard valid invocations. This is the determinism guard for
// all existing tests in test_test.go that reach the resolution call site.
func TestRunTestSubcmd_SyntheticSuccessOverride_DoesNotReturnUsageError(t *testing.T) {
	// Must not call t.Parallel(): uses the shared resolveAndAnnounce variable
	// (set by TestMain; this test does NOT further override it).
	root := makeTempCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})

	var out, errOut bytes.Buffer
	code := RunTestCommand(
		context.Background(),
		[]string{"test", "--catalog", root, "--suite", "smoke", "--harness", "claude-code"},
		t.TempDir(),
		&out, &errOut,
	)

	// The synthetic success stub allows resolution to pass. Any failure
	// after that comes from the orchestrator (no real binary), which returns
	// ExitFailure, not ExitUsage. Assert that flag validation still passes.
	if code == ExitUsage {
		t.Errorf("synthetic success override must not produce ExitUsage; got ExitUsage with stderr: %q", errOut.String())
	}
}

// =============================================================================
// Manual verification checklist for AC4.9 (Windows .cmd shim with spaces)
// =============================================================================
//
// AC4.9 is a manual-only verification step. The automated tests above cover
// the programmatic resolution and fail-fast contracts. The following checklist
// documents the manual verification steps for Windows:
//
//  1. Install a harness binary via npm in a path with spaces, e.g.:
//       C:\Program Files\nodejs\claude.cmd
//  2. Run the test suite:
//       mosaic-run test --catalog <repo> --suite smoke --harness claude-code
//  3. Verify that the startup output shows the resolved path:
//       claude-code: C:\Program Files\nodejs\claude.cmd
//  4. Inspect the test-run subprocess invocation to confirm --executable-path
//     is forwarded:
//       mosaic-run run --executable-path "C:\Program Files\nodejs\claude.cmd" ...
//  5. Verify the subprocess launches correctly (exec.Command wraps cmd /c,
//     which handles the quoted space-containing path via EscapeArg).
//  6. Confirm a test run completes (pass or fail, not an OS error).
//
// This checklist is captured as a comment per the AC4.9 requirement.
