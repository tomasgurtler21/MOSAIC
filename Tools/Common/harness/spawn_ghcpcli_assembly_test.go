package harness_test

// Tests for the Spawner returned by NewGHCPCLI: the request-shaped assembly of
// ResolveExecutable, BuildGHCPCLIArgs, Run, ParseGHCPCLIEnvelope and
// ExtractProtocolJSON.
//
// Wiring tests verify that the spawner correctly wires together its component
// steps: arguments constructed by BuildGHCPCLIArgs reach the subprocess,
// working directory and environment from the request reach the subprocess,
// timeout precedence resolves in the documented order, a successful stream
// yields a Response with Text and Protocol populated, and the Sink receives
// the diagnostic events Run emits.
//
// Exit-code independence tests pin the behaviour that distinguishes this
// spawner from both its neighbours: a non-zero process exit accompanied by a
// stream that reports success yields success (with the exit code preserved on
// Response for diagnosis); a zero process exit with no terminal result event
// yields ErrGHCPCLIStreamIncomplete; the observed failure shape (MCP events
// on stdout, model error on stderr, no terminal event) yields an error
// carrying the stderr text; and a missing executable, a timeout, and a
// cancelled context each return their existing shared sentinels without
// attempting to parse.
//
// Reuses the ordinaryAgent fixture from buildargs_test.go and the
// setHelperEnv/helperExe/readArgs/containsArg/containsSequence helpers plus
// the recordingSink type from helper_test.go/spawn_test.go, since all live in
// this package.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mosaic-common/harness"
)

func minimalGHCPCLISpawnRequest(agent harness.AgentRef) harness.SpawnRequest {
	return harness.SpawnRequest{
		Agent:        agent,
		Prompt:       "do the thing",
		OutputFormat: "json",
		GHCPCLIMode:  harness.GHCPCLIModeBlanket,
	}
}

// ---------------------------------------------------------------------------
// Wiring: argument construction reaches the subprocess
// ---------------------------------------------------------------------------

// TestGHCPCLISpawn_ArgumentConstruction_ReachesSubprocess verifies that the
// arguments BuildGHCPCLIArgs produces for the request are the ones delivered
// to the subprocess. The test inspects the args file the helper process writes
// to confirm the fixed flags and prompt placement.
func TestGHCPCLISpawn_ArgumentConstruction_ReachesSubprocess(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	// BuildGHCPCLIArgs always emits these fixed flags.
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want --output-format json in subprocess args, got %v", args)
	}
	if !containsArg(args, "--yolo") {
		t.Errorf("want --yolo in subprocess args, got %v", args)
	}
	if !containsArg(args, "--no-ask-user") {
		t.Errorf("want --no-ask-user in subprocess args, got %v", args)
	}
	// -p and the prompt value are always the final two elements.
	if len(args) < 2 || args[len(args)-2] != "-p" {
		t.Errorf("want -p as second-to-last arg, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// Wiring: successful stream yields Response with Text and Protocol
// ---------------------------------------------------------------------------

func TestGHCPCLISpawn_Success_ReturnsResponseWithTextAndProtocol(t *testing.T) {
	setHelperEnv(t, "ghcpcli-success")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	resp, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if resp.ExitCode != 0 {
		t.Errorf("want ExitCode=0, got %d", resp.ExitCode)
	}
	if resp.Text == "" {
		t.Errorf("want non-empty Text set from the assistant.message event content")
	}
	if len(resp.Protocol) == 0 {
		t.Fatalf("want a non-empty Protocol payload")
	}
	if !strings.Contains(string(resp.Protocol), "test-agent#1") {
		t.Errorf("want the protocol payload to carry the agent instance id, got %q", resp.Protocol)
	}
}

// ---------------------------------------------------------------------------
// Wiring: working directory from the request reaches the subprocess
// ---------------------------------------------------------------------------

// TestGHCPCLISpawn_WorkingDir_ReachesSubprocess verifies that SpawnRequest.WorkingDir
// is forwarded to Run's RunOptions.WorkingDir and reaches the subprocess.
//
// The echo-cwd helper outputs the subprocess working directory as raw text (not a
// valid GHCP event stream), so the spawner returns a parse error alongside the
// populated Response. The raw stdout is the subprocess's cwd, which the test
// checks against the requested working directory.
func TestGHCPCLISpawn_WorkingDir_ReachesSubprocess(t *testing.T) {
	setHelperEnv(t, "echo-cwd")
	workingDir := t.TempDir()
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	req := minimalGHCPCLISpawnRequest(ordinaryAgent())
	req.WorkingDir = workingDir
	resp, err := spawner.Spawn(context.Background(), req)

	// echo-cwd output is raw text, not a GHCP JSONL stream, so a parse error
	// is expected. The populated Response carries raw stdout regardless.
	if err == nil {
		t.Fatal("want a parse error from non-JSONL output, got nil — raw-text output should not succeed")
	}
	if len(resp.Stdout) == 0 {
		t.Fatal("want raw stdout preserved on the Response alongside the parse error")
	}
	// The raw stdout is the subprocess's working directory.
	if !strings.Contains(string(resp.Stdout), workingDir) {
		t.Errorf("want working directory %q in subprocess raw stdout, got %q", workingDir, string(resp.Stdout))
	}
}

// ---------------------------------------------------------------------------
// Wiring: environment from the request reaches the subprocess
// ---------------------------------------------------------------------------

// TestGHCPCLISpawn_Env_ReachesSubprocess verifies that SpawnRequest.Env is
// forwarded to Run's RunOptions.Env and reaches the subprocess.
//
// The echo-env helper outputs the named env var's value as raw text (not a
// valid GHCP event stream), so the spawner returns a parse error alongside the
// populated Response. The raw stdout is the env var value, which the test
// checks for the sentinel.
func TestGHCPCLISpawn_Env_ReachesSubprocess(t *testing.T) {
	setHelperEnv(t, "echo-env")
	t.Setenv("GO_HELPER_ECHO_VAR", "MOSAIC_GHCPCLI_TEST_VAR")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	req := minimalGHCPCLISpawnRequest(ordinaryAgent())
	req.Env = []string{"MOSAIC_GHCPCLI_TEST_VAR=ghcpcli-sentinel-value"}
	resp, err := spawner.Spawn(context.Background(), req)

	// echo-env output is raw text, not a GHCP JSONL stream, so a parse error
	// is expected. The populated Response carries raw stdout regardless.
	if err == nil {
		t.Fatal("want a parse error from non-JSONL output, got nil — raw-text output should not succeed")
	}
	if len(resp.Stdout) == 0 {
		t.Fatal("want raw stdout preserved on the Response alongside the parse error")
	}
	// The raw stdout is the value of MOSAIC_GHCPCLI_TEST_VAR in the subprocess.
	if !strings.Contains(string(resp.Stdout), "ghcpcli-sentinel-value") {
		t.Errorf("want env var sentinel value in subprocess raw stdout, got %q", string(resp.Stdout))
	}
}

// ---------------------------------------------------------------------------
// Wiring: timeout precedence (req.Timeout > spawner timeout > package default)
// ---------------------------------------------------------------------------

// TestGHCPCLISpawn_RequestTimeout_TakesPrecedenceOverSpawnerTimeout verifies
// that req.Timeout wins when it is shorter than the spawner-level timeout.
// The hang subprocess is killed within the shorter req.Timeout deadline.
func TestGHCPCLISpawn_RequestTimeout_TakesPrecedenceOverSpawnerTimeout(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(30*time.Second))

	req := minimalGHCPCLISpawnRequest(ordinaryAgent())
	req.Timeout = 100 * time.Millisecond
	_, err := spawner.Spawn(context.Background(), req)

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout when req.Timeout is shorter than spawner timeout, got %v", err)
	}
}

