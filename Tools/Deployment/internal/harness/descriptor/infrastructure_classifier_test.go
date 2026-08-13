package descriptor_test

// infrastructure_classifier_test.go verifies that the three infrastructure-agent
// frontmatter fields — infrastructure, triggers, and on_failure — classify as
// ClassMosaic rather than ClassUnknown when Classify is called for any harness
// descriptor.
//
// These tests pin the Stage 5 fix at the FieldClassifier surface. The root cause
// of the skipped-mismatch failure is that agentfields.genericVocabularyKeys does not
// include the three fields; Classify therefore returns ClassUnknown, which sets
// hasNegative=true in DetectHarnessMatch, which produces HarnessMatchNo, which
// transform_service_impl.go maps to StatusSkippedMismatch.
//
// These tests are the TDD RED phase for I5.1. They fail until the three fields
// are added to agentfields.genericVocabularyKeys, which is the single correct fix
// location per the AC5.5 constraint (no special-casing outside internal/agentfields).
//
// Verified behaviours:
//
//   - "infrastructure" classifies as ClassMosaic, not ClassUnknown.
//   - "triggers" classifies as ClassMosaic, not ClassUnknown.
//   - "on_failure" classifies as ClassMosaic, not ClassUnknown.
//   - All three fields classify as ClassMosaic across different harness descriptors,
//     confirming the fix is at the vocabulary level (not per-harness whitelisting).
//   - ClassMosaic beats ClassHarness for these fields even if a pathological harness
//     declares one of them as its own key (consistent with the existing precedence rule).

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// ---------------------------------------------------------------------------
// Individual field assertions
// ---------------------------------------------------------------------------

// TestClassify_InfrastructureField_IsMosaic verifies that the "infrastructure" key
// classifies as ClassMosaic regardless of the harness descriptor in use. Before the
// fix, this field is ClassUnknown — which triggers hasNegative=true in DetectHarnessMatch
// and causes infrastructure agents to be reported as skipped-mismatch.
func TestClassify_InfrastructureField_IsMosaic(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML)

	got := clf.Classify("infrastructure")
	if got != descriptor.ClassMosaic {
		t.Errorf("Classify(%q) = %q, want %q; "+
			"\"infrastructure\" is an infrastructure-agent vocabulary field and must classify "+
			"as ClassMosaic so infrastructure agents are not reported as skipped-mismatch; "+
			"fix: add \"infrastructure\" to agentfields.genericVocabularyKeys (I5.1)",
			"infrastructure", got, descriptor.ClassMosaic)
	}
}

// TestClassify_TriggersField_IsMosaic verifies that the "triggers" key classifies
// as ClassMosaic regardless of the harness descriptor in use.
func TestClassify_TriggersField_IsMosaic(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML)

	got := clf.Classify("triggers")
	if got != descriptor.ClassMosaic {
		t.Errorf("Classify(%q) = %q, want %q; "+
			"\"triggers\" is an infrastructure-agent vocabulary field and must classify "+
			"as ClassMosaic; fix: add \"triggers\" to agentfields.genericVocabularyKeys (I5.1)",
			"triggers", got, descriptor.ClassMosaic)
	}
}

// TestClassify_OnFailureField_IsMosaic verifies that the "on_failure" key classifies
// as ClassMosaic regardless of the harness descriptor in use.
func TestClassify_OnFailureField_IsMosaic(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML)

	got := clf.Classify("on_failure")
	if got != descriptor.ClassMosaic {
		t.Errorf("Classify(%q) = %q, want %q; "+
			"\"on_failure\" is an infrastructure-agent vocabulary field and must classify "+
			"as ClassMosaic; fix: add \"on_failure\" to agentfields.genericVocabularyKeys (I5.1)",
			"on_failure", got, descriptor.ClassMosaic)
	}
}

// ---------------------------------------------------------------------------
// Table-driven assertion across all three fields
// ---------------------------------------------------------------------------

// TestClassify_InfrastructureFields_AllAreMosaic documents the complete Stage 5
// vocabulary extension: all three infrastructure-agent fields must classify as
// ClassMosaic when evaluated against any standard harness classifier.
func TestClassify_InfrastructureFields_AllAreMosaic(t *testing.T) {
	clf := buildClassifier(t, noDestFieldClassifierDescriptorYAML)

	infraFields := []string{"infrastructure", "triggers", "on_failure"}
	for _, key := range infraFields {
		got := clf.Classify(key)
		if got != descriptor.ClassMosaic {
			t.Errorf("Classify(%q) = %q, want %q; "+
				"all three infrastructure-agent fields must classify as ClassMosaic "+
				"to remove the hasNegative signal that causes skipped-mismatch verdicts",
				key, got, descriptor.ClassMosaic)
		}
	}
}

// ---------------------------------------------------------------------------
// Cross-descriptor assertion: vocabulary-level, not per-harness
// ---------------------------------------------------------------------------

// TestClassify_InfrastructureFields_NotUnknownForAnyHarness verifies that the three
// fields are never ClassUnknown regardless of which harness descriptor is tested. This
// is the AC5.5 guard: the fix must live in agentfields.genericVocabularyKeys — a
// vocabulary-level change — never in per-harness whitelisting in classifier.go,
// retarget.go, or descriptor descriptors.
func TestClassify_InfrastructureFields_NotUnknownForAnyHarness(t *testing.T) {
	descriptors := []struct {
		name string
		yaml string
	}{
		{"no-destfield-harness", noDestFieldClassifierDescriptorYAML},
		{"diverted-field-harness", divertedClassifierDescriptorYAML},
	}

	infraFields := []string{"infrastructure", "triggers", "on_failure"}

	for _, d := range descriptors {
		clf := buildClassifier(t, d.yaml)
		for _, key := range infraFields {
			got := clf.Classify(key)
			if got == descriptor.ClassUnknown {
				t.Errorf("descriptor=%q: Classify(%q) = ClassUnknown; "+
					"infrastructure-agent fields must classify as ClassMosaic across all harness "+
					"descriptors — ClassUnknown sets hasNegative=true in DetectHarnessMatch, "+
					"which produces a mismatch verdict; the fix must be in "+
					"agentfields.genericVocabularyKeys, not per-harness (AC5.5)",
					d.name, key)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Precedence: ClassMosaic beats ClassHarness for infrastructure fields
// ---------------------------------------------------------------------------

// TestClassify_InfrastructureField_MosaicBeatsHarness verifies that if a pathological
// harness declares "infrastructure" as its own key (e.g. as ModelKey), ClassMosaic is
// still returned — consistent with the existing precedence rule for all vocabulary keys.
// This guard prevents a harness from accidentally capturing an infrastructure field.
func TestClassify_InfrastructureField_MosaicBeatsHarness(t *testing.T) {
	d := loadDescriptor(t, `schema_version: "1"
id: "pathological-infra-harness"
display_name: "Pathological Infrastructure Harness"
tools:
  shape: list
  universe: []
  mappings: []
frontmatter:
  model_key: "infrastructure"
  tools_key: "harness_tools"
`)
	clf := descriptor.NewFieldClassifier(d, domain.FrontmatterPlan{})

	got := clf.Classify("infrastructure")
	if got != descriptor.ClassMosaic {
		t.Errorf("Classify(%q) = %q, want %q; "+
			"ClassMosaic must beat ClassHarness even when a harness declares an "+
			"infrastructure-agent vocabulary key as its own model key — "+
			"consistent with the precedence rule for all MOSAIC vocabulary keys",
			"infrastructure", got, descriptor.ClassMosaic)
	}
}
