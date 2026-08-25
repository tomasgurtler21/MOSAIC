package agentdeploy_test

// Tests for the delegating agent deployer. The Render tests cover exit-code
// outcome mapping, the successful-render destination-path and source-version
// contract, failure and degradation modes, and argument pass-through including
// the workflow set. The Deploy tests cover argument-list construction for the
// deploy subcommand, workflow-ID wire guarantees, result mapping from the
// run-summary JSON, failure/timeout/cancellation mapping, and the temporary
// selections file contract.
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
	"os"
	"path/filepath"
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

// TestRender_CatalogFolder_PassedAsCatalogFolderFlag asserts that a non-empty
// CatalogFolder in Options reaches the delegate as the --catalog-folder flag,
// so the deployment tool sources agents and workflows from the caller-specified
// catalogue rather than its own auto-resolved one. This mirrors the MosaicRoot
// pattern exactly: both are root-scoped overrides and both follow the same
// empty-means-omitted convention.
func TestRender_CatalogFolder_PassedAsCatalogFolderFlag(t *testing.T) {
	var capturedArgs []string

	deployer := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		CatalogFolder:  "/opt/mosaic/Tools/AgentTest/catalog",
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), minimalRequest())

	if !containsFlag(capturedArgs, "--catalog-folder", "/opt/mosaic/Tools/AgentTest/catalog") {
		t.Errorf("args = %v, want --catalog-folder /opt/mosaic/Tools/AgentTest/catalog when CatalogFolder is set", capturedArgs)
	}
}

// TestRender_CatalogFolderEmpty_CatalogFolderFlagAbsent asserts that when
// CatalogFolder is empty (the default), --catalog-folder is not passed, so
// the deployment tool resolves its own catalogue under its own root — the
// correct behaviour for a correctly staged distribution. This is the
// empty-means-omitted guard that keeps every existing Render call's argument
// list byte-identical when the new field is left at its zero value.
func TestRender_CatalogFolderEmpty_CatalogFolderFlagAbsent(t *testing.T) {
	var capturedArgs []string

	deployer := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		CatalogFolder:  "", // empty = do not override
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), minimalRequest())

	if flagIndex(capturedArgs, "--catalog-folder") >= 0 {
		t.Errorf("args = %v, want --catalog-folder to be absent when CatalogFolder is empty (empty means do not override)", capturedArgs)
	}
}

// TestRender_CatalogFolderEmpty_ArgumentListByteIdenticalToBaseline pins the
// backwards-compatibility contract: the argument list produced with CatalogFolder
// explicitly empty must be byte-identical to the list produced when the field
// is absent at all. Any deviation — including appending the flag with an empty
// value — would silently change every pre-existing Render call's wire shape.
//
// This test proves the empty-means-omitted invariant is a compiler-verifiable
// property, not just a comment: both Options values must produce the same slice.
func TestRender_CatalogFolderEmpty_ArgumentListByteIdenticalToBaseline(t *testing.T) {
	req := minimalRequest()

	// Baseline: Options without CatalogFolder in the struct at all — this
	// is the construction every pre-existing test uses.
	var baselineArgs []string
	baselineDeployer := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		MosaicRoot:     "/opt/mosaic",
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			baselineArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})
	_, _ = baselineDeployer.Render(context.Background(), req)

	// Same Options but with CatalogFolder set to the empty string explicitly.
	// Must produce the identical argument slice.
	var withEmptyArgs []string
	withEmptyDeployer := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		MosaicRoot:     "/opt/mosaic",
		CatalogFolder:  "", // explicit empty — must not alter the argument list
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			withEmptyArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})
	_, _ = withEmptyDeployer.Render(context.Background(), req)

	if len(baselineArgs) != len(withEmptyArgs) {
		t.Errorf("arg count: baseline Options = %d args, Options with empty CatalogFolder = %d args; want byte-identical lists (empty CatalogFolder must not add any argument)",
			len(baselineArgs), len(withEmptyArgs))
		return
	}
	for i := range baselineArgs {
		if baselineArgs[i] != withEmptyArgs[i] {
			t.Errorf("args[%d]: baseline = %q, with empty CatalogFolder = %q; want byte-identical argument lists",
				i, baselineArgs[i], withEmptyArgs[i])
		}
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

// =============================================================================
// Deploy test helpers
// =============================================================================

// agentActionJSON returns a JSON object for an agent artifact action in the
// wireActionRecord shape (capital field names — RunSummary carries no JSON tags).
func agentActionJSON(key, targetPath string) string {
	return `{"Ref":{"Kind":"agent","Key":"` + key + `"},"TargetPath":"` + targetPath + `","Taken":"created","Err":""}`
}

// nonAgentActionJSON returns a JSON object for a non-agent artifact action
// (e.g. a skill or hook). These must not appear in DeployResult.Agents.
func nonAgentActionJSON(kind, key, targetPath string) string {
	return `{"Ref":{"Kind":"` + kind + `","Key":"` + key + `"},"TargetPath":"` + targetPath + `","Taken":"created","Err":""}`
}

// deploySuccessJSON builds a minimal valid run summary JSON for exit 0.
// actions are individual wireActionRecord JSON objects.
func deploySuccessJSON(actions ...string) []byte {
	if len(actions) == 0 {
		return []byte(`{"Actions":[],"Outcome":"success"}`)
	}
	return []byte(`{"Actions":[` + strings.Join(actions, ",") + `],"Outcome":"success"}`)
}

// minimalDeployRequest builds the smallest DeployRequest that satisfies the
// always-present-workflows contract. Tests that care about specific field values
// set them explicitly.
func minimalDeployRequest() domain.DeployRequest {
	return domain.DeployRequest{
		HarnessID:     "claude-code",
		WorkspaceRoot: "/sandbox",
		Workflows:     []string{"brownfield-tdd"},
	}
}

// =============================================================================
// T3.1 — Deploy argument-list construction
// =============================================================================

// TestDeploy_SubcommandName_IsDeployNotRender asserts that Deploy invokes the
// "deploy" subcommand. Using the wrong subcommand silently routes the call to
// the render handler, which has a completely different argument surface.
func TestDeploy_SubcommandName_IsDeployNotRender(t *testing.T) {
	var capturedArgs []string

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), minimalDeployRequest())

	if len(capturedArgs) == 0 {
		t.Fatal("Deploy passed no arguments to CommandRunner, want at least the subcommand name")
	}
	if capturedArgs[0] != "deploy" {
		t.Errorf("args[0] = %q, want %q (the deploy subcommand, not render)", capturedArgs[0], "deploy")
	}
}

// TestDeploy_HarnessID_PassedAsHarnessFlag asserts that the HarnessID from the
// request reaches the delegate as the --harness flag.
func TestDeploy_HarnessID_PassedAsHarnessFlag(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.HarnessID = "claude-code"

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if !containsFlag(capturedArgs, "--harness", "claude-code") {
		t.Errorf("args = %v, want --harness claude-code", capturedArgs)
	}
}

// TestDeploy_WorkspaceRoot_PassedAsWorkspaceFlag asserts that WorkspaceRoot
// from the request reaches the delegate as --workspace.
func TestDeploy_WorkspaceRoot_PassedAsWorkspaceFlag(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.WorkspaceRoot = "/var/sandbox/run-99"

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if !containsFlag(capturedArgs, "--workspace", "/var/sandbox/run-99") {
		t.Errorf("args = %v, want --workspace /var/sandbox/run-99", capturedArgs)
	}
}

// TestDeploy_OutputJSONFlag_AlwaysPresent asserts that --output json is always
// passed so the deployer can parse the run summary rather than scraping
// human-readable output.
func TestDeploy_OutputJSONFlag_AlwaysPresent(t *testing.T) {
	var capturedArgs []string

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), minimalDeployRequest())

	if !containsFlag(capturedArgs, "--output", "json") {
		t.Errorf("args = %v, want --output json to always be present", capturedArgs)
	}
}

