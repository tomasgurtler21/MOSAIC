package descriptor_test

// Registry-wide tests for MinimalGenericToolSet and RenderMinimalToolGrant.
//
// These tests iterate over ALL FIVE registered built-in harnesses, so each must
// blank-import the five builtin harness packages to trigger their init() functions.
// The import is legitimate: the harness-isolation check in tools/importcheck/main.go
// exempts _test.go files from the harness-isolation rule.
//
// The Codex harness reads Catalog/HarnessInjections/Codex/ at construction time, which
// does not yet exist in the live repository (that directory is created by the catalog
// stage). Therefore every test that resolves the codex module uses frozenCatalogRoot()
// rather than the live repo root.
//
// TDD RED: every test in this file references descriptor.MinimalGenericToolSet,
// descriptor.RenderMinimalToolGrant and descriptor.MinimalGrant, none of which exist
// until I14.1-I14.2 are implemented. Tests also reference the five harness init()
// functions and registry.Discover, which do exist, so the compile failure is limited
// to the descriptor symbols.

import (
	"errors"
	"path/filepath"
	"testing"

	// Side-effect imports ensure all five built-in harness init() functions run,
	// registering their factories with the package-level registry before Discover runs.
	_ "mosaic-deploy/internal/harness/builtin/claudecode"
	_ "mosaic-deploy/internal/harness/builtin/codex"
	_ "mosaic-deploy/internal/harness/builtin/ghcpcli"
	_ "mosaic-deploy/internal/harness/builtin/opencode"
	_ "mosaic-deploy/internal/harness/builtin/vscodeghcp"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/harness/registry"
)

// ---------------------------------------------------------------------------
// Registry discovery helpers
// ---------------------------------------------------------------------------

// repoRootFromDescriptor returns the absolute path to the repository root,
// navigating up from this package's directory.
// Package is at Tools/Deployment/internal/harness/descriptor/ -- 5 levels below repo root.
func repoRootFromDescriptor(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "..", "..", "..")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return abs
}

// frozenCatalogRootForDescriptor returns the absolute path to the frozen Catalog
// fixture tree at Tools/Deployment/testdata/frozen-catalog. The frozen catalog
// carries Catalog/HarnessInjections/Codex/ so the Codex module initialises with
// Usable:true, unlike the live repo root which does not yet have that directory.
func frozenCatalogRootForDescriptor(t *testing.T) string {
	t.Helper()
	root := repoRootFromDescriptor(t)
	return filepath.Join(root, "Tools", "Deployment", "testdata", "frozen-catalog")
}

// discoverAllBuiltins discovers all five registered built-in harnesses from the
// frozen catalog root. No ToolMappings hook is installed so the descriptors' own
// declared mappings are used, which is the correct baseline for rendering tests.
func discoverAllBuiltins(t *testing.T) registry.Registry {
	t.Helper()
	root := frozenCatalogRootForDescriptor(t)
	reg, err := registry.Discover(registry.Options{
		MosaicRoot: root,
	})
	if err != nil {
		t.Fatalf("Discover (frozen catalog, no hook): %v", err)
	}
	return reg
}

// resolveModule resolves one harness by ID, failing the test if it is not found or
// not usable.
func resolveModule(t *testing.T, reg registry.Registry, id string) domain.HarnessModule {
	t.Helper()
	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	return m
}

// ---------------------------------------------------------------------------
// T14.1(d): every MinimalGenericToolSet member has a mapping in tools-key harnesses
// ---------------------------------------------------------------------------

