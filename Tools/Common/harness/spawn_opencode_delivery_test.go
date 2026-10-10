package harness_test

// Tests for how BuildOpenCodeArgs and the OpenCode spawner deliver prompt
// content: on the child's stdin, never in argv, with no positional message.
//
// cmd.exe truncates a command line at the first newline, and OpenCode stores
// `positional + "\n" + stdin` when both are present, so the builder must emit
// no positional message at all and hand the whole message back as stdin.
// Fixtures reuse ordinaryAgent/orchestratorAgent (buildargs_test.go) and the
// setHelperEnv/helperExe/readArgs helpers (helper_test.go).

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mosaic-common/harness"
)

const (
	deliverySystemPrompt = "<env>\nWorking directory: /work/dir\nPlatform: test\n</env>"
	deliveryPrompt       = "request-line-one\n{\"task\":\"request-line-two\"}\nrequest-line-three"
)

// specialPayload exercises every character class that cmd.exe or a shell
// would mangle if the content travelled through the command line.
const specialPayload = "line1 with spaces\n" +
	`{"q":"say \"hi\"","path":"C:\Users\x y\dir\"}` + "\n" +
	`%OS% %PATH% 100%% ^ & | < > !OS! 'single' "double"` + "\n" +
	`trailing backslash \`

// assertNoPromptInArgv fails if any element carries a newline or a fragment
// of the given prompt strings.
func assertNoPromptInArgv(t *testing.T, args []string, fragments ...string) {
	t.Helper()
	for _, a := range args {
		if strings.ContainsAny(a, "\r\n") {
			t.Errorf("want no argv element containing a newline, got %q in %q", a, args)
		}
		for _, f := range fragments {
			if f != "" && strings.Contains(a, f) {
				t.Errorf("want no argv element carrying prompt content %q, got %q in %q", f, a, args)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Builder: argv carries no prompt content
// ---------------------------------------------------------------------------

func TestBuildOpenCodeArgs_NoArgContainsNewline_Ordinary(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{
		Agent: ordinaryAgent(), SystemPrompt: deliverySystemPrompt, Prompt: deliveryPrompt,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoPromptInArgv(t, args, "request-line", "Working directory", "<env>")
}

func TestBuildOpenCodeArgs_NoArgContainsNewline_Orchestrator(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{
		Agent: orchestratorAgent(), SystemPrompt: deliverySystemPrompt, Prompt: deliveryPrompt,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoPromptInArgv(t, args, "request-line", "Working directory", "<env>")
}

func TestBuildOpenCodeArgs_NoPositionalMessageAfterFlags(t *testing.T) {
	cases := map[string]struct {
		req  harness.SpawnRequest
		last string
	}{
		"flags only": {
			req:  harness.SpawnRequest{Agent: ordinaryAgent(), SystemPrompt: deliverySystemPrompt, Prompt: deliveryPrompt},
			last: "--auto",
		},
		"with model": {
			req:  harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: deliveryPrompt, Model: "some-model"},
			last: "some-model",
		},
		"with extra args": {
			req:  harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: deliveryPrompt, Model: "m", ExtraArgs: []string{"--x", "y"}},
			last: "y",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			args, _, err := harness.BuildOpenCodeArgs(tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(args) == 0 || args[len(args)-1] != tc.last {
				t.Errorf("want %q as the final argument with no positional message after it, got %q", tc.last, args)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Builder: stdin content
// ---------------------------------------------------------------------------

func TestBuildOpenCodeArgs_StdinIsSystemPromptNewlinePrompt(t *testing.T) {
	_, stdin, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{
		Agent: orchestratorAgent(), SystemPrompt: deliverySystemPrompt, Prompt: deliveryPrompt,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := deliverySystemPrompt + "\n" + deliveryPrompt
	if string(stdin) != want {
		t.Errorf("want stdin %q, got %q", want, stdin)
	}
}

func TestBuildOpenCodeArgs_NoSystemPrompt_StdinIsPromptAlone(t *testing.T) {
	_, stdin, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: deliveryPrompt})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(stdin) != deliveryPrompt {
		t.Errorf("want stdin %q, got %q", deliveryPrompt, stdin)
	}
}

func TestBuildOpenCodeArgs_StdinPreservesSpecialCharactersUnchanged(t *testing.T) {
	_, stdin, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{
		Agent: ordinaryAgent(), SystemPrompt: deliverySystemPrompt, Prompt: specialPayload,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := deliverySystemPrompt + "\n" + specialPayload; string(stdin) != want {
		t.Errorf("want stdin byte-for-byte %q, got %q", want, stdin)
	}
}

func TestBuildOpenCodeArgs_PromptWithoutTrailingNewline_NothingAppended(t *testing.T) {
	_, stdin, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "no-trailing-lf"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(stdin) != "no-trailing-lf" {
		t.Errorf("want stdin exactly %q, got %q", "no-trailing-lf", stdin)
	}
}

func TestBuildOpenCodeArgs_ErrorsReturnNilArgsAndNilStdin(t *testing.T) {
	agent := ordinaryAgent()
	agent.Identifier = ""
	cases := map[string]harness.SpawnRequest{
		"empty identifier":   {Agent: agent, Prompt: "x"},
		"unsupported format": {Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "stream-json"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			args, stdin, err := harness.BuildOpenCodeArgs(req)
			if err == nil {
				t.Fatalf("want an error")
			}
			if args != nil || stdin != nil {
				t.Errorf("want (nil, nil, err), got args=%q stdin=%q", args, stdin)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Spawner: stdin reaches the process unchanged
// ---------------------------------------------------------------------------

// spawnEchoStdin runs the spawner against a helper that echoes its stdin and
// returns what the process received on stdin plus the argv it was given. The
// echoed text is not an OpenCode event stream, so Spawn's own error is
// expected and ignored; the raw Response still carries stdout.
func spawnEchoStdin(t *testing.T, exe string, req harness.SpawnRequest) (stdin string, argv []string) {
	t.Helper()
	argsFile := setHelperEnv(t, "echo-stdin")
	spawner := harness.NewOpenCode(exe, harness.WithTimeout(10*time.Second))
	resp, _ := spawner.Spawn(context.Background(), req)
	return string(resp.Stdout), readArgs(t, argsFile)
}

func TestOpenCodeSpawn_SpecialCharacterPayloadReachesStdinUnchanged(t *testing.T) {
	req := harness.SpawnRequest{
		Agent: orchestratorAgent(), SystemPrompt: harness.EnvBlock(""), Prompt: specialPayload, OutputFormat: "json",
	}

	got, argv := spawnEchoStdin(t, helperExe(t), req)

	if want := harness.EnvBlock("") + "\n" + specialPayload; got != want {
		t.Errorf("want stdin byte-for-byte %q, got %q", want, got)
	}
	assertNoPromptInArgv(t, argv, "line1 with spaces", "trailing backslash", "100%%")
}

func TestOpenCodeSpawn_LargePayloadReachesStdinIntact(t *testing.T) {
	big := strings.Repeat("0123456789abcdef\n", 4096) // well past command-line limits
	req := harness.SpawnRequest{Agent: ordinaryAgent(), SystemPrompt: harness.EnvBlock(""), Prompt: big, OutputFormat: "json"}

	got, _ := spawnEchoStdin(t, helperExe(t), req)

	if want := harness.EnvBlock("") + "\n" + big; got != want {
		t.Errorf("want the %d-byte payload intact on stdin, got %d bytes", len(want), len(got))
	}
}

// TestOpenCodeSpawn_WindowsCmdShim_MultiLineRequestArrivesOnStdin drives a
// .cmd shim through cmd.exe, the exact path where a multi-line argv element
// was cut at its first newline, and asserts the complete env block and
// request arrive on stdin with nothing prompt-related in argv.
func TestOpenCodeSpawn_WindowsCmdShim_MultiLineRequestArrivesOnStdin(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("observes stdin delivery through a cmd.exe .cmd shim; Windows only")
	}
	shimPath := filepath.Join(t.TempDir(), "opencode.cmd")
	shim := "@echo off\r\n\"" + os.Args[0] + "\" %*\r\n"
	if err := os.WriteFile(shimPath, []byte(shim), 0644); err != nil {
		t.Fatalf("WriteFile shim: %v", err)
	}
	req := harness.SpawnRequest{
		Agent: orchestratorAgent(), SystemPrompt: harness.EnvBlock(""), Prompt: specialPayload, OutputFormat: "json",
	}

	got, argv := spawnEchoStdin(t, shimPath, req)

	if want := harness.EnvBlock("") + "\n" + specialPayload; got != want {
		t.Errorf("want complete env block and request on stdin through the shim: %q, got %q", want, got)
	}
	assertNoPromptInArgv(t, argv, "line1 with spaces", "Working directory")
}
