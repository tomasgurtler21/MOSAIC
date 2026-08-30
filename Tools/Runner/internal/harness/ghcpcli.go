// Package harness — GHCPCLIAdapter implementation.
//
// GHCPCLIAdapter maps Runner's domain vocabulary onto mosaic-common/harness's
// GHCP CLI spawner, mirroring OpenCodeAdapter's thin mapping-wrapper shape:
// it converts domain.AgentReference/ProtocolRequest into the shared package's
// neutral types, delegates the spawn, and converts the result back.
//
// Two deliberate differences from OpenCodeAdapter:
//
//  1. No SystemPrompt injection. OpenCodeAdapter sets SystemPrompt to an EnvBlock
//     because OpenCode's --agent replaces the CLI's default system prompt. GHCP CLI
//     layers instructions from files it discovers itself (.github/copilot-instructions.md,
//     .agent.md profiles, etc.), and MOSAIC orchestration context is rendered into the
//     agent's .agent.md profile at deploy time by Tools/Deployment. No runtime injection
//     is needed or correct; the field is left unset for both invocation kinds.
//
//  2. Distinct sentinel alias names. The package already exports ErrStreamError and
//     ErrStreamIncomplete, aliased onto OpenCode's stream sentinels in opencode.go.
//     Those names are taken. Reusing them would silently conflate two harnesses'
//     failure taxonomies. GHCP CLI's terminal result event carries the verdict (not the
//     process exit code), which is structurally similar to OpenCode always exiting 0
//     but for a different reason — and so the names are distinct rather than shared.
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

// Sentinel aliases, GHCP-specific names so they do not collide with the
// OpenCode aliases ErrStreamError / ErrStreamIncomplete in this package.
// OpenCode always exits 0 and its stream carries the verdict; GHCP CLI's
// terminal result event, not its exit code, carries the verdict — a
// structurally similar failure mode for a different reason, which is why
// the names are distinct rather than shared.
var (
	ErrGHCPCLIStreamError      = commonharness.ErrGHCPCLIStreamError
	ErrGHCPCLIStreamIncomplete = commonharness.ErrGHCPCLIStreamIncomplete
)

// GHCPCLIAdapter implements domain.HarnessAdapter by delegating to
// mosaic-common/harness for spawning, argument construction and
// event-stream parsing.
type GHCPCLIAdapter struct {
	executablePath string
	timeout        time.Duration
	spawner        commonharness.Spawner
	sink           commonharness.Sink
	logger         domain.DebugLogger
	mode           commonharness.GHCPCLIPermissionMode
}

// ExecutablePath implements domain.ExecutableRevealer. It returns the
// executable path or command name this adapter was constructed with.
func (a *GHCPCLIAdapter) ExecutablePath() string { return a.executablePath }

// NewGHCPCLIAdapter creates an adapter with debug logging disabled.
// A zero timeout means 30 minutes.
//
// UNCHANGED SIGNATURE -- existing call sites and tests keep compiling.
// Defaults to GHCPCLIModeBlanket (--yolo behavior), preserving pre-change
// behavior for all ~18 call sites in ghcpcli_test.go and ~9 in
// rawinvoker_test.go. To use a different permission mode, call
// NewGHCPCLIAdapterWithMode instead.
func NewGHCPCLIAdapter(executablePath string, timeout time.Duration) *GHCPCLIAdapter {
	return NewGHCPCLIAdapterWithLogger(executablePath, timeout, nil)
}

// NewGHCPCLIAdapterWithLogger creates an adapter that records every
// invocation's raw stdout, raw stderr and outcome to the debug log. A nil
// logger is normalised to domain.NopDebugLogger.
//
// UNCHANGED SIGNATURE -- existing call sites and tests keep compiling.
// Defaults to GHCPCLIModeBlanket (--yolo behavior), preserving pre-change
// behavior for the production call site in main.go:674 (buildAdapter).
// To use a different permission mode, call NewGHCPCLIAdapterWithMode instead.
func NewGHCPCLIAdapterWithLogger(executablePath string, timeout time.Duration, logger domain.DebugLogger) *GHCPCLIAdapter {
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	if logger == nil {
		logger = domain.NopDebugLogger{}
	}
	sink := &debugLoggerSink{logger: logger}
	spawner := commonharness.NewGHCPCLI(executablePath,
		commonharness.WithTimeout(timeout),
		commonharness.WithSink(sink),
	)
	return &GHCPCLIAdapter{executablePath: executablePath, timeout: timeout, spawner: spawner, sink: sink, logger: logger, mode: commonharness.GHCPCLIModeBlanket}
}

// NewGHCPCLIAdapterWithMode creates an adapter with an explicit
// GHCPCLIPermissionMode. The mode is stored on the adapter and consulted on
// every Invoke/InvokeRaw call: Blanket mode emits --yolo (current behaviour);
// Partial Allowlist mode extracts --allow-tool entries from the agent's
// deployed tools frontmatter at invocation time.
//
// A nil logger is normalised to domain.NopDebugLogger. A zero timeout
// defaults to 30 minutes.
//
// Stage 4 switches the production buildAdapter call site to this constructor.
// Until then, NewGHCPCLIAdapter and NewGHCPCLIAdapterWithLogger remain the
// primary constructors; this constructor is available for tests and future
// production wiring.
func NewGHCPCLIAdapterWithMode(executablePath string, timeout time.Duration, logger domain.DebugLogger, mode commonharness.GHCPCLIPermissionMode) *GHCPCLIAdapter {
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	if logger == nil {
		logger = domain.NopDebugLogger{}
	}
	sink := &debugLoggerSink{logger: logger}
	spawner := commonharness.NewGHCPCLI(executablePath,
		commonharness.WithTimeout(timeout),
		commonharness.WithSink(sink),
	)
	return &GHCPCLIAdapter{executablePath: executablePath, timeout: timeout, spawner: spawner, sink: sink, logger: logger, mode: mode}
}

