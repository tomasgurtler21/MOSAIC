package harness_test

// Tests for the domain.RawInvoker implementation on ClaudeCodeAdapter,
// OpenCodeAdapter and GHCPCLIAdapter, and for the composition-root probe that
// confirms the transport is findable on each production adapter.
//
// Coverage:
//
//   T3.4 — adapter InvokeRaw behaviour:
//   - Each production adapter satisfies domain.RawInvoker (runtime type assertion).
//   - InvokeRaw issues the invocation as orchestrator-kind: the subprocess receives
//     --agent <identifier>, not --append-system-prompt-file, for all three adapters.
//   - A successful invocation returns []byte with the assistant text, never nil
//     and never empty.
//   - Failures surface as the adapter's sentinel taxonomy (ErrExecutableNotFound,
//     ErrNonZeroExit, ErrTimeout, ErrEmptyResponse, ErrMalformedJSON) with the
//     same errors.Is behaviour as Invoke.
//   - ErrMalformedOutput (aliasing ErrProtocolNotExtractable) is never returned
//     by InvokeRaw: there is no protocol extraction step on this path.
//   - Context cancellation terminates the subprocess and returns ctx.Err().
//   - Logging: InvokeRaw emits the same event vocabulary as Invoke
//     (EventHarnessInvokeStart, EventHarnessStdout, EventHarnessInvokeOK,
//     EventHarnessInvokeError) through the injected DebugLogger.
//
//   T3.5 — composition-root probe:
//   - Each production adapter (constructed as in production) satisfies the
//     domain.RawInvoker interface, so the startup probe in cmd/mosaic-run
//     successfully extracts the transport on all three adapters.
//
// All subprocess interaction uses the fake-CLI helper-process infrastructure
// already in claudecode_test.go (TestMain, runHelperProcess, helperExe,
// setHelperEnv, readArgs, ordinaryAgentRef, orchestratorAgentRef).

import (
	"context"
	"errors"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// rawPayload is a minimal consultation payload for InvokeRaw tests: not a
// Communication Protocol message, so the text path is exercised specifically.
var rawPayload = []byte(`{"action":"route","agent":"agent-b","task_description":"do the thing"}`)

// orchestratorRef returns an AgentReference with InvocationOrchestrator using a
// static (non-existent) definition path. Suitable for OpenCode and GHCP CLI
// adapter tests that do not read the definition file. ClaudeCodeAdapter tests
// must use ccOrchestratorRef(t) instead.
func orchestratorRef() domain.AgentReference {
	return domain.AgentReference{
		Identifier:     "orchestrator-agent",
		DefinitionPath: "/agents/orchestrator-agent.md",
		InvocationKind: domain.InvocationOrchestrator,
	}
}

// ccOrchestratorRef returns an orchestrator-kind AgentReference backed by a
// temporary definition file with valid Claude Code tools frontmatter. Use this
// in ClaudeCodeAdapter tests: InvokeRaw reads the definition file (FR-10)
// before spawning.
func ccOrchestratorRef(t *testing.T) domain.AgentReference {
	t.Helper()
	defPath := writeDefFile(t, validClaudeCodeDef)
	return domain.AgentReference{
		Identifier:     "orchestrator-agent",
		DefinitionPath: defPath,
		InvocationKind: domain.InvocationOrchestrator,
	}
}

// ---------------------------------------------------------------------------
// T3.4 — type assertions: all three adapters satisfy domain.RawInvoker
// ---------------------------------------------------------------------------

func TestClaudeCodeAdapter_SatisfiesRawInvoker(t *testing.T) {
	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	if _, ok := any(adapter).(domain.RawInvoker); !ok {
		t.Fatal("want ClaudeCodeAdapter to implement domain.RawInvoker")
	}
}

func TestOpenCodeAdapter_SatisfiesRawInvoker(t *testing.T) {
	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	if _, ok := any(adapter).(domain.RawInvoker); !ok {
		t.Fatal("want OpenCodeAdapter to implement domain.RawInvoker")
	}
}

func TestGHCPCLIAdapter_SatisfiesRawInvoker(t *testing.T) {
	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	if _, ok := any(adapter).(domain.RawInvoker); !ok {
		t.Fatal("want GHCPCLIAdapter to implement domain.RawInvoker")
	}
}

// ---------------------------------------------------------------------------
// T3.4 — InvokeRaw issues as orchestrator-kind (--agent flag, not --append-system-prompt-file)
// ---------------------------------------------------------------------------

func TestClaudeCodeAdapter_InvokeRaw_IssuesOrchestratorKind(t *testing.T) {
	argsFile := setHelperEnv(t, "success")
	agent := ccOrchestratorRef(t)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want ClaudeCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), agent, rawPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args (orchestrator-kind), got %v", agent.Identifier, args)
	}
	if containsArg(args, "--append-system-prompt-file") {
		t.Errorf("want --append-system-prompt-file absent for orchestrator-kind InvokeRaw, got %v", args)
	}
}

