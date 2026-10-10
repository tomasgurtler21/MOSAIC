// GHCP CLI argument building lives in its own file, sitting beside
// spawn_opencode.go's OpenCode BuildOpenCodeArgs rather than being folded into
// either of the existing builders: the three CLIs' contracts diverge too far
// for one branching function, and this one diverges further still — it has no
// system-prompt injection flag at all. GHCP CLI layers instructions from files
// it discovers itself (.github/copilot-instructions.md, .agent.md profiles,
// .github/instructions/**/*.instructions.md, ~/.copilot/copilot-instructions.md,
// etc.); any orchestration context a MOSAIC agent needs is rendered into its
// profile at deploy time by Tools/Deployment, not injected at spawn time.
package harness

import (
	"context"
	"errors"
)

// ErrGHCPCLIUnsupportedOutputFormat reports a SpawnRequest asking for an
// output format this harness's machine-readable mode does not offer.
// Only "json" is supported; empty means "json".
var ErrGHCPCLIUnsupportedOutputFormat = errors.New("harness: ghcp-cli supports only the json output format")

// ErrGHCPCLIEmptyPrompt reports a SpawnRequest with no prompt text. The
// prompt is the value of -p, which selects single-shot non-interactive mode;
// there is no meaningful argument to build without it.
var ErrGHCPCLIEmptyPrompt = errors.New("harness: ghcp-cli requires a non-empty prompt")

// ErrGHCPCLIModeUnresolved is returned by BuildGHCPCLIArgs when
// GHCPCLIMode is zero/unresolved. The caller must resolve a mode (via TUI
// screen or CLI flag) before building GHCP CLI arguments.
var ErrGHCPCLIModeUnresolved = errors.New("harness: GHCP CLI permission mode not resolved")

// ErrGHCPCLIAllowlistEmpty is returned by BuildGHCPCLIArgs when
// GHCPCLIMode is GHCPCLIModePartialAllowlist, DerivedTools is nil or
// empty, and ToolsDerived is false. This guard protects against callers
// that did not perform tool derivation. When ToolsDerived is true, an
// empty DerivedTools slice is valid (all tools are ungated).
var ErrGHCPCLIAllowlistEmpty = errors.New("harness: GHCP CLI partial allowlist mode requires non-empty DerivedTools or ToolsDerived flag")

