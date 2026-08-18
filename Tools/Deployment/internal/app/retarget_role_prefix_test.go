package app

// retarget_role_prefix_test.go verifies that BuildRetargetedAgent retains the role field
// across a harness-to-harness transform, regardless of whether the source file carries the
// role under the migrated "mosaic_role" key or the legacy bare "role" key.
//
// The retarget contract for the role field:
//   - The retargeted output carries the role under the deployed prefixed key "mosaic_role".
//   - The correct role value is preserved from the source, whichever key form it used.
//   - The bare legacy "role" key does not appear in the retargeted output (the value is
//     re-written under the prefixed form, as retarget does for bundle_version).
//
// Failure modes being pinned:
//   - Before I3.1+I3.4: a source with "mosaic_role" has it stripped as ClassUnknown
//     (IsKnownMosaicKey("mosaic_role") is currently false). The role is lost entirely.
//     → Tests asserting mosaic_role presence FAIL.
//   - After I3.1 but before I3.4: the retarget loop adds both "mosaic_role" and "role" to
//     removeKeys (because the role entry is now in All() but not yet carved out). Both
//     forms are stripped and no role survives in the output.
//     → Tests asserting mosaic_role presence still FAIL.
//   - After I3.1+I3.4: the role is captured before removal, both forms are stripped, and
//     the value is re-written as "mosaic_role". All tests PASS.
//
// Tests using bare-role sources currently also fail (the bare role is currently kept as
// ClassMosaic, so it passes through — but as bare "role", not "mosaic_role"). The assertion
// that the output carries "mosaic_role" (not bare "role") will FAIL until I3.4 re-writes
// the captured value under the prefixed key.

import (
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// minimalRetargetSourceWithMosaicRole returns source bytes for a deployed file whose role
// is carried under the prefixed "mosaic_role" key. This is the migrated form.
func minimalRetargetSourceWithMosaicRole(role string) []byte {
	return []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_injections_version: \"1.2.0\"\n" +
		"version: \"1.5.0\"\n" +
		"mosaic_id: \"55\"\n" +
		"mosaic_role: " + role + "\n" +
		"model: alpha-model-id\n" +
		"tools: [AlphaBash, AlphaRead]\n" +
		"alpha_stamp: alpha-v1\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the RetargetRoleTestAgent.\n" +
		"</Identity>\n")
}

// minimalRetargetSourceWithBareRole returns source bytes for a deployed file whose role
// is carried under the legacy bare "role" key. This is the pre-migration form.
func minimalRetargetSourceWithBareRole(role string) []byte {
	return []byte("---\n" +
		"transform_version: \"3.0.0\"\n" +
		"injections_version: \"1.2.0\"\n" +
		"version: \"1.5.0\"\n" +
		"id: \"55\"\n" +
		"role: " + role + "\n" +
		"model: alpha-model-id\n" +
		"tools: [AlphaBash, AlphaRead]\n" +
		"alpha_stamp: alpha-v1\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the RetargetRoleTestAgent.\n" +
		"</Identity>\n")
}

// retargetRoleSource calls BuildRetargetedAgent using the alpha-harness as source and
// beta-harness as target (both defined in retarget_helpers_test.go). Returns the parsed
// output document. t.Fatal is called if the retarget fails.
func retargetRoleSource(t *testing.T, src []byte) *docformat.Document {
	t.Helper()
	out, _, err := BuildRetargetedAgent(RetargetInput{
		Source:       src,
		SourceModule: newAlphaHarnessModule(),
		TargetModule: newBetaHarnessModule(),
		AgentKey:     "retarget-role-test",
	})
	if err != nil {
		t.Fatalf("BuildRetargetedAgent failed: %v", err)
	}
	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("docformat.Parse output: %v", parseErr)
	}
	return doc
}

// ---------------------------------------------------------------------------
// T3.5 — Role retention across retarget
// ---------------------------------------------------------------------------

// TestRetarget_MosaicRoleKey_RetainedAsMosaicRole verifies that a source file carrying
// "mosaic_role: orchestrator" produces a retargeted output that also carries
// "mosaic_role: orchestrator". The role value must not be lost during the harness-to-harness
// transform.
//
// FAILS currently because IsKnownMosaicKey("mosaic_role") is false (not in registry yet),
// so the key is classified as ClassUnknown and stripped. After I3.1 it is kept but may be
// stripped again by the removal loop until I3.4 carves it out.
func TestRetarget_MosaicRoleKey_RetainedAsMosaicRole(t *testing.T) {
	src := minimalRetargetSourceWithMosaicRole("orchestrator")
	doc := retargetRoleSource(t, src)
	fm := doc.Frontmatter()

	v, ok := fm.Get("mosaic_role")
	if !ok {
		t.Error("retargeted output does not carry \"mosaic_role\"; the role field must be retained across a harness-to-harness transform")
		return
	}
	if v.Scalar != "orchestrator" {
		t.Errorf("mosaic_role = %q, want \"orchestrator\"; role value must be preserved verbatim across retarget", v.Scalar)
	}
}

