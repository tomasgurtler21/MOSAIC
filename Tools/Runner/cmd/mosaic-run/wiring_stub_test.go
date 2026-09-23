// wiring_stub_test.go tests the buildTestRunnerFactory function, focusing on
// the resolution failure path (T4.3): when resolveAndAnnounceFn returns an
// error, the factory must return (nil, error) without running any tests.
//
// The resolveAndAnnounceFn variable is overridden per-test using t.Cleanup for
// restore. Tests in this file must NOT use t.Parallel() because the variable
// is a shared package-level mutable.
//
// The factory calls testcatalog.Load before the resolution call site. Tests
// therefore supply a cfg.MosaicRoot pointing to the repository root so the
// real TestCatalog directory is available. The repository root is found at
// compile time via runtime.Caller.
package main

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/testrun"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// wbRepoRoot returns the absolute path to the MOSAIC repository root by
// walking up from the wiring_stub_test.go source file (4 levels).
// Directory layout:
//
//	<root>/Tools/Runner/cmd/mosaic-run/wiring_stub_test.go
//	        ^4   ^3      ^2   ^1
func wbRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to locate wiring_stub_test.go")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 4; i++ {
		dir = filepath.Dir(dir)
	}
	return dir
}

// setResolveAndAnnounceFn overrides the package-level resolveAndAnnounceFn
// variable for the duration of a single test and restores the original value
// via t.Cleanup. Tests using this helper must NOT call t.Parallel().
func setResolveAndAnnounceFn(t *testing.T, fn func(
	harnesses []string,
	lookPath func(string) (string, error),
	cliOutput io.Writer,
	logger domain.DebugLogger,
) (map[string]string, error)) {
	t.Helper()
	orig := resolveAndAnnounceFn
	resolveAndAnnounceFn = fn
	t.Cleanup(func() { resolveAndAnnounceFn = orig })
}

// wbNoopReporter is a minimal ProgressReporter that records whether any
// lifecycle callbacks were invoked. It lets tests verify that no test
// infrastructure started when resolution fails.
type wbNoopReporter struct {
	deployStarted bool
	testStarted   bool
}

func (r *wbNoopReporter) OnDeployStart()                                                          { r.deployStarted = true }
func (r *wbNoopReporter) OnDeployDone(err error)                                                  {}
func (r *wbNoopReporter) OnTestStart(harness string, workflow string, mode string)                { r.testStarted = true }
func (r *wbNoopReporter) OnTestDone(harness string, workflow string, mode string, result testrun.TestRunResult) {
}

// ---------------------------------------------------------------------------
// T4.3: TUI factory resolution failure path
// ---------------------------------------------------------------------------

// TestBuildTestRunnerFactory_ResolutionFailure_ReturnsNilSummaryAndError
// verifies that when resolveAndAnnounceFn returns an error, the factory
// returns (nil, non-nil error). The TUI app.go maps (nil, error) to a
// TestSummary{DeployError: err} fallback; the factory must not build its own
// fallback on the resolution-failure path (that is app.go's responsibility).
func TestBuildTestRunnerFactory_ResolutionFailure_ReturnsNilSummaryAndError(t *testing.T) {
	// Must not call t.Parallel(): overrides shared resolveAndAnnounceFn.
	injectedErr := newWBError("test: harness binary not found on PATH")
	setResolveAndAnnounceFn(t, func(_ []string, _ func(string) (string, error), _ io.Writer, _ domain.DebugLogger) (map[string]string, error) {
		return nil, injectedErr
	})

	factory := buildTestRunnerFactory()
	cfg := testrun.TestConfig{
		MosaicRoot: wbRepoRoot(t),
		Workspace:  t.TempDir(),
		Harnesses:  []string{"claude-code"},
		Scope:      testrun.ScopeSmoke,
	}
	reporter := &wbNoopReporter{}

	summary, err := factory(context.Background(), cfg, reporter)

	if summary != nil {
		t.Errorf("factory must return nil summary on resolution failure, got non-nil: %+v", summary)
	}
	if err == nil {
		t.Error("factory must return non-nil error on resolution failure, got nil")
	}
}

