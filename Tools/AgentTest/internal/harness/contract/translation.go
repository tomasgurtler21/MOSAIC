package contract

import (
	"fmt"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// CheckTranslationRoundTrip is the verdict-only counterpart of
// testTranslationRoundTrip: instead of failing t directly via t.Errorf, it
// reports the same round-trip verdict as a returned error, so a caller
// outside the t.Run subtest tree (see contract_test.go's dishonest double
// tests) can inspect the verdict without the parent *testing.T being marked
// failed as a side effect of a subtest. It is exported so external test
// packages (contract_test) can call it directly.
//
// t is accepted for setup only (cfg.New, cfg.Observe, mustMarshal,
// randomToken); a setup failure is still a genuine failure and is surfaced
// via t.Fatalf as before.
func CheckTranslationRoundTrip(t *testing.T, cfg Config) error {
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
	const marker = "distinguishing-round-trip-marker"

	native := cfg.NativePre(id, msg, token)
	call, err := adapter.TranslateCall(domain.PhasePre, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre): %v", err)
	}

	var outcome domain.InterceptionOutcome
	if caps.SupportsDirectSubstitution {
		outcome = domain.InterceptionOutcome{
			Kind:             domain.OutcomeSubstitute,
			StubResponse:     mustMarshal(t, map[string]string{"marker": marker}),
			CorrelationToken: token,
		}
	} else {
		outcome = domain.InterceptionOutcome{
			Kind:             domain.OutcomeRewritePrompt,
			RewrittenPrompt:  "respond containing " + marker,
			CorrelationToken: token,
		}
	}

	reply, err := adapter.TranslateOutcome(outcome, call)
	if err != nil {
		t.Fatalf("TranslateOutcome: %v", err)
	}

	observed := cfg.Observe(t, reply)
	combined := observed.SubstitutedBody + observed.RewrittenPrompt
	if !strings.Contains(combined, marker) {
		return fmt.Errorf("translation round trip: marker %q not recoverable from observed effect (substituted=%q, rewritten=%q)", marker, observed.SubstitutedBody, observed.RewrittenPrompt)
	}
	return nil
}

// testTranslationRoundTrip asserts that a native payload translated to a
// normalized call and back to a native response preserves the outcome's
// meaning: the content the suite asked the adapter to deliver is
// recognisable in what Config.Observe reports back.
func testTranslationRoundTrip(t *testing.T, cfg Config) {
	t.Helper()
	if err := CheckTranslationRoundTrip(t, cfg); err != nil {
		t.Error(err)
	}
}

// testMalformedNativePayload asserts that an unrecognised or malformed
// native payload is surfaced as a handleable error, never a panic. An
// adapter that crashes the harness's interception point degrades the
// subject's run, so graceful degradation is a contract obligation, not
// politeness. TranslateCall's doc comment states this obligation without a
// phase qualifier, so both interception points are exercised: PhasePost is
// often the more complex decoding path (it may also consume adapter-side
// scripted state), so it is a materially different code path from PhasePre
// and must be checked independently.
func testMalformedNativePayload(t *testing.T, cfg Config) {
	t.Helper()

	adapter := cfg.New(t, t.TempDir())

	malformed := [][]byte{
		[]byte(`not json at all {{{`),
		[]byte(``),
		[]byte(`{"unexpected_shape_entirely": true}`),
	}

	for _, phase := range []domain.InterceptionPhase{domain.PhasePre, domain.PhasePost} {
		for _, payload := range malformed {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("TranslateCall(%s, %q) panicked instead of returning an error: %v", phase, payload, r)
					}
				}()

				call, err := adapter.TranslateCall(phase, payload)
				if err == nil {
					t.Errorf("TranslateCall(%s, %q): expected an error for a malformed or unrecognised payload, got call=%+v", phase, payload, call)
				}
			}()
		}
	}
}