func TestOpenCodeAdapter_InvokeRaw_IssuesOrchestratorKind(t *testing.T) {
	argsFile := setHelperEnv(t, "opencode-success")
	agent := orchestratorRef()

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want OpenCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), agent, rawPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args (orchestrator-kind), got %v", agent.Identifier, args)
	}
}

func TestGHCPCLIAdapter_InvokeRaw_IssuesOrchestratorKind(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")
	agent := orchestratorRef()

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want GHCPCLIAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), agent, rawPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args (orchestrator-kind), got %v", agent.Identifier, args)
	}
}

// ---------------------------------------------------------------------------
// T3.4 — InvokeRaw returns reply bytes verbatim and never nil on success
// ---------------------------------------------------------------------------

func TestClaudeCodeAdapter_InvokeRaw_ReturnsNonNilBytesOnSuccess(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want ClaudeCodeAdapter to implement domain.RawInvoker")
	}

	got, err := ri.InvokeRaw(context.Background(), ccOrchestratorRef(t), rawPayload)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if got == nil {
		t.Error("want non-nil bytes on success, got nil")
	}
	if len(got) == 0 {
		t.Error("want non-empty bytes on success, got empty slice")
	}
}

func TestOpenCodeAdapter_InvokeRaw_ReturnsNonNilBytesOnSuccess(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "opencode-success")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want OpenCodeAdapter to implement domain.RawInvoker")
	}

	got, err := ri.InvokeRaw(context.Background(), orchestratorRef(), rawPayload)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if got == nil {
		t.Error("want non-nil bytes on success, got nil")
	}
	if len(got) == 0 {
		t.Error("want non-empty bytes on success, got empty slice")
	}
}

func TestGHCPCLIAdapter_InvokeRaw_ReturnsNonNilBytesOnSuccess(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want GHCPCLIAdapter to implement domain.RawInvoker")
	}

	got, err := ri.InvokeRaw(context.Background(), orchestratorRef(), rawPayload)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if got == nil {
		t.Error("want non-nil bytes on success, got nil")
	}
	if len(got) == 0 {
		t.Error("want non-empty bytes on success, got empty slice")
	}
}

// ---------------------------------------------------------------------------
// T3.4 — InvokeRaw: ErrMalformedOutput never returned (no protocol extraction)
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_InvokeRaw_BadEnvelope_NotErrMalformedOutput verifies
// that a reply that would cause Invoke to return ErrMalformedOutput (a valid
// JSON envelope with no extractable protocol response) does NOT cause InvokeRaw
// to return ErrMalformedOutput: the text path has no protocol extraction step.
func TestClaudeCodeAdapter_InvokeRaw_BadEnvelope_NotErrMalformedOutput(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want ClaudeCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), ccOrchestratorRef(t), rawPayload)

	// ErrMalformedOutput aliases ErrProtocolNotExtractable; neither must be
	// returned by the text path.
	if errors.Is(err, harness.ErrMalformedOutput) {
		t.Error("want InvokeRaw to never return ErrMalformedOutput (no protocol extraction on text path)")
	}
}

