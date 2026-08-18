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
//   - Non-JSON CLI stdout error contains no internal Go type name "cliEnvelopeEntry".
//   - Valid JSON envelope with no extractable protocol response returns
//     ErrMalformedOutput.
//
//   Envelope recovery (bare and embedded JSON objects):
//   - Bare JSON object that is a valid protocol response is recovered successfully.
//   - Valid protocol response embedded in surrounding CLI text is recovered.
//
//   Envelope recovery — unrecoverable cases (formerly direct, in-package
//   coverage of the unexported parseEnvelope helper; now asserted only
//   through Invoke because that helper is moving to mosaic-common/harness):
//   - Bare JSON object missing required protocol fields returns ErrMalformedJSON
//     and the error contains the raw CLI output.
//   - Large unrecoverable output is truncated with a visible "[truncated" indicator.
//
//   Debug log raw-content gap-fill:
//   - An extractable protocol response that fails to decode into
//     domain.ProtocolResponse logs the raw content to the debug log.
//   - A valid envelope with no extractable protocol response logs the raw
//     content to the debug log.
//   - Raw content reaches the debug log untruncated for payloads exceeding
//     the shared package's 2000-byte error-string cap.
//   - The pre-existing non-JSON-output logging behaviour is unchanged: the
//     event is logged exactly once, with no duplicated raw payload.
//   - A successful invocation returning a realistic object-shaped CLI
//     envelope logs no parse-recovered event.
//   - Bare and embedded protocol responses still log a parse-recovered event
//     after the envelope-shape classification is widened.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

	// Capture stdin if the test requested it. The prompt now travels on
	// stdin rather than as a -p argv value, so tests that verify prompt
	// delivery read this file instead of args[pIdx+1].
	if stdinFile := os.Getenv("GO_HELPER_STDIN_FILE"); stdinFile != "" {
		stdinData, _ := io.ReadAll(os.Stdin)
		os.WriteFile(stdinFile, stdinData, 0644) //nolint:errcheck
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

	case "bare-json":
		// Write a bare JSON object (not a JSON array) that is a valid
		// Communication Protocol response. The adapter should recover it.
		os.Stdout.WriteString(`{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`) //nolint:errcheck

	case "embedded-json":
		// Write CLI noise with a valid protocol response embedded within it.
		// The adapter should scan and recover the embedded response.
		os.Stdout.WriteString("Warning: pre-flight check failed\n")                                                 //nolint:errcheck
		os.Stdout.WriteString(`{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`) //nolint:errcheck
		os.Stdout.WriteString("\nCLI exiting\n")                                                                    //nolint:errcheck

	case "json-object-no-protocol-fields":
		// Write a bare JSON object missing the required protocol fields
		// (agent_instance_id, status_code). Not recoverable: exercises the
		// "recovery also fails" branch for an object that IS valid JSON but
		// isn't a protocol response.
		os.Stdout.WriteString(`{"hello":"world","foo":"bar"}`) //nolint:errcheck

	case "large-garbage":
		// Write output well beyond the 2000-character readability cap, none
		// of it JSON. Exercises the truncation-with-indicator behaviour.
		os.Stdout.WriteString(strings.Repeat("x", 3000)) //nolint:errcheck

	case "protocol-decode-fail":
		// Write a bare JSON object that IS extractable as a Communication
		// Protocol candidate (agent_instance_id and status_code are present
		// as strings, so it passes the shared package's recognition check)
		// but fails to decode into domain.ProtocolResponse: error_code is a
		// number where the target field is string-typed. Exercises the
		// ErrMalformedOutput path inside the adapter itself.
		os.Stdout.WriteString(`{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok","error_code":12345}`) //nolint:errcheck

	case "bad-envelope-large":
		// Write a valid JSON array with no "result"-type entry, the same
		// shape as "bad-envelope", but padded well beyond the shared
		// package's 2000-byte error-string cap. Exercises untruncated
		// raw-content capture on the ErrProtocolNotExtractable path.
		os.Stdout.WriteString(`[{"type":"assistant","message":"` + strings.Repeat("x", 3000) + `"}]`) //nolint:errcheck

	case "object-envelope":
		// Write a realistic single-object CLI envelope (the shape Claude
		// Code actually emits for an ordinary turn): a "type":"result"
		// object carrying a "result" field with the protocol response text,
		// surrounded by other CLI-emitted fields. A successful invocation
		// against this must be classified as a normal envelope, not a
		// recovery.
		type objectEnvelope struct {
			IsError    bool   `json:"is_error"`
			SessionID  string `json:"session_id"`
			Subtype    string `json:"subtype"`
			Type       string `json:"type"`
			Result     string `json:"result"`
			DurationMs int    `json:"duration_ms"`
		}
		protocolResp := `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`
		envelope := objectEnvelope{
			IsError:    false,
			SessionID:  "session-abc123",
			Subtype:    "success",
			Type:       "result",
			Result:     protocolResp,
			DurationMs: 15756,
		}
		data, _ := json.Marshal(envelope)
		os.Stdout.Write(data) //nolint:errcheck

	case "opencode-success":
		// Produce a valid `opencode run --format json` event stream: one text
		// event carrying the protocol response, terminated by a step_finish
		// whose reason is "stop" (success). Exit 0.
		protocolResp := `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`
		os.Stdout.WriteString(`{"type":"step_start"}` + "\n")                                                         //nolint:errcheck
		os.Stdout.WriteString(`{"type":"text","part":{"type":"text","text":` + opencodeJSONQuote(protocolResp) + `}}` + "\n") //nolint:errcheck
		os.Stdout.WriteString(`{"type":"step_finish","part":{"reason":"stop"}}` + "\n")                               //nolint:errcheck

	case "opencode-stream-error":
		// Simulate the zero-exit trap: the process exits 0 but the event
		// stream itself reports failure via an "error" event.
		os.Stdout.WriteString(`{"type":"error","error":{"name":"SomeError","data":{"message":"simulated failure"}}}` + "\n") //nolint:errcheck

	case "opencode-incomplete":
		// Emit a recognised event but never a terminal one. Exit 0.
		os.Stdout.WriteString(`{"type":"step_start"}` + "\n") //nolint:errcheck

	case "ghcpcli-success":
		// Produce a valid GHCP CLI JSONL event stream: one assistant.message
		// event carrying the protocol response as data.content, terminated by
		// a result event with exitCode:0 at the top level (not inside data).
		// The process exits 0.
		protocolResp := `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`
		os.Stdout.WriteString(`{"type":"assistant.message","data":{"content":` + opencodeJSONQuote(protocolResp) + `}}` + "\n") //nolint:errcheck
		os.Stdout.WriteString(`{"type":"result","exitCode":0}` + "\n")                                                          //nolint:errcheck

	case "ghcpcli-stream-error":
		// Simulate the GHCP CLI stream-verdict failure: the result event has a
		// non-zero exitCode at the top level, while the process itself exits 0.
		// The adapter must report ErrGHCPCLIStreamError, not success.
		os.Stderr.WriteString("Error: simulated ghcp-cli failure") //nolint:errcheck
		os.Stdout.WriteString(`{"type":"assistant.message_delta","data":{"deltaContent":"partial"}}` + "\n") //nolint:errcheck
		os.Stdout.WriteString(`{"type":"result","exitCode":1}` + "\n")                                       //nolint:errcheck

	case "ghcpcli-incomplete":
		// Emit a recognised assistant.message event but no terminal result
		// event. Exit 0. The adapter must report ErrGHCPCLIStreamIncomplete
		// rather than treating the absence of a result as success.
		os.Stdout.WriteString(`{"type":"assistant.message","data":{"content":"partial"}}` + "\n") //nolint:errcheck

	case "ghcpcli-decode-fail":
		// Produce a stream that the GHCP CLI parser accepts as successful
		// (result line with exitCode:0) but whose data.content, once
		// accumulated and extracted, is a Communication Protocol candidate that
		// cannot be decoded into domain.ProtocolResponse: error_code is a
		// number where the target field is string-typed. Exercises the
		// ErrMalformedOutput path inside GHCPCLIAdapter.
		badProto := `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok","error_code":99999}`
		os.Stdout.WriteString(`{"type":"assistant.message","data":{"content":` + opencodeJSONQuote(badProto) + `}}` + "\n") //nolint:errcheck
		os.Stdout.WriteString(`{"type":"result","exitCode":0}` + "\n")                                                       //nolint:errcheck

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

// opencodeJSONQuote renders s as a JSON string literal (quotes and escaping
// included), so an OpenCode helper case can embed arbitrary text inside a
// hand-written event-stream line without hand-escaping it.
func opencodeJSONQuote(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

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

// setupStdinCapture configures the current process environment so that any
// subprocess spawned by the adapter will write its received stdin to a file.
// The prompt content travels on stdin (not as a -p argv value) since the
// shared harness changed to stdin-based delivery to prevent truncation on
// Windows. All env changes are reverted by t.Cleanup via t.Setenv.
func setupStdinCapture(t *testing.T) (stdinFile string) {
	t.Helper()
	stdinFile = filepath.Join(t.TempDir(), "stdin.bin")
	t.Setenv("GO_HELPER_STDIN_FILE", stdinFile)
	return stdinFile
}

// readHelperStdin reads the stdin capture file written by the helper subprocess.
// Returns the raw bytes the adapter delivered on the subprocess's standard input.
func readHelperStdin(t *testing.T, stdinFile string) []byte {
	t.Helper()
	data, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("readHelperStdin: could not read stdin file %q: %v", stdinFile, err)
	}
	return data
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
// ordinary invocations include -p as a bare flag and deliver the marshalled
// request JSON on stdin. The prompt travels on stdin rather than as the value
// following -p, so that multi-line prompts are not truncated by cmd.exe on Windows.
func TestClaudeCodeAdapter_OrdinaryInvocation_IncludesPromptFlag(t *testing.T) {
	argsFile := setHelperEnv(t, "success")
	stdinFile := setupStdinCapture(t)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
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
// stdin payload. The env block and prompt content travel on stdin rather than
// as the value following -p, so that multi-line content is not truncated by
// cmd.exe on Windows.
func TestClaudeCodeAdapter_OrchestratorInvocation_IncludesEnvBlock(t *testing.T) {
	argsFile := setHelperEnv(t, "success")
	stdinFile := setupStdinCapture(t)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
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
	stdinFile := setupStdinCapture(t)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
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
	stdinFile := setupStdinCapture(t)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
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
	stdinFile := setupStdinCapture(t)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
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
	stdinFile := setupStdinCapture(t)

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)
	_, err := adapter.Invoke(context.Background(), orchestratorAgentRef(), minimalClaudeRequest("orchestrator-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// agent_instance_id is a reliable field from the marshalled request JSON.
	stdin := string(readHelperStdin(t, stdinFile))
	if !strings.Contains(stdin, "orchestrator-agent#1") {
		t.Errorf("want marshalled request JSON (containing agent_instance_id) in orchestrator stdin payload, got %q", stdin)
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

// TestClaudeCodeAdapter_NonJSONOutput_ErrorHasNoGoTypeName verifies that when
// the CLI produces output that is not valid JSON and cannot be recovered, the
// error message does not contain the internal Go type name "cliEnvelopeEntry".
// That string originates in encoding/json's unmarshal error and must be
// suppressed so the user sees a readable diagnostic.
func TestClaudeCodeAdapter_NonJSONOutput_ErrorHasNoGoTypeName(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "bad-json")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	if strings.Contains(err.Error(), "cliEnvelopeEntry") {
		t.Errorf("want error message free of internal Go type name 'cliEnvelopeEntry', got: %q", err.Error())
	}
}

// TestClaudeCodeAdapter_JSONObjectWithoutProtocolFields_ReturnsErrMalformedOutput
// verifies that a bare JSON object that IS valid JSON but is missing the
// required protocol fields (agent_instance_id, status_code) is not
// recoverable and returns an error wrapping ErrMalformedJSON.
//
// This test (and the two below it) supersede what was previously direct,
// in-package coverage of the unexported parseEnvelope helper. That helper is
// moving to mosaic-common/harness, so this behaviour is now asserted only
// through Invoke — the adapter's observable behaviour — which is what keeps
// this suite compiling and passing once the adapter starts delegating.
func TestClaudeCodeAdapter_JSONObjectWithoutProtocolFields_ReturnsErrMalformedOutput(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "json-object-no-protocol-fields")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrMalformedJSON) {
		t.Errorf("want ErrMalformedJSON, got %v", err)
	}
}

// TestClaudeCodeAdapter_JSONObjectWithoutProtocolFields_ErrorContainsRawOutput
// verifies that the error carries the raw CLI output for traceability.
func TestClaudeCodeAdapter_JSONObjectWithoutProtocolFields_ErrorContainsRawOutput(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "json-object-no-protocol-fields")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), `{"hello":"world","foo":"bar"}`) {
		t.Errorf("want error to contain the raw CLI output, got %q", err.Error())
	}
}

