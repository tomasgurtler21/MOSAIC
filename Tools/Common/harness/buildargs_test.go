package harness_test

// Tests for BuildArgs: argument construction per invocation kind, ported and
// adapted from mosaic-run/internal/harness's claudecode_test.go CLI argument
// construction coverage. BuildArgs is now a directly testable, pure step
// rather than being inlined into the spawn call.

import (
	"strings"
	"testing"

	"mosaic-common/harness"
)

func ordinaryAgent() harness.AgentRef {
	return harness.AgentRef{
		Identifier:     "test-agent",
		DefinitionPath: "/agents/test-agent.md",
		Kind:           harness.InvocationOrdinary,
	}
}

func orchestratorAgent() harness.AgentRef {
	return harness.AgentRef{
		Identifier:     "orchestrator-agent",
		DefinitionPath: "/agents/orchestrator-agent.md",
		Kind:           harness.InvocationOrchestrator,
	}
}

// ---------------------------------------------------------------------------
// Ordinary invocations
// ---------------------------------------------------------------------------

func TestBuildArgs_Ordinary_IncludesAppendSystemPromptFile(t *testing.T) {
	agent := ordinaryAgent()
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: agent, Prompt: "do the thing", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--append-system-prompt-file", agent.DefinitionPath) {
		t.Errorf("want --append-system-prompt-file %q in args, got %v", agent.DefinitionPath, args)
	}
}

func TestBuildArgs_Ordinary_IncludesPromptFlag(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "do the thing", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	if args[pIdx+1] != "do the thing" {
		t.Errorf("want -p value %q, got %q", "do the thing", args[pIdx+1])
	}
}

func TestBuildArgs_Ordinary_IncludesOutputFormat(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "do the thing", OutputFormat: "stream-json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--output-format", "stream-json") {
		t.Errorf("want --output-format stream-json in args, got %v", args)
	}
}

func TestBuildArgs_Ordinary_IncludesPermissionModeAuto(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--permission-mode", "auto") {
		t.Errorf("want --permission-mode auto in args, got %v", args)
	}
}

func TestBuildArgs_Ordinary_IncludesNoSessionPersistence(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(args, "--no-session-persistence") {
		t.Errorf("want --no-session-persistence in args, got %v", args)
	}
}

func TestBuildArgs_Ordinary_NeverDangerouslySkipPermissions(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions must never appear, got %v", args)
	}
}

func TestBuildArgs_Ordinary_NoEnvBlockInPrompt(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	if strings.Contains(args[pIdx+1], "<env>") {
		t.Errorf("want no synthesized <env> block in ordinary prompt, got %q", args[pIdx+1])
	}
}

// ---------------------------------------------------------------------------
// Orchestrator invocations
// ---------------------------------------------------------------------------

func TestBuildArgs_Orchestrator_IncludesAgentFlag(t *testing.T) {
	agent := orchestratorAgent()
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: agent, Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args, got %v", agent.Identifier, args)
	}
}

func TestBuildArgs_Orchestrator_NoAppendSystemPromptFile(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--append-system-prompt-file") {
		t.Errorf("want --append-system-prompt-file absent from orchestrator args, got %v", args)
	}
}

func TestBuildArgs_Orchestrator_IncludesOutputFormat(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want --output-format json in args, got %v", args)
	}
}

func TestBuildArgs_Orchestrator_IncludesPermissionModeAuto(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--permission-mode", "auto") {
		t.Errorf("want --permission-mode auto in args, got %v", args)
	}
}

func TestBuildArgs_Orchestrator_IncludesNoSessionPersistence(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(args, "--no-session-persistence") {
		t.Errorf("want --no-session-persistence in args, got %v", args)
	}
}

func TestBuildArgs_Orchestrator_NeverDangerouslySkipPermissions(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions must never appear, got %v", args)
	}
}

func TestBuildArgs_Orchestrator_IncludesEnvBlockWithWorkingDirPlatformDate(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	prompt := args[pIdx+1]
	for _, want := range []string{"<env>", "</env>", "Working directory:", "Platform:", "Current date:"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("want %q in synthesized env block, got %q", want, prompt)
		}
	}
}

func TestBuildArgs_Orchestrator_IncludesPromptAfterEnvBlock(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "unique-prompt-marker", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	if !strings.Contains(args[pIdx+1], "unique-prompt-marker") {
		t.Errorf("want the request prompt present alongside the env block, got %q", args[pIdx+1])
	}
}

// ---------------------------------------------------------------------------
// ExtraArgs passthrough (applies to both invocation kinds)
// ---------------------------------------------------------------------------

func TestBuildArgs_ExtraArgs_AppendedToBuiltArgs(t *testing.T) {
	args, err := harness.BuildArgs(harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "x",
		OutputFormat: "json",
		ExtraArgs:    []string{"--custom-flag", "custom-value"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--custom-flag", "custom-value") {
		t.Errorf("want ExtraArgs appended to built args, got %v", args)
	}
}
