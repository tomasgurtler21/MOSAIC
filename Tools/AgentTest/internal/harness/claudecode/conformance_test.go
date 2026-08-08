package claudecode_test

// TestConformance drives this adapter through the reusable adapter
// conformance suite (internal/harness/contract.Run), unmodified, per T10.7.
// If the suite ever needs changing to accommodate this adapter, that is
// evidence the port was shaped around one harness and must be raised rather
// than patched over here.
//
// This stage (Stage 10) implements provisioning, composition, the
// interpreter and outside-scope obligations only; payload translation and
// subject spawning are Stage 11's responsibility (see Stage-10/Plan.md).
// The suite's translation- and spawn-dependent subtests are therefore
// expected to keep failing until Stage 11 wires TranslateCall,
// TranslateOutcome and SpawnPlan — the suite is still run whole and
// unmodified, exactly as T10.7 requires, and every subtest's pass/fail
// state is a truthful report of what has been implemented so far.

import (
	"encoding/json"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
	"mosaic-agent-test/internal/harness/contract"
)

var conformanceWorkerID = domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "Worker"}

func conformanceNativePre(id domain.CollaboratorIdentity, msg domain.TaskMessage, token string) []byte {
	input := claudecode.TaskToolInput{
		SubagentType: id.AgentIdentity,
		Description:  msg.TaskDescription,
		Prompt:       msg.Raw,
	}
	// A real caller plants the token via PlantToken before this payload is
	// ever sent; this fixture stands in for that caller, so it must plant
	// the token the same way rather than merely accepting one that never
	// arrives. TranslateCall recovers the token from this exact field.
	input = claudecode.PlantToken(input, token)
	toolInput, _ := json.Marshal(input)
	b, _ := json.Marshal(claudecode.PreToolUsePayload{
		HookEventName: "PreToolUse",
		ToolName:      id.ToolName,
		ToolInput:     toolInput,
	})
	return b
}

func conformanceNativePost(id domain.CollaboratorIdentity, token string, observed string) []byte {
	// The post-invocation payload echoes the tool's (already rewritten)
	// input, which is where the harness preserves the planted token through
	// to the post-invocation point. Plant it here too, for the same reason
	// as conformanceNativePre.
	input := claudecode.PlantToken(claudecode.TaskToolInput{SubagentType: id.AgentIdentity}, token)
	toolInput, _ := json.Marshal(input)
	b, _ := json.Marshal(claudecode.PostToolUsePayload{
		HookEventName: "PostToolUse",
		ToolName:      id.ToolName,
		ToolInput:     toolInput,
		ToolResponse:  json.RawMessage(`"` + observed + `"`),
	})
	return b
}

func conformanceObserve(t *testing.T, native []byte) contract.ObservedEffect {
	t.Helper()
	var reply claudecode.HookReply
	if err := json.Unmarshal(native, &reply); err != nil {
		t.Fatalf("conformanceObserve: malformed reply: %v", err)
	}
	if reply.HookSpecificOutput == nil {
		return contract.ObservedEffect{Kind: domain.OutcomePassthrough}
	}
	out := reply.HookSpecificOutput
	switch out.PermissionDecision {
	case "deny":
		return contract.ObservedEffect{Kind: domain.OutcomeHalt}
	case "allow":
		var updated claudecode.TaskToolInput
		if err := json.Unmarshal(out.UpdatedInput, &updated); err != nil {
			t.Fatalf("conformanceObserve: malformed updated input: %v", err)
		}
		token, _ := claudecode.RecoverToken(updated)
		return contract.ObservedEffect{
			Kind:             domain.OutcomeRewritePrompt,
			RewrittenPrompt:  string(out.UpdatedInput),
			CorrelationToken: token,
		}
	default:
		return contract.ObservedEffect{Kind: domain.OutcomePassthrough}
	}
}

func newConformanceConfig() contract.Config {
	return contract.Config{
		Name: "claude-code",
		New: func(t *testing.T, dir string) domain.HarnessAdapter {
			return claudecode.New(claudecode.Options{})
		},
		NativePre:  conformanceNativePre,
		NativePost: conformanceNativePost,
		Observe:    conformanceObserve,
		Subject: domain.SubjectUnderTest{
			Identity:       "orchestrator",
			DefinitionPath: "agents/orchestrator.md",
			OpeningMessage: "begin the run",
			InvocationKind: "agent",
			Model:          "test-model",
		},
	}
}

func TestConformance(t *testing.T) {
	contract.Run(t, newConformanceConfig())
}
