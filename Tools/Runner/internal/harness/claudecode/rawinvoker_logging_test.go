package claudecode_test

// Tests for InvokeRaw debug-log emission on ClaudeCodeAdapter.
// Uses the fake-CLI helper process defined in helperprocess_test.go.

import (
	"context"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness/claudecode"
)

func TestClaudeCodeAdapter_InvokeRaw_Success_LogsInvokeEvents(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "success")

	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want ClaudeCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), ccOrchestratorRef(t), rawPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !logger.eventLogged(domain.EventHarnessInvokeStart) {
		t.Errorf("want %q logged, but it was not", domain.EventHarnessInvokeStart)
	}
	if !logger.eventLogged(domain.EventHarnessInvokeOK) {
		t.Errorf("want %q logged on success, but it was not", domain.EventHarnessInvokeOK)
	}
}

func TestClaudeCodeAdapter_InvokeRaw_Failure_LogsInvokeError(t *testing.T) {
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger("/nonexistent/claude", 5*time.Second, logger)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want ClaudeCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), ccOrchestratorRef(t), rawPayload)
	if err == nil {
		t.Fatal("want error for missing executable, got nil")
	}

	if !logger.eventLogged(domain.EventHarnessInvokeError) {
		t.Errorf("want %q logged on failure, but it was not", domain.EventHarnessInvokeError)
	}
}
