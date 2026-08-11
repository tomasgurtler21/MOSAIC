package agentdeploy_test

// Tests for the delegating agent deployer: the outcome mapping across the
// deployment tool's exit codes (T4.1), the successful-render contract that
// destination path and source version come from the tool's JSON output rather
// than being reconstructed (T4.2), the failure and degradation modes (T4.3),
// and the argument pass-through including the workflow set (T4.5).
//
// No real deployment tool binary is present in these tests. Every interaction
// with the delegate goes through the Invoke seam, exactly as internal/cost
// exercises its CommandRunner seam. Because the seam also returns stderr, the
// "tool's message reaches the caller verbatim" property is assertable with no
// process involved.
//
// These tests cover the RED phase of the TDD cycle: they compile, they run,
// and they fail because the implementation is a stub that does not yet parse
// JSON output or map exit codes.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/agentdeploy"
	"mosaic-agent-test/internal/domain"
)

// The deployment tool's stable exit codes, mirrored here as plain ints because
// this test file must not import mosaic-deploy to learn its own outcome
// contract — only the stable wire shape and exit-code meanings are shared,
// per the no-import convention documented on agentdeploy.go's constants.
const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 3
)

// successJSON is a minimal valid deployment tool JSON result for exit 0,
// mirroring app.RenderAgentResult's JSON tags.
const successJSON = `{
	"destinationPath": "/sandbox/.claude/agents/orchestrator.md",
	"sourceVersion": "2.1",
	"agentKey": "orchestrator",
	"createdDirectories": ["/sandbox/.claude/agents"]
}`

// failureEnvelopeJSON is a minimal valid RenderFailure envelope for a non-zero
// exit, mirroring cli.RenderFailure's JSON tags. Reason is the stable code
// preflight branches on.
func failureEnvelopeJSON(reason, detail, message string, exitCode int) []byte {
	return []byte(`{"error":true,"reason":"` + reason + `","message":"` + message +
		`","detail":"` + detail + `","exitCode":` + fmt.Sprintf("%d", exitCode) + `}`)
}

// minimalRequest builds the smallest RenderAgentRequest that exercises the
// seam. Fields that affect argument construction are set explicitly in each
// test that cares about them.
func minimalRequest() domain.RenderAgentRequest {
	return domain.RenderAgentRequest{
		CatalogAgentKey: "orchestrator",
		HarnessID:       "claude-code",
		WorkspaceRoot:   "/sandbox",
	}
}

// newDeployer constructs an agentdeploy.New with the given Options. Every
// test supplies its own Invoke seam; ExecutablePath is set to a stable
// sentinel so tests can assert it is passed through to the seam.
func newDeployer(opts agentdeploy.Options) domain.AgentDeployer {
	if opts.ExecutablePath == "" {
		opts.ExecutablePath = "mosaic-deploy"
	}
	return agentdeploy.New(opts)
}

// =============================================================================
// T4.1 — Outcome mapping across the deployment tool's exit codes
// =============================================================================

// TestRender_ExitSuccess_WithValidJSON_ReturnsResultAndNilError asserts that
// exit 0 with a valid JSON result document is the success path: the deployer
// returns a non-zero RenderAgentResult and a nil error.
func TestRender_ExitSuccess_WithValidJSON_ReturnsResultAndNilError(t *testing.T) {
	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	result, err := deployer.Render(context.Background(), minimalRequest())

	if err != nil {
		t.Errorf("Render returned an error on exit 0 with valid JSON: %v", err)
	}
	if result.DestinationPath == "" {
		t.Error("Render returned an empty DestinationPath on success, want the path from the JSON output")
	}
	if result.AgentKey != "orchestrator" {
		t.Errorf("AgentKey = %q, want %q (the agentKey from the JSON output)", result.AgentKey, "orchestrator")
	}
}

