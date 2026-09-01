package harness_test

// Tests for GHCPCLIAdapter.Invoke and GHCPCLIAdapter.InvokeRaw when
// constructed via NewGHCPCLIAdapterWithMode with an explicit
// GHCPCLIPermissionMode.
//
// These tests are written in the TDD RED phase before the Invoke/InvokeRaw
// mode-aware implementation (I3.3 and I3.4). They compile once:
//   - GHCPCLIPermissionMode type and constants exist in mosaic-common/harness
//   - NewGHCPCLIAdapterWithMode is defined in mosaic-run/internal/harness
//
// The tests for Partial Allowlist behavior will fail at runtime until
// I3.3/I3.4 modify Invoke/InvokeRaw to:
//   1. Read the stored mode from the adapter struct
//   2. Call ExtractGHCPCLITools(agent.DefinitionPath) when mode is Partial Allowlist
//   3. Place the result in SpawnRequest.DerivedTools
//   4. Set SpawnRequest.GHCPCLIMode from the stored mode
//
// Blanket mode tests may pass even before I3.3/I3.4 if the stub constructor
// creates an adapter that falls back to current --yolo behavior.
//
// All subprocess interaction uses the fake-CLI helper-process infrastructure
// from claudecode_test.go (TestMain, runHelperProcess, helperExe, setHelperEnv,
// readArgs, ordinaryAgentRef, orchestratorAgentRef, minimalClaudeRequest).
//
// Coverage (T3.2 - Invoke):
//   - NewGHCPCLIAdapterWithMode exists and returns a non-nil adapter
//   - Blanket mode: --yolo present, --allow-tool absent in subprocess args
//   - Partial Allowlist mode: --allow-tool entries present (translated from frontmatter)
//   - Partial Allowlist mode: --yolo absent in subprocess args
//   - Partial Allowlist mode: read/search excluded from --allow-tool entries
//   - Partial Allowlist mode: missing DefinitionPath returns an error before spawning
//   - Partial Allowlist mode: agent with only excluded tools returns ErrToolsEmpty
//
// Coverage (T3.3 - InvokeRaw):
//   - Blanket mode: --yolo present in subprocess args
//   - Partial Allowlist mode: --allow-tool entries present (translated from frontmatter)
//   - Partial Allowlist mode: --yolo absent, --no-ask-user present
//   - Partial Allowlist mode: missing DefinitionPath returns an error before spawning

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	commonharness "mosaic-common/harness"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// writeGHCPCLITestAgentFile creates a temporary GHCP CLI agent definition file
// with the given tools in flow-style YAML list format and returns its path.
// The file is removed automatically when the test ends via t.TempDir cleanup.
func writeGHCPCLITestAgentFile(t *testing.T, tools []string) string {
	t.Helper()
	items := make([]string, len(tools))
	for i, tool := range tools {
		items[i] = "'" + tool + "'"
	}
	toolsValue := "[" + strings.Join(items, ", ") + "]"
	content := fmt.Sprintf("---\nname: test-agent\ntools: %s\n---\n\nTest agent body.\n", toolsValue)
	dir := t.TempDir()
	path := filepath.Join(dir, "test-agent.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeGHCPCLITestAgentFile: %v", err)
	}
	return path
}

// agentRefWithDefinitionPath returns a domain.AgentReference with the given
// definition path, using ordinary invocation kind. Used for Partial Allowlist
// tests that require the adapter to read the file.
func agentRefWithDefinitionPath(path string) domain.AgentReference {
	return domain.AgentReference{
		Identifier:     "test-agent",
		DefinitionPath: path,
		InvocationKind: domain.InvocationOrdinary,
	}
}

// ---------------------------------------------------------------------------
// T3.2: GHCPCLIAdapter.Invoke with GHCPCLIPermissionMode
// ---------------------------------------------------------------------------

