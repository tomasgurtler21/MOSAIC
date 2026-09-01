// Package harness — ClaudeCodeAdapter implementation.
//
// ClaudeCodeAdapter maps Runner's domain vocabulary onto mosaic-common/harness,
// which owns the actual subprocess spawning, CLI argument construction and
// output-envelope parsing. This file is a thin mapping wrapper: it converts
// domain.AgentReference/ProtocolRequest into the shared package's neutral
// types, delegates the spawn, and converts the result back.
package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/domain"
)

// Sentinel errors for ClaudeCodeAdapter failure modes, aliased onto the
// shared package's sentinels so errors.Is at existing call sites keeps
// working unchanged.
var (
	// ErrExecutableNotFound is returned when the Claude Code CLI binary cannot
	// be found or executed.
	ErrExecutableNotFound = commonharness.ErrExecutableNotFound

	// ErrNonZeroExit is returned when the CLI process exits with a non-zero
	// status. The wrapped error includes the exit code and captured stderr.
	ErrNonZeroExit = commonharness.ErrNonZeroExit

	// ErrTimeout is returned when the invocation exceeds the configured timeout.
	// The subprocess is killed before this error is returned.
	ErrTimeout = commonharness.ErrTimeout

	// ErrEmptyResponse is returned when the CLI produces no stdout output.
	ErrEmptyResponse = commonharness.ErrEmptyResponse

	// ErrMalformedJSON is returned when the CLI output is not valid JSON at all.
	ErrMalformedJSON = commonharness.ErrMalformedJSON

	// ErrMalformedOutput is returned when the CLI output is valid JSON but the
	// Communication Protocol response cannot be located or parsed within it.
	ErrMalformedOutput = commonharness.ErrProtocolNotExtractable
)

// ClaudeCodeAdapter implements domain.HarnessAdapter by delegating to
// mosaic-common/harness for every Invoke call.
//
// For InvocationOrdinary: uses --append-system-prompt-file with the agent's
// definition file, keeping the CLI's default system prompt (including <env>).
//
// For InvocationOrchestrator: uses --agent <identifier> to launch the
// orchestrator as the primary/main thread with native subagent-spawning
// capability. Because --agent fully replaces the default system prompt
// (losing the CLI's <env> block), the shared package synthesizes an
// equivalent <env> block and prepends it to the -p prompt content.
//
// Both invocation kinds read the agent's deployed definition file to extract
// the tools frontmatter field, then use --permission-mode dontAsk with
// --allowedTools entries derived from that field. An agent with a missing or
// empty tools field is rejected before any subprocess is spawned (FR-10).
// --dangerously-skip-permissions is never used.
type ClaudeCodeAdapter struct {
	executablePath string
	timeout        time.Duration
	spawner        commonharness.Spawner
	sink           commonharness.Sink
	logger         domain.DebugLogger
}

// ExecutablePath implements domain.ExecutableRevealer. It returns the
// executable path or command name this adapter was constructed with.
func (a *ClaudeCodeAdapter) ExecutablePath() string { return a.executablePath }

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
	sink := &debugLoggerSink{logger: logger}
	spawner := commonharness.NewClaudeCode(executablePath,
		commonharness.WithTimeout(timeout),
		commonharness.WithSink(sink),
	)
	return &ClaudeCodeAdapter{executablePath: executablePath, timeout: timeout, spawner: spawner, sink: sink, logger: logger}
}

