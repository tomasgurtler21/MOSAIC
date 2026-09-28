package ghcpcli_test

// Fake-CLI infrastructure shared by the adapter tests in this package.
//
// This test binary doubles as the fake CLI executable (claude, opencode,
// copilot) when the environment variable GO_WANT_HELPER_PROCESS=1 is set.
// TestMain intercepts the subprocess entry point before Go's test framework
// can attempt to parse the CLI flags as test flags. Individual tests set
// GO_WANT_HELPER_PROCESS=1 (via t.Setenv) so the value is inherited by
// subprocesses they spawn; the current test process itself started without
// the variable and is unaffected.
//
// GO_HELPER_CMD selects the scenario, GO_HELPER_ARGS_FILE receives the
// serialised argv, and GO_HELPER_STDIN_FILE (optional) receives stdin.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/domain"
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
	recordHelperArgs()
	captureHelperStdin()

	scenario, ok := helperScenarios[os.Getenv("GO_HELPER_CMD")]
	if !ok {
		os.Stderr.WriteString("unknown GO_HELPER_CMD: " + os.Getenv("GO_HELPER_CMD")) //nolint:errcheck
		os.Exit(2)
	}
	scenario()

	os.Exit(0)
}

// recordHelperArgs persists the received args before any output is produced.
func recordHelperArgs() {
	if argsFile := os.Getenv("GO_HELPER_ARGS_FILE"); argsFile != "" {
		data, _ := json.Marshal(os.Args[1:])
		os.WriteFile(argsFile, data, 0644) //nolint:errcheck
	}
}

// captureHelperStdin captures stdin if the test requested it. The prompt now
// travels on stdin rather than as a -p argv value, so tests that verify prompt
// delivery read this file instead of args[pIdx+1].
func captureHelperStdin() {
	if stdinFile := os.Getenv("GO_HELPER_STDIN_FILE"); stdinFile != "" {
		stdinData, _ := io.ReadAll(os.Stdin)
		os.WriteFile(stdinFile, stdinData, 0644) //nolint:errcheck
	}
}

// helperScenarios maps each GO_HELPER_CMD value to the scenario that fakes it.
var helperScenarios = map[string]func(){
	"success":                        helperScenarioSuccess,
	"exit1":                          helperScenarioExit1,
	"empty":                          helperScenarioEmpty,
	"bad-json":                       helperScenarioBadJson,
	"bad-envelope":                   helperScenarioBadEnvelope,
	"bare-json":                      helperScenarioBareJson,
	"embedded-json":                  helperScenarioEmbeddedJson,
	"json-object-no-protocol-fields": helperScenarioJsonObjectNoProtocolFields,
	"large-garbage":                  helperScenarioLargeGarbage,
	"protocol-decode-fail":           helperScenarioProtocolDecodeFail,
	"bad-envelope-large":             helperScenarioBadEnvelopeLarge,
	"object-envelope":                helperScenarioObjectEnvelope,
	"opencode-success":               helperScenarioOpencodeSuccess,
	"opencode-stream-error":          helperScenarioOpencodeStreamError,
	"opencode-incomplete":            helperScenarioOpencodeIncomplete,
	"ghcpcli-success":                helperScenarioGhcpcliSuccess,
	"ghcpcli-stream-error":           helperScenarioGhcpcliStreamError,
	"ghcpcli-incomplete":             helperScenarioGhcpcliIncomplete,
	"ghcpcli-decode-fail":            helperScenarioGhcpcliDecodeFail,
	"hang":                           helperScenarioHang,
}

// helperScenarioSuccess implements the GO_HELPER_CMD=success scenario.
func helperScenarioSuccess() {
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
}

// helperScenarioExit1 implements the GO_HELPER_CMD=exit1 scenario.
func helperScenarioExit1() {
	// Exit non-zero with recognisable stderr content.
	os.Stderr.WriteString("simulated stderr output") //nolint:errcheck
	os.Exit(1)
}

// helperScenarioEmpty implements the GO_HELPER_CMD=empty scenario.
func helperScenarioEmpty() {
	// Write nothing to stdout; exit zero.
}

// helperScenarioBadJson implements the GO_HELPER_CMD=bad-json scenario.
func helperScenarioBadJson() {
	// Write output that is not valid JSON.
	os.Stdout.WriteString("this is not JSON at all") //nolint:errcheck
}

// helperScenarioBadEnvelope implements the GO_HELPER_CMD=bad-envelope scenario.
func helperScenarioBadEnvelope() {
	// Write valid JSON that contains no "result" type entry and therefore
	// has no extractable protocol response.
	os.Stdout.WriteString(`[{"type":"assistant","message":"hello there"}]`) //nolint:errcheck
}

// helperScenarioBareJson implements the GO_HELPER_CMD=bare-json scenario.
func helperScenarioBareJson() {
	// Write a bare JSON object (not a JSON array) that is a valid
	// Communication Protocol response. The adapter should recover it.
	os.Stdout.WriteString(`{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`) //nolint:errcheck
}

