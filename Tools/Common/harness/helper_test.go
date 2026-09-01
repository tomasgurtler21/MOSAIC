package harness_test

// Shared fake-subprocess infrastructure for the mosaic-common/harness test
// suite.
//
// Subprocess faking: this test binary doubles as a fake CLI when the
// environment variable GO_WANT_HELPER_PROCESS=1 is set. TestMain intercepts
// the subprocess entry point before Go's test framework can attempt to parse
// the fake CLI's flags as test flags. Individual tests set
// GO_WANT_HELPER_PROCESS=1 (via t.Setenv) so the value is inherited by
// subprocesses they spawn; the current test process itself started without
// the variable and is unaffected.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMain intercepts the process entry point. When GO_WANT_HELPER_PROCESS=1
// the binary acts as the fake CLI instead of running tests.
func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		runHelperProcess()
		// runHelperProcess calls os.Exit; this line is unreachable.
	}
	os.Exit(m.Run())
}

// runHelperProcess is the fake CLI.
//
// It writes os.Args[1:] (the arguments the caller passed) to the file named
// by GO_HELPER_ARGS_FILE so tests can inspect them. It then simulates CLI
// behaviour according to GO_HELPER_CMD.
func runHelperProcess() {
	if argsFile := os.Getenv("GO_HELPER_ARGS_FILE"); argsFile != "" {
		data, _ := json.Marshal(os.Args[1:])
		os.WriteFile(argsFile, data, 0644) //nolint:errcheck
	}

	switch os.Getenv("GO_HELPER_CMD") {
	case "success":
		type resultEntry struct {
			Type   string `json:"type"`
			Result string `json:"result"`
		}
		protocolResp := `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`
		envelope := []resultEntry{{Type: "result", Result: protocolResp}}
		data, _ := json.Marshal(envelope)
		os.Stdout.Write(data) //nolint:errcheck

	case "exit1":
		os.Stderr.WriteString("simulated stderr output") //nolint:errcheck
		os.Exit(1)

	case "exit1-no-stderr":
		os.Exit(1)

	case "empty":
		// Write nothing to stdout; exit zero.

	case "bad-json":
		os.Stdout.WriteString("this is not JSON at all") //nolint:errcheck

	case "bad-envelope":
		os.Stdout.WriteString(`[{"type":"assistant","message":"hello there"}]`) //nolint:errcheck

	case "bare-json":
		os.Stdout.WriteString(`{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`) //nolint:errcheck

	case "embedded-json":
		os.Stdout.WriteString("Warning: pre-flight check failed\n")                                                 //nolint:errcheck
		os.Stdout.WriteString(`{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`) //nolint:errcheck
		os.Stdout.WriteString("\nCLI exiting\n")                                                                    //nolint:errcheck

	case "echo-env":
		// Echo a named environment variable to stdout, so tests can assert
		// Run/Spawn honoured RunOptions.Env / SpawnRequest.Env.
		//
		// GO_HELPER_ECHO_VAR holds the *name* of the variable to echo (e.g.
		// "MOSAIC_TEST_VAR"), not the value itself, so this requires a
		// double lookup: resolve the name, then look that name up.
		os.Stdout.WriteString(os.Getenv(os.Getenv("GO_HELPER_ECHO_VAR"))) //nolint:errcheck

	case "echo-cwd":
		// Echo the process's working directory, so tests can assert
		// WorkingDir was honoured.
		cwd, _ := os.Getwd()
		os.Stdout.WriteString(cwd) //nolint:errcheck

	case "echo-stdin":
		// Echo whatever was piped to stdin, so tests can assert Stdin was
		// honoured. io.ReadAll is used rather than a fixed-size buffer so
		// that payloads larger than any fixed limit are captured intact.
		data, _ := io.ReadAll(os.Stdin)
		os.Stdout.Write(data) //nolint:errcheck

	case "opencode-success":
		// Produce a valid `opencode run --format json` event stream: one text
		// event carrying the protocol response, terminated by a step_finish
		// whose reason is "stop" (success). Exit 0.
		protocolResp := `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`
		os.Stdout.WriteString(`{"type":"step_start"}` + "\n")                                             //nolint:errcheck
		os.Stdout.WriteString(`{"type":"text","part":{"type":"text","text":` + jsonQuote(protocolResp) + `}}` + "\n") //nolint:errcheck
		os.Stdout.WriteString(`{"type":"step_finish","part":{"reason":"stop"}}` + "\n")                    //nolint:errcheck

	case "opencode-stream-error":
		// Simulate the zero-exit trap: the process exits 0 but the event
		// stream itself reports failure via an "error" event. Nothing here
		// resembles a non-zero exit — the whole point is that the exit code
		// carries no information for this harness.
		os.Stdout.WriteString(`{"type":"error","error":{"name":"SomeError","data":{"message":"simulated failure"}}}` + "\n") //nolint:errcheck

	case "opencode-incomplete":
		// Emit a recognised event but never a terminal one (no step_finish,
		// no error). Exit 0. The stream's outcome cannot be established.
		os.Stdout.WriteString(`{"type":"step_start"}` + "\n") //nolint:errcheck

	case "ghcpcli-success":
		// A valid GHCP CLI event stream: one assistant.message carrying the
		// Communication Protocol response, followed by a result line reporting
		// exitCode 0. Exit 0.
		protocolResp := `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`
		os.Stdout.WriteString(`{"type":"assistant.message","data":{"content":` + jsonQuote(protocolResp) + `},"id":"id1","timestamp":"2026-08-12T21:00:00Z","parentId":"pid1"}` + "\n") //nolint:errcheck
		os.Stdout.WriteString(`{"type":"result","timestamp":"2026-08-12T21:00:01Z","sessionId":"s1","exitCode":0,"usage":{}}` + "\n")                                                    //nolint:errcheck

	case "ghcpcli-nonzero-exit-success-stream":
		// The key GHCP CLI differentiator: the stream's terminal result event
		// reports success (stream exitCode 0) but the process itself exits 1.
		// The spawner must continue past ErrNonZeroExit from Run and use the
		// stream's own verdict — success — rather than abandoning on the process
		// exit code.
		protocolResp := `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`
		os.Stdout.WriteString(`{"type":"assistant.message","data":{"content":` + jsonQuote(protocolResp) + `},"id":"id1","timestamp":"2026-08-12T21:00:00Z","parentId":"pid1"}` + "\n") //nolint:errcheck
		os.Stdout.WriteString(`{"type":"result","timestamp":"2026-08-12T21:00:01Z","sessionId":"s1","exitCode":0,"usage":{}}` + "\n")                                                    //nolint:errcheck
		os.Exit(1)

	case "ghcpcli-zero-exit-incomplete-stream":
		// Some GHCP events but no terminal result line; exits 0. A zero process
		// exit does not constitute success when the stream lacks a result event.
		os.Stdout.WriteString(`{"type":"session.mcp_servers_loaded","data":{},"id":"id1","timestamp":"2026-08-12T21:00:00Z","parentId":"pid1"}` + "\n")                                  //nolint:errcheck
		os.Stdout.WriteString(`{"type":"assistant.message","data":{"content":"partial"},"id":"id2","timestamp":"2026-08-12T21:00:00Z","parentId":"pid1"}` + "\n")                        //nolint:errcheck
		// No result line. Exit 0.

	case "ghcpcli-observed-failure":
		// The observed failure signature: MCP events on stdout, model error on
		// stderr, no terminal result event. Exits 1. Reproduces the real failure
		// capture when an unavailable model is specified.
		os.Stdout.WriteString(`{"type":"session.mcp_servers_loaded","data":{},"id":"id1","timestamp":"2026-08-12T21:00:00Z","parentId":"pid1"}` + "\n")                                  //nolint:errcheck
		os.Stdout.WriteString(`{"type":"session.mcp_server_status_changed","data":{},"ephemeral":true,"id":"id2","timestamp":"2026-08-12T21:00:00Z","parentId":"pid1"}` + "\n")          //nolint:errcheck
		os.Stderr.WriteString(`Error: Model "definitely-not-a-real-model" from --model flag is not available.`)                                                                          //nolint:errcheck
		os.Exit(1)

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
// CLI executable when GO_WANT_HELPER_PROCESS=1 is set.
func helperExe(t *testing.T) string {
	t.Helper()
	return os.Args[0]
}

// setHelperEnv configures the current process environment so that any
// subprocess spawned by the code under test will enter helper-process mode.
//
// It sets GO_WANT_HELPER_PROCESS=1, GO_HELPER_CMD=cmd, and
// GO_HELPER_ARGS_FILE to a fresh temp file path. The returned path is where
// the helper will write the serialised os.Args[1:] it receives.
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
// the argument slice the code under test passed to the subprocess.
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

// argsWithout returns a copy of args with every element that equals flag
// removed. It does not remove flag-value pairs (two-element sequences); it
// only removes standalone flags such as --no-session-persistence.
func argsWithout(args []string, flag string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a != flag {
			out = append(out, a)
		}
	}
	return out
}