// TestGHCPCLISpawn_SpawnerTimeout_AppliesToSubprocessWhenRequestTimeoutAbsent
// verifies that the spawner-level timeout is used when the request does not
// specify one (req.Timeout == 0).
func TestGHCPCLISpawn_SpawnerTimeout_AppliesToSubprocessWhenRequestTimeoutAbsent(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(100*time.Millisecond))

	_, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout from the spawner-level timeout, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Wiring: diagnostic sink receives events
// ---------------------------------------------------------------------------

func TestGHCPCLISpawn_WithSink_ReceivesDiagnosticEvents(t *testing.T) {
	setHelperEnv(t, "ghcpcli-success")
	sink := &recordingSink{}
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second), harness.WithSink(sink))

	_, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sink.count() == 0 {
		t.Errorf("want at least one diagnostic event logged through WithSink")
	}
}

// ---------------------------------------------------------------------------
// Wiring: NewGHCPCLI returns the shared Spawner interface
// ---------------------------------------------------------------------------

// TestNewGHCPCLI_ReturnsSpawnerInterface verifies that NewGHCPCLI's return
// value satisfies the shared Spawner interface, the same contract NewClaudeCode
// and NewOpenCode satisfy — callers construct any of the three behind the same
// variable type.
func TestNewGHCPCLI_ReturnsSpawnerInterface(t *testing.T) {
	var _ harness.Spawner = harness.NewGHCPCLI("copilot")
}

// ---------------------------------------------------------------------------
// Exit-code independence: non-zero process exit with a success stream
// ---------------------------------------------------------------------------

// TestGHCPCLISpawn_NonZeroExitWithSuccessStream_ReportsSuccess is the primary
// exit-code independence test for this spawner. The helper process exits 1
// while its stream emits a result line with exitCode 0. The spawner must
// continue past ErrNonZeroExit from Run and use the stream's own verdict:
// success. Neither Claude Code nor OpenCode exhibits this behaviour — it is
// the one property specific to this spawner that is most likely to be
// "simplified" away later.
func TestGHCPCLISpawn_NonZeroExitWithSuccessStream_ReportsSuccess(t *testing.T) {
	setHelperEnv(t, "ghcpcli-nonzero-exit-success-stream")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	resp, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want success when the stream reports success despite a non-zero process exit, got %v", err)
	}
	if len(resp.Protocol) == 0 {
		t.Errorf("want Protocol populated on success, got empty")
	}
}