// TestClaudeCodeAdapter_LargeUnrecoverableOutput_TruncatedWithIndicator
// verifies that when unrecoverable CLI output exceeds the 2000-character
// readability cap, the error message contains a visible truncation
// indicator beginning with "[truncated".
func TestClaudeCodeAdapter_LargeUnrecoverableOutput_TruncatedWithIndicator(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "large-garbage")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrMalformedJSON) {
		t.Errorf("want ErrMalformedJSON, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "[truncated") {
		t.Errorf("want a truncation indicator '[truncated' in the error message, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Envelope recovery integration tests
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_BareJSONProtocolResponse_Recovered verifies that when
// the CLI outputs a bare JSON object (not a JSON array) that is a valid
// Communication Protocol response, Invoke recovers it and returns the parsed
// response rather than ErrMalformedJSON.
func TestClaudeCodeAdapter_BareJSONProtocolResponse_Recovered(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "bare-json")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if err != nil {
		t.Fatalf("want successful recovery of bare JSON protocol response, got error: %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode=SUCCESS, got %q", resp.StatusCode)
	}
	if resp.AgentInstanceID != "test-agent#1" {
		t.Errorf("want AgentInstanceID=test-agent#1, got %q", resp.AgentInstanceID)
	}
}

// TestClaudeCodeAdapter_EmbeddedProtocolResponseInCliText_Recovered verifies
// that when the CLI outputs surrounding non-JSON text with a valid
// Communication Protocol response embedded within it, Invoke recovers the
// protocol response and returns it rather than ErrMalformedJSON.
func TestClaudeCodeAdapter_EmbeddedProtocolResponseInCliText_Recovered(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "embedded-json")

	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if err != nil {
		t.Fatalf("want successful recovery of embedded protocol response, got error: %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode=SUCCESS, got %q", resp.StatusCode)
	}
	if resp.AgentInstanceID != "test-agent#1" {
		t.Errorf("want AgentInstanceID=test-agent#1, got %q", resp.AgentInstanceID)
	}
}

// ---------------------------------------------------------------------------
// recordingLogger — fake domain.DebugLogger for logging tests (T5.1, T5.2, T5.6)
// ---------------------------------------------------------------------------

// harnessLogEntry is one captured call to recordingLogger.Log.
type harnessLogEntry struct {
	Event   string
	Message string
	Fields  []domain.DebugField
}

// recordingLogger is a thread-safe domain.DebugLogger that records every Log
// call. Tests assert on the recorded entries after invoking the adapter.
type recordingLogger struct {
	mu      sync.Mutex
	entries []harnessLogEntry
}

// Log implements domain.DebugLogger.
func (r *recordingLogger) Log(event string, message string, fields ...domain.DebugField) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, harnessLogEntry{
		Event:   event,
		Message: message,
		Fields:  append([]domain.DebugField{}, fields...),
	})
}

