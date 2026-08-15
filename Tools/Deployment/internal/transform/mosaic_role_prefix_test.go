package transform_test

// mosaic_role_prefix_test.go verifies that the transform/deploy write path renames the
// generic source's `role` frontmatter field to its mosaic_-prefixed deployed form
// `mosaic_role`, and that the bare `role` key does not appear in the deployed output.
//
// The rename is parallel to the existing "Step 4c" id rename: the agentfields registry
// supplies both names via ByGeneric("role"), and the source's role value is preserved
// verbatim under the prefixed key. No literal "mosaic_role" string appears outside
// the agentfields package and its tests (AC3.6).
//
// Behaviours verified (all FAIL until I3.2 adds the role rename step to frontmatter.go):
//
//   - A source carrying role: subagent produces output with mosaic_role: subagent and
//     no bare role key.
//   - A source carrying role: orchestrator produces output with mosaic_role: orchestrator
//     and no bare role key.
//   - A source carrying role: utility produces output with mosaic_role: utility and
//     no bare role key.
//   - A source carrying role: standalone produces output with mosaic_role: standalone
//     and no bare role key.
//   - The role value is carried verbatim; it is never defaulted or transformed.
//
// Note: T3.1 asserts the registry predicates that make the rename possible. These tests
// focus exclusively on the write-path output.

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Shared fixture helpers for role prefix write-path tests
// ---------------------------------------------------------------------------

// roleSourceWithRole returns a minimal generic agent source with the given role value
// in the frontmatter. All other required fields are present so the transform pipeline
// accepts the document.
func roleSourceWithRole(role string) []byte {
	return []byte("---\n" +
		"id: 77\n" +
		"version: 1.0.0\n" +
		"name: role-prefix-test-agent\n" +
		"description: Agent for role prefix write-path tests\n" +
		"role: " + role + "\n" +
		"model: {model-identifier}\n" +
		"tools: [file_read, file_write]\n" +
		"recommended_tier: MEDIUM\n" +
		"tier_rationale: standard capability\n" +
		"required_skills: []\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Minimal identity section for role prefix tests.\n" +
		"</Identity>\n")
}

// applyRoleSource transforms a source with the given role value using the fixture module
// and returns the parsed output document. t.Fatal is called if the transform or parse fails.
func applyRoleSource(t *testing.T, role string) *docformat.Document {
	t.Helper()
	req := transform.Request{
		Source: roleSourceWithRole(role),
		Kind:   domain.ArtifactAgent,
		Key:    "role-test-" + role,
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("transform.Apply (role=%q): %v", role, err)
	}
	doc, parseErr := docformat.Parse(result.Output)
	if parseErr != nil {
		t.Fatalf("docformat.Parse output (role=%q): %v", role, parseErr)
	}
	return doc
}

// ---------------------------------------------------------------------------
// Write path — role renamed to mosaic_role for each role value
// ---------------------------------------------------------------------------

// TestTransform_WritesSubagentRoleAsMosaicRole verifies that a source carrying role: subagent
// produces deployed output with mosaic_role: subagent and no bare role key.
// FAILS until I3.2 adds the role rename step to frontmatter.go.
func TestTransform_WritesSubagentRoleAsMosaicRole(t *testing.T) {
	doc := applyRoleSource(t, "subagent")
	fm := doc.Frontmatter()

	if v, ok := fm.Get("mosaic_role"); !ok {
		t.Error("output frontmatter does not carry \"mosaic_role\"; " +
			"the generic source's role must be renamed to mosaic_role in the deployed output")
	} else if v.Kind != domain.KindScalar || v.Scalar != "subagent" {
		t.Errorf("mosaic_role = %q (kind=%v), want scalar \"subagent\"; role value must be preserved verbatim",
			v.Scalar, v.Kind)
	}
	if _, ok := fm.Get("role"); ok {
		t.Error("output frontmatter carries bare \"role\"; the deploy path must rename role to mosaic_role and not carry the generic key")
	}
}

