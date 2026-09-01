package transform_test

// version_stamp_preservation_test.go covers version-stamp frontmatter field preservation
// when the incoming version value is empty (no new stamp to write).
//
// The bug: touchedKeys entries for mosaic_harness_version, mosaic_tool_mappings_version,
// and mosaic_bundle_version are set unconditionally BEFORE applyVersionStamp is called.
// When applyVersionStamp is a no-op (empty incoming value), the key remains in touchedKeys
// and the Step 5c preservation pass skips it, silently dropping the previously-deployed
// value instead of carrying it forward.
//
// Tests:
//   - Empty ToolMappingsVersion: deployed mosaic_tool_mappings_version is preserved in
//     the output (RED -- fails until I2.1 moves the touchedKeys assignment)
//   - Empty TransformVersion: deployed mosaic_harness_version is preserved in the output
//     (RED -- fails until I2.1 moves the touchedKeys assignment)
//   - Non-applicable bundle: deployed mosaic_bundle_version is preserved in the output
//     (RED -- fails until I2.1 moves the bundle touchedKeys assignment inside the if block)
//   - Non-empty ToolMappingsVersion overwrites the deployed value (regression guard, GREEN)

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Inline descriptor for version-stamp preservation tests
// ---------------------------------------------------------------------------

// versionStampDescriptorYAML is a minimal harness descriptor with no transform_version
// field (so desc.TransformVersion == "") and no bundle-specific configuration. Used for
// tests where the incoming version value is empty and the deployed value must survive.
const versionStampDescriptorYAML = `schema_version: "1"
id: "version-stamp-harness"
display_name: "Version Stamp Preservation Harness"
`

// ---------------------------------------------------------------------------
// Source for version-stamp preservation tests
// ---------------------------------------------------------------------------

// versionStampSource is a minimal generic source with no tools field. The transform
// returns an empty tool result for sources without a tools declaration (safe no-op).
// We keep the source minimal to focus assertions on the version-stamp frontmatter fields.
const versionStampSource = `---
id: 300
version: 1.0.0
description: Version stamp preservation test agent
---
Body.
`

// ---------------------------------------------------------------------------
// Deployed files for version-stamp preservation tests
// ---------------------------------------------------------------------------

// deployedWithToolMappingsVersion is a deployed file carrying a mosaic_tool_mappings_version
// stamp written by a previous run. On the next run with an empty ToolMappingsVersion, this
// value must be preserved unchanged (not silently dropped).
const deployedWithToolMappingsVersion = `---
mosaic_id: 300
version: 1.0.0
mosaic_tool_mappings_version: hash-v1
---
Body.
`

// deployedWithHarnessVersion is a deployed file carrying a mosaic_harness_version stamp
// written by a previous run. On the next run with an empty TransformVersion (desc field),
// this value must be preserved unchanged (not silently dropped).
const deployedWithHarnessVersion = `---
mosaic_id: 300
version: 1.0.0
mosaic_harness_version: 2.0.0
---
Body.
`

// deployedWithBundleVersion is a deployed file carrying a mosaic_bundle_version stamp
// written by a previous run. When the bundle does not apply to the current role (or the
// bundle content is empty), the deployed value must be preserved (not silently dropped).
const deployedWithBundleVersion = `---
mosaic_id: 300
version: 1.0.0
mosaic_bundle_version: bundle-1.0
---
Body.
`

// deployedWithOldToolMappingsVersion is a deployed file carrying an older
// mosaic_tool_mappings_version stamp. Used in the regression guard to verify that a
// non-empty incoming ToolMappingsVersion correctly overwrites the deployed value.
const deployedWithOldToolMappingsVersion = `---
mosaic_id: 300
version: 1.0.0
mosaic_tool_mappings_version: hash-v1
---
Body.
`

// ---------------------------------------------------------------------------
// AC2.1: Empty ToolMappingsVersion preserves deployed mosaic_tool_mappings_version
// ---------------------------------------------------------------------------

