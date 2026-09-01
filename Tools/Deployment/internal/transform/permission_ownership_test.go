package transform_test

// permission_ownership_test.go covers the tools/permission field user-ownership behavior
// added in Stage 4. On Update (req.Deployed != nil), the tools/permission field from the
// deployed file is preserved byte-exact in the output instead of being re-generated from
// source. On Create (req.Deployed == nil), tools are still computed from source as before.
//
// These tests are in the TDD RED phase: they compile but fail until Stage 4 is implemented
// in applyFrontmatter. Some tests also depend on Stage 1 (docformat flow-mapping tolerance)
// and will fail at the Apply call until Stage 1 is complete.
//
// Coverage (Stage 4 plan clause references):
//   (a) On Update, source-computed tools are NOT written to the output
//   (b) A deployed file with a per-command sub-permission flow-mapping is preserved verbatim
//   (c) A deployed file with standard scalar permissions is preserved verbatim (regression guard)
//   (d) Create (req.Deployed == nil) still computes tools from source (regression guard)
//   (e) User-added tools in the deployed file are preserved (covered by tool_preservation_test.go;
//       verbatim copy achieves the same result as the old mergeDeployedTools for that subset)
//
// NOTE: The existing TestToolPreservation_NewSourceTool_AddedAlongsidePreservedUserTool in
// tool_preservation_test.go tests that a new source tool (delta) is added alongside preserved
// user tools. Stage 4's verbatim-copy behavior contradicts this: on Update, source-computed
// fields are not written, so new source tools are NOT added. That existing test will require
// an update once Stage 4 is implemented. This is a known design trade-off (FR-6: tools are
// user-owned after initial Deploy; new tools only appear on a fresh Deploy).

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Descriptor YAML for permission ownership tests
// ---------------------------------------------------------------------------

// ownershipListDescriptorYAML is a list-shape harness with four Universe tools
// (alpha, beta, gamma, delta) and one generic mapping per tool. Used for tests that
// verify source-computed tools are not written to the output on Update, and that the
// deployed tools field is copied verbatim regardless of what the source would compute.
const ownershipListDescriptorYAML = `schema_version: "1"
id: "ownership-list-harness"
display_name: "Ownership List Harness"
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

// ownershipPermissionDescriptorYAML is a permission-shape harness that stores tools under
// the "permission" frontmatter key. The universe contains one tool ("bash") mapped from
// the generic "run_terminal". Used for tests that exercise the bash sub-permission map
// preservation behavior (plan clauses (b) and (c)).
const ownershipPermissionDescriptorYAML = `schema_version: "1"
id: "ownership-permission-harness"
display_name: "Ownership Permission Harness"
tools:
  shape: permission
  universe:
    - name: "bash"
      unused: deny
      by_convention: false
  mappings:
    - generic: "run_terminal"
      destinations:
        - to: main
          names:
            - "bash"
frontmatter:
  tools_key: "permission"
`

// ---------------------------------------------------------------------------
// Generic source file constants for permission ownership tests
// ---------------------------------------------------------------------------

// ownershipSourceA declares only generic_a, which maps to alpha via ownershipListDescriptorYAML.
const ownershipSourceA = `---
id: 200
version: 1.0.0
description: Ownership test agent (source A)
tools: [generic_a]
---
Body.
`

// ownershipSourceAB declares generic_a and generic_b, which map to alpha and beta.
const ownershipSourceAB = `---
id: 201
version: 1.0.0
description: Ownership test agent (source AB)
tools: [generic_a, generic_b]
---
Body.
`

// ownershipPermissionSource declares run_terminal, which maps to bash: allow via
// ownershipPermissionDescriptorYAML. The source's generic "tools" key drives mapping;
// the output frontmatter key is "permission" (from the descriptor's tools_key).
const ownershipPermissionSource = `---
id: 202
version: 1.0.0
description: Ownership permission test agent
tools: [run_terminal]
---
Body.
`

// ---------------------------------------------------------------------------
// Deployed file constants for permission ownership tests
//
// Deployed files represent the currently-deployed state (req.Deployed). They contain
// the harness-specific rendered form of tools rather than generic names. Their
// frontmatter format mimics what a previous deployment would have produced, including
// user customizations made after the initial deployment.
// ---------------------------------------------------------------------------

// ownershipDeployedGamma is a deployed file whose tools field contains only gamma.
// The user has completely replaced the source-computed tools: the source would compute
// alpha (from generic_a), but the deployed file no longer has alpha at all.
const ownershipDeployedGamma = `---
mosaic_id: 200
mosaic_version: 1.0.0
tools: [gamma]
---
Body.
`

// ownershipDeployedGammaDeltaUserCustom is a deployed file whose tools field contains
// gamma, delta, and a user-added tool "user-custom" that has no generic-source mapping.
// None of these are the source-computed values (alpha, beta). Used to verify that the
// entire deployed set is preserved verbatim — not merged with source-computed values.
const ownershipDeployedGammaDeltaUserCustom = `---
mosaic_id: 201
mosaic_version: 1.0.0
tools: [gamma, delta, user-custom]
---
Body.
`

// ownershipDeployedPermissionBashAllow is a deployed file whose permission field
// contains a standard scalar allow value for bash. This represents the most common
// deployed permission state, produced by the initial Deploy when the user has not
// customized the bash permission.
const ownershipDeployedPermissionBashAllow = `---
mosaic_id: 202
mosaic_version: 1.0.0
permission:
  bash: allow