// TestTransform_WritesOrchestratorRoleAsMosaicRole verifies that a source carrying
// role: orchestrator produces deployed output with mosaic_role: orchestrator and no bare role.
// FAILS until I3.2 adds the role rename step.
func TestTransform_WritesOrchestratorRoleAsMosaicRole(t *testing.T) {
	doc := applyRoleSource(t, "orchestrator")
	fm := doc.Frontmatter()

	if v, ok := fm.Get("mosaic_role"); !ok {
		t.Error("output frontmatter does not carry \"mosaic_role\" for an orchestrator-role source")
	} else if v.Kind != domain.KindScalar || v.Scalar != "orchestrator" {
		t.Errorf("mosaic_role = %q, want \"orchestrator\"; orchestrator role value must be preserved verbatim", v.Scalar)
	}
	if _, ok := fm.Get("role"); ok {
		t.Error("output frontmatter carries bare \"role\" for an orchestrator-role source; must be renamed to mosaic_role")
	}
}

// TestTransform_WritesUtilityRoleAsMosaicRole verifies that a source carrying role: utility
// produces deployed output with mosaic_role: utility and no bare role.
// FAILS until I3.2 adds the role rename step.
func TestTransform_WritesUtilityRoleAsMosaicRole(t *testing.T) {
	doc := applyRoleSource(t, "utility")
	fm := doc.Frontmatter()

	if v, ok := fm.Get("mosaic_role"); !ok {
		t.Error("output frontmatter does not carry \"mosaic_role\" for a utility-role source")
	} else if v.Kind != domain.KindScalar || v.Scalar != "utility" {
		t.Errorf("mosaic_role = %q, want \"utility\"; utility role value must be preserved verbatim", v.Scalar)
	}
	if _, ok := fm.Get("role"); ok {
		t.Error("output frontmatter carries bare \"role\" for a utility-role source; must be renamed to mosaic_role")
	}
}

// TestTransform_WritesStandaloneRoleAsMosaicRole verifies that a source carrying
// role: standalone produces deployed output with mosaic_role: standalone and no bare role.
// FAILS until I3.2 adds the role rename step.
func TestTransform_WritesStandaloneRoleAsMosaicRole(t *testing.T) {
	doc := applyRoleSource(t, "standalone")
	fm := doc.Frontmatter()

	if v, ok := fm.Get("mosaic_role"); !ok {
		t.Error("output frontmatter does not carry \"mosaic_role\" for a standalone-role source")
	} else if v.Kind != domain.KindScalar || v.Scalar != "standalone" {
		t.Errorf("mosaic_role = %q, want \"standalone\"; standalone role value must be preserved verbatim", v.Scalar)
	}
	if _, ok := fm.Get("role"); ok {
		t.Error("output frontmatter carries bare \"role\" for a standalone-role source; must be renamed to mosaic_role")
	}
}

// TestTransform_NoBareRoleKeyInDeployedOutput verifies across all four role values that the
// bare "role" key never appears in deployed output, and that "mosaic_role" carries the
// correct value. FAILS until I3.2 adds the role rename step.
func TestTransform_NoBareRoleKeyInDeployedOutput(t *testing.T) {
	for _, role := range []string{"subagent", "orchestrator", "utility", "standalone"} {
		t.Run(role, func(t *testing.T) {
			doc := applyRoleSource(t, role)
			fm := doc.Frontmatter()

			if _, ok := fm.Get("role"); ok {
				t.Errorf("role=%q: deployed output carries bare \"role\" key; must be renamed to mosaic_role", role)
			}
			if v, ok := fm.Get("mosaic_role"); !ok {
				t.Errorf("role=%q: deployed output does not carry \"mosaic_role\" key", role)
			} else if v.Scalar != role {
				t.Errorf("role=%q: mosaic_role = %q, want %q; value must be preserved verbatim", role, v.Scalar, role)
			}
		})
	}
}

// TestTransform_MosaicRoleValuePreservedVerbatim verifies that the role value is never
// coerced, defaulted, or transformed — it is placed under mosaic_role exactly as the
// generic source declared it. FAILS until I3.2 adds the rename step.
func TestTransform_MosaicRoleValuePreservedVerbatim(t *testing.T) {
	doc := applyRoleSource(t, "orchestrator")
	fm := doc.Frontmatter()

	v, ok := fm.Get("mosaic_role")
	if !ok {
		t.Fatal("mosaic_role absent from output; cannot verify value is preserved verbatim")
	}
	if v.Kind != domain.KindScalar {
		t.Fatalf("mosaic_role kind = %v, want KindScalar; role must be a scalar value", v.Kind)
	}
	if v.Scalar != "orchestrator" {
		t.Errorf("mosaic_role = %q, want \"orchestrator\"; the role value must be preserved verbatim, not coerced", v.Scalar)
	}
}