// TestRender_ExitFailure_WithFailureEnvelope_ReturnsErrRenderFailed asserts
// that exit 1 (service failure) with a parseable failure envelope produces an
// error wrapping ErrRenderFailed, carrying the envelope's reason code.
func TestRender_ExitFailure_WithFailureEnvelope_ReturnsErrRenderFailed(t *testing.T) {
	envelope := failureEnvelopeJSON("agent-not-in-catalog", "nonexistent", "agent 'nonexistent' is not in the catalog", exitFailure)
	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return envelope, []byte("agent 'nonexistent' is not in the catalog"), exitFailure, nil
		},
	})

	_, err := deployer.Render(context.Background(), minimalRequest())

	if err == nil {
		t.Fatal("Render returned nil error on exit 1, want an error wrapping ErrRenderFailed")
	}
	if !errors.Is(err, agentdeploy.ErrRenderFailed) {
		t.Errorf("Render error = %v, want it to wrap ErrRenderFailed", err)
	}
	var re *agentdeploy.RenderError
	if !errors.As(err, &re) {
		t.Errorf("Render error = %v, want it to be a *RenderError carrying the reason", err)
	} else if re.Reason != "agent-not-in-catalog" {
		t.Errorf("RenderError.Reason = %q, want %q", re.Reason, "agent-not-in-catalog")
	}
}

// TestRender_ExitUsage_WithFailureEnvelope_ReturnsErrRenderFailed asserts that
// exit 3 (usage / flag error) is also mapped to ErrRenderFailed with the
// envelope's reason code, not to a distinct error class: from the caller's
// perspective, a usage error is still a render failure.
func TestRender_ExitUsage_WithFailureEnvelope_ReturnsErrRenderFailed(t *testing.T) {
	envelope := failureEnvelopeJSON("harness-required", "", "flag --harness is required", exitUsage)
	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return envelope, []byte("flag --harness is required"), exitUsage, nil
		},
	})

	_, err := deployer.Render(context.Background(), minimalRequest())

	if err == nil {
		t.Fatal("Render returned nil error on exit 3, want an error wrapping ErrRenderFailed")
	}
	if !errors.Is(err, agentdeploy.ErrRenderFailed) {
		t.Errorf("Render error = %v, want it to wrap ErrRenderFailed", err)
	}
	// The harness-required failure names no single input, so Detail must be empty.
	var re *agentdeploy.RenderError
	if errors.As(err, &re) && re.Detail != "" {
		t.Errorf("RenderError.Detail = %q for harness-required failure, want empty string (failure names no single input)", re.Detail)
	}
}

// TestRender_NonZeroExit_NoDecodableEnvelope_ErrRenderFailedWithEmptyReason
// asserts graceful degradation: when the delegate exits non-zero but its
// stdout cannot be decoded as a failure envelope (an older binary, a crash
// before the writer ran, empty stdout), ErrRenderFailed is still returned —
// with an empty Reason, not a parse error in its place. Callers treat an empty
// Reason as "undifferentiated failure".
func TestRender_NonZeroExit_NoDecodableEnvelope_ErrRenderFailedWithEmptyReason(t *testing.T) {
	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			// Not JSON — simulates an older binary or a crash dump.
			return []byte("render: panic: runtime error"), []byte("render: panic"), exitFailure, nil
		},
	})

	_, err := deployer.Render(context.Background(), minimalRequest())

	if err == nil {
		t.Fatal("Render returned nil error on non-zero exit with undecodable stdout, want ErrRenderFailed")
	}
	if !errors.Is(err, agentdeploy.ErrRenderFailed) {
		t.Errorf("Render error = %v, want it to wrap ErrRenderFailed", err)
	}
	var re *agentdeploy.RenderError
	if errors.As(err, &re) && re.Reason != "" {
		t.Errorf("RenderError.Reason = %q for undecodable envelope, want empty (undifferentiated failure)", re.Reason)
	}
}

// =============================================================================
// T4.2 — Successful render: destination path and source version from JSON output
// =============================================================================

