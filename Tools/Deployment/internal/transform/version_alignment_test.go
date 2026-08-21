package transform_test

// version_alignment_test.go verifies the version field alignment changes introduced in Stage 1:
//
//   T1.2 — version to mosaic_version rename in transform.Apply:
//     - The generic source's "version" field is renamed to "mosaic_version" in the deployed output.
//     - The bare "version" key is absent from the deployed output (the rename consumes it).
//     - The value is preserved verbatim under the new key name.
//
//   T1.3 — mosaic_transform_version to mosaic_harness_version stamp rename:
//     - transform.Apply writes the harness version stamp under "mosaic_harness_version".
//     - "mosaic_transform_version" is absent from the output (replaced by the new name).
//     - The value stamped under "mosaic_harness_version" equals the descriptor's declared version.
//
//   T1.4 — backwards-compatible migration stripping:
//     - When transform.Apply produces output for an Update scenario (req.Deployed non-nil),
//       legacy fields "mosaic_injections_version" and "mosaic_orchestrator_injections_version"
//       are absent from the output frontmatter (stripped by the migration step).
//     - The Report.Fields slice includes at least one FieldChange with Reason == "migration strip"
//       for each field that was stripped.
//
// All tests in this file FAIL until the corresponding Stage 1 implementation tasks are complete.

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Shared fixture for version alignment tests
// ---------------------------------------------------------------------------

// versionAlignmentSource is a minimal generic agent source that exercises the version field
// rename (T1.2) and harness version stamp rename (T1.3). It is intentionally identical in
// structure to other test sources so that the fixture module accepts it; the key property
// is that it declares "version: 1.7.0" which is the value the rename step must preserve.
const versionAlignmentSource = `---
id: 55
version: 1.7.0
name: version-alignment-agent
description: Agent for version alignment tests
model: {model-identifier}
tools: [file_read, file_write]
recommended_tier: MEDIUM
tier_rationale: medium-level task
required_skills: []
---

<Identity type="core">
Minimal body for version alignment tests.
</Identity>
`

// versionAlignmentDeployed is a minimal previously-deployed agent document that carries the
// OLD frontmatter field names. Passing this as req.Deployed simulates the Update scenario
// where an existing deployed file needs to be migrated away from legacy field names.
//
// The document contains:
//   - mosaic_transform_version: the old harness version stamp key (replaced by mosaic_harness_version)
//   - mosaic_injections_version: the old injections stamp key (relocated to region tags in Stage 2;
//     stripped by migration in Stage 1)
//   - mosaic_orchestrator_injections_version: the old orchestrator injections stamp key (stripped)
const versionAlignmentDeployed = `---
mosaic_id: 55
mosaic_version: 1.6.0
mosaic_transform_version: 2.9.0
mosaic_injections_version: 1.1.0
mosaic_orchestrator_injections_version: 1.0.0
mode: subagent
model: claude/claude-sonnet
tools: [read-file, write-file]
description: Agent for version alignment tests
---

<Identity type="core">
Previously deployed identity body.
</Identity>
`

// applyVersionAlignmentSource calls transform.Apply on versionAlignmentSource with the
// fixture module and the supplied deployed bytes (nil for create, non-nil for update).
// Returns the parsed output document and the result. Fails the test on any error.
func applyVersionAlignmentSource(t *testing.T, deployed []byte) (*docformat.Document, transform.Result) {
	t.Helper()
	req := transform.Request{
		Source:   []byte(versionAlignmentSource),
		Kind:     domain.ArtifactAgent,
		Key:      "version-alignment-agent",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Deployed: deployed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("docformat.Parse output: %v", err)
	}
	return doc, result
}

// ---------------------------------------------------------------------------
// T1.2 — version to mosaic_version rename
// ---------------------------------------------------------------------------