// BuildGHCPCLIArgs constructs the CLI arguments and the stdin content for one
// request against the `copilot --output-format json` single-shot
// non-interactive contract (prompt on stdin, no -p).
// Pure: no file, process, clock or environment access on any path.
//
// Permission mode is selected via req.GHCPCLIMode, which must be set to a
// non-zero value before this function is called:
//
//   - GHCPCLIModeBlanket: emits --yolo and --no-ask-user, granting blanket
//     permission (equivalent to --allow-all-tools --allow-all-paths
//     --allow-all-urls). req.DerivedTools is ignored.
//   - GHCPCLIModePartialAllowlist:
//     When req.ToolsDerived is true and req.DerivedTools is empty: emits
//     --no-ask-user with zero --allow-tool entries and no --yolo. This is
//     the valid "all ungated tools" case.
//     When req.ToolsDerived is false and req.DerivedTools is empty: returns
//     ErrGHCPCLIAllowlistEmpty (backward-compatible guard for callers that
//     did not perform derivation).
//     When req.DerivedTools is non-empty: emits --allow-tool entries as
//     before, regardless of ToolsDerived value.
//   - GHCPCLIModeUnresolved (zero value): returns ErrGHCPCLIModeUnresolved
//     before any args are built.
//
// --output-format json is always present. It selects JSONL output (one JSON
// object per line); the default text mode interleaves model output with UI
// chrome and is not safely parseable.
//
// req.SystemPrompt is deliberately ignored. The CLI layers instructions from
// files it discovers itself (.github/copilot-instructions.md, .agent.md
// profiles, etc.); there is no system-prompt injection flag, and none should
// be invented.
//
// req.Agent.DefinitionPath is deliberately ignored. The CLI resolves an agent
// by name against its scoped agent directories (~/.copilot/agents,
// .github/agents, etc.), never by file path.
//
// req.MaxTurns is deliberately ignored. The CLI offers no turn-limit flag.
//
// req.AllowedTools is deliberately ignored. Use req.DerivedTools for
// per-tool permission in Partial Allowlist mode; Blanket mode grants all
// permissions via --yolo.
//
// -s is deliberately not emitted. It was empirically verified that -s
// alongside --output-format json produces an identical event stream (same
// event types, same counts, same terminal event). Its absence is a finding,
// not a gap: adding it would imply a behavioural difference that does not
// exist.
//
// --resume, --continue and --name are never emitted for any input. Every
// invocation creates a fresh session; this is a structural guarantee, not a
// conditional one.
//
// Prompt delivery: the CLI documents no flag that reads the prompt from a
// file or stdin, only -p <text>. A -p value travels in argv, and on Windows
// the npm copilot.cmd shim forwards it through cmd.exe (%*), which mangles
// quotes, %VAR%, ^, &, |, <, > and caps the line at 8191 characters. The
// verified mechanism (Copilot CLI 1.0.91, direct binary and through
// cmd /c copilot.cmd, 38 KB payload included) is to omit -p entirely and pipe
// the prompt on stdin: the CLI then receives it byte-for-byte. The returned
// stdin is req.Prompt and the caller must pass it as RunOptions.Stdin.
// A -p flag must never be emitted: a non-empty value takes precedence and
// silently drops stdin, and a bare -p is a parse error. The values of
// --allow-tool and req.ExtraArgs still travel in argv (and through cmd.exe);
// they are outside the prompt-content guarantee.
//
// Argument ordering: --output-format json, permission flags (--yolo
// --no-ask-user for Blanket; --allow-tool entries then --no-ask-user for
// Partial Allowlist), then --agent and --model when their values are
// non-empty, then req.ExtraArgs in caller order.
func BuildGHCPCLIArgs(req SpawnRequest) (args []string, stdin []byte, err error) {
	// Validation order: output-format check first, then mode check, then
	// empty-prompt check. When output-format is invalid, that sentinel is
	// returned regardless of mode. When mode is unresolved and prompt is
	// also empty, the mode sentinel is returned.
	if req.OutputFormat != "" && req.OutputFormat != "json" {
		return nil, nil, ErrGHCPCLIUnsupportedOutputFormat
	}
	if req.GHCPCLIMode == GHCPCLIModeUnresolved {
		return nil, nil, ErrGHCPCLIModeUnresolved
	}
	if req.Prompt == "" {
		return nil, nil, ErrGHCPCLIEmptyPrompt
	}

	args = []string{
		"--output-format", "json",
	}

	switch req.GHCPCLIMode {
	case GHCPCLIModeBlanket:
		// Blanket mode: --yolo grants all permissions; --no-ask-user
		// disables the ask_user tool so an unattended run cannot stall.
		args = append(args, "--yolo", "--no-ask-user")
	case GHCPCLIModePartialAllowlist:
		// Partial Allowlist mode: emit one --allow-tool entry per
		// DerivedTools element, then --no-ask-user. When ToolsDerived is
		// false and DerivedTools is empty, return an error (caller did not
		// perform derivation). When ToolsDerived is true and DerivedTools is
		// empty, the agent has only ungated tools -- succeed with no entries.
		if len(req.DerivedTools) == 0 && !req.ToolsDerived {
			return nil, nil, ErrGHCPCLIAllowlistEmpty
		}
		for _, tool := range req.DerivedTools {
			args = append(args, "--allow-tool", tool)
		}
		args = append(args, "--no-ask-user")
	}

	// --agent is optional: an empty identifier selects the default assistant
	// persona. Unlike BuildOpenCodeArgs, an empty identifier is not an error.
	if req.Agent.Identifier != "" {
		args = append(args, "--agent", req.Agent.Identifier)
	}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// ExtraArgs are appended in caller order, last, after the permission,
	// --agent and --model flags. A fresh copy is made to ensure the returned
	// slice does not alias req.ExtraArgs.
	if len(req.ExtraArgs) > 0 {
		extra := make([]string, len(req.ExtraArgs))
		copy(extra, req.ExtraArgs)
		args = append(args, extra...)
	}

	// No -p is emitted: the prompt travels on stdin (see the doc comment).
	return args, []byte(req.Prompt), nil
}