// TestDeploy_AutoConfirmFlag_AlwaysPresent asserts that --auto-confirm is
// always passed. Without it the deployment tool halts on a plan-review gate
// and the non-interactive call fails before writing anything.
func TestDeploy_AutoConfirmFlag_AlwaysPresent(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.DryRun = false

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	found := false
	for _, arg := range capturedArgs {
		if arg == "--auto-confirm" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("args = %v, want --auto-confirm to always be present (plan-review gate fires on every call)", capturedArgs)
	}
}

// TestDeploy_AutoConfirmFlag_PresentEvenOnDryRun asserts that --auto-confirm is
// present even when DryRun is true. The plan-review gate runs before the
// dry-run check, so a dry-run call without --auto-confirm fails on the gate.
func TestDeploy_AutoConfirmFlag_PresentEvenOnDryRun(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.DryRun = true

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	found := false
	for _, arg := range capturedArgs {
		if arg == "--auto-confirm" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("args = %v, want --auto-confirm even on a dry-run call (plan-review gate fires before the dry-run check)", capturedArgs)
	}
}

// TestDeploy_DryRun_PassedAsDryRunFlag asserts that DryRun: true reaches the
// delegate as --dry-run, so preflight validation calls do not write any file.
func TestDeploy_DryRun_PassedAsDryRunFlag(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.DryRun = true

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	found := false
	for _, arg := range capturedArgs {
		if arg == "--dry-run" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("args = %v, want --dry-run when DryRun is true", capturedArgs)
	}
}

// TestDeploy_DryRunFalse_DryRunFlagAbsent asserts that when DryRun is false,
// --dry-run is absent so the tool actually writes the files.
func TestDeploy_DryRunFalse_DryRunFlagAbsent(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.DryRun = false

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if len(capturedArgs) == 0 {
		t.Fatal("Deploy did not invoke CommandRunner; cannot assert argument absence")
	}
	for i, arg := range capturedArgs {
		if arg == "--dry-run" {
			t.Errorf("args[%d] = --dry-run, want it absent when DryRun is false", i)
		}
	}
}

// TestDeploy_CatalogFolder_PassedAsCatalogFolderFlag asserts that a non-empty
// CatalogFolder in Options reaches the delegate as --catalog-folder. This is
// what makes a test run source from the test catalogue instead of the
// production one.
func TestDeploy_CatalogFolder_PassedAsCatalogFolderFlag(t *testing.T) {
	var capturedArgs []string

	d := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		CatalogFolder:  "/opt/mosaic/Tools/AgentTest/catalog",
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), minimalDeployRequest())

	if !containsFlag(capturedArgs, "--catalog-folder", "/opt/mosaic/Tools/AgentTest/catalog") {
		t.Errorf("args = %v, want --catalog-folder /opt/mosaic/Tools/AgentTest/catalog when CatalogFolder is set", capturedArgs)
	}
}

// TestDeploy_CatalogFolderEmpty_CatalogFolderFlagAbsent asserts that when
// CatalogFolder is empty, --catalog-folder is not passed. Empty-means-omitted
// keeps real deployments unaffected when no catalogue override is configured.
func TestDeploy_CatalogFolderEmpty_CatalogFolderFlagAbsent(t *testing.T) {
	var capturedArgs []string

	d := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		CatalogFolder:  "",
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), minimalDeployRequest())

	if len(capturedArgs) == 0 {
		t.Fatal("Deploy did not invoke CommandRunner; cannot assert argument absence")
	}
	if flagIndex(capturedArgs, "--catalog-folder") >= 0 {
		t.Errorf("args = %v, want --catalog-folder absent when CatalogFolder is empty", capturedArgs)
	}
}

// TestDeploy_MosaicRoot_PassedAsMosaicRootFlag asserts that a non-empty
// MosaicRoot in Options reaches the delegate as --mosaic-root on the deploy
// subcommand. This mirrors the Render variant: an implementation that passes
// --mosaic-root to render but omits it from deploy would go undetected without
// this test.
func TestDeploy_MosaicRoot_PassedAsMosaicRootFlag(t *testing.T) {
	var capturedArgs []string

	d := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		MosaicRoot:     "/opt/mosaic",
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), minimalDeployRequest())

	if !containsFlag(capturedArgs, "--mosaic-root", "/opt/mosaic") {
		t.Errorf("args = %v, want --mosaic-root /opt/mosaic when MosaicRoot is set", capturedArgs)
	}
}

// TestDeploy_MosaicRootEmpty_MosaicRootFlagAbsent asserts that when MosaicRoot
// is empty (the default), --mosaic-root is not passed to the deploy subcommand,
// so the deployment tool resolves its own root. Empty-means-omitted is symmetric
// with the Render path.
func TestDeploy_MosaicRootEmpty_MosaicRootFlagAbsent(t *testing.T) {
	var capturedArgs []string

	d := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		MosaicRoot:     "", // empty = do not override
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), minimalDeployRequest())

	if len(capturedArgs) == 0 {
		t.Fatal("Deploy did not invoke CommandRunner; cannot assert argument absence")
	}
	if flagIndex(capturedArgs, "--mosaic-root") >= 0 {
		t.Errorf("args = %v, want --mosaic-root to be absent when MosaicRoot is empty", capturedArgs)
	}
}

// =============================================================================
// T3.2 — Workflow IDs are never nil/absent on the wire
// =============================================================================

// TestDeploy_WorkflowsNil_WorkflowsFlagPresentOnWire asserts the critical
// non-nil guarantee: when the caller supplies nil Workflows ("not specified"),
// Deploy must still emit --workflows. Without this guard, a nil selection
// reaching the tool resolves to an empty list, deploys only the orchestrator,
// and silently omits every workflow-referenced agent while reporting success.
func TestDeploy_WorkflowsNil_WorkflowsFlagPresentOnWire(t *testing.T) {
	var capturedArgs []string
	req := domain.DeployRequest{
		HarnessID:     "claude-code",
		WorkspaceRoot: "/sandbox",
		Workflows:     nil, // explicitly nil — caller did not specify
	}

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if flagIndex(capturedArgs, "--workflows") < 0 {
		t.Errorf("args = %v, want --workflows to be present even when request.Workflows is nil "+
			"(nil means 'not specified' by the caller, but Deploy must always supply a selection on the wire "+
			"to prevent a silent empty deployment)", capturedArgs)
	}
}

// TestDeploy_WorkflowsNonNilEmpty_WorkflowsFlagPresentAsEmpty asserts that an
// explicitly-empty workflow set (non-nil but zero-length) reaches the delegate
// as --workflows with an empty value.
func TestDeploy_WorkflowsNonNilEmpty_WorkflowsFlagPresentAsEmpty(t *testing.T) {
	var capturedArgs []string
	req := domain.DeployRequest{
		HarnessID:     "claude-code",
		WorkspaceRoot: "/sandbox",
		Workflows:     []string{}, // non-nil empty = explicitly none
	}

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	idx := flagIndex(capturedArgs, "--workflows")
	if idx < 0 {
		t.Errorf("args = %v, want --workflows flag to be present when Workflows is non-nil empty", capturedArgs)
		return
	}
	if idx+1 < len(capturedArgs) && capturedArgs[idx+1] != "" {
		t.Errorf("--workflows value = %q, want empty string for an explicitly-empty workflow set", capturedArgs[idx+1])
	}
}

// TestDeploy_WorkflowsPopulated_WorkflowsFlagWithCommaJoinedValues asserts
// that a populated Workflows slice reaches the delegate as
// --workflows <comma-joined-ids>.
func TestDeploy_WorkflowsPopulated_WorkflowsFlagWithCommaJoinedValues(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.Workflows = []string{"brownfield-tdd", "greenfield"}

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	idx := flagIndex(capturedArgs, "--workflows")
	if idx < 0 || idx+1 >= len(capturedArgs) {
		t.Errorf("args = %v, want --workflows <ids> when Workflows is populated", capturedArgs)
		return
	}
	if capturedArgs[idx+1] != "brownfield-tdd,greenfield" {
		t.Errorf("--workflows value = %q, want %q", capturedArgs[idx+1], "brownfield-tdd,greenfield")
	}
}

// =============================================================================
// T3.3 — Result mapping from the tool's JSON run summary
// =============================================================================

