package transform_test

// frontmatter_test.go tests descriptor-driven frontmatter rewriting (T8.3) and version
// stamping (T8.4). All variance is asserted on the parsed output document; the tests do
// not inspect internal state or implementation steps.
//
// The fixture harness descriptor (testdata/transform/fixture.yaml) declares:
//   - drop: [name, recommended_tier, tier_rationale]
//   - add:  [{key: mode, value: subagent}]
//   - model_key: model
//   - tools_key: tools
//   - key_order: [id, version, transform_version, injections_version, description, mode, model, tools]
//   - transform_version: 3.0.0
//   - injections_version: 1.2.0

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// frontmatterSource is a minimal generic agent source that exercises every frontmatter
// shaping rule in the fixture descriptor. The body is intentionally minimal; body content
// is covered by body_preservation_test.go.
const frontmatterSource = `---
id: 42
version: 2.5.0
name: fm-test-agent
description: Agent for frontmatter tests
model: {model-identifier}
tools: [file_read, file_write]
recommended_tier: MEDIUM
tier_rationale: medium-level task
required_skills: []
---

[[SECTION:Identity]]
Minimal body.
[[/SECTION:Identity]]
`

// applyFrontmatterSource calls transform.Apply on frontmatterSource with the fixture module
// and returns the parsed output document. It fails the test immediately on any error.
func applyFrontmatterSource(t *testing.T) *docformat.Document {
	t.Helper()
	req := transform.Request{
		Source: []byte(frontmatterSource),
		Kind:   domain.ArtifactAgent,
		Key:    "fm-test-agent",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err) // RED: fails here with ErrNotImplemented
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}
	return doc
}

// --- T8.3: Descriptor-driven frontmatter rewriting ---

// TestFrontmatter_DropFields_AbsentInOutput asserts that fields listed in the descriptor's
// Drop list are absent from the transformed output. The fixture descriptor drops: name,
// recommended_tier, tier_rationale.
func TestFrontmatter_DropFields_AbsentInOutput(t *testing.T) {
	doc := applyFrontmatterSource(t)
	fm := doc.Frontmatter()

	droppedKeys := []string{"name", "recommended_tier", "tier_rationale"}
	for _, key := range droppedKeys {
		if _, ok := fm.Get(key); ok {
			t.Errorf("field %q should have been dropped but is still present in the output frontmatter", key)
		}
	}
}

// TestFrontmatter_AddFields_PresentInOutput asserts that fields from the descriptor's Add
// list are present in the output. The fixture descriptor adds: mode: subagent.
func TestFrontmatter_AddFields_PresentInOutput(t *testing.T) {
	doc := applyFrontmatterSource(t)
	fm := doc.Frontmatter()

	v, ok := fm.Get("mode")
	if !ok {
		t.Fatal("field \"mode\" declared in descriptor Add list is absent from output frontmatter")
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("field \"mode\" value kind: want %q, got %q", domain.KindScalar, v.Kind)
	}
	if v.Scalar != "subagent" {
		t.Errorf("field \"mode\" value: want %q, got %q", "subagent", v.Scalar)
	}
}

// TestFrontmatter_ModelField_SetFromModelSelection asserts that the model field in the
// output is set to the selected model ID. The fixture descriptor uses model_key = "model".
func TestFrontmatter_ModelField_SetFromModelSelection(t *testing.T) {
	doc := applyFrontmatterSource(t)
	fm := doc.Frontmatter()

	v, ok := fm.Get("model")
	if !ok {
		t.Fatal("model field absent from output frontmatter")
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("model field value kind: want %q, got %q", domain.KindScalar, v.Kind)
	}
	if v.Scalar != "claude/claude-sonnet" {
		t.Errorf("model field value: want %q, got %q", "claude/claude-sonnet", v.Scalar)
	}
}

