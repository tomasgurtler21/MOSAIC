package harness_test

// Tests for BuildArgs: argument construction per invocation kind, ported and
// adapted from mosaic-run/internal/harness's claudecode_test.go CLI argument
// construction coverage. BuildArgs is now a directly testable, pure step
// rather than being inlined into the spawn call.
//
// Every test here uses the three-result signature
//   func BuildArgs(req SpawnRequest) (args []string, stdin []byte, err error)
// and asserts on both the argv slice and the stdin payload, because
// the delivery contract turns on both:
//   - no argv element ever contains a newline
//   - the prompt content travels on stdin, not as the value following -p
//   - an empty ordinary-invocation prompt yields nil stdin (no error)

import (
	"os"
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
	args, _, err := harness.BuildArgs(harness.SpawnRequest{Agent: agent, Prompt: "do the thing", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--append-system-prompt-file", agent.DefinitionPath) {
		t.Errorf("want --append-system-prompt-file %q in args, got %v", agent.DefinitionPath, args)
	}
}

func TestBuildArgs_Ordinary_IncludesPromptFlag(t *testing.T) {
	args, stdin, err := harness.BuildArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "do the thing", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// -p must be present as a bare flag — no following argv element is the prompt.
	if !containsArg(args, "-p") {
		t.Fatalf("want bare -p flag in args, got %v", args)
	}
	// The prompt must travel on stdin, not as an argv value.
	if string(stdin) != "do the thing" {
		t.Errorf("want stdin = %q (the prompt delivered via stdin), got %q", "do the thing", stdin)
	}
}

func TestBuildArgs_Ordinary_IncludesOutputFormat(t *testing.T) {
	args, _, err := harness.BuildArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "do the thing", OutputFormat: "stream-json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--output-format", "stream-json") {
		t.Errorf("want --output-format stream-json in args, got %v", args)
	}
}

func TestBuildArgs_Ordinary_IncludesPermissionModeAuto(t *testing.T) {
	args, _, err := harness.BuildArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--permission-mode", "auto") {
		t.Errorf("want --permission-mode auto in args, got %v", args)
	}
}

func TestBuildArgs_Ordinary_IncludesNoSessionPersistence(t *testing.T) {
	args, _, err := harness.BuildArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(args, "--no-session-persistence") {
		t.Errorf("want --no-session-persistence in args, got %v", args)
	}
}

func TestBuildArgs_Ordinary_NeverDangerouslySkipPermissions(t *testing.T) {
	args, _, err := harness.BuildArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions must never appear, got %v", args)
	}
}

func TestBuildArgs_Ordinary_NoEnvBlockInPrompt(t *testing.T) {
	args, stdin, err := harness.BuildArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// -p must be a bare flag.
	if !containsArg(args, "-p") {
		t.Fatalf("want bare -p flag in args, got %v", args)
	}
	// Ordinary invocation must not include a synthesized <env> block in the
	// stdin payload.
	if strings.Contains(string(stdin), "<env>") {
		t.Errorf("want no synthesized <env> block in ordinary stdin payload, got %q", stdin)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator invocations
// ---------------------------------------------------------------------------

func TestBuildArgs_Orchestrator_IncludesAgentFlag(t *testing.T) {
	agent := orchestratorAgent()
	args, _, err := harness.BuildArgs(harness.SpawnRequest{Agent: agent, Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args, got %v", agent.Identifier, args)
	}
}

func TestBuildArgs_Orchestrator_NoAppendSystemPromptFile(t *testing.T) {
	args, _, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--append-system-prompt-file") {
		t.Errorf("want --append-system-prompt-file absent from orchestrator args, got %v", args)
	}
}

func TestBuildArgs_Orchestrator_IncludesOutputFormat(t *testing.T) {
	args, _, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want --output-format json in args, got %v", args)
	}
}

func TestBuildArgs_Orchestrator_IncludesPermissionModeAuto(t *testing.T) {
	args, _, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--permission-mode", "auto") {
		t.Errorf("want --permission-mode auto in args, got %v", args)
	}
}

func TestBuildArgs_Orchestrator_IncludesNoSessionPersistence(t *testing.T) {
	args, _, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(args, "--no-session-persistence") {
		t.Errorf("want --no-session-persistence in args, got %v", args)
	}
}