// NewGHCPCLI constructs a Spawner bound to the given executable path,
// assembling BuildGHCPCLIArgs and ParseGHCPCLIEnvelope onto the shared
// executable resolution and subprocess lifecycle.
func NewGHCPCLI(executablePath string, opts ...Option) Spawner {
	cfg := &spawnerConfig{executablePath: executablePath}
	for _, opt := range opts {
		opt(cfg)
	}
	return &ghcpCLISpawner{cfg: cfg}
}

// ghcpCLISpawner is the concrete Spawner returned by NewGHCPCLI.
type ghcpCLISpawner struct {
	cfg *spawnerConfig
}

// Spawn implements Spawner.
//
// Unlike both neighbouring spawners (Claude Code and OpenCode), a non-zero
// process exit code is not a conclusive failure verdict for this harness. The
// empirical record is specific: this CLI has been observed to exit 0 after
// producing no output at all, and the event stream carries an explicit verdict
// in its terminal result line's exitCode field. A non-zero process exit
// therefore does not entitle this spawner to conclude the run failed, and a
// zero process exit does not entitle it to conclude the run succeeded.
//
// Two classes of Run error are distinguished:
//
//   - Unambiguous, no output to interpret (start failure, ErrExecutableNotFound,
//     ErrTimeout, ErrCancelled): Run returns an empty Response in each of these
//     cases. Return the error immediately without parsing.
//
//   - ErrNonZeroExit with output captured: Run returns a fully populated
//     Response. Continue to the parser; the stream's own verdict decides.
//     ErrNonZeroExit is never returned from this spawner — the stream verdict
//     replaces it. If the stream reports success, that is the answer; the
//     non-zero process exit is diagnostic noise that the caller can see on
//     Response.ExitCode. If the stream reports failure or is incomplete, the
//     parser's error is what the caller gets, and that error already carries
//     the stderr content and process exit code as context.
func (s *ghcpCLISpawner) Spawn(ctx context.Context, req SpawnRequest) (Response, error) {
	cmd, err := ResolveExecutable(s.cfg.executablePath)
	if err != nil {
		return Response{}, err
	}

	args, stdin, err := BuildGHCPCLIArgs(req)
	if err != nil {
		return Response{}, err
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = s.cfg.timeout
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	resp, err := Run(ctx, cmd, args, RunOptions{
		WorkingDir: req.WorkingDir,
		Env:        req.Env,
		Stdin:      stdin,
		Timeout:    timeout,
		Sink:       s.cfg.sink,
	})
	if err != nil {
		// Two-class error handling: only ErrNonZeroExit comes with a fully
		// populated Response that is worth parsing. Every other error (start
		// failure, timeout, cancellation) leaves the Response empty — return
		// immediately without attempting to parse.
		if !errors.Is(err, ErrNonZeroExit) {
			return Response{}, err
		}
		// ErrNonZeroExit: continue to the parser. The stream's own terminal
		// result event is the authoritative verdict. ErrNonZeroExit itself is
		// intentionally discarded here and never returned from this spawner.
	}

	text, parseErr := ParseGHCPCLIEnvelope(resp.Stdout, []byte(resp.Stderr), resp.ExitCode)
	if parseErr != nil {
		return resp, parseErr
	}

	protocol, err := ExtractProtocolJSON(text)
	if err != nil {
		return resp, err
	}

	resp.Text = text
	resp.Protocol = protocol
	return resp, nil
}