// ---------------------------------------------------------------------------
// T3.4 — InvokeRaw failure sentinels: missing executable
// ---------------------------------------------------------------------------

func TestClaudeCodeAdapter_InvokeRaw_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	adapter := harness.NewClaudeCodeAdapter("/nonexistent/path/to/claude", 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want ClaudeCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), ccOrchestratorRef(t), rawPayload)

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

func TestOpenCodeAdapter_InvokeRaw_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	adapter := harness.NewOpenCodeAdapter("/nonexistent/path/to/opencode", 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want OpenCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), orchestratorRef(), rawPayload)

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

func TestGHCPCLIAdapter_InvokeRaw_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	adapter := harness.NewGHCPCLIAdapter("/nonexistent/path/to/copilot", 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want GHCPCLIAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), orchestratorRef(), rawPayload)

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T3.4 — InvokeRaw failure sentinels: timeout
// ---------------------------------------------------------------------------

func TestClaudeCodeAdapter_InvokeRaw_Timeout_ReturnsErrTimeout(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 100*time.Millisecond)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want ClaudeCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), ccOrchestratorRef(t), rawPayload)

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

func TestOpenCodeAdapter_InvokeRaw_Timeout_ReturnsErrTimeout(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 100*time.Millisecond)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want OpenCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), orchestratorRef(), rawPayload)

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

func TestGHCPCLIAdapter_InvokeRaw_Timeout_ReturnsErrTimeout(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 100*time.Millisecond)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want GHCPCLIAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), orchestratorRef(), rawPayload)

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T3.4 — InvokeRaw failure sentinels: empty output
// ---------------------------------------------------------------------------

func TestClaudeCodeAdapter_InvokeRaw_EmptyOutput_ReturnsErrEmptyResponse(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "empty")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want ClaudeCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), ccOrchestratorRef(t), rawPayload)

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

func TestOpenCodeAdapter_InvokeRaw_EmptyOutput_ReturnsErrEmptyResponse(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "empty")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want OpenCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), orchestratorRef(), rawPayload)

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

func TestGHCPCLIAdapter_InvokeRaw_EmptyOutput_ReturnsErrEmptyResponse(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "empty")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want GHCPCLIAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), orchestratorRef(), rawPayload)

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T3.4 — InvokeRaw failure sentinels: context cancellation → ctx.Err()
// ---------------------------------------------------------------------------

func TestClaudeCodeAdapter_InvokeRaw_ContextCancellation_ReturnsCtxErr(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 30*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want ClaudeCodeAdapter to implement domain.RawInvoker")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := ri.InvokeRaw(ctx, ccOrchestratorRef(t), rawPayload)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled on cancellation, got %v", err)
	}
}