// TestBuildTestRunnerFactory_ResolutionFailure_ErrorContainsInjectedReason
// verifies that the error returned by the factory on resolution failure is the
// same error (or wraps it) as the one injected via the seam. This ensures the
// diagnostic information (binary name, PATH, reason) survives to the TUI.
func TestBuildTestRunnerFactory_ResolutionFailure_ErrorContainsInjectedReason(t *testing.T) {
	// Must not call t.Parallel(): overrides shared resolveAndAnnounceFn.
	const errReason = "test: diagnostic message from resolution"
	setResolveAndAnnounceFn(t, func(_ []string, _ func(string) (string, error), _ io.Writer, _ domain.DebugLogger) (map[string]string, error) {
		return nil, newWBError(errReason)
	})

	factory := buildTestRunnerFactory()
	cfg := testrun.TestConfig{
		MosaicRoot: wbRepoRoot(t),
		Workspace:  t.TempDir(),
		Harnesses:  []string{"claude-code"},
		Scope:      testrun.ScopeSmoke,
	}

	_, err := factory(context.Background(), cfg, &wbNoopReporter{})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != errReason {
		// The factory forwards the error directly (no wrapping on the
		// resolution-failure path). If the factory wraps it, the message may
		// differ; the important thing is that the injected reason appears somewhere.
		if !containsStr(err.Error(), errReason) {
			t.Errorf("error = %q, want it to contain injected reason %q", err.Error(), errReason)
		}
	}
}

// TestBuildTestRunnerFactory_ResolutionFailure_NoDeployStarted verifies that
// on resolution failure, the reporter's OnDeployStart is never called. This
// confirms the fail-fast contract: no test infrastructure runs after resolution
// fails.
func TestBuildTestRunnerFactory_ResolutionFailure_NoDeployStarted(t *testing.T) {
	// Must not call t.Parallel(): overrides shared resolveAndAnnounceFn.
	setResolveAndAnnounceFn(t, func(_ []string, _ func(string) (string, error), _ io.Writer, _ domain.DebugLogger) (map[string]string, error) {
		return nil, newWBError("test: injected failure")
	})

	factory := buildTestRunnerFactory()
	cfg := testrun.TestConfig{
		MosaicRoot: wbRepoRoot(t),
		Workspace:  t.TempDir(),
		Harnesses:  []string{"claude-code"},
		Scope:      testrun.ScopeSmoke,
	}
	reporter := &wbNoopReporter{}

	factory(context.Background(), cfg, reporter) //nolint

	if reporter.deployStarted {
		t.Error("OnDeployStart must not be called when resolution fails")
	}
}

// TestBuildTestRunnerFactory_ResolutionFailure_NoTestsStarted verifies that
// on resolution failure, the reporter's OnTestStart is never called. This is
// the observable invariant of the fail-fast contract from the TUI perspective:
// the progress screen never receives a test-start notification.
//
// The additional assertion on deployStarted ensures this test fails in TDD RED
// for the correct structural reason: when resolution is not intercepted,
// OnDeployStart IS called (confirmed by the NoDeployStarted sibling), so
// NoTestsStarted would otherwise pass trivially (no tests start after any deploy
// failure, including a missing-binary failure unrelated to resolution).
func TestBuildTestRunnerFactory_ResolutionFailure_NoTestsStarted(t *testing.T) {
	// Must not call t.Parallel(): overrides shared resolveAndAnnounceFn.
	setResolveAndAnnounceFn(t, func(_ []string, _ func(string) (string, error), _ io.Writer, _ domain.DebugLogger) (map[string]string, error) {
		return nil, newWBError("test: injected failure")
	})

	factory := buildTestRunnerFactory()
	cfg := testrun.TestConfig{
		MosaicRoot: wbRepoRoot(t),
		Workspace:  t.TempDir(),
		Harnesses:  []string{"claude-code"},
		Scope:      testrun.ScopeSmoke,
	}
	reporter := &wbNoopReporter{}

	factory(context.Background(), cfg, reporter) //nolint

	// Both assertions are required for discriminating RED coverage.
	// deployStarted fails in RED (resolution not yet intercepted, so deploy runs).
	// testStarted is a consequence of the same fail-fast contract.
	if reporter.deployStarted {
		t.Error("OnDeployStart must not be called when resolution fails")
	}
	if reporter.testStarted {
		t.Error("OnTestStart must not be called when resolution fails")
	}
}

// ---------------------------------------------------------------------------
// AC4.11: buildTestRunnerFactory sets ResolvedPaths on both summary paths
// ---------------------------------------------------------------------------

// setOrchRunFn overrides the package-level orchRunFn variable for the duration
// of a single test and restores the original value via t.Cleanup. Tests using
// this helper must NOT call t.Parallel().
func setOrchRunFn(t *testing.T, fn func(
	orch *testrun.Orchestrator,
	ctx context.Context,
	cfg testrun.TestConfig,
) (*testrun.TestSummary, error)) {
	t.Helper()
	orig := orchRunFn
	orchRunFn = fn
	t.Cleanup(func() { orchRunFn = orig })
}