// TestMinimalGenericToolSet_AllMembersHaveMappingsInToolsKeyHarnesses verifies that
// for every registered harness that declares a tools key, each member of
// MinimalGenericToolSet resolves to at least one harness-side tool name. If any
// member were unmapped, the minimal grant would be silently empty for that harness,
// defeating the whole point of this stage.
//
// The harness list is derived from the registry, not from a literal, so a new
// harness added later is covered automatically.
func TestMinimalGenericToolSet_AllMembersHaveMappingsInToolsKeyHarnesses(t *testing.T) {
	reg := discoverAllBuiltins(t)

	for _, ref := range reg.List() {
		if !ref.Usable {
			continue
		}
		m := resolveModule(t, reg, ref.ID)
		d := m.Descriptor()
		if d.Frontmatter.ToolsKey == "" {
			// No tools key: the harness states capability through another field.
			// MinimalGenericToolSet mappability is not required here.
			continue
		}

		t.Run(ref.ID, func(t *testing.T) {
			for _, generic := range descriptor.MinimalGenericToolSet {
				// Look for a mapping with at least one DestMain destination having names.
				var found bool
				for _, mapping := range d.Tools.Mappings {
					if mapping.Generic != generic {
						continue
					}
					for _, dest := range mapping.Destinations {
						if dest.Kind == domain.DestMain && len(dest.Names) > 0 {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if !found {
					t.Errorf("harness %q declares tools_key %q but has no DestMain mapping with at "+
						"least one name for generic tool %q; the minimal grant would be silently "+
						"empty for this harness", ref.ID, d.Frontmatter.ToolsKey, generic)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T14.2: per-harness rendering tests
// ---------------------------------------------------------------------------

// TestRenderMinimalToolGrant_ClaudeCode asserts that the Claude Code module, when
// rendered through RenderMinimalToolGrant, emits the tools value as a KindScalar
// with exactly "Read, Glob, Grep".
//
// Claude Code declares shape: list and emits a comma-separated scalar because its
// module's Tools() applies convertFieldsToScalar after calling MapTools. An entry
// point that bypasses the module and calls MapTools directly would emit a YAML list
// instead, which Claude Code reads as unrestricted. The exact scalar is the proof
// that the call goes through the module.
func TestRenderMinimalToolGrant_ClaudeCode(t *testing.T) {
	reg := discoverAllBuiltins(t)
	m := resolveModule(t, reg, "claude-code")

	grant, err := descriptor.RenderMinimalToolGrant(m, "test-agent")
	if err != nil {
		t.Fatalf("RenderMinimalToolGrant(claude-code): %v", err)
	}

	if !grant.ToolsKeyDeclared {
		t.Error("claude-code: ToolsKeyDeclared=false; want true (descriptor declares tools_key)")
	}

	toolsKey := m.Descriptor().Frontmatter.ToolsKey // "tools"
	var toolsField *domain.FrontmatterField
	for i := range grant.Fields {
		if grant.Fields[i].Key == toolsKey {
			toolsField = &grant.Fields[i]
			break
		}
	}
	if toolsField == nil {
		t.Fatalf("claude-code: declared tools key %q absent from grant Fields; want it present", toolsKey)
	}

	// Claude Code's module post-processes the list to a scalar.
	if toolsField.Value.Kind != domain.KindScalar {
		t.Errorf("claude-code: tools field value Kind=%v; want KindScalar; "+
			"the module converts the list to a comma-separated scalar and the entry "+
			"point must call the module, not the shared mapper directly", toolsField.Value.Kind)
	}

	wantScalar := "Read, Glob, Grep"
	if toolsField.Value.Scalar != wantScalar {
		t.Errorf("claude-code: tools scalar=%q; want %q; "+
			"the exact scalar value proves file_read->Read, file_search->Glob, "+
			"content_search->Grep in universe order", toolsField.Value.Scalar, wantScalar)
	}
}

// TestRenderMinimalToolGrant_GHCPCli asserts that the GHCP CLI module emits a
// KindList tools value containing exactly "read" and "search".
//
// GHCP CLI has no by-convention universe entries. file_read maps to "read" and both
// file_search and content_search map to "search" (many-to-one, deduplicated).
func TestRenderMinimalToolGrant_GHCPCli(t *testing.T) {
	reg := discoverAllBuiltins(t)
	m := resolveModule(t, reg, "ghcp-cli")

	grant, err := descriptor.RenderMinimalToolGrant(m, "test-agent")
	if err != nil {
		t.Fatalf("RenderMinimalToolGrant(ghcp-cli): %v", err)
	}

	if !grant.ToolsKeyDeclared {
		t.Error("ghcp-cli: ToolsKeyDeclared=false; want true")
	}

	toolsKey := m.Descriptor().Frontmatter.ToolsKey // "tools"
	var toolsField *domain.FrontmatterField
	for i := range grant.Fields {
		if grant.Fields[i].Key == toolsKey {
			toolsField = &grant.Fields[i]
			break
		}
	}
	if toolsField == nil {
		t.Fatalf("ghcp-cli: declared tools key %q absent from grant Fields", toolsKey)
	}

	if toolsField.Value.Kind != domain.KindList {
		t.Errorf("ghcp-cli: tools field Kind=%v; want KindList", toolsField.Value.Kind)
	}

	wantItems := []string{"read", "search"}
	gotItems := extractScalars(t, toolsField.Value.Items)

	if !stringSlicesEqual(gotItems, wantItems) {
		t.Errorf("ghcp-cli: tools list=%v; want %v; "+
			"file_read->read (universe index 0), file_search and content_search both->search "+
			"(universe index 2, deduplicated); order determined by buildListToolFields "+
			"universe-position sort: read at index 0, search at index 2 in ghcp-cli.yaml; "+
			"no by-convention entries for this descriptor; if the order changes, the "+
			"universe was reordered and this assertion must be updated to match", gotItems, wantItems)
	}
}

// TestRenderMinimalToolGrant_VSCodeGHCP asserts that the VS Code GHCP module emits a
// KindList tools value containing the three mapped names plus its by-convention entry.
//
// VS Code GHCP has one by_convention: true universe tool. The plan states that the
// by-convention entry must be read from the descriptor rather than named in the test
// body, so this test reads it dynamically.
func TestRenderMinimalToolGrant_VSCodeGHCP(t *testing.T) {
	reg := discoverAllBuiltins(t)
	m := resolveModule(t, reg, "vscode-ghcp")

	grant, err := descriptor.RenderMinimalToolGrant(m, "test-agent")
	if err != nil {
		t.Fatalf("RenderMinimalToolGrant(vscode-ghcp): %v", err)
	}

	if !grant.ToolsKeyDeclared {
		t.Error("vscode-ghcp: ToolsKeyDeclared=false; want true")
	}

	toolsKey := m.Descriptor().Frontmatter.ToolsKey // "tools"
	var toolsField *domain.FrontmatterField
	for i := range grant.Fields {
		if grant.Fields[i].Key == toolsKey {
			toolsField = &grant.Fields[i]
			break
		}
	}
	if toolsField == nil {
		t.Fatalf("vscode-ghcp: declared tools key %q absent from grant Fields", toolsKey)
	}

	if toolsField.Value.Kind != domain.KindList {
		t.Errorf("vscode-ghcp: tools field Kind=%v; want KindList", toolsField.Value.Kind)
	}

	// Collect by-convention names from the descriptor so the test survives a name change.
	byConventionNames := byConventionToolNames(m.Descriptor())
	if len(byConventionNames) == 0 {
		t.Fatal("vscode-ghcp descriptor has no by_convention entries; " +
			"expected at least one (the test verifies it is included in the minimal grant)")
	}

	// The expected list: file_read->read/readFile, file_search->search/fileSearch,
	// content_search->search/textSearch, plus all by-convention entries.
	// We verify this by checking each expected name is present and the total count matches.
	wantMapped := []string{"read/readFile", "search/fileSearch", "search/textSearch"}
	gotItems := extractScalars(t, toolsField.Value.Items)

	for _, name := range wantMapped {
		if !stringSliceContains(gotItems, name) {
			t.Errorf("vscode-ghcp: tools list missing expected mapped name %q; got %v", name, gotItems)
		}
	}
	for _, name := range byConventionNames {
		if !stringSliceContains(gotItems, name) {
			t.Errorf("vscode-ghcp: tools list missing by-convention name %q; got %v; "+
				"by-convention entries must always be emitted regardless of generic tools", name, gotItems)
		}
	}

	wantCount := len(wantMapped) + len(byConventionNames)
	// Deduplicate: if a mapped name is also a by-convention name, count it once.
	seen := make(map[string]bool, wantCount)
	var wantUnique []string
	for _, n := range wantMapped {
		if !seen[n] {
			seen[n] = true
			wantUnique = append(wantUnique, n)
		}
	}
	for _, n := range byConventionNames {
		if !seen[n] {
			seen[n] = true
			wantUnique = append(wantUnique, n)
		}
	}
	if len(gotItems) != len(wantUnique) {
		t.Errorf("vscode-ghcp: tools list length=%d; want %d; "+
			"expected %v, got %v", len(gotItems), len(wantUnique), wantUnique, gotItems)
	}
}

// TestRenderMinimalToolGrant_OpenCode asserts that the OpenCode module emits a
// KindMapping permission value that grants allow to the three mapped generics and to
// the by-convention tool, and assigns every other universe tool its declared unused
// disposition (deny for all OpenCode universe tools).
//
// The full permission mapping is asserted because the security property is what is
// denied, not only what is allowed.
func TestRenderMinimalToolGrant_OpenCode(t *testing.T) {
	reg := discoverAllBuiltins(t)
	m := resolveModule(t, reg, "opencode")

	grant, err := descriptor.RenderMinimalToolGrant(m, "test-agent")
	if err != nil {
		t.Fatalf("RenderMinimalToolGrant(opencode): %v", err)
	}

	if !grant.ToolsKeyDeclared {
		t.Error("opencode: ToolsKeyDeclared=false; want true")
	}

	toolsKey := m.Descriptor().Frontmatter.ToolsKey // "permission"
	var permField *domain.FrontmatterField
	for i := range grant.Fields {
		if grant.Fields[i].Key == toolsKey {
			permField = &grant.Fields[i]
			break
		}
	}
	if permField == nil {
		t.Fatalf("opencode: declared tools key %q absent from grant Fields", toolsKey)
	}

	if permField.Value.Kind != domain.KindMapping {
		t.Errorf("opencode: permission field Kind=%v; want KindMapping", permField.Value.Kind)
	}

	// Build the expected disposition per harness tool.
	// From opencode.yaml:
	//   file_read -> "read" (allow)
	//   file_search -> "glob" (allow)
	//   content_search -> "grep" (allow)
	//   by_convention: "list" (allow)
	//   every other universe tool -> their declared unused (deny for all)
	desc := m.Descriptor()
	allowedByMinimalSet := minimalGrantAllowSet(desc)
	allowedByConvention := byConventionToolNames(desc)
	for _, name := range allowedByConvention {
		allowedByMinimalSet[name] = true
	}

	// Build a map from harness tool name to actual emitted disposition.
	actual := make(map[string]string, len(permField.Value.Pairs))
	for _, pair := range permField.Value.Pairs {
		actual[pair.Key] = pair.Value.Scalar
	}

	for _, tool := range desc.Tools.Universe {
		gotDisp, present := actual[tool.Name]
		if !present {
			t.Errorf("opencode: permission map missing entry for universe tool %q; "+
				"the full mapping must be emitted for the permission shape", tool.Name)
			continue
		}
		if allowedByMinimalSet[tool.Name] {
			if gotDisp != string(domain.Allow) {
				t.Errorf("opencode: tool %q has disposition %q; want %q "+
					"(it is in the minimal grant or by-convention)", tool.Name, gotDisp, domain.Allow)
			}
		} else {
			wantDisp := string(tool.Unused)
			if gotDisp != wantDisp {
				t.Errorf("opencode: tool %q has disposition %q; want %q (its declared unused disposition)",
					tool.Name, gotDisp, wantDisp)
			}
		}
	}
}

// TestRenderMinimalToolGrant_Codex asserts that the Codex module emits exactly one
// field, which is sandbox_mode, and that no field carries a key that any registered
// harness declares as its tools key.
//
// Codex declares no tools key; capability is expressed via sandbox_mode. The entry
// point must set ToolsKeyDeclared=false for Codex and must not refuse the result
// because no tools key is expected.
//
// The value of sandbox_mode is NOT asserted here. That assertion belongs in the Codex
// module's own package test (Stage 5's T5.2), which is the only permitted test-side
// occurrence of the "read-only" literal outside the module.
func TestRenderMinimalToolGrant_Codex(t *testing.T) {
	reg := discoverAllBuiltins(t)
	m := resolveModule(t, reg, "codex")

	grant, err := descriptor.RenderMinimalToolGrant(m, "test-agent")
	if err != nil {
		t.Fatalf("RenderMinimalToolGrant(codex): %v", err)
	}

	// Codex declares no tools key.
	if grant.ToolsKeyDeclared {
		t.Error("codex: ToolsKeyDeclared=true; want false (codex declares no tools key)")
	}

	// The result must carry exactly one field: sandbox_mode.
	if len(grant.Fields) != 1 {
		t.Errorf("codex: len(Fields)=%d; want 1 (exactly sandbox_mode); "+
			"a literal empty-Fields result would be wrong: codex.Tools() always emits sandbox_mode",
			len(grant.Fields))
	}

	if len(grant.Fields) >= 1 {
		if grant.Fields[0].Key != "sandbox_mode" {
			t.Errorf("codex: Fields[0].Key=%q; want %q", grant.Fields[0].Key, "sandbox_mode")
		}
	}

	// Assert no field carries a key that any registered harness declares as its tools key.
	// Collect the tools keys of all registered harnesses.
	registeredToolsKeys := collectRegisteredToolsKeys(t, reg)

	for _, f := range grant.Fields {
		if registeredToolsKeys[f.Key] {
			t.Errorf("codex: grant Fields contains a field with key %q, which is the "+
				"declared tools key of a registered harness; codex must not emit any tools-shaped field",
				f.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// T14.4: always-emitted tools field invariant tests
// ---------------------------------------------------------------------------

// TestAlwaysEmittedToolsField_AllRegisteredHarnesses verifies that for every
// registered harness declaring a tools key, calling Tools() with an empty generic
// set returns a result that contains the declared tools key.
//
// This is the direct module-level check corresponding to contracttest placement 2.
// The contracttest suite enforces this as a universal invariant; this test verifies
// it holds for the five registered harnesses with no harness-isolation issue.
func TestAlwaysEmittedToolsField_AllRegisteredHarnesses(t *testing.T) {
	reg := discoverAllBuiltins(t)

	for _, ref := range reg.List() {
		if !ref.Usable {
			continue
		}
		m := resolveModule(t, reg, ref.ID)
		d := m.Descriptor()
		toolsKey := d.Frontmatter.ToolsKey
		if toolsKey == "" {
			// No tools key: checked separately.
			continue
		}

		t.Run(ref.ID, func(t *testing.T) {
			result, err := m.Tools(domain.ToolRequest{
				AgentKey: "invariant-check",
				Generic:  []string{},
			})
			if err != nil {
				t.Fatalf("Tools with empty generic: %v", err)
			}

			found := false
			for _, f := range result.Fields {
				if f.Key == toolsKey {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("harness %q: declared tools key %q absent from Tools() result for empty "+
					"generic set; a harness declaring a tools key must always emit that field so "+
					"that 'no tools' cannot degrade into 'inherit all'", ref.ID, toolsKey)
			}
		})
	}
}

// TestAlwaysEmittedToolsField_NoToolsKeyHarnessEmitsCapability verifies that for
// registered harnesses with no declared tools key (Codex), Tools() with an empty
// generic set returns a non-empty Fields slice. Capability must be stated through
// some field even when there is no tools key.
func TestAlwaysEmittedToolsField_NoToolsKeyHarnessEmitsCapability(t *testing.T) {
	reg := discoverAllBuiltins(t)

	for _, ref := range reg.List() {
		if !ref.Usable {
			continue
		}
		m := resolveModule(t, reg, ref.ID)
		d := m.Descriptor()
		if d.Frontmatter.ToolsKey != "" {
			// Has a tools key: covered by TestAlwaysEmittedToolsField_AllRegisteredHarnesses.
			continue
		}

		t.Run(ref.ID, func(t *testing.T) {
			result, err := m.Tools(domain.ToolRequest{
				AgentKey: "invariant-check",
				Generic:  []string{},
			})
			if err != nil {
				t.Fatalf("Tools with empty generic: %v", err)
			}

			if len(result.Fields) == 0 {
				t.Errorf("harness %q (no declared tools key): Tools() returned empty Fields for "+
					"empty generic set; a harness with no tools key must emit at least one field "+
					"stating capability (e.g. sandbox_mode)", ref.ID)
			}
		})
	}
}

// TestAlwaysEmittedToolsField_NegativeCase_DropsToolsField proves that the
// placement-1 guard catches a module whose Tools() override drops the declared
// tools field.
//
// The negative case is built from a stub wrapping the claude-code module and dropping
// the "tools" field from its result. This is the real bypass path: buildListToolFields
// already always emits the field through the descriptor path, so a negative case aimed
// only at the descriptor would prove nothing. The bypass arrives through a module
// override that suppresses the field post-descriptor.
//
// This test asserts that RenderMinimalToolGrant returns ErrMinimalGrantFieldAbsent
// for such a stub, proving the placement-1 guard fires after the module call.
func TestAlwaysEmittedToolsField_NegativeCase_DropsToolsField(t *testing.T) {
	reg := discoverAllBuiltins(t)
	inner := resolveModule(t, reg, "claude-code")

	stub := newStub(
		inner.Descriptor(),
		func(req domain.ToolRequest) (domain.ToolResult, error) {
			result, err := inner.Tools(req)
			if err != nil {
				return domain.ToolResult{}, err
			}
			// Drop the declared tools key from the result.
			toolsKey := inner.Descriptor().Frontmatter.ToolsKey
			filtered := result.Fields[:0]
			for _, f := range result.Fields {
				if f.Key != toolsKey {
					filtered = append(filtered, f)
				}
			}
			result.Fields = filtered
			return result, nil
		},
	)

	_, err := descriptor.RenderMinimalToolGrant(stub, "test-agent")
	if err == nil {
		t.Fatal("RenderMinimalToolGrant: got nil error for a stub that drops the tools field; " +
			"want ErrMinimalGrantFieldAbsent; the placement-1 guard must fire after the module " +
			"call and catch a post-processing override that removes the field")
	}
	if !errors.Is(err, descriptor.ErrMinimalGrantFieldAbsent) {
		t.Errorf("RenderMinimalToolGrant: got %v; want errors.Is(err, ErrMinimalGrantFieldAbsent)",
			err)
	}
}

// TestAlwaysEmittedToolsField_NegativeCase_NoToolsKeyEmptyFields proves that a stub
// with no declared tools key and empty Fields PASSES placement 1 (RenderMinimalToolGrant
// does not error), but the same stub WOULD fail the contracttest placement-2 invariant.
//
// This test is the assertion that proves the two placements are distinct and that
// narrowing placement 1 to tools-key-only does not leave a real hole: the contracttest
// invariant covers the no-tools-key case that placement 1 deliberately ignores.
//
// The distinction is verifiable here: placement 1 succeeds and the stub's Fields are
// empty, which is exactly the condition contracttest placement 2 would catch.
func TestAlwaysEmittedToolsField_NegativeCase_NoToolsKeyEmptyFields(t *testing.T) {
	// A stub with no declared tools key that returns empty Fields.
	stub := newStub(
		noToolsKeyDescriptor(),
		func(req domain.ToolRequest) (domain.ToolResult, error) {
			resolutions := make([]domain.ToolResolution, len(req.Generic))
			for i, g := range req.Generic {
				resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
			}
			return domain.ToolResult{Fields: nil, Resolutions: resolutions}, nil
		},
	)

	// Placement 1 must NOT fire for a no-tools-key stub with empty Fields.
	grant, err := descriptor.RenderMinimalToolGrant(stub, "test-agent")
	if err != nil {
		t.Errorf("RenderMinimalToolGrant: got error %v for a no-tools-key stub with empty Fields; "+
			"placement 1 must not refuse the no-tools-key case -- that is placement 2's job", err)
	}

	// The result has ToolsKeyDeclared=false and empty Fields, which is the violation
	// condition for the contracttest invariant (placement 2). Verify the state so the
	// test documents what placement 2 must catch.
	if grant.ToolsKeyDeclared {
		t.Error("stub with no tools key: ToolsKeyDeclared=true; want false")
	}
	if len(grant.Fields) != 0 {
		t.Errorf("stub returned %d fields; test expects empty Fields to verify placement-2 scope",
			len(grant.Fields))
	}
}

// TestAlwaysEmittedToolsField_NegativeCase_Placement2FiresForNoToolsKeyEmptyFields
// proves that the contracttest placement-2 invariant fires for a no-tools-key stub
// that returns empty Fields. This is the executable proof that is absent from
// TestAlwaysEmittedToolsField_NegativeCase_NoToolsKeyEmptyFields: that test documents
// the violation state but does not prove that the invariant check on that state
// would actually fail.
//
// Approach: inline the placement-2 invariant condition (the no-tools-key branch of
// contracttest.Tools_always_emits_capability_field_for_empty_generic) and assert the
// condition holds for the stub. The condition is: descriptor declares no tools key AND
// Tools() returns empty Fields for an empty generic set. Both are true of this stub,
// which proves the invariant would call t.Errorf for it.
//
// If the stub implementation changes such that it emits non-empty Fields, this test
// fails, requiring re-evaluation of whether placement-2 still covers the case.
func TestAlwaysEmittedToolsField_NegativeCase_Placement2FiresForNoToolsKeyEmptyFields(t *testing.T) {
	stub := newStub(
		noToolsKeyDescriptor(),
		func(req domain.ToolRequest) (domain.ToolResult, error) {
			resolutions := make([]domain.ToolResolution, len(req.Generic))
			for i, g := range req.Generic {
				resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
			}
			return domain.ToolResult{Fields: nil, Resolutions: resolutions}, nil
		},
	)

	// Call Tools directly, as the contracttest invariant does, with an empty generic set.
	result, err := stub.Tools(domain.ToolRequest{
		AgentKey: "placement-2-proof",
		Generic:  []string{},
	})
	if err != nil {
		t.Fatalf("stub.Tools: %v", err)
	}

	// Placement-2 invariant precondition: the descriptor must declare no tools key.
	desc := stub.Descriptor()
	if desc.Frontmatter.ToolsKey != "" {
		t.Fatalf("stub has tools key %q; placement-2 proof requires a no-tools-key stub",
			desc.Frontmatter.ToolsKey)
	}

	// Placement-2 invariant condition (no-tools-key branch, inlined from contracttest.go):
	//   len(result.Fields) == 0  =>  invariant fires (calls t.Errorf)
	//
	// Assert the condition holds. If this assertion fails, the stub emitted non-empty
	// Fields, which means either the stub is wrong or placement-2 would no longer fire
	// for this stub, and the coverage gap reasoning must be re-evaluated.
	if len(result.Fields) != 0 {
		t.Errorf("stub returned %d Fields for empty generic set; placement-2 proof requires "+
			"empty Fields (the violation condition the invariant catches); "+
			"if Fields is non-empty the invariant would NOT fire and the test is wrong",
			len(result.Fields))
		return
	}
	// Both invariant conditions are met: no declared tools key and empty Fields.
	// contracttest.Tools_always_emits_capability_field_for_empty_generic would call
	// t.Errorf for this module. This is the placement-2 guard that placement-1
	// (RenderMinimalToolGrant) deliberately omits for the no-tools-key case.
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// extractScalars extracts the Scalar string from each FieldValue in items.
// Fails the test if any item is not KindScalar.
func extractScalars(t *testing.T, items []domain.FieldValue) []string {
	t.Helper()
	out := make([]string, 0, len(items))
	for _, v := range items {
		if v.Kind != domain.KindScalar {
			t.Errorf("expected KindScalar list item, got Kind=%v", v.Kind)
			continue
		}
		out = append(out, v.Scalar)
	}
	return out
}

// stringSlicesEqual reports whether a and b are equal element-by-element.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// stringSliceContains reports whether s is in the slice.
func stringSliceContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// byConventionToolNames returns the names of all universe tools in the descriptor
// that have ByConvention == true.
func byConventionToolNames(d *domain.HarnessDescriptor) []string {
	var names []string
	for _, t := range d.Tools.Universe {
		if t.ByConvention {
			names = append(names, t.Name)
		}
	}
	return names
}

// minimalGrantAllowSet builds a map of harness-side tool names that the minimal
// generic set resolves to for this descriptor's DestMain mappings. These are the
// tools that should receive "allow" in a permission-shaped grant.
func minimalGrantAllowSet(d *domain.HarnessDescriptor) map[string]bool {
	allowed := make(map[string]bool)
	minimalSet := make(map[string]bool, len(descriptor.MinimalGenericToolSet))
	for _, g := range descriptor.MinimalGenericToolSet {
		minimalSet[g] = true
	}
	for _, mapping := range d.Tools.Mappings {
		if !minimalSet[mapping.Generic] {
			continue
		}
		for _, dest := range mapping.Destinations {
			if dest.Kind == domain.DestMain {
				for _, name := range dest.Names {
					allowed[name] = true
				}
			}
		}
	}
	return allowed
}

// collectRegisteredToolsKeys returns the set of tools keys declared by all registered
// harnesses, for use in asserting that Codex does not emit a field with any of them.
func collectRegisteredToolsKeys(t *testing.T, reg registry.Registry) map[string]bool {
	t.Helper()
	keys := make(map[string]bool)
	for _, ref := range reg.List() {
		if !ref.Usable {
			continue
		}
		m, err := reg.Resolve(ref.ID)
		if err != nil {
			t.Fatalf("collectRegisteredToolsKeys: Resolve(%q): %v", ref.ID, err)
		}
		if k := m.Descriptor().Frontmatter.ToolsKey; k != "" {
			keys[k] = true
		}
	}
	return keys
}