// TestDeploy_SuccessJSON_ReturnsNilError asserts that exit 0 with a valid run
// summary produces a nil error.
func TestDeploy_SuccessJSON_ReturnsNilError(t *testing.T) {
	action := agentActionJSON("orchestrator", "/sandbox/.claude/agents/orchestrator.md")

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return deploySuccessJSON(action), nil, exitSuccess, nil
		},
	})

	_, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err != nil {
		t.Errorf("Deploy returned error on exit 0 with valid JSON: %v", err)
	}
}

// TestDeploy_SuccessJSON_AgentActionsInResult asserts that agent artifact
// actions from the run summary appear in DeployResult.Agents with the correct
// Key and DestinationPath from the JSON output.
func TestDeploy_SuccessJSON_AgentActionsInResult(t *testing.T) {
	action := agentActionJSON("orchestrator", "/sandbox/.claude/agents/orchestrator.md")

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return deploySuccessJSON(action), nil, exitSuccess, nil
		},
	})

	result, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}
	if len(result.Agents) != 1 {
		t.Fatalf("DeployResult.Agents length = %d, want 1", len(result.Agents))
	}
	if result.Agents[0].Key != "orchestrator" {
		t.Errorf("Agents[0].Key = %q, want %q", result.Agents[0].Key, "orchestrator")
	}
	if result.Agents[0].DestinationPath != "/sandbox/.claude/agents/orchestrator.md" {
		t.Errorf("Agents[0].DestinationPath = %q, want %q",
			result.Agents[0].DestinationPath, "/sandbox/.claude/agents/orchestrator.md")
	}
}

// TestDeploy_SuccessJSON_NonAgentActionsDropped asserts that non-agent
// artifacts in the run summary (skills, hooks) are not included in
// DeployResult.Agents.
func TestDeploy_SuccessJSON_NonAgentActionsDropped(t *testing.T) {
	agentAction := agentActionJSON("orchestrator", "/sandbox/.claude/agents/orchestrator.md")
	skillAction := nonAgentActionJSON("skill", "lean-tdd", "/sandbox/.claude/skills/lean-tdd.md")
	hookAction := nonAgentActionJSON("hook", "pre-tool", "/sandbox/.claude/hooks/pre-tool.sh")

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return deploySuccessJSON(agentAction, skillAction, hookAction), nil, exitSuccess, nil
		},
	})

	result, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}
	if len(result.Agents) != 1 {
		t.Errorf("DeployResult.Agents length = %d, want 1 (only the agent action; skill and hook must be dropped)",
			len(result.Agents))
	}
	if len(result.Agents) == 1 && result.Agents[0].Key != "orchestrator" {
		t.Errorf("Agents[0].Key = %q, want %q", result.Agents[0].Key, "orchestrator")
	}
}

// TestDeploy_SuccessJSON_MultipleAgents_OrderPreservedFromJSONOutput asserts
// that multiple agent entries are returned in the order the tool reported them.
// No re-sorting or re-grouping is applied.
func TestDeploy_SuccessJSON_MultipleAgents_OrderPreservedFromJSONOutput(t *testing.T) {
	action1 := agentActionJSON("orchestrator", "/sandbox/.claude/agents/orchestrator.md")
	action2 := agentActionJSON("codebase-research", "/sandbox/.claude/agents/codebase-research.md")
	action3 := agentActionJSON("researcher", "/sandbox/.claude/agents/researcher.md")

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return deploySuccessJSON(action1, action2, action3), nil, exitSuccess, nil
		},
	})

	result, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}
	if len(result.Agents) != 3 {
		t.Fatalf("DeployResult.Agents length = %d, want 3", len(result.Agents))
	}
	wantKeys := []string{"orchestrator", "codebase-research", "researcher"}
	for i, want := range wantKeys {
		if result.Agents[i].Key != want {
			t.Errorf("Agents[%d].Key = %q, want %q (order must match the tool's reported order)",
				i, result.Agents[i].Key, want)
		}
	}
}

// TestDeploy_SuccessJSON_EmptyActions_NoError asserts that a run summary with
// no actions produces no error (empty is a valid outcome).
func TestDeploy_SuccessJSON_EmptyActions_NoError(t *testing.T) {
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err != nil {
		t.Errorf("Deploy returned error on exit 0 with empty Actions list: %v", err)
	}
}

// =============================================================================
// T3.4 — Failure, timeout, and cancellation mapping
// =============================================================================

// TestDeploy_NonZeroExit_ReturnsErrDeployFailed asserts that a non-zero exit
// maps to ErrDeployFailed, not ErrRenderFailed (which belongs to a different
// subcommand's failure vocabulary).
func TestDeploy_NonZeroExit_ReturnsErrDeployFailed(t *testing.T) {
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte("error: catalog not found"), []byte("catalog not found"), exitFailure, nil
		},
	})

	_, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1, want ErrDeployFailed")
	}
	if !errors.Is(err, agentdeploy.ErrDeployFailed) {
		t.Errorf("error = %v, want it to wrap ErrDeployFailed (not ErrRenderFailed)", err)
	}
}

// TestDeploy_NonZeroExit_ReturnsDeployErrorType asserts that the error on a
// non-zero exit is a *DeployError, which callers can inspect with errors.As.
func TestDeploy_NonZeroExit_ReturnsDeployErrorType(t *testing.T) {
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte("error: something failed"), []byte("something failed"), exitFailure, nil
		},
	})

	_, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1")
	}
	var de *agentdeploy.DeployError
	if !errors.As(err, &de) {
		t.Errorf("error = %v, want a *DeployError carrying diagnostic detail", err)
	}
}

// TestDeploy_NonZeroExit_DeployErrorCarriesExitCode asserts that the exit code
// the seam returned is stored in DeployError.ExitCode. Without this pin, an
// implementation that always stores 0 would pass the other DeployError tests
// while breaking callers that branch on the code.
func TestDeploy_NonZeroExit_DeployErrorCarriesExitCode(t *testing.T) {
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte("error: something failed"), []byte("something failed"), exitFailure, nil
		},
	})

	_, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1")
	}
	var de *agentdeploy.DeployError
	if !errors.As(err, &de) {
		t.Fatalf("error = %v, want a *DeployError", err)
	}
	if de.ExitCode != exitFailure {
		t.Errorf("DeployError.ExitCode = %d, want %d (the exit code the seam returned)", de.ExitCode, exitFailure)
	}
}

// TestDeploy_NonZeroExit_DeployErrorHasEmptyReason asserts that the deploy
// subcommand's absence of a structured failure envelope means Reason is always
// empty. Callers must treat an empty Reason as "undifferentiated failure".
func TestDeploy_NonZeroExit_DeployErrorHasEmptyReason(t *testing.T) {
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte("error: some deployment problem"), []byte("some deployment problem"), exitFailure, nil
		},
	})

	_, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1")
	}
	var de *agentdeploy.DeployError
	if !errors.As(err, &de) {
		t.Fatalf("error = %v, want a *DeployError", err)
	}
	if de.Reason != "" {
		t.Errorf("DeployError.Reason = %q, want empty (deploy subcommand emits no structured failure envelope)", de.Reason)
	}
}

// TestDeploy_NonZeroExit_ToolMessageFromStderr asserts that the deployment
// tool's stderr text is preserved in DeployError.ToolMessage. The deploy
// subcommand writes its diagnostic to stderr even under --output json.
func TestDeploy_NonZeroExit_ToolMessageFromStderr(t *testing.T) {
	const stderrText = "Error: workflow 'unknown-wf' not found in catalog"
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte("not json"), []byte(stderrText), exitFailure, nil
		},
	})

	_, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1")
	}
	var de *agentdeploy.DeployError
	if !errors.As(err, &de) {
		t.Fatalf("error = %v, want a *DeployError carrying the stderr text", err)
	}
	if !strings.Contains(de.ToolMessage, stderrText) {
		t.Errorf("DeployError.ToolMessage = %q, want it to contain the tool's stderr text %q verbatim", de.ToolMessage, stderrText)
	}
}