// eventLogged reports whether at least one entry with the given event name
// was recorded.
func (r *recordingLogger) eventLogged(event string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event == event {
			return true
		}
	}
	return false
}

// fieldValue returns the value for the given field key in the first entry
// with the given event name. Returns ("", false) when not found.
func (r *recordingLogger) fieldValue(event, key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event != event {
			continue
		}
		for _, f := range e.Fields {
			if f.Key == key {
				return f.Value, true
			}
		}
	}
	return "", false
}

// allEvents returns the event names of all recorded entries in order.
func (r *recordingLogger) allEvents() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, len(r.entries))
	for i, e := range r.entries {
		names[i] = e.Event
	}
	return names
}

// messageFor returns the message of the first recorded entry with the given
// event name. Returns ("", false) when no such entry was recorded.
func (r *recordingLogger) messageFor(event string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event == event {
			return e.Message, true
		}
	}
	return "", false
}

// countEvent returns how many entries were recorded for the given event name.
func (r *recordingLogger) countEvent(event string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.entries {
		if e.Event == event {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// T5.1 — adapter logs invocation start, raw stdout, raw stderr, and outcome
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsInvokeStart verifies
// that NewClaudeCodeAdapterWithLogger emits EventHarnessInvokeStart for each
// call to Invoke.
func TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsInvokeStart(t *testing.T) {
	setHelperEnv(t, "success")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !logger.eventLogged(domain.EventHarnessInvokeStart) {
		t.Errorf("want %s logged, got events: %v", domain.EventHarnessInvokeStart, logger.allEvents())
	}
}

// TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsRawStdout verifies
// that EventHarnessStdout is logged after the subprocess exits, capturing the
// raw stdout content.
func TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsRawStdout(t *testing.T) {
	setHelperEnv(t, "success")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessStdout) {
		t.Errorf("want %s logged after process exits, got events: %v", domain.EventHarnessStdout, logger.allEvents())
	}
}

// TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsRawStderr verifies
// that EventHarnessStderr is logged after the subprocess exits, even when
// stderr is empty (an empty log entry is still expected per invocation).
func TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsRawStderr(t *testing.T) {
	setHelperEnv(t, "success")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessStderr) {
		t.Errorf("want %s logged after process exits, got events: %v", domain.EventHarnessStderr, logger.allEvents())
	}
}

// TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsInvokeOK verifies
// that a successful invocation logs EventHarnessInvokeOK and does not log
// EventHarnessInvokeError.
func TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsInvokeOK(t *testing.T) {
	setHelperEnv(t, "success")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !logger.eventLogged(domain.EventHarnessInvokeOK) {
		t.Errorf("want %s logged on success, got events: %v", domain.EventHarnessInvokeOK, logger.allEvents())
	}
	if logger.eventLogged(domain.EventHarnessInvokeError) {
		t.Errorf("want no %s on success, but it was logged", domain.EventHarnessInvokeError)
	}
}

// TestClaudeCodeAdapterWithLogger_NonZeroExit_LogsInvokeError verifies that a
// non-zero subprocess exit logs EventHarnessInvokeError and does not log
// EventHarnessInvokeOK.
func TestClaudeCodeAdapterWithLogger_NonZeroExit_LogsInvokeError(t *testing.T) {
	setHelperEnv(t, "exit1")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessInvokeError) {
		t.Errorf("want %s logged on non-zero exit, got events: %v", domain.EventHarnessInvokeError, logger.allEvents())
	}
	if logger.eventLogged(domain.EventHarnessInvokeOK) {
		t.Errorf("want no %s on error, but it was logged", domain.EventHarnessInvokeOK)
	}
}

