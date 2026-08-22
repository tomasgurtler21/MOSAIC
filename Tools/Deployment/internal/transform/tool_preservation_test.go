package transform_test

// tool_preservation_test.go covers the tool preservation behavior of the Apply pipeline.
// When req.Deployed is non-nil (Update scenario), any tools present in the deployed
// file but absent from the current generic-source-derived output must be preserved in
// the output unchanged. This prevents user-added harness tools from being discarded on
// every Update.
//
// These tests are in the TDD RED phase: they compile but fail until mergeDeployedTools
// is implemented in applyFrontmatter after Step 5.
//
// Coverage:
//   - User-added tools survive Update when the deployed tools field is a flow-style list
//   - User-added tools survive Update when the deployed tools field is a comma-separated scalar
//   - New tools added by the generic source appear alongside preserved user tools
//   - Tools dropped from the generic source are never auto-removed from deployed files
//   - Preservation works for permission-shape (KindMapping) deployed tool fields
//   - Placeholder source: user-added tools beyond the placeholder expansion are preserved

import (
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Inline descriptor YAML for tool preservation tests
// ---------------------------------------------------------------------------

// preservationListDescriptorYAML is a list-shape harness with four Universe tools
// (alpha, beta, gamma, delta) and one generic mapping per tool. Used to construct
// scenarios where the generic source declares a subset of the deployed tools.
const preservationListDescriptorYAML = `schema_version: "1"
id: "preservation-list-harness"
display_name: "Preservation List Harness"
tools:
  shape: list
  universe:
    - name: "alpha"
      unused: deny
      by_convention: false
    - name: "beta"
      unused: deny
      by_convention: false
    - name: "gamma"
      unused: deny
      by_convention: false
    - name: "delta"
      unused: deny
      by_convention: false
  mappings:
    - generic: "generic_a"
      destinations:
        - to: main
          names:
            - "alpha"
    - generic: "generic_b"
      destinations:
        - to: main
          names:
            - "beta"
    - generic: "generic_c"
      destinations:
        - to: main
          names:
            - "gamma"
    - generic: "generic_d"
      destinations:
        - to: main
          names:
            - "delta"
frontmatter:
  tools_key: "tools"
`

// preservationPermissionDescriptorYAML is a permission-shape harness (ShapePermission)
// that stores tools as a KindMapping with per-tool allow/deny dispositions. Used to
// verify that mergeDeployedTools correctly handles the permission-shape deployed tool
// field via descriptor.ExtractToolEntries' ShapePermission branch.
const preservationPermissionDescriptorYAML = `schema_version: "1"
id: "preservation-permission-harness"
display_name: "Preservation Permission Harness"
tools:
  shape: permission
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
    - generic: "generic_b"
      destinations:
        - to: main
          names:
            - "beta"
frontmatter:
  tools_key: "tools"
`

// ---------------------------------------------------------------------------
// Generic source YAML for tool preservation tests
// ---------------------------------------------------------------------------

// preservationSourceAB is a generic source declaring only generic_a and generic_b.
// After transformation with preservationListDescriptorYAML, the output tools field
// contains alpha and beta — NOT gamma or delta, which are absent from this source.
const preservationSourceAB = `---
id: 100
version: 1.0.0
description: Tool preservation test agent
tools: [generic_a, generic_b]
---
Body.
`

// preservationSourceABD is a generic source declaring generic_a, generic_b, and generic_d.
// After transformation, the output tools field contains alpha, beta, and delta.
// Used to test that delta (a newly-added source tool) appears alongside preserved user tools.
const preservationSourceABD = `---
id: 101
version: 1.0.0
description: Tool addition with preservation test agent
tools: [generic_a, generic_b, generic_d]
---
Body.
`

// preservationPlaceholderSource is a generic source that uses the {tool-permissions}
// placeholder instead of an explicit list. Used with a descriptor whose placeholder
// expansion covers the full Universe, leaving no room for user-extra unless preserved.
const preservationPlaceholderSource = `---
id: 102
version: 1.0.0
description: Placeholder preservation test agent
tools: {tool-permissions}
---
Body.
`

// preservationPermissionSource is a generic source for the permission-shape descriptor.
const preservationPermissionSource = `---
id: 103
version: 1.0.0
description: Permission shape preservation test agent
tools: [generic_a, generic_b]
---
Body.
`

// ---------------------------------------------------------------------------
// Deployed file YAML for tool preservation tests
//
// Deployed files represent the currently-deployed state (req.Deployed). They contain
// the harness-specific rendered forms of tools (alpha/beta/...) rather than generic
// names. Their frontmatter format mimics what a previous deployment would have produced.
// ---------------------------------------------------------------------------

// deployedListABWithUserExtra is a deployed file where the tools field is a
// flow-style list (KindList). It contains the normally-computed tools alpha and beta
// plus a user-added tool "user-extra" that has no generic-source mapping.
const deployedListABWithUserExtra = `---
mosaic_id: 100
version: 1.0.0
transform_version: 1.0.0
tools: [alpha, beta, user-extra]
---
Body.
`

// deployedScalarABWithUserExtra is a deployed file where the tools field is a
// comma-separated scalar (KindScalar), matching the format produced by harnesses such
// as Claude Code. ExtractToolEntries handles this via comma-splitting.
const deployedScalarABWithUserExtra = `---
mosaic_id: 100
version: 1.0.0
transform_version: 1.0.0
tools: "alpha, beta, user-extra"
---
Body.
`

// deployedListABWithUserExtra_ForABD is a deployed file for the T4.2 scenario where
// the source later adds generic_d. The deployed file does not contain delta yet.
const deployedListABWithUserExtra_ForABD = `---
mosaic_id: 101
version: 1.0.0
transform_version: 1.0.0
tools: [alpha, beta, user-extra]
---
Body.
`

// deployedListABGamma is a deployed file that includes gamma, previously produced
// when the generic source included generic_c. In T4.3, the source has since dropped
// generic_c, but gamma must remain in the output (never auto-removed).
const deployedListABGamma = `---
mosaic_id: 100
version: 1.0.0
transform_version: 1.0.0
tools: [alpha, beta, gamma]
---
Body.
`

// deployedPermissionABWithUserExtra is a deployed file with a permission-shape
// (KindMapping) tools field. It contains alpha and beta with allow disposition, plus
// a user-added tool "user-extra: allow".
const deployedPermissionABWithUserExtra = `---
mosaic_id: 103
version: 1.0.0
transform_version: 1.0.0
tools:
  alpha: allow
  beta: allow
  user-extra: allow
---
Body.
`

// deployedPlaceholderWithUserExtra is a deployed file for the placeholder scenario.
// It contains the full Universe (alpha, beta, gamma) plus a user-added "user-extra".
// Used with placeholderNoExpansionDescriptorYAML (universe: alpha, beta, gamma).
const deployedPlaceholderWithUserExtra = `---
mosaic_id: 102
version: 1.0.0
transform_version: 1.0.0
tools: [alpha, beta, gamma, user-extra]
---
Body.
`

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// toolListContains reports whether toolName appears in the KindList or
// comma-separated KindScalar value of the frontmatter field identified by key.
// Returns false when the field is absent or of an unexpected kind.
func toolListContains(doc *docformat.Document, key, toolName string) bool {
	v, ok := doc.Frontmatter().Get(key)
	if !ok {
		return false
	}
	switch v.Kind {
	case domain.KindList:
		for _, item := range v.Items {
			if item.Kind == domain.KindScalar && item.Scalar == toolName {
				return true
			}
		}
	case domain.KindScalar:
		for _, part := range strings.Split(v.Scalar, ",") {
			if strings.TrimSpace(part) == toolName {
				return true
			}
		}
	}
	return false
}

// permissionToolContains reports whether toolName is present with allow disposition
// in the KindMapping value of the frontmatter field identified by key.
// Returns false when the field is absent, not a KindMapping, or toolName is absent or denied.
func permissionToolContains(doc *docformat.Document, key, toolName string) bool {
	v, ok := doc.Frontmatter().Get(key)
	if !ok {
		return false
	}
	if v.Kind != domain.KindMapping {
		return false
	}
	allowStr := string(domain.Allow)
	for _, pair := range v.Pairs {
		if pair.Key == toolName && pair.Value.Kind == domain.KindScalar && pair.Value.Scalar == allowStr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// T4.1: User-added tools survive Update — flow-style list deployed tools field
// ---------------------------------------------------------------------------

// TestToolPreservation_UserAddedTool_PresentInOutputAfterUpdate asserts that when the
// deployed file contains a user-added harness tool ("user-extra") not present in the
// generic source, that tool appears in the output tools field after Update. The deployed
// tools field is a flow-style list (KindList), exercising the KindList branch of
// descriptor.ExtractToolEntries.
//
// This test will be RED until mergeDeployedTools is implemented after Step 5 in
// applyFrontmatter: the current pipeline fully overwrites the tools field with
// source-derived tools only, discarding user-extra.
func TestToolPreservation_UserAddedTool_PresentInOutputAfterUpdate(t *testing.T) {
	mod := newDescriptorModule(t, preservationListDescriptorYAML, "inline:preservation-list")
	req := transform.Request{
		Source:   []byte(preservationSourceAB),
		Deployed: []byte(deployedListABWithUserExtra),
		Kind:     domain.ArtifactAgent,
		Key:      "preservation-test-agent",
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

	if !toolListContains(doc, "tools", "user-extra") {
		items := listFieldScalars(t, doc, "tools")
		t.Errorf("user-added tool %q absent from output tools field after Update; "+
			"user-added tools must survive Update unchanged (AC4.1); "+
			"got output tools: %v", "user-extra", items)
	}
}

// TestToolPreservation_NormallyComputedTools_StillPresentAlongsideUserAdded asserts that
// the tools derived from the generic source (alpha and beta) remain in the output tools
// field alongside any preserved user tools. Preservation must not discard source tools.
func TestToolPreservation_NormallyComputedTools_StillPresentAlongsideUserAdded(t *testing.T) {
	mod := newDescriptorModule(t, preservationListDescriptorYAML, "inline:preservation-list")
	req := transform.Request{
		Source:   []byte(preservationSourceAB),
		Deployed: []byte(deployedListABWithUserExtra),
		Kind:     domain.ArtifactAgent,
		Key:      "preservation-normal-tools-agent",
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

	for _, want := range []string{"alpha", "beta"} {
		if !toolListContains(doc, "tools", want) {
			items := listFieldScalars(t, doc, "tools")
			t.Errorf("source-mapped tool %q absent from output tools field; "+
				"source tools must remain present when user tools are also preserved; "+
				"got output tools: %v", want, items)
		}
	}
}

// ---------------------------------------------------------------------------
// T4.1: User-added tools survive Update — comma-separated scalar deployed tools field
// ---------------------------------------------------------------------------

// TestToolPreservation_ScalarShapeDeployed_UserAddedToolPreserved asserts that when the
// deployed file stores tools as a comma-separated scalar (KindScalar), as produced by
// harnesses like Claude Code, a user-added tool in that scalar survives Update. This
// exercises the comma-split branch of descriptor.ExtractToolEntries.
//
// This test will be RED until mergeDeployedTools is implemented: the current pipeline
// overwrites the tools field with source-derived list tools only, discarding user-extra.
func TestToolPreservation_ScalarShapeDeployed_UserAddedToolPreserved(t *testing.T) {
	mod := newDescriptorModule(t, preservationListDescriptorYAML, "inline:preservation-list")
	req := transform.Request{
		Source:   []byte(preservationSourceAB),
		Deployed: []byte(deployedScalarABWithUserExtra),
		Kind:     domain.ArtifactAgent,
		Key:      "scalar-preservation-agent",
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

	if !toolListContains(doc, "tools", "user-extra") {
		items := listFieldScalars(t, doc, "tools")
		t.Errorf("user-added tool %q absent from output tools field after Update with "+
			"comma-separated scalar deployed tools; "+
			"ExtractToolEntries must parse KindScalar (comma-split) deployed tools for preservation (AC4.4); "+
			"got output tools: %v", "user-extra", items)
	}
}

// ---------------------------------------------------------------------------
// Stage 4 contract: verbatim copy from deployed — new source tools NOT added
// ---------------------------------------------------------------------------

// TestToolPreservation_NewSourceTool_AddedAlongsidePreservedUserTool asserts the Stage 4
// contract: on Update, the output tools field is a verbatim copy of the deployed file's
// tools field. When the source gains a new tool mapping (generic_d → delta) that was NOT
// in the deployed file, delta is NOT added to the output. The deployed file's tools field
// — [alpha, beta, user-extra] — is preserved byte-exact. Source-computed tools (including
// new ones like delta) are not written to the output on Update; tools are user-owned after
// initial Deploy.
//
// Deployed file contains: [alpha, beta, user-extra]. Source maps to: [alpha, beta, delta].
// Expected output: exactly [alpha, beta, user-extra] (verbatim from deployed). delta must
// be absent (it is source-computed, not user-deployed). user-extra must be present
// (verbatim from deployed). The total count must be exactly 3.
//
// This test is RED until Stage 4 verbatim-copy behavior is implemented in applyFrontmatter:
// the current pipeline (Steps 5/5b) sets [alpha, beta, delta] from source and then merges
// user-extra, producing [alpha, beta, delta, user-extra] — delta appears when it should not.
func TestToolPreservation_NewSourceTool_AddedAlongsidePreservedUserTool(t *testing.T) {
	mod := newDescriptorModule(t, preservationListDescriptorYAML, "inline:preservation-list")
	req := transform.Request{
		Source:   []byte(preservationSourceABD),
		Deployed: []byte(deployedListABWithUserExtra_ForABD),
		Kind:     domain.ArtifactAgent,
		Key:      "addition-preservation-agent",
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

	// delta must NOT appear: it is a source-computed tool not present in the deployed file.
	// On Update, tools are user-owned and copied verbatim from the deployed file; new source
	// tools are not automatically added.
	if toolListContains(doc, "tools", "delta") {
		items := listFieldScalars(t, doc, "tools")
		t.Errorf("source-computed tool %q must not appear in output tools field on Update; "+
			"on Update, the tools field is user-owned and copied verbatim from the deployed "+
			"file — new source tools are not added; deployed was [alpha, beta, user-extra]; "+
			"got output tools: %v", "delta", items)
	}

	// user-extra must appear: it is present in the deployed file and must be preserved verbatim.
	if !toolListContains(doc, "tools", "user-extra") {
		items := listFieldScalars(t, doc, "tools")
		t.Errorf("deployed tool %q absent from output tools field after Update; "+
			"the deployed file's tools field must be preserved verbatim — [alpha, beta, user-extra]; "+
			"got output tools: %v", "user-extra", items)
	}

	// alpha and beta must appear: they are present in the deployed file.
	for _, tool := range []string{"alpha", "beta"} {
		if !toolListContains(doc, "tools", tool) {
			items := listFieldScalars(t, doc, "tools")
			t.Errorf("deployed tool %q absent from output tools field after Update; "+
				"the deployed file's tools field must be preserved verbatim — [alpha, beta, user-extra]; "+
				"got output tools: %v", tool, items)
		}
	}

	// The output must contain exactly 3 tools — the verbatim deployed set [alpha, beta, user-extra].
	items := listFieldScalars(t, doc, "tools")
	if len(items) != 3 {
		t.Errorf("output tools field has %d items, want exactly 3 "+
			"(verbatim deployed set [alpha, beta, user-extra]); "+
			"source-computed tools must not be merged into the output on Update; got: %v",
			len(items), items)
	}
}

// TestToolPreservation_NewSourceTool_NoUnintendedDuplication asserts that the merge
// does not duplicate tools that are already present in the output from the source
// mapping. When delta is added by the source and also happened to be in the deployed
// file, it must appear exactly once in the output.
func TestToolPreservation_NewSourceTool_NoUnintendedDuplication(t *testing.T) {
	// Deployed file already has delta (perhaps from a previous manual addition).
	const deployedWithDeltaAndUserExtra = `---
mosaic_id: 101
version: 1.0.0
transform_version: 1.0.0
tools: [alpha, beta, delta, user-extra]
---
Body.
`
	mod := newDescriptorModule(t, preservationListDescriptorYAML, "inline:preservation-list")
	req := transform.Request{
		Source:   []byte(preservationSourceABD),
		Deployed: []byte(deployedWithDeltaAndUserExtra),
		Kind:     domain.ArtifactAgent,
		Key:      "no-duplication-agent",
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

	// delta must appear exactly once (it comes from source; deployed copy must not be duplicated).
	items := listFieldScalars(t, doc, "tools")
	count := countOccurrences(items, "delta")
	if count != 1 {
		t.Errorf("tool %q appears %d times in output tools field; want exactly 1 (no duplication); "+
			"tools already present in source output must not be double-appended during merge; "+
			"got: %v", "delta", count, items)
	}
}

// ---------------------------------------------------------------------------
// T4.3: Tool dropped from source is never auto-removed from deployed file
// ---------------------------------------------------------------------------

// TestToolPreservation_SourceDroppedTool_NeverAutoRemovedFromOutput asserts that when
// the generic source previously mapped generic_c to gamma but now declares only
// generic_a and generic_b (generic_c dropped), gamma survives in the output because
// it is present in the deployed file. The "never auto-remove" rule means that a tool
// in the deployed file is preserved regardless of whether the source ever mapped to it
// or not (AC4.3).
//
// This test will be RED until mergeDeployedTools is implemented: currently Step 5
// overwrites the tools field with alpha and beta only, dropping gamma.
func TestToolPreservation_SourceDroppedTool_NeverAutoRemovedFromOutput(t *testing.T) {
	// Source has dropped generic_c (which mapped to gamma). The deployed file still has gamma.
	mod := newDescriptorModule(t, preservationListDescriptorYAML, "inline:preservation-list")
	req := transform.Request{
		Source:   []byte(preservationSourceAB),
		Deployed: []byte(deployedListABGamma),
		Kind:     domain.ArtifactAgent,
		Key:      "drop-preservation-agent",
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

	if !toolListContains(doc, "tools", "gamma") {
		items := listFieldScalars(t, doc, "tools")
		t.Errorf("tool %q absent from output tools field; tools present in the deployed file "+
			"must never be auto-removed even when the generic source no longer maps to them (AC4.3); "+
			"got output tools: %v", "gamma", items)
	}
}

// ---------------------------------------------------------------------------
// AC4.4: Permission-shape (KindMapping) deployed tool field preservation
// ---------------------------------------------------------------------------

// TestToolPreservation_PermissionShape_UserAddedToolPreserved asserts that when the
// descriptor uses shape: permission (producing a KindMapping tools field), a user-added
// tool with allow disposition in the deployed file is preserved in the output's KindMapping
// field. This exercises the ShapePermission branch of descriptor.ExtractToolEntries.
//
// This test will be RED until mergeDeployedTools handles the KindMapping append case:
// the current pipeline writes only alpha and beta with allow disposition, discarding
// the user-extra: allow entry from the deployed file.
func TestToolPreservation_PermissionShape_UserAddedToolPreserved(t *testing.T) {
	mod := newDescriptorModule(t, preservationPermissionDescriptorYAML, "inline:preservation-permission")
	req := transform.Request{
		Source:   []byte(preservationPermissionSource),
		Deployed: []byte(deployedPermissionABWithUserExtra),
		Kind:     domain.ArtifactAgent,
		Key:      "permission-preservation-agent",
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

	if !permissionToolContains(doc, "tools", "user-extra") {
		t.Errorf("user-added tool %q with allow disposition absent from output permission-shape "+
			"tools field after Update; "+
			"mergeDeployedTools must handle KindMapping tool fields and append "+
			"user-added allow entries (AC4.4); "+
			"output frontmatter: %v", "user-extra", doc.Frontmatter().Keys())
	}
}

// ---------------------------------------------------------------------------
// T4.4: Placeholder source — user-added deployed tools preserved beyond expansion set
// ---------------------------------------------------------------------------

// TestToolPreservation_PlaceholderSource_ExtraDeployedToolPreserved asserts that when
// the generic source uses the {tool-permissions} placeholder (which expands to the
// full Universe), a user-added tool in the deployed file that lies outside the Universe
// is still preserved in the output. The merge must compare the deployed set against
// the expanded output set and preserve any surplus entries.
//
// Uses placeholderNoExpansionDescriptorYAML (defined in tools_test.go) whose Universe
// is [alpha, beta, gamma]. The deployed file also contains "user-extra" which is not
// in the Universe. After expansion the output would be [alpha, beta, gamma]; after merge
// it must be [alpha, beta, gamma, user-extra].
//
// This test will be RED until mergeDeployedTools is implemented: the current pipeline
// produces only the expansion result [alpha, beta, gamma], discarding user-extra.
func TestToolPreservation_PlaceholderSource_ExtraDeployedToolPreserved(t *testing.T) {
	mod := newDescriptorModule(t, placeholderNoExpansionDescriptorYAML, "inline:no-expansion")
	req := transform.Request{
		Source:   []byte(preservationPlaceholderSource),
		Deployed: []byte(deployedPlaceholderWithUserExtra),
		Kind:     domain.ArtifactAgent,
		Key:      "placeholder-preservation-agent",
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

	if !toolListContains(doc, "tools", "user-extra") {
		items := listFieldScalars(t, doc, "tools")
		t.Errorf("user-added tool %q absent from output tools field after Update with "+
			"placeholder source; "+
			"tools in the deployed file beyond the placeholder expansion set must be "+
			"preserved (T4.4); got output tools: %v", "user-extra", items)
	}
}

// TestToolPreservation_PlaceholderSource_FullExpansionStillPresent asserts that when
// user tools are preserved alongside a placeholder expansion, the full expansion result
// (all Universe tools) is still present in the output. Preservation must not displace
// any tools from the expansion.
func TestToolPreservation_PlaceholderSource_FullExpansionStillPresent(t *testing.T) {
	mod := newDescriptorModule(t, placeholderNoExpansionDescriptorYAML, "inline:no-expansion")
	req := transform.Request{
		Source:   []byte(preservationPlaceholderSource),
		Deployed: []byte(deployedPlaceholderWithUserExtra),
		Kind:     domain.ArtifactAgent,
		Key:      "placeholder-full-expansion-agent",
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

	// All Universe tools (alpha, beta, gamma) must be present.
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !toolListContains(doc, "tools", want) {
			items := listFieldScalars(t, doc, "tools")
			t.Errorf("Universe tool %q absent from output tools field; "+
				"placeholder expansion must produce all Universe tools; got: %v", want, items)
		}
	}
}

// ---------------------------------------------------------------------------
// Nil-Deployed (create) case: preservation must be a no-op
// ---------------------------------------------------------------------------

// TestToolPreservation_NilDeployed_NoExtraToolsAppended asserts that when req.Deployed
// is nil (new deployment / create scenario), the mergeDeployedTools step is a no-op:
// only the tools derived from the generic source appear in the output, with no phantom
// tools added. This prevents spurious tool addition on first deploy.
func TestToolPreservation_NilDeployed_NoExtraToolsAppended(t *testing.T) {
	mod := newDescriptorModule(t, preservationListDescriptorYAML, "inline:preservation-list")
	req := transform.Request{
		Source:   []byte(preservationSourceAB),
		Deployed: nil, // create scenario — no deployed file
		Kind:     domain.ArtifactAgent,
		Key:      "create-no-extra-tools-agent",
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

	// Output must contain exactly the source-derived tools (alpha, beta) and no others.
	items := listFieldScalars(t, doc, "tools")
	for _, item := range items {
		if item != "alpha" && item != "beta" {
			t.Errorf("unexpected tool %q in output tools field on create (nil Deployed); "+
				"mergeDeployedTools must be a no-op when req.Deployed is nil; got: %v", item, items)
		}
	}
}
