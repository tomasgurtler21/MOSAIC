package builtin_test

// model_source_test.go tests that each CLI-backed builtin harness, when discovered through
// the registry with the ModelCatalog hook, offers the shared catalog's model ids and format
// hint; and that vscodeghcp continues to offer its own descriptor-sourced list (T9.1).
//
// The test also verifies that a descriptor-only harness unknown to the shared catalog keeps
// its own descriptor-sourced model catalog (AC9.5).
//
// All tests in this file reference registry.Options.ModelCatalog, which does not exist until
// I9.1 is implemented. Every test therefore fails to compile until then — that is the intended
// TDD RED state.
//
// The ModelCatalog hook is the seam that lets the registry overlay the shared catalog's data
// onto a module's Descriptor().Models without any package importing builtin harness data
// directly. The hook receives "no shared entry" (empty IDs) for vscodeghcp and any unknown
// harness, so those modules fall back to their own descriptor-sourced catalog.

import (
	"os"
	"path/filepath"
	"testing"

	// Side-effect imports ensure the four builtin harness init() functions run, registering
	// their factories with the package-level registry. Discover then constructs and hooks them.
	_ "mosaic-deploy/internal/harness/builtin/claudecode"
	_ "mosaic-deploy/internal/harness/builtin/ghcpcli"
	_ "mosaic-deploy/internal/harness/builtin/opencode"
	_ "mosaic-deploy/internal/harness/builtin/vscodeghcp"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/registry"
)

// ---------------------------------------------------------------------------
// Shared test catalog — a per-test substitute for the shared mosaic-common/harness
// catalog. Using concrete test IDs (rather than importing the real common module)
// keeps this module's go.mod unchanged and tests the hook mechanism independently
// of the common module's own content.
// ---------------------------------------------------------------------------

// testSharedModelCatalog is the model data the hook installs for the three CLI-backed
// harnesses. It uses deliberately invented IDs (prefixed "shared-") that appear in no
// real YAML descriptor, so the presence of any of them in a module's Models.IDs proves
// the hook was applied — not that the YAML happened to contain the same IDs.
var testSharedModelCatalog = map[string]domain.ModelCatalog{
	"claude-code": {
		IDs:        []string{"shared-claude-model-1", "shared-claude-model-2"},
		FormatHint: "shared-claude-<model>-<version>",
	},
	"opencode": {
		IDs:        []string{"shared-opencode-provider/shared-model"},
		FormatHint: "shared-provider/model-id",
	},
	"ghcp-cli": {
		IDs:        []string{"shared-ghcp-model-a", "shared-ghcp-model-b"},
		FormatHint: "shared-ghcp-<model>-<version>",
	},
	// "vscode-ghcp" is intentionally absent: it is not a CLI-backed harness and has no
	// entry in the shared catalog. The hook returns empty IDs for it, causing it to keep
	// its own descriptor-sourced list.
}

// testModelCatalogHook returns a ModelCatalog hook function backed by testSharedModelCatalog.
// It returns the catalog for known CLI harnesses and an empty catalog for all others,
// mirroring what the composition root does with mosaic-common/harness.LookupModelCatalog.
func testModelCatalogHook() func(string, domain.ModelCatalog) domain.ModelCatalog {
	return func(harnessID string, declared domain.ModelCatalog) domain.ModelCatalog {
		if cat, ok := testSharedModelCatalog[harnessID]; ok {
			return cat
		}
		return domain.ModelCatalog{} // empty IDs → keep descriptor's own catalog
	}
}

// discoverBuiltinsWithModelHook calls registry.Discover with the real repo root (so builtin
// factories can load their content files from Catalog/HarnessInjections/) and the test
// ModelCatalog hook. The four builtin harnesses must have registered themselves via init()
// through the side-effect imports above before this function is called.
func discoverBuiltinsWithModelHook(t *testing.T) registry.Registry {
	t.Helper()
	root := repoRoot(t)
	reg, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ModelCatalog: testModelCatalogHook(), // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover with ModelCatalog hook: %v", err)
	}
	return reg
}