// TestRender_SuccessDestinationPath_IsReportedFromJSONOutput_NotReconstructed
// asserts the core contract: the DestinationPath in the returned result comes
// from the tool's JSON output, not from anything this package computes. A fake
// Invoke that returns a path a reconstruction would never produce proves this
// property: if the result carries that path, it was read; if it carries a
// reconstructed one (or is empty), the test fails.
func TestRender_SuccessDestinationPath_IsReportedFromJSONOutput_NotReconstructed(t *testing.T) {
	// A path no reconstruction would ever produce — it is deliberately
	// unpredictable from the request fields alone.
	const distinctivePath = "/sandbox/.claude/agents/orchestrator--rendered-by-tool.md"
	resultJSON := `{"destinationPath":"` + distinctivePath + `","sourceVersion":"1.0","agentKey":"orchestrator"}`

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte(resultJSON), nil, exitSuccess, nil
		},
	})

	result, err := deployer.Render(context.Background(), minimalRequest())

	if err != nil {
		t.Fatalf("Render returned an error on exit 0: %v", err)
	}
	if result.DestinationPath != distinctivePath {
		t.Errorf("DestinationPath = %q, want %q (the path from the tool's JSON output, not a reconstruction)", result.DestinationPath, distinctivePath)
	}
}

// TestRender_SuccessSourceVersion_IsReportedFromJSONOutput asserts that the
// source agent's version is read from the tool's JSON output, not derived from
// the request or left empty. The version is what was actually deployed; any
// derivation here could disagree with the tool's own frontmatter read.
func TestRender_SuccessSourceVersion_IsReportedFromJSONOutput(t *testing.T) {
	const version = "3.14"
	resultJSON := `{"destinationPath":"/sandbox/.claude/agents/orchestrator.md","sourceVersion":"` + version + `","agentKey":"orchestrator"}`

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte(resultJSON), nil, exitSuccess, nil
		},
	})

	result, err := deployer.Render(context.Background(), minimalRequest())

	if err != nil {
		t.Fatalf("Render returned an error on exit 0: %v", err)
	}
	if result.SourceVersion != version {
		t.Errorf("SourceVersion = %q, want %q (the version from the tool's JSON output)", result.SourceVersion, version)
	}
}

// TestRender_SuccessAbsentSourceVersion_IsReportedAsEmptyNotMissing asserts
// that a source agent with no declared version yields an empty SourceVersion,
// never a stand-in value. The caller decides how to render an absent version
// (e.g. as "unknown" in reports); the port just reports what the tool said.
func TestRender_SuccessAbsentSourceVersion_IsReportedAsEmptyNotMissing(t *testing.T) {
	resultJSON := `{"destinationPath":"/sandbox/.claude/agents/orchestrator.md","agentKey":"orchestrator"}`

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte(resultJSON), nil, exitSuccess, nil
		},
	})

	result, err := deployer.Render(context.Background(), minimalRequest())

	if err != nil {
		t.Fatalf("Render returned an error on exit 0: %v", err)
	}
	if result.SourceVersion != "" {
		t.Errorf("SourceVersion = %q, want empty string for a source that declared no version", result.SourceVersion)
	}
}

// TestRender_SuccessCreatedDirectories_AreReportedFromJSONOutput asserts that
// CreatedDirectories in the result comes from the tool's JSON output, so the
// teardown ledger can record exactly what appeared — including directories
// created by the external tool that the adapter never touched.
func TestRender_SuccessCreatedDirectories_AreReportedFromJSONOutput(t *testing.T) {
	resultJSON := `{
		"destinationPath":"/sandbox/.claude/agents/orchestrator.md",
		"sourceVersion":"1.0",
		"agentKey":"orchestrator",
		"createdDirectories":["/sandbox/.claude","/sandbox/.claude/agents"]
	}`

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte(resultJSON), nil, exitSuccess, nil
		},
	})

	result, err := deployer.Render(context.Background(), minimalRequest())

	if err != nil {
		t.Fatalf("Render returned an error on exit 0: %v", err)
	}
	if len(result.CreatedDirectories) != 2 {
		t.Errorf("CreatedDirectories = %v (len %d), want 2 directories from the JSON output",
			result.CreatedDirectories, len(result.CreatedDirectories))
	} else {
		if result.CreatedDirectories[0] != "/sandbox/.claude" {
			t.Errorf("CreatedDirectories[0] = %q, want %q", result.CreatedDirectories[0], "/sandbox/.claude")
		}
		if result.CreatedDirectories[1] != "/sandbox/.claude/agents" {
			t.Errorf("CreatedDirectories[1] = %q, want %q", result.CreatedDirectories[1], "/sandbox/.claude/agents")
		}
	}
}