// TestDeploy_ZeroExitUnparseableOutput_ReturnsErrUnparseableOutput asserts
// that exit 0 with undecodable stdout is ErrUnparseableOutput — distinct from
// ErrDeployFailed so a caller can tell apart infrastructure garbage from a
// rejected request.
func TestDeploy_ZeroExitUnparseableOutput_ReturnsErrUnparseableOutput(t *testing.T) {
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte("not valid json"), nil, exitSuccess, nil
		},
	})

	_, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 0 with unparseable output, want ErrUnparseableOutput")
	}
	if !errors.Is(err, agentdeploy.ErrUnparseableOutput) {
		t.Errorf("error = %v, want it to wrap ErrUnparseableOutput", err)
	}
}

// TestDeploy_ZeroExitUnparseableOutput_ErrorNamesOutputSize asserts the
// diagnosability requirement for the unparseable-output case on the Deploy path:
// the error must name how many bytes were produced, so a developer can distinguish
// "exited zero and said nothing" from "exited zero and said something garbled".
// This mirrors TestRender_ZeroExitUnparseableOutput_ErrorNamesOutputSize.
func TestDeploy_ZeroExitUnparseableOutput_ErrorNamesOutputSize(t *testing.T) {
	const output = "not valid json at all"
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return []byte(output), nil, exitSuccess, nil
		},
	})

	_, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 0 with unparseable output")
	}
	// The error must name the byte count so "zero bytes" is distinguishable
	// from "non-zero bytes that are garbled".
	byteCount := fmt.Sprintf("%d", len(output))
	if !strings.Contains(err.Error(), byteCount) && !strings.Contains(err.Error(), "bytes") {
		t.Errorf("error = %q, want it to name the output size (%s bytes) for diagnosability", err.Error(), byteCount)
	}
}

// TestDeploy_ToolUnavailable_ReturnsErrToolUnavailable asserts that an
// invocation failure maps to ErrToolUnavailable, not ErrDeployFailed — a
// missing binary is an infrastructure problem, not a bad declaration.
func TestDeploy_ToolUnavailable_ReturnsErrToolUnavailable(t *testing.T) {
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, nil, 0, errors.New("exec: executable file not found in $PATH")
		},
	})

	_, err := d.Deploy(context.Background(), minimalDeployRequest())

	if err == nil {
		t.Fatal("Deploy returned nil error when the tool cannot be invoked, want ErrToolUnavailable")
	}
	if !errors.Is(err, agentdeploy.ErrToolUnavailable) {
		t.Errorf("error = %v, want it to wrap ErrToolUnavailable", err)
	}
}

// TestDeploy_PortTimeout_ReturnsErrTimedOut asserts that when the configured
// timeout fires before the tool responds, Deploy returns ErrTimedOut without
// hanging.
func TestDeploy_PortTimeout_ReturnsErrTimedOut(t *testing.T) {
	d := newDeployer(agentdeploy.Options{
		Timeout: 20 * time.Millisecond,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			<-ctx.Done()
			return nil, nil, 0, ctx.Err()
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := d.Deploy(ctx, minimalDeployRequest())

	if err == nil {
		t.Fatal("Deploy returned nil error when the port timeout fired, want ErrTimedOut")
	}
	if !errors.Is(err, agentdeploy.ErrTimedOut) {
		t.Errorf("error = %v, want it to wrap ErrTimedOut (not ErrToolUnavailable or ErrDeployFailed)", err)
	}
}

// TestDeploy_CallerContextCancelled_ReturnsError asserts that a cancelled
// caller context produces an error that wraps context.Canceled and is NOT
// ErrTimedOut and NOT ErrToolUnavailable — caller cancellation must propagate
// as-is, not be remapped to a port-specific error class.
func TestDeploy_CallerContextCancelled_ReturnsError(t *testing.T) {
	d := newDeployer(agentdeploy.Options{
		Timeout: 10 * time.Second,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			<-ctx.Done()
			return nil, nil, 0, ctx.Err()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := d.Deploy(ctx, minimalDeployRequest())

	if err == nil {
		t.Fatal("Deploy returned nil error when the caller's context was cancelled, want an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled (caller cancellation must propagate as-is)", err)
	}
	if errors.Is(err, agentdeploy.ErrTimedOut) {
		t.Errorf("error = %v, want it NOT to wrap ErrTimedOut (caller cancellation is not a port timeout)", err)
	}
	if errors.Is(err, agentdeploy.ErrToolUnavailable) {
		t.Errorf("error = %v, want it NOT to wrap ErrToolUnavailable (caller cancellation is not an invocation failure)", err)
	}
}

// =============================================================================
// T3.5 — Temporary selections file: creation, content, and cleanup
// =============================================================================

// TestDeploy_PopulatedTierModels_SelectionsFlagPresent asserts that a non-empty
// TierModels map causes --selections to appear in the argument list. Without
// this flag the tool receives no tier-model map and the run succeeds with
// unresolved model placeholders.
func TestDeploy_PopulatedTierModels_SelectionsFlagPresent(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.TierModels = map[string]string{
		domain.TierTestSubject: "claude-opus-4-5",
		domain.TierTestStub:    "claude-haiku-3-5",
	}

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if flagIndex(capturedArgs, "--selections") < 0 {
		t.Errorf("args = %v, want --selections <path> when TierModels is populated", capturedArgs)
	}
}

// TestDeploy_PopulatedTierModels_SelectionsFileExistsDuringCall asserts that
// the selections file exists while the CommandRunner seam is executing. The
// seam is the only place where both the --selections path and an in-flight
// file are simultaneously observable.
func TestDeploy_PopulatedTierModels_SelectionsFileExistsDuringCall(t *testing.T) {
	req := minimalDeployRequest()
	req.TierModels = map[string]string{
		domain.TierTestSubject: "claude-opus-4-5",
	}

	var fileExistedDuringCall bool
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				if _, err := os.Stat(args[idx+1]); err == nil {
					fileExistedDuringCall = true
				}
			}
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if !fileExistedDuringCall {
		t.Error("selections file did not exist while CommandRunner was executing, " +
			"want a readable file at the --selections path during the call")
	}
}

// TestDeploy_PopulatedTierModels_SelectionsFileHasTierModelsKey asserts that
// the temporary selections file contains the tier_models key with the caller's
// map entries verbatim. Without the correct key the deployment tool finds no
// tier pre-answers.
func TestDeploy_PopulatedTierModels_SelectionsFileHasTierModelsKey(t *testing.T) {
	req := minimalDeployRequest()
	req.TierModels = map[string]string{
		domain.TierTestSubject: "claude-opus-4-5",
		domain.TierTestStub:    "claude-haiku-3-5",
	}

	var fileContent []byte
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				fileContent, _ = os.ReadFile(args[idx+1])
			}
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if len(fileContent) == 0 {
		t.Fatal("selections file was empty or not readable during the call; cannot assert content")
	}
	s := string(fileContent)
	for _, want := range []string{
		"tier_models:",
		domain.TierTestSubject,
		"claude-opus-4-5",
		domain.TierTestStub,
		"claude-haiku-3-5",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("selections file content = %q, want it to contain %q", s, want)
		}
	}
}

// TestDeploy_PopulatedTierModels_SelectionsFileHasNoOtherTopLevelKeys asserts
// that the temporary selections file contains ONLY the tier_models key. An
// emitted workflows key — even empty — would fight the --workflows flag this
// same call carries.
func TestDeploy_PopulatedTierModels_SelectionsFileHasNoOtherTopLevelKeys(t *testing.T) {
	req := minimalDeployRequest()
	req.TierModels = map[string]string{
		domain.TierTestSubject: "claude-opus-4-5",
	}

	var fileContent []byte
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				fileContent, _ = os.ReadFile(args[idx+1])
			}
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if len(fileContent) == 0 {
		t.Fatal("selections file was empty or not readable during the call; cannot assert content")
	}
	s := string(fileContent)
	for _, forbidden := range []string{"workflows:", "utility_agents:", "infrastructure_agents:", "hooks:"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("selections file content = %q, must not contain %q "+
				"(only tier_models is permitted; any other key could conflict with explicit flags)", s, forbidden)
		}
	}
}

