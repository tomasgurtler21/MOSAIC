package harness_test

// Tests for ClaudeCodeAdapter's deterministic dontAsk permission mode behavior.
//
// These tests cover Stage 2: replacing hardcoded --permission-mode auto with
// --permission-mode dontAsk + --allowedTools derived from each agent's deployed
// tools frontmatter, and FR-10's pre-spawn rejection of agents with missing or
// empty tools.
//
// All subprocess interaction uses the fake-CLI helper-process infrastructure
// already in claudecode_test.go (TestMain, runHelperProcess, helperExe,
// setHelperEnv, readArgs).
//
// Coverage:
//
//   FR-10 rejection (T2.2) -- Invoke path:
//   - Invoke returns an error wrapping ErrToolsMissing when the agent definition
//     file has no tools key in its frontmatter.
//   - Invoke returns an error wrapping ErrToolsEmpty when the tools key is
//     present but resolves to zero tool names.
//   - The FR-10 rejection error message identifies the agent by its Identifier.
//
//   FR-10 rejection (T2.2) -- InvokeRaw path:
//   - InvokeRaw returns an error wrapping ErrToolsMissing for missing tools key.
//   - InvokeRaw returns an error wrapping ErrToolsEmpty for empty tools value.
//
//   Invoke integration with tool extraction (T2.3):
//   - Invoke reads the agent's DefinitionPath, extracts Claude Code tools, and
//     causes --permission-mode dontAsk to appear in the subprocess arguments.
//   - Invoke populates --allowedTools with the correct tool names from frontmatter.
//   - --dangerously-skip-permissions is still never emitted.
//
//   InvokeRaw integration with tool extraction (T2.4):
//   - InvokeRaw reads the agent's DefinitionPath, extracts tools, and causes
//     --permission-mode dontAsk to appear in the subprocess arguments.
//   - InvokeRaw populates --allowedTools with the correct tool names.

import (
	"context"
	"errors"
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
// helpers specific to deterministic-mode tests
// ---------------------------------------------------------------------------

// writeDefFile writes a Claude Code agent definition file to a fresh temp dir
// and returns the absolute path. The file is removed automatically when the
// test ends via t.TempDir's cleanup.
func writeDefFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeDefFile: %v", err)
	}
	return path
}

// agentWithDef returns an ordinary-kind AgentReference whose DefinitionPath
// points to the given file. Used by T2.3 tests that supply a real temp file
// with known tools frontmatter.
func agentWithDef(identifier, defPath string) domain.AgentReference {
	return domain.AgentReference{
		Identifier:     identifier,
		DefinitionPath: defPath,
		InvocationKind: domain.InvocationOrdinary,
	}
}

// orchestratorWithDef returns an orchestrator-kind AgentReference whose
// DefinitionPath points to the given file. Used by T2.4 tests.
func orchestratorWithDef(identifier, defPath string) domain.AgentReference {
	return domain.AgentReference{
		Identifier:     identifier,
		DefinitionPath: defPath,
		InvocationKind: domain.InvocationOrchestrator,
	}
}

// validClaudeCodeDef is a realistic deployed Claude Code agent definition with
// a comma-separated tools scalar, matching the format ExtractClaudeCodeTools
// expects. Tool names are the realistic set used by production agent files.
const validClaudeCodeDef = "---\nname: test-agent\nmodel: claude-sonnet-4-6\ntools: Read, Write, Edit, Bash\n---\n\nAgent body.\n"

// missingToolsDef is a Claude Code agent definition with no tools key at all.
// ExtractClaudeCodeTools will return ErrToolsMissing for this content.
const missingToolsDef = "---\nname: test-agent\nmodel: claude-sonnet-4-6\n---\n\nAgent body.\n"

// emptyToolsDef is a Claude Code agent definition with an empty tools value.
// ExtractClaudeCodeTools will return ErrToolsEmpty for this content.
const emptyToolsDef = "---\nname: test-agent\nmodel: claude-sonnet-4-6\ntools:\n---\n\nAgent body.\n"

// ---------------------------------------------------------------------------
// T2.2: FR-10 rejection -- Invoke path
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_Invoke_RejectsAgentWithMissingToolsKey verifies that
// when the agent's definition file has no tools key in its frontmatter,
// ClaudeCodeAdapter.Invoke returns an error wrapping ErrToolsMissing before
// spawning any subprocess (FR-10 pre-spawn rejection).
func TestClaudeCodeAdapter_Invoke_RejectsAgentWithMissingToolsKey(t *testing.T) {
	// Set up the helper subprocess so it would succeed if spawned -- but it
	// must not be spawned, because the adapter should reject before spawning.
	setHelperEnv(t, "success")

	defPath := writeDefFile(t, missingToolsDef)
	agent := agentWithDef("missing-tools-agent", defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest("missing-tools-agent#1"))

	if err == nil {
		t.Fatal("want error when agent definition has no tools key, got nil")
	}
	if !errors.Is(err, commonharness.ErrToolsMissing) {
		t.Errorf("want errors.Is(err, ErrToolsMissing), got %v", err)
	}
}

