package app

// detect_harness_match_test.go covers DetectHarnessMatch — the pure harness identity and
// mismatch detection function introduced in Stage 5. Tests are in package app (not package
// app_test) because DetectHarnessMatch is a pure internal function in the same convention
// as buildGenericAgent and eligibleHarnessOnly.
//
// Verified behaviours (T5.1):
//
// Positive match (HarnessMatchYes):
//   - A file deployed for alpha-harness, when checked against the alpha descriptor,
//     yields HarnessMatchYes (positive distinguishing evidence found).
//   - The returned Evidence list is non-empty and contains at least one signal naming
//     an alpha-distinctive field or tool name.
//   - HarnessMatchYes verdicts carry an empty Reason.
//
// Negative match / mismatch (HarnessMatchNo):
//   - A file deployed for beta-harness, when checked against the alpha descriptor,
//     yields HarnessMatchNo (positive evidence of a different harness).
//   - The returned Reason is non-empty and human-readable.
//   - The Evidence for HarnessMatchNo contains information about the conflicting signals.
//
// Conservative indeterminate (HarnessMatchIndeterminate):
//   - A valid MOSAIC agent carrying only MOSAIC-known generic vocabulary keys and no
//     harness-distinctive signals yields HarnessMatchIndeterminate when checked against
//     any harness. Absence of signals is never declared as a mismatch.
//   - The Reason for HarnessMatchIndeterminate is non-empty (explains why detection
//     could not determine harness ownership).
//
// Non-agent rejection (HarnessMatchNotAgent):
//   - A file without transform_version (fails eligibility signal one) yields
//     HarnessMatchNotAgent.
//   - The Reason for HarnessMatchNotAgent carries the EligibilityVerdict's Reason
//     verbatim so the caller can report the specific disqualifying signal.
//   - Completely unparseable bytes yield HarnessMatchNotAgent (not a panic or error).
//
// Purity:
//   - The input src bytes are byte-identical before and after the call.
//   - Two calls with identical arguments return verdicts with identical fields.

import (
	"bytes"
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Positive match tests
// ---------------------------------------------------------------------------

// TestDetectHarnessMatch_AlphaAgentMatchesAlphaDescriptor verifies that a file deployed
// for alpha-harness yields HarnessMatchYes when checked against the alpha descriptor.
// This is the canonical "correct harness" case.
func TestDetectHarnessMatch_AlphaAgentMatchesAlphaDescriptor(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "retarget-test-agent")

	if verdict.Status != HarnessMatchYes {
		t.Errorf("DetectHarnessMatch status = %q, want %q; Reason: %q",
			verdict.Status, HarnessMatchYes, verdict.Reason)
	}
	if verdict.Reason != "" {
		t.Errorf("HarnessMatchYes Reason should be empty, got %q", verdict.Reason)
	}
}

// TestDetectHarnessMatch_AlphaMatchEvidence verifies that when a match is detected, the
// Evidence list is non-empty and names at least one alpha-distinctive signal.
// This makes the detection reason auditable and testable without whole-file comparison.
func TestDetectHarnessMatch_AlphaMatchEvidence(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "retarget-test-agent")

	if verdict.Status != HarnessMatchYes {
		t.Fatalf("expected HarnessMatchYes, got %q", verdict.Status)
	}
	if len(verdict.Evidence) == 0 {
		t.Error("Evidence must be non-empty for HarnessMatchYes; detection found no signals")
	}
}

// TestDetectHarnessMatch_BetaAgentMatchesBetaDescriptor verifies that a file deployed for
// beta-harness yields HarnessMatchYes when checked against the beta descriptor.
// Mirrors the alpha-match test to confirm the logic is not harness-specific.
func TestDetectHarnessMatch_BetaAgentMatchesBetaDescriptor(t *testing.T) {
	src := betaDeployedAgentBytes()
	beta := newBetaHarnessModule()

	verdict := DetectHarnessMatch(src, beta, domain.ArtifactAgent, "beta-agent")

	if verdict.Status != HarnessMatchYes {
		t.Errorf("DetectHarnessMatch status = %q, want %q; Reason: %q",
			verdict.Status, HarnessMatchYes, verdict.Reason)
	}
	if verdict.Reason != "" {
		t.Errorf("HarnessMatchYes Reason should be empty, got %q", verdict.Reason)
	}
}

// ---------------------------------------------------------------------------
// Mismatch / negative tests
// ---------------------------------------------------------------------------

// TestDetectHarnessMatch_BetaAgentMismatchesAlphaDescriptor verifies that a file deployed
// for beta-harness yields HarnessMatchNo when checked against the alpha descriptor.
// The beta agent has distinctive non-alpha signals (model_id, allowed_tools, beta_stamp,
// BetaBash / BetaRead tool names) that identify it as belonging to a different harness.
func TestDetectHarnessMatch_BetaAgentMismatchesAlphaDescriptor(t *testing.T) {
	src := betaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "beta-agent")

	if verdict.Status != HarnessMatchNo {
		t.Errorf("DetectHarnessMatch status = %q, want %q; Reason: %q",
			verdict.Status, HarnessMatchNo, verdict.Reason)
	}
	if verdict.Reason == "" {
		t.Error("HarnessMatchNo must carry a non-empty human-readable Reason")
	}
}

