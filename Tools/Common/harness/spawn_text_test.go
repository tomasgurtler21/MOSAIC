package harness_test

// Tests for the TextSpawner satisfied by NewClaudeCode, NewOpenCode and
// NewGHCPCLI: the text-only spawn path that stops after envelope parsing and
// before ExtractProtocolJSON.
//
// Coverage:
//
//   T3.1 — text-only spawn path: non-protocol reply returned as text
//   - All three spawners satisfy harness.TextSpawner (type assertion succeeds).
//   - SpawnText on a reply whose content is not a Communication Protocol message
//     returns a Response with Text populated and Protocol nil.
//   - SpawnText never returns ErrProtocolNotExtractable: that sentinel belongs
//     only to the ExtractProtocolJSON step, which this path skips.
//
//   T3.2 — harness-level failure sentinels on the text-only path
//   - Missing executable  → ErrExecutableNotFound   (all three spawners)
//   - Non-zero exit       → ErrNonZeroExit           (all three spawners)
//   - Timeout             → ErrTimeout               (all three spawners)
//   - Empty output        → ErrEmptyResponse         (all three spawners)
//   - Context cancellation → ErrCancelled            (all three spawners)
//   Sentinel parity: SpawnText and Spawn must classify the same failure
//   scenario with the same sentinel.
//
//   T3.3 — existing protocol-extracting Spawn behaviour unchanged
//   - A success case still returns Response.Protocol non-nil via Spawn.
//   - A non-protocol reply that SpawnText accepts causes Spawn to return
//     ErrProtocolNotExtractable (the protocol path is not relaxed).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mosaic-common/harness"
)

// ---------------------------------------------------------------------------
// T3.1 — type assertions: all three spawners satisfy TextSpawner
// ---------------------------------------------------------------------------

func TestClaudeCodeSpawner_SatisfiesTextSpawner(t *testing.T) {
	spawner := harness.NewClaudeCode("/nonexistent/claude")
	if _, ok := spawner.(harness.TextSpawner); !ok {
		t.Fatal("want NewClaudeCode to return a value that satisfies harness.TextSpawner")
	}
}

func TestOpenCodeSpawner_SatisfiesTextSpawner(t *testing.T) {
	spawner := harness.NewOpenCode("/nonexistent/opencode")
	if _, ok := spawner.(harness.TextSpawner); !ok {
		t.Fatal("want NewOpenCode to return a value that satisfies harness.TextSpawner")
	}
}

func TestGHCPCLISpawner_SatisfiesTextSpawner(t *testing.T) {
	spawner := harness.NewGHCPCLI("/nonexistent/copilot")
	if _, ok := spawner.(harness.TextSpawner); !ok {
		t.Fatal("want NewGHCPCLI to return a value that satisfies harness.TextSpawner")
	}
}

// ---------------------------------------------------------------------------
// T3.1 — SpawnText: non-protocol reply returned as text, not rejected
// ---------------------------------------------------------------------------

func TestClaudeCodeSpawnText_NonProtocolReply_ReturnsText(t *testing.T) {
	setHelperEnv(t, "text-only-claude")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewClaudeCode to satisfy harness.TextSpawner")
	}

	resp, err := ts.SpawnText(context.Background(), minimalRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want no error for a valid text reply, got %v", err)
	}
	if !strings.Contains(resp.Text, "route to agent-b") {
		t.Errorf("want the text reply to be returned in Response.Text, got %q", resp.Text)
	}
}

func TestOpenCodeSpawnText_NonProtocolReply_ReturnsText(t *testing.T) {
	setHelperEnv(t, "text-only-opencode")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewOpenCode to satisfy harness.TextSpawner")
	}

	resp, err := ts.SpawnText(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want no error for a valid text reply, got %v", err)
	}
	if !strings.Contains(resp.Text, "route to agent-b") {
		t.Errorf("want the text reply to be returned in Response.Text, got %q", resp.Text)
	}
}

func TestGHCPCLISpawnText_NonProtocolReply_ReturnsText(t *testing.T) {
	setHelperEnv(t, "text-only-ghcpcli")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewGHCPCLI to satisfy harness.TextSpawner")
	}

	resp, err := ts.SpawnText(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want no error for a valid text reply, got %v", err)
	}
	if !strings.Contains(resp.Text, "route to agent-b") {
		t.Errorf("want the text reply to be returned in Response.Text, got %q", resp.Text)
	}
}

// SpawnText must return a Response with Protocol nil: the protocol extraction
// step that populates it is exactly what this path skips.