func TestBuildArgs_Orchestrator_NeverDangerouslySkipPermissions(t *testing.T) {
	args, _, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions must never appear, got %v", args)
	}
}

func TestBuildArgs_Orchestrator_IncludesEnvBlockWithWorkingDirPlatformDate(t *testing.T) {
	args, stdin, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// -p must be a bare flag.
	if !containsArg(args, "-p") {
		t.Fatalf("want bare -p flag in args, got %v", args)
	}
	// The synthesized <env> block must be present in the stdin payload.
	stdinStr := string(stdin)
	for _, want := range []string{"<env>", "</env>", "Working directory:", "Platform:", "Current date:"} {
		if !strings.Contains(stdinStr, want) {
			t.Errorf("want %q in synthesized env block (stdin payload), got %q", want, stdinStr)
		}
	}
}

func TestBuildArgs_Orchestrator_IncludesPromptAfterEnvBlock(t *testing.T) {
	args, stdin, err := harness.BuildArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "unique-prompt-marker", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// -p must be a bare flag.
	if !containsArg(args, "-p") {
		t.Fatalf("want bare -p flag in args, got %v", args)
	}
	if !strings.Contains(string(stdin), "unique-prompt-marker") {
		t.Errorf("want the request prompt present in the stdin payload alongside the env block, got %q", stdin)
	}
}

// ---------------------------------------------------------------------------
// ExtraArgs passthrough (applies to both invocation kinds)
// ---------------------------------------------------------------------------