// TestDeploy_EmptyTierModels_NoSelectionsFlagOrFile asserts that an empty
// TierModels map produces no --selections flag. Empty-means-omitted prevents
// a no-op selections file from silently overriding a flag on a real call.
func TestDeploy_EmptyTierModels_NoSelectionsFlagOrFile(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.TierModels = map[string]string{} // empty map

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if len(capturedArgs) == 0 {
		t.Fatal("Deploy did not invoke CommandRunner; cannot assert argument absence")
	}
	if flagIndex(capturedArgs, "--selections") >= 0 {
		t.Errorf("args = %v, want --selections to be absent when TierModels is empty", capturedArgs)
	}
}

// TestDeploy_NilTierModels_NoSelectionsFlagOrFile asserts that a nil TierModels
// map (the zero value) produces no --selections flag. Callers that do not
// pre-answer tier models must not cause a file to be created.
func TestDeploy_NilTierModels_NoSelectionsFlagOrFile(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.TierModels = nil // nil map

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if len(capturedArgs) == 0 {
		t.Fatal("Deploy did not invoke CommandRunner; cannot assert argument absence")
	}
	if flagIndex(capturedArgs, "--selections") >= 0 {
		t.Errorf("args = %v, want --selections to be absent when TierModels is nil", capturedArgs)
	}
}

// TestDeploy_SelectionsFile_RemovedAfterSuccess asserts that no temporary
// selections file remains on disk after a successful Deploy call. A file per
// call accumulates across a long suite run.
func TestDeploy_SelectionsFile_RemovedAfterSuccess(t *testing.T) {
	var capturedPath string
	req := minimalDeployRequest()
	req.TierModels = map[string]string{domain.TierTestSubject: "claude-opus-4-5"}

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				capturedPath = args[idx+1]
			}
			return deploySuccessJSON(agentActionJSON("orchestrator", "/sandbox/.claude/agents/orchestrator.md")), nil, exitSuccess, nil
		},
	})

	_, err := d.Deploy(context.Background(), req)

	if err != nil {
		t.Fatalf("Deploy returned error (want success for this cleanup test): %v", err)
	}
	if capturedPath == "" {
		t.Fatal("--selections path was not captured from CommandRunner; cannot assert file removal")
	}
	if _, statErr := os.Stat(capturedPath); !os.IsNotExist(statErr) {
		t.Errorf("selections file at %q still exists after Deploy returned successfully; "+
			"it must be removed on every exit path including success", capturedPath)
	}
}

// TestDeploy_SelectionsFile_RemovedAfterFailure asserts that no temporary
// selections file remains on disk after a failed Deploy call. Cleanup must be
// unconditional — a leaked file on the failure path is a defect.
func TestDeploy_SelectionsFile_RemovedAfterFailure(t *testing.T) {
	var capturedPath string
	req := minimalDeployRequest()
	req.TierModels = map[string]string{domain.TierTestSubject: "claude-opus-4-5"}

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				capturedPath = args[idx+1]
			}
			// Simulate a non-zero exit.
			return []byte("error: something went wrong"), []byte("something went wrong"), exitFailure, nil
		},
	})

	_, err := d.Deploy(context.Background(), req)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1 (want failure for this cleanup test)")
	}
	if capturedPath == "" {
		t.Fatal("--selections path was not captured from CommandRunner; cannot assert file removal")
	}
	if _, statErr := os.Stat(capturedPath); !os.IsNotExist(statErr) {
		t.Errorf("selections file at %q still exists after Deploy returned a failure; "+
			"it must be removed on every exit path including failure", capturedPath)
	}
}

// TestDeploy_SelectionsFile_RemovedAfterTimeout asserts that no temporary
// selections file remains on disk when the port's own timeout fires. Cleanup
// must be unconditional across all exit paths — a defer skipped by a panic or
// placed in the wrong scope would leak the file on this path while passing the
// success and failure cleanup tests.
func TestDeploy_SelectionsFile_RemovedAfterTimeout(t *testing.T) {
	var capturedPath string
	req := minimalDeployRequest()
	req.TierModels = map[string]string{domain.TierTestSubject: "claude-opus-4-5"}

	d := newDeployer(agentdeploy.Options{
		Timeout: 20 * time.Millisecond,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				capturedPath = args[idx+1]
			}
			// Block until the port's timeout cancels the context.
			<-ctx.Done()
			return nil, nil, 0, ctx.Err()
		},
	})

	// The outer context is generous; the port's own timeout fires first.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := d.Deploy(ctx, req)

	if err == nil {
		t.Fatal("Deploy returned nil error on timeout (want ErrTimedOut for this cleanup test)")
	}
	if capturedPath == "" {
		t.Fatal("--selections path was not captured from CommandRunner; cannot assert file removal")
	}
	if _, statErr := os.Stat(capturedPath); !os.IsNotExist(statErr) {
		t.Errorf("selections file at %q still exists after Deploy returned on timeout; "+
			"cleanup must be unconditional across all exit paths including timeout", capturedPath)
	}
}

// TestDeploy_SelectionsFile_RemovedAfterCancellation asserts that no temporary
// selections file remains on disk when the caller's context is cancelled while
// the tool is running. Cleanup must be unconditional — a leaked file on the
// cancellation path is a defect just as much as on the failure path.
func TestDeploy_SelectionsFile_RemovedAfterCancellation(t *testing.T) {
	var capturedPath string
	req := minimalDeployRequest()
	req.TierModels = map[string]string{domain.TierTestSubject: "claude-opus-4-5"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := newDeployer(agentdeploy.Options{
		Timeout: 10 * time.Second, // much longer than the test will run
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				capturedPath = args[idx+1]
			}
			// Cancel the caller's context now that the path is captured, then
			// block until the combined (port timeout + caller cancel) context fires.
			cancel()
			<-ctx.Done()
			return nil, nil, 0, ctx.Err()
		},
	})

	_, err := d.Deploy(ctx, req)

	if err == nil {
		t.Fatal("Deploy returned nil error when caller context was cancelled (want an error for this cleanup test)")
	}
	if capturedPath == "" {
		t.Fatal("--selections path was not captured from CommandRunner; cannot assert file removal")
	}
	if _, statErr := os.Stat(capturedPath); !os.IsNotExist(statErr) {
		t.Errorf("selections file at %q still exists after Deploy returned on cancellation; "+
			"cleanup must be unconditional across all exit paths including caller cancellation", capturedPath)
	}
}

// =============================================================================
// Cleanup: selections file triggered by InfrastructureAgentIDs (not TierModels)
// =============================================================================
//
// The same defer-based cleanup that removes the selections file on success,
// failure, timeout, and cancellation also covers the code path where
// InfrastructureAgentIDs (not TierModels) triggered the file. These four
// tests verify that exit path explicitly so a refactor of the guard or the
// defer placement cannot regress without turning these tests red.

// TestDeploy_InfraAgentIDs_SelectionsFile_RemovedAfterSuccess asserts that no
// temporary selections file remains on disk after a successful Deploy call
// when InfrastructureAgentIDs (and not TierModels) triggered the file write.
func TestDeploy_InfraAgentIDs_SelectionsFile_RemovedAfterSuccess(t *testing.T) {
	var capturedPath string
	req := minimalDeployRequest()
	req.TierModels = nil
	req.InfrastructureAgentIDs = []string{} // non-nil empty — the only trigger

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				capturedPath = args[idx+1]
			}
			return deploySuccessJSON(agentActionJSON("orchestrator", "/sandbox/.claude/agents/orchestrator.md")), nil, exitSuccess, nil
		},
	})

	_, err := d.Deploy(context.Background(), req)

	if err != nil {
		t.Fatalf("Deploy returned error (want success for this cleanup test): %v", err)
	}
	if capturedPath == "" {
		t.Fatal("--selections path was not captured from CommandRunner; cannot assert file removal")
	}
	if _, statErr := os.Stat(capturedPath); !os.IsNotExist(statErr) {
		t.Errorf("selections file at %q still exists after Deploy returned successfully; "+
			"cleanup must fire on the InfrastructureAgentIDs-triggered path just as "+
			"it does on the TierModels-triggered path", capturedPath)
	}
}

