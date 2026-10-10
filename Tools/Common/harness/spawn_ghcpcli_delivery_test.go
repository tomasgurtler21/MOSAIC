package harness_test

// Tests for how BuildGHCPCLIArgs and the GHCP CLI spawner deliver prompt
// content: on the child's stdin, never in argv, with no -p at all.
//
// Copilot CLI silently drops stdin when -p has a non-empty value and rejects a
// bare -p, and the npm copilot.cmd shim forwards argv through cmd.exe, which
// mangles quotes and special characters. The builder therefore emits no -p and
// hands the whole prompt back as stdin. Fixtures reuse ordinaryAgent/
// orchestratorAgent, the setHelperEnv/helperExe/readArgs helpers
// (helper_test.go), and deliveryPrompt/specialPayload/assertNoPromptInArgv
// (spawn_opencode_delivery_test.go).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mosaic-common/harness"
)

// ghcpDeliveryRequests returns one request per permission mode, each carrying
// the given prompt plus agent, model and extra args so every flag branch is
// exercised.
func ghcpDeliveryRequests(prompt string) map[string]harness.SpawnRequest {
	return map[string]harness.SpawnRequest{
		"blanket": {
			Agent: ordinaryAgent(), Prompt: prompt, Model: "some-model",
			ExtraArgs: []string{"--extra-flag"}, GHCPCLIMode: harness.GHCPCLIModeBlanket,
		},
		"allowlist": {
			Agent: ordinaryAgent(), Prompt: prompt, Model: "some-model",
			ExtraArgs: []string{"--extra-flag"}, GHCPCLIMode: harness.GHCPCLIModePartialAllowlist,
			DerivedTools: []string{"shell(git status:*)", "write"},
		},
		"allowlist with only ungated tools": {
			Agent: ordinaryAgent(), Prompt: prompt, GHCPCLIMode: harness.GHCPCLIModePartialAllowlist,
			ToolsDerived: true,
		},
	}
}

// assertNoPromptFlag fails if the argv carries -p or --prompt, or any empty
// element that could stand in for an empty -p value.
func assertNoPromptFlag(t *testing.T, args []string) {
	t.Helper()
	for _, a := range args {
		if a == "-p" || a == "--prompt" || strings.HasPrefix(a, "--prompt=") {
			t.Errorf("want no prompt flag in argv, got %q in %q", a, args)
		}
		if a == "" {
			t.Errorf("want no empty argv element, got %q", args)
		}
	}
}

// ---------------------------------------------------------------------------
// Builder: argv carries no prompt content and no -p
// ---------------------------------------------------------------------------

