package app

// promote_prefix_test.go verifies that buildGenericAgent correctly handles the mosaic_
// field prefix when producing a generic agent source file from a deployed harness-only agent.
//
// Two behaviours are required (both FAIL until the implementation is updated):
//
//  1. Dropped: every MOSAIC-only deployed field under either its prefixed (mosaic_-prefixed)
//     or its legacy (unprefixed) name must be absent from the generated generic output.
//     Currently promoteDroppedFrontmatterKeys only lists the legacy names, so a source
//     carrying prefixed names (e.g. "mosaic_transform_version") survives into the output.
//
//  2. ID restoration: when the deployed source carries the id under its prefixed name
//     "mosaic_id", promote must restore it as the generic "id" key. Currently the
//     eligibility check only recognises the legacy "transform_version" signal, so a file
//     with "mosaic_transform_version" cannot even be promoted.
//
// The backward-compatibility direction — legacy keys are dropped — is already tested in
// promote_test.go and is expected to pass both before and after the implementation.

import (
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// minimalEligiblePrefixedSource returns the smallest source bytes for a file that carries
// the MOSAIC-only stamps under their prefixed names. This is what a fully-migrated deployed
// file looks like.
func minimalEligiblePrefixedSource() []byte {
	return []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_injections_version: \"1.2.0\"\n" +
		"mosaic_tool_mappings_version: \"hash99\"\n" +
		"version: \"1.5.0\"\n" +
		"mosaic_id: \"77\"\n" +
		"name: prefix-test-agent\n" +
		"description: An agent carrying prefixed MOSAIC stamps\n" +
		"role: subagent\n" +
		"recommended_tier: MEDIUM\n" +
		"tier_rationale: standard capability requirement\n" +
		"tools: [file_read, file_write]\n" +
		"required_skills: []\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are the PrefixTestAgent.\n" +
		"[[/SECTION:Identity]]\n")
}

// minimalEligibleMixedSource returns source bytes for a file that carries both the prefixed
// and legacy forms of MOSAIC stamps (a transitional state). Promote must handle this without
// either name surviving into the generic output.
func minimalEligibleMixedSource() []byte {
	return []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"transform_version: \"3.0.0\"\n" +
		"mosaic_injections_version: \"1.2.0\"\n" +
		"injections_version: \"1.2.0\"\n" +
		"version: \"1.5.0\"\n" +
		"mosaic_id: \"88\"\n" +
		"name: mixed-test-agent\n" +
		"description: An agent with both prefixed and legacy stamps\n" +
		"role: subagent\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are the MixedTestAgent.\n" +
		"[[/SECTION:Identity]]\n")
}

// promoteWithPrefixedSource calls buildGenericAgent on the given source and returns the
// generated output bytes. Skips the test if the eligibility check reports the source
// ineligible (rather than failing) so that eligibility-related failures are diagnosed
// by the dedicated eligibility tests rather than this file.
func promoteWithPrefixedSource(t *testing.T, src []byte, numericID string) []byte {
	t.Helper()
	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: numericID,
		Key:       "prefix-test-agent",
	})
	if err != nil {
		// Eligibility failures produce ErrPromoteNotTransformed. Check whether the source
		// itself is ineligible (separate concern from prefix handling) vs. a genuine failure.
		t.Fatalf("buildGenericAgent failed: %v", err)
	}
	return out
}

// parseFrontmatterKeys is a helper that parses src and returns the frontmatter key list in
// document order, for use in ordered assertions.
func parseFrontmatterKeys(t *testing.T, src []byte) []string {
	t.Helper()
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("parseFrontmatterKeys: docformat.Parse failed: %v", err)
	}
	return doc.Frontmatter().Keys()
}

// ---------------------------------------------------------------------------
// Eligibility: prefixed transform_version must satisfy signal one
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_PrefixedTransformVersion_EligibilityAccepted verifies that a
// deployed source carrying "mosaic_transform_version" (not "transform_version") is accepted
// by the eligibility check inside buildGenericAgent and produces output. FAILS until the
// eligibility check inside buildGenericAgent reads the prefixed name via ReadOrder.
func TestBuildGenericAgent_PrefixedTransformVersion_EligibilityAccepted(t *testing.T) {
	src := minimalEligiblePrefixedSource()
	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "77",
		Key:       "prefix-test-agent",
	})
	if err != nil {
		t.Fatalf("buildGenericAgent rejected a source with mosaic_transform_version: %v; "+
			"eligibility check must recognise the prefixed name as satisfying signal one", err)
	}
	if len(out) == 0 {
		t.Error("buildGenericAgent produced empty output for a valid prefixed source")
	}
}

