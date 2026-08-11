package transform_test

// mosaic_prefix_test.go verifies that the transform/deploy write path stamps every
// MOSAIC-only bookkeeping field under its prefixed (mosaic_-prefixed) name in the
// deployed output, and that no legacy unprefixed form of those fields appears.
//
// Additionally, this file verifies that the deployed frontmatter key order declared
// by the harness descriptor is respected once the prefixed names are in use — that is,
// the descriptor's key_order entries name the prefixed fields and the output honours them.
//
// Behaviours verified (will FAIL until the implementation uses prefixed names):
//
//   Write path — prefixed stamps replace legacy names:
//     - The output frontmatter carries "mosaic_transform_version", not "transform_version".
//     - The output frontmatter carries "mosaic_injections_version", not "injections_version".
//     - The output frontmatter carries "mosaic_tool_mappings_version", not "tool_mappings_version".
//     - The generic source's "id" field is renamed to "mosaic_id" in the deployed output.
//     - None of the legacy unprefixed stamp names appear in the deployed output.
//
//   Untouched fields:
//     - "model", "tools", "name" (absent per descriptor drop), "description", and
//       "version" are not renamed — their names are unchanged or absent per descriptor rules.
//
//   Key ordering:
//     - The deployed frontmatter key order matches the descriptor's declared order, which
//       uses the prefixed field names (e.g. "mosaic_transform_version" at its declared
//       position, not "transform_version").

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Shared fixture helpers
// ---------------------------------------------------------------------------

// mosaicPrefixSource is a minimal generic agent source used by the prefixed-stamp tests.
// It has all frontmatter fields the fixture descriptor interacts with, plus a minimal body
// with one canonical section so the transform pipeline accepts it.
const mosaicPrefixSource = `---
id: 99
version: 2.0.0
name: prefix-test-agent
description: Agent for mosaic-prefix stamp tests
model: {model-identifier}
tools: [file_read, file_write]
recommended_tier: MEDIUM
tier_rationale: medium-level task
required_skills: []
---

[[SECTION:Identity]]
Minimal identity body for prefix stamp tests.
[[/SECTION:Identity]]
`

// applyMosaicPrefixSource calls transform.Apply on mosaicPrefixSource with the fixture
// module and an optional ToolMappingsVersion. Returns the parsed output document.
func applyMosaicPrefixSource(t *testing.T, toolMappingsVersion string) *docformat.Document {
	t.Helper()
	req := transform.Request{
		Source:              []byte(mosaicPrefixSource),
		Kind:                domain.ArtifactAgent,
		Key:                 "prefix-test-agent",
		Module:              newFixtureModule(t),
		Model:               fixtureModel(),
		Scope:               domain.ScopeProject,
		ToolMappingsVersion: toolMappingsVersion,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("docformat.Parse output: %v", err)
	}
	return doc
}

// ---------------------------------------------------------------------------
// Write path — prefixed stamp names
// ---------------------------------------------------------------------------

// TestTransform_WritesMosaicTransformVersion_NotLegacy verifies that the deployed output
// carries "mosaic_transform_version" and that the legacy "transform_version" key is absent.
// This test FAILS until the transform write path uses prefixed field names from the registry.
func TestTransform_WritesMosaicTransformVersion_NotLegacy(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "")
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_transform_version"); !ok {
		t.Error("output frontmatter does not carry \"mosaic_transform_version\"; transform must stamp the prefixed name")
	}
	if _, ok := fm.Get("transform_version"); ok {
		t.Error("output frontmatter carries legacy \"transform_version\"; the deploy path must not write the unprefixed name")
	}
}

// TestTransform_WritesMosaicInjectionsVersion_NotLegacy verifies that the deployed output
// carries "mosaic_injections_version" and that the legacy "injections_version" key is absent.
func TestTransform_WritesMosaicInjectionsVersion_NotLegacy(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "")
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_injections_version"); !ok {
		t.Error("output frontmatter does not carry \"mosaic_injections_version\"; transform must stamp the prefixed name")
	}
	if _, ok := fm.Get("injections_version"); ok {
		t.Error("output frontmatter carries legacy \"injections_version\"; the deploy path must not write the unprefixed name")
	}
}

