package claudecode_test

// Tests for ClaudeCodeAdapter CLI argument construction (ordinary and orchestrator invocations).
// Uses the fake-CLI helper process defined in helperprocess_test.go.

import (
	"context"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/harness/claudecode"
)

// ---------------------------------------------------------------------------
// CLI argument construction — ordinary invocations
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_OrdinaryInvocation_IncludesAppendSystemPromptFile
// verifies that ordinary invocations include --append-system-prompt-file with
// the agent's DefinitionPath.
func TestClaudeCodeAdapter_OrdinaryInvocation_IncludesAppendSystemPromptFile(t *testing.T) {
	argsFile := setHelperEnv(t, "success")
	agent := ordinaryAgentRefCC(t)

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--append-system-prompt-file", agent.DefinitionPath) {
		t.Errorf("want --append-system-prompt-file %q in args, got %v", agent.DefinitionPath, args)
	}
}

// TestClaudeCodeAdapter_OrdinaryInvocation_IncludesPromptFlag verifies that
// ordinary invocations include -p as a bare flag and deliver the marshalled
// request JSON on stdin. The prompt travels on stdin rather than as the value
// following -p, so that multi-line prompts are not truncated by cmd.exe on Windows.
func TestClaudeCodeAdapter_OrdinaryInvocation_IncludesPromptFlag(t *testing.T) {
	argsFile := setHelperEnv(t, "success")
	stdinFile := newTestStdinCapture(t)

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	// -p must be present as a bare flag — the prompt travels on stdin, not as
	// the following argv element.
	if !containsArg(args, "-p") {
		t.Fatalf("want bare -p flag in args, got %v", args)
	}
	// The prompt content (agent_instance_id is a reliable field) must be on stdin.
	stdin := readHelperStdin(t, stdinFile)
	if !strings.Contains(string(stdin), "test-agent#1") {
		t.Errorf("want agent_instance_id %q in stdin payload, got %q", "test-agent#1", stdin)
	}
}

// TestClaudeCodeAdapter_OrdinaryInvocation_IncludesOutputFormatJSON verifies
// that ordinary invocations include --output-format json.
func TestClaudeCodeAdapter_OrdinaryInvocation_IncludesOutputFormatJSON(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want --output-format json in args, got %v", args)
	}
}

// TestClaudeCodeAdapter_OrdinaryInvocation_IncludesPermissionModeDontAsk verifies
// that ordinary invocations use --permission-mode dontAsk when the agent's
// definition file provides a valid tools frontmatter field.
func TestClaudeCodeAdapter_OrdinaryInvocation_IncludesPermissionModeDontAsk(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--permission-mode", "dontAsk") {
		t.Errorf("want --permission-mode dontAsk in args (derived from agent tools frontmatter), got %v", args)
	}
}

// TestClaudeCodeAdapter_OrdinaryInvocation_NeverDangerouslySkipPermissions
// verifies that --dangerously-skip-permissions is never present in ordinary
// invocation arguments.
func TestClaudeCodeAdapter_OrdinaryInvocation_NeverDangerouslySkipPermissions(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsArg(args, "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions must never appear in ordinary invocation args, got %v", args)
	}
}

// TestClaudeCodeAdapter_OrdinaryInvocation_NoEnvBlock verifies that ordinary
// invocations do NOT include a synthesized <env> block in the -p prompt content
// (the CLI's own <env> block is preserved via --append-system-prompt-file).
func TestClaudeCodeAdapter_OrdinaryInvocation_NoEnvBlock(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	if strings.Contains(args[pIdx+1], "<env>") {
		t.Errorf("want NO <env> block in ordinary -p prompt, got %q", args[pIdx+1])
	}
}

