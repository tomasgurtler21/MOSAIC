// Package harness — ClaudeCodeAdapter implementation.
//
// ClaudeCodeAdapter spawns the Claude Code CLI as a subprocess for each
// Invoke call, parses the --output-format json response envelope, and
// extracts the Communication Protocol v1.8 response from the result text.
//
// Fragility note: the --output-format json envelope structure is inferred
// from observed CLI behavior and is not formally documented. The adapter
// parses defensively and returns ErrMalformedOutput when the structure does
// not match expectations.
package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"mosaic-run/internal/domain"
)

// Sentinel errors for ClaudeCodeAdapter failure modes.
// Tests use errors.Is to distinguish them, following the pattern in
// Tools/Deployment/internal/harness/external/external.go.
var (
	// ErrExecutableNotFound is returned when the Claude Code CLI binary cannot
	// be found or executed.
	ErrExecutableNotFound = errors.New("claude code: executable not found")

	// ErrNonZeroExit is returned when the CLI process exits with a non-zero
	// status. The wrapped error includes the exit code and captured stderr.
	ErrNonZeroExit = errors.New("claude code: non-zero exit")

	// ErrTimeout is returned when the invocation exceeds the configured timeout.
	// The subprocess is killed before this error is returned.
	ErrTimeout = errors.New("claude code: invocation timed out")

	// ErrEmptyResponse is returned when the CLI produces no stdout output.
	ErrEmptyResponse = errors.New("claude code: empty response")

	// ErrMalformedJSON is returned when the CLI output is not valid JSON at all.
	ErrMalformedJSON = errors.New("claude code: output is not valid JSON")

	// ErrMalformedOutput is returned when the CLI output is valid JSON but the
	// Communication Protocol response cannot be located or parsed within it.
	ErrMalformedOutput = errors.New("claude code: protocol response not extractable from output")
)

// ClaudeCodeAdapter implements domain.HarnessAdapter by spawning the Claude
// Code CLI as a subprocess for each Invoke call.
//
// For InvocationOrdinary: uses --append-system-prompt-file with the agent's
// definition file, keeping the CLI's default system prompt (including <env>).
//
// For InvocationOrchestrator: uses --agent <identifier> to launch the
// orchestrator as the primary/main thread with native subagent-spawning
// capability. Because --agent fully replaces the default system prompt
// (losing the CLI's <env> block), the adapter synthesizes an equivalent
// <env> block and prepends it to the -p prompt content.
//
// Both invocation kinds use --permission-mode auto and --output-format json.
// --dangerously-skip-permissions is never used.
type ClaudeCodeAdapter struct {
	executablePath string
	timeout        time.Duration
	logger         domain.DebugLogger
}

// NewClaudeCodeAdapter creates a ClaudeCodeAdapter with debug logging disabled.
// UNCHANGED SIGNATURE — existing call sites and tests keep compiling.
// Equivalent to NewClaudeCodeAdapterWithLogger(executablePath, timeout, nil).
//
// executablePath is the path/name of the Claude Code CLI binary (e.g. "claude").
// timeout is the maximum duration for a single Invoke call; zero means 30 minutes.
func NewClaudeCodeAdapter(executablePath string, timeout time.Duration) *ClaudeCodeAdapter {
	return NewClaudeCodeAdapterWithLogger(executablePath, timeout, nil)
}

// NewClaudeCodeAdapterWithLogger creates a ClaudeCodeAdapter that records every
// invocation's raw stdout, raw stderr and outcome to the debug log.
//
// A nil logger is normalised to domain.NopDebugLogger here, so the adapter's
// logger field is never nil and Invoke never nil-checks it.
func NewClaudeCodeAdapterWithLogger(executablePath string, timeout time.Duration, logger domain.DebugLogger) *ClaudeCodeAdapter {
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	if logger == nil {
		logger = domain.NopDebugLogger{}
	}
	return &ClaudeCodeAdapter{
		executablePath: executablePath,
		timeout:        timeout,
		logger:         logger,
	}
}

