package runner_test

// Sandbox-adoption invariant (Stage 7, T7.2): a retained sandbox must never
// be silently adopted by a later run against the same run key.
// workspace.Manager already refuses to Create in a non-empty existing
// directory (workspace.ErrDirectoryNotEmpty) — its in-memory "already
// created" bookkeeping (workspace.ErrSandboxExists) fires first for a second
// Create call against the *same* Manager instance, which would not exercise
// the disk-level refusal this invariant actually depends on.
//
// What this test pins down is the cross-process shape a real rerun takes: a
// second attempt against the same run key, driven through a fresh
// workspace.Manager instance rooted at the same directory, carrying none of
// the first manager's in-memory state — only the retained directory the
// first run left on disk.

import (
	"context"
	"errors"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
	"mosaic-agent-test/internal/workspace"
)

func TestRun_RerunAgainstARetainedSandbox_FailsClearly_NeverAdopts(t *testing.T) {
	h := newHarness(t)
	req := newRequest("retain-and-rerun")
	req.Retention = domain.RetainAlways

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("first Run returned unexpected error: %v", err)
	}

	if _, err := h.Deps.Workspaces.Locate(req.Key); err != nil {
		t.Fatalf("sandbox was not retained after the first run (Locate returned %v); the adoption guard this test exercises is meaningless without it", err)
	}

	// A fresh manager instance rooted at the same directory, standing in for
	// a later process that knows nothing about the first run's in-memory
	// bookkeeping — only the retained directory on disk.
	freshWs := &recordingWorkspace{
		inner: workspace.NewManager(h.WorkspaceRoot, h.Clock),
		rec:   &callRecorder{},
	}
	d2 := h.Deps
	d2.Workspaces = freshWs

	_, err := runner.Run(context.Background(), d2, req)
	if err == nil {
		t.Fatal("second Run against a retained sandbox's run key returned no error, want a clear refusal rather than silent adoption")
	}
	if !errors.Is(err, workspace.ErrDirectoryNotEmpty) {
		t.Errorf("second Run's error = %v, want it to wrap workspace.ErrDirectoryNotEmpty — the refusal must be surfaced clearly, not some other failure", err)
	}
}