// =============================================================================
// T4.3 — Failure and degradation: no hang, no panic, always diagnosable
// =============================================================================

// TestRender_ToolUnavailable_ReturnsErrToolUnavailable asserts that an
// invocation failure (the binary is absent, not executable, or killed before
// the exit code is known) maps to ErrToolUnavailable, not to ErrRenderFailed
// or to any error that could be mistaken for a declaration problem.
func TestRender_ToolUnavailable_ReturnsErrToolUnavailable(t *testing.T) {
	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, nil, 0, errors.New("exec: executable file not found in $PATH")
		},
	})

	_, err := deployer.Render(context.Background(), minimalRequest())

	if err == nil {
		t.Fatal("Render returned nil error when the tool cannot be invoked, want ErrToolUnavailable")
	}
	if !errors.Is(err, agentdeploy.ErrToolUnavailable) {
		t.Errorf("Render error = %v, want it to wrap ErrToolUnavailable (not ErrRenderFailed or a raw error)", err)
	}
}

// TestRender_ToolUnavailable_ErrorNamesExecutablePath asserts the diagnosable
// requirement: the error must name the executable path that was searched, so
// the operator knows what to install or override.
func TestRender_ToolUnavailable_ErrorNamesExecutablePath(t *testing.T) {
	const binaryPath = "/usr/local/bin/mosaic-deploy"
	deployer := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: binaryPath,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, nil, 0, errors.New("exec: no such file or directory")
		},
	})

	_, err := deployer.Render(context.Background(), minimalRequest())

	if err == nil {
		t.Fatal("Render returned nil error, want ErrToolUnavailable")
	}
	if !strings.Contains(err.Error(), binaryPath) {
		t.Errorf("error = %q, want it to name the executable path %q so the operator knows what was searched", err.Error(), binaryPath)
	}
}

// TestRender_ExitNonZeroWithReason_ErrRenderFailedCarriesReasonAndDetail
// asserts the differentiation contract: when the deploy tool exits non-zero
// with a parseable envelope, the returned error carries both the reason code
// and the detail field verbatim. Preflight branches on Reason to produce
// distinct diagnostics ("unknown-subject-agent" vs "unknown-workflow"); without
// this the two are indistinguishable.
func TestRender_ExitNonZeroWithReason_ErrRenderFailedCarriesReasonAndDetail(t *testing.T) {
	envelope := failureEnvelopeJSON("unknown-workflow", "wf-custom", "workflow id 'wf-custom' is not in the catalog", exitFailure)
	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return envelope, nil, exitFailure, nil
		},
	})

	_, err := deployer.Render(context.Background(), minimalRequest())

	if err == nil {
		t.Fatal("Render returned nil error on exit 1, want ErrRenderFailed")
	}
	var re *agentdeploy.RenderError
	if !errors.As(err, &re) {
		t.Fatalf("Render error = %v, want a *RenderError carrying reason and detail", err)
	}
	if re.Reason != "unknown-workflow" {
		t.Errorf("RenderError.Reason = %q, want %q", re.Reason, "unknown-workflow")
	}
	if re.Detail != "wf-custom" {
		t.Errorf("RenderError.Detail = %q, want %q (the offending workflow id)", re.Detail, "wf-custom")
	}
}

// TestRender_NonZeroExitWithEnvelope_ToolMessageCarriedVerbatim asserts that
// the envelope's human-readable message field is preserved verbatim in
// RenderError.ToolMessage, so a preflight diagnostic can quote the deployment
// tool rather than paraphrase it. This is what the widened seam (returning
// stderr as well as stdout) was designed to enable.
func TestRender_NonZeroExitWithEnvelope_ToolMessageCarriedVerbatim(t *testing.T) {
	const toolMsg = "agent 'nonexistent-agent' is not registered in the catalog"
	envelope := failureEnvelopeJSON("agent-not-in-catalog", "nonexistent-agent", toolMsg, exitFailure)
	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return envelope, []byte(toolMsg), exitFailure, nil
		},
	})

	_, err := deployer.Render(context.Background(), minimalRequest())

	if err == nil {
		t.Fatal("Render returned nil error on exit 1")
	}
	var re *agentdeploy.RenderError
	if !errors.As(err, &re) {
		t.Fatalf("Render error = %v, want a *RenderError", err)
	}
	if re.ToolMessage != toolMsg {
		t.Errorf("RenderError.ToolMessage = %q, want the tool's own message %q quoted verbatim", re.ToolMessage, toolMsg)
	}
}

