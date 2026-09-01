package runner_test

// Tests for Stage 8: evidence bucketing and the produced-logs signal.
//
// These tests pin four behaviours that the runner must honour after this
// stage's implementation changes are in place:
//
//  1. Sandbox.LogsRoot() returns the OrchestrationLogs parent directory,
//     distinct from Sandbox.LogRoot() which returns the per-run subfolder.
//
//  2. The runner passes LogsRoot in the cost query, so the provider can
//     scan the entire OrchestrationLogs tree (including the sibling fallback
//     bucket) rather than only the per-run folder.
//
//  3. LogsProduced reflects the session as a whole: it is true when logs
//     exist in the unknown-run fallback bucket, even when the per-run folder
//     is absent or empty.
//
//  4. FallbackBucketPresent is true when the unknown-run bucket exists
//     alongside the run-id folder, and false when it does not. The field
//     reaches RunEvidence so conditions.go can produce a non-misleading
//     no-logs signal without the evaluate package performing any I/O.
//
// All tests drive runner.Run and capture evidence through the eval callback,
// so no direct Snapshot construction is needed and every assertion reflects
// observable output, not internal state.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
)

const bucketingRunID = "20260822T120000Z-buck"

// TestSandbox_LogsRoot_ReturnsOrchestrationLogsParentDirectory asserts that
// Sandbox.LogsRoot() returns the OrchestrationLogs directory (the parent of
// all per-run log folders) rather than a run-scoped subfolder. This is the
// anchor for sibling-bucket detection: the unknown-run fallback bucket lives
// at LogsRoot()/unknown-run, not inside LogRoot().
func TestSandbox_LogsRoot_ReturnsOrchestrationLogsParentDirectory(t *testing.T) {
	sb := domain.Sandbox{
		Key:        domain.RunKey{RunID: bucketingRunID},
		SubjectDir: "/workspace/subject",
	}

	got := sb.LogsRoot()
	want := filepath.Join("/workspace/subject", "OrchestrationLogs")

	if got != want {
		t.Errorf("Sandbox.LogsRoot() = %q, want %q — the logs root must be the OrchestrationLogs parent, not the per-run subfolder", got, want)
	}
}

// TestSandbox_LogsRoot_IsParentOfLogRoot asserts the structural relationship
// between LogsRoot() and LogRoot(): the per-run folder must be a direct child
// of the logs root, so LogsRoot() is never confused with LogRoot().
func TestSandbox_LogsRoot_IsParentOfLogRoot(t *testing.T) {
	sb := domain.Sandbox{
		Key:        domain.RunKey{RunID: bucketingRunID},
		SubjectDir: "/workspace/subject",
	}

	logsRoot := sb.LogsRoot()
	logRoot := sb.LogRoot()

	expectedLogRoot := filepath.Join(logsRoot, bucketingRunID)
	if logRoot != expectedLogRoot {
		t.Errorf("Sandbox.LogRoot() = %q, want %q — LogRoot must be LogsRoot()+/+RunID", logRoot, expectedLogRoot)
	}
	if !strings.HasPrefix(logRoot, logsRoot) {
		t.Errorf("LogRoot() = %q does not have prefix LogsRoot() = %q — the per-run folder must be a child of the logs root", logRoot, logsRoot)
	}
}

// TestRun_CostQueryReceivesLogsRootFromSandbox asserts that runner.Run
// populates CostQuery.LogsRoot with the sandbox's own LogsRoot() value, so
// the cost provider can scan the full OrchestrationLogs tree and detect the
// unknown-run sibling bucket. Failing to set this field causes the provider
// to scan only the per-run folder and miss the fallback bucket.
func TestRun_CostQueryReceivesLogsRootFromSandbox(t *testing.T) {
	h := newHarness(t)
	req := newRequest("cost-query-logs-root")
	req.Key.RunID = bucketingRunID

	var sandboxLogsRoot string
	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sandboxLogsRoot = h.Adapter.lastProvisionReq.Sandbox.LogsRoot()
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if sandboxLogsRoot == "" {
		t.Fatal("sandbox LogsRoot() was never observed; launcher callback did not run")
	}
	if h.Cost.lastQuery.LogsRoot != sandboxLogsRoot {
		t.Errorf(
			"cost query LogsRoot = %q, want the sandbox's own LogsRoot() %q — "+
				"the runner must pass the logs parent to the provider so it can scan sibling buckets",
			h.Cost.lastQuery.LogsRoot, sandboxLogsRoot,
		)
	}
}

