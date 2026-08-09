package harness_test

// Tests for the Spawner returned by NewOpenCode: the request-shaped assembly
// of ResolveExecutable, BuildOpenCodeArgs, Run, ParseOpenCodeEnvelope and
// ExtractProtocolJSON (T4.1, AC4.2).
//
// The single highest-value test here is the zero-exit trap
// (TestOpenCodeSpawn_StreamReportedFailure_IsFailureDespiteZeroExit): the
// helper process exits 0 while its event stream reports failure, and
// NewOpenCode's Spawn must still classify the call as a failure. `opencode
// run` never reports success/failure through its exit code, so this is the
// property the whole spawner assembly exists to get right.
//
// Reuses the ordinaryAgent fixture from buildargs_test.go and the
// setHelperEnv/helperExe/readArgs/containsSequence helpers plus the
// recordingSink type from helper_test.go/spawn_test.go, since all of these
// live in this package.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mosaic-common/harness"
)

func minimalOpenCodeSpawnRequest(agent harness.AgentRef) harness.SpawnRequest {
	return harness.SpawnRequest{
		Agent:        agent,
		Prompt:       "do the thing",
		OutputFormat: "json",
	}
}

// ---------------------------------------------------------------------------
// Success path
// ---------------------------------------------------------------------------

func TestOpenCodeSpawn_Success_ReturnsResponseWithProtocol(t *testing.T) {
	setHelperEnv(t, "opencode-success")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second))

	resp, err := spawner.Spawn(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if resp.ExitCode != 0 {
		t.Errorf("want ExitCode=0, got %d", resp.ExitCode)
	}
	if len(resp.Protocol) == 0 {
		t.Fatalf("want a non-empty Protocol payload")
	}
	if !strings.Contains(string(resp.Protocol), "test-agent#1") {
		t.Errorf("want the protocol payload to carry the agent instance id, got %q", resp.Protocol)
	}
}

func TestOpenCodeSpawn_ArgumentConstruction_ReachesTheHelperProcess(t *testing.T) {
	argsFile := setHelperEnv(t, "opencode-success")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if len(args) == 0 || args[0] != "run" {
		t.Errorf("want the built args to start with run, got %v", args)
	}
	if !containsSequence(args, "--format", "json") {
		t.Errorf("want the built args to include --format json, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// The zero-exit trap (AC4.2)
// ---------------------------------------------------------------------------

// TestOpenCodeSpawn_StreamReportedFailure_IsFailureDespiteZeroExit is the
// single highest-value test in this feature: a fixture stdout carrying an
// "error" event, paired with a nil error from the underlying process exit
// (the helper exits 0), must decode to a failure.
func TestOpenCodeSpawn_StreamReportedFailure_IsFailureDespiteZeroExit(t *testing.T) {
	setHelperEnv(t, "opencode-stream-error")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second))

	resp, err := spawner.Spawn(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrOpenCodeStreamError) {
		t.Fatalf("want ErrOpenCodeStreamError despite a zero process exit code, got %v", err)
	}
	// The populated Response is still returned alongside the error, matching
	// the Claude Code spawner's behaviour on a parse failure, so the caller
	// keeps raw stdout for logging.
	if len(resp.Stdout) == 0 {
		t.Errorf("want raw stdout preserved on the response even on a stream-reported failure")
	}
}

func TestOpenCodeSpawn_IncompleteStream_ReturnsErrOpenCodeStreamIncomplete(t *testing.T) {
	setHelperEnv(t, "opencode-incomplete")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrOpenCodeStreamIncomplete) {
		t.Errorf("want ErrOpenCodeStreamIncomplete, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Failure sentinels shared with the plan-execution lifecycle (Run is unchanged)
// ---------------------------------------------------------------------------

func TestOpenCodeSpawn_NonZeroExit_ReturnsErrNonZeroExit(t *testing.T) {
	setHelperEnv(t, "exit1")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrNonZeroExit) {
		t.Errorf("want ErrNonZeroExit, got %v", err)
	}
}

func TestOpenCodeSpawn_Timeout_ReturnsErrTimeout(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(100*time.Millisecond))

	_, err := spawner.Spawn(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

func TestOpenCodeSpawn_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	spawner := harness.NewOpenCode("/nonexistent/path/to/opencode", harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

func TestOpenCodeSpawn_ContextCancellation_ReturnsErrCancelled(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(30*time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := spawner.Spawn(ctx, minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrCancelled) {
		t.Errorf("want ErrCancelled, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Diagnostic sink
// ---------------------------------------------------------------------------

func TestOpenCodeSpawn_WithSink_ReceivesDiagnosticEvents(t *testing.T) {
	setHelperEnv(t, "opencode-success")
	sink := &recordingSink{}
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second), harness.WithSink(sink))

	_, err := spawner.Spawn(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sink.count() == 0 {
		t.Errorf("want at least one diagnostic event logged through WithSink")
	}
}

// ---------------------------------------------------------------------------
// WithTimeout / WithSink are the same Option type NewClaudeCode uses
// ---------------------------------------------------------------------------

// TestNewOpenCode_ReturnsSpawnerInterface verifies that NewOpenCode's return
// value satisfies the shared Spawner interface, the same contract
// NewClaudeCode satisfies — callers construct either behind the same
// variable type.
func TestNewOpenCode_ReturnsSpawnerInterface(t *testing.T) {
	var _ harness.Spawner = harness.NewOpenCode("opencode")
}
