package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// defaultTimeout is applied when neither the request nor the spawner
// configuration specifies one, matching the prior Runner behaviour.
const defaultTimeout = 30 * time.Minute

// WithTimeout sets the per-invocation timeout on a Spawner constructed by
// NewClaudeCode.
func WithTimeout(d time.Duration) Option {
	return func(c *spawnerConfig) { c.timeout = d }
}

// WithSink sets the diagnostic sink on a Spawner constructed by
// NewClaudeCode.
func WithSink(s Sink) Option {
	return func(c *spawnerConfig) { c.sink = s }
}

// NewClaudeCode constructs a Spawner bound to the given executable path.
func NewClaudeCode(executablePath string, opts ...Option) Spawner {
	cfg := &spawnerConfig{executablePath: executablePath}
	for _, opt := range opts {
		opt(cfg)
	}
	return &claudeCodeSpawner{cfg: cfg}
}

// claudeCodeSpawner is the concrete Spawner returned by NewClaudeCode.
type claudeCodeSpawner struct {
	cfg *spawnerConfig
}

// Spawn implements Spawner.
//
// It resolves the executable, builds the CLI arguments for req, and
// delegates the actual subprocess lifecycle to Run so both entry points
// share one implementation. Once Run succeeds, the CLI's output-format
// envelope is parsed and the Communication Protocol response extracted.
func (s *claudeCodeSpawner) Spawn(ctx context.Context, req SpawnRequest) (Response, error) {
	cmd, err := ResolveExecutable(s.cfg.executablePath)
	if err != nil {
		return Response{}, err
	}

	args, stdin, err := BuildArgs(req)
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
		return resp, err
	}

	text, err := ParseClaudeCodeEnvelope(resp.Stdout)
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

// ResolveExecutable resolves the configured path, handling the Windows
// .cmd/.bat shim case by returning a command that wraps it with "cmd /c" —
// the shape npm-installed CLI shims require on Windows.
func ResolveExecutable(path string) (Command, error) {
	if runtime.GOOS == "windows" {
		lower := strings.ToLower(path)
		if strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat") {
			return Command{Path: "cmd", PrefixArgs: []string{"/c", path}}, nil
		}
	}
	return Command{Path: path}, nil
}

// BuildArgs constructs the CLI arguments for one request and the payload
// delivered on the subprocess's standard input, such that no argv element
// ever contains a newline.
//
// For InvocationOrdinary: uses --append-system-prompt-file with the agent's
// definition file, keeping the CLI's default system prompt (including
// <env>).
//
// For InvocationOrchestrator: uses --agent <identifier> to launch the
// orchestrator as the primary/main thread. Because --agent fully replaces
// the default system prompt (losing the CLI's <env> block), an equivalent
// <env> block is synthesized and prepended to the stdin payload alongside the
// prompt content.
//
// Both invocation kinds use --permission-mode auto and never include
// --dangerously-skip-permissions.
//
// The prompt content never appears as an argv value. -p is emitted as a bare
// flag with no following value; the prompt content (with the <env> block
// prepended for orchestrator invocations) is returned as stdin. stdin is nil
// when req.Prompt is empty and no <env> block was prepended, so the caller's
// existing if o.Stdin != nil guard behaves as it did before this change.
//
// Note: this function calls EnvBlock for orchestrator invocations, which reads
// the clock and the process working directory and is therefore not pure.
func BuildArgs(req SpawnRequest) (args []string, stdin []byte, err error) {
	stdinContent := req.Prompt

	switch req.Agent.Kind {
	case InvocationOrchestrator:
		stdinContent = EnvBlock(req.WorkingDir) + "\n" + stdinContent
		args = []string{
			"--agent", req.Agent.Identifier,
			"-p",
			"--output-format", req.OutputFormat,
			"--permission-mode", "auto",
			"--no-session-persistence",
		}
	default: // InvocationOrdinary
		args = []string{
			"-p",
			"--append-system-prompt-file", req.Agent.DefinitionPath,
			"--output-format", req.OutputFormat,
			"--permission-mode", "auto",
			"--no-session-persistence",
		}
	}

	args = append(args, req.ExtraArgs...)

	if stdinContent != "" {
		stdin = []byte(stdinContent)
	}
	return args, stdin, nil
}