// TestClaudeCodeAdapterWithLogger_InvokeStart_CarriesAgentAndKindFields
// verifies that the EventHarnessInvokeStart entry carries the agent instance ID
// (from the request) and the invocation kind as structured fields.
func TestClaudeCodeAdapterWithLogger_InvokeStart_CarriesAgentAndKindFields(t *testing.T) {
	setHelperEnv(t, "success")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	agentVal, ok := logger.fieldValue(domain.EventHarnessInvokeStart, "agent")
	if !ok {
		t.Errorf("want 'agent' field on %s entry; events: %v",
			domain.EventHarnessInvokeStart, logger.allEvents())
	} else if agentVal != "test-agent#1" {
		t.Errorf("want agent=test-agent#1 on %s, got %q", domain.EventHarnessInvokeStart, agentVal)
	}

	kindVal, ok := logger.fieldValue(domain.EventHarnessInvokeStart, "kind")
	if !ok {
		t.Errorf("want 'kind' field on %s entry", domain.EventHarnessInvokeStart)
	} else if kindVal != "ordinary" {
		t.Errorf("want kind=ordinary on %s, got %q", domain.EventHarnessInvokeStart, kindVal)
	}
}

// ---------------------------------------------------------------------------
// T5.2 — adapter logs envelope parse failure and recovery
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapterWithLogger_NonJSONOutput_LogsParseFailed verifies that
// when the CLI outputs non-JSON text and recovery also fails, the adapter logs
// EventHarnessParseFailed. The error return remains ErrMalformedJSON.
func TestClaudeCodeAdapterWithLogger_NonJSONOutput_LogsParseFailed(t *testing.T) {
	setHelperEnv(t, "bad-json")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessParseFailed) {
		t.Errorf("want %s logged on non-JSON output with failed recovery, got events: %v",
			domain.EventHarnessParseFailed, logger.allEvents())
	}
}