// TestNewGHCPCLIAdapterWithMode_ReturnsNonNilAdapter verifies that
// NewGHCPCLIAdapterWithMode is defined with the expected signature and returns
// a usable adapter. This test compiles only when the constructor exists with
// the (executablePath, timeout, logger, mode) parameter shape.
func TestNewGHCPCLIAdapterWithMode_ReturnsNonNilAdapter(t *testing.T) {
	adapter := harness.NewGHCPCLIAdapterWithMode(
		"/path/to/copilot",
		5*time.Second,
		nil,
		commonharness.GHCPCLIModeBlanket,
	)
	if adapter == nil {
		t.Fatal("want non-nil adapter from NewGHCPCLIAdapterWithMode")
	}
}

// TestGHCPCLIAdapterWithMode_BlanketMode_Invoke_EmitsYolo verifies that an
// adapter constructed with GHCPCLIModeBlanket includes --yolo in the args
// passed to the subprocess on Invoke.
func TestGHCPCLIAdapterWithMode_BlanketMode_Invoke_EmitsYolo(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModeBlanket,
	)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsArg(args, "--yolo") {
		t.Errorf("want --yolo in Blanket mode Invoke subprocess args, got %v", args)
	}
}

// TestGHCPCLIAdapterWithMode_BlanketMode_Invoke_DoesNotEmitAllowTool verifies
// that Blanket mode does not emit --allow-tool entries even when no
// DerivedTools are extracted: --yolo grants all permissions.
func TestGHCPCLIAdapterWithMode_BlanketMode_Invoke_DoesNotEmitAllowTool(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModeBlanket,
	)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsArg(args, "--allow-tool") {
		t.Errorf("want --allow-tool absent in Blanket mode Invoke subprocess args, got %v", args)
	}
}

// TestGHCPCLIAdapterWithMode_PartialAllowlist_Invoke_ExtractsAndEmitsAllowTool
// verifies that Partial Allowlist mode reads the agent's tools frontmatter,
// translates the tool names, and emits --allow-tool entries in subprocess args.
//
// Agent file has tools: ['edit', 'execute', 'agent', 'skill'].
// Expected translation: edit->write, execute->shell, agent->agent, skill->skill.
func TestGHCPCLIAdapterWithMode_PartialAllowlist_Invoke_ExtractsAndEmitsAllowTool(t *testing.T) {
	agentFile := writeGHCPCLITestAgentFile(t, []string{"edit", "execute", "agent", "skill"})
	agentRef := agentRefWithDefinitionPath(agentFile)

	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModePartialAllowlist,
	)
	_, err := adapter.Invoke(context.Background(), agentRef, minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	for _, tool := range []string{"write", "shell", "agent", "skill"} {
		if !containsSequence(args, "--allow-tool", tool) {
			t.Errorf("want --allow-tool %s in Partial Allowlist Invoke subprocess args, got %v", tool, args)
		}
	}
}

// TestGHCPCLIAdapterWithMode_PartialAllowlist_Invoke_NoYolo verifies that
// Partial Allowlist mode does not emit --yolo in subprocess args.
func TestGHCPCLIAdapterWithMode_PartialAllowlist_Invoke_NoYolo(t *testing.T) {
	agentFile := writeGHCPCLITestAgentFile(t, []string{"edit", "execute"})
	agentRef := agentRefWithDefinitionPath(agentFile)

	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModePartialAllowlist,
	)
	_, err := adapter.Invoke(context.Background(), agentRef, minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsArg(args, "--yolo") {
		t.Errorf("want --yolo absent in Partial Allowlist Invoke subprocess args, got %v", args)
	}
}

