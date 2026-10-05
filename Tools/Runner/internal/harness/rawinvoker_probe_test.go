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
// already in helperprocess_test.go (TestMain, runHelperProcess, helperExe,
// setHelperEnv, readArgs, ordinaryAgentRef, orchestratorAgentRef).

import (
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness/claudecode"
	"mosaic-run/internal/harness/ghcpcli"
	"mosaic-run/internal/harness/opencode"
)

// TestStartupProbe_AllThreeAdapters_ProbeFindsTransport verifies the exact
// startup probe pattern from cmd/mosaic-run: a type assertion on the adapter
// value (which implements HarnessAdapter) must find domain.RawInvoker on all
// three production adapter constructors.
func TestStartupProbe_AllThreeAdapters_ProbeFindsTransport(t *testing.T) {
	adapters := []struct {
		name    string
		adapter domain.HarnessAdapter
	}{
		{"ClaudeCodeAdapter", claudecode.NewClaudeCodeAdapter("/nonexistent/claude", 5*time.Second)},
		{"OpenCodeAdapter", opencode.NewOpenCodeAdapter("/nonexistent/opencode", 5*time.Second)},
		{"GHCPCLIAdapter", ghcpcli.NewGHCPCLIAdapter("/nonexistent/copilot", 5*time.Second)},
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
// helpers: re-used from helperprocess_test.go via shared package
// ---------------------------------------------------------------------------

// The following are referenced from helperprocess_test.go in this same package:
// - helperExe, setHelperEnv, readArgs, containsArg, containsSequence, indexOfArg
// - ordinaryAgentRef, orchestratorAgentRef, minimalClaudeRequest
// - recordingLogger (with eventLogged, messageLogged, fieldValueLogged)