// helperScenarioEmbeddedJson implements the GO_HELPER_CMD=embedded-json scenario.
func helperScenarioEmbeddedJson() {
	// Write CLI noise with a valid protocol response embedded within it.
	// The adapter should scan and recover the embedded response.
	os.Stdout.WriteString("Warning: pre-flight check failed\n")                                                 //nolint:errcheck
	os.Stdout.WriteString(`{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`) //nolint:errcheck
	os.Stdout.WriteString("\nCLI exiting\n")                                                                    //nolint:errcheck
}

// helperScenarioJsonObjectNoProtocolFields implements the GO_HELPER_CMD=json-object-no-protocol-fields scenario.
func helperScenarioJsonObjectNoProtocolFields() {
	// Write a bare JSON object missing the required protocol fields
	// (agent_instance_id, status_code). Not recoverable: exercises the
	// "recovery also fails" branch for an object that IS valid JSON but
	// isn't a protocol response.
	os.Stdout.WriteString(`{"hello":"world","foo":"bar"}`) //nolint:errcheck
}

// helperScenarioLargeGarbage implements the GO_HELPER_CMD=large-garbage scenario.
func helperScenarioLargeGarbage() {
	// Write output well beyond the 2000-character readability cap, none
	// of it JSON. Exercises the truncation-with-indicator behaviour.
	os.Stdout.WriteString(strings.Repeat("x", 3000)) //nolint:errcheck
}

// helperScenarioProtocolDecodeFail implements the GO_HELPER_CMD=protocol-decode-fail scenario.
func helperScenarioProtocolDecodeFail() {
	// Write a bare JSON object that IS extractable as a Communication
	// Protocol candidate (agent_instance_id and status_code are present
	// as strings, so it passes the shared package's recognition check)
	// but fails to decode into domain.ProtocolResponse: error_code is a
	// number where the target field is string-typed. Exercises the
	// ErrMalformedOutput path inside the adapter itself.
	os.Stdout.WriteString(`{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok","error_code":12345}`) //nolint:errcheck
}

// helperScenarioBadEnvelopeLarge implements the GO_HELPER_CMD=bad-envelope-large scenario.
func helperScenarioBadEnvelopeLarge() {
	// Write a valid JSON array with no "result"-type entry, the same
	// shape as "bad-envelope", but padded well beyond the shared
	// package's 2000-byte error-string cap. Exercises untruncated
	// raw-content capture on the ErrProtocolNotExtractable path.
	os.Stdout.WriteString(`[{"type":"assistant","message":"` + strings.Repeat("x", 3000) + `"}]`) //nolint:errcheck
}

// helperScenarioObjectEnvelope implements the GO_HELPER_CMD=object-envelope scenario.
func helperScenarioObjectEnvelope() {
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
}

// helperScenarioOpencodeSuccess implements the GO_HELPER_CMD=opencode-success scenario.
func helperScenarioOpencodeSuccess() {
	// Produce a valid `opencode run --format json` event stream: one text
	// event carrying the protocol response, terminated by a step_finish
	// whose reason is "stop" (success). Exit 0.
	protocolResp := `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`
	os.Stdout.WriteString(`{"type":"step_start"}` + "\n")                                                                 //nolint:errcheck
	os.Stdout.WriteString(`{"type":"text","part":{"type":"text","text":` + opencodeJSONQuote(protocolResp) + `}}` + "\n") //nolint:errcheck
	os.Stdout.WriteString(`{"type":"step_finish","part":{"reason":"stop"}}` + "\n")                                       //nolint:errcheck
}

// helperScenarioOpencodeStreamError implements the GO_HELPER_CMD=opencode-stream-error scenario.
func helperScenarioOpencodeStreamError() {
	// Simulate the zero-exit trap: the process exits 0 but the event
	// stream itself reports failure via an "error" event.
	os.Stdout.WriteString(`{"type":"error","error":{"name":"SomeError","data":{"message":"simulated failure"}}}` + "\n") //nolint:errcheck
}

// helperScenarioOpencodeIncomplete implements the GO_HELPER_CMD=opencode-incomplete scenario.
func helperScenarioOpencodeIncomplete() {
	// Emit a recognised event but never a terminal one. Exit 0.
	os.Stdout.WriteString(`{"type":"step_start"}` + "\n") //nolint:errcheck
}

// helperScenarioGhcpcliSuccess implements the GO_HELPER_CMD=ghcpcli-success scenario.
func helperScenarioGhcpcliSuccess() {
	// Produce a valid GHCP CLI JSONL event stream: one assistant.message
	// event carrying the protocol response as data.content, terminated by
	// a result event with exitCode:0 at the top level (not inside data).
	// The process exits 0.
	protocolResp := `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`
	os.Stdout.WriteString(`{"type":"assistant.message","data":{"content":` + opencodeJSONQuote(protocolResp) + `}}` + "\n") //nolint:errcheck
	os.Stdout.WriteString(`{"type":"result","exitCode":0}` + "\n")                                                          //nolint:errcheck
}

