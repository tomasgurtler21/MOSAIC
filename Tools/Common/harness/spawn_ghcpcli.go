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

// BuildGHCPCLIArgs constructs the CLI arguments for one request against the
// `copilot -p ... --output-format json` single-shot non-interactive contract.
// Pure: no file, process, clock or environment access on any path.
//
// The fixed flags --output-format json, --yolo, and --no-ask-user are always
// present. --output-format json selects JSONL output (one JSON object per
// line); the default text mode interleaves model output with UI chrome and is
// not safely parseable. --yolo grants blanket permission, documented by the
// CLI as equivalent to --allow-all-tools --allow-all-paths --allow-all-urls;
// the CLI states blanket tool permission is required for non-interactive mode.
// --no-ask-user disables the ask_user tool so an unattended run cannot stall.
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
// req.AllowedTools is deliberately ignored. --yolo already grants the
// blanket permission AllowedTools would express.
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
// Argument ordering: --output-format json, --yolo, --no-ask-user, then
// --agent and --model when their values are non-empty, then req.ExtraArgs
// in caller order, then -p PROMPT last. Keeping -p and its value last ensures
// a caller-supplied extra argument can never be swallowed as part of the
// prompt.
func BuildGHCPCLIArgs(req SpawnRequest) ([]string, error) {
	// Validation order: output-format check first, then empty-prompt check.
	// When both conditions hold, the output-format sentinel is returned.
	if req.OutputFormat != "" && req.OutputFormat != "json" {
		return nil, ErrGHCPCLIUnsupportedOutputFormat
	}
	if req.Prompt == "" {
		return nil, ErrGHCPCLIEmptyPrompt
	}

	args := []string{
		"--output-format", "json",
		"--yolo",
		"--no-ask-user",
	}

	// --agent is optional: an empty identifier selects the default assistant
	// persona. Unlike BuildOpenCodeArgs, an empty identifier is not an error.
	if req.Agent.Identifier != "" {
		args = append(args, "--agent", req.Agent.Identifier)
	}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// ExtraArgs are appended in caller order, after the fixed flags and before
	// -p, so a caller-supplied argument can never be swallowed into the prompt.
	// A fresh copy is made to ensure the returned slice does not alias req.ExtraArgs.
	if len(req.ExtraArgs) > 0 {
		extra := make([]string, len(req.ExtraArgs))
		copy(extra, req.ExtraArgs)
		args = append(args, extra...)
	}

	// -p and the prompt value are always the final two elements.
	args = append(args, "-p", req.Prompt)

	return args, nil
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

	args, err := BuildGHCPCLIArgs(req)
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

