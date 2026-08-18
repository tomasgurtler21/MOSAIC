package harness_test

// Tests for GHCPCLIAdapter.Invoke, following the structure of opencode_test.go.
//
// Reuses the shared fake-subprocess infrastructure defined in
// claudecode_test.go (TestMain, runHelperProcess, setHelperEnv, helperExe,
// readArgs, containsArg, containsSequence, indexOfArg, ordinaryAgentRef,
// orchestratorAgentRef, minimalClaudeRequest, recordingLogger) since all
// three adapter test files live in this package.
//
// Coverage:
//
//   Request mapping:
//   - The -p prompt carries the marshalled request for both invocation kinds.
//   - --agent <Identifier> is emitted for both invocation kinds.
//   - SystemPrompt is left unset: no <env> block appears in the prompt
//     for either invocation kind, unlike OpenCodeAdapter which always injects
//     one because --agent replaces the default system prompt for that harness.
//   - domain.AgentReference.InvocationKind is mapped onto commonharness.AgentRef.Kind.
//
//   Protocol response mapping:
//   - A successful GHCP CLI event stream decodes into domain.ProtocolResponse.
//
//   Sentinel error taxonomy:
//   - Missing executable returns ErrExecutableNotFound.
//   - Timeout returns ErrTimeout.
//   - A stream-reported failure (non-zero exitCode on the result line) returns
//     ErrGHCPCLIStreamError.
//   - An incomplete stream (no result line) returns ErrGHCPCLIStreamIncomplete.
//   - ErrGHCPCLIStreamError and ErrGHCPCLIStreamIncomplete do not satisfy
//     errors.Is against ErrStreamError / ErrStreamIncomplete from OpenCodeAdapter,
//     keeping the two harnesses' failure taxonomies distinguishable at a Runner
//     call site.
//
//   Context cancellation:
//   - Cancelling the context returns ctx.Err(), not the shared ErrCancelled
//     sentinel. Runner's existing call sites and tests expect ctx.Err().
//
//   Malformed protocol response:
//   - A successful stream whose accumulated content cannot be decoded into
//     domain.ProtocolResponse returns an error wrapping ErrMalformedOutput.
//
//   Debug-log sink adaptation:
//   - EventHarnessInvokeStart is logged for every invocation.
//   - EventHarnessInvokeOK is logged on success.
//   - EventHarnessParseFailed is logged for stream-class failures
//     (ErrGHCPCLIStreamError and ErrGHCPCLIStreamIncomplete), reusing the
//     existing debugLoggerSink rather than duplicating it.
//   - EventHarnessInvokeError is logged on every failure path.

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

// TestGHCPCLIAdapter_OrdinaryInvocation_PromptCarriesMarshalledRequest verifies
// that the -p prompt value contains the marshalled request JSON for ordinary
// invocations.
func TestGHCPCLIAdapter_OrdinaryInvocation_PromptCarriesMarshalledRequest(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	if !strings.Contains(args[pIdx+1], "test-agent#1") {
		t.Errorf("want -p value to contain the marshalled request (agent_instance_id), got %q", args[pIdx+1])
	}
}

// TestGHCPCLIAdapter_OrchestratorInvocation_PromptCarriesMarshalledRequest
// verifies the same for orchestrator invocations.
func TestGHCPCLIAdapter_OrchestratorInvocation_PromptCarriesMarshalledRequest(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	if !strings.Contains(args[pIdx+1], "orchestrator-agent#1") {
		t.Errorf("want -p value to contain the marshalled request (agent_instance_id), got %q", args[pIdx+1])
	}
}

// TestGHCPCLIAdapter_OrdinaryInvocation_IncludesAgentFlag verifies that
// ordinary invocations include --agent <Identifier>.
func TestGHCPCLIAdapter_OrdinaryInvocation_IncludesAgentFlag(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")
	agent := ordinaryAgentRef()

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args for ordinary invocation, got %v", agent.Identifier, args)
	}
}

// TestGHCPCLIAdapter_OrchestratorInvocation_IncludesAgentFlag verifies that
// orchestrator invocations include --agent <Identifier>.
func TestGHCPCLIAdapter_OrchestratorInvocation_IncludesAgentFlag(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")
	agent := orchestratorAgentRef()

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args for orchestrator invocation, got %v", agent.Identifier, args)
	}
}

