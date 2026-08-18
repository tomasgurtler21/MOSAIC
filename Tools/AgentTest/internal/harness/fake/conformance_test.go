package fake_test

// TestConformance drives the reusable adapter conformance suite
// (internal/harness/contract.Run) against this package's own adapter. The
// fake is the suite's first subject and its proof of reusability: once
// internal/harness/fake is fully implemented, this test must pass exactly
// like every other adapter's own conformance run, with no special case for
// "the fake one" anywhere in the suite.

import (
	"encoding/json"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/contract"
	"mosaic-agent-test/internal/harness/fake"
)

type wireReply struct {
	Kind   string          `json:"kind"`
	Body   json.RawMessage `json:"body,omitempty"`
	Prompt string          `json:"prompt,omitempty"`
	Token  string          `json:"token"`
}

func conformanceObserve(t *testing.T, native []byte) contract.ObservedEffect {
	t.Helper()
	var w wireReply
	if err := json.Unmarshal(native, &w); err != nil {
		t.Fatalf("conformanceObserve: malformed reply: %v", err)
	}
	return contract.ObservedEffect{
		Kind:             domain.OutcomeKind(w.Kind),
		SubstitutedBody:  string(w.Body),
		RewrittenPrompt:  w.Prompt,
		CorrelationToken: w.Token,
	}
}

// conformanceNativePost adapts contract.Config's NativePost signature (which
// carries an externally observed response, meaningful for a real harness)
// onto this package's nativePost, which carries none — the fake supplies the
// observed response from its own Script instead.
func conformanceNativePost(id domain.CollaboratorIdentity, token string, observed string) []byte {
	return nativePost(id, token)
}

func newConformanceConfig(caps domain.HarnessCapabilities) contract.Config {
	return contract.Config{
		Name: "fake",
		New: func(t *testing.T, dir string) domain.HarnessAdapter {
			return fake.New(fake.Options{
				Capabilities: caps,
				Script: map[string][]fake.Turn{
					workerID.Key(): {{Body: "scripted collaborator response"}},
				},
			})
		},
		NativePre:  nativePre,
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

func TestConformance_SubstitutionCapable(t *testing.T) {
	contract.Run(t, newConformanceConfig(domain.HarnessCapabilities{
		SupportsDirectSubstitution: true,
		SupportsPostInterception:   true,
		CorrelationField:           "token",
	}))
}

func TestConformance_RewriteOnly(t *testing.T) {
	contract.Run(t, newConformanceConfig(domain.HarnessCapabilities{
		SupportsDirectSubstitution: false,
		SupportsPostInterception:   true,
		CorrelationField:           "token",
	}))
}