// TestTransform_WritesMosaicToolMappingsVersion_NotLegacy verifies that when a
// ToolMappingsVersion is supplied in the request, the deployed output carries
// "mosaic_tool_mappings_version" and the legacy "tool_mappings_version" is absent.
func TestTransform_WritesMosaicToolMappingsVersion_NotLegacy(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "abc123")
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_tool_mappings_version"); !ok {
		t.Error("output frontmatter does not carry \"mosaic_tool_mappings_version\"; transform must stamp the prefixed name")
	}
	if _, ok := fm.Get("tool_mappings_version"); ok {
		t.Error("output frontmatter carries legacy \"tool_mappings_version\"; the deploy path must not write the unprefixed name")
	}
}

// TestTransform_WritesMosaicID_NotLegacyID verifies that the generic source's "id" field is
// renamed to "mosaic_id" in the deployed output, and that the generic "id" key is absent.
// The prefixing of the id field requires a rename step in the deploy path because id is
// carried through from the generic source rather than written by applyVersionStamp.
func TestTransform_WritesMosaicID_NotLegacyID(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "")
	fm := doc.Frontmatter()

	if v, ok := fm.Get("mosaic_id"); !ok {
		t.Error("output frontmatter does not carry \"mosaic_id\"; the generic source's id must be renamed to mosaic_id")
	} else if v.Kind != domain.KindScalar || v.Scalar != "99" {
		t.Errorf("mosaic_id = %q (kind=%v), want scalar \"99\"; id value must be preserved verbatim under the prefixed name",
			v.Scalar, v.Kind)
	}
	if _, ok := fm.Get("id"); ok {
		t.Error("output frontmatter carries generic \"id\"; the deploy path must not carry the unprefixed id field into a deployed file")
	}
}

// TestTransform_NoLegacyStampKeyInOutput verifies that none of the legacy unprefixed stamp
// keys appear anywhere in the deployed output's frontmatter. A deployed file must carry only
// the prefixed forms.
func TestTransform_NoLegacyStampKeyInOutput(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "hash42")
	fm := doc.Frontmatter()

	legacyKeys := []string{
		"transform_version",
		"injections_version",
		"tool_mappings_version",
	}
	for _, key := range legacyKeys {
		if _, ok := fm.Get(key); ok {
			t.Errorf("output frontmatter carries legacy stamp key %q; deployed files must use only prefixed names", key)
		}
	}
}

// ---------------------------------------------------------------------------
// Untouched fields — names must not be changed
// ---------------------------------------------------------------------------

// TestTransform_ModelAndVersionAndDescriptionUnprefixed verifies that the harness model key
// ("model"), the "version" field, and the "description" field are written under their
// existing unprefixed names and are not renamed.
func TestTransform_ModelAndVersionAndDescriptionUnprefixed(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "")
	fm := doc.Frontmatter()

	if _, ok := fm.Get("model"); !ok {
		t.Error("output frontmatter does not carry \"model\"; the harness model key must never be prefixed")
	}
	if _, ok := fm.Get("version"); !ok {
		t.Error("output frontmatter does not carry \"version\"; the shared version field must never be prefixed")
	}
	if _, ok := fm.Get("description"); !ok {
		t.Error("output frontmatter does not carry \"description\"; description must be carried through unchanged")
	}
}

// TestTransform_MosaicTransformVersionValueMatchesDescriptor verifies that the value written
// under "mosaic_transform_version" equals the descriptor's declared transform_version ("3.0.0"
// in the fixture descriptor), confirming the stamp carries the right version, not an empty
// string or a stale value.
func TestTransform_MosaicTransformVersionValueMatchesDescriptor(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "")
	fm := doc.Frontmatter()

	v, ok := fm.Get("mosaic_transform_version")
	if !ok {
		t.Fatal("mosaic_transform_version absent from output; cannot verify value")
	}
	if v.Kind != domain.KindScalar {
		t.Fatalf("mosaic_transform_version kind = %v, want KindScalar", v.Kind)
	}
	// The fixture descriptor declares transform_version: "3.0.0".
	if v.Scalar != "3.0.0" {
		t.Errorf("mosaic_transform_version = %q, want %q (from fixture descriptor)", v.Scalar, "3.0.0")
	}
}