// ---------------------------------------------------------------------------
// Dropped: prefixed MOSAIC stamps must not appear in generic output
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_PrefixedMosaicTransformVersion_DroppedFromOutput verifies that
// "mosaic_transform_version" does not appear in the generated generic agent output.
// FAILS until promoteDroppedFrontmatterKeys (or its replacement) covers the prefixed name.
func TestBuildGenericAgent_PrefixedMosaicTransformVersion_DroppedFromOutput(t *testing.T) {
	out := promoteWithPrefixedSource(t, minimalEligiblePrefixedSource(), "77")

	keys := parseFrontmatterKeys(t, out)
	for _, k := range keys {
		if k == "mosaic_transform_version" {
			t.Error("generic output carries \"mosaic_transform_version\"; MOSAIC-only deployed stamps must be dropped during promote")
		}
		if k == "transform_version" {
			t.Error("generic output carries \"transform_version\" (legacy form); all forms of MOSAIC-only deployed stamps must be dropped")
		}
	}
}

// TestBuildGenericAgent_PrefixedMosaicInjectionsVersion_DroppedFromOutput verifies that
// "mosaic_injections_version" does not appear in the generated generic output.
func TestBuildGenericAgent_PrefixedMosaicInjectionsVersion_DroppedFromOutput(t *testing.T) {
	out := promoteWithPrefixedSource(t, minimalEligiblePrefixedSource(), "77")

	keys := parseFrontmatterKeys(t, out)
	for _, k := range keys {
		if k == "mosaic_injections_version" {
			t.Error("generic output carries \"mosaic_injections_version\"; must be dropped during promote")
		}
		if k == "injections_version" {
			t.Error("generic output carries \"injections_version\" (legacy form); must be dropped during promote")
		}
	}
}

// TestBuildGenericAgent_PrefixedMosaicToolMappingsVersion_DroppedFromOutput verifies that
// "mosaic_tool_mappings_version" does not appear in the generated generic output.
func TestBuildGenericAgent_PrefixedMosaicToolMappingsVersion_DroppedFromOutput(t *testing.T) {
	out := promoteWithPrefixedSource(t, minimalEligiblePrefixedSource(), "77")

	keys := parseFrontmatterKeys(t, out)
	for _, k := range keys {
		if k == "mosaic_tool_mappings_version" {
			t.Error("generic output carries \"mosaic_tool_mappings_version\"; must be dropped during promote")
		}
		if k == "tool_mappings_version" {
			t.Error("generic output carries \"tool_mappings_version\" (legacy form); must be dropped during promote")
		}
	}
}