// TestClaudeCodeAdapter_Invoke_RejectsAgentWithEmptyToolsValue verifies that
// when the agent's definition file has a tools key but its value resolves to
// zero tool names, Invoke returns an error wrapping ErrToolsEmpty.
func TestClaudeCodeAdapter_Invoke_RejectsAgentWithEmptyToolsValue(t *testing.T) {
	setHelperEnv(t, "success")

	defPath := writeDefFile(t, emptyToolsDef)
	agent := agentWithDef("empty-tools-agent", defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest("empty-tools-agent#1"))

	if err == nil {
		t.Fatal("want error when agent definition has an empty tools value, got nil")
	}
	if !errors.Is(err, commonharness.ErrToolsEmpty) {
		t.Errorf("want errors.Is(err, ErrToolsEmpty), got %v", err)
	}
}

// TestClaudeCodeAdapter_Invoke_FR10ErrorMentionsAgentIdentifier verifies that
// the FR-10 rejection error message identifies the agent, so operators can
// quickly determine which agent deployment is missing a tools field.
func TestClaudeCodeAdapter_Invoke_FR10ErrorMentionsAgentIdentifier(t *testing.T) {
	setHelperEnv(t, "success")

	defPath := writeDefFile(t, missingToolsDef)
	const agentID = "identifiable-agent-for-fr10-check"
	agent := agentWithDef(agentID, defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest(agentID+"#1"))

	if err == nil {
		t.Fatal("want error for missing tools, got nil")
	}
	if !strings.Contains(err.Error(), agentID) {
		t.Errorf("want error message to contain agent identifier %q, got %q", agentID, err.Error())
	}
}