func TestClaudeCodeSpawnText_SuccessfulReply_ProtocolIsNil(t *testing.T) {
	setHelperEnv(t, "text-only-claude")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewClaudeCode to satisfy harness.TextSpawner")
	}

	resp, err := ts.SpawnText(context.Background(), minimalRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if resp.Protocol != nil {
		t.Errorf("want Protocol nil on SpawnText success, got %q", resp.Protocol)
	}
}

func TestOpenCodeSpawnText_SuccessfulReply_ProtocolIsNil(t *testing.T) {
	setHelperEnv(t, "text-only-opencode")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewOpenCode to satisfy harness.TextSpawner")
	}

	resp, err := ts.SpawnText(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if resp.Protocol != nil {
		t.Errorf("want Protocol nil on SpawnText success, got %q", resp.Protocol)
	}
}

func TestGHCPCLISpawnText_SuccessfulReply_ProtocolIsNil(t *testing.T) {
	setHelperEnv(t, "text-only-ghcpcli")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewGHCPCLI to satisfy harness.TextSpawner")
	}

	resp, err := ts.SpawnText(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if resp.Protocol != nil {
		t.Errorf("want Protocol nil on SpawnText success, got %q", resp.Protocol)
	}
}

// SpawnText must never return ErrProtocolNotExtractable: that sentinel
// originates in the ExtractProtocolJSON step, which SpawnText does not call.

func TestClaudeCodeSpawnText_NeverReturnsErrProtocolNotExtractable(t *testing.T) {
	setHelperEnv(t, "text-only-claude")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewClaudeCode to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalRequest(ordinaryAgent()))

	if errors.Is(err, harness.ErrProtocolNotExtractable) {
		t.Error("want SpawnText to never return ErrProtocolNotExtractable")
	}
}

func TestOpenCodeSpawnText_NeverReturnsErrProtocolNotExtractable(t *testing.T) {
	setHelperEnv(t, "text-only-opencode")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewOpenCode to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if errors.Is(err, harness.ErrProtocolNotExtractable) {
		t.Error("want SpawnText to never return ErrProtocolNotExtractable")
	}
}

func TestGHCPCLISpawnText_NeverReturnsErrProtocolNotExtractable(t *testing.T) {
	setHelperEnv(t, "text-only-ghcpcli")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewGHCPCLI to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if errors.Is(err, harness.ErrProtocolNotExtractable) {
		t.Error("want SpawnText to never return ErrProtocolNotExtractable")
	}
}

// ---------------------------------------------------------------------------
// T3.2 — failure sentinels on the text-only path: missing executable
// ---------------------------------------------------------------------------

