package ghcpcli_test

// Tests that GHCPCLIAdapter.Invoke and InvokeRaw deliver the complete request
// on the child's stdin in both permission modes, with nothing prompt-related
// in argv (no -p, no payload fragment), while the relied-on flags
// (--output-format json, permission flags, --agent) stay unchanged.
// Uses the fake-CLI helper process (helperprocess_test.go) and the agent-file
// fixtures in ghcpcli_mode_test.go.

import (
	"context"
	"strings"
	"testing"
	"time"

	commonharness "mosaic-common/harness"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/harness/ghcpcli"
)

// stdinSpecialTask carries the characters that cmd.exe would mangle in argv.
const stdinSpecialTask = `UNIQUE-TASK-MARKER say "hi" 100% %OS% ^ & | < > !X! C:\Users\x y\dir\`

// stdinSpecialFragments are substrings no argv element may contain.
var stdinSpecialFragments = []string{"UNIQUE-TASK-MARKER", "100%", "C:\\Users", "agent_instance_id"}

func stdinSpecialRequest() domain.ProtocolRequest {
	req := minimalClaudeRequest("stdin-agent#1")
	req.TaskDescription = stdinSpecialTask
	return req
}

// assertArgvCarriesNoPrompt fails if argv has a prompt flag or any fragment.
func assertArgvCarriesNoPrompt(t *testing.T, args []string, fragments ...string) {
	t.Helper()
	for _, a := range args {
		if a == "-p" || a == "--prompt" {
			t.Errorf("want no prompt flag in argv, got %q", args)
		}
		if strings.ContainsAny(a, "\r\n") {
			t.Errorf("want no newline in any argv element, got %q", a)
		}
		for _, f := range fragments {
			if strings.Contains(a, f) {
				t.Errorf("want no argv element carrying prompt content %q, got %q", f, a)
			}
		}
	}
}

// stdinModeCase is one permission mode with the agent and adapter it needs.
type stdinModeCase struct {
	name    string
	adapter func(t *testing.T) *ghcpcli.GHCPCLIAdapter
	agent   func(t *testing.T) domain.AgentReference
	flags   func(t *testing.T, args []string)
}

func stdinModeCases() []stdinModeCase {
	newAdapter := func(mode commonharness.GHCPCLIPermissionMode) func(t *testing.T) *ghcpcli.GHCPCLIAdapter {
		return func(t *testing.T) *ghcpcli.GHCPCLIAdapter {
			return ghcpcli.NewGHCPCLIAdapterWithMode(helperExe(t), 10*time.Second, nil, mode)
		}
	}
	return []stdinModeCase{
		{
			name:    "blanket",
			adapter: newAdapter(commonharness.GHCPCLIModeBlanket),
			agent:   func(t *testing.T) domain.AgentReference { return ordinaryAgentRef() },
			flags: func(t *testing.T, args []string) {
				if !containsArg(args, "--yolo") || !containsArg(args, "--no-ask-user") {
					t.Errorf("want --yolo and --no-ask-user in blanket args, got %v", args)
				}
			},
		},
		{
			name:    "allowlist",
			adapter: newAdapter(commonharness.GHCPCLIModePartialAllowlist),
			agent: func(t *testing.T) domain.AgentReference {
				return agentRefWithDefinitionPath(writeGHCPCLITestAgentFile(t, []string{"edit", "execute"}))
			},
			flags: func(t *testing.T, args []string) {
				if !containsSequence(args, "--allow-tool", "write") || !containsSequence(args, "--allow-tool", "shell") {
					t.Errorf("want --allow-tool write and shell in allowlist args, got %v", args)
				}
				if containsArg(args, "--yolo") || !containsArg(args, "--no-ask-user") {
					t.Errorf("want --no-ask-user and no --yolo in allowlist args, got %v", args)
				}
			},
		},
	}
}

func TestGHCPCLIAdapter_Invoke_DeliversCompleteRequestOnStdin(t *testing.T) {
	for _, mc := range stdinModeCases() {
		t.Run(mc.name, func(t *testing.T) {
			argsFile := setHelperEnv(t, "ghcpcli-success")
			stdinFile := newTestStdinCapture(t)
			request := stdinSpecialRequest()
			want, err := harness.MarshalRequest(request)
			if err != nil {
				t.Fatalf("MarshalRequest: %v", err)
			}

			_, err = mc.adapter(t).Invoke(context.Background(), mc.agent(t), request)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := readHelperStdin(t, stdinFile); string(got) != string(want) {
				t.Errorf("want the marshalled request on stdin\n  want: %q\n  got:  %q", want, got)
			}
			args := readArgs(t, argsFile)
			assertArgvCarriesNoPrompt(t, args, stdinSpecialFragments...)
			assertRelianceFlags(t, args, mc)
		})
	}
}

func TestGHCPCLIAdapter_InvokeRaw_DeliversCompletePayloadOnStdin(t *testing.T) {
	payload := []byte(`{"action":"route","task_description":"UNIQUE-TASK-MARKER 100% %OS% ^ & | < > !X! C:\\Users\\x y"}` + "\n\nsecond line")
	for _, mc := range stdinModeCases() {
		t.Run(mc.name, func(t *testing.T) {
			argsFile := setHelperEnv(t, "ghcpcli-success")
			stdinFile := newTestStdinCapture(t)

			_, err := mc.adapter(t).InvokeRaw(context.Background(), mc.agent(t), payload)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := readHelperStdin(t, stdinFile); string(got) != string(payload) {
				t.Errorf("want the raw payload on stdin\n  want: %q\n  got:  %q", payload, got)
			}
			args := readArgs(t, argsFile)
			assertArgvCarriesNoPrompt(t, args, "UNIQUE-TASK-MARKER", "100%", "second line", "\"action\"")
			assertRelianceFlags(t, args, mc)
		})
	}
}

// assertRelianceFlags checks the flags callers already rely on are unchanged.
func assertRelianceFlags(t *testing.T, args []string, mc stdinModeCase) {
	t.Helper()
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want --output-format json, got %v", args)
	}
	if !containsSequence(args, "--agent", "test-agent") && !containsSequence(args, "--agent", "orchestrator-agent") {
		t.Errorf("want --agent <identifier>, got %v", args)
	}
	mc.flags(t, args)
}
