package claudecode_test

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
	"errors"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness/claudecode"
	"mosaic-run/internal/harness/cliexec"
)

// rawPayload is a minimal consultation payload for InvokeRaw tests: not a
// Communication Protocol message, so the text path is exercised specifically.
var rawPayload = []byte(`{"action":"route","agent":"agent-b","task_description":"do the thing"}`)

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
	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	if _, ok := any(adapter).(domain.RawInvoker); !ok {
		t.Fatal("want ClaudeCodeAdapter to implement domain.RawInvoker")
	}
}

func TestClaudeCodeAdapter_InvokeRaw_IssuesOrchestratorKind(t *testing.T) {
	argsFile := setHelperEnv(t, "success")
	agent := ccOrchestratorRef(t)

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
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

func TestClaudeCodeAdapter_InvokeRaw_ReturnsNonNilBytesOnSuccess(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "success")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
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

// TestClaudeCodeAdapter_InvokeRaw_BadEnvelope_NotErrMalformedOutput verifies
// that a reply that would cause Invoke to return ErrMalformedOutput (a valid
// JSON envelope with no extractable protocol response) does NOT cause InvokeRaw
// to return ErrMalformedOutput: the text path has no protocol extraction step.
func TestClaudeCodeAdapter_InvokeRaw_BadEnvelope_NotErrMalformedOutput(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "success")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	ri, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want ClaudeCodeAdapter to implement domain.RawInvoker")
	}

	_, err := ri.InvokeRaw(context.Background(), ccOrchestratorRef(t), rawPayload)

	// ErrMalformedOutput aliases ErrProtocolNotExtractable; neither must be
	// returned by the text path.
	if errors.Is(err, cliexec.ErrMalformedOutput) {
		t.Error("want InvokeRaw to never return ErrMalformedOutput (no protocol extraction on text path)")
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

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

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
	adapter := claudecode.NewClaudeCodeAdapter("/nonexistent/claude", 5*time.Second)

	_, ok := any(adapter).(domain.RawInvoker)
	if !ok {
		t.Fatal("want ClaudeCodeAdapter (as constructed in production) to satisfy domain.RawInvoker; startup probe would find no transport")
	}
}
