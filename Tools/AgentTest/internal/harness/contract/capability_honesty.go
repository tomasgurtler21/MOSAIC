package contract

import (
	"fmt"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// CheckCapabilityHonesty is the verdict-only counterpart of
// testCapabilityHonesty: instead of failing t directly via t.Errorf, it
// reports the same capability-honesty verdict as a returned error, so a
// caller outside the t.Run subtest tree (see contract_test.go's dishonest
// double tests) can inspect the verdict without the parent *testing.T being
// marked failed as a side effect of a subtest. It is exported so external
// test packages (contract_test) can call it directly.
//
// t is accepted for setup only (cfg.New, cfg.Observe, mustMarshal,
// randomToken); a setup failure is still a genuine failure and is surfaced
// via t.Fatalf as before.
func CheckCapabilityHonesty(t *testing.T, cfg Config) error {
	t.Helper()

	adapter := cfg.New(t, t.TempDir())
	caps := adapter.Capabilities()

	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "Worker"}
	msg := domain.TaskMessage{
		AgentInstanceID: "Worker#1",
		TaskDescription: "do the work",
		InputArtifacts:  []string{},
		OutputArtifacts: []string{},
	}
	token := randomToken(t)

	native := cfg.NativePre(id, msg, token)
	call, err := adapter.TranslateCall(domain.PhasePre, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre): %v", err)
	}

	// The outcome the suite asks for is exactly what a truthful adapter's
	// declared capability predicts: Substitute when direct substitution is
	// supported, RewritePrompt otherwise. This mirrors internal/intercept's
	// own Decide rule, so the suite never needs a harness name to make the
	// choice.
	wantKind := domain.OutcomeRewritePrompt
	outcome := domain.InterceptionOutcome{
		Kind:             domain.OutcomeRewritePrompt,
		RewrittenPrompt:  "please respond with the scripted stub content",
		CorrelationToken: token,
	}
	if caps.SupportsDirectSubstitution {
		wantKind = domain.OutcomeSubstitute
		outcome = domain.InterceptionOutcome{
			Kind:             domain.OutcomeSubstitute,
			StubResponse:     mustMarshal(t, map[string]string{"status_code": "SUCCESS"}),
			CorrelationToken: token,
		}
	}

	reply, err := adapter.TranslateOutcome(outcome, call)
	if err != nil {
		t.Fatalf("TranslateOutcome: %v", err)
	}

	observed := cfg.Observe(t, reply)

	if observed.Kind != wantKind {
		return fmt.Errorf(
			"capability honesty: adapter declares SupportsDirectSubstitution=%v (so effect should be %q) but the observed effect was %q — a capability flag must match what the adapter actually achieves",
			caps.SupportsDirectSubstitution, wantKind, observed.Kind,
		)
	}
	if observed.CorrelationToken != token {
		return fmt.Errorf("capability honesty: observed CorrelationToken = %q, want %q", observed.CorrelationToken, token)
	}
	return nil
}

// testCapabilityHonesty drives a stubbed invocation through the adapter and
// asserts that the observed effect matches the capability the adapter
// declares. This is the single most important test in the suite: an
// adapter claiming direct substitution while only achieving prompt
// rewriting must fail here, because a wrong capability flag otherwise
// produces stubs that are injected unfaithfully in a way no downstream
// report can see.
func testCapabilityHonesty(t *testing.T, cfg Config) {
	t.Helper()
	if err := CheckCapabilityHonesty(t, cfg); err != nil {
		t.Error(err)
	}
}