// TestDetectHarnessMatch_AlphaAgentMismatchesBetaDescriptor verifies the reverse direction:
// a file deployed for alpha-harness yields HarnessMatchNo when checked against the beta
// descriptor. The alpha agent has distinctive non-beta signals.
func TestDetectHarnessMatch_AlphaAgentMismatchesBetaDescriptor(t *testing.T) {
	src := alphaDeployedAgentBytes()
	beta := newBetaHarnessModule()

	verdict := DetectHarnessMatch(src, beta, domain.ArtifactAgent, "retarget-test-agent")

	if verdict.Status != HarnessMatchNo {
		t.Errorf("DetectHarnessMatch status = %q, want %q; Reason: %q",
			verdict.Status, HarnessMatchNo, verdict.Reason)
	}
	if verdict.Reason == "" {
		t.Error("HarnessMatchNo must carry a non-empty human-readable Reason")
	}
}

// TestDetectHarnessMatch_MismatchEvidence verifies that a mismatch verdict populates the
// Evidence list with the conflicting signals it found. This is the foundation of the
// human-readable explanation the caller uses in the TransformFileOutcome.
func TestDetectHarnessMatch_MismatchEvidence(t *testing.T) {
	src := betaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "beta-agent")

	if verdict.Status != HarnessMatchNo {
		t.Fatalf("expected HarnessMatchNo, got %q", verdict.Status)
	}
	// Evidence should be non-empty: the detection must record what signals it found
	// when reaching a mismatch verdict.
	if len(verdict.Evidence) == 0 {
		t.Error("Evidence must be non-empty for HarnessMatchNo; detection found no conflicting signals")
	}
}

// ---------------------------------------------------------------------------
// Conservative indeterminate tests
// ---------------------------------------------------------------------------

// TestDetectHarnessMatch_MosaicOnlyAgentIsIndeterminate verifies the conservative invariant:
// a valid MOSAIC agent carrying only MOSAIC-known keys (no harness-distinctive fields or
// tool names) yields HarnessMatchIndeterminate when checked against any harness. Absence
// of this harness's signals is never declared as a mismatch.
func TestDetectHarnessMatch_MosaicOnlyAgentIsIndeterminate(t *testing.T) {
	src := mosaicOnlyAgentBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "bare-agent")

	if verdict.Status != HarnessMatchIndeterminate {
		t.Errorf("DetectHarnessMatch status = %q, want %q; Reason: %q",
			verdict.Status, HarnessMatchIndeterminate, verdict.Reason)
	}
	if verdict.Reason == "" {
		t.Error("HarnessMatchIndeterminate must carry a non-empty Reason explaining why detection was inconclusive")
	}
}

// TestDetectHarnessMatch_MosaicOnlyAgentIndeterminateForBeta verifies the conservative
// invariant holds for the beta descriptor as well — same sparse file, different descriptor,
// same indeterminate outcome.
func TestDetectHarnessMatch_MosaicOnlyAgentIndeterminateForBeta(t *testing.T) {
	src := mosaicOnlyAgentBytes()
	beta := newBetaHarnessModule()

	verdict := DetectHarnessMatch(src, beta, domain.ArtifactAgent, "bare-agent")

	if verdict.Status != HarnessMatchIndeterminate {
		t.Errorf("DetectHarnessMatch status = %q, want %q; Reason: %q",
			verdict.Status, HarnessMatchIndeterminate, verdict.Reason)
	}
}

// ---------------------------------------------------------------------------
// Non-agent rejection tests
// ---------------------------------------------------------------------------

// TestDetectHarnessMatch_FileWithoutTransformVersionIsNotAgent verifies that a file
// lacking transform_version (fails eligibility signal one) yields HarnessMatchNotAgent.
// This ensures the shared eligibility check is always consulted first, so the detection
// function is not the only gate against non-agent files.
func TestDetectHarnessMatch_FileWithoutTransformVersionIsNotAgent(t *testing.T) {
	src := notAnAgentBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "plain-doc")

	if verdict.Status != HarnessMatchNotAgent {
		t.Errorf("DetectHarnessMatch status = %q, want %q", verdict.Status, HarnessMatchNotAgent)
	}
	if verdict.Reason == "" {
		t.Error("HarnessMatchNotAgent must carry the eligibility verdict's Reason verbatim")
	}
}

// TestDetectHarnessMatch_NotAgentReasonPreservesEligibilityReason verifies that the Reason
// in a HarnessMatchNotAgent verdict is the verbatim reason from the EligibilityVerdict, so
// the caller receives the specific disqualifying signal without reformatting.
func TestDetectHarnessMatch_NotAgentReasonPreservesEligibilityReason(t *testing.T) {
	// A file with no frontmatter at all — eligibility fails with "no frontmatter block".
	src := []byte("Just plain text with no frontmatter and no MOSAIC tags.\n")
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "plain")

	if verdict.Status != HarnessMatchNotAgent {
		t.Fatalf("expected HarnessMatchNotAgent, got %q", verdict.Status)
	}
	// The reason must be non-empty and must describe the eligibility failure
	// (not a generic "not an agent" message that erases the specific cause).
	if verdict.Reason == "" {
		t.Error("Reason must carry the specific eligibility failure message")
	}
}