// modelSlicesEqual reports whether a and b contain exactly the same elements in the same
// order. It is used to verify that model ID lists match the hook's expected output exactly.
func modelSlicesEqual(a, b []string) bool {
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

// anyOfIn reports whether slice contains any element from want.
func anyOfIn(slice []string, want []string) bool {
	set := make(map[string]bool, len(slice))
	for _, s := range slice {
		set[s] = true
	}
	for _, w := range want {
		if set[w] {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// T9.1 — CLI-backed harnesses offer shared catalog's model ids and format hint
// ---------------------------------------------------------------------------

// TestModelSource_ClaudeCode_OffersSharedCatalogIDs verifies that the claude-code module,
// when discovered with the ModelCatalog hook, offers exactly the shared catalog's model ids
// rather than whatever ids are declared in its own YAML models: block.
func TestModelSource_ClaudeCode_OffersSharedCatalogIDs(t *testing.T) {
	reg := discoverBuiltinsWithModelHook(t)

	m, err := reg.Resolve("claude-code")
	if err != nil {
		t.Fatalf("Resolve(%q): %v", "claude-code", err)
	}

	want := testSharedModelCatalog["claude-code"]
	got := m.Descriptor().Models

	if !modelSlicesEqual(got.IDs, want.IDs) {
		t.Errorf("claude-code: Descriptor().Models.IDs = %v; want %v (from shared catalog hook); "+
			"the ModelCatalog hook result must be installed on the module's Descriptor().Models",
			got.IDs, want.IDs)
	}
}

// TestModelSource_ClaudeCode_OffersSharedCatalogFormatHint verifies that the claude-code
// module, when discovered with the ModelCatalog hook, offers the shared catalog's format hint.
func TestModelSource_ClaudeCode_OffersSharedCatalogFormatHint(t *testing.T) {
	reg := discoverBuiltinsWithModelHook(t)

	m, err := reg.Resolve("claude-code")
	if err != nil {
		t.Fatalf("Resolve(%q): %v", "claude-code", err)
	}

	want := testSharedModelCatalog["claude-code"].FormatHint
	got := m.Descriptor().Models.FormatHint

	if got != want {
		t.Errorf("claude-code: Descriptor().Models.FormatHint = %q; want %q (from shared catalog hook)",
			got, want)
	}
}

// TestModelSource_OpenCode_OffersSharedCatalogIDs verifies that the opencode module, when
// discovered with the ModelCatalog hook, offers exactly the shared catalog's model ids.
func TestModelSource_OpenCode_OffersSharedCatalogIDs(t *testing.T) {
	reg := discoverBuiltinsWithModelHook(t)

	m, err := reg.Resolve("opencode")
	if err != nil {
		t.Fatalf("Resolve(%q): %v", "opencode", err)
	}

	want := testSharedModelCatalog["opencode"]
	got := m.Descriptor().Models

	if !modelSlicesEqual(got.IDs, want.IDs) {
		t.Errorf("opencode: Descriptor().Models.IDs = %v; want %v (from shared catalog hook)",
			got.IDs, want.IDs)
	}
}

// TestModelSource_OpenCode_OffersSharedCatalogFormatHint verifies that the opencode module,
// when discovered with the ModelCatalog hook, offers the shared catalog's format hint.
func TestModelSource_OpenCode_OffersSharedCatalogFormatHint(t *testing.T) {
	reg := discoverBuiltinsWithModelHook(t)

	m, err := reg.Resolve("opencode")
	if err != nil {
		t.Fatalf("Resolve(%q): %v", "opencode", err)
	}

	want := testSharedModelCatalog["opencode"].FormatHint
	got := m.Descriptor().Models.FormatHint

	if got != want {
		t.Errorf("opencode: Descriptor().Models.FormatHint = %q; want %q (from shared catalog hook)",
			got, want)
	}
}

// TestModelSource_GHCPCLI_OffersSharedCatalogIDs verifies that the ghcp-cli module, when
// discovered with the ModelCatalog hook, offers exactly the shared catalog's model ids.
func TestModelSource_GHCPCLI_OffersSharedCatalogIDs(t *testing.T) {
	reg := discoverBuiltinsWithModelHook(t)

	m, err := reg.Resolve("ghcp-cli")
	if err != nil {
		t.Fatalf("Resolve(%q): %v", "ghcp-cli", err)
	}

	want := testSharedModelCatalog["ghcp-cli"]
	got := m.Descriptor().Models

	if !modelSlicesEqual(got.IDs, want.IDs) {
		t.Errorf("ghcp-cli: Descriptor().Models.IDs = %v; want %v (from shared catalog hook)",
			got.IDs, want.IDs)
	}
}

// TestModelSource_GHCPCLI_OffersSharedCatalogFormatHint verifies that the ghcp-cli module,
// when discovered with the ModelCatalog hook, offers the shared catalog's format hint.
func TestModelSource_GHCPCLI_OffersSharedCatalogFormatHint(t *testing.T) {
	reg := discoverBuiltinsWithModelHook(t)

	m, err := reg.Resolve("ghcp-cli")
	if err != nil {
		t.Fatalf("Resolve(%q): %v", "ghcp-cli", err)
	}

	want := testSharedModelCatalog["ghcp-cli"].FormatHint
	got := m.Descriptor().Models.FormatHint

	if got != want {
		t.Errorf("ghcp-cli: Descriptor().Models.FormatHint = %q; want %q (from shared catalog hook)",
			got, want)
	}
}

// ---------------------------------------------------------------------------
// T9.1 — vscodeghcp keeps its own descriptor-sourced list
// ---------------------------------------------------------------------------

// TestModelSource_VsCodeGHCP_KeepsDescriptorSourcedIDs verifies that the vscode-ghcp
// module, when discovered with a ModelCatalog hook that returns empty IDs for "vscode-ghcp"
// (because it is not in the shared CLI-only catalog), keeps its own YAML-sourced model
// catalog unchanged. This satisfies AC9.3.
func TestModelSource_VsCodeGHCP_KeepsDescriptorSourcedIDs(t *testing.T) {
	reg := discoverBuiltinsWithModelHook(t)

	m, err := reg.Resolve("vscode-ghcp")
	if err != nil {
		t.Fatalf("Resolve(%q): %v", "vscode-ghcp", err)
	}

	got := m.Descriptor().Models.IDs
	if len(got) == 0 {
		t.Fatal("vscode-ghcp: Descriptor().Models.IDs is empty after discovery with ModelCatalog hook; " +
			"vscodeghcp must keep its own descriptor-sourced catalog when the hook returns empty IDs; " +
			"the vscode-ghcp YAML declares model IDs and the empty-IDs fallback must preserve them")
	}

	// The test hook's IDs (prefixed "shared-") appear in no real YAML descriptor. If any
	// appear in vscode-ghcp's list, the fallback rule is broken.
	sharedOnlyIDs := []string{
		"shared-claude-model-1", "shared-claude-model-2",
		"shared-opencode-provider/shared-model",
		"shared-ghcp-model-a", "shared-ghcp-model-b",
	}
	if anyOfIn(got, sharedOnlyIDs) {
		t.Errorf("vscode-ghcp: Descriptor().Models.IDs = %v; "+
			"none of the shared-catalog hook IDs must appear; "+
			"vscodeghcp must source its models from its own descriptor, not the CLI-only shared catalog",
			got)
	}
}

// TestModelSource_VsCodeGHCP_HasNonEmptyModelListFromDescriptor verifies that vscode-ghcp
// offers a non-empty model list from its embedded YAML descriptor even when the hook applies.
// This is the base assertion that the empty-IDs fallback correctly preserves a populated
// descriptor-sourced catalog (AC9.3).
func TestModelSource_VsCodeGHCP_HasNonEmptyModelListFromDescriptor(t *testing.T) {
	reg := discoverBuiltinsWithModelHook(t)

	m, err := reg.Resolve("vscode-ghcp")
	if err != nil {
		t.Fatalf("Resolve(%q): %v", "vscode-ghcp", err)
	}

	ids := m.Descriptor().Models.IDs
	if len(ids) == 0 {
		t.Errorf("vscode-ghcp: Descriptor().Models.IDs is empty after discovery with ModelCatalog hook; "+
			"the vscode-ghcp YAML descriptor declares model IDs and the empty-IDs fallback must preserve them; "+
			"confirm registry.Options.ModelCatalog empty-IDs fallback rule is implemented")
	}
}

// ---------------------------------------------------------------------------
// T9.1 — Unknown harness falls back to descriptor-sourced catalog (AC9.5)
// ---------------------------------------------------------------------------

// TestModelSource_DescriptorOnlyHarness_KeepsDescriptorCatalog verifies that a
// descriptor-only on-disk harness with its own models: block keeps that catalog when the
// hook returns empty IDs (the "unknown to shared catalog" case). This satisfies AC9.5: a
// harness the shared catalog does not know must still resolve its model data from its own
// descriptor rather than producing an empty list.
func TestModelSource_DescriptorOnlyHarness_KeepsDescriptorCatalog(t *testing.T) {
	// Use a fresh temp root so we have full control over on-disk harnesses.
	tempRoot := t.TempDir()
	harnessDir := filepath.Join(tempRoot, "MosaicDeploy", "harnesses", "unknown-to-shared-catalog")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", harnessDir, err)
	}
	yaml := "schema_version: \"1\"\nid: \"unknown-to-shared-catalog\"\ndisplay_name: \"Unknown Harness\"\nmodels:\n  ids:\n    - \"descriptor-model-alpha\"\n    - \"descriptor-model-beta\"\n  format_hint: \"descriptor-format-hint\"\n"
	if err := os.WriteFile(filepath.Join(harnessDir, "harness.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("WriteFile harness.yaml: %v", err)
	}

	// The hook returns empty IDs for "unknown-to-shared-catalog" (it is not in the shared catalog).
	reg, err := registry.Discover(registry.Options{
		MosaicRoot: tempRoot,
		// field does not exist → compile error (TDD RED)
		ModelCatalog: func(harnessID string, declared domain.ModelCatalog) domain.ModelCatalog {
			if cat, ok := testSharedModelCatalog[harnessID]; ok {
				return cat
			}
			return domain.ModelCatalog{} // empty IDs → keep descriptor's own catalog
		},
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve("unknown-to-shared-catalog")
	if err != nil {
		t.Fatalf("Resolve(%q): %v", "unknown-to-shared-catalog", err)
	}

	got := m.Descriptor().Models
	if !modelSlicesEqual(got.IDs, []string{"descriptor-model-alpha", "descriptor-model-beta"}) {
		t.Errorf("unknown-to-shared-catalog: Descriptor().Models.IDs = %v; "+
			"want [descriptor-model-alpha descriptor-model-beta] from the descriptor; "+
			"a harness unknown to the shared catalog must fall back to its own descriptor-sourced catalog (AC9.5)",
			got.IDs)
	}
	if got.FormatHint != "descriptor-format-hint" {
		t.Errorf("unknown-to-shared-catalog: Descriptor().Models.FormatHint = %q; "+
			"want %q from the descriptor",
			got.FormatHint, "descriptor-format-hint")
	}
}