// Invoke implements domain.HarnessAdapter.
//
// It maps the request to the shared package's SpawnRequest, delegates the
// subprocess spawn and output parsing, and maps the result back to a
// domain.ProtocolResponse.
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
	a.logger.Log(domain.EventHarnessInvokeStart, "dispatching invocation",
		domain.F("agent", request.AgentInstanceID),
		domain.F("kind", string(agent.InvocationKind)),
	)

	// FR-10: extract tools from the agent's deployed definition file before
	// building the spawn request. An agent with missing or empty tools is
	// rejected here, before any subprocess is spawned.
	derivedTools, err := commonharness.ExtractClaudeCodeTools(agent.DefinitionPath)
	if err != nil {
		wrapped := fmt.Errorf("agent %q (%s): %w", agent.Identifier, agent.DefinitionPath, err)
		a.logger.Log(domain.EventHarnessInvokeError, wrapped.Error(),
			domain.F("agent", request.AgentInstanceID),
		)
		return domain.ProtocolResponse{}, wrapped
	}

	reqBytes, err := MarshalRequest(request)
	if err != nil {
		return domain.ProtocolResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	spawnReq := commonharness.SpawnRequest{
		Agent: commonharness.AgentRef{
			Identifier:     agent.Identifier,
			DefinitionPath: agent.DefinitionPath,
			Kind:           commonharness.InvocationKind(agent.InvocationKind),
		},
		Prompt:       string(reqBytes),
		OutputFormat: "json",
		DerivedTools: derivedTools,
	}

	resp, err := a.spawner.Spawn(ctx, spawnReq)
	if err != nil {
		// ErrMalformedJSON is only ever returned once the shared package's
		// envelope-array parse has failed and its own recovery attempt has
		// also failed; that is exactly the "parse failed" condition Runner's
		// debug log distinguishes.
		//
		// ErrProtocolNotExtractable (aliased here as ErrMalformedOutput) means
		// the envelope was valid but no protocol response could be located
		// within it. Both conditions mean "harness output could not be turned
		// into a protocol response", so both log under the same event.
		switch {
		case errors.Is(err, commonharness.ErrMalformedJSON):
			a.logger.Log(domain.EventHarnessParseFailed, string(resp.Stdout),
				domain.F("agent", request.AgentInstanceID),
			)
		case errors.Is(err, commonharness.ErrProtocolNotExtractable):
			a.logger.Log(domain.EventHarnessParseFailed, string(resp.Stdout),
				domain.F("agent", request.AgentInstanceID),
			)
		}
		// A missing or non-executable binary is recoverable via the override
		// screen. Wrap it in a domain.HarnessLaunchError so the TUI can
		// distinguish it from every other failure class by error identity.
		if errors.Is(err, commonharness.ErrExecutableNotFound) {
			launchErr := &domain.HarnessLaunchError{
				Harness:    commonharness.HarnessIDClaudeCode,
				Executable: a.executablePath,
				Err:        err,
			}
			a.logger.Log(domain.EventHarnessInvokeError, launchErr.Error(),
				domain.F("agent", request.AgentInstanceID),
			)
			return domain.ProtocolResponse{}, launchErr
		}
		// The shared package distinguishes parent-context cancellation from
		// adapter timeout via ErrCancelled. Runner's existing call sites and
		// tests expect ctx.Err() on cancellation, so translate it back here.
		if ctx.Err() != nil {
			a.logger.Log(domain.EventHarnessInvokeError, err.Error(),
				domain.F("agent", request.AgentInstanceID),
			)
			return domain.ProtocolResponse{}, ctx.Err()
		}
		a.logger.Log(domain.EventHarnessInvokeError, err.Error(),
			domain.F("agent", request.AgentInstanceID),
		)
		return domain.ProtocolResponse{}, err
	}

	// A successful Spawn whose raw stdout does not itself parse as a normal
	// CLI envelope shape means the shared package's defensive recovery path
	// produced this result: log it as such.
	if !isNormalEnvelope(resp.Stdout) {
		a.logger.Log(domain.EventHarnessParseRecovered,
			"envelope array parse failed; direct protocol response recovered",
			domain.F("agent", request.AgentInstanceID),
		)
	}

	var protoResp domain.ProtocolResponse
	if uErr := json.Unmarshal(resp.Protocol, &protoResp); uErr != nil {
		wrapped := fmt.Errorf("%w: response decode failed", ErrMalformedOutput)
		a.logger.Log(domain.EventHarnessParseFailed, string(resp.Protocol),
			domain.F("agent", request.AgentInstanceID),
		)
		a.logger.Log(domain.EventHarnessInvokeError, wrapped.Error(),
			domain.F("agent", request.AgentInstanceID),
		)
		return domain.ProtocolResponse{}, wrapped
	}

	a.logger.Log(domain.EventHarnessInvokeOK, "invocation succeeded",
		domain.F("agent", request.AgentInstanceID),
		domain.F("status", string(protoResp.StatusCode)),
	)
	return protoResp, nil
}

