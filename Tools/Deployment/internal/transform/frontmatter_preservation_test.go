package transform_test

// frontmatter_preservation_test.go covers non-mosaic-managed frontmatter field
// preservation on Update (Stage 1). The bug: when the deployed file has the tools key,
// the toolsPreservedVerbatim flag short-circuits the entire toolResult.Fields writing
// loop, causing non-ToolsKey to:field destinations (like mcpServers) to be silently
// dropped. Additionally, no general preservation policy exists for user-owned deployed
// fields that the transform never writes.
//
// Tests:
//   - mcpServers from a to:field destination is retained after Update when ToolsKey
//     preservation activates (RED -- fails until I1.1 is implemented)
//   - An arbitrary user-owned field (customField) not managed by any transform step
//     is retained after Update (RED -- fails until I1.2 is implemented)
//   - ToolsKey verbatim preservation continues to work after the fix (regression guard)
//   - Non-ToolsKey toolResult.Fields are written normally on Create (regression guard)

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Inline descriptor for field preservation tests
// ---------------------------------------------------------------------------

// fieldPreservationDescriptorYAML is a list-shape harness with two universe tools.
// generic_a maps to both a main destination (alpha) and a field destination
// (mcpServers: user-feedback). generic_b maps to main only (beta). The descriptor
// declares tools_key: "tools" so that toolsPreservedVerbatim activates on Update when
// the deployed file has a tools field.
const fieldPreservationDescriptorYAML = `schema_version: "1"
id: "field-preservation-harness"
display_name: "Field Preservation Harness"
tools:
  shape: list
  universe:
    - name: "alpha"
      unused: deny
      by_convention: false
    - name: "beta"
      unused: deny
      by_convention: false
  mappings:
    - generic: "generic_a"
      destinations:
        - to: main
          names:
            - "alpha"
        - to: field
          field: "mcpServers"
          names:
            - "user-feedback"
    - generic: "generic_b"
      destinations:
        - to: main
          names:
            - "beta"
frontmatter:
  tools_key: "tools"
`

// ---------------------------------------------------------------------------
// Source agent YAML for field preservation tests
// ---------------------------------------------------------------------------

// fieldPreservationSource is a generic source declaring generic_a (which maps to both
// the main tools field and the mcpServers field) and generic_b (main only).
const fieldPreservationSource = `---
id: 200
version: 1.0.0
description: Field preservation test agent
tools: [generic_a, generic_b]
---
Body.
`

// ---------------------------------------------------------------------------
// Deployed file YAML for field preservation tests
// ---------------------------------------------------------------------------

// deployedWithToolsAndMcpServers is a deployed file that contains both the tools field
// (a flow-style list, which activates toolsPreservedVerbatim) and an mcpServers field
// populated by a previous deploy run via the to:field destination.
const deployedWithToolsAndMcpServers = `---
mosaic_id: 200
version: 1.0.0
tools: [alpha, beta]
mcpServers:
  - user-feedback
---
Body.
`

// deployedWithToolsAndCustomField is a deployed file that contains both the tools field
// and a user-owned field (customField) that no transform step manages. On Update,
// customField must survive unchanged under the general preservation policy.
const deployedWithToolsAndCustomField = `---
mosaic_id: 200
version: 1.0.0
tools: [alpha, beta]
customField: "user-value"
---
Body.
`

