package harness_test

// Tests for ClaudeCodeAdapter.
//
// Subprocess faking: this test binary doubles as the fake "claude" CLI when
// the environment variable GO_WANT_HELPER_PROCESS=1 is set. TestMain intercepts
// the subprocess entry point before Go's test framework can attempt to parse the
// claude CLI flags as test flags. Individual tests set GO_WANT_HELPER_PROCESS=1
// (via t.Setenv) so the value is inherited by subprocesses they spawn; the
// current test process itself started without the variable and is unaffected.
//
// Coverage:
//
//   CLI argument construction — ordinary invocations:
//   - Includes --append-system-prompt-file <DefinitionPath>.
//   - Includes -p <marshalled-request-json>.
//   - Includes --output-format json.
//   - Includes --permission-mode auto.
//   - Includes --no-session-persistence.
//   - Does NOT include --dangerously-skip-permissions.
//   - Does NOT include a synthesized <env> block in the -p prompt.
//
//   CLI argument construction — orchestrator invocations:
//   - Includes --agent <Identifier>.
//   - Includes -p <env-block + marshalled-request-json>.
//   - Includes --output-format json.
//   - Includes --permission-mode auto.
//   - Includes --no-session-persistence.
//   - Does NOT include --append-system-prompt-file.
//   - Does NOT include --dangerously-skip-permissions.
//   - Includes a synthesized <env> block containing "Working directory:".
//   - Includes a synthesized <env> block containing "Platform:".
//   - Includes a synthesized <env> block containing "Current date:".
//   - Includes the marshalled request JSON in the -p prompt content.
//
//   Subprocess lifecycle:
//   - Context cancellation terminates the subprocess and returns ctx.Err().
//   - Invocation timeout returns ErrTimeout.
//   - Missing executable returns ErrExecutableNotFound.
//   - Non-zero exit returns ErrNonZeroExit.
//   - Non-zero exit error message contains the captured stderr content.
//
//   CLI output parsing:
//   - Valid --output-format json envelope with embedded protocol response is
//     parsed into a domain.ProtocolResponse.
//   - Empty CLI stdout returns ErrEmptyResponse.
//   - Non-JSON CLI stdout returns ErrMalformedJSON.
//   - Valid JSON envelope with no extractable protocol response returns
//     ErrMalformedOutput.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ---------------------------------------------------------------------------
// TestMain — fake subprocess entry point
// ---------------------------------------------------------------------------

// TestMain intercepts the process entry point. When GO_WANT_HELPER_PROCESS=1
// the binary acts as the fake "claude" CLI instead of running tests.
func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		runHelperProcess()
		// runHelperProcess calls os.Exit; this line is unreachable.
	}
	os.Exit(m.Run())
}

