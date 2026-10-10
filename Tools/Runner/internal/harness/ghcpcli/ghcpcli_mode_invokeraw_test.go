package ghcpcli_test

// Tests for GHCPCLIAdapter.InvokeRaw constructed with an explicit GHCPCLIPermissionMode.
// Shared fixtures (writeGHCPCLITestAgentFile, agentRefWithDefinitionPath) live in
// ghcpcli_mode_test.go.
// Uses the fake-CLI helper process defined in helperprocess_test.go.

import (
	"context"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness/ghcpcli"
	commonharness "mosaic-common/harness"
)

// ---------------------------------------------------------------------------
// T3.3: GHCPCLIAdapter.InvokeRaw with GHCPCLIPermissionMode
// ---------------------------------------------------------------------------

// TestGHCPCLIAdapterWithMode_BlanketMode_InvokeRaw_EmitsYolo verifies that
// Blanket mode passes --yolo to the subprocess on InvokeRaw.
func TestGHCPCLIAdapterWithMode_BlanketMode_InvokeRaw_EmitsYolo(t *testing.T) {
	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := ghcpcli.NewGHCPCLIAdapterWithMode(
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

	adapter := ghcpcli.NewGHCPCLIAdapterWithMode(
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

	adapter := ghcpcli.NewGHCPCLIAdapterWithMode(
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

	adapter := ghcpcli.NewGHCPCLIAdapterWithMode(
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

	adapter := ghcpcli.NewGHCPCLIAdapterWithMode(
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

	adapter := ghcpcli.NewGHCPCLIAdapterWithMode(
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

// ---------------------------------------------------------------------------
// T2.1: GHCPCLIAdapter.InvokeRaw -- ungated-only and empty-tools-list agents
// ---------------------------------------------------------------------------

// TestGHCPCLIAdapterWithMode_PartialAllowlist_InvokeRaw_UngatedOnlyAgent_Succeeds
// verifies that InvokeRaw in Partial Allowlist mode succeeds for an agent whose
// tools are all ungated (read, search, ask_user). After Stage 2, the adapter sets
// ToolsDerived=true so BuildGHCPCLIArgs accepts the empty derived-tools slice.
//
// Expected arg constraints:
//   - No --allow-tool entries
//   - No --yolo
//   - --no-ask-user present
//   - no -p (the prompt travels on stdin)
func TestGHCPCLIAdapterWithMode_PartialAllowlist_InvokeRaw_UngatedOnlyAgent_Succeeds(t *testing.T) {
	agentFile := writeGHCPCLITestAgentFile(t, []string{"read", "search", "ask_user"})
	agentRef := agentRefWithDefinitionPath(agentFile)

	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := ghcpcli.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModePartialAllowlist,
	)
	_, err := adapter.InvokeRaw(context.Background(), agentRef, rawPayload)
	if err != nil {
		t.Fatalf("want successful InvokeRaw for ungated-only agent, got error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsArg(args, "--allow-tool") {
		t.Errorf("want no --allow-tool for ungated-only agent InvokeRaw, got %v", args)
	}
	if containsArg(args, "--yolo") {
		t.Errorf("want no --yolo in Partial Allowlist InvokeRaw for ungated-only agent, got %v", args)
	}
	if !containsArg(args, "--no-ask-user") {
		t.Errorf("want --no-ask-user in Partial Allowlist InvokeRaw for ungated-only agent, got %v", args)
	}
	if containsArg(args, "-p") {
		t.Errorf("want no -p in InvokeRaw args (prompt travels on stdin), got %v", args)
	}
}

// TestGHCPCLIAdapterWithMode_PartialAllowlist_InvokeRaw_EmptyToolsList_Succeeds
// verifies that InvokeRaw in Partial Allowlist mode succeeds for an agent with
// an explicit empty tools list (tools: []). After Stage 2, the adapter sets
// ToolsDerived=true so BuildGHCPCLIArgs accepts the empty derived-tools slice.
//
// Expected arg constraints:
//   - No --allow-tool entries
//   - No --yolo
//   - --no-ask-user present
//   - no -p (the prompt travels on stdin)
func TestGHCPCLIAdapterWithMode_PartialAllowlist_InvokeRaw_EmptyToolsList_Succeeds(t *testing.T) {
	agentFile := writeGHCPCLITestAgentFile(t, []string{})
	agentRef := agentRefWithDefinitionPath(agentFile)

	argsFile := setHelperEnv(t, "ghcpcli-success")

	adapter := ghcpcli.NewGHCPCLIAdapterWithMode(
		helperExe(t),
		5*time.Second,
		nil,
		commonharness.GHCPCLIModePartialAllowlist,
	)
	_, err := adapter.InvokeRaw(context.Background(), agentRef, rawPayload)
	if err != nil {
		t.Fatalf("want successful InvokeRaw for empty-tools-list agent, got error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsArg(args, "--allow-tool") {
		t.Errorf("want no --allow-tool for empty-tools-list agent InvokeRaw, got %v", args)
	}
	if containsArg(args, "--yolo") {
		t.Errorf("want no --yolo in Partial Allowlist InvokeRaw for empty-tools-list agent, got %v", args)
	}
	if !containsArg(args, "--no-ask-user") {
		t.Errorf("want --no-ask-user in Partial Allowlist InvokeRaw for empty-tools-list agent, got %v", args)
	}
	if containsArg(args, "-p") {
		t.Errorf("want no -p in InvokeRaw args (prompt travels on stdin), got %v", args)
	}
}