// TestClaudeCodeAdapterWithLogger_BareJSONRecovery_LogsParseRecovered verifies
// that when the CLI outputs a bare JSON object that is a valid protocol response
// and recovery succeeds, EventHarnessParseRecovered is logged.
func TestClaudeCodeAdapterWithLogger_BareJSONRecovery_LogsParseRecovered(t *testing.T) {
	setHelperEnv(t, "bare-json")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error (bare-JSON recovery should succeed): %v", err)
	}

	if !logger.eventLogged(domain.EventHarnessParseRecovered) {
		t.Errorf("want %s logged after successful bare-JSON recovery, got events: %v",
			domain.EventHarnessParseRecovered, logger.allEvents())
	}
}

// TestClaudeCodeAdapterWithLogger_EmbeddedJSONRecovery_LogsParseRecovered
// verifies that when a valid protocol response embedded in surrounding CLI text
// is recovered, EventHarnessParseRecovered is logged.
func TestClaudeCodeAdapterWithLogger_EmbeddedJSONRecovery_LogsParseRecovered(t *testing.T) {
	setHelperEnv(t, "embedded-json")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error (embedded-JSON recovery should succeed): %v", err)
	}

	if !logger.eventLogged(domain.EventHarnessParseRecovered) {
		t.Errorf("want %s logged after successful embedded-JSON recovery, got events: %v",
			domain.EventHarnessParseRecovered, logger.allEvents())
	}
}