// TestGHCPCLIAdapter_OrdinaryInvocation_NoEnvBlockInPrompt verifies that the
// adapter does NOT inject an <env> block into the prompt for ordinary
// invocations. Unlike OpenCodeAdapter (which always sets SystemPrompt to an
// EnvBlock because --agent replaces the default system prompt for that harness),
// GHCPCLIAdapter never sets SystemPrompt: the harness layers instructions from
// files it discovers itself, and any orchestration context is rendered into the
// agent's .agent.md profile at deploy time by Tools/Deployment.
func TestGHCPCLIAdapter_OrdinaryInvocation_NoEnvBlockInPrompt(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	if strings.Contains(args[pIdx+1], "<env>") {
		t.Errorf("want NO <env> block in ordinary invocation prompt (SystemPrompt must be left unset), got %q", args[pIdx+1])
	}
}

// TestGHCPCLIAdapter_OrchestratorInvocation_NoEnvBlockInPrompt verifies that
// SystemPrompt is also left unset for orchestrator invocations. Both invocation
// kinds must leave the <env> injection absent.
func TestGHCPCLIAdapter_OrchestratorInvocation_NoEnvBlockInPrompt(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	if strings.Contains(args[pIdx+1], "<env>") {
		t.Errorf("want NO <env> block in orchestrator invocation prompt (SystemPrompt must be left unset), got %q", args[pIdx+1])
	}
}

// ---------------------------------------------------------------------------
// Protocol response mapping
// ---------------------------------------------------------------------------

// TestGHCPCLIAdapter_SuccessfulStream_ReturnsParsedResponse verifies that a
// GHCP CLI event stream terminating with exitCode:0 on the result line
// decodes into a domain.ProtocolResponse.
func TestGHCPCLIAdapter_SuccessfulStream_ReturnsParsedResponse(t *testing.T) {
	setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
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
// Sentinel error taxonomy
// ---------------------------------------------------------------------------

// TestGHCPCLIAdapter_MissingExecutable_ReturnsErrExecutableNotFound verifies
// that an unresolvable executable path returns ErrExecutableNotFound.
func TestGHCPCLIAdapter_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	adapter := harness.NewGHCPCLIAdapter("/nonexistent/path/to/copilot", 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

// TestGHCPCLIAdapter_Timeout_ReturnsErrTimeout verifies that an invocation
// exceeding the configured timeout returns ErrTimeout.
func TestGHCPCLIAdapter_Timeout_ReturnsErrTimeout(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 100*time.Millisecond)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

// TestGHCPCLIAdapter_StreamReportedFailure_ReturnsErrGHCPCLIStreamError is the
// stream-verdict trap test at the adapter layer: the helper process exits 0 but
// its result event has a non-zero exitCode at the top level. The stream's own
// verdict must drive the adapter's outcome, not the process exit code.
func TestGHCPCLIAdapter_StreamReportedFailure_ReturnsErrGHCPCLIStreamError(t *testing.T) {
	setHelperEnv(t, "ghcpcli-stream-error")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrGHCPCLIStreamError) {
		t.Errorf("want ErrGHCPCLIStreamError despite a zero process exit code, got %v", err)
	}
}

// TestGHCPCLIAdapter_IncompleteStream_ReturnsErrGHCPCLIStreamIncomplete verifies
// that a stream ending without any result event is never treated as success.
func TestGHCPCLIAdapter_IncompleteStream_ReturnsErrGHCPCLIStreamIncomplete(t *testing.T) {
	setHelperEnv(t, "ghcpcli-incomplete")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrGHCPCLIStreamIncomplete) {
		t.Errorf("want ErrGHCPCLIStreamIncomplete, got %v", err)
	}
}