// TestDeploy_InfraAgentIDs_SelectionsFile_RemovedAfterFailure asserts that no
// temporary selections file remains on disk after a failed Deploy call when
// InfrastructureAgentIDs (and not TierModels) triggered the file write.
func TestDeploy_InfraAgentIDs_SelectionsFile_RemovedAfterFailure(t *testing.T) {
	var capturedPath string
	req := minimalDeployRequest()
	req.TierModels = nil
	req.InfrastructureAgentIDs = []string{} // non-nil empty — the only trigger

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				capturedPath = args[idx+1]
			}
			// Simulate a non-zero exit.
			return []byte("error: something went wrong"), []byte("something went wrong"), exitFailure, nil
		},
	})

	_, err := d.Deploy(context.Background(), req)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1 (want failure for this cleanup test)")
	}
	if capturedPath == "" {
		t.Fatal("--selections path was not captured from CommandRunner; cannot assert file removal")
	}
	if _, statErr := os.Stat(capturedPath); !os.IsNotExist(statErr) {
		t.Errorf("selections file at %q still exists after Deploy returned a failure; "+
			"cleanup must fire on the InfrastructureAgentIDs-triggered path just as "+
			"it does on the TierModels-triggered path", capturedPath)
	}
}

// TestDeploy_InfraAgentIDs_SelectionsFile_RemovedAfterTimeout asserts that no
// temporary selections file remains on disk when the port's own timeout fires
// and InfrastructureAgentIDs (and not TierModels) triggered the file write.
func TestDeploy_InfraAgentIDs_SelectionsFile_RemovedAfterTimeout(t *testing.T) {
	var capturedPath string
	req := minimalDeployRequest()
	req.TierModels = nil
	req.InfrastructureAgentIDs = []string{} // non-nil empty — the only trigger

	d := newDeployer(agentdeploy.Options{
		Timeout: 20 * time.Millisecond,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				capturedPath = args[idx+1]
			}
			// Block until the port's timeout cancels the context.
			<-ctx.Done()
			return nil, nil, 0, ctx.Err()
		},
	})

	// The outer context is generous; the port's own timeout fires first.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := d.Deploy(ctx, req)

	if err == nil {
		t.Fatal("Deploy returned nil error on timeout (want ErrTimedOut for this cleanup test)")
	}
	if capturedPath == "" {
		t.Fatal("--selections path was not captured from CommandRunner; cannot assert file removal")
	}
	if _, statErr := os.Stat(capturedPath); !os.IsNotExist(statErr) {
		t.Errorf("selections file at %q still exists after Deploy returned on timeout; "+
			"cleanup must fire on the InfrastructureAgentIDs-triggered path just as "+
			"it does on the TierModels-triggered path", capturedPath)
	}
}

// TestDeploy_InfraAgentIDs_SelectionsFile_RemovedAfterCancellation asserts
// that no temporary selections file remains on disk when the caller's context
// is cancelled and InfrastructureAgentIDs (and not TierModels) triggered the
// file write.
func TestDeploy_InfraAgentIDs_SelectionsFile_RemovedAfterCancellation(t *testing.T) {
	var capturedPath string
	req := minimalDeployRequest()
	req.TierModels = nil
	req.InfrastructureAgentIDs = []string{} // non-nil empty — the only trigger

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := newDeployer(agentdeploy.Options{
		Timeout: 10 * time.Second, // much longer than the test will run
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				capturedPath = args[idx+1]
			}
			// Cancel the caller's context now that the path is captured, then
			// block until the combined (port timeout + caller cancel) context fires.
			cancel()
			<-ctx.Done()
			return nil, nil, 0, ctx.Err()
		},
	})

	_, err := d.Deploy(ctx, req)

	if err == nil {
		t.Fatal("Deploy returned nil error when caller context was cancelled (want an error for this cleanup test)")
	}
	if capturedPath == "" {
		t.Fatal("--selections path was not captured from CommandRunner; cannot assert file removal")
	}
	if _, statErr := os.Stat(capturedPath); !os.IsNotExist(statErr) {
		t.Errorf("selections file at %q still exists after Deploy returned on cancellation; "+
			"cleanup must fire on the InfrastructureAgentIDs-triggered path just as "+
			"it does on the TierModels-triggered path", capturedPath)
	}
}

// =============================================================================
// Infrastructure agent IDs: Deploy method guard and selections file content
// =============================================================================

// TestDeploy_InfrastructureAgentIDsNonNilEmpty_SelectionsFlagPresent asserts
// that a non-nil empty InfrastructureAgentIDs slice causes --selections to
// appear in the argument list even when TierModels is nil. The nil/non-nil
// distinction is the "explicitly none" contract: a non-nil value means the
// caller has specified a selection and the deploy tool must be told about it,
// otherwise it fires an interactive prompt that hangs an automated run.
func TestDeploy_InfrastructureAgentIDsNonNilEmpty_SelectionsFlagPresent(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.TierModels = nil
	req.InfrastructureAgentIDs = []string{} // non-nil empty = explicitly none

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if flagIndex(capturedArgs, "--selections") < 0 {
		t.Errorf("args = %v, want --selections to be present when InfrastructureAgentIDs is non-nil empty "+
			"(explicitly none must write a file to prevent an interactive prompt)", capturedArgs)
	}
}

// TestDeploy_InfrastructureAgentIDsPopulated_SelectionsFlagPresent asserts that
// a populated InfrastructureAgentIDs slice causes --selections to appear in the
// argument list so the deploy tool receives the caller's explicit agent
// selection.
func TestDeploy_InfrastructureAgentIDsPopulated_SelectionsFlagPresent(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.TierModels = nil
	req.InfrastructureAgentIDs = []string{"checkpoint-manager-git", "commit-manager-git"}

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if flagIndex(capturedArgs, "--selections") < 0 {
		t.Errorf("args = %v, want --selections to be present when InfrastructureAgentIDs is populated", capturedArgs)
	}
}

// TestDeploy_NilInfrastructureAgentIDs_NilTierModels_NoSelectionsFlag asserts
// that the selections file is NOT written when both InfrastructureAgentIDs and
// TierModels are nil. Neither field is specified, so no file is needed and no
// --selections flag appears.
func TestDeploy_NilInfrastructureAgentIDs_NilTierModels_NoSelectionsFlag(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.TierModels = nil
	req.InfrastructureAgentIDs = nil // nil = not specified

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if flagIndex(capturedArgs, "--selections") >= 0 {
		t.Errorf("args = %v, want --selections to be absent when both InfrastructureAgentIDs and TierModels are nil "+
			"(neither field specifies anything)", capturedArgs)
	}
}

// TestDeploy_NonNilEmptyInfrastructureAgentIDs_SelectionsFileHasEmptyList
// asserts that a non-nil empty InfrastructureAgentIDs produces
// `infrastructure_agents: []` in the selections file. This is the
// "explicitly none" signal: the deploy tool reads an empty list, not an absent
// key, and does not prompt interactively.
func TestDeploy_NonNilEmptyInfrastructureAgentIDs_SelectionsFileHasEmptyList(t *testing.T) {
	req := minimalDeployRequest()
	req.TierModels = nil
	req.InfrastructureAgentIDs = []string{} // non-nil empty = explicitly none

	var fileContent []byte
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				fileContent, _ = os.ReadFile(args[idx+1])
			}
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if len(fileContent) == 0 {
		t.Fatal("selections file was empty or not readable during the call; cannot assert content")
	}
	s := string(fileContent)
	if !strings.Contains(s, "infrastructure_agents:") {
		t.Errorf("selections file content = %q, want it to contain \"infrastructure_agents:\" key", s)
	}
	// An empty YAML sequence serialises as "infrastructure_agents: []" on a
	// single line — not as a key with no following items.
	if !strings.Contains(s, "infrastructure_agents: []") {
		t.Errorf("selections file content = %q, want \"infrastructure_agents: []\" for a non-nil empty slice "+
			"(explicitly none; the deploy tool must not prompt for the selection)", s)
	}
}

