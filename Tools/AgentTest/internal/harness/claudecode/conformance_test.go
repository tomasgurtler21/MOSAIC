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

var conformanceWorkerID = domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Worker"}

// conformanceNativePre synthesises a native payload the way the real harness
// actually sends one. The identity it receives carries the normalized,
// harness-neutral vocabulary (see contract.Config.NativePre's doc comment);
// the wire tool_name field, in contrast, always carries this harness's own
// native dispatch-tool name, exactly as a live invocation would, regardless
// of what identity vocabulary the caller supplied.
//
// token is placed in ToolUseID — the dispatch-scoped identifier the harness
// sends on every PreToolUse event and echoes on the matching PostToolUse and
// SubagentStop events. TranslateCall recovers the token from this field.
func conformanceNativePre(id domain.CollaboratorIdentity, msg domain.TaskMessage, token string) []byte {
	input := claudecode.TaskToolInput{
		SubagentType: id.AgentIdentity,
		Description:  msg.TaskDescription,
		Prompt:       msg.Raw,
	}
	toolInput, _ := json.Marshal(input)
	b, _ := json.Marshal(claudecode.PreToolUsePayload{
		HookEventName: "PreToolUse",
		ToolName:      claudecode.NativeDispatchToolName,
		ToolInput:     toolInput,
		ToolUseID:     token,
	})
	return b
}

// conformanceNativePost is conformanceNativePre's post-invocation
// counterpart: the wire tool_name field always carries this harness's own
// native dispatch-tool name, and token is echoed in ToolUseID, for the same
// reason as conformanceNativePre.
func conformanceNativePost(id domain.CollaboratorIdentity, token string, observed string) []byte {
	input := claudecode.TaskToolInput{SubagentType: id.AgentIdentity}
	toolInput, _ := json.Marshal(input)
	b, _ := json.Marshal(claudecode.PostToolUsePayload{
		HookEventName: "PostToolUse",
		ToolName:      claudecode.NativeDispatchToolName,
		ToolInput:     toolInput,
		ToolResponse:  json.RawMessage(`"` + observed + `"`),
		ToolUseID:     token,
	})
	return b
}

// conformanceNativeCompletion synthesises this harness's own SubagentStop
// completion-signal payload, carrying observed as the collaborator's real
// reply. AgentTranscriptPath is deliberately empty: this check exercises
// ObservedResponse recovery, not correlation-token recovery, which is
// already covered by testCorrelation via NativePre/NativePost.
func conformanceNativeCompletion(observed string) []byte {
	b, _ := json.Marshal(claudecode.CompletionPayload{
		HookEventName:        "SubagentStop",
		LastAssistantMessage: observed,
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
		// With the tool_use_id correlation mechanism, the correlation token
		// is carried on the harness's own inbound field (tool_use_id), not
		// planted in the outbound rewrite reply. The outbound reply contains
		// only the rewritten prompt; CorrelationToken is left empty here
		// because it is not observable in the outbound direction.
		return contract.ObservedEffect{
			Kind:            domain.OutcomeRewritePrompt,
			RewrittenPrompt: string(out.UpdatedInput),
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
		NativePre:        conformanceNativePre,
		NativePost:       conformanceNativePost,
		NativeCompletion: conformanceNativeCompletion,
		Observe:          conformanceObserve,
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