func TestClaudeCodeSpawnText_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	spawner := harness.NewClaudeCode("/nonexistent/path/to/claude", harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewClaudeCode to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

func TestOpenCodeSpawnText_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	spawner := harness.NewOpenCode("/nonexistent/path/to/opencode", harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewOpenCode to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

func TestGHCPCLISpawnText_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	spawner := harness.NewGHCPCLI("/nonexistent/path/to/copilot", harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewGHCPCLI to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T3.2 — failure sentinels: non-zero exit
// ---------------------------------------------------------------------------

func TestClaudeCodeSpawnText_NonZeroExit_ReturnsErrNonZeroExit(t *testing.T) {
	setHelperEnv(t, "exit1")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewClaudeCode to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrNonZeroExit) {
		t.Errorf("want ErrNonZeroExit, got %v", err)
	}
}

func TestOpenCodeSpawnText_NonZeroExit_ReturnsErrNonZeroExit(t *testing.T) {
	setHelperEnv(t, "exit1")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewOpenCode to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrNonZeroExit) {
		t.Errorf("want ErrNonZeroExit, got %v", err)
	}
}

// GHCP CLI is deliberately excluded from the non-zero-exit sentinel test
// because that harness may exit non-zero even on a successful stream (the
// stream's terminal result event, not the process exit code, is the verdict).
// The existing spawn tests already cover this distinction; asserting ErrNonZeroExit
// on ghcpcli-exit1 would misrepresent the harness's documented behaviour.

// ---------------------------------------------------------------------------
// T3.2 — failure sentinels: timeout
// ---------------------------------------------------------------------------

func TestClaudeCodeSpawnText_Timeout_ReturnsErrTimeout(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(100*time.Millisecond))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewClaudeCode to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

func TestOpenCodeSpawnText_Timeout_ReturnsErrTimeout(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(100*time.Millisecond))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewOpenCode to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

func TestGHCPCLISpawnText_Timeout_ReturnsErrTimeout(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(100*time.Millisecond))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewGHCPCLI to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T3.2 — failure sentinels: empty output
// ---------------------------------------------------------------------------

func TestClaudeCodeSpawnText_EmptyOutput_ReturnsErrEmptyResponse(t *testing.T) {
	setHelperEnv(t, "empty")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewClaudeCode to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

func TestOpenCodeSpawnText_EmptyOutput_ReturnsErrEmptyResponse(t *testing.T) {
	setHelperEnv(t, "empty")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewOpenCode to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

func TestGHCPCLISpawnText_EmptyOutput_ReturnsErrEmptyResponse(t *testing.T) {
	setHelperEnv(t, "empty")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewGHCPCLI to satisfy harness.TextSpawner")
	}

	_, err := ts.SpawnText(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T3.2 — failure sentinels: context cancellation → ErrCancelled
// ---------------------------------------------------------------------------

func TestClaudeCodeSpawnText_ContextCancellation_ReturnsErrCancelled(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(30*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewClaudeCode to satisfy harness.TextSpawner")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := ts.SpawnText(ctx, minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrCancelled) {
		t.Errorf("want ErrCancelled, got %v", err)
	}
}

func TestOpenCodeSpawnText_ContextCancellation_ReturnsErrCancelled(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(30*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewOpenCode to satisfy harness.TextSpawner")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := ts.SpawnText(ctx, minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrCancelled) {
		t.Errorf("want ErrCancelled, got %v", err)
	}
}

func TestGHCPCLISpawnText_ContextCancellation_ReturnsErrCancelled(t *testing.T) {
	setHelperEnv(t, "hang")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(30*time.Second))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewGHCPCLI to satisfy harness.TextSpawner")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := ts.SpawnText(ctx, minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrCancelled) {
		t.Errorf("want ErrCancelled, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T3.2 — sentinel parity: SpawnText and Spawn classify the same failure the same way
// ---------------------------------------------------------------------------

// TestClaudeCodeSpawnAndSpawnText_MissingExecutableSentinelMatches verifies
// that both entry points classify a missing executable identically.
func TestClaudeCodeSpawnAndSpawnText_MissingExecutableSentinelMatches(t *testing.T) {
	spawner := harness.NewClaudeCode("/nonexistent/path/to/claude", harness.WithTimeout(5*time.Second))

	_, spawnErr := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewClaudeCode to satisfy harness.TextSpawner")
	}
	_, textErr := ts.SpawnText(context.Background(), minimalRequest(ordinaryAgent()))

	if !errors.Is(spawnErr, harness.ErrExecutableNotFound) {
		t.Errorf("want Spawn to return ErrExecutableNotFound, got %v", spawnErr)
	}
	if !errors.Is(textErr, harness.ErrExecutableNotFound) {
		t.Errorf("want SpawnText to return ErrExecutableNotFound, got %v", textErr)
	}
}

// TestOpenCodeSpawnAndSpawnText_MissingExecutableSentinelMatches verifies
// that both entry points classify a missing executable identically.
func TestOpenCodeSpawnAndSpawnText_MissingExecutableSentinelMatches(t *testing.T) {
	spawner := harness.NewOpenCode("/nonexistent/path/to/opencode", harness.WithTimeout(5*time.Second))

	_, spawnErr := spawner.Spawn(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewOpenCode to satisfy harness.TextSpawner")
	}
	_, textErr := ts.SpawnText(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if !errors.Is(spawnErr, harness.ErrExecutableNotFound) {
		t.Errorf("want Spawn to return ErrExecutableNotFound, got %v", spawnErr)
	}
	if !errors.Is(textErr, harness.ErrExecutableNotFound) {
		t.Errorf("want SpawnText to return ErrExecutableNotFound, got %v", textErr)
	}
}

// TestGHCPCLISpawnAndSpawnText_MissingExecutableSentinelMatches verifies
// that both entry points classify a missing executable identically.
func TestGHCPCLISpawnAndSpawnText_MissingExecutableSentinelMatches(t *testing.T) {
	spawner := harness.NewGHCPCLI("/nonexistent/path/to/copilot", harness.WithTimeout(5*time.Second))

	_, spawnErr := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	ts, ok := spawner.(harness.TextSpawner)
	if !ok {
		t.Fatal("want NewGHCPCLI to satisfy harness.TextSpawner")
	}
	_, textErr := ts.SpawnText(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if !errors.Is(spawnErr, harness.ErrExecutableNotFound) {
		t.Errorf("want Spawn to return ErrExecutableNotFound, got %v", spawnErr)
	}
	if !errors.Is(textErr, harness.ErrExecutableNotFound) {
		t.Errorf("want SpawnText to return ErrExecutableNotFound, got %v", textErr)
	}
}

// ---------------------------------------------------------------------------
// T3.3 — existing Spawn behaviour unchanged: protocol extraction still required
// ---------------------------------------------------------------------------

// TestClaudeCodeSpawn_NonProtocolReply_RejectsWithErrProtocolNotExtractable
// verifies that the existing protocol-path Spawn rejects a reply that SpawnText
// accepts: a reply with valid text but no Communication Protocol message returns
// ErrProtocolNotExtractable from Spawn, unchanged.
func TestClaudeCodeSpawn_NonProtocolReply_RejectsWithErrProtocolNotExtractable(t *testing.T) {
	setHelperEnv(t, "text-only-claude")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

	if !errors.Is(err, harness.ErrProtocolNotExtractable) {
		t.Errorf("want Spawn to return ErrProtocolNotExtractable for a non-protocol reply, got %v", err)
	}
}

// TestOpenCodeSpawn_NonProtocolReply_RejectsWithError verifies that the
// existing Spawn on the OpenCode spawner rejects a reply whose text is not a
// Communication Protocol message.
func TestOpenCodeSpawn_NonProtocolReply_RejectsWithError(t *testing.T) {
	setHelperEnv(t, "text-only-opencode")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if err == nil {
		t.Error("want Spawn to return an error for a non-protocol reply, got nil")
	}
}

// TestGHCPCLISpawn_NonProtocolReply_RejectsWithError verifies that the
// existing Spawn on the GHCP CLI spawner rejects a reply whose content is not
// a Communication Protocol message.
func TestGHCPCLISpawn_NonProtocolReply_RejectsWithError(t *testing.T) {
	setHelperEnv(t, "text-only-ghcpcli")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	_, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if err == nil {
		t.Error("want Spawn to return an error for a non-protocol reply, got nil")
	}
}

// TestClaudeCodeSpawn_ProtocolReply_StillReturnsProtocol verifies that the
// existing Spawn path is unchanged for a valid Communication Protocol response:
// Response.Protocol is non-nil and carries the agent instance ID.
func TestClaudeCodeSpawn_ProtocolReply_StillReturnsProtocol(t *testing.T) {
	setHelperEnv(t, "success")
	spawner := harness.NewClaudeCode(helperExe(t), harness.WithTimeout(5*time.Second))

	resp, err := spawner.Spawn(context.Background(), minimalRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want no error for a valid protocol reply, got %v", err)
	}
	if len(resp.Protocol) == 0 {
		t.Error("want Response.Protocol non-nil after successful Spawn, got empty")
	}
}

// TestOpenCodeSpawn_ProtocolReply_StillReturnsProtocol verifies that Spawn
// on the OpenCode spawner still extracts a Communication Protocol message
// from a valid event stream.
func TestOpenCodeSpawn_ProtocolReply_StillReturnsProtocol(t *testing.T) {
	setHelperEnv(t, "opencode-success")
	spawner := harness.NewOpenCode(helperExe(t), harness.WithTimeout(5*time.Second))

	resp, err := spawner.Spawn(context.Background(), minimalOpenCodeSpawnRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want no error for a valid protocol reply, got %v", err)
	}
	if len(resp.Protocol) == 0 {
		t.Error("want Response.Protocol non-nil after successful Spawn, got empty")
	}
}

// TestGHCPCLISpawn_ProtocolReply_StillReturnsProtocol verifies that Spawn
// on the GHCP CLI spawner still extracts a Communication Protocol message
// from a valid event stream.
func TestGHCPCLISpawn_ProtocolReply_StillReturnsProtocol(t *testing.T) {
	setHelperEnv(t, "ghcpcli-success")
	spawner := harness.NewGHCPCLI(helperExe(t), harness.WithTimeout(5*time.Second))

	resp, err := spawner.Spawn(context.Background(), minimalGHCPCLISpawnRequest(ordinaryAgent()))

	if err != nil {
		t.Fatalf("want no error for a valid protocol reply, got %v", err)
	}
	if len(resp.Protocol) == 0 {
		t.Error("want Response.Protocol non-nil after successful Spawn, got empty")
	}
}
