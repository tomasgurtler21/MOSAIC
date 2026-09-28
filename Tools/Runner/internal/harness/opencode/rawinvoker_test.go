package opencode_test

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
// already in helperprocess_test.go (TestMain, runHelperProcess, helperExe,
// setHelperEnv, readArgs, ordinaryAgentRef, orchestratorAgentRef).

import (
	"context"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness/opencode"
)

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

func TestOpenCodeAdapter_SatisfiesRawInvoker(t *testing.T) {
	adapter := opencode.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	if _, ok := any(adapter).(domain.RawInvoker); !ok {
		t.Fatal("want OpenCodeAdapter to implement domain.RawInvoker")
	}
}

func TestOpenCodeAdapter_InvokeRaw_IssuesOrchestratorKind(t *testing.T) {
	argsFile := setHelperEnv(t, "opencode-success")
	agent := orchestratorRef()

	adapter := opencode.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
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

func TestOpenCodeAdapter_InvokeRaw_ReturnsNonNilBytesOnSuccess(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "opencode-success")

	adapter := opencode.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
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

// TestStartupProbe_OpenCodeAdapter_SatisfiesRawInvoker verifies that
// OpenCodeAdapter satisfies domain.RawInvoker so the composition root's probe
// succeeds for the opencode harness.
func TestStartupProbe_OpenCodeAdapter_SatisfiesRawInvoker(t *testing.T) {
	adapter := opencode.NewOpenCodeAdapter("/nonexistent/opencode", 5*time.Second)

	_, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want OpenCodeAdapter (as constructed in production) to satisfy domain.RawInvoker; startup probe would find no transport")
	}
}