// TestDeploy_PopulatedInfrastructureAgentIDs_SelectionsFileHasAgentIDs asserts
// that a populated InfrastructureAgentIDs slice produces an
// infrastructure_agents key in the selections file with each ID as a list
// item. The deploy tool reads these to pre-answer QInfrastructureAgents.
func TestDeploy_PopulatedInfrastructureAgentIDs_SelectionsFileHasAgentIDs(t *testing.T) {
	req := minimalDeployRequest()
	req.TierModels = nil
	req.InfrastructureAgentIDs = []string{"checkpoint-manager-git", "commit-manager-git"}

	var fileContent []byte
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				fileContent, _ = os.ReadFile(args[idx+1])
			}
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if len(fileContent) == 0 {
		t.Fatal("selections file was empty or not readable during the call; cannot assert content")
	}
	s := string(fileContent)
	for _, want := range []string{
		"infrastructure_agents:",
		"checkpoint-manager-git",
		"commit-manager-git",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("selections file content = %q, want it to contain %q", s, want)
		}
	}
}

// TestDeploy_PopulatedTierModels_WithNonNilEmptyInfraAgentIDs_SelectionsFileHasBothKeys
// asserts that both tier_models and infrastructure_agents appear in the
// selections file when TierModels is populated and InfrastructureAgentIDs is
// non-nil empty. Neither key suppresses the other.
func TestDeploy_PopulatedTierModels_WithNonNilEmptyInfraAgentIDs_SelectionsFileHasBothKeys(t *testing.T) {
	req := minimalDeployRequest()
	req.TierModels = map[string]string{
		domain.TierTestSubject: "claude-opus-4-5",
	}
	req.InfrastructureAgentIDs = []string{} // non-nil empty

	var fileContent []byte
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				fileContent, _ = os.ReadFile(args[idx+1])
			}
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if len(fileContent) == 0 {
		t.Fatal("selections file was empty or not readable during the call; cannot assert content")
	}
	s := string(fileContent)
	for _, want := range []string{"tier_models:", "infrastructure_agents:"} {
		if !strings.Contains(s, want) {
			t.Errorf("selections file content = %q, want it to contain %q "+
				"(both keys must be present when both fields are set)", s, want)
		}
	}
}

// TestDeploy_NilTierModels_PopulatedInfraAgentIDs_SelectionsFileHasOnlyInfraAgentsKey
// asserts that when TierModels is nil and InfrastructureAgentIDs is populated,
// the file contains infrastructure_agents but NOT tier_models. The tier_models
// key is written only when TierModels is non-empty.
func TestDeploy_NilTierModels_PopulatedInfraAgentIDs_SelectionsFileHasOnlyInfraAgentsKey(t *testing.T) {
	req := minimalDeployRequest()
	req.TierModels = nil
	req.InfrastructureAgentIDs = []string{"checkpoint-manager-git"}

	var fileContent []byte
	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--selections")
			if idx >= 0 && idx+1 < len(args) {
				fileContent, _ = os.ReadFile(args[idx+1])
			}
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if len(fileContent) == 0 {
		t.Fatal("selections file was empty or not readable during the call; cannot assert content")
	}
	s := string(fileContent)
	if strings.Contains(s, "tier_models:") {
		t.Errorf("selections file content = %q, must not contain tier_models: when TierModels is nil "+
			"(tier_models is written only when the map is non-empty)", s)
	}
	if !strings.Contains(s, "infrastructure_agents:") {
		t.Errorf("selections file content = %q, want it to contain infrastructure_agents: key", s)
	}
}

// =============================================================================
// DeployRequest.InfrastructureAgentIDs field contract
// =============================================================================

// TestDeployRequest_InfrastructureAgentIDs_NilIsZeroValue asserts that the
// InfrastructureAgentIDs field exists on domain.DeployRequest and that its
// zero value is nil. nil is "not specified" — no selections file key is
// written and the deploy tool may ask its interactive question.
func TestDeployRequest_InfrastructureAgentIDs_NilIsZeroValue(t *testing.T) {
	req := domain.DeployRequest{
		HarnessID:     "claude-code",
		WorkspaceRoot: "/sandbox",
		Workflows:     []string{"brownfield-tdd"},
	}
	if req.InfrastructureAgentIDs != nil {
		t.Errorf("InfrastructureAgentIDs zero value = %v (non-nil), want nil "+
			"(nil means not-specified, which is the natural zero)", req.InfrastructureAgentIDs)
	}
}

// TestDeployRequest_InfrastructureAgentIDs_CanCarryNonNilEmpty asserts that
// the field accepts a non-nil empty slice and preserves its non-nil property.
// Non-nil empty is the "explicitly none" sentinel: deploy no infrastructure
// agents, but write the selections file so the deploy tool receives a
// pre-answer rather than an interactive prompt.
func TestDeployRequest_InfrastructureAgentIDs_CanCarryNonNilEmpty(t *testing.T) {
	req := domain.DeployRequest{
		HarnessID:              "claude-code",
		WorkspaceRoot:          "/sandbox",
		Workflows:              []string{"brownfield-tdd"},
		InfrastructureAgentIDs: []string{},
	}
	if req.InfrastructureAgentIDs == nil {
		t.Error("InfrastructureAgentIDs set to []string{} is nil, want non-nil "+
			"(non-nil empty is the explicitly-none sentinel, distinct from nil which means not-specified)")
	}
	if len(req.InfrastructureAgentIDs) != 0 {
		t.Errorf("InfrastructureAgentIDs len = %d, want 0 "+
			"(non-nil empty means explicitly none, not populated)", len(req.InfrastructureAgentIDs))
	}
}

// TestDeployRequest_InfrastructureAgentIDs_CanCarryPopulatedSlice asserts that
// the field carries a populated slice of infrastructure agent IDs verbatim.
// The values are forwarded to the deploy tool via the selections file's
// infrastructure_agents key.
func TestDeployRequest_InfrastructureAgentIDs_CanCarryPopulatedSlice(t *testing.T) {
	ids := []string{"checkpoint-manager-git", "commit-manager-git"}
	req := domain.DeployRequest{
		HarnessID:              "claude-code",
		WorkspaceRoot:          "/sandbox",
		Workflows:              []string{"brownfield-tdd"},
		InfrastructureAgentIDs: ids,
	}
	if len(req.InfrastructureAgentIDs) != 2 {
		t.Errorf("InfrastructureAgentIDs len = %d, want 2", len(req.InfrastructureAgentIDs))
	}
	if req.InfrastructureAgentIDs[0] != "checkpoint-manager-git" {
		t.Errorf("InfrastructureAgentIDs[0] = %q, want %q",
			req.InfrastructureAgentIDs[0], "checkpoint-manager-git")
	}
	if req.InfrastructureAgentIDs[1] != "commit-manager-git" {
		t.Errorf("InfrastructureAgentIDs[1] = %q, want %q",
			req.InfrastructureAgentIDs[1], "commit-manager-git")
	}
}

// =============================================================================
// T8.1 — Per-run log destination on the deploy path; empty-means-omitted
// =============================================================================

// TestDeploy_LogDirNonEmpty_LogDirFlagPresent asserts that when LogDir is
// non-empty, --log-dir is present in the deploy argument list. The flag is
// what routes the delegate's two sink files (latest.log, history.log) away
// from the shared repository root and into the run's sandbox. Without it,
// every concurrent attempt truncates the same latest.log.
func TestDeploy_LogDirNonEmpty_LogDirFlagPresent(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.LogDir = "/tmp/run-a/ctl/deploy-logs"

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if flagIndex(capturedArgs, "--log-dir") < 0 {
		t.Errorf("args = %v, want --log-dir to be present when LogDir is non-empty "+
			"(needed to redirect the delegate's sink files away from the shared repository root)", capturedArgs)
	}
}

