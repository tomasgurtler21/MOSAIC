package contract

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// bannedCorrelationVocabulary is checked against the declared correlation
// field name. A field like "mosaic_test_token" would tip off the subject
// that it is being exercised, defeating the entire point of interception —
// the subject must not be able to tell it is being tested.
var bannedCorrelationVocabulary = []string{"test", "mosaic", "stub", "fake", "mock", "harness"}

// testCorrelation asserts that the field an adapter nominates to carry the
// correlation token survives the pre-to-post round trip, that a
// post-invocation record can be keyed back to its start record by it, and
// that neither the field name nor the token itself reveals anything about
// testing.
func testCorrelation(t *testing.T, cfg Config) {
	t.Helper()

	adapter := cfg.New(t, t.TempDir())
	caps := adapter.Capabilities()

	field := strings.ToLower(caps.CorrelationField)
	for _, banned := range bannedCorrelationVocabulary {
		if strings.Contains(field, banned) {
			t.Errorf("correlation: CorrelationField %q contains test-revealing vocabulary %q", caps.CorrelationField, banned)
		}
	}

	// Normalized vocabulary: NativePre/NativePost encode it into the
	// harness's own wire shape, per Config.NativePre's doc comment.
	id := domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Worker"}
	msg := domain.TaskMessage{
		AgentInstanceID: "Worker#1",
		TaskDescription: "do the work",
		InputArtifacts:  []string{},
		OutputArtifacts: []string{},
	}
	token := randomToken(t)
	for _, banned := range bannedCorrelationVocabulary {
		if strings.Contains(strings.ToLower(token), banned) {
			t.Fatalf("correlation: test-generated token %q unexpectedly contains %q; fix randomToken", token, banned)
		}
	}

	preNative := cfg.NativePre(id, msg, token)
	preCall, err := adapter.TranslateCall(domain.PhasePre, preNative)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre): %v", err)
	}
	if preCall.CorrelationToken != token {
		t.Fatalf("correlation: pre-invocation token = %q, want %q", preCall.CorrelationToken, token)
	}

	postNative := cfg.NativePost(id, token, "the collaborator's observed response")
	postCall, err := adapter.TranslateCall(domain.PhasePost, postNative)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost): %v", err)
	}
	if postCall.CorrelationToken != token {
		t.Fatalf("correlation: post-invocation token = %q, want %q", postCall.CorrelationToken, token)
	}

	// The whole point of the token is that a post record keys back to its
	// start record by it, without relying on identity or sequence alone.
	if preCall.CorrelationToken != postCall.CorrelationToken {
		t.Errorf("correlation: pre token %q and post token %q do not key the same invocation", preCall.CorrelationToken, postCall.CorrelationToken)
	}
}
