package harness_test

// Tests for the Spawner returned by NewClaudeCode: the request-shaped entry
// point. Ported and adapted from mosaic-run/internal/harness's
// ClaudeCodeAdapter.Invoke integration coverage.
//
// A dedicated parity section asserts that this entry point and the
// plan-execution entry point (Run, covered in run_test.go) reach the same
// lifecycle code: the same helper-process scenarios must produce the same
// sentinel classification through both paths. A behaviour asserted through
// one and not the other would be a second implementation wearing one name.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"mosaic-common/harness"
)

func minimalRequest(agent harness.AgentRef) harness.SpawnRequest {
	return harness.SpawnRequest{
		Agent:        agent,
		Prompt:       "do the thing",
		OutputFormat: "json",
	}
}

// ---------------------------------------------------------------------------
// Success path
// ---------------------------------------------------------------------------

func TestSpawn_Success_ReturnsResponseWithProtocol(t *testing.T) {
	setHelperEnv(t, "success")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	resp, err := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

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

func TestSpawn_ArgumentConstruction_ReachesTheHelperProcess(t *testing.T) {
	argsFile := setHelperEnv(t, "success")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want the built args to include --output-format json, got %v", args)
	}
	if !containsArg(args, "-p") {
		t.Errorf("want the built args to include -p, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// Failure sentinels
// ---------------------------------------------------------------------------

func TestSpawn_EmptyOutput_ReturnsErrEmptyResponse(t *testing.T) {
	setHelperEnv(t, "empty")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

func TestSpawn_NonJSONOutput_ReturnsErrMalformedJSON(t *testing.T) {
	setHelperEnv(t, "bad-json")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrMalformedJSON) {
		t.Errorf("want ErrMalformedJSON, got %v", err)
	}
}

func TestSpawn_NoResultEntry_ReturnsErrProtocolNotExtractable(t *testing.T) {
	setHelperEnv(t, "bad-envelope")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrProtocolNotExtractable) {
		t.Errorf("want ErrProtocolNotExtractable, got %v", err)
	}
}

func TestSpawn_NonZeroExit_ReturnsErrNonZeroExit(t *testing.T) {
	setHelperEnv(t, "exit1")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrNonZeroExit) {
		t.Errorf("want ErrNonZeroExit, got %v", err)
	}
}

func TestSpawn_Timeout_ReturnsErrTimeout(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(100*time.Millisecond))

	_, err := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

func TestSpawn_ContextCancellation_ReturnsErrCancelled(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(30*time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := spawner.Spawn(ctx, minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrCancelled) {
		t.Errorf("want ErrCancelled, got %v", err)
	}
}

func TestSpawn_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	spawner := harness.NewClaudeCode("/nonexistent/path/to/claude", harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Diagnostic sink
// ---------------------------------------------------------------------------

type recordingSink struct {
	mu     sync.Mutex
	events []harness.Event
}

func (s *recordingSink) Log(ev harness.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

func TestSpawn_WithSink_ReceivesDiagnosticEvents(t *testing.T) {
	setHelperEnv(t, "success")
	sink := &recordingSink{}
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second), harness.WithSink(sink))

	_, err := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sink.count() == 0 {
		t.Errorf("want at least one diagnostic event logged through WithSink")
	}
}

// ---------------------------------------------------------------------------
// Parity between the request-shaped and plan-execution entry points (T2.3)
// ---------------------------------------------------------------------------

// TestSpawnAndRun_ShareOneLifecycle_TimeoutSentinelMatches verifies that
// Spawn (request-shaped) and Run (plan-execution) classify the identical
// hang scenario identically. If the two entry points diverged into separate
// lifecycle implementations, this is exactly the kind of test that would
// pass through one and fail through the other.
func TestSpawnAndRun_ShareOneLifecycle_TimeoutSentinelMatches(t *testing.T) {
	setHelperEnv(t, "hang")

	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(100*time.Millisecond))
	_, spawnErr := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

	_, runErr := harness.Run(context.Background(), harness.Command{Path: helperExe(t)}, nil, harness.RunOptions{
		Timeout: 100 * time.Millisecond,
	})

	if !errors.Is(spawnErr, harness.ErrTimeout) {
		t.Errorf("want Spawn to return ErrTimeout, got %v", spawnErr)
	}
	if !errors.Is(runErr, harness.ErrTimeout) {
		t.Errorf("want Run to return ErrTimeout, got %v", runErr)
	}
}

// TestSpawnAndRun_ShareOneLifecycle_NonZeroExitSentinelMatches verifies that
// both entry points classify a non-zero subprocess exit identically.
func TestSpawnAndRun_ShareOneLifecycle_NonZeroExitSentinelMatches(t *testing.T) {
	setHelperEnv(t, "exit1")

	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))
	_, spawnErr := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

	_, runErr := harness.Run(context.Background(), harness.Command{Path: helperExe(t)}, nil, harness.RunOptions{
		Timeout: 5 * time.Second,
	})

	if !errors.Is(spawnErr, harness.ErrNonZeroExit) {
		t.Errorf("want Spawn to return ErrNonZeroExit, got %v", spawnErr)
	}
	if !errors.Is(runErr, harness.ErrNonZeroExit) {
		t.Errorf("want Run to return ErrNonZeroExit, got %v", runErr)
	}
}

// TestSpawnAndRun_ShareOneLifecycle_MissingExecutableSentinelMatches
// verifies that both entry points classify an unresolvable executable
// identically.
func TestSpawnAndRun_ShareOneLifecycle_MissingExecutableSentinelMatches(t *testing.T) {
	spawner := harness.NewClaudeCode("/nonexistent/path/to/claude", harness.WithTimeout(5*time.Second))
	_, spawnErr := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

	_, runErr := harness.Run(context.Background(), harness.Command{Path: "/nonexistent/path/to/claude"}, nil, harness.RunOptions{
		Timeout: 5 * time.Second,
	})

	if !errors.Is(spawnErr, harness.ErrExecutableNotFound) {
		t.Errorf("want Spawn to return ErrExecutableNotFound, got %v", spawnErr)
	}
	if !errors.Is(runErr, harness.ErrExecutableNotFound) {
		t.Errorf("want Run to return ErrExecutableNotFound, got %v", runErr)
	}
}