// TestTransform_MosaicInjectionsVersionValueMatchesDescriptor verifies that the value
// written under "mosaic_injections_version" equals the fixture descriptor's injections_version.
func TestTransform_MosaicInjectionsVersionValueMatchesDescriptor(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "")
	fm := doc.Frontmatter()

	v, ok := fm.Get("mosaic_injections_version")
	if !ok {
		t.Fatal("mosaic_injections_version absent from output; cannot verify value")
	}
	if v.Kind != domain.KindScalar {
		t.Fatalf("mosaic_injections_version kind = %v, want KindScalar", v.Kind)
	}
	// The fixture descriptor declares injections_version: "1.2.0".
	if v.Scalar != "1.2.0" {
		t.Errorf("mosaic_injections_version = %q, want %q (from fixture descriptor)", v.Scalar, "1.2.0")
	}
}

// TestTransform_ToolMappingsVersionAbsentWhenNotSupplied verifies that when the request
// carries an empty ToolMappingsVersion, no tool-mappings stamp (neither legacy nor prefixed)
// appears in the output. An empty version means no config mappings are active.
func TestTransform_ToolMappingsVersionAbsentWhenNotSupplied(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "") // empty ToolMappingsVersion
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_tool_mappings_version"); ok {
		t.Error("mosaic_tool_mappings_version present in output but ToolMappingsVersion was empty; stamp must be omitted")
	}
	if _, ok := fm.Get("tool_mappings_version"); ok {
		t.Error("tool_mappings_version present in output but ToolMappingsVersion was empty; stamp must be omitted")
	}
}

// ---------------------------------------------------------------------------
// Key ordering — prefixed names at the declared positions
// ---------------------------------------------------------------------------

// TestTransform_KeyOrderContainsMosaicTransformVersionNotLegacy verifies that in the
// deployed output's frontmatter key list, "mosaic_transform_version" appears and
// "transform_version" does not. This confirms that the descriptor's key_order has been
// updated to the prefixed name and the stamp is written under the same prefixed name,
// so reordering places it at the declared position.
func TestTransform_KeyOrderContainsMosaicTransformVersionNotLegacy(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "")
	keys := doc.Frontmatter().Keys()

	var foundPrefixed, foundLegacy bool
	for _, k := range keys {
		if k == "mosaic_transform_version" {
			foundPrefixed = true
		}
		if k == "transform_version" {
			foundLegacy = true
		}
	}

	if !foundPrefixed {
		t.Error("\"mosaic_transform_version\" not found in output frontmatter keys; descriptor key_order must name the prefixed field")
	}
	if foundLegacy {
		t.Error("\"transform_version\" found in output frontmatter keys; legacy stamp must not appear after the deploy path uses prefixed names")
	}
}

// TestTransform_KeyOrderContainsMosaicInjectionsVersionNotLegacy verifies that
// "mosaic_injections_version" appears in the output key order and "injections_version" does not.
func TestTransform_KeyOrderContainsMosaicInjectionsVersionNotLegacy(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "")
	keys := doc.Frontmatter().Keys()

	var foundPrefixed, foundLegacy bool
	for _, k := range keys {
		if k == "mosaic_injections_version" {
			foundPrefixed = true
		}
		if k == "injections_version" {
			foundLegacy = true
		}
	}

	if !foundPrefixed {
		t.Error("\"mosaic_injections_version\" not found in output frontmatter keys; descriptor key_order must name the prefixed field")
	}
	if foundLegacy {
		t.Error("\"injections_version\" found in output frontmatter keys; legacy stamp must not appear")
	}
}

// TestTransform_KeyOrderContainsMosaicIDNotLegacyID verifies that "mosaic_id" appears in the
// output key list at the position the descriptor's key_order declares for the id field, and
// that the unprefixed "id" is absent.
func TestTransform_KeyOrderContainsMosaicIDNotLegacyID(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "")
	keys := doc.Frontmatter().Keys()

	var foundPrefixed, foundLegacy bool
	for _, k := range keys {
		if k == "mosaic_id" {
			foundPrefixed = true
		}
		if k == "id" {
			foundLegacy = true
		}
	}

	if !foundPrefixed {
		t.Error("\"mosaic_id\" not found in output frontmatter keys; generic id must be renamed to mosaic_id in deployed output")
	}
	if foundLegacy {
		t.Error("\"id\" found in output frontmatter keys; the generic id key must not appear in the deployed output")
	}
}