// TestFrontmatter_ModelField_UsesModelKeyFromDescriptor asserts that the model is written
// to the key declared in the descriptor's model_key, not any hardcoded key. This ensures
// AC8.4: no harness-specific key is baked into the transform package.
func TestFrontmatter_ModelField_UsesModelKeyFromDescriptor(t *testing.T) {
	// Verify that the fixture descriptor's model_key is "model" (so the test below is
	// targeting the right key) and that the key in the output matches.
	mod := newFixtureModule(t)
	desc := mod.Descriptor()
	if desc.Frontmatter.ModelKey == "" {
		t.Skip("fixture descriptor has no model_key — model field test not applicable")
	}

	req := transform.Request{
		Source: []byte(frontmatterSource),
		Kind:   domain.ArtifactAgent,
		Key:    "fm-test-agent",
		Module: mod,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}
	fm := doc.Frontmatter()

	v, ok := fm.Get(desc.Frontmatter.ModelKey)
	if !ok {
		t.Errorf("model field %q (from descriptor.Frontmatter.ModelKey) absent from output", desc.Frontmatter.ModelKey)
	}
	if v.Scalar != fixtureModel().ModelID {
		t.Errorf("model field %q value: want %q, got %q",
			desc.Frontmatter.ModelKey, fixtureModel().ModelID, v.Scalar)
	}
}

// TestFrontmatter_ToolsField_ContainsMappedHarnessTools asserts that the tools field in
// the output contains the harness-side tool names derived from the agent's generic tool
// list via the descriptor's tool mappings. The exact list order must match the Universe
// declaration order.
func TestFrontmatter_ToolsField_ContainsMappedHarnessTools(t *testing.T) {
	// frontmatterSource declares: tools: [file_read, file_write]
	// fixture.yaml maps: file_read → read-file, file_write → write-file
	// Universe order: read-file, write-file, edit-file, search-file, search-text, run-terminal, ask-user
	// Expected tools list: [read-file, write-file]
	doc := applyFrontmatterSource(t)
	fm := doc.Frontmatter()

	v, ok := fm.Get("tools")
	if !ok {
		t.Fatal("tools field absent from output frontmatter")
	}
	if v.Kind != domain.KindList {
		t.Fatalf("tools field value kind: want %q, got %q", domain.KindList, v.Kind)
	}

	wantTools := []string{"read-file", "write-file"}
	if len(v.Items) != len(wantTools) {
		t.Fatalf("tools list length: want %d, got %d (items: %v)",
			len(wantTools), len(v.Items), extractScalars(v.Items))
	}
	for i, want := range wantTools {
		if v.Items[i].Scalar != want {
			t.Errorf("tools[%d]: want %q, got %q", i, want, v.Items[i].Scalar)
		}
	}
}