// TestGHCPCLIAdapterWithMode_PartialAllowlist_Invoke_ExcludesReadAndSearch
// verifies that read and search are excluded from --allow-tool entries even
// when present in the agent's tools frontmatter. The translation table
// excludes them because GHCP CLI has no read kind and auto-allows searches.
func TestGHCPCLIAdapterWithMode_PartialAllowlist_Invoke_ExcludesReadAndSearch(t *testing.T) {
	// Agent has read, search, and edit tools. Only edit should produce a
	// --allow-tool entry (translated to "write").
	agentFile := writeGHCPCLITestAgentFile(t, []string{"read", "search", "edit"})
	agentRef := agentRefWithDefinitionPath(agentFile)

	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModePartialAllowlist,
	)
	_, err := adapter.Invoke(context.Background(), agentRef, minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsSequence(args, "--allow-tool", "read") {
		t.Errorf("want --allow-tool read absent (read is excluded), got %v", args)
	}
	if containsSequence(args, "--allow-tool", "search") {
		t.Errorf("want --allow-tool search absent (search is excluded), got %v", args)
	}
	if !containsSequence(args, "--allow-tool", "write") {
		t.Errorf("want --allow-tool write (translated from edit), got %v", args)
	}
}

// TestGHCPCLIAdapterWithMode_PartialAllowlist_Invoke_MissingDefinitionPath_ReturnsError
// verifies that Partial Allowlist mode returns an error before spawning when
// the agent's DefinitionPath does not point to a real file.
func TestGHCPCLIAdapterWithMode_PartialAllowlist_Invoke_MissingDefinitionPath_ReturnsError(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "ghcpcli-success")

	agentRef := domain.AgentReference{
		Identifier:     "test-agent",
		DefinitionPath: "/nonexistent/path/to/agent.md",
		InvocationKind: domain.InvocationOrdinary,
	}

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModePartialAllowlist,
	)
	_, err := adapter.Invoke(context.Background(), agentRef, minimalClaudeRequest("test-agent#1"))
	if err == nil {
		t.Fatal("want error when DefinitionPath does not exist for Partial Allowlist Invoke, got nil")
	}
}