// EnvBlock synthesizes the <env> preamble a harness's default system prompt
// would normally supply, for the case where selecting a named agent replaces
// that default prompt entirely.
//
// workingDir names the directory the block should report. An empty
// workingDir resolves the current process's working directory, which is the
// behaviour the Claude Code orchestrator path has always had; a caller that
// knows the subject will run somewhere else (a sandbox, for instance) passes
// that directory explicitly rather than letting the block describe the wrong
// place.
//
// This function reads the clock and may read the process working directory.
// BuildArgs calls it for InvocationOrchestrator requests.
func EnvBlock(workingDir string) string {
	dir := workingDir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	date := time.Now().Format("2006-01-02")
	return fmt.Sprintf("<env>\nWorking directory: %s\nPlatform: %s\nCurrent date: %s\n</env>", dir, runtime.GOOS, date)
}

// Run executes an already-built plan: a resolved command plus arguments plus
// run options. This is the plan-execution entry point mosaic-agent-test
// consumes: it performs no argument construction, executable resolution or
// envelope parsing of its own — those stay with the caller (or, for the
// request-shaped Spawn, with the steps that precede this call).
//
// Run owns the one subprocess-lifecycle implementation shared by both entry
// points: start, wait, adapter timeout with process kill, and parent-context
// cancellation with process kill. Timeout and cancellation remain
// distinguishable to the caller via ErrTimeout and ErrCancelled.
func Run(ctx context.Context, cmd Command, args []string, o RunOptions) (Response, error) {
	start := time.Now()

	fullArgs := make([]string, 0, len(cmd.PrefixArgs)+len(args))
	fullArgs = append(fullArgs, cmd.PrefixArgs...)
	fullArgs = append(fullArgs, args...)

	execCmd := exec.Command(cmd.Path, fullArgs...)
	if o.WorkingDir != "" {
		execCmd.Dir = o.WorkingDir
	}
	if len(o.Env) > 0 {
		execCmd.Env = append(os.Environ(), o.Env...)
	}
	if o.Stdin != nil {
		execCmd.Stdin = bytes.NewReader(o.Stdin)
	}

	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	if err := execCmd.Start(); err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrExecutableNotFound, err)
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- execCmd.Wait() }()

	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	var waitErr error
	select {
	case <-ctx.Done():
		_ = execCmd.Process.Kill()
		<-waitCh
		return Response{}, ErrCancelled
	case <-time.After(timeout):
		_ = execCmd.Process.Kill()
		<-waitCh
		logEvent(o.Sink, "spawn.timeout", "invocation exceeded configured timeout", nil)
		return Response{}, ErrTimeout
	case waitErr = <-waitCh:
	}

	duration := time.Since(start)

	logEvent(o.Sink, "spawn.stdout", stdout.String(), []Field{{Key: "bytes", Value: strconv.Itoa(stdout.Len())}})
	logEvent(o.Sink, "spawn.stderr", stderr.String(), []Field{{Key: "bytes", Value: strconv.Itoa(stderr.Len())}})

	if waitErr != nil {
		var exitErr *exec.ExitError
		var wrapped error
		exitCode := -1
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
			stderrContent := strings.TrimSpace(stderr.String())
			if stderrContent != "" {
				wrapped = fmt.Errorf("%w: exit status %d: %s", ErrNonZeroExit, exitCode, stderrContent)
			} else {
				wrapped = fmt.Errorf("%w: exit status %d", ErrNonZeroExit, exitCode)
			}
		} else {
			wrapped = fmt.Errorf("%w: %v", ErrNonZeroExit, waitErr)
		}
		logEvent(o.Sink, "spawn.error", wrapped.Error(), nil)
		// The exit code and captured stderr are carried through even on
		// failure: a caller decoding a non-zero exit (an authentication or
		// environment failure, for instance) needs both to diagnose it
		// without reproducing outside the tool. Only Start failing (handled
		// above) leaves the response genuinely empty.
		return Response{
			ExitCode: exitCode,
			Stdout:   stdout.Bytes(),
			Stderr:   stderr.String(),
			Duration: duration,
		}, wrapped
	}

	return Response{
		ExitCode: 0,
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.String(),
		Duration: duration,
	}, nil
}

// logEvent logs an event to sink if non-nil. Contract: never panics, never
// blocks, mirrors the Sink.Log contract.
func logEvent(sink Sink, name, message string, fields []Field) {
	if sink == nil {
		return
	}
	sink.Log(Event{Name: name, Message: message, Fields: fields})
}