// runHelperProcess is the fake claude CLI.
//
// It writes os.Args[1:] (the arguments the adapter passed) to the file named
// by GO_HELPER_ARGS_FILE so tests can inspect them. It then simulates CLI
// behaviour according to GO_HELPER_CMD.
func runHelperProcess() {
	// Persist the received args before producing any output.
	if argsFile := os.Getenv("GO_HELPER_ARGS_FILE"); argsFile != "" {
		data, _ := json.Marshal(os.Args[1:])
		os.WriteFile(argsFile, data, 0644) //nolint:errcheck
	}

	switch os.Getenv("GO_HELPER_CMD") {
	case "success":
		// Produce a valid --output-format json envelope containing a
		// Communication Protocol response embedded in the result string.
		type resultEntry struct {
			Type   string `json:"type"`
			Result string `json:"result"`
		}
		protocolResp := `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`
		envelope := []resultEntry{{Type: "result", Result: protocolResp}}
		data, _ := json.Marshal(envelope)
		os.Stdout.Write(data) //nolint:errcheck

	case "exit1":
		// Exit non-zero with recognisable stderr content.
		os.Stderr.WriteString("simulated stderr output") //nolint:errcheck
		os.Exit(1)

	case "empty":
		// Write nothing to stdout; exit zero.

	case "bad-json":
		// Write output that is not valid JSON.
		os.Stdout.WriteString("this is not JSON at all") //nolint:errcheck

	case "bad-envelope":
		// Write valid JSON that contains no "result" type entry and therefore
		// has no extractable protocol response.
		os.Stdout.WriteString(`[{"type":"assistant","message":"hello there"}]`) //nolint:errcheck

	case "hang":
		// Block indefinitely so the test can exercise timeout and context
		// cancellation. time.Sleep avoids the goroutine-deadlock panic that
		// select{} would emit on stderr.
		time.Sleep(24 * time.Hour)

	default:
		os.Stderr.WriteString("unknown GO_HELPER_CMD: " + os.Getenv("GO_HELPER_CMD")) //nolint:errcheck
		os.Exit(2)
	}

	os.Exit(0)
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// helperExe returns the path to this test binary, which acts as the fake
// "claude" executable when GO_WANT_HELPER_PROCESS=1 is set.
func helperExe(t *testing.T) string {
	t.Helper()
	return os.Args[0]
}

// setHelperEnv configures the current process environment so that any
// subprocess spawned by the adapter will enter helper-process mode.
//
// It sets GO_WANT_HELPER_PROCESS=1, GO_HELPER_CMD=cmd, and
// GO_HELPER_ARGS_FILE to a fresh temp file path. The returned path is
// where the helper will write the serialised os.Args[1:] it receives.
//
// All env changes are reverted by t.Cleanup via t.Setenv.
func setHelperEnv(t *testing.T, cmd string) (argsFile string) {
	t.Helper()
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", cmd)
	argsFile = filepath.Join(t.TempDir(), "args.json")
	t.Setenv("GO_HELPER_ARGS_FILE", argsFile)
	return argsFile
}

// readArgs reads the args file written by the helper subprocess and returns
// the argument slice the adapter passed to exec.Command.
func readArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("readArgs: could not read args file %q: %v", argsFile, err)
	}
	var args []string
	if err := json.Unmarshal(data, &args); err != nil {
		t.Fatalf("readArgs: could not unmarshal args: %v", err)
	}
	return args
}

// containsArg reports whether arg appears anywhere in args.
func containsArg(args []string, arg string) bool {
	for _, a := range args {
		if a == arg {
			return true
		}
	}
	return false
}

// containsSequence reports whether a and b appear as adjacent elements in args.
func containsSequence(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
}

// indexOfArg returns the index of arg in args, or -1 if not found.
func indexOfArg(args []string, arg string) int {
	for i, a := range args {
		if a == arg {
			return i
		}
	}
	return -1
}

// ordinaryAgentRef returns an AgentReference with InvocationOrdinary.
func ordinaryAgentRef() domain.AgentReference {
	return domain.AgentReference{
		Identifier:     "test-agent",
		DefinitionPath: "/agents/test-agent.md",
		InvocationKind: domain.InvocationOrdinary,
	}
}

// orchestratorAgentRef returns an AgentReference with InvocationOrchestrator.
func orchestratorAgentRef() domain.AgentReference {
	return domain.AgentReference{
		Identifier:     "orchestrator-agent",
		DefinitionPath: "/agents/orchestrator-agent.md",
		InvocationKind: domain.InvocationOrchestrator,
	}
}

// minimalClaudeRequest returns a minimal ProtocolRequest for adapter tests.
func minimalClaudeRequest(instanceID string) domain.ProtocolRequest {
	return domain.ProtocolRequest{
		AgentInstanceID: instanceID,
		TaskDescription: "do the thing",
		InputArtifacts:  []string{},
		OutputArtifacts: []string{},
	}
}

// ---------------------------------------------------------------------------
// CLI argument construction — ordinary invocations
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_OrdinaryInvocation_IncludesAppendSystemPromptFile
// verifies that ordinary invocations include --append-system-prompt-file with
// the agent's DefinitionPath.
func TestClaudeCodeAdapter_OrdinaryInvocation_IncludesAppendSystemPromptFile(t *testing.T) {
	argsFile := setHelperEnv(t, "success")
	agent := ordinaryAgentRef()

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
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
// ordinary invocations include -p with the marshalled request JSON.
func TestClaudeCodeAdapter_OrdinaryInvocation_IncludesPromptFlag(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	// The prompt value must contain the marshalled request (agent_instance_id is a
	// reliable field to check for).
	if !strings.Contains(args[pIdx+1], "test-agent#1") {
		t.Errorf("want -p value to contain agent_instance_id, got %q", args[pIdx+1])
	}
}

// TestClaudeCodeAdapter_OrdinaryInvocation_IncludesOutputFormatJSON verifies
// that ordinary invocations include --output-format json.
func TestClaudeCodeAdapter_OrdinaryInvocation_IncludesOutputFormatJSON(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want --output-format json in args, got %v", args)
	}
}

