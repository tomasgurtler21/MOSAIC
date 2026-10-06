package opencode_test

// Tests that OpenCodeAdapter delivers prompt content on stdin and never in
// argv, on both the ordinary dispatch path (Invoke) and the raw/consultation
// path (InvokeRaw). cmd.exe cuts a command line at the first newline and
// OpenCode prepends any positional message to stdin, so the env block and the
// request must arrive on stdin with no positional message at all.
//
// Reuses setHelperEnv, newTestStdinCapture, readHelperStdin, readArgs,
// containsSequence and the agent-reference fixtures from helperprocess_test.go.

import (
	"context"
	"strings"
	"testing"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/harness/opencode"
)

// multiLineRawPayload is a consultation request with embedded newlines, quotes
// and cmd.exe metacharacters, so any argv delivery would be mangled or cut.
const multiLineRawPayload = "{\"action\":\"route\",\n \"note\":\"say \\\"hi\\\" 100%% ^ & | < > !X!\",\n \"path\":\"C:\\\\dir\\\\\"}"

// assertFlagsOnlyArgv fails unless argv is exactly the flag set the adapter is
// expected to build, with no positional message and no newline anywhere.
func assertFlagsOnlyArgv(t *testing.T, args []string, agentID string, fragments ...string) {
	t.Helper()
	if len(args) == 0 || args[0] != "run" {
		t.Errorf("want args to start with run, got %q", args)
	}
	if !containsSequence(args, "--agent", agentID) {
		t.Errorf("want --agent %q in args, got %q", agentID, args)
	}
	if !containsSequence(args, "--format", "json") {
		t.Errorf("want --format json in args, got %q", args)
	}
	if args[len(args)-1] != "--auto" {
		t.Errorf("want --auto as the final argument with no positional message after it, got %q", args)
	}
	for _, a := range args {
		if strings.ContainsAny(a, "\r\n") {
			t.Errorf("want no argv element containing a newline, got %q", a)
		}
		for _, f := range fragments {
			if strings.Contains(a, f) {
				t.Errorf("want no argv element carrying prompt content %q, got %q", f, a)
			}
		}
	}
}

func TestOpenCodeAdapter_Invoke_DeliversEnvBlockAndRequestOnStdin(t *testing.T) {
	for name, agent := range map[string]domain.AgentReference{
		"ordinary":     ordinaryAgentRef(),
		"orchestrator": orchestratorAgentRef(),
	} {
		t.Run(name, func(t *testing.T) {
			argsFile := setHelperEnv(t, "opencode-success")
			stdinFile := newTestStdinCapture(t)
			request := minimalClaudeRequest(agent.Identifier + "#1")
			request.TaskDescription = "first line\nsecond line with \"quotes\" and 100%"

			adapter := opencode.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
			if _, err := adapter.Invoke(context.Background(), agent, request); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			reqBytes, err := harness.MarshalRequest(request)
			if err != nil {
				t.Fatalf("MarshalRequest: %v", err)
			}
			got := string(readHelperStdin(t, stdinFile))
			if !strings.HasSuffix(got, "\n"+string(reqBytes)) {
				t.Errorf("want stdin to end with a newline and the full marshalled request %q, got %q", reqBytes, got)
			}
			if !strings.HasPrefix(got, "<env>") || !strings.Contains(got, "</env>\n") {
				t.Errorf("want stdin to begin with the env block, got %q", got)
			}
			assertFlagsOnlyArgv(t, readArgs(t, argsFile), agent.Identifier, "<env>", "second line", agent.Identifier+"#1")
		})
	}
}

func TestOpenCodeAdapter_InvokeRaw_DeliversEnvBlockAndPayloadOnStdin(t *testing.T) {
	argsFile := setHelperEnv(t, "opencode-success")
	stdinFile := newTestStdinCapture(t)

	adapter := opencode.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	if _, err := adapter.InvokeRaw(context.Background(), orchestratorRef(), []byte(multiLineRawPayload)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := commonharness.EnvBlock("") + "\n" + multiLineRawPayload
	if got := string(readHelperStdin(t, stdinFile)); got != want {
		t.Errorf("want stdin byte-for-byte %q, got %q", want, got)
	}
	assertFlagsOnlyArgv(t, readArgs(t, argsFile), orchestratorRef().Identifier, "<env>", "\"action\"", "100%%")
}

func TestOpenCodeAdapter_InvokeRaw_LargePayloadReachesStdinIntact(t *testing.T) {
	setHelperEnv(t, "opencode-success")
	stdinFile := newTestStdinCapture(t)
	big := strings.Repeat("0123456789abcdef\n", 4096) // past cmd.exe and CreateProcess limits

	adapter := opencode.NewOpenCodeAdapter(helperExe(t), 5*time.Second)
	if _, err := adapter.InvokeRaw(context.Background(), orchestratorRef(), []byte(big)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := commonharness.EnvBlock("") + "\n" + big
	if got := string(readHelperStdin(t, stdinFile)); got != want {
		t.Errorf("want the %d-byte payload intact on stdin, got %d bytes", len(want), len(got))
	}
}
