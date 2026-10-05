package claudecode_test

// Tests for ClaudeCodeAdapter subprocess lifecycle: cancellation, timeout, missing executable, non-zero exit.
// Uses the fake-CLI helper process defined in helperprocess_test.go.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/harness/claudecode"
	"mosaic-run/internal/harness/cliexec"
)

// ---------------------------------------------------------------------------
// Subprocess lifecycle
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_ContextCancellation_ReturnsCtxErr verifies that
// cancelling the context causes Invoke to terminate the subprocess and return
// ctx.Err().
func TestClaudeCodeAdapter_ContextCancellation_ReturnsCtxErr(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 30*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := adapter.Invoke(ctx, ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

// TestClaudeCodeAdapter_Timeout_ReturnsErrTimeout verifies that an invocation
// that exceeds the configured timeout returns ErrTimeout.
func TestClaudeCodeAdapter_Timeout_ReturnsErrTimeout(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	// Very short timeout so the test completes quickly.
	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 100*time.Millisecond)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, cliexec.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

// TestClaudeCodeAdapter_MissingExecutable_ReturnsErrExecutableNotFound verifies
// that invoking with a nonexistent executable path returns ErrExecutableNotFound.
func TestClaudeCodeAdapter_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	adapter := claudecode.NewClaudeCodeAdapter("/nonexistent/path/to/claude", 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, cliexec.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

// TestClaudeCodeAdapter_NonZeroExit_ReturnsErrNonZeroExit verifies that a
// subprocess exiting with a non-zero status returns ErrNonZeroExit.
func TestClaudeCodeAdapter_NonZeroExit_ReturnsErrNonZeroExit(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "exit1")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, cliexec.ErrNonZeroExit) {
		t.Errorf("want ErrNonZeroExit, got %v", err)
	}
}

// TestClaudeCodeAdapter_NonZeroExit_ErrorContainsStderr verifies that the
// ErrNonZeroExit error carries the subprocess stderr content for traceability.
func TestClaudeCodeAdapter_NonZeroExit_ErrorContainsStderr(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "exit1")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	// "simulated stderr output" is written by the helper's exit1 case.
	if !strings.Contains(err.Error(), "simulated stderr output") {
		t.Errorf("want error message to include stderr content, got %q", err.Error())
	}
}