// TestVersionRename_MosaicVersionPresentInOutput verifies that when the generic source
// declares "version: X", the deployed output carries "mosaic_version: X". This is the
// primary assertion for Stage 1's new Step 4e rename (parallel to the id/role renames in
// Steps 4c/4d).
//
// This test FAILS until Step 4e (version-to-mosaic_version rename) is implemented in
// applyFrontmatter.
func TestVersionRename_MosaicVersionPresentInOutput(t *testing.T) {
	doc, _ := applyVersionAlignmentSource(t, nil)
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_version"); !ok {
		t.Error("output frontmatter does not carry \"mosaic_version\"; the generic source's \"version\" field must be renamed to \"mosaic_version\" in deployed output (Step 4e)")
	}
}

// TestVersionRename_BareVersionAbsentFromOutput verifies that after the rename, the bare
// "version" key no longer appears in the deployed output. The rename step must both remove
// the generic "version" key and set the "mosaic_version" key; the source value must not
// persist under the old name.
//
// This test FAILS until Step 4e removes the "version" key from the frontmatter.
func TestVersionRename_BareVersionAbsentFromOutput(t *testing.T) {
	doc, _ := applyVersionAlignmentSource(t, nil)
	fm := doc.Frontmatter()

	if _, ok := fm.Get("version"); ok {
		t.Error("output frontmatter still carries bare \"version\"; the rename step must remove the generic \"version\" key and rewrite it as \"mosaic_version\"")
	}
}

// TestVersionRename_ValuePreservedVerbatim verifies that the version value declared in the
// generic source ("1.7.0") is preserved exactly under the renamed "mosaic_version" key. The
// rename must not alter, normalise, or lose the value.
//
// This test FAILS until Step 4e carries the value through to "mosaic_version".
func TestVersionRename_ValuePreservedVerbatim(t *testing.T) {
	doc, _ := applyVersionAlignmentSource(t, nil)
	fm := doc.Frontmatter()

	v, ok := fm.Get("mosaic_version")
	if !ok {
		t.Fatal("\"mosaic_version\" absent from output; cannot verify value preservation")
	}
	if v.Kind != domain.KindScalar {
		t.Fatalf("\"mosaic_version\" kind = %v, want KindScalar", v.Kind)
	}
	const want = "1.7.0"
	if v.Scalar != want {
		t.Errorf("\"mosaic_version\" = %q, want %q; the version value must be preserved verbatim under the renamed key", v.Scalar, want)
	}
}

// TestVersionRename_FieldChangeRecorded verifies that the rename is captured as a
// FieldChange in Report.Fields with Key == "mosaic_version" and a non-empty Reason.
// Every frontmatter modification must be audited per the transform contract (AC8.5).
//
// This test FAILS until Step 4e records a FieldChange for the version rename.
func TestVersionRename_FieldChangeRecorded(t *testing.T) {
	_, result := applyVersionAlignmentSource(t, nil)

	var found bool
	for _, fc := range result.Report.Fields {
		if fc.Key == "mosaic_version" && fc.Reason != "" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Report.Fields does not contain a FieldChange for \"mosaic_version\" with a non-empty Reason; " +
			"the rename step must record an audit entry (AC8.5)")
	}
}

// ---------------------------------------------------------------------------
// T1.3 — mosaic_transform_version to mosaic_harness_version stamp rename
// ---------------------------------------------------------------------------

// TestHarnessVersionStamp_MosaicHarnessVersionPresentInOutput verifies that transform.Apply
// writes the harness version stamp under "mosaic_harness_version". After Stage 1's I1.3
// change, the applyVersionStamp call uses ByDeployedName("harness_version") instead of
// ByDeployedName("transform_version"), causing the stamp to be written under the new key.
//
// This test FAILS until I1.3 changes the ByDeployedName lookup from "transform_version" to
// "harness_version".
func TestHarnessVersionStamp_MosaicHarnessVersionPresentInOutput(t *testing.T) {
	doc, _ := applyVersionAlignmentSource(t, nil)
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_harness_version"); !ok {
		t.Error("output frontmatter does not carry \"mosaic_harness_version\"; the harness version stamp must use the new key name after Stage 1's I1.3 change")
	}
}

