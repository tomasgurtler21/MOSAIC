package transform_test

// skill_version_rename_test.go verifies RenameSkillVersionField, the pure function
// responsible for renaming the "version" frontmatter field to "mosaic_version" in skill
// source bytes. All tests here exercise the public API; no implementation internals are
// tested.
//
// Behaviours verified (will FAIL until the implementation is complete):
//
//   Rename path — "version" present:
//     - The output contains "mosaic_version" in the frontmatter.
//     - The output does not contain bare "version" in the frontmatter.
//     - The value is preserved verbatim under the new key name.
//     - The renamed field occupies the same position in the key order as the original
//       "version" field (position-preserving rename).
//
//   Edge cases:
//     - Source with no "version" field returns bytes byte-identical to the input; nil error.
//     - Source with unparseable frontmatter returns bytes byte-identical to the input; nil error.
//
//   Agent isolation (FR-7 out-of-scope boundary):
//     - transform.Apply on agent source does not rename "version" to "mosaic_version";
//       agents continue to carry the bare "version" key in deployed output.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Rename path — "version" field present in skill source
// ---------------------------------------------------------------------------

// TestRenameSkillVersionField_WithVersionField_OutputHasMosaicVersion verifies that
// when the source bytes carry a "version" field in the frontmatter, the output contains
// "mosaic_version". This is the primary success path of the rename function.
// FAILS until the implementation performs the rename.
func TestRenameSkillVersionField_WithVersionField_OutputHasMosaicVersion(t *testing.T) {
	src := []byte("---\nmosaic_id: lean-tdd\nversion: \"1.2.0\"\nname: Lean TDD\n---\n\nSkill body.\n")

	out, err := transform.RenameSkillVersionField(src)
	if err != nil {
		t.Fatalf("RenameSkillVersionField returned unexpected error: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("output is not valid frontmatter: %v", parseErr)
	}
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_version"); !ok {
		t.Error("output frontmatter does not contain \"mosaic_version\"; RenameSkillVersionField must rename \"version\" to \"mosaic_version\"")
	}
}