// TestRender_NonZeroExitNoEnvelope_ToolMessageComesFromStderr asserts that
// when the delegate exits non-zero but stdout cannot be decoded as an envelope
// (no structured output), ToolMessage is taken from stderr — the fallback that
// ensures a human-readable diagnostic is always available.
func TestRender_NonZeroExitNoEnvelope_ToolMessageComesFromStderr(t *testing.T) {
	const stderrText = "fatal: unexpected panic in render handler"
	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte("not valid json"), []byte(stderrText), exitFailure, nil
		},
	})

	_, err := deployer.Render(context.Background(), minimalRequest())

	if err == nil {
		t.Fatal("Render returned nil error on exit 1 with undecodable stdout")
	}
	var re *agentdeploy.RenderError
	if !errors.As(err, &re) {
		t.Fatalf("Render error = %v, want a *RenderError", err)
	}
	if !strings.Contains(re.ToolMessage, stderrText) {
		t.Errorf("RenderError.ToolMessage = %q, want it to contain the stderr text %q (the fallback when no envelope was decoded)", re.ToolMessage, stderrText)
	}
}

// TestRender_ZeroExitUnparseableOutput_ReturnsErrUnparseableOutput asserts
// that a zero exit paired with output that cannot be decoded is
// ErrUnparseableOutput — distinct from ErrRenderFailed so a caller can tell
// apart "the tool succeeded but produced garbage" from "the tool rejected the
// request". The former is an infrastructure problem; the latter is a
// declaration problem.
func TestRender_ZeroExitUnparseableOutput_ReturnsErrUnparseableOutput(t *testing.T) {
	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte("not valid json output"), nil, exitSuccess, nil
		},
	})

	_, err := deployer.Render(context.Background(), minimalRequest())

	if err == nil {
		t.Fatal("Render returned nil error on exit 0 with unparseable output, want ErrUnparseableOutput")
	}
	if !errors.Is(err, agentdeploy.ErrUnparseableOutput) {
		t.Errorf("Render error = %v, want it to wrap ErrUnparseableOutput (not ErrRenderFailed, which would imply a declaration problem)", err)
	}
}

// TestRender_ZeroExitUnparseableOutput_ErrorNamesOutputSize asserts the
// diagnosable requirement for the unparseable-output case: the error must name
// how many bytes were produced, so a developer can distinguish "exited zero and
// said nothing" from "exited zero and said something garbled".
func TestRender_ZeroExitUnparseableOutput_ErrorNamesOutputSize(t *testing.T) {
	const output = "partial output that cannot be decoded"
	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte(output), nil, exitSuccess, nil
		},
	})

	_, err := deployer.Render(context.Background(), minimalRequest())

	if err == nil {
		t.Fatal("Render returned nil error on exit 0 with unparseable output")
	}
	// The error must name the byte count so "zero bytes" is distinguishable
	// from "non-zero bytes that are garbled".
	if !strings.Contains(err.Error(), "37") && !strings.Contains(err.Error(), "bytes") {
		t.Errorf("error = %q, want it to name the output size (%d bytes) for diagnosability", err.Error(), len(output))
	}
}

// TestRender_TimeoutExceeded_ReturnsErrTimedOut asserts that when the deploy
// tool exceeds the configured timeout, the call returns ErrTimedOut and does
// not hang: the timeout applied by the port bounds the delegate's runtime, so
// a stalled binary degrades a run rather than stalling it.
func TestRender_TimeoutExceeded_ReturnsErrTimedOut(t *testing.T) {
	deployer := newDeployer(agentdeploy.Options{
		Timeout: 20 * time.Millisecond,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			// Block until the context the port applied expires.
			<-ctx.Done()
			return nil, nil, 0, ctx.Err()
		},
	})

	// The outer context is generous; the port's own timeout fires first.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := deployer.Render(ctx, minimalRequest())

	if err == nil {
		t.Fatal("Render returned nil error when the tool timed out, want ErrTimedOut")
	}
	if !errors.Is(err, agentdeploy.ErrTimedOut) {
		t.Errorf("Render error = %v, want it to wrap ErrTimedOut (the tool was bounded, not the caller's context cancelled)", err)
	}
}