// ---------------------------------------------------------------------------
// CLI argument construction — orchestrator invocations
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_OrchestratorInvocation_IncludesAgentFlag verifies that
// orchestrator invocations include --agent <Identifier>.
func TestClaudeCodeAdapter_OrchestratorInvocation_IncludesAgentFlag(t *testing.T) {
	argsFile := setHelperEnv(t, "success")
	agent := orchestratorAgentRefCC(t)

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), agent, minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args, got %v", agent.Identifier, args)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_NoAppendSystemPromptFile
// verifies that orchestrator invocations do NOT include
// --append-system-prompt-file.
func TestClaudeCodeAdapter_OrchestratorInvocation_NoAppendSystemPromptFile(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRefCC(t), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsArg(args, "--append-system-prompt-file") {
		t.Errorf("want --append-system-prompt-file absent from orchestrator args, got %v", args)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_IncludesOutputFormatJSON
// verifies that orchestrator invocations include --output-format json.
func TestClaudeCodeAdapter_OrchestratorInvocation_IncludesOutputFormatJSON(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRefCC(t), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want --output-format json in orchestrator args, got %v", args)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_IncludesPermissionModeDontAsk
// verifies that orchestrator invocations use --permission-mode dontAsk when
// the agent's definition file provides a valid tools frontmatter field.
func TestClaudeCodeAdapter_OrchestratorInvocation_IncludesPermissionModeDontAsk(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRefCC(t), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--permission-mode", "dontAsk") {
		t.Errorf("want --permission-mode dontAsk in orchestrator args (derived from agent tools frontmatter), got %v", args)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_NeverDangerouslySkipPermissions
// verifies that --dangerously-skip-permissions is never present in orchestrator
// invocation arguments.
func TestClaudeCodeAdapter_OrchestratorInvocation_NeverDangerouslySkipPermissions(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRefCC(t), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if containsArg(args, "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions must never appear in orchestrator invocation args, got %v", args)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_IncludesEnvBlock verifies that
// orchestrator invocations include a synthesized <env>...</env> block in the
// stdin payload. The env block and prompt content travel on stdin rather than
// as the value following -p, so that multi-line content is not truncated by
// cmd.exe on Windows.
func TestClaudeCodeAdapter_OrchestratorInvocation_IncludesEnvBlock(t *testing.T) {
	argsFile := setHelperEnv(t, "success")
	stdinFile := newTestStdinCapture(t)

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRefCC(t), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	// -p must be present as a bare flag.
	if !containsArg(args, "-p") {
		t.Fatalf("want bare -p flag in orchestrator args, got %v", args)
	}
	// The synthesized <env> block must be present in the stdin payload.
	stdin := string(readHelperStdin(t, stdinFile))
	if !strings.Contains(stdin, "<env>") {
		t.Errorf("want <env> opening tag in orchestrator stdin payload, got %q", stdin)
	}
	if !strings.Contains(stdin, "</env>") {
		t.Errorf("want </env> closing tag in orchestrator stdin payload, got %q", stdin)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_EnvBlockContainsWorkingDir
// verifies that the synthesized <env> block in the stdin payload includes the
// working directory.
func TestClaudeCodeAdapter_OrchestratorInvocation_EnvBlockContainsWorkingDir(t *testing.T) {
	setHelperEnv(t, "success")
	stdinFile := newTestStdinCapture(t)

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRefCC(t), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stdin := string(readHelperStdin(t, stdinFile))
	if !strings.Contains(stdin, "Working directory:") {
		t.Errorf("want 'Working directory:' in orchestrator stdin payload (env block), got %q", stdin)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_EnvBlockContainsPlatform
// verifies that the synthesized <env> block in the stdin payload includes the
// platform (runtime.GOOS).
func TestClaudeCodeAdapter_OrchestratorInvocation_EnvBlockContainsPlatform(t *testing.T) {
	setHelperEnv(t, "success")
	stdinFile := newTestStdinCapture(t)

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRefCC(t), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stdin := string(readHelperStdin(t, stdinFile))
	if !strings.Contains(stdin, "Platform:") {
		t.Errorf("want 'Platform:' in orchestrator stdin payload (env block), got %q", stdin)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_EnvBlockContainsDate verifies
// that the synthesized <env> block in the stdin payload includes the current
// date in the format required by the design spec ("Current date: YYYY-MM-DD").
func TestClaudeCodeAdapter_OrchestratorInvocation_EnvBlockContainsDate(t *testing.T) {
	setHelperEnv(t, "success")
	stdinFile := newTestStdinCapture(t)

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRefCC(t), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stdin := string(readHelperStdin(t, stdinFile))
	if !strings.Contains(stdin, "Current date:") {
		t.Errorf("want 'Current date:' in orchestrator stdin payload (env block), got %q", stdin)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_IncludesRequestInPrompt verifies
// that the marshalled request JSON is present in the orchestrator stdin payload.
// An implementation that emits only the <env> block and omits the request would
// fail this test.
func TestClaudeCodeAdapter_OrchestratorInvocation_IncludesRequestInPrompt(t *testing.T) {
	setHelperEnv(t, "success")
	stdinFile := newTestStdinCapture(t)

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRefCC(t), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// agent_instance_id is a reliable field from the marshalled request JSON.
	stdin := string(readHelperStdin(t, stdinFile))
	if !strings.Contains(stdin, "orchestrator-agent#1") {
		t.Errorf("want marshalled request JSON (containing agent_instance_id) in orchestrator stdin payload, got %q", stdin)
	}
}
