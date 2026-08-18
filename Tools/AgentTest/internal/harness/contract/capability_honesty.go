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

	// Normalized vocabulary: NativePre encodes it into the harness's own
	// wire shape, per Config.NativePre's doc comment.
	id := domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Worker"}
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
	// For adapters that plant the correlation token in the outbound reply
	// (prompt-based correlation), verify the token is visible in the observed
	// effect. For identifier-based adapters, the token rides on the harness's
	// own inbound field and is not present in the outbound reply — Config.Observe
	// cannot extract it from the reply direction, so observed.CorrelationToken
	// is empty and the check is skipped. The Correlation subtest covers token
	// survival on the inbound path for all adapters.
	if observed.CorrelationToken != "" && observed.CorrelationToken != token {
		return fmt.Errorf("capability honesty: observed CorrelationToken = %q, want %q", observed.CorrelationToken, token)
	}

	if err := checkReplyRecoveryHonesty(t, cfg, caps); err != nil {
		return err
	}
	return nil
}

// checkReplyRecoveryHonesty holds SupportsReplyRecovery to the same standard
// CheckCapabilityHonesty already holds SupportsDirectSubstitution to: a
// declared capability must be demonstrated, not merely asserted. Driving
// only a synthetic PhasePre/PhasePost round trip — as the rest of this check
// does — cannot demonstrate it, because reply recovery is specifically about
// what happens at a phase neither of those payloads exercises. A synthetic
// payload alone must not be able to satisfy this declaration.
func checkReplyRecoveryHonesty(t *testing.T, cfg Config, caps domain.HarnessCapabilities) error {
	t.Helper()

	if !caps.SupportsReplyRecovery {
		return nil
	}
	if cfg.NativeCompletion == nil {
		return fmt.Errorf(
			"capability honesty: adapter declares SupportsReplyRecovery=true but Config.NativeCompletion is nil — " +
				"the suite has no seam to drive a real completion payload through TranslateCall(domain.PhaseCompletion, ...), " +
				"so this declaration is currently unverifiable rather than honoured",
		)
	}

	adapter := cfg.New(t, t.TempDir())
	const wantReply = "the collaborator's real completion reply, recovered from the completion signal"

	native := cfg.NativeCompletion(wantReply)
	call, err := adapter.TranslateCall(domain.PhaseCompletion, native)
	if err != nil {
		return fmt.Errorf("capability honesty: TranslateCall(PhaseCompletion): %v", err)
	}
	if call.ObservedResponse != wantReply {
		return fmt.Errorf(
			"capability honesty: adapter declares SupportsReplyRecovery=true but TranslateCall(PhaseCompletion) returned ObservedResponse=%q, want the recovered reply %q — a declared capability must match what the adapter actually observes",
			call.ObservedResponse, wantReply,
		)
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