// TestRender_CallerContextCancelled_ReturnsError asserts that if the caller's
// own context is cancelled before the port's timeout fires, the call returns
// promptly with an error rather than hanging until the internal timeout.
func TestRender_CallerContextCancelled_ReturnsError(t *testing.T) {
	deployer := newDeployer(agentdeploy.Options{
		Timeout: 10 * time.Second, // much longer than the test will run
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			<-ctx.Done()
			return nil, nil, 0, ctx.Err()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled immediately

	_, err := deployer.Render(ctx, minimalRequest())

	if err == nil {
		t.Fatal("Render returned nil error when the caller's context was cancelled, want an error")
	}
}

// =============================================================================
// T4.5 — Argument pass-through: workflow set and other request fields
// =============================================================================

// TestRender_ExecutablePath_PassedToCommandRunner asserts that the configured
// ExecutablePath is what the seam receives — not a hardcoded constant or a
// discovery path. The seam is what makes the outcome mapping testable; a
// correct path is what makes a real invocation reach the right binary.
func TestRender_ExecutablePath_PassedToCommandRunner(t *testing.T) {
	const binaryPath = "/usr/local/bin/mosaic-deploy"
	var gotPath string

	deployer := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: binaryPath,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			gotPath = path
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), minimalRequest())

	if gotPath != binaryPath {
		t.Errorf("CommandRunner received path %q, want the configured ExecutablePath %q", gotPath, binaryPath)
	}
}

// TestRender_HarnessID_PassedAsHarnessFlag asserts that the HarnessID from
// the request reaches the delegate as the --harness flag value.
func TestRender_HarnessID_PassedAsHarnessFlag(t *testing.T) {
	var capturedArgs []string
	req := minimalRequest()
	req.HarnessID = "claude-code"

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), req)

	if !containsFlag(capturedArgs, "--harness", "claude-code") {
		t.Errorf("args = %v, want --harness claude-code to be present", capturedArgs)
	}
}

// TestRender_WorkspaceRoot_PassedAsWorkspaceFlag asserts that WorkspaceRoot
// from the request reaches the delegate as the --workspace flag value, so the
// harness descriptor (not this package) resolves the destination path beneath
// it.
func TestRender_WorkspaceRoot_PassedAsWorkspaceFlag(t *testing.T) {
	var capturedArgs []string
	req := minimalRequest()
	req.WorkspaceRoot = "/var/sandbox/run-42"

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), req)

	if !containsFlag(capturedArgs, "--workspace", "/var/sandbox/run-42") {
		t.Errorf("args = %v, want --workspace /var/sandbox/run-42 to be present", capturedArgs)
	}
}

// TestRender_CatalogAgentKey_PassedAsSourceAgentFlag asserts that
// CatalogAgentKey (the subject sourced from the real catalogue) reaches the
// delegate as the --source-agent flag, so the tool resolves the catalogue
// path — not this package.
func TestRender_CatalogAgentKey_PassedAsSourceAgentFlag(t *testing.T) {
	var capturedArgs []string
	req := minimalRequest()
	req.CatalogAgentKey = "orchestrator"

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), req)

	if !containsFlag(capturedArgs, "--source-agent", "orchestrator") {
		t.Errorf("args = %v, want --source-agent orchestrator when CatalogAgentKey is set", capturedArgs)
	}
}

// TestRender_SourcePath_PassedAsSourceFlag asserts that SourcePath (a stub
// definition from the AgentTest workspace, not the catalogue) reaches the
// delegate as the --source flag.
func TestRender_SourcePath_PassedAsSourceFlag(t *testing.T) {
	var capturedArgs []string
	req := domain.RenderAgentRequest{
		SourcePath:    "/abs/path/to/agents/codebase-research.md",
		HarnessID:     "claude-code",
		WorkspaceRoot: "/sandbox",
	}

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), req)

	if !containsFlag(capturedArgs, "--source", "/abs/path/to/agents/codebase-research.md") {
		t.Errorf("args = %v, want --source <path> when SourcePath is set", capturedArgs)
	}
}

