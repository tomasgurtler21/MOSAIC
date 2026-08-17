// Package harness — OpenCodeAdapter implementation.
//
// OpenCodeAdapter maps Runner's domain vocabulary onto mosaic-common/harness's
// OpenCode spawner, mirroring ClaudeCodeAdapter's thin mapping-wrapper shape:
// it converts domain.AgentReference/ProtocolRequest into the shared package's
// neutral types, delegates the spawn, and converts the result back.
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

// Sentinel errors specific to OpenCodeAdapter, aliased onto the shared
// module's so errors.Is works across the boundary. The taxonomy
// ClaudeCodeAdapter exposes (ErrExecutableNotFound, ErrNonZeroExit,
// ErrTimeout, ErrEmptyResponse, ErrMalformedJSON, ErrMalformedOutput) is
// package-level and shared unchanged; these two are additional.
var (
	// ErrStreamError is returned when the harness's own event stream
	// reported the run failed. It has no Claude Code counterpart because
	// that harness reports a failed run through its exit code.
	ErrStreamError = commonharness.ErrOpenCodeStreamError

	// ErrStreamIncomplete is returned when the event stream ended without a
	// terminal event, leaving the run's outcome unknown.
	ErrStreamIncomplete = commonharness.ErrOpenCodeStreamIncomplete
)

// OpenCodeAdapter implements domain.HarnessAdapter by delegating to
// mosaic-common/harness for spawning, argument construction and
// event-stream parsing. See claudecode.go's ClaudeCodeAdapter for the
// mapping shape this implementation mirrors.
type OpenCodeAdapter struct {
	executablePath string
	timeout        time.Duration
	spawner        commonharness.Spawner
	sink           commonharness.Sink
	logger         domain.DebugLogger
}

// ExecutablePath implements domain.ExecutableRevealer. It returns the
// executable path or command name this adapter was constructed with.
func (a *OpenCodeAdapter) ExecutablePath() string { return a.executablePath }

// NewOpenCodeAdapter creates an OpenCodeAdapter with debug logging disabled.
// Zero timeout means 30 minutes, matching the existing adapter.
func NewOpenCodeAdapter(executablePath string, timeout time.Duration) *OpenCodeAdapter {
	return NewOpenCodeAdapterWithLogger(executablePath, timeout, nil)
}

// NewOpenCodeAdapterWithLogger creates an adapter that records every
// invocation's raw stdout, raw stderr and outcome to the debug log. A nil
// logger is normalised to domain.NopDebugLogger.
func NewOpenCodeAdapterWithLogger(executablePath string, timeout time.Duration, logger domain.DebugLogger) *OpenCodeAdapter {
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	if logger == nil {
		logger = domain.NopDebugLogger{}
	}
	sink := &debugLoggerSink{logger: logger}
	spawner := commonharness.NewOpenCode(executablePath,
		commonharness.WithTimeout(timeout),
		commonharness.WithSink(sink),
	)
	return &OpenCodeAdapter{executablePath: executablePath, timeout: timeout, spawner: spawner, sink: sink, logger: logger}
}

// Invoke implements domain.HarnessAdapter.
//
// Error returns use the sentinel error taxonomy shared with ClaudeCodeAdapter
// (ErrExecutableNotFound, ErrNonZeroExit, ErrTimeout), plus this adapter's
// own ErrStreamError and ErrStreamIncomplete for the stream-derived verdict
// `opencode run`'s exit code cannot report.
//
// On context cancellation, terminates the subprocess and returns ctx.Err().
func (a *OpenCodeAdapter) Invoke(ctx context.Context, agent domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	a.logger.Log(domain.EventHarnessInvokeStart, "dispatching invocation",
		domain.F("agent", request.AgentInstanceID),
		domain.F("kind", string(agent.InvocationKind)),
	)

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
		// SystemPrompt carries a synthesized <env> block for BOTH invocation
		// kinds, unlike ClaudeCodeAdapter (which only needs one for the
		// orchestrator kind): OpenCode's --agent replaces the default system
		// prompt regardless of which agent is selected, so the <env>
		// preamble is lost in both cases and must be supplied in both.
		SystemPrompt: commonharness.EnvBlock(""),
	}

	resp, err := a.spawner.Spawn(ctx, spawnReq)
	if err != nil {
		// ErrMalformedJSON/ErrProtocolNotExtractable mean the harness's
		// output could not be turned into a protocol response despite a
		// process-level success; ErrStreamError/ErrStreamIncomplete mean the
		// same thing for OpenCode's stream-derived verdict, which has no
		// Claude Code counterpart because that harness reports failure
		// through its exit code instead.
		switch {
		case errors.Is(err, commonharness.ErrMalformedJSON),
			errors.Is(err, commonharness.ErrProtocolNotExtractable),
			errors.Is(err, ErrStreamError),
			errors.Is(err, ErrStreamIncomplete):
			a.logger.Log(domain.EventHarnessParseFailed, string(resp.Stdout),
				domain.F("agent", request.AgentInstanceID),
			)
		}
		// A missing or non-executable binary is recoverable via the override
		// screen. Wrap it in a domain.HarnessLaunchError so the TUI can
		// distinguish it from every other failure class by error identity.
		if errors.Is(err, commonharness.ErrExecutableNotFound) {
			launchErr := &domain.HarnessLaunchError{
				Harness:    commonharness.HarnessIDOpenCode,
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
// On context cancellation, returns ctx.Err().
func (a *OpenCodeAdapter) InvokeRaw(ctx context.Context, agent domain.AgentReference, payload []byte) ([]byte, error) {
	a.logger.Log(domain.EventHarnessInvokeStart, "dispatching raw invocation",
		domain.F("agent", agent.Identifier),
		domain.F("kind", string(agent.InvocationKind)),
	)

	spawnReq := commonharness.SpawnRequest{
		Agent: commonharness.AgentRef{
			Identifier:     agent.Identifier,
			DefinitionPath: agent.DefinitionPath,
			Kind:           commonharness.InvocationKind(agent.InvocationKind),
		},
		Prompt:       string(payload),
		OutputFormat: "json",
		SystemPrompt: commonharness.EnvBlock(""),
	}

	cmd, err := commonharness.ResolveExecutable(a.executablePath)
	if err != nil {
		a.logger.Log(domain.EventHarnessInvokeError, err.Error(),
			domain.F("agent", agent.Identifier),
		)
		return nil, err
	}

	args, err := commonharness.BuildOpenCodeArgs(spawnReq)
	if err != nil {
		a.logger.Log(domain.EventHarnessInvokeError, err.Error(),
			domain.F("agent", agent.Identifier),
		)
		return nil, err
	}

	resp, err := commonharness.Run(ctx, cmd, args, commonharness.RunOptions{
		WorkingDir: spawnReq.WorkingDir,
		Env:        spawnReq.Env,
		Timeout:    a.timeout,
		Sink:       a.sink,
	})
	if err != nil {
		// A missing or non-executable binary is recoverable via the override
		// screen. Wrap it in a domain.HarnessLaunchError so the TUI can
		// distinguish it from every other failure class by error identity.
		if errors.Is(err, commonharness.ErrExecutableNotFound) {
			launchErr := &domain.HarnessLaunchError{
				Harness:    commonharness.HarnessIDOpenCode,
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

	text, err := commonharness.ParseOpenCodeEnvelope(resp.Stdout)
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
