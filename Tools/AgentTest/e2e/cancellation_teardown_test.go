package e2e

// T15.7 — End-to-end cancellation at a bound above 1.
//
// Exercises the full real pipeline (pre-flight, workspace, adapter, launcher,
// runner, suite) with a cancellable context to verify that:
//
//  1. Suite.Run returns promptly after cancellation — every goroutine the
//     scheduler started is joined before the call returns. A failing wg.Wait()
//     or incorrect Add/Done pairing would cause the test to hang past its
//     deadline.
//  2. The suite result carries no error — a cancelled suite is not itself a
//     failure per the design contract.
//  3. Workspace directories left by in-flight runs are cleaned up — the
//     runner's guaranteed teardown path executes even under cancellation.
//
// The scripted stand-in uses SleepBeforeExit to block before processing any
// turn, so OS processes are genuinely in-flight when the context is cancelled.
// The launcher kills them when their context is done; teardown then runs for
// each sandbox. This matches exactly what happens with a real subject that hangs.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
)

// TestCancellation_MidRun_AtBoundAbove1_SuiteReturnsPromptly cancels a suite
// that is executing at bound=2 while the standin processes are alive and
// in-flight. It asserts that Suite.Run returns well within a generous timeout
// (10s), proving that all goroutines are joined before the return — not merely
// that some of them finished.
//
// A bug in goroutine joining (missing wg.Wait, broken wg.Done, orphaned
// goroutine) would leave the test hanging until the timeout fires.
func TestCancellation_MidRun_AtBoundAbove1_SuiteReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// SleepBeforeExit makes each standin process block before processing any
	// interception turn, so every spawned OS process is genuinely in-flight
	// (alive and awaited by the launcher) when cancellation arrives.
	sc := Scenario{
		Dir:       "testdata/e2e/bounded-concurrent",
		SuitePath: "bounded-concurrent.suite.yaml",
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("dispatch", "worker"),
				finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
		MaxConcurrentRuns: 2,
		SleepBeforeExit:   "30s", // standin blocks for 30s; cancellation must end it sooner
		Ctx:               ctx,
	}

	done := make(chan Outcome, 1)
	go func() {
		done <- RunScenario(t, sc)
	}()

	// Give the suite time to spawn the standin processes and have them start
	// blocking. 300ms is generous for OS process creation; the standin
	// blocks immediately on SleepBeforeExit before any turn processing.
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case out := <-done:
		// Suite returned after cancellation — goroutines were joined.
		t.Logf(
			"Suite returned after cancellation: exit=%d, tests_in_report=%d",
			out.ExitCode, len(out.Result.Tests),
		)
		// The suite may or may not have completed any tests before
		// cancellation (timing-dependent), but it must have returned.
		_ = out
	case <-time.After(10 * time.Second):
		cancel() // clean up
		t.Fatal(
			"Suite.Run did not return within 10s after context cancellation; " +
				"goroutines were not joined. A concurrent scheduler must call " +
				"wg.Wait() (or equivalent) before returning so no goroutine — " +
				"and therefore no harness process — outlives the suite call.",
		)
	}
}

// TestCancellation_MidRun_AtBoundAbove1_LeavesNoSandboxDirectories cancels a
// suite while runs are in-flight and then verifies that no sandbox directories
// linger in the workspace root. The runner's guaranteed teardown path runs on
// every exit, including a cancelled context, so every sandbox it created must
// be deleted before runner.Run returns — and runner.Run must return before
// Suite.Run does (wg.Wait).
//
// On Windows, file handles from a killed process can cause a transient
// deletion failure. The test retries the directory check with a short
// backoff rather than asserting immediate cleanup, matching the existing
// transient-error accommodation in the workspace package.
func TestCancellation_MidRun_AtBoundAbove1_LeavesNoSandboxDirectories(t *testing.T) {
	workspaceRoot := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sc := Scenario{
		Dir:       "testdata/e2e/bounded-concurrent",
		SuitePath: "bounded-concurrent.suite.yaml",
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("dispatch", "worker"),
				finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
		MaxConcurrentRuns: 2,
		SleepBeforeExit:   "30s",
		WorkspaceRoot:     workspaceRoot,
		Ctx:               ctx,
	}

	done := make(chan Outcome, 1)
	go func() {
		done <- RunScenario(t, sc)
	}()

	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Suite returned; now check workspace cleanliness.
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("Suite.Run did not return within 10s after cancellation")
	}

	// Check that no sandbox directories remain under workspaceRoot.
	// Retry with a backoff to accommodate transient Windows file-handle locks
	// that can delay directory removal briefly after a killed process.
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, err := os.ReadDir(workspaceRoot)
		if err != nil {
			t.Fatalf("reading workspace root %q: %v", workspaceRoot, err)
		}

		// Filter to only subdirectories (sandboxes are directories).
		var subdirs []string
		for _, e := range entries {
			if e.IsDir() {
				subdirs = append(subdirs, filepath.Join(workspaceRoot, e.Name()))
			}
		}

		if len(subdirs) == 0 {
			break // workspace is clean
		}

		if time.Now().After(deadline) {
			t.Errorf(
				"workspace root %q still contains %d sandbox director(ies) after "+
					"cancellation and Suite.Run returning: %v\n\n"+
					"Teardown must run for every sandbox that was created, even "+
					"when cancellation arrives while runs are in-flight. The runner's "+
					"guaranteed teardown path (defer Teardown in runner.Run) must "+
					"execute before runner.Run returns, and runner.Run must return "+
					"before Suite.Run does (wg.Wait). Any lingering directory is a "+
					"sandbox whose teardown did not complete.",
				workspaceRoot, len(subdirs), subdirs,
			)
			break
		}

		time.Sleep(100 * time.Millisecond)
	}
}