// TestRender_WorkflowsNil_WorkflowsFlagAbsentFromArgs asserts the nil/empty
// distinction at the argument level: nil Workflows means "not specified", so
// the --workflows flag must be absent entirely. Passing it with an empty value
// would mean "explicitly none", which is a different rendering.
func TestRender_WorkflowsNil_WorkflowsFlagAbsentFromArgs(t *testing.T) {
	var capturedArgs []string
	req := minimalRequest()
	req.Workflows = nil // nil = not specified, flag must be absent

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), req)

	for i, arg := range capturedArgs {
		if arg == "--workflows" {
			t.Errorf("args[%d] = --workflows, want the flag to be absent when Workflows is nil (nil = not specified, not explicitly empty)", i)
		}
	}
}

// TestRender_WorkflowsNonNilSet_WorkflowsFlagPresentWithValues asserts that
// a non-nil, non-empty Workflows slice reaches the delegate as
// --workflows <comma-joined-ids>. This is how an orchestrator subject is
// rendered with its declared workflow set.
func TestRender_WorkflowsNonNilSet_WorkflowsFlagPresentWithValues(t *testing.T) {
	var capturedArgs []string
	req := minimalRequest()
	req.Workflows = []string{"tdd-soft", "review"} // non-nil, non-empty

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), req)

	idx := flagIndex(capturedArgs, "--workflows")
	if idx < 0 || idx+1 >= len(capturedArgs) {
		t.Errorf("args = %v, want --workflows <ids> to be present when Workflows is non-nil non-empty", capturedArgs)
		return
	}
	val := capturedArgs[idx+1]
	if val != "tdd-soft,review" {
		t.Errorf("--workflows value = %q, want %q (comma-joined, no other separator or ordering)", val, "tdd-soft,review")
	}
}

// TestRender_WorkflowsNonNilEmpty_WorkflowsFlagPassedAsEmptyString asserts
// the explicitly-none case: a non-nil but empty Workflows slice means the
// orchestrator is to be rendered with no workflows at all. The --workflows flag
// must be present with an empty value (not absent), so the deploy tool knows
// to suppress the workflow region rather than inherit a default.
func TestRender_WorkflowsNonNilEmpty_WorkflowsFlagPassedAsEmptyString(t *testing.T) {
	var capturedArgs []string
	req := minimalRequest()
	req.Workflows = []string{} // non-nil empty = explicitly none

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), req)

	idx := flagIndex(capturedArgs, "--workflows")
	if idx < 0 {
		t.Errorf("args = %v, want --workflows flag to be present even when Workflows is non-nil empty (explicitly none)", capturedArgs)
		return
	}
	if idx+1 >= len(capturedArgs) || capturedArgs[idx+1] != "" {
		var val string
		if idx+1 < len(capturedArgs) {
			val = capturedArgs[idx+1]
		}
		t.Errorf("--workflows value = %q, want empty string for explicitly-none workflow set", val)
	}
}

// TestRender_DryRun_PassedAsDryRunFlag asserts that DryRun: true in the
// request reaches the delegate as the --dry-run flag, so preflight's
// validation calls do not write any file.
func TestRender_DryRun_PassedAsDryRunFlag(t *testing.T) {
	var capturedArgs []string
	req := minimalRequest()
	req.DryRun = true

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), req)

	found := false
	for _, arg := range capturedArgs {
		if arg == "--dry-run" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("args = %v, want --dry-run to be present when DryRun is true", capturedArgs)
	}
}

// TestRender_DryRun_False_DryRunFlagAbsent asserts the symmetric case: when
// DryRun is false, --dry-run must be absent so the tool actually writes the
// file.
func TestRender_DryRun_False_DryRunFlagAbsent(t *testing.T) {
	var capturedArgs []string
	req := minimalRequest()
	req.DryRun = false

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), req)

	for i, arg := range capturedArgs {
		if arg == "--dry-run" {
			t.Errorf("args[%d] = --dry-run, want it to be absent when DryRun is false", i)
		}
	}
}

