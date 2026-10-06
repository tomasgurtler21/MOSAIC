// OpenCode argument building lives in its own file, sitting beside spawn.go's
// Claude Code BuildArgs rather than being folded into it: the two CLIs'
// contracts diverge enough (stdin prompt vs -p flag, no session-reuse
// flags, no append-system-prompt equivalent) that a shared function would
// need to branch on more than InvocationKind.
package harness

import (
	"context"
	"errors"
)

// ErrOpenCodeUnsupportedOutputFormat reports a SpawnRequest asking for an
// output format this harness's machine-readable mode does not offer.
var ErrOpenCodeUnsupportedOutputFormat = errors.New("harness: opencode supports only the json output format")

// ErrOpenCodeEmptyAgentIdentifier reports a SpawnRequest whose
// Agent.Identifier is empty. The identifier is what `--agent` selects the
// agent definition by; an empty value has no meaningful CLI argument to
// build.
var ErrOpenCodeEmptyAgentIdentifier = errors.New("harness: opencode requires a non-empty agent identifier")

// BuildOpenCodeArgs constructs the CLI arguments for one request against the
// `opencode run` contract and returns the prompt content to be written to the
// child's stdin. Pure: no file, process, clock or environment access on any
// path.
//
// The prompt is delivered on stdin, never in argv: a `.cmd`/`.bat` shim runs
// through `cmd /c`, and cmd.exe truncates the command line at the first
// newline, which the multi-line env block always contains (the same rule as the
// Claude Code BuildArgs). stdin is SystemPrompt + "\n" + Prompt when
// SystemPrompt is non-empty, otherwise Prompt; it is nil when that content is
// empty, in which case OpenCode reports "You must provide a message or a
// command".
//
// No positional message is ever emitted, not even an empty one: OpenCode
// 1.18.18 stores `positional + "\n" + stdin` as the user message when both are
// present, and uses stdin verbatim when no positional is given. Verified live
// against OpenCode 1.18.18 by comparing the stored message with the stdin
// payload byte for byte: native exe and `cmd /c` shim, payloads with and
// without a trailing newline, and a 38 568-byte payload (beyond the cmd.exe
// and CreateProcess command-line limits). --agent and --auto behave as with a
// positional message.
//
// OpenCode's only alternative for injecting system-prompt content is a config
// file's `prompt` field with a `{file:...}` reference, which requires writing
// a config file - I/O a pure builder cannot perform.
//
// req.Agent.DefinitionPath, req.MaxTurns and req.AllowedTools are
// deliberately unused: the documented `opencode run` flag list offers no
// counterpart for any of them, and inventing one would diverge from the
// documented contract.
//
// The ephemeral-session guarantee is structural: --session, --continue and
// --fork appear nowhere in this function, for any input, so a fresh session
// is created on every invocation. OpenCode may nonetheless leave orphaned
// session state on disk even though no reuse flag is ever passed; that is a
// known and accepted limitation, not something this builder can prevent.
func BuildOpenCodeArgs(req SpawnRequest) (args []string, stdin []byte, err error) {
	if req.Agent.Identifier == "" {
		return nil, nil, ErrOpenCodeEmptyAgentIdentifier
	}
	if req.OutputFormat != "" && req.OutputFormat != "json" {
		return nil, nil, ErrOpenCodeUnsupportedOutputFormat
	}

	args = []string{
		"run",
		"--agent", req.Agent.Identifier,
		"--format", "json",
		// --auto satisfies the no-manual-permission-prompts requirement by
		// converting every "ask" permission into an implicit "allow" for the
		// duration of this invocation while leaving explicitly-denied
		// capabilities unchanged. It does NOT override a capability that
		// defaults to deny (currently only .env file access is documented as
		// such). If a MOSAIC OpenCode agent's toolset is later found to need
		// such a capability, the fix is a permissive OpenCode config entry
		// for that capability — not an assumption that --auto already covers
		// it. No per-tool allowlist is needed or possible: OpenCode offers no
		// CLI flag to scope --auto to a named tool subset.
		"--auto",
	}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// ExtraArgs come last; there is no positional message after them.
	args = append(args, req.ExtraArgs...)

	content := req.Prompt
	if req.SystemPrompt != "" {
		content = req.SystemPrompt + "\n" + req.Prompt
	}
	if content != "" {
		stdin = []byte(content)
	}

	return args, stdin, nil
}

// NewOpenCode constructs a Spawner bound to the given executable path,
// assembling the OpenCode argument builder and envelope parser onto the same
// shared executable resolution and subprocess lifecycle the Claude Code
// spawner uses.
func NewOpenCode(executablePath string, opts ...Option) Spawner {
	cfg := &spawnerConfig{executablePath: executablePath}
	for _, opt := range opts {
		opt(cfg)
	}
	return &openCodeSpawner{cfg: cfg}
}

// openCodeSpawner is the concrete Spawner returned by NewOpenCode.
type openCodeSpawner struct {
	cfg *spawnerConfig
}

// Spawn implements Spawner.
//
// Unlike the Claude Code spawner, a zero exit code from Run says nothing
// about whether the agent turn succeeded: `opencode run` exits 0 even on
// failure. Step 5's ParseOpenCodeEnvelope verdict is the only success
// signal. On a stream-reported failure, the populated Response is still
// returned alongside the error — matching the Claude Code spawner's
// behaviour on a parse failure — so the caller keeps raw stdout for
// logging.
func (s *openCodeSpawner) Spawn(ctx context.Context, req SpawnRequest) (Response, error) {
	cmd, err := ResolveExecutable(s.cfg.executablePath)
	if err != nil {
		return Response{}, err
	}

	args, stdin, err := BuildOpenCodeArgs(req)
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
		return Response{}, err
	}

	text, err := ParseOpenCodeEnvelope(resp.Stdout)
	if err != nil {
		return resp, err
	}

	protocol, err := ExtractProtocolJSON(text)
	if err != nil {
		return resp, err
	}

	resp.Text = text
	resp.Protocol = protocol
	return resp, nil
}