// TestClaudeCodeAdapter_OrdinaryInvocation_IncludesPermissionModeAuto verifies
// that ordinary invocations include --permission-mode auto.
func TestClaudeCodeAdapter_OrdinaryInvocation_IncludesPermissionModeAuto(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--permission-mode", "auto") {
		t.Errorf("want --permission-mode auto in args, got %v", args)
	}
}

// TestClaudeCodeAdapter_OrdinaryInvocation_IncludesNoSessionPersistence
// verifies that ordinary invocations include --no-session-persistence.
func TestClaudeCodeAdapter_OrdinaryInvocation_IncludesNoSessionPersistence(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsArg(args, "--no-session-persistence") {
		t.Errorf("want --no-session-persistence in args, got %v", args)
	}
}

// TestClaudeCodeAdapter_OrdinaryInvocation_NeverDangerouslySkipPermissions
// verifies that --dangerously-skip-permissions is never present in ordinary
// invocation arguments.
func TestClaudeCodeAdapter_OrdinaryInvocation_NeverDangerouslySkipPermissions(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
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

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
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
	agent := orchestratorAgentRef()

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
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

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
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

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want --output-format json in orchestrator args, got %v", args)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_IncludesPermissionModeAuto
// verifies that orchestrator invocations include --permission-mode auto.
func TestClaudeCodeAdapter_OrchestratorInvocation_IncludesPermissionModeAuto(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsSequence(args, "--permission-mode", "auto") {
		t.Errorf("want --permission-mode auto in orchestrator args, got %v", args)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_IncludesNoSessionPersistence
// verifies that orchestrator invocations include --no-session-persistence.
func TestClaudeCodeAdapter_OrchestratorInvocation_IncludesNoSessionPersistence(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	if !containsArg(args, "--no-session-persistence") {
		t.Errorf("want --no-session-persistence in orchestrator args, got %v", args)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_NeverDangerouslySkipPermissions
// verifies that --dangerously-skip-permissions is never present in orchestrator
// invocation arguments.
func TestClaudeCodeAdapter_OrchestratorInvocation_NeverDangerouslySkipPermissions(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
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
// -p prompt content.
func TestClaudeCodeAdapter_OrchestratorInvocation_IncludesEnvBlock(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	prompt := args[pIdx+1]
	if !strings.Contains(prompt, "<env>") {
		t.Errorf("want <env> opening tag in orchestrator -p prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "</env>") {
		t.Errorf("want </env> closing tag in orchestrator -p prompt, got %q", prompt)
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_EnvBlockContainsWorkingDir
// verifies that the synthesized <env> block includes the working directory.
func TestClaudeCodeAdapter_OrchestratorInvocation_EnvBlockContainsWorkingDir(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	if !strings.Contains(args[pIdx+1], "Working directory:") {
		t.Errorf("want 'Working directory:' in env block, got %q", args[pIdx+1])
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_EnvBlockContainsPlatform
// verifies that the synthesized <env> block includes the platform (runtime.GOOS).
func TestClaudeCodeAdapter_OrchestratorInvocation_EnvBlockContainsPlatform(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	if !strings.Contains(args[pIdx+1], "Platform:") {
		t.Errorf("want 'Platform:' in env block, got %q", args[pIdx+1])
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_EnvBlockContainsDate verifies
// that the synthesized <env> block includes the current date in the format
// required by the design spec ("Current date: YYYY-MM-DD").
func TestClaudeCodeAdapter_OrchestratorInvocation_EnvBlockContainsDate(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	if !strings.Contains(args[pIdx+1], "Current date:") {
		t.Errorf("want 'Current date:' in env block, got %q", args[pIdx+1])
	}
}

// TestClaudeCodeAdapter_OrchestratorInvocation_IncludesRequestInPrompt verifies
// that the marshalled request JSON is present in the orchestrator -p value.
// An implementation that emits only the <env> block and omits the request would
// fail this test.
func TestClaudeCodeAdapter_OrchestratorInvocation_IncludesRequestInPrompt(t *testing.T) {
	argsFile := setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readArgs(t, argsFile)
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	// agent_instance_id is a reliable field from the marshalled request JSON.
	if !strings.Contains(args[pIdx+1], "orchestrator-agent#1") {
		t.Errorf("want marshalled request JSON (containing agent_instance_id) in orchestrator -p prompt, got %q", args[pIdx+1])
	}
}

// ---------------------------------------------------------------------------
// Subprocess lifecycle
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_ContextCancellation_ReturnsCtxErr verifies that
// cancelling the context causes Invoke to terminate the subprocess and return
// ctx.Err().
func TestClaudeCodeAdapter_ContextCancellation_ReturnsCtxErr(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 30*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := adapter.Invoke(ctx, ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

// TestClaudeCodeAdapter_Timeout_ReturnsErrTimeout verifies that an invocation
// that exceeds the configured timeout returns ErrTimeout.
func TestClaudeCodeAdapter_Timeout_ReturnsErrTimeout(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "hang")

	// Very short timeout so the test completes quickly.
	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 100*time.Millisecond)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrTimeout) {
		t.Errorf("want ErrTimeout, got %v", err)
	}
}

// TestClaudeCodeAdapter_MissingExecutable_ReturnsErrExecutableNotFound verifies
// that invoking with a nonexistent executable path returns ErrExecutableNotFound.
func TestClaudeCodeAdapter_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	adapter := harness.NewClaudeCodeAdapter("/nonexistent/path/to/claude", 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrExecutableNotFound) {
		t.Errorf("want ErrExecutableNotFound, got %v", err)
	}
}

// TestClaudeCodeAdapter_NonZeroExit_ReturnsErrNonZeroExit verifies that a
// subprocess exiting with a non-zero status returns ErrNonZeroExit.
func TestClaudeCodeAdapter_NonZeroExit_ReturnsErrNonZeroExit(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "exit1")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrNonZeroExit) {
		t.Errorf("want ErrNonZeroExit, got %v", err)
	}
}

// TestClaudeCodeAdapter_NonZeroExit_ErrorContainsStderr verifies that the
// ErrNonZeroExit error carries the subprocess stderr content for traceability.
func TestClaudeCodeAdapter_NonZeroExit_ErrorContainsStderr(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "exit1")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	// "simulated stderr output" is written by the helper's exit1 case.
	if !strings.Contains(err.Error(), "simulated stderr output") {
		t.Errorf("want error message to include stderr content, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// CLI output parsing
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_ValidEnvelope_ReturnsParsedResponse verifies that a
// valid --output-format json envelope containing a Communication Protocol
// response is correctly parsed into a domain.ProtocolResponse.
func TestClaudeCodeAdapter_ValidEnvelope_ReturnsParsedResponse(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "success")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode=SUCCESS, got %q", resp.StatusCode)
	}
	if resp.AgentInstanceID != "test-agent#1" {
		t.Errorf("want AgentInstanceID=test-agent#1, got %q", resp.AgentInstanceID)
	}
}

// TestClaudeCodeAdapter_EmptyOutput_ReturnsErrEmptyResponse verifies that
// empty CLI stdout returns ErrEmptyResponse.
func TestClaudeCodeAdapter_EmptyOutput_ReturnsErrEmptyResponse(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "empty")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

// TestClaudeCodeAdapter_NonJSONOutput_ReturnsErrMalformedJSON verifies that
// CLI stdout that is not valid JSON returns ErrMalformedJSON.
func TestClaudeCodeAdapter_NonJSONOutput_ReturnsErrMalformedJSON(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "bad-json")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrMalformedJSON) {
		t.Errorf("want ErrMalformedJSON, got %v", err)
	}
}

// TestClaudeCodeAdapter_ValidJSONNoProtocolResponse_ReturnsErrMalformedOutput
// verifies that valid JSON CLI output with no extractable Communication Protocol
// response returns ErrMalformedOutput.
func TestClaudeCodeAdapter_ValidJSONNoProtocolResponse_ReturnsErrMalformedOutput(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "bad-envelope")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrMalformedOutput) {
		t.Errorf("want ErrMalformedOutput, got %v", err)
	}
}