// ---------------------------------------------------------------------------
// T5.6 — nil logger defaults to no-op; adapter behaviour unchanged
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapterWithLogger_NilLogger_NormalisedToNop verifies that
// passing nil as the logger is safe: nil is normalised to domain.NopDebugLogger,
// Invoke does not panic, and the response is correct.
func TestClaudeCodeAdapterWithLogger_NilLogger_NormalisedToNop(t *testing.T) {
	setHelperEnv(t, "success")

	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, nil)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("want no error with nil logger, got %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want SUCCESS with nil logger, got %q", resp.StatusCode)
	}
}

// TestClaudeCodeAdapter_BasicConstructor_UnchangedBehaviourAfterLoggerAdded
// verifies that the original two-argument NewClaudeCodeAdapter constructor
// continues to produce correct results after the logger field was introduced.
// This regression guard ensures all existing call sites keep working unchanged.
func TestClaudeCodeAdapter_BasicConstructor_UnchangedBehaviourAfterLoggerAdded(t *testing.T) {
	setHelperEnv(t, "success")
	adapter := harness.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("want no error with basic constructor, got %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want SUCCESS with basic constructor, got %q", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Debug log raw-content gap-fill
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapterWithLogger_ProtocolDecodeFailure_LogsRawContent
// verifies that when the CLI output IS extractable as a Communication
// Protocol candidate but fails to decode into domain.ProtocolResponse, the
// adapter logs the raw content to the debug log rather than leaving only the
// short wrapped error message.
func TestClaudeCodeAdapterWithLogger_ProtocolDecodeFailure_LogsRawContent(t *testing.T) {
	setHelperEnv(t, "protocol-decode-fail")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrMalformedOutput) {
		t.Fatalf("want ErrMalformedOutput, got %v", err)
	}

	msg, ok := logger.messageFor(domain.EventHarnessParseFailed)
	if !ok {
		t.Fatalf("want %s logged when the extracted protocol response fails to decode, got events: %v",
			domain.EventHarnessParseFailed, logger.allEvents())
	}
	if !strings.Contains(msg, `"error_code":12345`) {
		t.Errorf("want logged message to contain the raw undecodable content, got %q", msg)
	}
}

// TestClaudeCodeAdapterWithLogger_ProtocolNotExtractable_LogsRawContent
// verifies that when the CLI produces a valid envelope with no extractable
// Communication Protocol response, the adapter logs the raw content to the
// debug log. Before this behaviour exists, only a short wrapped error is
// logged under EventHarnessInvokeError and no raw content reaches the log.
func TestClaudeCodeAdapterWithLogger_ProtocolNotExtractable_LogsRawContent(t *testing.T) {
	setHelperEnv(t, "bad-envelope")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrMalformedOutput) {
		t.Fatalf("want ErrMalformedOutput, got %v", err)
	}

	msg, ok := logger.messageFor(domain.EventHarnessParseFailed)
	if !ok {
		t.Fatalf("want %s logged when no protocol response is extractable, got events: %v",
			domain.EventHarnessParseFailed, logger.allEvents())
	}
	if !strings.Contains(msg, `"type":"assistant","message":"hello there"`) {
		t.Errorf("want logged message to contain the raw CLI output, got %q", msg)
	}
}