// InvokeRaw implements domain.RawInvoker.
//
// It performs the same steps as Invoke up through envelope parsing but stops
// before ExtractProtocolJSON, so non-protocol replies (consultation responses,
// routing instructions) are returned as plain text without being rejected.
//
// Error returns follow the same sentinel taxonomy as Invoke. On context
// cancellation, returns ctx.Err().
func (a *ClaudeCodeAdapter) InvokeRaw(ctx context.Context, agent domain.AgentReference, payload []byte) ([]byte, error) {
	a.logger.Log(domain.EventHarnessInvokeStart, "dispatching raw invocation",
		domain.F("agent", agent.Identifier),
		domain.F("kind", string(agent.InvocationKind)),
	)

	// FR-10: extract tools from the agent's deployed definition file before
	// building the spawn request. An agent with missing or empty tools is
	// rejected here, before any subprocess is spawned.
	derivedTools, err := commonharness.ExtractClaudeCodeTools(agent.DefinitionPath)
	if err != nil {
		wrapped := fmt.Errorf("agent %q (%s): %w", agent.Identifier, agent.DefinitionPath, err)
		a.logger.Log(domain.EventHarnessInvokeError, wrapped.Error(),
			domain.F("agent", agent.Identifier),
		)
		return nil, wrapped
	}

	spawnReq := commonharness.SpawnRequest{
		Agent: commonharness.AgentRef{
			Identifier:     agent.Identifier,
			DefinitionPath: agent.DefinitionPath,
			Kind:           commonharness.InvocationKind(agent.InvocationKind),
		},
		Prompt:       string(payload),
		OutputFormat: "json",
		DerivedTools: derivedTools,
	}

	cmd, err := commonharness.ResolveExecutable(a.executablePath)
	if err != nil {
		a.logger.Log(domain.EventHarnessInvokeError, err.Error(),
			domain.F("agent", agent.Identifier),
		)
		return nil, err
	}

	args, stdin, err := commonharness.BuildArgs(spawnReq)
	if err != nil {
		a.logger.Log(domain.EventHarnessInvokeError, err.Error(),
			domain.F("agent", agent.Identifier),
		)
		return nil, err
	}

	resp, err := commonharness.Run(ctx, cmd, args, commonharness.RunOptions{
		WorkingDir: spawnReq.WorkingDir,
		Env:        spawnReq.Env,
		Stdin:      stdin,
		Timeout:    a.timeout,
		Sink:       a.sink,
	})
	if err != nil {
		// A missing or non-executable binary is recoverable via the override
		// screen. Wrap it in a domain.HarnessLaunchError so the TUI can
		// distinguish it from every other failure class by error identity.
		if errors.Is(err, commonharness.ErrExecutableNotFound) {
			launchErr := &domain.HarnessLaunchError{
				Harness:    commonharness.HarnessIDClaudeCode,
				Executable: a.executablePath,
				Err:        err,
			}
			a.logger.Log(domain.EventHarnessInvokeError, launchErr.Error(),
				domain.F("agent", agent.Identifier),
			)
			return nil, launchErr
		}
		if ctx.Err() != nil {
			a.logger.Log(domain.EventHarnessInvokeError, err.Error(),
				domain.F("agent", agent.Identifier),
			)
			return nil, ctx.Err()
		}
		a.logger.Log(domain.EventHarnessInvokeError, err.Error(),
			domain.F("agent", agent.Identifier),
		)
		return nil, err
	}

	text, err := commonharness.ParseClaudeCodeEnvelope(resp.Stdout)
	if err != nil {
		a.logger.Log(domain.EventHarnessInvokeError, err.Error(),
			domain.F("agent", agent.Identifier),
		)
		return nil, err
	}

	a.logger.Log(domain.EventHarnessStdout, text,
		domain.F("agent", agent.Identifier),
	)
	a.logger.Log(domain.EventHarnessInvokeOK, "raw invocation succeeded",
		domain.F("agent", agent.Identifier),
	)
	return []byte(text), nil
}

// isNormalEnvelope reports whether data is shaped like one of the CLI's
// normal --output-format json envelopes: either a top-level JSON array, or a
// single result-bearing object (Claude Code's actual envelope shape:
// {"type":"result", "result": "...", ...}). Used only to classify a
// successful Spawn for the debug log — recognising the shape is not the same
// as parsing it, which stays exactly once in mosaic-common/harness.
//
// This must stay narrow: a bare protocol response object, or a protocol
// response embedded in surrounding CLI noise, is a genuine recovery by the
// shared package's defensive path and must not be classified as normal here.
// Only an object carrying the envelope's own result-bearing fields
// ("type":"result" with a "result" field) qualifies.
func isNormalEnvelope(data []byte) bool {
	var arr []json.RawMessage
	if json.Unmarshal(data, &arr) == nil {
		return true
	}
	var obj struct {
		Type   string `json:"type"`
		Result string `json:"result"`
	}
	if json.Unmarshal(data, &obj) != nil {
		return false
	}
	return obj.Type == "result" && obj.Result != ""
}

// debugLoggerSink adapts domain.DebugLogger to commonharness.Sink, so the
// shared package's diagnostic events (raw stdout, raw stderr, parse
// recovery/failure) continue to reach Runner's debug log.
type debugLoggerSink struct {
	logger domain.DebugLogger
}

// Log implements commonharness.Sink.
func (s *debugLoggerSink) Log(ev commonharness.Event) {
	event := sinkEventName(ev.Name)
	fields := make([]domain.DebugField, 0, len(ev.Fields))
	for _, f := range ev.Fields {
		fields = append(fields, domain.F(f.Key, f.Value))
	}
	s.logger.Log(event, ev.Message, fields...)
}

// sinkEventName maps a mosaic-common/harness event name to Runner's closed
// debug-log event vocabulary. An unrecognised event name is passed through
// unchanged rather than dropped, so a future shared-package event still
// leaves a trace even if this mapping has not been extended for it yet.
func sinkEventName(name string) string {
	switch name {
	case "spawn.stdout":
		return domain.EventHarnessStdout
	case "spawn.stderr":
		return domain.EventHarnessStderr
	case "spawn.timeout", "spawn.error":
		return domain.EventHarnessInvokeError
	default:
		return name
	}
}