func TestBuildGHCPCLIArgs_NoPromptFlagInAnyMode(t *testing.T) {
	for name, req := range ghcpDeliveryRequests(deliveryPrompt) {
		t.Run(name, func(t *testing.T) {
			args, _, err := harness.BuildGHCPCLIArgs(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertNoPromptFlag(t, args)
		})
	}
}

func TestBuildGHCPCLIArgs_NoArgCarriesPromptContent(t *testing.T) {
	for name, req := range ghcpDeliveryRequests(specialPayload) {
		t.Run(name, func(t *testing.T) {
			args, _, err := harness.BuildGHCPCLIArgs(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertNoPromptInArgv(t, args, "line1 with spaces", "trailing backslash", "100%%", "request-line")
		})
	}
}

func TestBuildGHCPCLIArgs_NoNewlineInArgvForMultiLinePrompt(t *testing.T) {
	req := harness.SpawnRequest{
		Agent: orchestratorAgent(), SystemPrompt: deliverySystemPrompt, Prompt: deliveryPrompt,
		GHCPCLIMode: harness.GHCPCLIModeBlanket,
	}
	args, _, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoPromptInArgv(t, args, "request-line", "Working directory", "<env>")
}

func TestBuildGHCPCLIArgs_ExtraArgsAreFinalNoTrailingPromptArgs(t *testing.T) {
	for name, req := range ghcpDeliveryRequests(deliveryPrompt) {
		if len(req.ExtraArgs) == 0 {
			continue
		}
		t.Run(name, func(t *testing.T) {
			args, _, err := harness.BuildGHCPCLIArgs(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if args[len(args)-1] != "--extra-flag" {
				t.Errorf("want ExtraArgs as the final argument with nothing after it, got %q", args)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Builder: stdin content
// ---------------------------------------------------------------------------

func TestBuildGHCPCLIArgs_StdinEqualsPromptExactly(t *testing.T) {
	prompts := map[string]string{
		"multi-line":            deliveryPrompt,
		"special characters":    specialPayload,
		"no trailing newline":   "no-trailing-lf",
		"trailing newline kept": "ends-with-lf\n",
	}
	for pname, prompt := range prompts {
		for mname, req := range ghcpDeliveryRequests(prompt) {
			t.Run(pname+"/"+mname, func(t *testing.T) {
				_, stdin, err := harness.BuildGHCPCLIArgs(req)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if string(stdin) != prompt {
					t.Errorf("want stdin byte-for-byte %q, got %q", prompt, stdin)
				}
			})
		}
	}
}

func TestBuildGHCPCLIArgs_SystemPromptIsNotPrependedToStdin(t *testing.T) {
	_, stdin, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{
		Agent: ordinaryAgent(), SystemPrompt: deliverySystemPrompt, Prompt: deliveryPrompt,
		GHCPCLIMode: harness.GHCPCLIModeBlanket,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(stdin) != deliveryPrompt {
		t.Errorf("want stdin to be the prompt alone %q, got %q", deliveryPrompt, stdin)
	}
}

func TestBuildGHCPCLIArgs_SuccessStdinIsNeverNil(t *testing.T) {
	_, stdin, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{
		Agent: ordinaryAgent(), Prompt: "x", GHCPCLIMode: harness.GHCPCLIModeBlanket,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdin == nil || len(stdin) != 1 {
		t.Errorf("want a one-byte stdin, got %q", stdin)
	}
}

func TestBuildGHCPCLIArgs_ErrorsReturnNilArgsAndNilStdin(t *testing.T) {
	cases := map[string]struct {
		req  harness.SpawnRequest
		want error
	}{
		"unsupported format": {
			req:  harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "stream-json", GHCPCLIMode: harness.GHCPCLIModeBlanket},
			want: harness.ErrGHCPCLIUnsupportedOutputFormat,
		},
		"unresolved mode": {
			req:  harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x"},
			want: harness.ErrGHCPCLIModeUnresolved,
		},
		"empty prompt": {
			req:  harness.SpawnRequest{Agent: ordinaryAgent(), GHCPCLIMode: harness.GHCPCLIModeBlanket},
			want: harness.ErrGHCPCLIEmptyPrompt,
		},
		"empty allowlist": {
			req:  harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", GHCPCLIMode: harness.GHCPCLIModePartialAllowlist},
			want: harness.ErrGHCPCLIAllowlistEmpty,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			args, stdin, err := harness.BuildGHCPCLIArgs(tc.req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
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

// spawnGHCPEchoStdin runs the GHCP spawner against a helper that echoes its
// stdin and returns what the process received on stdin plus the argv it was
// given. The echoed text is not a GHCP event stream, so Spawn's own error is
// expected and ignored; the raw Response still carries stdout.
func spawnGHCPEchoStdin(t *testing.T, exe string, req harness.SpawnRequest) (stdin string, argv []string) {
	t.Helper()
	argsFile := setHelperEnv(t, "echo-stdin")
	spawner := harness.NewGHCPCLI(exe, harness.WithTimeout(10*time.Second))
	resp, _ := spawner.Spawn(context.Background(), req)
	return string(resp.Stdout), readArgs(t, argsFile)
}

func TestGHCPCLISpawn_SpecialCharacterPayloadReachesStdinUnchanged(t *testing.T) {
	for name, req := range ghcpDeliveryRequests(specialPayload) {
		t.Run(name, func(t *testing.T) {
			req.OutputFormat = "json"

			got, argv := spawnGHCPEchoStdin(t, helperExe(t), req)

			if got != specialPayload {
				t.Errorf("want stdin byte-for-byte %q, got %q", specialPayload, got)
			}
			assertNoPromptFlag(t, argv)
			assertNoPromptInArgv(t, argv, "line1 with spaces", "trailing backslash", "100%%")
		})
	}
}

func TestGHCPCLISpawn_LargePayloadReachesStdinIntact(t *testing.T) {
	big := strings.Repeat("0123456789abcdef\n", 4096) // well past command-line limits
	req := harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: big, OutputFormat: "json", GHCPCLIMode: harness.GHCPCLIModeBlanket}

	got, _ := spawnGHCPEchoStdin(t, helperExe(t), req)

	if got != big {
		t.Errorf("want the %d-byte payload intact on stdin, got %d bytes", len(big), len(got))
	}
}

// TestGHCPCLISpawn_WindowsCmdShim_RequestArrivesOnStdin drives a .cmd shim
// shaped like the npm copilot.cmd (forwarding %*) through cmd.exe, the route
// that mangles any prompt-bearing argv element, and asserts the complete
// request arrives on stdin and the argv is exactly the intended flags.
func TestGHCPCLISpawn_WindowsCmdShim_RequestArrivesOnStdin(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("observes stdin delivery through a cmd.exe .cmd shim; Windows only")
	}
	for name, req := range ghcpDeliveryRequests(specialPayload) {
		t.Run(name, func(t *testing.T) {
			shimPath := filepath.Join(t.TempDir(), "copilot.cmd")
			shim := "@ECHO off\r\nSETLOCAL\r\n\"" + os.Args[0] + "\" %*\r\n"
			if err := os.WriteFile(shimPath, []byte(shim), 0644); err != nil {
				t.Fatalf("WriteFile shim: %v", err)
			}
			req.OutputFormat = "json"

			got, argv := spawnGHCPEchoStdin(t, shimPath, req)

			if got != specialPayload {
				t.Errorf("want complete request on stdin through the shim: %q, got %q", specialPayload, got)
			}
			assertNoPromptFlag(t, argv)
			assertNoPromptInArgv(t, argv, "line1 with spaces", "trailing backslash", "100%%")
			if !containsSequence(argv, "--output-format", "json") || !containsArg(argv, "--no-ask-user") {
				t.Errorf("want intended flags forwarded through the shim, got %q", argv)
			}
			wantedMode := "--yolo"
			if req.GHCPCLIMode == harness.GHCPCLIModePartialAllowlist {
				wantedMode = "--no-ask-user"
				if containsArg(argv, "--yolo") {
					t.Errorf("want no --yolo in allowlist mode, got %q", argv)
				}
			}
			if !containsArg(argv, wantedMode) {
				t.Errorf("want %s forwarded through the shim, got %q", wantedMode, argv)
			}
		})
	}
}