// TestFrontmatter_KeyOrder_OutputMatchesDescriptorOrder asserts that the keys present in
// both the output and the descriptor's key_order appear in exactly that order. Keys not
// in the order list may appear in any relative position after the ordered keys.
func TestFrontmatter_KeyOrder_OutputMatchesDescriptorOrder(t *testing.T) {
	mod := newFixtureModule(t)
	desc := mod.Descriptor()
	wantOrder := desc.Frontmatter.KeyOrder // [id, version, transform_version, injections_version, description, mode, model, tools]

	req := transform.Request{
		Source: []byte(frontmatterSource),
		Kind:   domain.ArtifactAgent,
		Key:    "fm-test-agent",
		Module: mod,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	outputKeys := doc.Frontmatter().Keys()

	// Collect only the positions of keys that appear in wantOrder.
	pos := 0
	for _, want := range wantOrder {
		found := false
		for pos < len(outputKeys) {
			if outputKeys[pos] == want {
				found = true
				pos++
				break
			}
			pos++
		}
		if !found {
			t.Errorf("key %q (from descriptor key_order) not found in output at or after expected position; output keys: %v",
				want, outputKeys)
		}
	}
}

// TestFrontmatter_UntouchedFieldsPreservedVerbatim asserts that fields not in the Add,
// Drop, or special-cased (model, tools, version stamps, id rename) sets are carried through
// byte-identical. The fixture descriptor preserves: version, description, required_skills.
// Note: the generic "id" field is renamed to "mosaic_id" in the deployed output (Step 4c),
// so it is verified under its deployed name here.
func TestFrontmatter_UntouchedFieldsPreservedVerbatim(t *testing.T) {
	doc := applyFrontmatterSource(t)
	fm := doc.Frontmatter()

	type check struct {
		key      string
		wantKind domain.ValueKind
		scalar   string // non-empty for KindScalar
	}
	checks := []check{
		{key: "mosaic_id", wantKind: domain.KindScalar, scalar: "42"},
		{key: "description", wantKind: domain.KindScalar, scalar: "Agent for frontmatter tests"},
		{key: "required_skills", wantKind: domain.KindList},
	}

	for _, c := range checks {
		v, ok := fm.Get(c.key)
		if !ok {
			t.Errorf("field %q expected to be preserved but is absent from output", c.key)
			continue
		}
		if v.Kind != c.wantKind {
			t.Errorf("field %q value kind: want %q, got %q", c.key, c.wantKind, v.Kind)
		}
		if c.scalar != "" && v.Scalar != c.scalar {
			t.Errorf("field %q value: want %q, got %q", c.key, c.scalar, v.Scalar)
		}
	}
}

// --- T8.4: Version stamping ---

// TestVersionStamp_TransformVersionFromModule asserts that the output frontmatter contains
// transform_version equal to the harness module's descriptor TransformVersion.
func TestVersionStamp_TransformVersionFromModule(t *testing.T) {
	mod := newFixtureModule(t)
	desc := mod.Descriptor()

	req := transform.Request{
		Source: []byte(frontmatterSource),
		Kind:   domain.ArtifactAgent,
		Key:    "fm-test-agent",
		Module: mod,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	v, ok := doc.Frontmatter().Get("mosaic_transform_version")
	if !ok {
		t.Fatal("mosaic_transform_version absent from output frontmatter")
	}
	if v.Scalar != desc.TransformVersion {
		t.Errorf("mosaic_transform_version: want %q (from module descriptor), got %q",
			desc.TransformVersion, v.Scalar)
	}
}

// TestVersionStamp_InjectionsVersionFromModule asserts that the output frontmatter contains
// injections_version equal to the harness module's descriptor InjectionsVersion.
func TestVersionStamp_InjectionsVersionFromModule(t *testing.T) {
	mod := newFixtureModule(t)
	desc := mod.Descriptor()

	req := transform.Request{
		Source: []byte(frontmatterSource),
		Kind:   domain.ArtifactAgent,
		Key:    "fm-test-agent",
		Module: mod,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	v, ok := doc.Frontmatter().Get("mosaic_injections_version")
	if !ok {
		t.Fatal("mosaic_injections_version absent from output frontmatter")
	}
	if v.Scalar != desc.InjectionsVersion {
		t.Errorf("mosaic_injections_version: want %q (from module descriptor), got %q",
			desc.InjectionsVersion, v.Scalar)
	}
}

// TestVersionStamp_SourceVersionCarriedThrough asserts that the source agent's version
// field value is present in the output unchanged. The transform engine must never alter
// the source version; it stamps transform_version and injections_version separately.
func TestVersionStamp_SourceVersionCarriedThrough(t *testing.T) {
	doc := applyFrontmatterSource(t)
	fm := doc.Frontmatter()

	// frontmatterSource declares version: 2.5.0
	v, ok := fm.Get("version")
	if !ok {
		t.Fatal("version field absent from output frontmatter")
	}
	if v.Scalar != "2.5.0" {
		t.Errorf("version field: want %q (source value, unchanged), got %q", "2.5.0", v.Scalar)
	}
}

// TestVersionStamp_AllThreeVersionFieldsPresent asserts that all three version-related
// fields appear in the output: version (source), transform_version, and injections_version.
// This covers the minimum observable contract for the version stamping stage.
func TestVersionStamp_AllThreeVersionFieldsPresent(t *testing.T) {
	doc := applyFrontmatterSource(t)
	fm := doc.Frontmatter()

	requiredVersionKeys := []string{"version", "mosaic_transform_version", "mosaic_injections_version"}
	for _, key := range requiredVersionKeys {
		if _, ok := fm.Get(key); !ok {
			t.Errorf("version field %q absent from output frontmatter", key)
		}
	}
}

// extractScalars returns the Scalar field of every KindScalar FieldValue in items.
// Used to produce readable assertion messages when a list comparison fails.
func extractScalars(items []domain.FieldValue) []string {
	out := make([]string, len(items))
	for i, v := range items {
		out[i] = v.Scalar
	}
	return out
}