---
Body.
`

// ownershipDeployedPermissionBashFlowMapping is a deployed file whose permission field
// contains a per-command sub-permission flow-mapping for bash. This is the central
// scenario motivating Stage 1 (docformat flow-mapping tolerance) and Stage 4 (verbatim
// preservation). The user has replaced the simple "allow" with a granular
// `{"*": "deny", "dotnet *": "allow"}` map after initial Deploy.
//
// Parsing this deployed file requires Stage 1 to be implemented in docformat: without
// Stage 1, docformat.Parse fails on the `{...}` flow-mapping value and transform.Apply
// returns an error, making this test fail at the Apply call rather than the assertion.
const ownershipDeployedPermissionBashFlowMapping = `---
mosaic_id: 202
mosaic_version: 1.0.0
permission:
  bash: {"*": "deny", "dotnet *": "allow"}
---
Body.
`

// ---------------------------------------------------------------------------
// Tests — plan clause (a): source-computed tools NOT written on Update
// ---------------------------------------------------------------------------

// TestPermissionOwnership_Update_SourceComputedToolsAbsentFromOutput asserts that on Update
// (req.Deployed != nil), tools computed from the generic source are NOT written to the output.
// The tools/permission field is user-owned after initial Deploy; on Update, only the deployed
// file's value is used. Source computation (Step 5) must be skipped for the tools/permission
// field when req.Deployed is non-nil.
//
// Source maps generic_a -> alpha. Deployed has only [gamma] (user replaced all tools).
// Expected: alpha is NOT in the output. gamma IS in the output (verbatim from deployed).
//
// This test is RED: the current implementation writes alpha via Step 5, then merges gamma
// via Step 5b (mergeDeployedTools). The output is [alpha, gamma] — alpha should not appear.
func TestPermissionOwnership_Update_SourceComputedToolsAbsentFromOutput(t *testing.T) {
	mod := newDescriptorModule(t, ownershipListDescriptorYAML, "inline:ownership-list")
	req := transform.Request{
		Source:   []byte(ownershipSourceA),
		Deployed: []byte(ownershipDeployedGamma),
		Kind:     domain.ArtifactAgent,
		Key:      "ownership-source-computed-absent",
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

	// The source-computed tool "alpha" must NOT appear in the output on Update.
	// On Update, the tools/permission field is user-owned (verbatim from deployed), not
	// re-generated from source. Source computation must be skipped entirely for this field.
	if toolListContains(doc, "tools", "alpha") {
		items := listFieldScalars(t, doc, "tools")
		t.Errorf("source-computed tool %q must not appear in output tools field on Update; "+
			"on Update, the tools/permission field is user-owned — it is copied verbatim from "+
			"the deployed file, not re-generated from source; got output tools: %v",
			"alpha", items)
	}

	// The deployed tool "gamma" must be present (verbatim from deployed file).
	if !toolListContains(doc, "tools", "gamma") {
		items := listFieldScalars(t, doc, "tools")
		t.Errorf("deployed tool %q absent from output tools field on Update; "+
			"the deployed file's tools field must be preserved verbatim; got output tools: %v",
			"gamma", items)
	}
}

// TestPermissionOwnership_Update_DeployedToolsFieldPreservedVerbatim asserts that on Update,
// the entire output tools field is the verbatim value from the deployed file — not a merge
// of source-computed tools and deployed extras.
//
// Source maps generic_a, generic_b -> [alpha, beta]. Deployed has [gamma, delta, user-custom].
// Expected output tools: exactly [gamma, delta, user-custom] (verbatim from deployed), with
// no alpha or beta. The source-computed values must not appear, and no extra tools must be
// added or removed.
//
// This test is RED: the current pipeline (Step 5 sets [alpha, beta], Step 5b merges
// [gamma, delta, user-custom]) produces [alpha, beta, gamma, delta, user-custom] — five items
// instead of the three verbatim items from the deployed file.
func TestPermissionOwnership_Update_DeployedToolsFieldPreservedVerbatim(t *testing.T) {
	mod := newDescriptorModule(t, ownershipListDescriptorYAML, "inline:ownership-list")
	req := transform.Request{
		Source:   []byte(ownershipSourceAB),
		Deployed: []byte(ownershipDeployedGammaDeltaUserCustom),
		Kind:     domain.ArtifactAgent,
		Key:      "ownership-verbatim-preservation",
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

	// Source-computed tools must NOT appear in the output on Update.
	for _, sourceTool := range []string{"alpha", "beta"} {
		if toolListContains(doc, "tools", sourceTool) {
			items := listFieldScalars(t, doc, "tools")
			t.Errorf("source-computed tool %q must not appear in output on Update; "+
				"tools/permission field is user-owned and verbatim from deployed; got: %v",
				sourceTool, items)
		}
	}

	// All deployed tools must be present verbatim in the output.
	for _, deployedTool := range []string{"gamma", "delta", "user-custom"} {
		if !toolListContains(doc, "tools", deployedTool) {
			items := listFieldScalars(t, doc, "tools")
			t.Errorf("deployed tool %q absent from output on Update; "+
				"the deployed file's tools field must be copied verbatim to the output; got: %v",
				deployedTool, items)
		}
	}

	// The output must contain exactly the three deployed tools — no extras from source.
	items := listFieldScalars(t, doc, "tools")
	if len(items) != 3 {
		t.Errorf("output tools field has %d items, want exactly 3 "+
			"(the verbatim deployed set [gamma, delta, user-custom]); "+
			"source-computed tools must not be merged into the output on Update; got: %v",
			len(items), items)
	}
}

// ---------------------------------------------------------------------------
// Tests — plan clause (b): flow-mapping permission value preserved verbatim
// ---------------------------------------------------------------------------

// TestPermissionOwnership_Update_FlowMappingPermission_PreservedVerbatim asserts that on
// Update, when the deployed file's permission.bash contains a per-command sub-permission
// flow-mapping (`bash: {"*": "deny", "dotnet *": "allow"}`), that value is preserved
// byte-exact in the output.
//
// This test depends on Stage 1 (docformat flow-mapping tolerance). Without Stage 1,
// transform.Apply fails with a parse error when it processes the deployed file containing
// `{"*": "deny", "dotnet *": "allow"}` — the test is RED for that reason. After Stage 1
// is implemented but before Stage 4, Apply succeeds but Step 5 overwrites the permission
// field with the source-computed value (bash: allow), destroying the user-customized
// flow-mapping — the test is RED for that reason. Only after both Stage 1 and Stage 4
// are implemented does this test pass.
func TestPermissionOwnership_Update_FlowMappingPermission_PreservedVerbatim(t *testing.T) {
	mod := newDescriptorModule(t, ownershipPermissionDescriptorYAML, "inline:ownership-permission")
	req := transform.Request{
		Source:   []byte(ownershipPermissionSource),
		Deployed: []byte(ownershipDeployedPermissionBashFlowMapping),
		Kind:     domain.ArtifactAgent,
		Key:      "ownership-flow-mapping-permission",
		Module:   mod,
		Model:    domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:    domain.ScopeProject,
	}

	// Apply requires Stage 1 (docformat flow-mapping tolerance) to parse the deployed
	// file's `bash: {"*": "deny", ...}` value without a parse error.
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (requires Stage 1 docformat flow-mapping tolerance for deployed file): %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	permV, ok := doc.Frontmatter().Get("permission")
	if !ok {
		t.Fatal("permission field absent from output frontmatter; " +
			"the permission field from the deployed file must be preserved verbatim on Update")
	}

	if permV.Kind != domain.KindMapping {
		t.Fatalf("permission field kind: want %q (deployed was a mapping with bash sub-key), "+
			"got %q; verbatim preservation must maintain the original field kind",
			domain.KindMapping, permV.Kind)
	}

	// The bash sub-key must be present with the exact opaque scalar value from the deployed file.
	// After Stage 1, `{"*": "deny", "dotnet *": "allow"}` is stored as a KindScalar opaque string.
	const wantBashValue = `{"*": "deny", "dotnet *": "allow"}`
	bashFound := false
	for _, pair := range permV.Pairs {
		if pair.Key == "bash" {
			bashFound = true
			if pair.Value.Kind != domain.KindScalar {
				t.Errorf("permission.bash value kind: want %q (opaque scalar from Stage 1 "+
					"flow-mapping parse), got %q",
					domain.KindScalar, pair.Value.Kind)
			} else if pair.Value.Scalar != wantBashValue {
				t.Errorf("permission.bash value: want %q (byte-exact from deployed file), "+
					"got %q; the user-customized flow-mapping must be preserved verbatim "+
					"on Update — not overwritten by the source-computed %q",
					wantBashValue, pair.Value.Scalar, "allow")
			}
		}
	}
	if !bashFound {
		t.Errorf("permission.bash sub-key absent from output; "+
			"the deployed file's permission mapping must be preserved verbatim; "+
			"permission field pair count: %d", len(permV.Pairs))
	}
}

// ---------------------------------------------------------------------------
// Tests — plan clause (c): standard scalar permission preserved (regression guard)
// ---------------------------------------------------------------------------

// TestPermissionOwnership_Update_ScalarPermission_PreservedVerbatim asserts that on Update,
// when the deployed file's permission.bash contains a standard scalar "allow" value, the
// entire permission mapping is preserved verbatim in the output. This is a regression guard:
// the current implementation already produces this result via Step 5 + Step 5b for the case
// where source and deployed agree on the permission shape. After Stage 4 changes the code
// path to verbatim copy, this test verifies that the new path still correctly preserves
// standard scalar permissions.
func TestPermissionOwnership_Update_ScalarPermission_PreservedVerbatim(t *testing.T) {
	mod := newDescriptorModule(t, ownershipPermissionDescriptorYAML, "inline:ownership-permission")
	req := transform.Request{
		Source:   []byte(ownershipPermissionSource),
		Deployed: []byte(ownershipDeployedPermissionBashAllow),
		Kind:     domain.ArtifactAgent,
		Key:      "ownership-scalar-permission",
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

	permV, ok := doc.Frontmatter().Get("permission")
	if !ok {
		t.Fatal("permission field absent from output frontmatter; " +
			"the permission field from the deployed file must be preserved on Update")
	}

	if permV.Kind != domain.KindMapping {
		t.Fatalf("permission field kind: want %q (deployed was a mapping), got %q",
			domain.KindMapping, permV.Kind)
	}

	// The bash sub-key must be present with its scalar "allow" value.
	bashFound := false
	for _, pair := range permV.Pairs {
		if pair.Key == "bash" {
			bashFound = true
			if pair.Value.Kind != domain.KindScalar {
				t.Errorf("permission.bash value kind: want %q, got %q; "+
					"standard scalar allow permission must be preserved verbatim",
					domain.KindScalar, pair.Value.Kind)
			} else if pair.Value.Scalar != "allow" {
				t.Errorf("permission.bash value: want %q, got %q; "+
					"standard scalar permission value must be preserved verbatim from deployed file",
					"allow", pair.Value.Scalar)
			}
		}
	}
	if !bashFound {
		t.Errorf("permission.bash sub-key absent from output; "+
			"the deployed permission mapping must be preserved verbatim; "+
			"permission field pair count: %d", len(permV.Pairs))
	}
}

// ---------------------------------------------------------------------------
// Tests — plan clause (d): Create (nil Deployed) still computes from source
// ---------------------------------------------------------------------------

// TestPermissionOwnership_Create_ToolsComputedFromSource asserts that when req.Deployed is
// nil (initial Deploy / Create), the tools field is computed from the generic source as
// before. Stage 4's verbatim-preservation only applies on Update; the Create path is
// unchanged. This is a regression guard for the Create flow.
//
// Source maps generic_a -> alpha. Expected: output tools = [alpha] (computed from source).
func TestPermissionOwnership_Create_ToolsComputedFromSource(t *testing.T) {
	mod := newDescriptorModule(t, ownershipListDescriptorYAML, "inline:ownership-list")
	req := transform.Request{
		Source:   []byte(ownershipSourceA),
		Deployed: nil, // Create — no previously-deployed file
		Kind:     domain.ArtifactAgent,
		Key:      "ownership-create-from-source",
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

	// On Create, the source-computed tool "alpha" must be present in the output.
	if !toolListContains(doc, "tools", "alpha") {
		items := listFieldScalars(t, doc, "tools")
		t.Errorf("source-computed tool %q absent from output tools field on Create "+
			"(req.Deployed == nil); when there is no deployed file, tools must be computed "+
			"from source as before — Stage 4 must not affect the Create path; "+
			"got output tools: %v", "alpha", items)
	}

	// On Create, no phantom tools should appear (no user-deployed file to pull tools from).
	items := listFieldScalars(t, doc, "tools")
	for _, item := range items {
		if item != "alpha" {
			t.Errorf("unexpected tool %q in output tools field on Create (req.Deployed == nil); "+
				"only source-computed tools should appear; got: %v", item, items)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests — deployed-parse-failure fallback inside applyFrontmatter
// ---------------------------------------------------------------------------

// ownershipDeployedMalformed is a deployed file whose frontmatter contains an unparseable
// line — a line with no colon in a key:value context. docformat.Parse fails on this content.
// Used to exercise the fallback path in applyFrontmatter where docformat.Parse(req.Deployed)
// returns an error: the output must still have a non-empty tools/permission key, set to the
// source-computed value (same as Create path).
const ownershipDeployedMalformed = `---
mosaic_id: 200
@invalid line without colon
---
Body.
`

// TestPermissionOwnership_Update_DeployedParseFailure_FallsThrough asserts that when
// req.Deployed contains bytes that docformat.Parse cannot parse, transform.Apply still
// succeeds (does not return an error) and the output frontmatter's tools key is set to
// the source-computed value — not left absent.
//
// This exercises the defensive fallback in applyFrontmatter: when docformat.Parse fails
// on the deployed file inside applyFrontmatter, the code falls through to the Create path
// (Steps 5/5b), setting the tools/permission key to the freshly-computed catalog-derived
// value rather than leaving it absent.
//
// This test is RED until Stage 4 handles this fallback path. Currently, the malformed
// deployed file causes the pipeline to return a hard error before reaching applyFrontmatter
// (buildDeployedRegionMap fails on parse), so Apply returns an error at the first assertion.
func TestPermissionOwnership_Update_DeployedParseFailure_FallsThrough(t *testing.T) {
	mod := newDescriptorModule(t, ownershipListDescriptorYAML, "inline:ownership-list")
	req := transform.Request{
		Source:   []byte(ownershipSourceA),
		Deployed: []byte(ownershipDeployedMalformed),
		Kind:     domain.ArtifactAgent,
		Key:      "ownership-parse-failure-fallback",
		Module:   mod,
		Model:    domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:    domain.ScopeProject,
	}

	// Apply must not return an error even when the deployed file cannot be parsed.
	// The pipeline must fall through to the Create path inside applyFrontmatter and
	// compute tools from source rather than aborting with a parse error.
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply must not return an error when the deployed file is unparseable; "+
			"the fallback path in applyFrontmatter must recover by computing tools from source; "+
			"got error: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	// The tools key must be set in the output — not absent. The fallback computes tools
	// from source (same as Create path), so the source-computed "alpha" must appear.
	if !toolListContains(doc, "tools", "alpha") {
		items := listFieldScalars(t, doc, "tools")
		t.Errorf("source-computed tool %q absent from output tools field after deployed-parse-failure; "+
			"when docformat.Parse(req.Deployed) fails in applyFrontmatter, the output must receive "+
			"freshly-computed tools from source (Create path fallback) — the key must not be absent; "+
			"got output tools: %v", "alpha", items)
	}
}

// ---------------------------------------------------------------------------
// Tests — deployed file with no tools/permission key (minor coverage)
// ---------------------------------------------------------------------------

// ownershipDeployedNoToolsKey is a deployed file whose frontmatter does not contain
// a tools key at all. This represents an older-format deployed file predating the tools
// field, or a harness that does not emit the tools key on initial Deploy. docformat.Parse
// succeeds, but deployedFM.Get("tools") returns ok=false.
const ownershipDeployedNoToolsKey = `---
mosaic_id: 200
mosaic_version: 1.0.0
---
Body.
`

// TestPermissionOwnership_Update_DeployedNoToolsKey_FreshlyComputed asserts that when
// the deployed file has no tools/permission key in its frontmatter, the output receives
// freshly-computed source tools (same as Create path). This covers the guard condition
// inside the verbatim-copy block: when deployedFM.Get(desc.Frontmatter.ToolsKey) returns
// ok=false, the verbatim copy is skipped and the code falls through to compute tools from
// source, ensuring the key is never absent from the output.
//
// Source maps generic_a -> alpha. Deployed has no tools key.
// Expected output: tools = [alpha] (freshly computed from source).
func TestPermissionOwnership_Update_DeployedNoToolsKey_FreshlyComputed(t *testing.T) {
	mod := newDescriptorModule(t, ownershipListDescriptorYAML, "inline:ownership-list")
	req := transform.Request{
		Source:   []byte(ownershipSourceA),
		Deployed: []byte(ownershipDeployedNoToolsKey),
		Kind:     domain.ArtifactAgent,
		Key:      "ownership-no-tools-key",
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

	// The source-computed tool "alpha" must appear: when the deployed file lacks the tools
	// key, the verbatim copy is skipped and tools are freshly computed from source.
	if !toolListContains(doc, "tools", "alpha") {
		items := listFieldScalars(t, doc, "tools")
		t.Errorf("source-computed tool %q absent from output tools field on Update when "+
			"deployed file has no tools key; when the deployed file's tools key is absent, "+
			"the output must receive freshly-computed tools from source — same as Create path; "+
			"got output tools: %v", "alpha", items)
	}
}

// ---------------------------------------------------------------------------
// Tests — descriptor with no tools key (ToolsKey == "") on Update (minor coverage)
// ---------------------------------------------------------------------------

// ownershipNoToolsKeyDescriptorYAML is a list-shape harness that declares no frontmatter
// tools_key. This exercises the guard condition `desc.Frontmatter.ToolsKey != ""` in the
// verbatim-copy block: when ToolsKey is empty, the verbatim-copy block is skipped entirely,
// and the Create path handles tool field generation (which also produces nothing, since there
// is no output key to write to).
const ownershipNoToolsKeyDescriptorYAML = `schema_version: "1"
id: "ownership-no-toolskey-harness"
display_name: "Ownership No ToolsKey Harness"
tools:
  shape: list
  universe:
    - name: "alpha"
      unused: deny
      by_convention: false
  mappings:
    - generic: "generic_a"
      destinations:
        - to: main
          names:
            - "alpha"