// Invoke implements domain.HarnessAdapter.
//
// SystemPrompt is deliberately left unset for both invocation kinds — unlike
// OpenCodeAdapter which always injects an EnvBlock. GHCP CLI discovers context
// from files on disk (.agent.md profiles, etc.), and MOSAIC orchestration
// context is rendered into the agent profile at deploy time by Tools/Deployment.
//
// On context cancellation, terminates the subprocess and returns ctx.Err().
func (a *GHCPCLIAdapter) Invoke(ctx context.Context, agent domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
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
		GHCPCLIMode:  a.mode,
		// SystemPrompt is deliberately left unset for both invocation kinds.
		// GHCP CLI layers instructions from files it discovers itself; MOSAIC
		// orchestration context is rendered into the agent's .agent.md profile
		// at deploy time by Tools/Deployment. No EnvBlock injection is correct.
	}

	// For Partial Allowlist mode, extract tool names from the agent's deployed
	// definition file and populate DerivedTools. BuildGHCPCLIArgs reads
	// DerivedTools to emit --allow-tool entries. In Blanket mode this step is
	// skipped: --yolo already grants all permissions regardless of tool list.
	if a.mode == commonharness.GHCPCLIModePartialAllowlist {
		tools, extractErr := commonharness.ExtractGHCPCLITools(agent.DefinitionPath)
		if extractErr != nil {
			a.logger.Log(domain.EventHarnessInvokeError, extractErr.Error(),
				domain.F("agent", request.AgentInstanceID),
			)
			return domain.ProtocolResponse{}, fmt.Errorf("extract GHCP CLI tools for %s: %w", agent.Identifier, extractErr)
		}
		spawnReq.DerivedTools = tools
	}

	resp, err := a.spawner.Spawn(ctx, spawnReq)
	if err != nil {
		// Log parse-class failures (stream verdict errors) before the
		// cancellation check so the parse-failed event is always recorded
		// when the stream itself is the problem.
		switch {
		case errors.Is(err, commonharness.ErrMalformedJSON),
			errors.Is(err, commonharness.ErrProtocolNotExtractable),
			errors.Is(err, ErrGHCPCLIStreamError),
			errors.Is(err, ErrGHCPCLIStreamIncomplete):
			a.logger.Log(domain.EventHarnessParseFailed, string(resp.Stdout),
				domain.F("agent", request.AgentInstanceID),
			)
		}
		// A missing or non-executable binary is recoverable via the override
		// screen. Wrap it in a domain.HarnessLaunchError so the TUI can
		// distinguish it from every other failure class by error identity.
		if errors.Is(err, commonharness.ErrExecutableNotFound) {
			launchErr := &domain.HarnessLaunchError{
				Harness:    commonharness.HarnessIDGHCPCLI,
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
// SystemPrompt is deliberately left unset, matching Invoke's behaviour for
// this harness: GHCP CLI layers instructions from files it discovers itself.
//
// Like Spawn, ErrNonZeroExit from Run does not terminate the call early:
// the envelope parser's verdict from the stream is authoritative.
//
// On context cancellation, returns ctx.Err().
func (a *GHCPCLIAdapter) InvokeRaw(ctx context.Context, agent domain.AgentReference, payload []byte) ([]byte, error) {
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
		GHCPCLIMode:  a.mode,
	}

	// For Partial Allowlist mode, extract tool names from the agent's deployed
	// definition file and populate DerivedTools, matching Invoke's behavior.
	if a.mode == commonharness.GHCPCLIModePartialAllowlist {
		tools, extractErr := commonharness.ExtractGHCPCLITools(agent.DefinitionPath)
		if extractErr != nil {
			a.logger.Log(domain.EventHarnessInvokeError, extractErr.Error(),
				domain.F("agent", agent.Identifier),
			)
			return nil, fmt.Errorf("extract GHCP CLI tools for %s: %w", agent.Identifier, extractErr)
		}
		spawnReq.DerivedTools = tools
	}

	cmd, err := commonharness.ResolveExecutable(a.executablePath)
	if err != nil {
		a.logger.Log(domain.EventHarnessInvokeError, err.Error(),
			domain.F("agent", agent.Identifier),
		)
		return nil, err
	}

	args, err := commonharness.BuildGHCPCLIArgs(spawnReq)
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
		if !errors.Is(err, commonharness.ErrNonZeroExit) {
			// ErrNonZeroExit is not a conclusive failure for this harness: the
			// envelope parser's own verdict decides. Any other error (launch
			// failure, timeout, cancellation) is conclusive.
			if errors.Is(err, commonharness.ErrExecutableNotFound) {
				launchErr := &domain.HarnessLaunchError{
					Harness:    commonharness.HarnessIDGHCPCLI,
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
		// ErrNonZeroExit: continue to the envelope parser. The stream's own
		// terminal result event is the authoritative verdict.
	}

	text, parseErr := commonharness.ParseGHCPCLIEnvelope(resp.Stdout, []byte(resp.Stderr), resp.ExitCode)
	if parseErr != nil {
		a.logger.Log(domain.EventHarnessInvokeError, parseErr.Error(),
			domain.F("agent", agent.Identifier),
		)
		return nil, parseErr
	}

	a.logger.Log(domain.EventHarnessStdout, text,
		domain.F("agent", agent.Identifier),
	)
	a.logger.Log(domain.EventHarnessInvokeOK, "raw invocation succeeded",
		domain.F("agent", agent.Identifier),
	)
	return []byte(text), nil
}
