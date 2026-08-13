package app

// infrastructure_vocabulary_retarget_test.go verifies Stage 5 behaviour at the
// DetectHarnessMatch level: infrastructure-agent files (carrying "infrastructure",
// "triggers", and "on_failure" frontmatter fields) are not classified as
// HarnessMatchNo and therefore are not reported as skipped-mismatch when
// TransformHarness processes a directory containing them.
//
// These tests are the TDD RED phase for I5.1. Before the fix, the three fields
// classify as ClassUnknown (hasNegative=true, hasPositive=false), and the verdict
// rules take the hasNegative && !hasPositive branch → HarnessMatchNo → StatusSkippedMismatch.
// After the fix the fields classify as ClassMosaic (hasNegative=false, hasPositive=false)
// → the verdict rules take the !hasPositive && !hasNegative branch
// → HarnessMatchIndeterminate, which transformHarness treats the same as HarnessMatchYes
// so the agent transforms normally with an informational per-file warning.
//
// Verified behaviours (T5.2):
//
// Infrastructure agent (non-mismatch):
//   - An infrastructure-agent file carrying "infrastructure", "triggers", and "on_failure"
//     alongside MOSAIC-known fields yields HarnessMatchIndeterminate, not HarnessMatchNo.
//   - The verdict is HarnessMatchIndeterminate (not HarnessMatchNo) for both the alpha and
//     the beta descriptor, confirming the fix is at the vocabulary level.
//
// Foreign-field regression guard (AC5.4):
//   - A file carrying a genuinely foreign field (not MOSAIC vocabulary and not declared by
//     the harness) still yields HarnessMatchNo after the Stage 5 fix.
//   - The mismatch reason for a genuinely foreign field is non-empty.
//   - An infrastructure-agent file with an additional foreign field still yields
//     HarnessMatchNo (the presence of vocabulary-correct infrastructure fields does not
//     absorb a genuinely unknown key).

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Infrastructure agent byte fixture
// ---------------------------------------------------------------------------

// infraAgentBytes returns bytes of a minimal infrastructure agent file carrying the
// three infrastructure-agent-specific frontmatter fields alongside the two eligibility
// signals (transform_version and canonical SECTION tags). Before the Stage 5 fix the
// three infrastructure fields classify as ClassUnknown → hasNegative=true → HarnessMatchNo.
func infraAgentBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"version: \"2.0\"\n" +
		"id: \"10\"\n" +
		"name: checkpoint-agent\n" +
		"description: Creates checkpoints between stages\n" +
		"role: worker\n" +
		"infrastructure: checkpoint\n" +
		"triggers:\n" +
		"- on_stage_complete\n" +
		"on_failure: notify\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are the CheckpointAgent.\n" +
		"[[/SECTION:Identity]]\n")
}

// foreignFieldAgentBytes returns bytes of an agent file that carries a genuinely foreign
// field ("unknown_harness_field") alongside the two eligibility signals. This field is not
// MOSAIC vocabulary and is not declared by any test harness descriptor, so it must still
// produce HarnessMatchNo after the Stage 5 vocabulary widening (AC5.4 regression guard).
func foreignFieldAgentBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"version: \"1.0\"\n" +
		"id: \"99\"\n" +
		"name: foreign-field-agent\n" +
		"description: An agent carrying a genuinely foreign field\n" +
		"role: worker\n" +
		"unknown_harness_field: some-value\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are the ForeignFieldAgent.\n" +
		"[[/SECTION:Identity]]\n")
}

// infraAgentWithForeignFieldBytes returns bytes of an infrastructure agent that carries
// all three vocabulary-correct infrastructure fields AND one genuinely foreign field.
// Used to verify that vocabulary-correct fields do not absorb a co-resident unknown key.
func infraAgentWithForeignFieldBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"version: \"2.0\"\n" +
		"id: \"11\"\n" +
		"name: mixed-infra-agent\n" +
		"description: Infrastructure agent with an additional foreign field\n" +
		"role: worker\n" +
		"infrastructure: checkpoint\n" +
		"triggers:\n" +
		"- on_stage_complete\n" +
		"on_failure: notify\n" +
		"unknown_harness_field: intruding-value\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are the MixedInfraAgent.\n" +
		"[[/SECTION:Identity]]\n")
}

// ---------------------------------------------------------------------------
// T5.2 — Infrastructure agent does not produce a mismatch verdict
// ---------------------------------------------------------------------------

// TestDetectHarnessMatch_InfrastructureAgent_NotMismatch verifies that an infrastructure
// agent file carrying "infrastructure", "triggers", and "on_failure" frontmatter fields
// does NOT produce HarnessMatchNo when checked against a harness descriptor. This is the
// primary regression test for the skipped-mismatch failure reported in Stage 5.
//
// Before the fix: three ClassUnknown fields → hasNegative=true → HarnessMatchNo.
// After the fix: three ClassMosaic fields → hasNegative=false → HarnessMatchIndeterminate.
func TestDetectHarnessMatch_InfrastructureAgent_NotMismatch(t *testing.T) {
	src := infraAgentBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "checkpoint-agent")

	if verdict.Status == HarnessMatchNo {
		t.Errorf("DetectHarnessMatch returned HarnessMatchNo for an infrastructure agent; "+
			"Reason: %q; "+
			"\"infrastructure\", \"triggers\", and \"on_failure\" must be recognized as "+
			"MOSAIC vocabulary (ClassMosaic via agentfields.genericVocabularyKeys) — "+
			"ClassUnknown for these fields sets hasNegative=true → HarnessMatchNo → "+
			"skipped-mismatch; the fix is to add them to genericVocabularyKeys (I5.1)",
			verdict.Reason)
	}
}