// TestRenameSkillVersionField_WithVersionField_NoBareVersionInOutput verifies that the
// output does not carry the bare "version" key after the rename. A deployed skill file
// must not carry both "version" and "mosaic_version".
// FAILS until the implementation removes the original "version" key.
func TestRenameSkillVersionField_WithVersionField_NoBareVersionInOutput(t *testing.T) {
	src := []byte("---\nmosaic_id: lean-tdd\nversion: \"1.2.0\"\nname: Lean TDD\n---\n\nSkill body.\n")

	out, err := transform.RenameSkillVersionField(src)
	if err != nil {
		t.Fatalf("RenameSkillVersionField returned unexpected error: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("output is not valid frontmatter: %v", parseErr)
	}
	fm := doc.Frontmatter()

	if _, ok := fm.Get("version"); ok {
		t.Error("output frontmatter still contains bare \"version\" key; it must be removed when renamed to \"mosaic_version\"")
	}
}

// TestRenameSkillVersionField_WithVersionField_ValuePreservedVerbatim verifies that
// the version value is preserved exactly as written under the new key. The rename must
// not alter, normalise, or re-encode the value.
// FAILS until the implementation correctly carries the value.
func TestRenameSkillVersionField_WithVersionField_ValuePreservedVerbatim(t *testing.T) {
	src := []byte("---\nmosaic_id: efficient-reading\nversion: \"2.0.0-beta\"\nname: Efficient Reading\n---\n\nSkill body.\n")

	out, err := transform.RenameSkillVersionField(src)
	if err != nil {
		t.Fatalf("RenameSkillVersionField returned unexpected error: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("output is not valid frontmatter: %v", parseErr)
	}
	fm := doc.Frontmatter()

	v, ok := fm.Get("mosaic_version")
	if !ok {
		t.Fatal("\"mosaic_version\" not found in output; cannot verify value")
	}
	if v.Kind != domain.KindScalar {
		t.Fatalf("\"mosaic_version\" kind = %v, want KindScalar", v.Kind)
	}
	const want = "2.0.0-beta"
	if v.Scalar != want {
		t.Errorf("mosaic_version = %q, want %q; the version value must be preserved verbatim", v.Scalar, want)
	}
}

// TestRenameSkillVersionField_FieldPreservesPosition verifies that the renamed
// "mosaic_version" field appears at the same position in the frontmatter key order as
// the original "version" field. Skills do not go through a Reorder step after deploy, so
// the position must be maintained by the Insert algorithm.
//
// Scenario: source has keys [mosaic_id, version, name]. After rename, the key order
// must be [mosaic_id, mosaic_version, name] — "mosaic_version" at index 1, same as the
// original "version".
// FAILS until the implementation uses a position-preserving Insert rather than append.
func TestRenameSkillVersionField_FieldPreservesPosition(t *testing.T) {
	// Three-field frontmatter: mosaic_id at 0, version at 1, name at 2.
	src := []byte("---\nmosaic_id: lean-tdd\nversion: \"1.0.0\"\nname: Lean TDD Skill\n---\n\nSkill body.\n")

	out, err := transform.RenameSkillVersionField(src)
	if err != nil {
		t.Fatalf("RenameSkillVersionField returned unexpected error: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("output is not valid frontmatter: %v", parseErr)
	}
	keys := doc.Frontmatter().Keys()

	// Find the index of "mosaic_version" in the output key list.
	var idx = -1
	for i, k := range keys {
		if k == "mosaic_version" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("\"mosaic_version\" not found in output keys; cannot verify position")
	}
	// In the source, "version" was at index 1 (after "mosaic_id").
	// After rename, "mosaic_version" must still be at index 1.
	if idx != 1 {
		t.Errorf("\"mosaic_version\" is at index %d in output keys %v, want index 1; "+
			"the rename must preserve the original field position", idx, keys)
	}
	// The key immediately after "mosaic_version" must still be "name".
	if idx+1 < len(keys) && keys[idx+1] != "name" {
		t.Errorf("key after \"mosaic_version\" is %q, want \"name\"; field ordering must be preserved around the renamed key",
			keys[idx+1])
	}
}

// TestRenameSkillVersionField_VersionAsLastField_PreservesPosition verifies position
// preservation when "version" is the last frontmatter field. The renamed "mosaic_version"
// must remain last (appended to the end), which is the correct position when "version"
// had no successor key.
// FAILS until the implementation handles the last-field case correctly.
func TestRenameSkillVersionField_VersionAsLastField_PreservesPosition(t *testing.T) {
	// "version" is the last field.
	src := []byte("---\nmosaic_id: skill-key\nname: Test Skill\nversion: \"3.1.0\"\n---\n\nBody.\n")

	out, err := transform.RenameSkillVersionField(src)
	if err != nil {
		t.Fatalf("RenameSkillVersionField returned unexpected error: %v", err)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("output is not valid frontmatter: %v", parseErr)
	}
	keys := doc.Frontmatter().Keys()

	// "mosaic_version" must be the last key.
	if len(keys) == 0 {
		t.Fatal("output has no frontmatter keys")
	}
	last := keys[len(keys)-1]
	if last != "mosaic_version" {
		t.Errorf("last frontmatter key = %q, want \"mosaic_version\"; when \"version\" was the last field, the renamed field must remain last",
			last)
	}
}

// ---------------------------------------------------------------------------
// Edge case — no "version" field
// ---------------------------------------------------------------------------

// TestRenameSkillVersionField_NoVersionField_ReturnsBytesUnchanged verifies that when
// the source frontmatter does not contain a "version" field, RenameSkillVersionField
// returns bytes byte-identical to the input and a nil error. A skill without a version
// field is deployed as-is.
// FAILS if the implementation incorrectly modifies bytes or returns a non-nil error.
func TestRenameSkillVersionField_NoVersionField_ReturnsBytesUnchanged(t *testing.T) {
	// Source has a frontmatter but no "version" field.
	src := []byte("---\nmosaic_id: no-version-skill\nname: No Version Skill\n---\n\nSkill body without a version.\n")

	out, err := transform.RenameSkillVersionField(src)
	if err != nil {
		t.Fatalf("RenameSkillVersionField returned non-nil error for source without version field: %v", err)
	}
	if !bytes.Equal(out, src) {
		t.Errorf("RenameSkillVersionField modified the bytes despite no \"version\" field being present; "+
			"the output must be byte-identical to the input when there is nothing to rename")
	}
}

// TestRenameSkillVersionField_NoVersionField_NilError verifies that nil error is
// returned specifically when no "version" field is found, documenting the graceful
// degradation contract separately from the byte-identity invariant.
// This test PASSES from the start (the stub returns nil error), but is retained as
// explicit documentation of the contract.
func TestRenameSkillVersionField_NoVersionField_NilError(t *testing.T) {
	src := []byte("---\nmosaic_id: some-skill\nname: Some Skill\n---\n\nBody.\n")

	_, err := transform.RenameSkillVersionField(src)
	if err != nil {
		t.Errorf("RenameSkillVersionField returned non-nil error: %v; the contract requires nil error when no version field is present", err)
	}
}

// ---------------------------------------------------------------------------
// Edge case — unparseable frontmatter
// ---------------------------------------------------------------------------

// TestRenameSkillVersionField_UnparseableFrontmatter_ReturnsBytesUnchanged verifies
// that when the source bytes have malformed YAML frontmatter that cannot be parsed,
// RenameSkillVersionField returns bytes byte-identical to the input and a nil error.
// The skill is deployed as-is (graceful degradation).
// FAILS if the implementation incorrectly modifies bytes or returns a non-nil error.
func TestRenameSkillVersionField_UnparseableFrontmatter_ReturnsBytesUnchanged(t *testing.T) {
	// Duplicate key triggers a YAML parse error in docformat.
	src := []byte("---\nversion: \"1.0\"\nversion: \"2.0\"\n---\n\nSkill body after malformed frontmatter.\n")

	out, err := transform.RenameSkillVersionField(src)
	if err != nil {
		t.Fatalf("RenameSkillVersionField returned non-nil error for unparseable frontmatter: %v; "+
			"the contract requires nil error with graceful degradation", err)
	}
	if !bytes.Equal(out, src) {
		t.Errorf("RenameSkillVersionField modified bytes when frontmatter was unparseable; "+
			"output must be byte-identical to input on parse failure")
	}
}

// TestRenameSkillVersionField_UnparseableFrontmatter_NilError verifies that nil error
// is returned specifically for unparseable frontmatter, documenting the graceful
// degradation contract. A skill with malformed frontmatter must not block deployment.
// This test PASSES from the start (the stub returns nil error).
func TestRenameSkillVersionField_UnparseableFrontmatter_NilError(t *testing.T) {
	src := []byte("---\nversion: \"1.0\"\nversion: \"2.0\"\n---\n\nBody.\n")

	_, err := transform.RenameSkillVersionField(src)
	if err != nil {
		t.Errorf("RenameSkillVersionField returned non-nil error for malformed frontmatter: %v; "+
			"the graceful degradation contract requires nil error", err)
	}
}

// ---------------------------------------------------------------------------
// Agent isolation — FR-7 out-of-scope boundary
// ---------------------------------------------------------------------------

// TestAgentIsolation_AgentVersionFieldUnchangedByTransform verifies that transform.Apply
// on an agent source does NOT rename the "version" field to "mosaic_version". Agents
// must continue to carry the bare "version" key in deployed frontmatter; only skill
// artifacts go through RenameSkillVersionField.
//
// This test PASSES from the start (agents have always kept bare "version") and serves
// as a regression guard: the implementation must not inadvertently route agent content
// through RenameSkillVersionField or rename "version" during applyFrontmatter.
func TestAgentIsolation_AgentVersionFieldUnchangedByTransform(t *testing.T) {
	// Minimal agent source with a "version" field. The version field must remain "version"
	// (not become "mosaic_version") in the deployed output.
	agentSrc := []byte(`---
id: 42
version: 2.0.0
name: isolation-test-agent
description: Agent for isolation boundary test
model: {model-identifier}
tools: [file_read]
recommended_tier: MEDIUM
tier_rationale: medium task
required_skills: []
---

<Identity type="core">
Agent identity for isolation test.
</Identity>
`)
	req := transform.Request{
		Source: agentSrc,
		Kind:   domain.ArtifactAgent,
		Key:    "isolation-test-agent",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	doc, parseErr := docformat.Parse(result.Output)
	if parseErr != nil {
		t.Fatalf("docformat.Parse output: %v", parseErr)
	}
	fm := doc.Frontmatter()

	if _, ok := fm.Get("version"); !ok {
		t.Error("agent output frontmatter does not contain \"version\"; " +
			"agents must keep the bare \"version\" key — only skills are renamed to \"mosaic_version\"")
	}
	if _, ok := fm.Get("mosaic_version"); ok {
		t.Error("agent output frontmatter contains \"mosaic_version\"; " +
			"the \"version\" field must NEVER be renamed to \"mosaic_version\" for agents (FR-7 out-of-scope boundary)")
	}
}
