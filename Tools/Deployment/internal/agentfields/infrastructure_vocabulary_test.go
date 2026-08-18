package agentfields_test

// infrastructure_vocabulary_test.go verifies that the three infrastructure-agent
// frontmatter fields — infrastructure, triggers, and on_failure — are recognized
// as MOSAIC generic vocabulary keys and therefore as known MOSAIC keys.
//
// These tests are the TDD RED phase for I5.1. They fail until infrastructure,
// triggers, and on_failure are added to genericVocabularyKeys in agentfields.go.
//
// Verified behaviours:
//
//   - IsGenericVocabularyKey returns true for "infrastructure", "triggers", and "on_failure".
//   - IsKnownMosaicKey returns true for all three fields (derives from IsGenericVocabularyKey).
//   - The three fields are not in the deployed-field registry (they are generic-source
//     vocabulary, not MOSAIC-owned bookkeeping stamps).

import (
	"testing"

	"mosaic-deploy/internal/agentfields"
)

// ---------------------------------------------------------------------------
// IsGenericVocabularyKey — infrastructure fields
// ---------------------------------------------------------------------------

// TestIsGenericVocabularyKey_InfrastructureField_True verifies that "infrastructure"
// is recognized as a generic vocabulary key. Before the Stage 5 fix this returns false,
// causing infrastructure agents to carry ClassUnknown fields that trigger hasNegative=true
// in DetectHarnessMatch and produce a skipped-mismatch verdict.
func TestIsGenericVocabularyKey_InfrastructureField_True(t *testing.T) {
	if !agentfields.IsGenericVocabularyKey("infrastructure") {
		t.Error("IsGenericVocabularyKey(\"infrastructure\") = false, want true; " +
			"\"infrastructure\" is an infrastructure-agent frontmatter field declared in " +
			"Catalog/SourceFilesFormat.md and must be added to " +
			"genericVocabularyKeys so it classifies as ClassMosaic rather than ClassUnknown")
	}
}

// TestIsGenericVocabularyKey_TriggersField_True verifies that "triggers"
// is recognized as a generic vocabulary key.
func TestIsGenericVocabularyKey_TriggersField_True(t *testing.T) {
	if !agentfields.IsGenericVocabularyKey("triggers") {
		t.Error("IsGenericVocabularyKey(\"triggers\") = false, want true; " +
			"\"triggers\" is an infrastructure-agent frontmatter field and must be added to " +
			"genericVocabularyKeys so it classifies as ClassMosaic rather than ClassUnknown")
	}
}

// TestIsGenericVocabularyKey_OnFailureField_True verifies that "on_failure"
// is recognized as a generic vocabulary key.
func TestIsGenericVocabularyKey_OnFailureField_True(t *testing.T) {
	if !agentfields.IsGenericVocabularyKey("on_failure") {
		t.Error("IsGenericVocabularyKey(\"on_failure\") = false, want true; " +
			"\"on_failure\" is an infrastructure-agent frontmatter field and must be added to " +
			"genericVocabularyKeys so it classifies as ClassMosaic rather than ClassUnknown")
	}
}

// TestIsGenericVocabularyKey_InfrastructureFields_AllTrue verifies all three
// infrastructure-agent fields in one table-driven assertion.
func TestIsGenericVocabularyKey_InfrastructureFields_AllTrue(t *testing.T) {
	infraKeys := []string{"infrastructure", "triggers", "on_failure"}
	for _, key := range infraKeys {
		if !agentfields.IsGenericVocabularyKey(key) {
			t.Errorf("IsGenericVocabularyKey(%q) = false, want true; "+
				"all three infrastructure-agent frontmatter fields must be present in "+
				"genericVocabularyKeys so infrastructure agents are not treated as carrying "+
				"unknown fields and are not reported as skipped-mismatch",
				key)
		}
	}
}

// ---------------------------------------------------------------------------
// IsKnownMosaicKey — infrastructure fields
// ---------------------------------------------------------------------------

// TestIsKnownMosaicKey_InfrastructureField_True verifies that "infrastructure"
// is recognized as a known MOSAIC key. IsKnownMosaicKey is the predicate
// FieldClassifier.Classify calls to determine ClassMosaic.
func TestIsKnownMosaicKey_InfrastructureField_True(t *testing.T) {
	if !agentfields.IsKnownMosaicKey("infrastructure") {
		t.Error("IsKnownMosaicKey(\"infrastructure\") = false, want true; " +
			"the field must be reachable through IsGenericVocabularyKey(\"infrastructure\") " +
			"so the FieldClassifier returns ClassMosaic for it")
	}
}

// TestIsKnownMosaicKey_TriggersField_True verifies that "triggers" is a known MOSAIC key.
func TestIsKnownMosaicKey_TriggersField_True(t *testing.T) {
	if !agentfields.IsKnownMosaicKey("triggers") {
		t.Error("IsKnownMosaicKey(\"triggers\") = false, want true; " +
			"the field must be reachable through IsGenericVocabularyKey(\"triggers\")")
	}
}

// TestIsKnownMosaicKey_OnFailureField_True verifies that "on_failure" is a known MOSAIC key.
func TestIsKnownMosaicKey_OnFailureField_True(t *testing.T) {
	if !agentfields.IsKnownMosaicKey("on_failure") {
		t.Error("IsKnownMosaicKey(\"on_failure\") = false, want true; " +
			"the field must be reachable through IsGenericVocabularyKey(\"on_failure\")")
	}
}

// ---------------------------------------------------------------------------
// Registry boundary — infrastructure fields must not appear in the
// MOSAIC-only deployed-field registry
// ---------------------------------------------------------------------------

// TestAll_DoesNotContainInfrastructureFields verifies that the three infrastructure-agent
// fields are NOT entries in the deployed-field registry returned by All(). They are generic
// source vocabulary (like "role" or "recommended_tier"), not MOSAIC-only bookkeeping stamps.
// Adding them to the registry would be incorrect and would cause the mosaic_-prefix
// convention check to fail.
func TestAll_DoesNotContainInfrastructureFields(t *testing.T) {
	forbidden := []string{"infrastructure", "triggers", "on_failure"}
	for _, f := range agentfields.All() {
		for _, bad := range forbidden {
			if f.Generic == bad || f.Deployed == bad || f.Legacy == bad {
				t.Errorf("infrastructure-agent field %q must not appear in the deployed-field "+
					"registry (All()); it is generic source vocabulary, not a MOSAIC-only stamp "+
					"— only genericVocabularyKeys should be extended (I5.1)",
					bad)
			}
		}
	}
}