// TestGHCPCLISpawn_NonZeroExitWithSuccessStream_ExitCodePreservedOnResponse
// verifies that when the spawner continues past a non-zero process exit and
// reaches a success verdict, the process exit code is still visible on the
// Response. Callers and diagnostic logging can see it for post-hoc diagnosis
// even though it did not control the outcome.
func TestGHCPCLISpawn_NonZeroExitWithSuccessStream_ExitCodePreservedOnResponse(t *testing.T) {
	setHelperEnv(t, "ghcpcli-nonzero-exit-success-stream")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	resp, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want success, got %v", err)
	}
	if resp.ExitCode == 0 {
		t.Errorf("want non-zero ExitCode preserved on Response for diagnosis, got ExitCode=%d", resp.ExitCode)
	}
}

// ---------------------------------------------------------------------------
// Exit-code independence: zero process exit with no terminal event
// ---------------------------------------------------------------------------

// TestGHCPCLISpawn_ZeroExitWithNoTerminalEvent_ReturnsErrGHCPCLIStreamIncomplete
// verifies the converse of the non-zero-exit success case: a zero process exit
// is not sufficient for a success verdict when the stream contains no terminal
// result event. This is the scenario the upstream report describes: the CLI
// exits 0 after producing nothing at all.
func TestGHCPCLISpawn_ZeroExitWithNoTerminalEvent_ReturnsErrGHCPCLIStreamIncomplete(t *testing.T) {
	setHelperEnv(t, "ghcpcli-zero-exit-incomplete-stream")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrGHCPCLIStreamIncomplete) {
		t.Errorf("want ErrGHCPCLIStreamIncomplete when process exits 0 but stream has no terminal event, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Exit-code independence: observed failure shape
// ---------------------------------------------------------------------------

// TestGHCPCLISpawn_ObservedFailureShape_ReturnsErrWithStderrText reproduces the
// real failure capture: MCP events on stdout, no terminal result event, and the
// model error on stderr. The error returned must carry the stderr content
// because that is the only place the CLI writes the real diagnosis (auth
// failure, bad model, etc.).
func TestGHCPCLISpawn_ObservedFailureShape_ReturnsErrWithStderrText(t *testing.T) {
	setHelperEnv(t, "ghcpcli-observed-failure")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if err == nil {
		t.Fatal("want an error for the observed failure shape, got nil")
	}
	if !errors.Is(err, harness.ErrGHCPCLIStreamIncomplete) {
		t.Errorf("want ErrGHCPCLIStreamIncomplete for the observed failure shape, got %v", err)
	}
	// The error must carry the stderr model-error text so callers can diagnose
	// the failure without separately re-running the CLI.
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("want error to contain the stderr model-unavailable text, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Unambiguous failures: shared sentinels returned without parsing
// ---------------------------------------------------------------------------

// TestGHCPCLISpawn_MissingExecutable_ReturnsErrExecutableNotFound verifies
// that a missing executable is classified immediately as ErrExecutableNotFound
// without any attempt to parse. Run returns an empty Response in this case, so
// there is nothing to parse.
func TestGHCPCLISpawn_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	spawner := harness.NewGHCPCLI("/nonexistent/path/to/copilot", harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

// TestGHCPCLISpawn_Timeout_ReturnsErrTimeout verifies that an invocation that
// exceeds the configured timeout is classified as ErrTimeout without parsing.
// Run kills the subprocess and returns an empty Response in this case.
func TestGHCPCLISpawn_Timeout_ReturnsErrTimeout(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(100*time.Millisecond))

	_, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

// TestGHCPCLISpawn_CancelledContext_ReturnsErrCancelled verifies that a
// cancelled parent context is classified as ErrCancelled without parsing.
// Run kills the subprocess and returns an empty Response in this case.
func TestGHCPCLISpawn_CancelledContext_ReturnsErrCancelled(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(30*time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := spawner.Spawn(ctx, minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrCancelled) {
		t.Errorf("want ErrCancelled, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Populated Response returned alongside parse and extraction errors
// ---------------------------------------------------------------------------

// TestGHCPCLISpawn_ParseFailure_ReturnsPopulatedResponseAlongsideError verifies
// that on a parse failure the populated Response is returned alongside the
// error — matching both existing spawners' behaviour — so the caller retains
// raw stdout for logging.
func TestGHCPCLISpawn_ParseFailure_ReturnsPopulatedResponseAlongsideError(t *testing.T) {
	setHelperEnv(t, "ghcpcli-zero-exit-incomplete-stream")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	resp, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	// The populated Response is returned alongside the error so the caller
	// keeps raw stdout for logging, matching both existing spawners.
	if len(resp.Stdout) == 0 {
		t.Errorf("want raw stdout preserved on the Response alongside the parse error")
	}
}