frontmatter:
  tools_key: ""
`

// TestPermissionOwnership_Update_NoDescriptorToolsKey_NoOp asserts that on Update, when
// the descriptor declares no frontmatter tools_key (ToolsKey == ""), Apply succeeds without
// error or panic. The verbatim-copy block is skipped (guard condition ToolsKey != "" is
// false) and no attempt is made to Get or Set a field with an empty key. This verifies
// that removing the guard would not silently introduce a panic or undefined behavior.
func TestPermissionOwnership_Update_NoDescriptorToolsKey_NoOp(t *testing.T) {
	mod := newDescriptorModule(t, ownershipNoToolsKeyDescriptorYAML, "inline:ownership-no-toolskey")
	req := transform.Request{
		Source:   []byte(ownershipSourceA),
		Deployed: []byte(ownershipDeployedGamma),
		Kind:     domain.ArtifactAgent,
		Key:      "ownership-no-toolskey-agent",
		Module:   mod,
		Model:    domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:    domain.ScopeProject,
	}

	// Apply must not panic or return an error when the descriptor has no tools_key.
	// The verbatim-copy guard (ToolsKey != "") prevents any key manipulation on empty ToolsKey.
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply must not return an error when descriptor has no tools_key; "+
			"the guard condition (ToolsKey != \"\") must prevent any tools field manipulation; "+
			"got error: %v", err)
	}

	// The output must be parseable.
	_, err = docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}
	// No assertion on specific tools field content: when ToolsKey is empty, no tools field
	// is written to the output frontmatter. The test verifies absence of error/panic only.
}