// Invoke implements domain.HarnessAdapter.
//
// It spawns the Claude Code CLI as a subprocess, blocks until it completes or
// is cancelled, and parses the --output-format json output for the agent's
// Communication Protocol v1.8 response.
//
// Error returns use the sentinel error taxonomy:
//   - ErrExecutableNotFound: claude binary not found or not executable.
//   - ErrNonZeroExit: CLI exited non-zero (wraps stderr content).
//   - ErrTimeout: invocation exceeded the configured timeout.
//   - ErrEmptyResponse: CLI produced no stdout output.
//   - ErrMalformedJSON: CLI output is not valid JSON.
//   - ErrMalformedOutput: valid JSON envelope but protocol response not extractable.
//
// On context cancellation, terminates the subprocess and returns ctx.Err().
func (a *ClaudeCodeAdapter) Invoke(ctx context.Context, agent domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	// Log invocation start with the agent identifier and invocation kind.
	a.logger.Log(domain.EventHarnessInvokeStart, "dispatching invocation",
		domain.F("agent", request.AgentInstanceID),
		domain.F("kind", string(agent.InvocationKind)),
	)

	// Serialize the request.
	reqBytes, err := MarshalRequest(request)
	if err != nil {
		return domain.ProtocolResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	// Build prompt content; synthesize <env> block for orchestrator invocations.
	promptContent := string(reqBytes)
	if agent.InvocationKind == domain.InvocationOrchestrator {
		promptContent = buildEnvBlock() + "\n" + promptContent
	}

	// Build CLI argument list. --dangerously-skip-permissions is never included.
	var args []string
	switch agent.InvocationKind {
	case domain.InvocationOrchestrator:
		args = []string{
			"--agent", agent.Identifier,
			"-p", promptContent,
			"--output-format", "json",
			"--permission-mode", "auto",
			"--no-session-persistence",
		}
	default: // InvocationOrdinary
		args = []string{
			"-p", promptContent,
			"--append-system-prompt-file", agent.DefinitionPath,
			"--output-format", "json",
			"--permission-mode", "auto",
			"--no-session-persistence",
		}
	}

	// Build exec.Cmd, handling Windows .cmd/.bat shims via "cmd /c".
	cmd := buildCommand(a.executablePath, args)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start the subprocess.
	if err := cmd.Start(); err != nil {
		return domain.ProtocolResponse{}, fmt.Errorf("%w: %v", ErrExecutableNotFound, err)
	}

	// Wait for the process to complete, or for context cancellation/timeout.
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	var waitErr error
	select {
	case <-ctx.Done():
		// Parent context cancelled: kill and surface ctx.Err().
		cmd.Process.Kill() //nolint:errcheck
		<-waitCh
		return domain.ProtocolResponse{}, ctx.Err()
	case <-time.After(a.timeout):
		// Adapter timeout: kill and return ErrTimeout.
		cmd.Process.Kill() //nolint:errcheck
		<-waitCh
		timeoutErr := fmt.Errorf("%w", ErrTimeout)
		a.logger.Log(domain.EventHarnessInvokeError, timeoutErr.Error(),
			domain.F("agent", request.AgentInstanceID),
		)
		return domain.ProtocolResponse{}, timeoutErr
	case waitErr = <-waitCh:
	}

	// Process exited via waitCh. Log raw stdout and stderr in full.
	a.logger.Log(domain.EventHarnessStdout, stdout.String(),
		domain.F("agent", request.AgentInstanceID),
		domain.F("bytes", strconv.Itoa(stdout.Len())),
	)
	a.logger.Log(domain.EventHarnessStderr, stderr.String(),
		domain.F("agent", request.AgentInstanceID),
		domain.F("bytes", strconv.Itoa(stderr.Len())),
	)

	// Check for non-zero exit status.
	if waitErr != nil {
		var exitErr *exec.ExitError
		var exitErrResult error
		if errors.As(waitErr, &exitErr) {
			stderrContent := strings.TrimSpace(stderr.String())
			if stderrContent != "" {
				exitErrResult = fmt.Errorf("%w: exit status %d: %s", ErrNonZeroExit, exitErr.ExitCode(), stderrContent)
			} else {
				exitErrResult = fmt.Errorf("%w: exit status %d", ErrNonZeroExit, exitErr.ExitCode())
			}
		} else {
			exitErrResult = fmt.Errorf("%w: %v", ErrNonZeroExit, waitErr)
		}
		a.logger.Log(domain.EventHarnessInvokeError, exitErrResult.Error(),
			domain.F("agent", request.AgentInstanceID),
		)
		return domain.ProtocolResponse{}, exitErrResult
	}

	// Parse the --output-format json envelope and extract the protocol response.
	resp, parseErr := parseEnvelopeWithLogger(stdout.Bytes(), a.logger)
	if parseErr != nil {
		a.logger.Log(domain.EventHarnessInvokeError, parseErr.Error(),
			domain.F("agent", request.AgentInstanceID),
		)
		return domain.ProtocolResponse{}, parseErr
	}
	a.logger.Log(domain.EventHarnessInvokeOK, "invocation succeeded",
		domain.F("agent", request.AgentInstanceID),
		domain.F("status", string(resp.StatusCode)),
	)
	return resp, nil
}

// buildCommand constructs an exec.Cmd for the given executable and args.
// On Windows, .cmd and .bat executables are wrapped with "cmd /c" to handle
// npm-installed CLI shims.
func buildCommand(execPath string, args []string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		lower := strings.ToLower(execPath)
		if strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat") {
			cmdArgs := append([]string{"/c", execPath}, args...)
			return exec.Command("cmd", cmdArgs...)
		}
	}
	return exec.Command(execPath, args...)
}

// buildEnvBlock synthesizes the <env> block prepended to orchestrator prompts.
// This compensates for --agent mode fully replacing the CLI's default system
// prompt (which normally includes the CLI's own <env> block).
func buildEnvBlock() string {
	cwd, _ := os.Getwd()
	date := time.Now().Format("2006-01-02")
	return fmt.Sprintf("<env>\nWorking directory: %s\nPlatform: %s\nCurrent date: %s\n</env>", cwd, runtime.GOOS, date)
}