// ---------------------------------------------------------------------------
// T2.2: FR-10 rejection -- InvokeRaw path
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_InvokeRaw_RejectsAgentWithMissingToolsKey verifies that
// when the agent's definition file has no tools key, InvokeRaw returns an error
// wrapping ErrToolsMissing before spawning any subprocess.
func TestClaudeCodeAdapter_InvokeRaw_RejectsAgentWithMissingToolsKey(t *testing.T) {
	setHelperEnv(t, "success")

	defPath := writeDefFile(t, missingToolsDef)
	agent := orchestratorWithDef("missing-tools-orchestrator", defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.InvokeRaw(context.Background(), agent, []byte(`{"action":"route"}`))

	if err == nil {
		t.Fatal("want error when agent definition has no tools key (InvokeRaw), got nil")
	}
	if !errors.Is(err, commonharness.ErrToolsMissing) {
		t.Errorf("want errors.Is(err, ErrToolsMissing) from InvokeRaw, got %v", err)
	}
}

// TestClaudeCodeAdapter_InvokeRaw_RejectsAgentWithEmptyToolsValue verifies that
// when the agent's definition file has a tools key with an empty value,
// InvokeRaw returns an error wrapping ErrToolsEmpty.
func TestClaudeCodeAdapter_InvokeRaw_RejectsAgentWithEmptyToolsValue(t *testing.T) {
	setHelperEnv(t, "success")

	defPath := writeDefFile(t, emptyToolsDef)
	agent := orchestratorWithDef("empty-tools-orchestrator", defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.InvokeRaw(context.Background(), agent, []byte(`{"action":"route"}`))

	if err == nil {
		t.Fatal("want error when agent definition has an empty tools value (InvokeRaw), got nil")
	}
	if !errors.Is(err, commonharness.ErrToolsEmpty) {
		t.Errorf("want errors.Is(err, ErrToolsEmpty) from InvokeRaw, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T2.3: Invoke integration with tool extraction
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_Invoke_WithValidTools_EmitsDontAskMode verifies that
// when the agent's definition file has a valid tools scalar, Invoke reads the
// file, extracts the tools, and causes --permission-mode dontAsk to appear in
// the subprocess arguments. This confirms the adapter's integration with
// ExtractClaudeCodeTools and BuildArgs's new dontAsk path.
func TestClaudeCodeAdapter_Invoke_WithValidTools_EmitsDontAskMode(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	defPath := writeDefFile(t, validClaudeCodeDef)
	agent := agentWithDef("tools-agent", defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest("tools-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--permission-mode", "dontAsk") {
		t.Errorf("want --permission-mode dontAsk when agent definition has valid tools, got %v", args)
	}
}

// TestClaudeCodeAdapter_Invoke_WithValidTools_DoesNotEmitAutoMode verifies that
// when the agent's definition file has a valid tools scalar, the subprocess
// does NOT receive --permission-mode auto. The dontAsk mode replaces auto on
// the production Runner path.
func TestClaudeCodeAdapter_Invoke_WithValidTools_DoesNotEmitAutoMode(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	defPath := writeDefFile(t, validClaudeCodeDef)
	agent := agentWithDef("tools-agent-no-auto", defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest("tools-agent-no-auto#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsSequence(args, "--permission-mode", "auto") {
		t.Errorf("want --permission-mode auto absent when agent definition has valid tools, got %v", args)
	}
}

// TestClaudeCodeAdapter_Invoke_WithValidTools_EmitsAllowedToolsFlags verifies
// that when the agent's definition file has a valid tools scalar, Invoke
// populates --allowedTools with each tool name extracted from the frontmatter.
// The fixture uses "Read, Write, Edit, Bash" (the validClaudeCodeDef content).
func TestClaudeCodeAdapter_Invoke_WithValidTools_EmitsAllowedToolsFlags(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	defPath := writeDefFile(t, validClaudeCodeDef)
	agent := agentWithDef("tools-agent-flags", defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest("tools-agent-flags#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	for _, tool := range []string{"Read", "Write", "Edit", "Bash"} {
		if !containsSequence(args, "--allowedTools", tool) {
			t.Errorf("want --allowedTools %q in args (from frontmatter tools scalar), got %v", tool, args)
		}
	}
}

// TestClaudeCodeAdapter_Invoke_WithValidTools_NeverDangerouslySkipPermissions
// verifies that --dangerously-skip-permissions is never emitted even when
// dontAsk mode is selected via DerivedTools.
func TestClaudeCodeAdapter_Invoke_WithValidTools_NeverDangerouslySkipPermissions(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	defPath := writeDefFile(t, validClaudeCodeDef)
	agent := agentWithDef("tools-agent-safe", defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest("tools-agent-safe#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsArg(args, "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions must never appear even with dontAsk mode, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// T2.4: InvokeRaw integration with tool extraction
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_InvokeRaw_WithValidTools_EmitsDontAskMode verifies
// that when the agent's definition file has a valid tools scalar, InvokeRaw
// reads the file, extracts the tools, and causes --permission-mode dontAsk to
// appear in the subprocess arguments.
func TestClaudeCodeAdapter_InvokeRaw_WithValidTools_EmitsDontAskMode(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	defPath := writeDefFile(t, validClaudeCodeDef)
	agent := orchestratorWithDef("tools-orchestrator", defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.InvokeRaw(context.Background(), agent, []byte(`{"action":"route","agent":"subagent"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--permission-mode", "dontAsk") {
		t.Errorf("want --permission-mode dontAsk from InvokeRaw when agent definition has valid tools, got %v", args)
	}
}

// TestClaudeCodeAdapter_InvokeRaw_WithValidTools_DoesNotEmitAutoMode verifies
// that when the agent's definition file has a valid tools scalar, InvokeRaw
// does NOT emit --permission-mode auto to the subprocess.
func TestClaudeCodeAdapter_InvokeRaw_WithValidTools_DoesNotEmitAutoMode(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	defPath := writeDefFile(t, validClaudeCodeDef)
	agent := orchestratorWithDef("tools-orchestrator-no-auto", defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.InvokeRaw(context.Background(), agent, []byte(`{"action":"route","agent":"subagent"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsSequence(args, "--permission-mode", "auto") {
		t.Errorf("want --permission-mode auto absent from InvokeRaw when agent definition has valid tools, got %v", args)
	}
}

// TestClaudeCodeAdapter_InvokeRaw_WithValidTools_EmitsAllowedToolsFlags
// verifies that when the agent's definition file has a valid tools scalar,
// InvokeRaw populates --allowedTools with each extracted tool name.
func TestClaudeCodeAdapter_InvokeRaw_WithValidTools_EmitsAllowedToolsFlags(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	defPath := writeDefFile(t, validClaudeCodeDef)
	agent := orchestratorWithDef("tools-orchestrator-flags", defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.InvokeRaw(context.Background(), agent, []byte(`{"action":"route","agent":"subagent"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	for _, tool := range []string{"Read", "Write", "Edit", "Bash"} {
		if !containsSequence(args, "--allowedTools", tool) {
			t.Errorf("want --allowedTools %q in InvokeRaw args (from frontmatter tools scalar), got %v", tool, args)
		}
	}
}

// TestClaudeCodeAdapter_InvokeRaw_WithValidTools_NeverDangerouslySkipPermissions
// verifies that --dangerously-skip-permissions is still never emitted by
// InvokeRaw even when dontAsk mode is selected via DerivedTools.
func TestClaudeCodeAdapter_InvokeRaw_WithValidTools_NeverDangerouslySkipPermissions(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	defPath := writeDefFile(t, validClaudeCodeDef)
	agent := orchestratorWithDef("tools-orchestrator-safe", defPath)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.InvokeRaw(context.Background(), agent, []byte(`{"action":"route","agent":"subagent"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsArg(args, "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions must never appear in InvokeRaw even with dontAsk mode, got %v", args)
	}
}