func TestBuildArgs_ExtraArgs_AppendedToBuiltArgs(t *testing.T) {
	args, _, err := harness.BuildArgs(harness.SpawnRequest{
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

// ---------------------------------------------------------------------------
// Delivery contract: no newline in any argv element (T2.2)
// ---------------------------------------------------------------------------

// TestBuildArgs_NoArgContainsNewline_Ordinary pins the invariant that no
// argv element ever contains a newline, even when the prompt itself is
// multi-line. On Windows, an argv element containing a newline is silently
// truncated by cmd.exe; this test guards against that class of defect.
func TestBuildArgs_NoArgContainsNewline_Ordinary(t *testing.T) {
	args, _, err := harness.BuildArgs(harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "first line\nsecond line\nthird line",
		OutputFormat: "json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, a := range args {
		if strings.Contains(a, "\n") {
			t.Errorf("args[%d] = %q: argv elements must never contain a newline (prompt must travel on stdin)", i, a)
		}
	}
}

// TestBuildArgs_NoArgContainsNewline_Orchestrator is the same guard for
// orchestrator invocations, whose stdin payload also carries the synthesized
// <env> block (itself multi-line).
func TestBuildArgs_NoArgContainsNewline_Orchestrator(t *testing.T) {
	args, _, err := harness.BuildArgs(harness.SpawnRequest{
		Agent:        orchestratorAgent(),
		Prompt:       "first line\nsecond line\nthird line",
		OutputFormat: "json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, a := range args {
		if strings.Contains(a, "\n") {
			t.Errorf("args[%d] = %q: argv elements must never contain a newline (prompt and env block must travel on stdin)", i, a)
		}
	}
}

// TestBuildArgs_EmptyPrompt_Ordinary_YieldsNilStdin pins the contract that
// an empty ordinary-invocation prompt produces nil stdin (not an error and
// not an empty-but-non-nil slice), so the caller's existing
// `if o.Stdin != nil` guard behaves as it did before the delivery change.
func TestBuildArgs_EmptyPrompt_Ordinary_YieldsNilStdin(t *testing.T) {
	_, stdin, err := harness.BuildArgs(harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "",
		OutputFormat: "json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdin != nil {
		t.Errorf("want nil stdin for empty ordinary-invocation prompt, got %q", stdin)
	}
}

// TestBuildArgs_EmptyPrompt_Orchestrator_YieldsNonNilStdinContainingEnvBlock
// pins the boundary case that an orchestrator invocation with an empty request
// prompt still yields non-nil stdin, because the synthesized <env> block is
// always prepended. Nil stdin may only occur when there is neither prompt
// content nor an <env> block; for orchestrators the <env> block is always
// present, so the `if o.Stdin != nil` guard in Run behaves correctly.
func TestBuildArgs_EmptyPrompt_Orchestrator_YieldsNonNilStdinContainingEnvBlock(t *testing.T) {
	_, stdin, err := harness.BuildArgs(harness.SpawnRequest{
		Agent:        orchestratorAgent(),
		Prompt:       "",
		OutputFormat: "json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdin == nil {
		t.Fatalf("want non-nil stdin for empty orchestrator prompt: the <env> block is always prepended, so stdin is never nil for orchestrator invocations")
	}
	if !strings.Contains(string(stdin), "<env>") {
		t.Errorf("want <env> block in orchestrator stdin payload even when prompt is empty, got %q", stdin)
	}
}

// TestBuildArgs_Ordinary_PromptDeliveredViaStdin verifies the core delivery
// contract for ordinary invocations: the prompt appears in the stdin payload,
// not as an argv element.
func TestBuildArgs_Ordinary_PromptDeliveredViaStdin(t *testing.T) {
	const prompt = "the task content for delivery verification"
	args, stdin, err := harness.BuildArgs(harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       prompt,
		OutputFormat: "json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The raw prompt string must not appear as any argv element.
	for _, a := range args {
		if a == prompt {
			t.Errorf("prompt must not appear as a raw argv element, but found %q in args %v", prompt, args)
		}
	}
	// The prompt must be present in the stdin payload.
	if !strings.Contains(string(stdin), prompt) {
		t.Errorf("want prompt %q in stdin payload, got %q", prompt, stdin)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator working directory in env block
// ---------------------------------------------------------------------------

// TestBuildArgs_Orchestrator_WorkingDirNamedInEnvBlock verifies that when a
// SpawnRequest for an orchestrator invocation carries a non-empty WorkingDir,
// the synthesized <env> block in the stdin payload names that directory.
// Without the fix, BuildArgs passes "" to EnvBlock, which falls back to the
// process's own working directory — not the request's working directory.
func TestBuildArgs_Orchestrator_WorkingDirNamedInEnvBlock(t *testing.T) {
	const wantDir = "/the/sandbox/subject/dir"
	_, stdin, err := harness.BuildArgs(harness.SpawnRequest{
		Agent:        orchestratorAgent(),
		Prompt:       "x",
		OutputFormat: "json",
		WorkingDir:   wantDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(stdin), wantDir) {
		t.Errorf("want orchestrator env block to name the request's working directory %q, got stdin %q", wantDir, stdin)
	}
}

// TestBuildArgs_Orchestrator_EmptyWorkingDirFallsBackToProcessCwd verifies
// that when a SpawnRequest carries no working directory, the synthesized <env>
// block falls back to the process's own current working directory — preserving
// the behaviour every existing caller that passes no working directory relies on.
func TestBuildArgs_Orchestrator_EmptyWorkingDirFallsBackToProcessCwd(t *testing.T) {
	processCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	_, stdin, err := harness.BuildArgs(harness.SpawnRequest{
		Agent:        orchestratorAgent(),
		Prompt:       "x",
		OutputFormat: "json",
		// WorkingDir intentionally absent — exercises the fallback path.
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(stdin), processCwd) {
		t.Errorf("want orchestrator env block to fall back to process cwd %q when request WorkingDir is empty, got stdin %q", processCwd, stdin)
	}
}

// TestBuildArgs_Orchestrator_EnvBlockAndPromptDeliveredViaStdin verifies the
// delivery contract for orchestrator invocations: both the synthesized <env>
// block and the request prompt appear in the stdin payload, not in argv.
func TestBuildArgs_Orchestrator_EnvBlockAndPromptDeliveredViaStdin(t *testing.T) {
	const prompt = "unique-orchestrator-delivery-marker"
	args, stdin, err := harness.BuildArgs(harness.SpawnRequest{
		Agent:        orchestratorAgent(),
		Prompt:       prompt,
		OutputFormat: "json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The raw prompt must not appear as any argv element.
	for _, a := range args {
		if a == prompt {
			t.Errorf("prompt must not appear as a raw argv element, but found %q in args %v", prompt, args)
		}
	}
	stdinStr := string(stdin)
	// Both the env block and the request prompt must be in the stdin payload.
	if !strings.Contains(stdinStr, "<env>") {
		t.Errorf("want <env> block in orchestrator stdin payload, got %q", stdinStr)
	}
	if !strings.Contains(stdinStr, prompt) {
		t.Errorf("want request prompt %q in orchestrator stdin payload, got %q", prompt, stdinStr)
	}
}