// TestClaudeCodeAdapterWithLogger_ProtocolNotExtractable_LargePayloadUntruncated
// verifies that raw content reaching the debug log for the
// ErrProtocolNotExtractable path is not bounded by the shared package's
// 2000-byte error-string cap: a payload well beyond that cap must reach the
// log in full.
func TestClaudeCodeAdapterWithLogger_ProtocolNotExtractable_LargePayloadUntruncated(t *testing.T) {
	setHelperEnv(t, "bad-envelope-large")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrMalformedOutput) {
		t.Fatalf("want ErrMalformedOutput, got %v", err)
	}

	msg, ok := logger.messageFor(domain.EventHarnessParseFailed)
	if !ok {
		t.Fatalf("want %s logged for the large unrecoverable payload, got events: %v",
			domain.EventHarnessParseFailed, logger.allEvents())
	}
	if strings.Contains(msg, "[truncated") {
		t.Errorf("want no truncation indicator in the debug-log message, got %q", msg)
	}
	if !strings.Contains(msg, strings.Repeat("x", 3000)) {
		t.Errorf("want the full 3000-byte payload present in the debug-log message untruncated")
	}
}

// TestClaudeCodeAdapterWithLogger_MalformedJSON_NoDuplicateRawContentLog is a
// regression guard: the already-covered ErrMalformedJSON path must keep
// logging EventHarnessParseFailed exactly once per invocation. Gap-filling
// the two other unrecoverable paths must not introduce a second, duplicated
// raw-content log entry on this pre-existing path.
func TestClaudeCodeAdapterWithLogger_MalformedJSON_NoDuplicateRawContentLog(t *testing.T) {
	setHelperEnv(t, "bad-json")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, harness.ErrMalformedJSON) {
		t.Fatalf("want ErrMalformedJSON, got %v", err)
	}

	if got := logger.countEvent(domain.EventHarnessParseFailed); got != 1 {
		t.Errorf("want exactly one %s entry, got %d: %v",
			domain.EventHarnessParseFailed, got, logger.allEvents())
	}
}

// TestClaudeCodeAdapterWithLogger_ObjectShapedEnvelope_NoParseRecoveredLogged
// verifies that a successful invocation returning a realistic object-shaped
// CLI envelope (the shape Claude Code actually emits: a single "type":"result"
// object, not a JSON array) is classified as a normal envelope, not a
// recovery: no EventHarnessParseRecovered entry is logged, and the response
// is still parsed correctly.
func TestClaudeCodeAdapterWithLogger_ObjectShapedEnvelope_NoParseRecoveredLogged(t *testing.T) {
	setHelperEnv(t, "object-envelope")
	logger := &recordingLogger{}
	adapter := harness.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), minimalClaudeRequest("test-agent#1"))

	if err != nil {
		t.Fatalf("want no error for a successful object-shaped envelope, got %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode=SUCCESS, got %q", resp.StatusCode)
	}
	if resp.AgentInstanceID != "test-agent#1" {
		t.Errorf("want AgentInstanceID=test-agent#1, got %q", resp.AgentInstanceID)
	}
	if logger.eventLogged(domain.EventHarnessParseRecovered) {
		t.Errorf("want no %s for a genuine object-shaped envelope, got events: %v",
			domain.EventHarnessParseRecovered, logger.allEvents())
	}
}