// helperScenarioGhcpcliStreamError implements the GO_HELPER_CMD=ghcpcli-stream-error scenario.
func helperScenarioGhcpcliStreamError() {
	// Simulate the GHCP CLI stream-verdict failure: the result event has a
	// non-zero exitCode at the top level, while the process itself exits 0.
	// The adapter must report ErrGHCPCLIStreamError, not success.
	os.Stderr.WriteString("Error: simulated ghcp-cli failure")                                           //nolint:errcheck
	os.Stdout.WriteString(`{"type":"assistant.message_delta","data":{"deltaContent":"partial"}}` + "\n") //nolint:errcheck
	os.Stdout.WriteString(`{"type":"result","exitCode":1}` + "\n")                                       //nolint:errcheck
}

// helperScenarioGhcpcliIncomplete implements the GO_HELPER_CMD=ghcpcli-incomplete scenario.
func helperScenarioGhcpcliIncomplete() {
	// Emit a recognised assistant.message event but no terminal result
	// event. Exit 0. The adapter must report ErrGHCPCLIStreamIncomplete
	// rather than treating the absence of a result as success.
	os.Stdout.WriteString(`{"type":"assistant.message","data":{"content":"partial"}}` + "\n") //nolint:errcheck
}

// helperScenarioGhcpcliDecodeFail implements the GO_HELPER_CMD=ghcpcli-decode-fail scenario.
func helperScenarioGhcpcliDecodeFail() {
	// Produce a stream that the GHCP CLI parser accepts as successful
	// (result line with exitCode:0) but whose data.content, once
	// accumulated and extracted, is a Communication Protocol candidate that
	// cannot be decoded into domain.ProtocolResponse: error_code is a
	// number where the target field is string-typed. Exercises the
	// ErrMalformedOutput path inside GHCPCLIAdapter.
	badProto := `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok","error_code":99999}`
	os.Stdout.WriteString(`{"type":"assistant.message","data":{"content":` + opencodeJSONQuote(badProto) + `}}` + "\n") //nolint:errcheck
	os.Stdout.WriteString(`{"type":"result","exitCode":0}` + "\n")                                                      //nolint:errcheck
}

// helperScenarioHang implements the GO_HELPER_CMD=hang scenario.
func helperScenarioHang() {
	// Block indefinitely so the test can exercise timeout and context
	// cancellation. time.Sleep avoids the goroutine-deadlock panic that
	// select{} would emit on stderr.
	time.Sleep(24 * time.Hour)
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

// newTestStdinCapture configures the current process environment so that any
// subprocess spawned by the adapter will write its received stdin to a file.
// The prompt content travels on stdin (not as a -p argv value) since the
// shared harness changed to stdin-based delivery to prevent truncation on
// Windows. All env changes are reverted by t.Cleanup via t.Setenv.
func newTestStdinCapture(t *testing.T) (stdinFile string) {
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

// ordinaryAgentRef returns an AgentReference with InvocationOrdinary using a
// static (non-existent) definition path. Used by adapter tests that do not
// exercise tool extraction (e.g., GHCP CLI and OpenCode adapters).
// ClaudeCodeAdapter tests must use ordinaryAgentRefCC(t) instead.
func ordinaryAgentRef() domain.AgentReference {
	return domain.AgentReference{
		Identifier:     "test-agent",
		DefinitionPath: "/agents/test-agent.md",
		InvocationKind: domain.InvocationOrdinary,
	}
}

// orchestratorAgentRef returns an AgentReference with InvocationOrchestrator
// using a static (non-existent) definition path. Used by adapter tests that do
// not exercise tool extraction. ClaudeCodeAdapter tests must use
// orchestratorAgentRefCC(t) instead.
func orchestratorAgentRef() domain.AgentReference {
	return domain.AgentReference{
		Identifier:     "orchestrator-agent",
		DefinitionPath: "/agents/orchestrator-agent.md",
		InvocationKind: domain.InvocationOrchestrator,
	}
}

// ordinaryAgentRefCC returns an AgentReference with InvocationOrdinary backed
// by a temporary definition file containing valid Claude Code tools frontmatter.
// Use this in ClaudeCodeAdapter tests: the adapter's FR-10 extraction reads the
// definition file before spawning.
func ordinaryAgentRefCC(t *testing.T) domain.AgentReference {
	t.Helper()
	defPath := writeDefFile(t, validClaudeCodeDef)
	return domain.AgentReference{
		Identifier:     "test-agent",
		DefinitionPath: defPath,
		InvocationKind: domain.InvocationOrdinary,
	}
}

// orchestratorAgentRefCC returns an AgentReference with InvocationOrchestrator
// backed by a temporary definition file containing valid Claude Code tools
// frontmatter. Use this in ClaudeCodeAdapter tests.
func orchestratorAgentRefCC(t *testing.T) domain.AgentReference {
	t.Helper()
	defPath := writeDefFile(t, validClaudeCodeDef)
	return domain.AgentReference{
		Identifier:     "orchestrator-agent",
		DefinitionPath: defPath,
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