// cliEnvelopeEntry is one element of the --output-format json array.
type cliEnvelopeEntry struct {
	Type   string `json:"type"`
	Result string `json:"result"`
}

// parseEnvelope extracts a domain.ProtocolResponse from the CLI's JSON output.
// Its signature is unchanged from the original; it delegates to
// parseEnvelopeWithLogger with the no-op logger so the in-package unit tests
// continue to compile and run unchanged.
//
// Extraction strategy:
//  1. Empty output → ErrEmptyResponse.
//  2. Try to unmarshal as a JSON array envelope (the normal --output-format json shape):
//     a. On success → scan backward for the last "result" entry → extract via
//        extractProtocolResponse → return on success, or ErrMalformedOutput on failure.
//     b. On failure → attempt recovery by calling extractProtocolResponse on the raw
//        output text directly. If recovery succeeds, return the response.
//        If recovery also fails → ErrMalformedJSON wrapping the raw CLI output text
//        (trimmed, capped at 2000 characters with a visible truncation indicator when
//        exceeded). The Go unmarshal error string and internal type names are never
//        included in this message.
//  3. No "result" type entry in the successfully-parsed array → ErrMalformedOutput.
//  4. Protocol response not parseable from the result text → ErrMalformedOutput.
func parseEnvelope(data []byte) (domain.ProtocolResponse, error) {
	return parseEnvelopeWithLogger(data, domain.NopDebugLogger{})
}

// parseEnvelopeWithLogger is the implementation of parseEnvelope that also emits
// EventHarnessParseFailed and EventHarnessParseRecovered to the supplied logger.
// Invoke calls this variant directly so recovery and failure events reach the log.
func parseEnvelopeWithLogger(data []byte, logger domain.DebugLogger) (domain.ProtocolResponse, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return domain.ProtocolResponse{}, ErrEmptyResponse
	}

	var entries []cliEnvelopeEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		// Array unmarshal failed. Attempt recovery: try extracting a valid
		// protocol response directly from the raw output text.
		rawText := string(data)
		if resp, recErr := extractProtocolResponse(strings.TrimSpace(rawText)); recErr == nil {
			logger.Log(domain.EventHarnessParseRecovered, "envelope array parse failed; direct protocol response recovered")
			return resp, nil
		}

		// Recovery also failed. Log the full raw output for diagnosis, then
		// return ErrMalformedJSON with the raw output text instead of the Go
		// unmarshal error (which would expose the internal type name
		// cliEnvelopeEntry). Trim and cap for TUI readability.
		logger.Log(domain.EventHarnessParseFailed, rawText)
		const rawCap = 2000
		trimmed := strings.TrimSpace(rawText)
		if len(trimmed) > rawCap {
			total := len(trimmed)
			trimmed = trimmed[:rawCap] + fmt.Sprintf("... [truncated, %d bytes total]", total)
		}
		return domain.ProtocolResponse{}, fmt.Errorf("%w: %s", ErrMalformedJSON, trimmed)
	}

	// Scan backward for the last "result" type entry.
	resultText := ""
	found := false
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type == "result" {
			resultText = entries[i].Result
			found = true
			break
		}
	}
	if !found {
		return domain.ProtocolResponse{}, fmt.Errorf("%w: no result entry in CLI envelope", ErrMalformedOutput)
	}

	// Extract the Communication Protocol response from the result text.
	// Try direct unmarshal first (fast path for clean responses where the
	// result string IS the protocol JSON). If that fails, scan for an embedded
	// JSON object containing the required fields.
	resp, err := extractProtocolResponse(strings.TrimSpace(resultText))
	if err != nil {
		return domain.ProtocolResponse{}, err
	}
	return resp, nil
}

// extractProtocolResponse locates a Communication Protocol response within text.
// It first attempts a direct unmarshal of the full text, then scans for a JSON
// object containing both "status_code" and "agent_instance_id" fields.
func extractProtocolResponse(text string) (domain.ProtocolResponse, error) {
	// Fast path: the result text is exactly the protocol JSON.
	if resp, err := UnmarshalResponse([]byte(text)); err == nil {
		if resp.StatusCode != "" && resp.AgentInstanceID != "" {
			return resp, nil
		}
	}

	// Scan path: find an embedded JSON object within surrounding text.
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		// Try increasing substrings starting at this '{'.
		for j := i + 1; j <= len(text); j++ {
			if text[j-1] != '}' {
				continue
			}
			candidate := text[i:j]
			var resp domain.ProtocolResponse
			if err := json.Unmarshal([]byte(candidate), &resp); err == nil {
				if resp.StatusCode != "" && resp.AgentInstanceID != "" {
					return resp, nil
				}
			}
		}
	}

	return domain.ProtocolResponse{}, fmt.Errorf("%w: no Communication Protocol response found in result text", ErrMalformedOutput)
}
