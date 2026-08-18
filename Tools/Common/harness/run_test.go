package harness_test

// Tests for Run: the plan-execution entry point that takes an already
// resolved command, arguments and run options rather than a SpawnRequest.
// This is the entry point mosaic-agent-test consumes, so its harness
// adapters produce a declarative spawn plan without doing any argument
// construction, executable resolution or envelope parsing of their own.
//
// Coverage:
//   - honours WorkingDir, Env additions, Stdin and Timeout
//   - timeout and parent-context cancellation stay distinguishable
//   - non-zero exit and an absent executable produce the expected sentinels

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-common/harness"
)

func TestRun_HonoursWorkingDir(t *testing.T) {
	setHelperEnv(t, "echo-cwd")
	dir := t.TempDir()

	resp, err := harness.Run(context.Background(), harness.Command{Path: helperExe(t)}, nil, harness.RunOptions{
		WorkingDir: dir,
		Timeout:    5 * time.Second,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(resp.Stdout), filepath.Base(dir)) {
		t.Errorf("want subprocess cwd to reflect WorkingDir %q, got stdout %q", dir, resp.Stdout)
	}
}

func TestRun_HonoursEnvAdditions(t *testing.T) {
	setHelperEnv(t, "echo-env")
	t.Setenv("GO_HELPER_ECHO_VAR", "MOSAIC_TEST_VAR")

	resp, err := harness.Run(context.Background(), harness.Command{Path: helperExe(t)}, nil, harness.RunOptions{
		Env:     []string{"MOSAIC_TEST_VAR=injected-value"},
		Timeout: 5 * time.Second,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(resp.Stdout) != "injected-value" {
		t.Errorf("want the RunOptions.Env addition to be visible to the subprocess, got stdout %q", resp.Stdout)
	}
}

func TestRun_HonoursStdin(t *testing.T) {
	setHelperEnv(t, "echo-stdin")

	resp, err := harness.Run(context.Background(), harness.Command{Path: helperExe(t)}, nil, harness.RunOptions{
		Stdin:   []byte("piped-input"),
		Timeout: 5 * time.Second,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(resp.Stdout), "piped-input") {
		t.Errorf("want RunOptions.Stdin delivered to the subprocess, got stdout %q", resp.Stdout)
	}
}

func TestRun_Timeout_ReturnsErrTimeout(t *testing.T) {
	setHelperEnv(t, "hang")

	_, err := harness.Run(context.Background(), harness.Command{Path: helperExe(t)}, nil, harness.RunOptions{
		Timeout: 100 * time.Millisecond,
	})

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

func TestRun_ParentContextCancellation_ReturnsErrCancelled(t *testing.T) {
	setHelperEnv(t, "hang")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := harness.Run(ctx, harness.Command{Path: helperExe(t)}, nil, harness.RunOptions{
		Timeout: 30 * time.Second,
	})

	if !errors.Is(err, harness.ErrCancelled) {
		t.Errorf("want ErrCancelled, got %v", err)
	}
}

func TestRun_TimeoutAndCancellation_AreDistinguishable(t *testing.T) {
	// Same underlying condition (the process hangs) but two different causes
	// must yield two different, errors.Is-distinguishable sentinels.
	setHelperEnv(t, "hang")

	_, timeoutErr := harness.Run(context.Background(), harness.Command{Path: helperExe(t)}, nil, harness.RunOptions{
		Timeout: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, cancelErr := harness.Run(ctx, harness.Command{Path: helperExe(t)}, nil, harness.RunOptions{
		Timeout: 30 * time.Second,
	})

	if errors.Is(timeoutErr, harness.ErrCancelled) {
		t.Errorf("timeout must not also satisfy errors.Is(err, ErrCancelled), got %v", timeoutErr)
	}
	if errors.Is(cancelErr, harness.ErrTimeout) {
		t.Errorf("cancellation must not also satisfy errors.Is(err, ErrTimeout), got %v", cancelErr)
	}
}

func TestRun_NonZeroExit_WithStderr_ReturnsErrNonZeroExitContainingStderr(t *testing.T) {
	setHelperEnv(t, "exit1")

	_, err := harness.Run(context.Background(), harness.Command{Path: helperExe(t)}, nil, harness.RunOptions{
		Timeout: 5 * time.Second,
	})

	if !errors.Is(err, harness.ErrNonZeroExit) {
		t.Errorf("want ErrNonZeroExit, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "simulated stderr output") {
		t.Errorf("want error to include captured stderr content, got %v", err)
	}
}

func TestRun_NonZeroExit_WithoutStderr_ReturnsErrNonZeroExit(t *testing.T) {
	setHelperEnv(t, "exit1-no-stderr")

	_, err := harness.Run(context.Background(), harness.Command{Path: helperExe(t)}, nil, harness.RunOptions{
		Timeout: 5 * time.Second,
	})

	if !errors.Is(err, harness.ErrNonZeroExit) {
		t.Errorf("want ErrNonZeroExit even with no captured stderr, got %v", err)
	}
}

func TestRun_AbsentExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	_, err := harness.Run(context.Background(), harness.Command{Path: "/nonexistent/path/to/claude"}, nil, harness.RunOptions{
		Timeout: 5 * time.Second,
	})

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}