// TestDetectHarnessMatch_UnparseableBytesYieldNotAgent verifies that completely
// unparseable bytes produce HarnessMatchNotAgent without panicking or returning an error.
func TestDetectHarnessMatch_UnparseableBytesYieldNotAgent(t *testing.T) {
	// Bytes that cannot be parsed as a YAML-frontmatter document.
	src := []byte("---\nnot: valid: yaml: {\n---\n")
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "broken")

	if verdict.Status != HarnessMatchNotAgent {
		t.Errorf("DetectHarnessMatch status = %q, want %q", verdict.Status, HarnessMatchNotAgent)
	}
	if verdict.Reason == "" {
		t.Error("HarnessMatchNotAgent from unparseable bytes must carry a non-empty Reason")
	}
}

// TestDetectHarnessMatch_MixedPositiveAndUnknownEvidenceIsIndeterminate verifies the
// conservative boundary: a file that carries positive distinguishing evidence for this harness
// (ClassHarness fields or Universe tool names) alongside fields unknown to this harness must
// yield HarnessMatchIndeterminate rather than HarnessMatchNo.
//
// This is the false-positive shape called out in the Risk table: a file that is otherwise
// unambiguously alpha-shaped but also carries one genuinely idiosyncratic, non-harness-owned
// key (e.g. a hand-added annotation field). The unknown field is not positive evidence of a
// different harness — it could be an annotation. A hard HarnessMatchNo verdict requires
// unknown fields with NO positive same-harness evidence to support it.
func TestDetectHarnessMatch_MixedPositiveAndUnknownEvidenceIsIndeterminate(t *testing.T) {
	// A file that carries alpha_stamp (ClassHarness for alpha — positive evidence) alongside
	// hand_added_annotation (unknown to alpha and to all other harnesses — not evidence of a
	// specific different harness). The conservative rule must not let the unknown field override
	// the positive evidence and declare HarnessMatchNo.
	src := []byte("---\n" +
		"transform_version: \"alpha-1.0.0\"\n" +
		"injections_version: \"alpha-1.0.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"99\"\n" +
		"name: mixed-evidence-agent\n" +
		"description: Agent with mixed positive and unknown evidence\n" +
		"role: subagent\n" +
		"model: claude-3-5-sonnet\n" +
		"tools:\n" +
		"- AlphaBash\n" +
		"alpha_stamp: alpha-v1\n" +          // ClassHarness for alpha: positive evidence
		"hand_added_annotation: my-value\n" + // unknown to alpha: not a different-harness identifier
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are the MixedEvidenceAgent.\n" +
		"[[/SECTION:Identity]]\n")

	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "mixed-evidence-agent")

	if verdict.Status != HarnessMatchIndeterminate {
		t.Errorf("DetectHarnessMatch status = %q, want %q; mixed positive+unknown evidence must not "+
			"produce a hard mismatch verdict; Reason: %q",
			verdict.Status, HarnessMatchIndeterminate, verdict.Reason)
	}
	if verdict.Reason == "" {
		t.Error("HarnessMatchIndeterminate must carry a non-empty Reason explaining the ambiguity")
	}
}

// ---------------------------------------------------------------------------
// Purity tests
// ---------------------------------------------------------------------------

// TestDetectHarnessMatch_SourceBytesUnchanged verifies that the source bytes are not
// mutated by the detection call. Pure function contract.
func TestDetectHarnessMatch_SourceBytesUnchanged(t *testing.T) {
	src := alphaDeployedAgentBytes()
	before := make([]byte, len(src))
	copy(before, src)

	alpha := newAlphaHarnessModule()
	DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "retarget-test-agent")

	if !bytes.Equal(src, before) {
		t.Error("DetectHarnessMatch mutated the source bytes; it must be pure")
	}
}

// TestDetectHarnessMatch_DeterministicOutput verifies that two calls with identical
// arguments return verdicts with identical Status, Reason, and Evidence values.
func TestDetectHarnessMatch_DeterministicOutput(t *testing.T) {
	src := alphaDeployedAgentBytes()
	alpha := newAlphaHarnessModule()

	v1 := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "retarget-test-agent")
	v2 := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "retarget-test-agent")

	if v1.Status != v2.Status {
		t.Errorf("non-deterministic Status: first call %q, second call %q", v1.Status, v2.Status)
	}
	if v1.Reason != v2.Reason {
		t.Errorf("non-deterministic Reason: first call %q, second call %q", v1.Reason, v2.Reason)
	}
	if len(v1.Evidence) != len(v2.Evidence) {
		t.Errorf("non-deterministic Evidence length: first call %d, second call %d",
			len(v1.Evidence), len(v2.Evidence))
	}
	for i, e1 := range v1.Evidence {
		if e1 != v2.Evidence[i] {
			t.Errorf("non-deterministic Evidence[%d]: first call %q, second call %q", i, e1, v2.Evidence[i])
		}
	}
}