// TestDetectHarnessMatch_InfrastructureAgent_IsIndeterminate verifies the expected verdict
// outcome for an infrastructure agent: HarnessMatchIndeterminate. The file carries only
// MOSAIC-known fields plus the three infrastructure vocabulary fields — no harness-distinctive
// evidence. After the fix: hasNegative=false, hasPositive=false → HarnessMatchIndeterminate,
// which transformHarness treats as HarnessMatchYes so the agent is transformed.
func TestDetectHarnessMatch_InfrastructureAgent_IsIndeterminate(t *testing.T) {
	src := infraAgentBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "checkpoint-agent")

	if verdict.Status != HarnessMatchIndeterminate {
		t.Errorf("DetectHarnessMatch status = %q, want %q for an infrastructure agent; "+
			"Reason: %q; "+
			"with \"infrastructure\", \"triggers\", and \"on_failure\" classified as ClassMosaic "+
			"(not ClassUnknown), hasNegative must be false; with no harness-distinctive evidence "+
			"hasPositive is also false; the verdict rules must therefore land on "+
			"HarnessMatchIndeterminate — the agent transforms, not skips",
			verdict.Status, HarnessMatchIndeterminate, verdict.Reason)
	}
}

// TestDetectHarnessMatch_InfrastructureAgent_IndeterminateForBeta verifies the
// HarnessMatchIndeterminate outcome holds for a second harness descriptor. This confirms
// the fix acts at the vocabulary level (agentfields.genericVocabularyKeys applies globally)
// rather than through any per-harness special case, satisfying AC5.5.
func TestDetectHarnessMatch_InfrastructureAgent_IndeterminateForBeta(t *testing.T) {
	src := infraAgentBytes()
	beta := newBetaHarnessModule()

	verdict := DetectHarnessMatch(src, beta, domain.ArtifactAgent, "checkpoint-agent")

	if verdict.Status != HarnessMatchIndeterminate {
		t.Errorf("DetectHarnessMatch status = %q, want %q for an infrastructure agent checked "+
			"against the beta descriptor; Reason: %q; "+
			"vocabulary-level recognition via agentfields.genericVocabularyKeys must be "+
			"harness-agnostic — the indeterminate outcome must not depend on which "+
			"descriptor is queried (AC5.5: no special-casing outside internal/agentfields)",
			verdict.Status, HarnessMatchIndeterminate, verdict.Reason)
	}
}

// ---------------------------------------------------------------------------
// T5.2 — Genuinely foreign field still yields mismatch (AC5.4 regression guard)
// ---------------------------------------------------------------------------

// TestDetectHarnessMatch_ForeignUnknownField_StillYieldsMismatch verifies that a file
// carrying a genuinely foreign field (not MOSAIC vocabulary, not declared by the harness)
// still produces HarnessMatchNo after the Stage 5 vocabulary widening. Adding the three
// infrastructure fields to genericVocabularyKeys must not over-extend the vocabulary:
// truly unknown fields must still cause a mismatch verdict (AC5.4).
func TestDetectHarnessMatch_ForeignUnknownField_StillYieldsMismatch(t *testing.T) {
	src := foreignFieldAgentBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "foreign-field-agent")

	if verdict.Status != HarnessMatchNo {
		t.Errorf("DetectHarnessMatch status = %q, want %q for a file carrying a genuinely "+
			"foreign unknown field (\"unknown_harness_field\"); Reason: %q; "+
			"the Stage 5 vocabulary widening covers only the three infrastructure-agent fields — "+
			"any field that is not MOSAIC vocabulary and not declared by the harness must still "+
			"yield HarnessMatchNo (AC5.4: genuinely foreign fields must not pass silently)",
			verdict.Status, HarnessMatchNo, verdict.Reason)
	}
}

// TestDetectHarnessMatch_ForeignUnknownField_MismatchReasonNonEmpty verifies that the
// mismatch verdict for a genuinely foreign field carries a non-empty human-readable Reason,
// so the caller can surface which field triggered the mismatch in the per-file outcome.
func TestDetectHarnessMatch_ForeignUnknownField_MismatchReasonNonEmpty(t *testing.T) {
	src := foreignFieldAgentBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "foreign-field-agent")

	if verdict.Status != HarnessMatchNo {
		t.Fatalf("expected HarnessMatchNo for a foreign-field agent, got %q; "+
			"cannot assert Reason without a mismatch verdict", verdict.Status)
	}
	if verdict.Reason == "" {
		t.Error("HarnessMatchNo for a genuinely foreign field must carry a non-empty Reason; " +
			"the Reason is surfaced in the per-file TransformFileOutcome so the user can " +
			"identify which field triggered the mismatch")
	}
}

// TestDetectHarnessMatch_InfraFieldsDoNotAbsorbAdditionalForeignField verifies that the
// presence of vocabulary-correct infrastructure fields ("infrastructure", "triggers",
// "on_failure") does NOT prevent a co-resident genuinely foreign field from triggering
// HarnessMatchNo. Each unknown field contributes independently to the hasNegative signal.
func TestDetectHarnessMatch_InfraFieldsDoNotAbsorbAdditionalForeignField(t *testing.T) {
	src := infraAgentWithForeignFieldBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "mixed-infra-agent")

	if verdict.Status != HarnessMatchNo {
		t.Errorf("DetectHarnessMatch status = %q, want %q for a file that carries "+
			"vocabulary-correct infrastructure fields AND a genuinely foreign field; "+
			"Reason: %q; "+
			"ClassMosaic recognition of \"infrastructure\", \"triggers\", and \"on_failure\" "+
			"must not absorb co-resident unknown fields — each ClassUnknown field still "+
			"sets hasNegative=true and must still trigger a HarnessMatchNo verdict (AC5.4)",
			verdict.Status, HarnessMatchNo, verdict.Reason)
	}
}