// TestRun_LogsProducedIsTrueWhenLogsExistOnlyInFallbackBucket drives a full
// Run and writes a log file into the unknown-run fallback bucket (sibling of
// the per-run folder) while leaving the per-run folder absent. Asserts that
// the resulting evidence reports LogsProduced true — the session did produce
// logs, just not under the expected run-id path.
//
// Under the old implementation this would report LogsProduced false because
// logRootHasFiles only walked the per-run folder. This test catches that bug
// and will fail until TakeSnapshot inspects the session as a whole.
func TestRun_LogsProducedIsTrueWhenLogsExistOnlyInFallbackBucket(t *testing.T) {
	h := newHarness(t)
	req := newRequest("logs-in-fallback-bucket")
	req.Key.RunID = bucketingRunID

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox
		// Write a log file into the fallback bucket, not the per-run folder.
		fallbackDir := filepath.Join(sb.LogsRoot(), "unknown-run")
		if err := os.MkdirAll(fallbackDir, 0o755); err != nil {
			t.Fatalf("creating fallback bucket dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(fallbackDir, "session.jsonl"), []byte(`{"event":"run_start"}`), 0o644); err != nil {
			t.Fatalf("writing fallback log file: %v", err)
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	var capturedEvidence domain.RunEvidence
	eval := func(ev domain.RunEvidence) domain.TestResult {
		capturedEvidence = ev
		return domain.TestResult{}
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, eval); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if !capturedEvidence.LogsProduced {
		t.Error(
			"capturedEvidence.LogsProduced = false, want true — " +
				"the session produced logs in the fallback bucket; the signal must reflect the whole session, not just the per-run folder",
		)
	}
}

// TestRun_FallbackBucketPresentIsTrueWhenFallbackBucketExists asserts that
// RunEvidence.FallbackBucketPresent is true when the unknown-run directory
// exists alongside the run-id folder, regardless of whether it contains any
// files. This lets conditions.go produce a non-misleading signal without
// performing I/O itself.
func TestRun_FallbackBucketPresentIsTrueWhenFallbackBucketExists(t *testing.T) {
	h := newHarness(t)
	req := newRequest("fallback-bucket-present")
	req.Key.RunID = bucketingRunID

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox
		// Create the fallback bucket directory (it exists; may be empty).
		fallbackDir := filepath.Join(sb.LogsRoot(), "unknown-run")
		if err := os.MkdirAll(fallbackDir, 0o755); err != nil {
			t.Fatalf("creating fallback bucket dir: %v", err)
		}
		// Also write a file so LogsProduced becomes true via the fallback.
		if err := os.WriteFile(filepath.Join(fallbackDir, "session.jsonl"), []byte(`{"event":"run_start"}`), 0o644); err != nil {
			t.Fatalf("writing fallback log file: %v", err)
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	var capturedEvidence domain.RunEvidence
	eval := func(ev domain.RunEvidence) domain.TestResult {
		capturedEvidence = ev
		return domain.TestResult{}
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, eval); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if !capturedEvidence.FallbackBucketPresent {
		t.Error(
			"capturedEvidence.FallbackBucketPresent = false, want true — " +
				"the unknown-run directory was created; FallbackBucketPresent must reflect its existence",
		)
	}
}

// TestRun_FallbackBucketPresentIsFalseWhenFallbackBucketAbsent asserts that
// FallbackBucketPresent is false when no unknown-run directory exists. This
// is the normal case: the run's identity bound correctly and all logs landed
// in the per-run folder. FallbackBucketPresent must not be set speculatively.
func TestRun_FallbackBucketPresentIsFalseWhenFallbackBucketAbsent(t *testing.T) {
	h := newHarness(t)
	req := newRequest("no-fallback-bucket")
	req.Key.RunID = bucketingRunID

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox
		// Write logs into the run-id folder only; no fallback bucket.
		if err := os.MkdirAll(sb.LogRoot(), 0o755); err != nil {
			t.Fatalf("creating log root: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sb.LogRoot(), "session.jsonl"), []byte(`{"event":"run_start"}`), 0o644); err != nil {
			t.Fatalf("writing run log file: %v", err)
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	var capturedEvidence domain.RunEvidence
	eval := func(ev domain.RunEvidence) domain.TestResult {
		capturedEvidence = ev
		return domain.TestResult{}
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, eval); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if capturedEvidence.FallbackBucketPresent {
		t.Error(
			"capturedEvidence.FallbackBucketPresent = true, want false — " +
				"no unknown-run directory was created; the field must not be set speculatively",
		)
	}
}

// TestRun_LogsProducedFalseWhenNeitherRunFolderNorFallbackHasLogs asserts
// that LogsProduced is false when the entire OrchestrationLogs tree is empty
// — neither the per-run folder nor the fallback bucket contains any files.
// This is the genuine "no logs" case that raises ConditionNoLogsProduced.
func TestRun_LogsProducedFalseWhenNeitherRunFolderNorFallbackHasLogs(t *testing.T) {
	h := newHarness(t)
	req := newRequest("no-logs-anywhere")
	req.Key.RunID = bucketingRunID

	// Launcher does not write any log files.
	var capturedEvidence domain.RunEvidence
	eval := func(ev domain.RunEvidence) domain.TestResult {
		capturedEvidence = ev
		return domain.TestResult{}
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, eval); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if capturedEvidence.LogsProduced {
		t.Error(
			"capturedEvidence.LogsProduced = true, want false — " +
				"no log files were written anywhere in the OrchestrationLogs tree",
		)
	}
}