// deployedWithToolsAndUserExtra is a minimal deployed file with the tools field
// containing a user-added harness tool (user-extra) not present in the source.
// Used to verify that ToolsKey verbatim preservation still holds after the Stage 1 fix.
const deployedWithToolsAndUserExtra = `---
mosaic_id: 200
version: 1.0.0
tools: [alpha, beta, user-extra]
---
Body.
`

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// listContainsScalar reports whether the frontmatter field identified by key in doc
// is a KindList that contains a KindScalar item equal to want. Returns false when the
// field is absent or not a list.
func listContainsScalar(doc *docformat.Document, key, want string) bool {
	v, ok := doc.Frontmatter().Get(key)
	if !ok {
		return false
	}
	if v.Kind != domain.KindList {
		return false
	}
	for _, item := range v.Items {
		if item.Kind == domain.KindScalar && item.Scalar == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// AC1.1: mcpServers from toolResult.Fields is retained after Update when ToolsKey preserved
// ---------------------------------------------------------------------------

// TestFrontmatterPreservation_McpServersField_RetainedAfterUpdate asserts that when the
// deployed file has both the tools key (activating toolsPreservedVerbatim) and an
// mcpServers field populated by a previous to:field destination, the mcpServers field
// is present in the output after Update.
//
// This test is RED: the current toolsPreservedVerbatim path (frontmatter.go) short-circuits
// the entire toolResult.Fields loop, so mcpServers is never written when ToolsKey
// preservation activates. Until I1.1 restructures the path so that non-ToolsKey fields
// from toolResult.Fields are still written, this field is silently dropped.
func TestFrontmatterPreservation_McpServersField_RetainedAfterUpdate(t *testing.T) {
	mod := newDescriptorModule(t, fieldPreservationDescriptorYAML, "inline:field-preservation")
	req := transform.Request{
		Source:   []byte(fieldPreservationSource),
		Deployed: []byte(deployedWithToolsAndMcpServers),
		Kind:     domain.ArtifactAgent,
		Key:      "field-preservation-mcp-update-test",
		Module:   mod,
		Model:    domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	v, ok := doc.Frontmatter().Get("mcpServers")
	if !ok {
		t.Errorf("mcpServers field absent from output frontmatter after Update; "+
			"non-ToolsKey to:field destinations must be written even when ToolsKey is "+
			"preserved verbatim (AC1.1); frontmatter keys: %v", doc.Frontmatter().Keys())
		return
	}

	// The field must contain user-feedback, the name declared in the to:field destination.
	if !listContainsScalar(doc, "mcpServers", "user-feedback") {
		t.Errorf("mcpServers field does not contain %q after Update; "+
			"the to:field destination value must be written regardless of ToolsKey preservation; "+
			"field kind=%v", "user-feedback", v.Kind)
	}
}

// ---------------------------------------------------------------------------
// AC1.2: Arbitrary user-owned field retained after Update
// ---------------------------------------------------------------------------

// TestFrontmatterPreservation_ArbitraryDeployedField_RetainedAfterUpdate asserts that
// when the deployed file contains a user-owned field that no transform step manages
// (customField), that field is preserved verbatim in the output after Update.
//
// This test is RED: the current pipeline has no general deployed-field preservation
// policy. Until I1.2 adds a post-step pass that copies unmanaged deployed fields into
// the output frontmatter, customField is silently dropped.
func TestFrontmatterPreservation_ArbitraryDeployedField_RetainedAfterUpdate(t *testing.T) {
	mod := newDescriptorModule(t, fieldPreservationDescriptorYAML, "inline:field-preservation")
	req := transform.Request{
		Source:   []byte(fieldPreservationSource),
		Deployed: []byte(deployedWithToolsAndCustomField),
		Kind:     domain.ArtifactAgent,
		Key:      "field-preservation-custom-update-test",
		Module:   mod,
		Model:    domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	v, ok := doc.Frontmatter().Get("customField")
	if !ok {
		t.Errorf("customField absent from output frontmatter after Update; "+
			"user-owned fields not managed by the transform must be preserved verbatim "+
			"from the deployed file (AC1.2); frontmatter keys: %v", doc.Frontmatter().Keys())
		return
	}
	if v.Kind != domain.KindScalar || v.Scalar != "user-value" {
		t.Errorf("customField value: want KindScalar %q, got kind=%v scalar=%q; "+
			"the deployed field value must be preserved verbatim",
			"user-value", v.Kind, v.Scalar)
	}
}

// ---------------------------------------------------------------------------
// AC1.3: ToolsKey verbatim preservation regression guard
// ---------------------------------------------------------------------------

// TestFrontmatterPreservation_ToolsKey_PreservedVerbatimAfterUpdate is a regression guard
// asserting that the ToolsKey field continues to be preserved verbatim from the deployed
// file on Update after the Stage 1 fix. A user-added harness tool (user-extra) absent from
// the source must survive in the output tools field.
//
// This test exercises existing behavior (toolsPreservedVerbatim) and is expected to be
// GREEN immediately. It guards against the Stage 1 fix accidentally breaking ToolsKey
// preservation when restructuring the toolsPreservedVerbatim path.
func TestFrontmatterPreservation_ToolsKey_PreservedVerbatimAfterUpdate(t *testing.T) {
	mod := newDescriptorModule(t, fieldPreservationDescriptorYAML, "inline:field-preservation")
	req := transform.Request{
		Source:   []byte(fieldPreservationSource),
		Deployed: []byte(deployedWithToolsAndUserExtra),
		Kind:     domain.ArtifactAgent,
		Key:      "field-preservation-tools-regression-test",
		Module:   mod,
		Model:    domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	// user-extra is present in the deployed tools field. Verbatim preservation means
	// user-extra must appear in the output even though the source does not map to it.
	if !toolListContains(doc, "tools", "user-extra") {
		items := listFieldScalars(t, doc, "tools")
		t.Errorf("user-extra absent from output tools field after Update; "+
			"ToolsKey field must be preserved verbatim from the deployed file (AC1.3 regression guard); "+
			"got tools: %v", items)
	}
}

// ---------------------------------------------------------------------------
// AC1.4: Non-ToolsKey toolResult.Fields written on Create regression guard
// ---------------------------------------------------------------------------

// TestFrontmatterPreservation_NonToolsKeyField_WrittenOnCreate is a regression guard
// asserting that on Create (req.Deployed == nil), non-ToolsKey fields from
// toolResult.Fields -- such as mcpServers from a to:field destination -- are written
// normally to the output frontmatter. The Create path must not be affected by the
// I1.1 fix that restructures the toolsPreservedVerbatim block.
//
// This test exercises existing Create behavior and is expected to be GREEN immediately.
func TestFrontmatterPreservation_NonToolsKeyField_WrittenOnCreate(t *testing.T) {
	mod := newDescriptorModule(t, fieldPreservationDescriptorYAML, "inline:field-preservation")
	req := transform.Request{
		Source:   []byte(fieldPreservationSource),
		Deployed: nil, // Create -- no deployed file
		Kind:     domain.ArtifactAgent,
		Key:      "field-preservation-create-test",
		Module:   mod,
		Model:    domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	// mcpServers must be present on Create: toolsPreservedVerbatim is false (no deployed
	// file), so the toolResult.Fields loop runs and writes the to:field destination.
	v, ok := doc.Frontmatter().Get("mcpServers")
	if !ok {
		t.Errorf("mcpServers field absent from output frontmatter on Create; "+
			"non-ToolsKey to:field destinations must be written on Create (AC1.4 regression guard); "+
			"frontmatter keys: %v", doc.Frontmatter().Keys())
		return
	}
	if !listContainsScalar(doc, "mcpServers", "user-feedback") {
		t.Errorf("mcpServers field does not contain %q on Create; "+
			"the to:field destination value must appear in the output (AC1.4 regression guard); "+
			"field kind=%v", "user-feedback", v.Kind)
	}
}
