package harness_test

// Tests for OpenCodeAdapter (T4.2).
//
// Reuses the shared fake-subprocess infrastructure defined in
// claudecode_test.go (TestMain, runHelperProcess, setHelperEnv, helperExe,
// readArgs, containsArg, containsSequence, indexOfArg, ordinaryAgentRef,
// orchestratorAgentRef, minimalClaudeRequest, recordingLogger) since both
// test files live in this package.
//
// Coverage:
//
//   Request mapping:
//   - Includes --agent <Identifier> for both invocation kinds.
//   - Includes --format json.
//   - Includes --auto.
//   - The positional message carries the marshalled request.
//   - SystemPrompt carries a synthesized <env> block for BOTH invocation
//     kinds (unlike ClaudeCodeAdapter, which only synthesizes one for the
//     orchestrator kind) — OpenCode's --agent replaces the default system
//     prompt in both cases.
//
//   Protocol response mapping:
//   - A successful stream-terminated run decodes into domain.ProtocolResponse.
//
//   Sentinel error taxonomy:
//   - Missing executable returns ErrExecutableNotFound.
//   - Non-zero exit returns ErrNonZeroExit.
//   - Timeout returns ErrTimeout.
//   - A stream-reported failure (zero exit, error event) returns ErrStreamError.
//   - An incomplete stream (zero exit, no terminal event) returns ErrStreamIncomplete.
//
//   Context cancellation:
//   - Cancelling the context returns ctx.Err().
//
//   Debug-log sink adaptation:
//   - A stream-class failure logs EventHarnessParseFailed, reusing the
//     existing debugLoggerSink/sinkEventName rather than duplicating them.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ---------------------------------------------------------------------------
// Request mapping
// ---------------------------------------------------------------------------

// TestOpenCodeAdapter_OrdinaryInvocation_IncludesAgentFlag verifies that
// ordinary invocations include --agent <Identifier>, mirroring the
// orchestrator case: OpenCode selects any agent (subagent or primary) by
// name through the same flag.
func TestOpenCodeAdapter_OrdinaryInvocation_IncludesAgentFlag(t *testing.T) {
	argsFile := setHelperEnv(t, "opencode-success")
	agent := ordinaryAgentRef()

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args, got %v", agent.Identifier, args)
	}
}

// TestOpenCodeAdapter_OrchestratorInvocation_IncludesAgentFlag verifies that
// orchestrator invocations include --agent <Identifier>.
func TestOpenCodeAdapter_OrchestratorInvocation_IncludesAgentFlag(t *testing.T) {
	argsFile := setHelperEnv(t, "opencode-success")
	agent := orchestratorAgentRef()

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args, got %v", agent.Identifier, args)
	}
}

// TestOpenCodeAdapter_IncludesFormatJSON verifies that --format json is
// present regardless of invocation kind.
func TestOpenCodeAdapter_IncludesFormatJSON(t *testing.T) {
	argsFile := setHelperEnv(t, "opencode-success")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--format", "json") {
		t.Errorf("want --format json in args, got %v", args)
	}
}

// TestOpenCodeAdapter_IncludesAutoFlag verifies that --auto is present.
func TestOpenCodeAdapter_IncludesAutoFlag(t *testing.T) {
	argsFile := setHelperEnv(t, "opencode-success")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsArg(args, "--auto") {
		t.Errorf("want --auto in args, got %v", args)
	}
}

// TestOpenCodeAdapter_PositionalMessageCarriesMarshalledRequest verifies that
// the final positional argument contains the marshalled request JSON.
func TestOpenCodeAdapter_PositionalMessageCarriesMarshalledRequest(t *testing.T) {
	argsFile := setHelperEnv(t, "opencode-success")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if len(args) == 0 {
		t.Fatalf("want at least one arg, got none")
	}
	if !strings.Contains(args[len(args)-1], "test-agent#1") {
		t.Errorf("want the positional message to contain the marshalled request (agent_instance_id), got %q", args[len(args)-1])
	}
}

// TestOpenCodeAdapter_OrdinaryInvocation_SystemPromptCarriesEnvBlock verifies
// that, unlike ClaudeCodeAdapter, an ORDINARY invocation also loses the
// default system prompt (OpenCode's --agent replaces it for every
// invocation kind), so the adapter must supply a synthesized <env> block via
// SystemPrompt for the ordinary case too.
func TestOpenCodeAdapter_OrdinaryInvocation_SystemPromptCarriesEnvBlock(t *testing.T) {
	argsFile := setHelperEnv(t, "opencode-success")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if len(args) == 0 {
		t.Fatalf("want at least one arg, got none")
	}
	message := args[len(args)-1]
	if !strings.Contains(message, "<env>") {
		t.Errorf("want a synthesized <env> block in the ordinary invocation's positional message, got %q", message)
	}
}

// TestOpenCodeAdapter_OrchestratorInvocation_SystemPromptCarriesEnvBlock
// verifies the same for the orchestrator invocation kind.
func TestOpenCodeAdapter_OrchestratorInvocation_SystemPromptCarriesEnvBlock(t *testing.T) {
	argsFile := setHelperEnv(t, "opencode-success")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if len(args) == 0 {
		t.Fatalf("want at least one arg, got none")
	}
	message := args[len(args)-1]
	if !strings.Contains(message, "<env>") {
		t.Errorf("want a synthesized <env> block in the orchestrator invocation's positional message, got %q", message)
	}
}