// TestGHCPCLIAdapterWithMode_PartialAllowlist_Invoke_OnlyExcludedTools_ReturnsErrToolsEmpty
// verifies that Partial Allowlist mode returns an error wrapping ErrToolsEmpty
// when the agent's tools frontmatter contains only tools excluded from
// --allow-tool (read, search, ask_user).
func TestGHCPCLIAdapterWithMode_PartialAllowlist_Invoke_OnlyExcludedTools_ReturnsErrToolsEmpty(t *testing.T) {
	// All three tools are excluded: read (ungated), search (auto-allowed),
	// ask_user (handled by --no-ask-user). The extraction yields an empty list.
	agentFile := writeGHCPCLITestAgentFile(t, []string{"read", "search", "ask_user"})
	agentRef := agentRefWithDefinitionPath(agentFile)

	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModePartialAllowlist,
	)
	_, err := adapter.Invoke(context.Background(), agentRef, minimalClaudeRequest("test-agent#1"))
	if err == nil {
		t.Fatal("want error when agent has only excluded tools in Partial Allowlist mode, got nil")
	}
	if !errors.Is(err, commonharness.ErrToolsEmpty) {
		t.Errorf("want error wrapping ErrToolsEmpty, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T3.3: GHCPCLIAdapter.InvokeRaw with GHCPCLIPermissionMode
// ---------------------------------------------------------------------------

// TestGHCPCLIAdapterWithMode_BlanketMode_InvokeRaw_EmitsYolo verifies that
// Blanket mode passes --yolo to the subprocess on InvokeRaw.
func TestGHCPCLIAdapterWithMode_BlanketMode_InvokeRaw_EmitsYolo(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModeBlanket,
	)
	_, err := adapter.InvokeRaw(context.Background(), ordinaryAgentRef(), rawPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsArg(args, "--yolo") {
		t.Errorf("want --yolo in Blanket mode InvokeRaw subprocess args, got %v", args)
	}
}

// TestGHCPCLIAdapterWithMode_BlanketMode_InvokeRaw_DoesNotEmitAllowTool
// verifies that Blanket mode does not emit --allow-tool on the InvokeRaw path.
func TestGHCPCLIAdapterWithMode_BlanketMode_InvokeRaw_DoesNotEmitAllowTool(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModeBlanket,
	)
	_, err := adapter.InvokeRaw(context.Background(), ordinaryAgentRef(), rawPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsArg(args, "--allow-tool") {
		t.Errorf("want --allow-tool absent in Blanket mode InvokeRaw subprocess args, got %v", args)
	}
}

// TestGHCPCLIAdapterWithMode_PartialAllowlist_InvokeRaw_ExtractsAndEmitsAllowTool
// verifies that Partial Allowlist mode reads the agent's tools frontmatter and
// emits --allow-tool entries on the InvokeRaw path, matching the same behavior
// as Invoke.
//
// Agent file has tools: ['edit', 'execute']. Expected: write, shell.
func TestGHCPCLIAdapterWithMode_PartialAllowlist_InvokeRaw_ExtractsAndEmitsAllowTool(t *testing.T) {
	agentFile := writeGHCPCLITestAgentFile(t, []string{"edit", "execute"})
	agentRef := agentRefWithDefinitionPath(agentFile)

	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModePartialAllowlist,
	)
	_, err := adapter.InvokeRaw(context.Background(), agentRef, rawPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--allow-tool", "write") {
		t.Errorf("want --allow-tool write in Partial Allowlist InvokeRaw subprocess args, got %v", args)
	}
	if !containsSequence(args, "--allow-tool", "shell") {
		t.Errorf("want --allow-tool shell in Partial Allowlist InvokeRaw subprocess args, got %v", args)
	}
}

// TestGHCPCLIAdapterWithMode_PartialAllowlist_InvokeRaw_NoYolo verifies that
// Partial Allowlist mode does not emit --yolo on the InvokeRaw path.
func TestGHCPCLIAdapterWithMode_PartialAllowlist_InvokeRaw_NoYolo(t *testing.T) {
	agentFile := writeGHCPCLITestAgentFile(t, []string{"edit"})
	agentRef := agentRefWithDefinitionPath(agentFile)

	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModePartialAllowlist,
	)
	_, err := adapter.InvokeRaw(context.Background(), agentRef, rawPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsArg(args, "--yolo") {
		t.Errorf("want --yolo absent in Partial Allowlist InvokeRaw subprocess args, got %v", args)
	}
}

// TestGHCPCLIAdapterWithMode_PartialAllowlist_InvokeRaw_EmitsNoAskUser verifies
// that --no-ask-user remains present in Partial Allowlist mode on the InvokeRaw
// path.
func TestGHCPCLIAdapterWithMode_PartialAllowlist_InvokeRaw_EmitsNoAskUser(t *testing.T) {
	agentFile := writeGHCPCLITestAgentFile(t, []string{"edit"})
	agentRef := agentRefWithDefinitionPath(agentFile)

	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModePartialAllowlist,
	)
	_, err := adapter.InvokeRaw(context.Background(), agentRef, rawPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsArg(args, "--no-ask-user") {
		t.Errorf("want --no-ask-user in Partial Allowlist InvokeRaw subprocess args, got %v", args)
	}
}

// TestGHCPCLIAdapterWithMode_PartialAllowlist_InvokeRaw_MissingDefinitionPath_ReturnsError
// verifies that InvokeRaw with Partial Allowlist mode returns an error when the
// agent's DefinitionPath does not point to a real file.
func TestGHCPCLIAdapterWithMode_PartialAllowlist_InvokeRaw_MissingDefinitionPath_ReturnsError(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "ghcpcli-success")

	agentRef := domain.AgentReference{
		Identifier:     "test-agent",
		DefinitionPath: "/nonexistent/path/to/agent.md",
		InvocationKind: domain.InvocationOrdinary,
	}

	adapter := harness.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModePartialAllowlist,
	)
	_, err := adapter.InvokeRaw(context.Background(), agentRef, rawPayload)
	if err == nil {
		t.Fatal("want error when DefinitionPath does not exist for Partial Allowlist InvokeRaw, got nil")
	}
}