// TestTransform_MosaicTransformVersionAppearsBeforeMosaicInjectionsVersion verifies that
// the relative order of the two version stamps matches the descriptor's declared key_order —
// mosaic_transform_version before mosaic_injections_version — once both are present under
// their prefixed names.
func TestTransform_MosaicTransformVersionAppearsBeforeMosaicInjectionsVersion(t *testing.T) {
	doc := applyMosaicPrefixSource(t, "")
	keys := doc.Frontmatter().Keys()

	var tvIdx, ivIdx = -1, -1
	for i, k := range keys {
		switch k {
		case "mosaic_transform_version":
			tvIdx = i
		case "mosaic_injections_version":
			ivIdx = i
		}
	}
	if tvIdx < 0 {
		t.Fatal("mosaic_transform_version not found in output; cannot assert ordering")
	}
	if ivIdx < 0 {
		t.Fatal("mosaic_injections_version not found in output; cannot assert ordering")
	}
	if tvIdx >= ivIdx {
		t.Errorf("mosaic_transform_version (index %d) does not appear before mosaic_injections_version (index %d); "+
			"descriptor key_order must place transform before injections", tvIdx, ivIdx)
	}
}

// ---------------------------------------------------------------------------
// Write path — mosaic_bundle_version (bundle-eligible role)
// ---------------------------------------------------------------------------

// applyMosaicPrefixSourceWithBundle calls transform.Apply on sourceWithAllBundleRegions with the
// fixture module, a subagent role, a protocol, and a bundle carrying the given version. It
// returns the parsed output document. The subagent role makes the agent bundle-eligible so that
// the bundle_version stamp path is exercised.
//
// sourceWithAllBundleRegions, fixtureBundle, and fixtureProtocol are defined in
// bundle_region_test.go in this package.
func applyMosaicPrefixSourceWithBundle(t *testing.T, bundleVersion string) *docformat.Document {
	t.Helper()
	req := transform.Request{
		Source:   []byte(sourceWithAllBundleRegions),
		Kind:     domain.ArtifactAgent,
		Key:      "bundle-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   fixtureBundle(bundleVersion),
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("transform.Apply (bundle request): %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("docformat.Parse output (bundle request): %v", err)
	}
	return doc
}

// TestTransform_WritesMosaicBundleVersion_NotLegacy verifies that when a bundle-eligible
// subagent is transformed with a non-empty bundle, the deployed output carries
// "mosaic_bundle_version" and the legacy "bundle_version" key is absent. This is the
// write-path RED test for the sixth FR-6 field that is written by the explicit bundle
// stamp in the deploy path (frontmatter.go's bundle_version set).
//
// This test FAILS until I4.2 updates the bundle stamp write to use the prefixed field name.
func TestTransform_WritesMosaicBundleVersion_NotLegacy(t *testing.T) {
	doc := applyMosaicPrefixSourceWithBundle(t, "1.0.0")
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_bundle_version"); !ok {
		t.Error("output frontmatter does not carry \"mosaic_bundle_version\"; " +
			"the bundle stamp write path must use the prefixed field name")
	}
	if _, ok := fm.Get("bundle_version"); ok {
		t.Error("output frontmatter carries legacy \"bundle_version\"; " +
			"the deploy path must not write the unprefixed bundle version name")
	}
}

// TestTransform_MosaicBundleVersionValueMatchesBundle verifies that the value written under
// "mosaic_bundle_version" equals BundleContent.Version verbatim, confirming that the stamp
// carries the correct version and not a stale or empty value.
//
// This test FAILS until I4.2 updates the bundle stamp write to use the prefixed field name.
func TestTransform_MosaicBundleVersionValueMatchesBundle(t *testing.T) {
	const bundleVer = "2.5.0"
	doc := applyMosaicPrefixSourceWithBundle(t, bundleVer)
	fm := doc.Frontmatter()

	v, ok := fm.Get("mosaic_bundle_version")
	if !ok {
		t.Fatal("mosaic_bundle_version absent from output; cannot verify value")
	}
	if v.Kind != domain.KindScalar {
		t.Fatalf("mosaic_bundle_version kind = %v, want KindScalar", v.Kind)
	}
	if v.Scalar != bundleVer {
		t.Errorf("mosaic_bundle_version = %q, want %q (BundleContent.Version verbatim)",
			v.Scalar, bundleVer)
	}
}