// TestHarnessVersionStamp_MosaicTransformVersionAbsentFromOutput verifies that
// "mosaic_transform_version" is absent from the deployed output. After I1.3 changes the
// stamp call site, the old key must no longer appear. The migration strip (I1.4) also
// removes it if it somehow remains.
//
// This test FAILS until I1.3 stops writing "mosaic_transform_version" (currently Step 4
// writes it via ByDeployedName("transform_version")).
func TestHarnessVersionStamp_MosaicTransformVersionAbsentFromOutput(t *testing.T) {
	doc, _ := applyVersionAlignmentSource(t, nil)
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_transform_version"); ok {
		t.Error("output frontmatter carries \"mosaic_transform_version\"; after Stage 1, the harness version stamp must be written under \"mosaic_harness_version\" only")
	}
}

// TestHarnessVersionStamp_ValueMatchesDescriptor verifies that the value written under
// "mosaic_harness_version" equals the descriptor's declared transform_version ("3.0.0" in
// the fixture descriptor). The stamp value source (desc.TransformVersion) is unchanged;
// only the frontmatter key name changes.
//
// This test FAILS until I1.3 writes the stamp under "mosaic_harness_version".
func TestHarnessVersionStamp_ValueMatchesDescriptor(t *testing.T) {
	doc, _ := applyVersionAlignmentSource(t, nil)
	fm := doc.Frontmatter()

	v, ok := fm.Get("mosaic_harness_version")
	if !ok {
		t.Fatal("\"mosaic_harness_version\" absent from output; cannot verify stamp value")
	}
	if v.Kind != domain.KindScalar {
		t.Fatalf("\"mosaic_harness_version\" kind = %v, want KindScalar", v.Kind)
	}
	// The fixture descriptor declares transform_version: "3.0.0" — this is still the value
	// source after the key rename. Only the frontmatter key name changes, not the source field.
	const want = "3.0.0"
	if v.Scalar != want {
		t.Errorf("\"mosaic_harness_version\" = %q, want %q (fixture descriptor's transform_version)", v.Scalar, want)
	}
}

// TestHarnessVersionStamp_FieldChangeRecorded verifies that the harness version stamp is
// captured in Report.Fields under the new key "mosaic_harness_version". The audit trail
// must name the key actually written.
//
// This test FAILS until I1.3 writes the stamp under "mosaic_harness_version" (currently
// the FieldChange carries Key="mosaic_transform_version").
func TestHarnessVersionStamp_FieldChangeRecorded(t *testing.T) {
	_, result := applyVersionAlignmentSource(t, nil)

	var found bool
	for _, fc := range result.Report.Fields {
		if fc.Key == "mosaic_harness_version" && fc.Reason != "" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Report.Fields does not contain a FieldChange for \"mosaic_harness_version\"; " +
			"after I1.3, the audit entry must use the new key name")
	}
}

// ---------------------------------------------------------------------------
// T1.4 — backwards-compatible migration stripping
// ---------------------------------------------------------------------------

// TestMigration_MosaicInjectionsVersion_AbsentFromOutput verifies that "mosaic_injections_version"
// is absent from the deployed output after Stage 1's migration strip. This field is currently
// written by Step 4's applyVersionStamp call; the migration strip (I1.4) removes it after
// writing so that it is absent from the final output. Stage 2 will later remove the write
// entirely; Stage 1 prepares for that by stripping after write.
//
// The Update scenario (req.Deployed non-nil) is used because the migration strip is designed
// for files that previously carried these fields and need them removed on the next deploy.
//
// This test FAILS until I1.4 adds the migration strip for "mosaic_injections_version".
func TestMigration_MosaicInjectionsVersion_AbsentFromOutput(t *testing.T) {
	doc, _ := applyVersionAlignmentSource(t, []byte(versionAlignmentDeployed))
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_injections_version"); ok {
		t.Error("output frontmatter carries \"mosaic_injections_version\"; the migration strip must remove this field (it will be relocated to region tags in Stage 2, and Stage 1 pre-strips it)")
	}
}