// TestDeploy_LogDirNonEmpty_LogDirFlagCarriesCorrectValue asserts that the
// --log-dir value is the exact path from req.LogDir, not a transformed or
// reconstructed version. The value is what the deployment tool uses as its
// log directory; any transformation here would put logs somewhere else.
func TestDeploy_LogDirNonEmpty_LogDirFlagCarriesCorrectValue(t *testing.T) {
	var capturedArgs []string
	const wantLogDir = "/tmp/run-a/ctl/deploy-logs"
	req := minimalDeployRequest()
	req.LogDir = wantLogDir

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if !containsFlag(capturedArgs, "--log-dir", wantLogDir) {
		t.Errorf("args = %v, want --log-dir %q (the LogDir value passed through verbatim)", capturedArgs, wantLogDir)
	}
}

// TestDeploy_LogDirEmpty_LogDirFlagAbsent asserts the empty-means-omitted
// contract: when LogDir is empty (the zero value), --log-dir must be absent
// from the argument list. An omitted flag lets the deployment tool write to
// its own default location — the correct behaviour for callers that have not
// opted into the per-run isolation.
func TestDeploy_LogDirEmpty_LogDirFlagAbsent(t *testing.T) {
	var capturedArgs []string
	req := minimalDeployRequest()
	req.LogDir = "" // empty = do not override

	d := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	if len(capturedArgs) == 0 {
		t.Fatal("Deploy did not invoke CommandRunner; cannot assert argument absence")
	}
	if flagIndex(capturedArgs, "--log-dir") >= 0 {
		t.Errorf("args = %v, want --log-dir to be absent when LogDir is empty (empty-means-omitted; absent flag lets the tool use its own default)", capturedArgs)
	}
}

// TestDeploy_LogDirEmpty_ArgumentListUnchangedFromBaseline pins the
// backwards-compatibility contract: the argument list produced with LogDir
// explicitly empty must be byte-identical to the list produced when the field
// is absent at all. Any deviation would silently alter every pre-existing
// Deploy call's wire shape — an implicit contract break that surfaces only
// when the delegate rejects the new argument.
func TestDeploy_LogDirEmpty_ArgumentListUnchangedFromBaseline(t *testing.T) {
	req := minimalDeployRequest()

	// Baseline: no LogDir field set (zero value, same as absent).
	var baselineArgs []string
	baselineDeployer := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		MosaicRoot:     "/opt/mosaic",
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			baselineArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})
	_, _ = baselineDeployer.Deploy(context.Background(), req)

	// Same request but with LogDir set to the empty string explicitly.
	// Must produce the identical argument slice.
	reqWithEmpty := req
	reqWithEmpty.LogDir = "" // explicit empty — must not alter the argument list
	var withEmptyArgs []string
	withEmptyDeployer := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		MosaicRoot:     "/opt/mosaic",
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			withEmptyArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})
	_, _ = withEmptyDeployer.Deploy(context.Background(), reqWithEmpty)

	if len(baselineArgs) != len(withEmptyArgs) {
		t.Errorf("arg count: baseline = %d, with empty LogDir = %d; want byte-identical argument lists (empty LogDir must not add any argument)",
			len(baselineArgs), len(withEmptyArgs))
		return
	}
	for i := range baselineArgs {
		if baselineArgs[i] != withEmptyArgs[i] {
			t.Errorf("args[%d]: baseline = %q, with empty LogDir = %q; want byte-identical argument lists",
				i, baselineArgs[i], withEmptyArgs[i])
		}
	}
}

// =============================================================================
// T8.3 — Deploy invocation writes no file under the shared repository root
//         outside the run's own location
// =============================================================================

// TestDeploy_LogDirFlag_NotUnderSharedMosaicRoot asserts the audit property:
// when the per-run log destination is derived from a sandbox (not from
// opts.MosaicRoot), the --log-dir argument value is not under the shared
// repository root. This is the structural guarantee that the deploy path
// writes its sink files only within the run's own sandbox.
//
// The test fails in the RED phase because buildDeployArgs does not yet emit
// --log-dir, so the flag is absent and the deployment tool would fall back to
// writing its default location (<mosaicRoot>/MosaicDeploy/logs) on every call.
func TestDeploy_LogDirFlag_NotUnderSharedMosaicRoot(t *testing.T) {
	const sharedMosaicRoot = "/opt/mosaic-shared"
	const sandboxControlDir = "/tmp/runs/run-a/ctl"

	var capturedArgs []string
	req := minimalDeployRequest()
	// LogDir is derived from a sandbox: under the sandbox's control dir,
	// not under sharedMosaicRoot.
	req.LogDir = sandboxControlDir + "/deploy-logs"

	d := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: "mosaic-deploy",
		MosaicRoot:     sharedMosaicRoot,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return deploySuccessJSON(), nil, exitSuccess, nil
		},
	})

	_, _ = d.Deploy(context.Background(), req)

	// First: --log-dir must be present — without it the delegate writes to
	// <sharedMosaicRoot>/MosaicDeploy/logs, which is a shared write.
	idx := flagIndex(capturedArgs, "--log-dir")
	if idx < 0 {
		t.Errorf("args = %v, want --log-dir to be present when LogDir is non-empty "+
			"(its absence means the delegate writes to the shared root at %q)", capturedArgs, sharedMosaicRoot)
		return
	}

	// Second: the --log-dir value must not be under the shared mosaic root.
	logDir := capturedArgs[idx+1]
	// Normalise to forward slashes for cross-platform comparison.
	cleanLogDir := filepath.ToSlash(filepath.Clean(logDir))
	cleanSharedRoot := filepath.ToSlash(filepath.Clean(sharedMosaicRoot))
	if strings.HasPrefix(cleanLogDir, cleanSharedRoot+"/") || cleanLogDir == cleanSharedRoot {
		t.Errorf("--log-dir = %q is under the shared mosaic root %q; the deploy log destination must be within the run's sandbox so concurrent calls cannot overwrite each other's logs", logDir, sharedMosaicRoot)
	}
}

// =============================================================================
// T8.4 — Render path is unaffected: no log destination, no run logging
// =============================================================================

// TestRender_HasNoLogDirFlag asserts that the render subcommand argument list
// never contains --log-dir under any circumstances. The render path triggers
// no run logging in the delegate, so it needs no destination override and
// adding one would be a contract break against the render subcommand's flag
// surface.
func TestRender_HasNoLogDirFlag(t *testing.T) {
	var capturedArgs []string

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), minimalRequest())

	if flagIndex(capturedArgs, "--log-dir") >= 0 {
		t.Errorf("render args = %v, want --log-dir to be absent from the render subcommand argument list "+
			"(render triggers no run logging and must not receive a log destination override)", capturedArgs)
	}
}

// TestRender_WithMosaicRoot_HasNoLogDirFlag asserts that --log-dir remains
// absent from render args even when MosaicRoot is set. The deploy path and the
// render path share opts.MosaicRoot, so an incorrect implementation that
// emitted --log-dir based on opts would affect both — this test catches that.
func TestRender_WithMosaicRoot_HasNoLogDirFlag(t *testing.T) {
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

	if flagIndex(capturedArgs, "--log-dir") >= 0 {
		t.Errorf("render args = %v, want --log-dir to be absent from render args even when MosaicRoot is set "+
			"(the flag is a deploy-path-only override; it must not appear on the render path)", capturedArgs)
	}
}

// TestRender_FirstArgIsRender_NotDeploy asserts that the render path still
// routes to the "render" subcommand and has not been accidentally changed
// to "deploy" by stage changes. This is a regression guard: a change that
// routes render calls to deploy would produce wrong output structure and
// succeed with garbage that nothing downstream could use.
func TestRender_FirstArgIsRender_NotDeploy(t *testing.T) {
	var capturedArgs []string

	deployer := newDeployer(agentdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(successJSON), nil, exitSuccess, nil
		},
	})

	_, _ = deployer.Render(context.Background(), minimalRequest())

	if len(capturedArgs) == 0 {
		t.Fatal("Render passed no arguments to CommandRunner; cannot assert subcommand name")
	}
	if capturedArgs[0] != "render" {
		t.Errorf("render args[0] = %q, want %q (must still be the render subcommand after stage changes)", capturedArgs[0], "render")
	}
}

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