// TestRender_OutputJSONFlag_AlwaysPresent asserts that --output json is always
// passed so the deployer can parse the result rather than scraping human output.
func TestRender_OutputJSONFlag_AlwaysPresent(t *testing.T) {
	var capturedArgs []string

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), minimalRequest())

	if !containsFlag(capturedArgs, "--output", "json") {
		t.Errorf("args = %v, want --output json to always be present so the deployer can parse the result", capturedArgs)
	}
}

// TestRender_OverwriteFlag_AlwaysPresent asserts that --overwrite is always
// passed: a freshly created sandbox may already contain a seeded file at the
// destination (e.g. from a previous partial run), and a render that refuses to
// overwrite would stall setup rather than refreshing the file.
func TestRender_OverwriteFlag_AlwaysPresent(t *testing.T) {
	var capturedArgs []string

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), minimalRequest())

	found := false
	for _, arg := range capturedArgs {
		if arg == "--overwrite" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("args = %v, want --overwrite to always be present so a seeded file can be refreshed", capturedArgs)
	}
}

// TestRender_MosaicRoot_PassedAsMosaicRootFlag asserts that a non-empty
// MosaicRoot in Options reaches the delegate as the --mosaic-root flag, so the
// deployment tool resolves its resources from the caller-specified root rather
// than its own auto-discovery.
func TestRender_MosaicRoot_PassedAsMosaicRootFlag(t *testing.T) {
	var capturedArgs []string

	deployer := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		MosaicRoot:     "/opt/mosaic",
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), minimalRequest())

	if !containsFlag(capturedArgs, "--mosaic-root", "/opt/mosaic") {
		t.Errorf("args = %v, want --mosaic-root /opt/mosaic when MosaicRoot is set", capturedArgs)
	}
}

// TestRender_MosaicRootEmpty_MosaicRootFlagAbsent asserts that when MosaicRoot
// is empty (the default), --mosaic-root is not passed, so the deployment tool
// resolves its own root — the correct behaviour for a correctly staged
// distribution.
func TestRender_MosaicRootEmpty_MosaicRootFlagAbsent(t *testing.T) {
	var capturedArgs []string

	deployer := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		MosaicRoot:     "", // empty = do not override
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), minimalRequest())

	if flagIndex(capturedArgs, "--mosaic-root") >= 0 {
		t.Errorf("args = %v, want --mosaic-root to be absent when MosaicRoot is empty", capturedArgs)
	}
}

// TestRender_Model_PassedAsModelFlag asserts that a non-empty Model in the
// request reaches the delegate as the --model flag value. The rendered agent
// declares the specified model; without this pass-through the field is silently
// dropped and every rendered agent falls back to a default regardless of what
// the caller requested.
func TestRender_Model_PassedAsModelFlag(t *testing.T) {
	var capturedArgs []string
	req := minimalRequest()
	req.Model = "claude-opus-4-5"

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), req)

	if !containsFlag(capturedArgs, "--model", "claude-opus-4-5") {
		t.Errorf("args = %v, want --model claude-opus-4-5 when Model is set", capturedArgs)
	}
}

// TestRender_ModelEmpty_ModelFlagAbsent asserts that when Model is empty (the
// default), --model is not passed, leaving the rendered agent to declare its
// own model or inherit a default from the catalogue definition.
func TestRender_ModelEmpty_ModelFlagAbsent(t *testing.T) {
	var capturedArgs []string
	req := minimalRequest()
	req.Model = "" // empty = do not override

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), req)

	if flagIndex(capturedArgs, "--model") >= 0 {
		t.Errorf("args = %v, want --model to be absent when Model is empty", capturedArgs)
	}
}

// =============================================================================
// Helpers
// =============================================================================

// containsFlag reports whether args contains flag followed immediately by value.
func containsFlag(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

// flagIndex returns the index of flag in args, or -1 if absent.
func flagIndex(args []string, flag string) int {
	for i, arg := range args {
		if arg == flag {
			return i
		}
	}
	return -1
}