// TestBuildGenericAgent_AllPrefixedMosaicStamps_NoneInOutput verifies in one assertion that
// none of the prefixed or legacy MOSAIC-only stamp keys survive into the generic output.
func TestBuildGenericAgent_AllPrefixedMosaicStamps_NoneInOutput(t *testing.T) {
	out := promoteWithPrefixedSource(t, minimalEligiblePrefixedSource(), "77")
	keys := parseFrontmatterKeys(t, out)

	prohibited := []string{
		"mosaic_transform_version", "transform_version",
		"mosaic_injections_version", "injections_version",
		"mosaic_tool_mappings_version", "tool_mappings_version",
		"mosaic_bundle_version", "bundle_version",
		"mosaic_orchestrator_injections_version", "orchestrator_injections_version",
		"mosaic_protocol_version", "protocol_version",
	}
	for _, k := range keys {
		for _, bad := range prohibited {
			if k == bad {
				t.Errorf("generic output carries %q; all MOSAIC-only deployed stamp keys (both prefixed and legacy) must be dropped during promote", k)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// ID restoration: mosaic_id must become the generic "id"
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_PrefixedMosaicID_RestoredAsGenericID verifies that when the
// deployed source carries "mosaic_id" (the prefixed name), the generated generic output
// carries "id" (the generic name) with the same value, and "mosaic_id" is absent.
// FAILS until the promote path reads mosaic_id via ReadOrder and writes it as "id".
func TestBuildGenericAgent_PrefixedMosaicID_RestoredAsGenericID(t *testing.T) {
	out := promoteWithPrefixedSource(t, minimalEligiblePrefixedSource(), "77")

	// "id" must be present with the supplied NumericID value.
	if v, ok := parseFrontmatterField(t, out, "id"); !ok {
		t.Error("generic output does not carry \"id\"; promote must restore the generic id from the deployed mosaic_id")
	} else if v != "77" {
		t.Errorf("generic id = %q, want %q; the value from mosaic_id must be preserved verbatim", v, "77")
	}

	// "mosaic_id" must not survive in the generic output.
	if _, ok := parseFrontmatterField(t, out, "mosaic_id"); ok {
		t.Error("generic output carries \"mosaic_id\"; the prefixed deployed name must not appear in a generic source file")
	}
}

// TestBuildGenericAgent_MixedIDSource_GenericIDCarriesCorrectValue verifies the same
// id restoration when both "mosaic_id" and the legacy "id" are present in the source.
// Promote must use the prefixed value (or the explicit NumericID from PromoteInput) and
// must not produce two id-like keys.
func TestBuildGenericAgent_MixedIDSource_GenericIDCarriesCorrectValue(t *testing.T) {
	src := []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_id: \"88\"\n" +
		"id: \"88\"\n" + // legacy form also present
		"name: mixed-id-agent\n" +
		"description: Agent with both id forms\n" +
		"role: subagent\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"Mixed id source agent.\n" +
		"[[/SECTION:Identity]]\n")

	out := promoteWithPrefixedSource(t, src, "88")

	if v, ok := parseFrontmatterField(t, out, "id"); !ok {
		t.Error("generic output does not carry \"id\"; promote must produce the generic id")
	} else if v != "88" {
		t.Errorf("generic id = %q, want %q", v, "88")
	}
	if _, ok := parseFrontmatterField(t, out, "mosaic_id"); ok {
		t.Error("generic output carries \"mosaic_id\"; the prefixed key must not appear in the generic output")
	}
}

// ---------------------------------------------------------------------------
// Mixed source: both prefixed and legacy stamps — all must be dropped
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_MixedPrefixedAndLegacyStamps_AllDropped verifies that when a
// deployed source carries both the prefixed and legacy forms of MOSAIC stamps (a transitional
// case), neither form survives into the generic output.
func TestBuildGenericAgent_MixedPrefixedAndLegacyStamps_AllDropped(t *testing.T) {
	out := promoteWithPrefixedSource(t, minimalEligibleMixedSource(), "88")
	keys := parseFrontmatterKeys(t, out)

	prohibited := []string{
		"mosaic_transform_version", "transform_version",
		"mosaic_injections_version", "injections_version",
		"mosaic_id",
	}
	for _, k := range keys {
		for _, bad := range prohibited {
			if k == bad {
				t.Errorf("generic output carries %q from a mixed-stamp source; "+
					"all MOSAIC-only stamp keys (both forms) must be dropped during promote", k)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Backward compatibility: legacy stamps are still dropped (existing behaviour)
// ---------------------------------------------------------------------------

// TestBuildGenericAgent_LegacyTransformVersion_StillDropped verifies that a source with
// the legacy "transform_version" key still has it dropped, just as before the mosaic_
// migration. This test PASSES both before and after the implementation.
func TestBuildGenericAgent_LegacyTransformVersion_StillDropped(t *testing.T) {
	src := minimalEligiblePromoteSource() // uses legacy transform_version
	out, err := buildGenericAgent(PromoteInput{Source: src, NumericID: "42", Key: "test-agent"})
	if err != nil {
		t.Fatalf("buildGenericAgent: %v", err)
	}

	keys := parseFrontmatterKeys(t, out)
	for _, k := range keys {
		if k == "transform_version" {
			t.Error("legacy transform_version must still be dropped during promote (backward compatibility)")
		}
	}
}
