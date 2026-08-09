// OpenCode argument building lives in its own file, sitting beside spawn.go's
// Claude Code BuildArgs rather than being folded into it: the two CLIs'
// contracts diverge enough (positional prompt vs -p flag, no session-reuse
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
// `opencode run` contract. Pure: no file, process, clock or environment
// access on any path.
//
// The positional message carries SystemPrompt (prepended, separated by a
// newline, when non-empty) rather than any file-based mechanism: OpenCode's
// only alternative for injecting system-prompt content is a config file's
// `prompt` field with a `{file:...}` reference, which requires writing a
// config file — I/O a pure builder cannot perform, and a file this spawn
// path deliberately does not write.
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
func BuildOpenCodeArgs(req SpawnRequest) ([]string, error) {
	if req.Agent.Identifier == "" {
		return nil, ErrOpenCodeEmptyAgentIdentifier
	}
	if req.OutputFormat != "" && req.OutputFormat != "json" {
		return nil, ErrOpenCodeUnsupportedOutputFormat
	}

	args := []string{
		"run",
		"--agent", req.Agent.Identifier,
		"--format", "json",
		// --auto auto-approves only what is not explicitly denied. It does
		// NOT override a capability that defaults to deny (currently only
		// .env file access is documented as such). If a MOSAIC OpenCode
		// agent's toolset is later found to need such a capability, the fix
		// is a permissive OpenCode config entry for that capability — not an
		// assumption that --auto already covers it.
		"--auto",
	}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// ExtraArgs must precede the positional message: the message is
	// positional, so appending anything after it would be read as further
	// message words rather than as arguments.
	args = append(args, req.ExtraArgs...)

	message := req.Prompt
	if req.SystemPrompt != "" {
		message = req.SystemPrompt + "\n" + req.Prompt
	}
	args = append(args, message)

	return args, nil
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

	args, err := BuildOpenCodeArgs(req)
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