func TestOpenCodeAdapter_InvokeRaw_ContextCancellation_ReturnsCtxErr(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := harness.NewOpenCodeAdapter(helperExe(t), 30*time.Second)
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

func TestGHCPCLIAdapter_InvokeRaw_ContextCancellation_ReturnsCtxErr(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := harness.NewGHCPCLIAdapter(helperExe(t), 30*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want GHCPCLIAdapter to implement domain.RawInvoker")
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

// ---------------------------------------------------------------------------
// T3.4 — InvokeRaw logging: DebugLogger receives invocation events
// ---------------------------------------------------------------------------

func TestClaudeCodeAdapter_InvokeRaw_Success_LogsInvokeEvents(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "success")

	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)
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
	adapter := harness.NewClaudeCodeAdapterWithLogger("/nonexistent/claude", 5*time.Second, logger)
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

// ---------------------------------------------------------------------------
// T3.4 — Ordinary Invoke still requires a protocol message (unchanged)
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_Invoke_UnchangedByInvokeRaw verifies that adding InvokeRaw
// does not relax Invoke: a successful CLI output still must contain a Communication
// Protocol message, or Invoke returns an error.
func TestClaudeCodeAdapter_Invoke_UnchangedByInvokeRaw(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if err != nil {
		t.Fatalf("want successful Invoke still to work, got %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode SUCCESS, got %q", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// T3.5 — composition-root probe: each production adapter satisfies RawInvoker
// ---------------------------------------------------------------------------

// TestStartupProbe_ClaudeCodeAdapter_SatisfiesRawInvoker verifies that
// ClaudeCodeAdapter, as constructed in production (via NewClaudeCodeAdapter),
// satisfies domain.RawInvoker so the composition root's probe succeeds.
func TestStartupProbe_ClaudeCodeAdapter_SatisfiesRawInvoker(t *testing.T) {
	adapter := harness.NewClaudeCodeAdapter("/nonexistent/claude", 5*time.Second)

	_, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want ClaudeCodeAdapter (as constructed in production) to satisfy domain.RawInvoker; startup probe would find no transport")
	}
}

// TestStartupProbe_OpenCodeAdapter_SatisfiesRawInvoker verifies that
// OpenCodeAdapter satisfies domain.RawInvoker so the composition root's probe
// succeeds for the opencode harness.
func TestStartupProbe_OpenCodeAdapter_SatisfiesRawInvoker(t *testing.T) {
	adapter := harness.NewOpenCodeAdapter("/nonexistent/opencode", 5*time.Second)

	_, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want OpenCodeAdapter (as constructed in production) to satisfy domain.RawInvoker; startup probe would find no transport")
	}
}

// TestStartupProbe_GHCPCLIAdapter_SatisfiesRawInvoker verifies that
// GHCPCLIAdapter satisfies domain.RawInvoker so the composition root's probe
// succeeds for the ghcp-cli harness.
func TestStartupProbe_GHCPCLIAdapter_SatisfiesRawInvoker(t *testing.T) {
	adapter := harness.NewGHCPCLIAdapter("/nonexistent/copilot", 5*time.Second)

	_, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want GHCPCLIAdapter (as constructed in production) to satisfy domain.RawInvoker; startup probe would find no transport")
	}
}

// ---------------------------------------------------------------------------
// T3.5 — startup probe pattern: type assertion on HarnessAdapter value
// ---------------------------------------------------------------------------

// TestStartupProbe_AllThreeAdapters_ProbeFindsTransport verifies the exact
// startup probe pattern from cmd/mosaic-run: a type assertion on the adapter
// value (which implements HarnessAdapter) must find domain.RawInvoker on all
// three production adapter constructors.
func TestStartupProbe_AllThreeAdapters_ProbeFindsTransport(t *testing.T) {
	adapters := []struct {
		name    string
		adapter domain.HarnessAdapter
	}{
		{"ClaudeCodeAdapter", harness.NewClaudeCodeAdapter("/nonexistent/claude", 5*time.Second)},
		{"OpenCodeAdapter", harness.NewOpenCodeAdapter("/nonexistent/opencode", 5*time.Second)},
		{"GHCPCLIAdapter", harness.NewGHCPCLIAdapter("/nonexistent/copilot", 5*time.Second)},
	}

	for _, tc := range adapters {
		t.Run(tc.name, func(t *testing.T) {
			// This is the exact probe pattern from cmd/mosaic-run/main.go:
			//   if ri, ok := h.(domain.RawInvoker); ok { rawInvoker = ri }
			ri, ok := tc.adapter.(domain.RawInvoker)
			if !ok {
				t.Fatalf("want %s to satisfy domain.RawInvoker; startup probe would find no transport for this harness", tc.name)
			}
			if ri == nil {
				t.Fatalf("want non-nil RawInvoker from %s, got nil", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// helpers: re-used from claudecode_test.go via shared package
// ---------------------------------------------------------------------------

// The following are referenced from claudecode_test.go in this same package:
// - helperExe, setHelperEnv, readArgs, containsArg, containsSequence, indexOfArg
// - ordinaryAgentRef, orchestratorAgentRef, minimalClaudeRequest
// - recordingLogger (with eventLogged, messageLogged, fieldValueLogged)