// TestGHCPCLISentinels_DoNotCollideWithOpenCodeSentinels verifies that
// ErrGHCPCLIStreamError and ErrGHCPCLIStreamIncomplete are distinct from the
// OpenCodeAdapter's ErrStreamError and ErrStreamIncomplete aliases. Two harnesses
// with structurally similar failure modes (OpenCode always exits 0; GHCP CLI's
// terminal result event carries the verdict) must remain distinguishable at a
// Runner call site so a caller can tell which harness failed and why.
func TestGHCPCLISentinels_DoNotCollideWithOpenCodeSentinels(t *testing.T) {
	if errors.Is(harness.ErrGHCPCLIStreamError, harness.ErrStreamError) {
		t.Error("ErrGHCPCLIStreamError must not satisfy errors.Is against ErrStreamError: the two harnesses' stream-error sentinels must be distinguishable")
	}
	if errors.Is(harness.ErrGHCPCLIStreamIncomplete, harness.ErrStreamIncomplete) {
		t.Error("ErrGHCPCLIStreamIncomplete must not satisfy errors.Is against ErrStreamIncomplete: the two harnesses' incomplete-stream sentinels must be distinguishable")
	}
	// Verify the reverse direction too: no accidental aliasing either way.
	if errors.Is(harness.ErrStreamError, harness.ErrGHCPCLIStreamError) {
		t.Error("ErrStreamError must not satisfy errors.Is against ErrGHCPCLIStreamError")
	}
	if errors.Is(harness.ErrStreamIncomplete, harness.ErrGHCPCLIStreamIncomplete) {
		t.Error("ErrStreamIncomplete must not satisfy errors.Is against ErrGHCPCLIStreamIncomplete")
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

// TestGHCPCLIAdapter_ContextCancellation_ReturnsCtxErr verifies that cancelling
// the context terminates the subprocess and returns ctx.Err(), not the shared
// ErrCancelled sentinel. Runner's existing call sites and tests expect ctx.Err().
func TestGHCPCLIAdapter_ContextCancellation_ReturnsCtxErr(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 30*time.Second)

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
// Malformed protocol response
// ---------------------------------------------------------------------------

// TestGHCPCLIAdapter_MalformedProtocolPayload_ReturnsErrMalformedOutput verifies
// that a stream the parser accepts as successful but whose accumulated content
// cannot be decoded into domain.ProtocolResponse returns an error wrapping
// ErrMalformedOutput.
func TestGHCPCLIAdapter_MalformedProtocolPayload_ReturnsErrMalformedOutput(t *testing.T) {
	setHelperEnv(t, "ghcpcli-decode-fail")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrMalformedOutput) {
		t.Errorf("want ErrMalformedOutput for an undecoded protocol payload, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Debug-log sink adaptation
// ---------------------------------------------------------------------------

// TestGHCPCLIAdapterWithLogger_SuccessfulInvocation_LogsInvokeStart verifies
// that NewGHCPCLIAdapterWithLogger emits EventHarnessInvokeStart for each call
// to Invoke, matching ClaudeCodeAdapter's and OpenCodeAdapter's logging contract.
func TestGHCPCLIAdapterWithLogger_SuccessfulInvocation_LogsInvokeStart(t *testing.T) {
	setHelperEnv(t, "ghcpcli-success")
	logger := &recordingLogger{}
	adapter := harness.NewGHCPCLIAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !logger.eventLogged(domain.EventHarnessInvokeStart) {
		t.Errorf("want %s logged, got events: %v", domain.EventHarnessInvokeStart, logger.allEvents())
	}
}

// TestGHCPCLIAdapterWithLogger_SuccessfulInvocation_LogsInvokeOK verifies
// that a successful invocation emits EventHarnessInvokeOK.
func TestGHCPCLIAdapterWithLogger_SuccessfulInvocation_LogsInvokeOK(t *testing.T) {
	setHelperEnv(t, "ghcpcli-success")
	logger := &recordingLogger{}
	adapter := harness.NewGHCPCLIAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !logger.eventLogged(domain.EventHarnessInvokeOK) {
		t.Errorf("want %s logged on success, got events: %v", domain.EventHarnessInvokeOK, logger.allEvents())
	}
}

// TestGHCPCLIAdapterWithLogger_StreamError_LogsParseFailed verifies that a
// stream-class failure (ErrGHCPCLIStreamError) logs EventHarnessParseFailed,
// reusing the existing debugLoggerSink rather than duplicating it.
func TestGHCPCLIAdapterWithLogger_StreamError_LogsParseFailed(t *testing.T) {
	setHelperEnv(t, "ghcpcli-stream-error")
	logger := &recordingLogger{}
	adapter := harness.NewGHCPCLIAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessParseFailed) {
		t.Errorf("want %s logged for a stream-reported failure, got events: %v", domain.EventHarnessParseFailed, logger.allEvents())
	}
}

// TestGHCPCLIAdapterWithLogger_IncompleteStream_LogsParseFailed verifies the
// same for ErrGHCPCLIStreamIncomplete.
func TestGHCPCLIAdapterWithLogger_IncompleteStream_LogsParseFailed(t *testing.T) {
	setHelperEnv(t, "ghcpcli-incomplete")
	logger := &recordingLogger{}
	adapter := harness.NewGHCPCLIAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessParseFailed) {
		t.Errorf("want %s logged for an incomplete stream, got events: %v", domain.EventHarnessParseFailed, logger.allEvents())
	}
}

// TestGHCPCLIAdapterWithLogger_Failure_LogsInvokeError verifies that every
// failure path emits EventHarnessInvokeError.
func TestGHCPCLIAdapterWithLogger_Failure_LogsInvokeError(t *testing.T) {
	setHelperEnv(t, "ghcpcli-stream-error")
	logger := &recordingLogger{}
	adapter := harness.NewGHCPCLIAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessInvokeError) {
		t.Errorf("want %s logged on failure, got events: %v", domain.EventHarnessInvokeError, logger.allEvents())
	}
}