// TestBuildTestRunnerFactory_OrcRunNilSummaryAndError_FallbackSummaryHasResolvedPaths
// verifies that when resolveAndAnnounceFn returns a resolved-paths map and
// orchRunFn returns (nil, error), buildTestRunnerFactory returns a non-nil
// fallback summary that carries ResolvedPaths equal to the resolved map.
//
// This is the factory-level unit test for AC4.11 (fallback path). It fails in
// TDD RED because before I4.3 the factory does not call resolveAndAnnounceFn
// at all and returns (nil, error) directly, so the returned summary is nil.
func TestBuildTestRunnerFactory_OrcRunNilSummaryAndError_FallbackSummaryHasResolvedPaths(t *testing.T) {
	// Must not call t.Parallel(): overrides shared package-level variables.
	wantPaths := map[string]string{"claude-code": "/test/bin/claude"}
	setResolveAndAnnounceFn(t, func(_ []string, _ func(string) (string, error), _ io.Writer, _ domain.DebugLogger) (map[string]string, error) {
		return wantPaths, nil
	})

	orchErr := errors.New("test: orchestration error after successful resolution")
	setOrchRunFn(t, func(_ *testrun.Orchestrator, _ context.Context, _ testrun.TestConfig) (*testrun.TestSummary, error) {
		return nil, orchErr
	})

	factory := buildTestRunnerFactory()
	cfg := testrun.TestConfig{
		MosaicRoot: wbRepoRoot(t),
		Workspace:  t.TempDir(),
		Harnesses:  []string{"claude-code"},
		Scope:      testrun.ScopeSmoke,
	}

	summary, _ := factory(context.Background(), cfg, &wbNoopReporter{})

	if summary == nil {
		t.Fatal("factory must return non-nil fallback summary when orch.Run returns (nil, error) after successful resolution; got nil -- AC4.11 fallback path not implemented")
	}
	if !reflect.DeepEqual(summary.ResolvedPaths, wantPaths) {
		t.Errorf("fallback summary.ResolvedPaths = %v, want %v -- factory must copy cfg.ResolvedPaths onto fallback summary (AC4.11)", summary.ResolvedPaths, wantPaths)
	}
}

// TestBuildTestRunnerFactory_OrcRunNonNilSummary_SummaryHasResolvedPaths
// verifies that when resolveAndAnnounceFn returns a resolved-paths map and
// orchRunFn returns a non-nil summary, buildTestRunnerFactory sets
// summary.ResolvedPaths on the returned summary.
//
// This is the factory-level unit test for AC4.11 (normal path). It fails in
// TDD RED because before I4.3 the factory does not call resolveAndAnnounceFn
// and never copies cfg.ResolvedPaths onto the summary.
func TestBuildTestRunnerFactory_OrcRunNonNilSummary_SummaryHasResolvedPaths(t *testing.T) {
	// Must not call t.Parallel(): overrides shared package-level variables.
	wantPaths := map[string]string{"claude-code": "/test/bin/claude"}
	setResolveAndAnnounceFn(t, func(_ []string, _ func(string) (string, error), _ io.Writer, _ domain.DebugLogger) (map[string]string, error) {
		return wantPaths, nil
	})

	returnedSummary := &testrun.TestSummary{AllPass: true, TotalPass: 1}
	setOrchRunFn(t, func(_ *testrun.Orchestrator, _ context.Context, _ testrun.TestConfig) (*testrun.TestSummary, error) {
		return returnedSummary, nil
	})

	factory := buildTestRunnerFactory()
	cfg := testrun.TestConfig{
		MosaicRoot: wbRepoRoot(t),
		Workspace:  t.TempDir(),
		Harnesses:  []string{"claude-code"},
		Scope:      testrun.ScopeSmoke,
	}

	summary, err := factory(context.Background(), cfg, &wbNoopReporter{})

	if err != nil {
		t.Fatalf("unexpected error from factory: %v", err)
	}
	if summary == nil {
		t.Fatal("factory must return non-nil summary when orch.Run succeeds; got nil")
	}
	if !reflect.DeepEqual(summary.ResolvedPaths, wantPaths) {
		t.Errorf("summary.ResolvedPaths = %v, want %v -- factory must copy cfg.ResolvedPaths onto summary (AC4.11)", summary.ResolvedPaths, wantPaths)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// wbError is a minimal error type for injecting test errors.
type wbError struct{ msg string }

func (e *wbError) Error() string { return e.msg }

func newWBError(msg string) error { return &wbError{msg: msg} }

// containsStr returns true when s contains sub.
func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}
