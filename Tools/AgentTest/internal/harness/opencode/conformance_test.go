package opencode_test

// TestConformance drives this adapter through the reusable adapter
// conformance suite (internal/harness/contract.Run), unmodified, per T7.4.
// If the suite ever needs changing to accommodate this adapter, that is
// evidence the port was shaped around one harness and must be raised rather
// than patched over here.
//
// Translation (TranslateCall, TranslateOutcome), NewToken, PlantToken and
// RecoverToken are still stubs at the time this configuration is written;
// the translation- and correlation-dependent subtests are expected to keep
// failing until the translation pass (I7.1-I7.4) replaces those stubs. The
// suite is still run whole and unmodified, exactly as T7.4 requires, and
// every subtest's pass/fail state is a truthful report of what has been
// implemented so far.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/contract"
	"mosaic-agent-test/internal/harness/opencode"
)

func conformanceNativePre(id domain.CollaboratorIdentity, msg domain.TaskMessage, token string) []byte {
	args := opencode.TaskToolArgs{
		SubagentType: id.AgentIdentity,
		Description:  msg.TaskDescription,
		Prompt:       msg.Raw,
	}
	// A real caller plants the token via PlantToken before this payload is
	// ever sent; this fixture stands in for that caller, so it must plant the
	// token the same way rather than merely accepting one that never arrives.
	// TranslateCall recovers the token from this exact field.
	args = opencode.PlantToken(args, token)
	rawArgs, _ := json.Marshal(args)
	b, _ := json.Marshal(opencode.ToolBeforePayload{
		HookEventName: "tool.execute.before",
		Tool:          opencode.InterceptedToolName,
		Args:          rawArgs,
	})
	return b
}

func conformanceNativePost(id domain.CollaboratorIdentity, token string, observed string) []byte {
	// The post-invocation payload echoes the tool's (already rewritten)
	// arguments, which is where the harness preserves the planted token
	// through to the post-invocation point. Plant it here too, for the same
	// reason as conformanceNativePre.
	args := opencode.PlantToken(opencode.TaskToolArgs{SubagentType: id.AgentIdentity}, token)
	rawArgs, _ := json.Marshal(args)
	observedJSON, _ := json.Marshal(observed)
	b, _ := json.Marshal(opencode.ToolAfterPayload{
		HookEventName: "tool.execute.after",
		Tool:          opencode.InterceptedToolName,
		Args:          rawArgs,
		Output:        observedJSON,
	})
	return b
}

func conformanceObserve(t *testing.T, native []byte) contract.ObservedEffect {
	t.Helper()
	var reply opencode.HookReply
	if err := json.Unmarshal(native, &reply); err != nil {
		t.Fatalf("conformanceObserve: malformed reply: %v", err)
	}
	switch reply.Decision {
	case "deny":
		return contract.ObservedEffect{Kind: domain.OutcomeHalt}
	case "allow":
		var updated opencode.TaskToolArgs
		if err := json.Unmarshal(reply.UpdatedArgs, &updated); err != nil {
			t.Fatalf("conformanceObserve: malformed updated args: %v", err)
		}
		token, _ := opencode.RecoverToken(updated)
		return contract.ObservedEffect{
			Kind:             domain.OutcomeRewritePrompt,
			RewrittenPrompt:  string(reply.UpdatedArgs),
			CorrelationToken: token,
		}
	default:
		return contract.ObservedEffect{Kind: domain.OutcomePassthrough}
	}
}

// conformanceCompetingRewriteRequest plants a foreign plugin file directly
// into the sandbox's plugins directory before Provision ever runs, which is
// how the single-rewriter invariant is expressed for this harness's
// directory-drop registration model (see ContractsDesign.md's Design
// Decision 7): presence alone activates a plugin, so a plugins directory
// that already contains an entry this provisioning did not create is a
// second rewriter the foreign-plugin scan must refuse.
func conformanceCompetingRewriteRequest(sb domain.Sandbox, subject domain.SubjectUnderTest) domain.ProvisionRequest {
	pluginsDir := filepath.Join(sb.SubjectDir, opencode.PluginsRelDir)
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		panic("conformanceCompetingRewriteRequest: creating plugins dir: " + err.Error())
	}
	foreign := filepath.Join(pluginsDir, "someone-elses-plugin.ts")
	if err := os.WriteFile(foreign, []byte("// not written by this provisioning\n"), 0o644); err != nil {
		panic("conformanceCompetingRewriteRequest: writing foreign plugin: " + err.Error())
	}

	return domain.ProvisionRequest{
		Sandbox:         sb,
		Subject:         subject,
		InterceptorPath: "interceptor",
		InterceptorArgs: []string{"intercept"},
	}
}

func newConformanceConfig() contract.Config {
	return contract.Config{
		Name: "opencode",
		New: func(t *testing.T, dir string) domain.HarnessAdapter {
			return opencode.New(opencode.Options{})
		},
		NativePre:               conformanceNativePre,
		NativePost:              conformanceNativePost,
		Observe:                 conformanceObserve,
		CompetingRewriteRequest: conformanceCompetingRewriteRequest,
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