// TestVersionStampPreservation_EmptyToolMappingsVersion_PreservesDeployedValue asserts
// that when ToolMappingsVersion is empty in the transform request (no new hash to write),
// the previously-deployed mosaic_tool_mappings_version value is preserved in the output.
//
// This test is RED: the current applyFrontmatter sets touchedKeys["mosaic_tool_mappings_version"]
// unconditionally before calling applyVersionStamp, so when applyVersionStamp returns nil
// (empty value), the key is still in touchedKeys and Step 5c's preservation pass skips it,
// dropping the deployed value. Until I2.1 moves the assignment to after the call (conditional
// on non-nil return), the deployed value is silently lost.
func TestVersionStampPreservation_EmptyToolMappingsVersion_PreservesDeployedValue(t *testing.T) {
	mod := newDescriptorModule(t, versionStampDescriptorYAML, "inline:version-stamp")
	req := transform.Request{
		Source:              []byte(versionStampSource),
		Deployed:            []byte(deployedWithToolMappingsVersion),
		Kind:                domain.ArtifactAgent,
		Key:                 "version-stamp-tool-mappings-test",
		Module:              mod,
		Model:               domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:               domain.ScopeProject,
		ToolMappingsVersion: "", // empty -> applyVersionStamp is a no-op; deployed value must survive
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	v, ok := doc.Frontmatter().Get("mosaic_tool_mappings_version")
	if !ok {
		t.Errorf("mosaic_tool_mappings_version absent from output after Update with empty ToolMappingsVersion; "+
			"when no new version is stamped, the deployed value must be preserved by the Step 5c "+
			"preservation pass (AC2.1); frontmatter keys: %v", doc.Frontmatter().Keys())
		return
	}
	if v.Kind != domain.KindScalar || v.Scalar != "hash-v1" {
		t.Errorf("mosaic_tool_mappings_version: want KindScalar %q, got kind=%v scalar=%q; "+
			"the deployed field value must be preserved verbatim when ToolMappingsVersion is empty (AC2.1)",
			"hash-v1", v.Kind, v.Scalar)
	}
}

// ---------------------------------------------------------------------------
// AC2.2: Empty TransformVersion preserves deployed mosaic_harness_version
// ---------------------------------------------------------------------------

// TestVersionStampPreservation_EmptyTransformVersion_PreservesDeployedHarnessVersion
// asserts that when the harness descriptor carries no transform_version (desc.TransformVersion
// == ""), the previously-deployed mosaic_harness_version value is preserved in the output.
//
// This test is RED: the current applyFrontmatter sets touchedKeys["mosaic_harness_version"]
// unconditionally before calling applyVersionStamp. When applyVersionStamp returns nil
// (empty TransformVersion), the key remains in touchedKeys and Step 5c's preservation pass
// skips it. Until I2.1 makes the touchedKeys assignment conditional on non-nil return,
// the deployed harness version is silently dropped.
func TestVersionStampPreservation_EmptyTransformVersion_PreservesDeployedHarnessVersion(t *testing.T) {
	// versionStampDescriptorYAML deliberately omits transform_version, so desc.TransformVersion
	// is the empty string and applyVersionStamp is a no-op for the harness version field.
	mod := newDescriptorModule(t, versionStampDescriptorYAML, "inline:version-stamp")
	req := transform.Request{
		Source:   []byte(versionStampSource),
		Deployed: []byte(deployedWithHarnessVersion),
		Kind:     domain.ArtifactAgent,
		Key:      "version-stamp-harness-version-test",
		Module:   mod,
		Model:    domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:    domain.ScopeProject,
		// ToolMappingsVersion left at zero value (empty)
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	v, ok := doc.Frontmatter().Get("mosaic_harness_version")
	if !ok {
		t.Errorf("mosaic_harness_version absent from output after Update with empty TransformVersion; "+
			"when no new harness version is stamped (descriptor has no transform_version), "+
			"the deployed value must be preserved by the Step 5c preservation pass (AC2.2); "+
			"frontmatter keys: %v", doc.Frontmatter().Keys())
		return
	}
	if v.Kind != domain.KindScalar || v.Scalar != "2.0.0" {
		t.Errorf("mosaic_harness_version: want KindScalar %q, got kind=%v scalar=%q; "+
			"the deployed field value must be preserved verbatim when TransformVersion is empty (AC2.2)",
			"2.0.0", v.Kind, v.Scalar)
	}
}

// ---------------------------------------------------------------------------
// AC2.3: Non-applicable bundle preserves deployed mosaic_bundle_version
// ---------------------------------------------------------------------------

// TestVersionStampPreservation_BundleNotApplicable_PreservesDeployedBundleVersion asserts
// that when the bundle does not apply to the current role (BundleContent.AppliesToRole
// returns false), the previously-deployed mosaic_bundle_version value is preserved.
//
// This test is RED: the current applyFrontmatter sets touchedKeys["mosaic_bundle_version"]
// unconditionally before the AppliesToRole check. When the bundle does not apply and the
// stamping block is skipped, the key is still in touchedKeys, causing Step 5c to skip
// preservation of the deployed value. Until I2.1 moves the touchedKeys assignment inside
// the AppliesToRole if-block, the deployed bundle version is silently dropped.
func TestVersionStampPreservation_BundleNotApplicable_PreservesDeployedBundleVersion(t *testing.T) {
	mod := newDescriptorModule(t, versionStampDescriptorYAML, "inline:version-stamp")
	req := transform.Request{
		Source:   []byte(versionStampSource),
		Deployed: []byte(deployedWithBundleVersion),
		Kind:     domain.ArtifactAgent,
		Key:      "version-stamp-bundle-version-test",
		Module:   mod,
		Model:    domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:    domain.ScopeProject,
		// Bundle with no blocks: AppliesToRole always returns false for any role.
		Bundle: domain.BundleContent{},
		Role:   domain.RoleSubagent,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	v, ok := doc.Frontmatter().Get("mosaic_bundle_version")
	if !ok {
		t.Errorf("mosaic_bundle_version absent from output after Update when bundle does not apply to role; "+
			"when AppliesToRole is false the bundle_version stamp is skipped and the deployed value "+
			"must be preserved by the Step 5c preservation pass (AC2.3); "+
			"frontmatter keys: %v", doc.Frontmatter().Keys())
		return
	}
	if v.Kind != domain.KindScalar || v.Scalar != "bundle-1.0" {
		t.Errorf("mosaic_bundle_version: want KindScalar %q, got kind=%v scalar=%q; "+
			"the deployed field value must be preserved verbatim when bundle does not apply to role (AC2.3)",
			"bundle-1.0", v.Kind, v.Scalar)
	}
}

// ---------------------------------------------------------------------------
// AC2.4: Non-empty version-stamp value overwrites deployed value (regression guard)
// ---------------------------------------------------------------------------

// TestVersionStampPreservation_NonEmptyToolMappingsVersion_OverwritesDeployedValue is a
// regression guard asserting that when ToolMappingsVersion is non-empty, the new value is
// written to the output and overwrites the previously-deployed mosaic_tool_mappings_version.
//
// This test exercises existing stamp-write behaviour and is expected to be GREEN immediately.
// It guards against the I2.1 fix accidentally suppressing stamp writes for non-empty values.
func TestVersionStampPreservation_NonEmptyToolMappingsVersion_OverwritesDeployedValue(t *testing.T) {
	mod := newDescriptorModule(t, versionStampDescriptorYAML, "inline:version-stamp")
	req := transform.Request{
		Source:              []byte(versionStampSource),
		Deployed:            []byte(deployedWithOldToolMappingsVersion), // deployed has "hash-v1"
		Kind:                domain.ArtifactAgent,
		Key:                 "version-stamp-overwrite-test",
		Module:              mod,
		Model:               domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:               domain.ScopeProject,
		ToolMappingsVersion: "hash-v2", // non-empty -> applyVersionStamp writes this value
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	v, ok := doc.Frontmatter().Get("mosaic_tool_mappings_version")
	if !ok {
		t.Errorf("mosaic_tool_mappings_version absent from output when non-empty ToolMappingsVersion provided; "+
			"version stamp must be written to the output when value is non-empty (AC2.4 regression guard); "+
			"frontmatter keys: %v", doc.Frontmatter().Keys())
		return
	}
	if v.Kind != domain.KindScalar || v.Scalar != "hash-v2" {
		t.Errorf("mosaic_tool_mappings_version: want KindScalar %q, got kind=%v scalar=%q; "+
			"a non-empty ToolMappingsVersion must overwrite the deployed value (AC2.4 regression guard)",
			"hash-v2", v.Kind, v.Scalar)
	}
}
