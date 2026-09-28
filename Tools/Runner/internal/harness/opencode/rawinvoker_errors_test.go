package opencode_test

// Tests for InvokeRaw failure sentinels on all three adapters: missing executable, timeout, empty output, context cancellation.
// Uses the fake-CLI helper process defined in helperprocess_test.go.

import (
	"context"
	"errors"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness/cliexec"
	"mosaic-run/internal/harness/opencode"
)

func TestOpenCodeAdapter_InvokeRaw_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	adapter := opencode.NewOpenCodeAdapter("/nonexistent/path/to/opencode", 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want OpenCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), orchestratorRef(), rawPayload)

	if !errors.Is(err, cliexec.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

func TestOpenCodeAdapter_InvokeRaw_Timeout_ReturnsErrTimeout(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := opencode.NewOpenCodeAdapter(helperExe(t), 100*time.Millisecond)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want OpenCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), orchestratorRef(), rawPayload)

	if !errors.Is(err, cliexec.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

func TestOpenCodeAdapter_InvokeRaw_EmptyOutput_ReturnsErrEmptyResponse(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "empty")

	adapter := opencode.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want OpenCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), orchestratorRef(), rawPayload)

	if !errors.Is(err, cliexec.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

func TestOpenCodeAdapter_InvokeRaw_ContextCancellation_ReturnsCtxErr(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := opencode.NewOpenCodeAdapter(helperExe(t), 30*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want OpenCodeAdapter to implement domain.RawInvoker")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := ri.InvokeRaw(ctx, orchestratorRef(), rawPayload)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled on cancellation, got %v", err)
	}
}