// TestRetarget_BareRoleKey_RetainedAsMosaicRole verifies that a source file carrying the
// legacy bare "role: orchestrator" key produces a retargeted output with "mosaic_role:
// orchestrator". The retarget path normalises the role key to the deployed prefixed form,
// whichever form the source used.
//
// FAILS currently because the output carries bare "role: orchestrator" (carried through as
// ClassMosaic without renaming), not the prefixed "mosaic_role: orchestrator". After I3.4
// adds the role carve-out and re-write, this test PASSES.
func TestRetarget_BareRoleKey_RetainedAsMosaicRole(t *testing.T) {
	src := minimalRetargetSourceWithBareRole("orchestrator")
	doc := retargetRoleSource(t, src)
	fm := doc.Frontmatter()

	v, ok := fm.Get("mosaic_role")
	if !ok {
		t.Error("retargeted output does not carry \"mosaic_role\" for a source with bare \"role\"; " +
			"retarget must re-write the role under the prefixed key form")
		return
	}
	if v.Scalar != "orchestrator" {
		t.Errorf("mosaic_role = %q, want \"orchestrator\"; role value must be preserved verbatim when normalised to the prefixed key", v.Scalar)
	}
}

// TestRetarget_BareRoleKey_NoBareRoleInOutput verifies that the bare "role" key does not
// appear in the retargeted output when the source carried it. The retarget output must
// always use the prefixed "mosaic_role" form — the bare key is stripped and the value is
// re-written under the prefixed name.
//
// FAILS currently because bare "role" passes through unchanged (ClassMosaic, no rename).
func TestRetarget_BareRoleKey_NoBareRoleInOutput(t *testing.T) {
	src := minimalRetargetSourceWithBareRole("utility")
	doc := retargetRoleSource(t, src)
	fm := doc.Frontmatter()

	if _, ok := fm.Get("role"); ok {
		t.Error("retargeted output carries bare \"role\" key; retarget must normalise the role to the prefixed \"mosaic_role\" key in the output")
	}
}

// TestRetarget_AllRoles_MosaicRoleSourceForm_RoleRetained verifies across all four role
// values that a source carrying "mosaic_role" produces a retargeted output with "mosaic_role"
// carrying the same value. FAILS until I3.1+I3.4.
func TestRetarget_AllRoles_MosaicRoleSourceForm_RoleRetained(t *testing.T) {
	for _, role := range []string{"subagent", "orchestrator", "utility", "standalone"} {
		t.Run(role, func(t *testing.T) {
			src := minimalRetargetSourceWithMosaicRole(role)
			doc := retargetRoleSource(t, src)
			fm := doc.Frontmatter()

			v, ok := fm.Get("mosaic_role")
			if !ok {
				t.Errorf("role=%q: retargeted output does not carry \"mosaic_role\"", role)
				return
			}
			if v.Scalar != role {
				t.Errorf("role=%q: mosaic_role = %q, want %q; role value must be preserved verbatim", role, v.Scalar, role)
			}
		})
	}
}

// TestRetarget_AllRoles_BareRoleSourceForm_MosaicRoleInOutput verifies across all four role
// values that a source carrying bare "role" produces a retargeted output with "mosaic_role"
// carrying the correct value. FAILS until I3.4 normalises the role key.
func TestRetarget_AllRoles_BareRoleSourceForm_MosaicRoleInOutput(t *testing.T) {
	for _, role := range []string{"subagent", "orchestrator", "utility", "standalone"} {
		t.Run(role, func(t *testing.T) {
			src := minimalRetargetSourceWithBareRole(role)
			doc := retargetRoleSource(t, src)
			fm := doc.Frontmatter()

			v, ok := fm.Get("mosaic_role")
			if !ok {
				t.Errorf("role=%q: retargeted output does not carry \"mosaic_role\" for a bare-role source", role)
				return
			}
			if v.Scalar != role {
				t.Errorf("role=%q: mosaic_role = %q, want %q; role value must be preserved verbatim when normalised", role, v.Scalar, role)
			}
			if _, bareOk := fm.Get("role"); bareOk {
				t.Errorf("role=%q: retargeted output carries bare \"role\" alongside the normalised mosaic_role; only the prefixed form should appear", role)
			}
		})
	}
}