// TestMigration_MosaicOrchestratorInjectionsVersion_AbsentFromOutput verifies that
// "mosaic_orchestrator_injections_version" is absent from the deployed output after Stage 1's
// migration strip. This field is written by module-level Frontmatter() logic for orchestrator
// agents; the migration strip (I1.4) removes it.
//
// The Update scenario (req.Deployed non-nil containing the old field) is used to simulate
// a file that previously carried this field being redeployed.
//
// This test documents the migration contract. With the fixture module (which does not write
// mosaic_orchestrator_injections_version), it passes trivially; it serves as a regression
// guard ensuring the field never appears when the fixture module is used.
func TestMigration_MosaicOrchestratorInjectionsVersion_AbsentFromOutput(t *testing.T) {
	doc, _ := applyVersionAlignmentSource(t, []byte(versionAlignmentDeployed))
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_orchestrator_injections_version"); ok {
		t.Error("output frontmatter carries \"mosaic_orchestrator_injections_version\"; the migration strip must remove this field")
	}
}

// TestMigration_MosaicTransformVersion_AbsentFromDeployedOutput verifies that
// "mosaic_transform_version" is absent from the deployed output in an Update scenario where
// the previously-deployed file carried this old field. The I1.3 change stops writing the
// field; the I1.4 migration strip removes it if it somehow remains (e.g. residual from an
// intermediate state or manual edit).
//
// This test exercises both the I1.3 (write-path rename) and I1.4 (migration strip) changes
// together in the Update scenario.
//
// This test FAILS until I1.3 stops writing "mosaic_transform_version".
func TestMigration_MosaicTransformVersion_AbsentFromDeployedOutput(t *testing.T) {
	doc, _ := applyVersionAlignmentSource(t, []byte(versionAlignmentDeployed))
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_transform_version"); ok {
		t.Error("output frontmatter carries \"mosaic_transform_version\" in Update scenario; both I1.3 (write-path rename) and I1.4 (migration strip) must ensure this key is absent from the output")
	}
}

// TestMigration_NoInjectionVersionMigrationStripFieldChange verifies that Report.Fields does
// NOT contain a FieldChange with Key=="mosaic_injections_version" and Reason=="migration strip".
// Stage 2 removes the applyVersionStamp call for mosaic_injections_version, so the migration
// strip code never finds the field in fm and never fires. The field's absence from frontmatter
// output is guaranteed by the stamp removal itself, not by a strip-after-stamp pattern.
func TestMigration_NoInjectionVersionMigrationStripFieldChange(t *testing.T) {
	_, result := applyVersionAlignmentSource(t, []byte(versionAlignmentDeployed))

	for _, fc := range result.Report.Fields {
		if fc.Key == "mosaic_injections_version" && fc.Reason == "migration strip" {
			t.Error("Report.Fields contains a FieldChange with Key==\"mosaic_injections_version\" " +
				"and Reason==\"migration strip\"; Stage 2 removes the stamp call so the strip " +
				"never fires — the version is absent from frontmatter because it is never written, " +
				"not because it is written and then stripped")
		}
	}
}

// TestMigration_InjectionsVersionAbsentFromFieldChanges verifies that "mosaic_injections_version"
// does not appear in Report.Fields at all (neither as a "version stamp" nor as a "migration strip").
// Stage 2 removes both the stamp call (which previously wrote the field) and relies on the
// migration strip never firing (since there's nothing to strip). The version is now on region tags.
func TestMigration_InjectionsVersionAbsentFromFieldChanges(t *testing.T) {
	_, result := applyVersionAlignmentSource(t, []byte(versionAlignmentDeployed))

	for _, fc := range result.Report.Fields {
		if fc.Key == "mosaic_injections_version" {
			t.Errorf("Report.Fields contains a FieldChange for \"mosaic_injections_version\" "+
				"with Reason=%q; Stage 2 removes the frontmatter stamp for this field — "+
				"it should not appear in FieldChanges at all", fc.Reason)
		}
	}
}