// ---------------------------------------------------------------------------
// Protocol response mapping
// ---------------------------------------------------------------------------

// TestOpenCodeAdapter_SuccessfulStream_ReturnsParsedResponse verifies that a
// stream terminating with step_finish reason "stop" decodes into a
// domain.ProtocolResponse.
func TestOpenCodeAdapter_SuccessfulStream_ReturnsParsedResponse(t *testing.T) {
	setHelperEnv(t, "opencode-success")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode=SUCCESS, got %q", resp.StatusCode)
	}
	if resp.AgentInstanceID != "test-agent#1" {
		t.Errorf("want AgentInstanceID=test-agent#1, got %q", resp.AgentInstanceID)
	}
}

// ---------------------------------------------------------------------------
// Sentinel error taxonomy (AC4.8)
// ---------------------------------------------------------------------------

// TestOpenCodeAdapter_MissingExecutable_ReturnsErrExecutableNotFound verifies
// that an unresolvable executable returns ErrExecutableNotFound — the same
// sentinel ClaudeCodeAdapter uses.
func TestOpenCodeAdapter_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	adapter := harness.NewOpenCodeAdapter("/nonexistent/path/to/opencode", 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

// TestOpenCodeAdapter_NonZeroExit_ReturnsErrNonZeroExit verifies that a
// subprocess exiting non-zero still returns ErrNonZeroExit — that path
// remains meaningful for OpenCode (missing argument, crash) even though the
// zero-exit case carries no success/failure information.
func TestOpenCodeAdapter_NonZeroExit_ReturnsErrNonZeroExit(t *testing.T) {
	setHelperEnv(t, "exit1")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrNonZeroExit) {
		t.Errorf("want ErrNonZeroExit, got %v", err)
	}
}

// TestOpenCodeAdapter_Timeout_ReturnsErrTimeout verifies that an invocation
// exceeding the configured timeout returns ErrTimeout.
func TestOpenCodeAdapter_Timeout_ReturnsErrTimeout(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 100*time.Millisecond)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

// TestOpenCodeAdapter_StreamReportedFailure_ReturnsErrStreamError is the
// zero-exit-trap test at the adapter layer: the helper process exits 0 but
// its event stream reports failure via an "error" event, and the adapter
// must still classify the invocation as a failure.
func TestOpenCodeAdapter_StreamReportedFailure_ReturnsErrStreamError(t *testing.T) {
	setHelperEnv(t, "opencode-stream-error")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrStreamError) {
		t.Errorf("want ErrStreamError despite a zero process exit code, got %v", err)
	}
}

// TestOpenCodeAdapter_IncompleteStream_ReturnsErrStreamIncomplete verifies
// that a stream ending without any terminal event returns ErrStreamIncomplete
// rather than being treated as success.
func TestOpenCodeAdapter_IncompleteStream_ReturnsErrStreamIncomplete(t *testing.T) {
	setHelperEnv(t, "opencode-incomplete")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrStreamIncomplete) {
		t.Errorf("want ErrStreamIncomplete, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

// TestOpenCodeAdapter_ContextCancellation_ReturnsCtxErr verifies that
// cancelling the context terminates the subprocess and returns ctx.Err(),
// exactly as ClaudeCodeAdapter does.
func TestOpenCodeAdapter_ContextCancellation_ReturnsCtxErr(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 30*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := adapter.Invoke(ctx, ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Debug-log sink adaptation
// ---------------------------------------------------------------------------

// TestOpenCodeAdapterWithLogger_StreamError_LogsParseFailed verifies that a
// stream-class failure (ErrStreamError) logs EventHarnessParseFailed — the
// same event ClaudeCodeAdapter logs for its own parse-class failures — and
// that the adapter reuses the package's existing debugLoggerSink rather than
// duplicating it (observable here as the event still reaching Runner's
// closed debug-log vocabulary).
func TestOpenCodeAdapterWithLogger_StreamError_LogsParseFailed(t *testing.T) {
	setHelperEnv(t, "opencode-stream-error")
	logger := &recordingLogger{}
	adapter := harness.NewOpenCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessParseFailed) {
		t.Errorf("want %s logged for a stream-reported failure, got events: %v", domain.EventHarnessParseFailed, logger.allEvents())
	}
}

// TestOpenCodeAdapterWithLogger_IncompleteStream_LogsParseFailed verifies the
// same for ErrStreamIncomplete.
func TestOpenCodeAdapterWithLogger_IncompleteStream_LogsParseFailed(t *testing.T) {
	setHelperEnv(t, "opencode-incomplete")
	logger := &recordingLogger{}
	adapter := harness.NewOpenCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessParseFailed) {
		t.Errorf("want %s logged for an incomplete stream, got events: %v", domain.EventHarnessParseFailed, logger.allEvents())
	}
}

// TestOpenCodeAdapterWithLogger_SuccessfulInvocation_LogsInvokeStart verifies
// that NewOpenCodeAdapterWithLogger emits EventHarnessInvokeStart for each
// call to Invoke, matching ClaudeCodeAdapter's logging contract.
func TestOpenCodeAdapterWithLogger_SuccessfulInvocation_LogsInvokeStart(t *testing.T) {
	setHelperEnv(t, "opencode-success")
	logger := &recordingLogger{}
	adapter := harness.NewOpenCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !logger.eventLogged(domain.EventHarnessInvokeStart) {
		t.Errorf("want %s logged, got events: %v", domain.EventHarnessInvokeStart, logger.allEvents())
	}
}
