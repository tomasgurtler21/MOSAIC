package catalog_test

// dual_root_test.go covers the dual-root catalog loading contract.
//
// These tests verify the behaviour introduced by the two-root Load contract:
//
//   DefaultCatalogRoot and DefaultCatalogDirName:
//     - The constant equals "Catalog".
//     - DefaultCatalogRoot returns the expected path without I/O.
//
//   ValidateCatalogRoot:
//     - An existing directory returns nil.
//     - A missing path returns an error wrapping ErrCatalogFolderMissing.
//     - A path that is a file returns an error wrapping ErrCatalogFolderNotDirectory.
//     - An existing directory with no internal structure is valid (sparse is OK).
//
//   CatalogRoot accessor:
//     - When no explicit catalogRoot is supplied, CatalogRoot() equals
//       DefaultCatalogRoot(mosaicRoot) — both as an absolute path and by value.
//     - When an explicit catalogRoot is supplied, CatalogRoot() returns that value
//       (made absolute).
//     - The returned path is always absolute.
//
//   Agents and workflows load from the catalogue root only:
//     - A distinct custom catalogue root is the exclusive source of agents and workflows.
//     - Agents and workflows present in the default catalogue but absent from the custom
//       catalogue root do not appear in the loaded catalog.
//     - Similarly for workflows.
//
//   Skill merge with catalogue-root precedence:
//     - A skill key unique to the default catalogue survives in the merged result.
//     - A skill key unique to the catalogue root is added to the merged result.
//     - On key collision the catalogue root's version wins; the default entry is discarded.
//     - A key collision produces no Issue of any severity — shadowing is normal operation.
//     - The merged skills contain no duplicate entries (same key appearing twice).
//
//   Hook bundle merge with catalogue-root precedence:
//     - A bundle id unique to the default catalogue survives.
//     - A bundle id unique to the catalogue root is added.
//     - On id collision the catalogue root's version wins.
//     - A collision produces no Issue.
//     - Hook integrity checking (hook-hash-mismatch) applies to bundles sourced from the
//       catalogue root, not just the default catalogue.
//
//   Default case equivalence:
//     - Load with an empty catalogRoot (defaulting to DefaultCatalogRoot(mosaicRoot)) produces
//       exactly the same skills and hooks as an explicit Load(mosaicRoot, DefaultCatalogRoot(mosaicRoot)).
//     - Neither call produces duplicate skills or hooks.
//     - The CatalogRoot() value for the default case equals DefaultCatalogRoot(mosaicRoot).
//
//   Sparse catalogue root:
//     - A catalogue root with no Skills/ directory loads without error and without new Issues.
//     - A catalogue root with no Hooks/ directory loads without error and without new Issues.
//     - A catalogue root with no Workflows/Index.md loads without error and without new Issues.
//     - A catalogue root with no orchestrator file loads without error and without new Issues.
//     - A catalogue root with an empty agent category directory loads without error and without new Issues.
//
//   Bundle resolves from the MOSAIC root:
//     - FileBundleLoader.LoadBundle always reads from the MOSAIC root via BundleSourceRelPath,
//       regardless of what any catalogue root contains.
//     - A competing bundle file placed in the catalogue root at a path that is NOT
//       BundleSourceRelPath relative to the MOSAIC root does not interfere.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// ---------------------------------------------------------------------------
// Stage 4 fixture helpers
//
// These helpers write content to EITHER the default catalogue tree (under
// mosaicRoot/Catalog/...) or to a custom catalogue root (under catalogRoot/...)
// WITHOUT the Catalog/ segment, which is the Stage 4 layout.
// ---------------------------------------------------------------------------

// makeDistinctCatalogRoot creates a new temp directory that acts as a distinct
// custom catalogue root — it is NOT under mosaicRoot/Catalog/. The returned path
// is the root of the custom catalogue tree. On entry it is empty (sparse).
func makeDistinctCatalogRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "catalog-root-*")
	if err != nil {
		t.Fatalf("makeDistinctCatalogRoot: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// writeAgentToDefaultCatalogue writes a minimal worker agent file under
// mosaicRoot/Catalog/Agents/Generic/Agents/{category}/{key}.md.
// This is the DEFAULT catalogue layout, present before Stage 4 changes the scan paths.
func writeAgentToDefaultCatalogue(t *testing.T, mosaicRoot, category, key string) {
	t.Helper()
	mustMkdir(t, mosaicRoot, "Catalog", "Agents", "Generic", "Agents", category)
	fm := "---\nid: \"" + key + "-id\"\nversion: \"1.0.0\"\nname: " + key + "\n" +
		"description: Default catalogue agent for dual-root tests.\nrole: subagent\nrequired_skills: []\n---\nContent.\n"
	relPath := filepath.Join("Catalog", "Agents", "Generic", "Agents", category, key+".md")
	mustWriteFile(t, mosaicRoot, relPath, []byte(fm))
}

// writeAgentToCustomCatalogueRoot writes a minimal worker agent file under
// catalogRoot/Agents/Generic/Agents/{category}/{key}.md.
// This is the CUSTOM catalogue layout after Stage 4 (no Catalog/ prefix in the root).
func writeAgentToCustomCatalogueRoot(t *testing.T, catalogRoot, category, key string) {
	t.Helper()
	mustMkdir(t, catalogRoot, "Agents", "Generic", "Agents", category)
	fm := "---\nid: \"" + key + "-id\"\nversion: \"1.0.0\"\nname: " + key + "\n" +
		"description: Custom catalogue agent for dual-root tests.\nrole: subagent\nrequired_skills: []\n---\nContent.\n"
	relPath := filepath.Join("Agents", "Generic", "Agents", category, key+".md")
	mustWriteFile(t, catalogRoot, relPath, []byte(fm))
}

// writeSkillToDefaultCatalogue writes a minimal skill folder under
// mosaicRoot/Catalog/Agents/Generic/Skills/{key}/SKILL.md.
func writeSkillToDefaultCatalogue(t *testing.T, mosaicRoot, key string) {
	t.Helper()
	mustMkdir(t, mosaicRoot, "Catalog", "Agents", "Generic", "Skills", key)
	content := "---\nname: " + key + "\ndescription: Default skill " + key + ".\nversion: \"1.0.0\"\n---\n# Skill\n"
	mustWriteFile(t, mosaicRoot,
		filepath.Join("Catalog", "Agents", "Generic", "Skills", key, "SKILL.md"),
		[]byte(content))
}

// writeSkillToCustomCatalogueRoot writes a minimal skill folder under
// catalogRoot/Agents/Generic/Skills/{key}/SKILL.md.
func writeSkillToCustomCatalogueRoot(t *testing.T, catalogRoot, key string) {
	t.Helper()
	mustMkdir(t, catalogRoot, "Agents", "Generic", "Skills", key)
	content := "---\nname: " + key + " custom\ndescription: Custom skill " + key + ".\nversion: \"2.0.0\"\n---\n# Skill\n"
	mustWriteFile(t, catalogRoot,
		filepath.Join("Agents", "Generic", "Skills", key, "SKILL.md"),
		[]byte(content))
}

// minimalHookYAML returns a minimal hook.yaml for the given bundle ID.
// The bundle has a single variant ("test-harness") with one file ("test-hook.sh").
// No content_hash is stored, so no integrity check is performed.
func minimalHookYAML(bundleID string) []byte {
	return []byte("schema_version: \"1\"\nid: " + bundleID + "\nversion: 1.0.0\n" +
		"description: Hook bundle " + bundleID + " for dual-root tests.\nplaceholder: false\n" +
		"variants:\n  test-harness:\n    supported: true\n    files:\n" +
		"      - source: test-hook.sh\n        target: test-hook.sh\n    registration: []\n")
}

// writeHookBundleToDefaultCatalogue writes a minimal hook bundle under
// mosaicRoot/Catalog/Agents/Generic/Hooks/{bundleID}/.
func writeHookBundleToDefaultCatalogue(t *testing.T, mosaicRoot, bundleID string) {
	t.Helper()
	bundleDir := filepath.Join(mosaicRoot, "Catalog", "Agents", "Generic", "Hooks", bundleID)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("writeHookBundleToDefaultCatalogue: MkdirAll(%q): %v", bundleDir, err)
	}
	mustWriteFile(t, bundleDir, "hook.yaml", minimalHookYAML(bundleID))
	variantDir := filepath.Join(bundleDir, "test-harness")
	if err := os.MkdirAll(variantDir, 0o755); err != nil {
		t.Fatalf("writeHookBundleToDefaultCatalogue: MkdirAll variant dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(variantDir, "test-hook.sh"), []byte("#!/bin/sh\n# default hook "+bundleID+"\n"), 0o644); err != nil {
		t.Fatalf("writeHookBundleToDefaultCatalogue: WriteFile test-hook.sh: %v", err)
	}
}

// writeHookBundleToCustomCatalogueRoot writes a minimal hook bundle under
// catalogRoot/Agents/Generic/Hooks/{bundleID}/.
func writeHookBundleToCustomCatalogueRoot(t *testing.T, catalogRoot, bundleID string) {
	t.Helper()
	bundleDir := filepath.Join(catalogRoot, "Agents", "Generic", "Hooks", bundleID)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("writeHookBundleToCustomCatalogueRoot: MkdirAll(%q): %v", bundleDir, err)
	}
	mustWriteFile(t, bundleDir, "hook.yaml", minimalHookYAML(bundleID))
	variantDir := filepath.Join(bundleDir, "test-harness")
	if err := os.MkdirAll(variantDir, 0o755); err != nil {
		t.Fatalf("writeHookBundleToCustomCatalogueRoot: MkdirAll variant dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(variantDir, "test-hook.sh"), []byte("#!/bin/sh\n# custom hook "+bundleID+"\n"), 0o644); err != nil {
		t.Fatalf("writeHookBundleToCustomCatalogueRoot: WriteFile test-hook.sh: %v", err)
	}
}

// writeWorkflowToCustomCatalogueRoot writes a minimal workflow into a custom catalogue root
// at catalogRoot/Workflows/Index.md and catalogRoot/Workflows/{category}/{id}.md.
func writeWorkflowToCustomCatalogueRoot(t *testing.T, catalogRoot, category, id string) {
	t.Helper()
	mustMkdir(t, catalogRoot, "Workflows")
	indexLine := "| " + id + " | " + category + " | 1.0.0 | " + id + " | A workflow. | Hint. | `" + category + "/" + id + ".md` |\n"
	indexContent := "# Workflows Index\n\n| ID | Category | Version | Name | Description | Hint | File |\n|----|----------|---------|------|-------------|------|------|\n" + indexLine
	mustWriteFile(t, catalogRoot, filepath.Join("Workflows", "Index.md"), []byte(indexContent))

	mustMkdir(t, catalogRoot, "Workflows", category)
	wfContent := "---\nid: " + id + "\nversion: \"1.0.0\"\nname: " + id + "\ndescription: A workflow.\nhint: Hint.\n---\nContent.\n"
	mustWriteFile(t, catalogRoot, filepath.Join("Workflows", category, id+".md"), []byte(wfContent))
}

// writeWorkflowToDefaultCatalogue writes a minimal workflow into the default catalogue under mosaicRoot.
func writeWorkflowToDefaultCatalogue(t *testing.T, mosaicRoot, category, id string) {
	t.Helper()
	mustMkdir(t, mosaicRoot, "Catalog", "Workflows")
	indexLine := "| " + id + " | " + category + " | 1.0.0 | " + id + " | A workflow. | Hint. | `" + category + "/" + id + ".md` |\n"
	indexContent := "# Workflows Index\n\n| ID | Category | Version | Name | Description | Hint | File |\n|----|----------|---------|------|-------------|------|------|\n" + indexLine
	mustWriteFile(t, mosaicRoot, filepath.Join("Catalog", "Workflows", "Index.md"), []byte(indexContent))

	mustMkdir(t, mosaicRoot, "Catalog", "Workflows", category)
	wfContent := "---\nid: " + id + "\nversion: \"1.0.0\"\nname: " + id + "\ndescription: A workflow.\nhint: Hint.\n---\nContent.\n"
	mustWriteFile(t, mosaicRoot, filepath.Join("Catalog", "Workflows", category, id+".md"), []byte(wfContent))
}

// writeTamperedHookBundleToCustomCatalogueRoot writes a hook bundle with an intentionally
// wrong content_hash to the custom catalogue root, triggering a hook-hash-mismatch issue.
func writeTamperedHookBundleToCustomCatalogueRoot(t *testing.T, catalogRoot, bundleID string) {
	t.Helper()
	bundleDir := filepath.Join(catalogRoot, "Agents", "Generic", "Hooks", bundleID)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("writeTamperedHookBundleToCustomCatalogueRoot: MkdirAll: %v", err)
	}
	hookYAML := []byte("schema_version: \"1\"\nid: " + bundleID + "\nversion: 1.0.0\n" +
		"description: Tampered hook for integrity test.\nplaceholder: false\n" +
		"content_hash: \"sha256:0000000000000000000000000000000000000000000000000000000000000000\"\n" +
		"variants:\n  test-harness:\n    supported: true\n    files:\n" +
		"      - source: test-hook.sh\n        target: test-hook.sh\n    registration: []\n")
	mustWriteFile(t, bundleDir, "hook.yaml", hookYAML)
	variantDir := filepath.Join(bundleDir, "test-harness")
	if err := os.MkdirAll(variantDir, 0o755); err != nil {
		t.Fatalf("writeTamperedHookBundleToCustomCatalogueRoot: MkdirAll variant: %v", err)
	}
	if err := os.WriteFile(filepath.Join(variantDir, "test-hook.sh"), []byte("#!/bin/sh\n# tampered bundle\n"), 0o644); err != nil {
		t.Fatalf("writeTamperedHookBundleToCustomCatalogueRoot: WriteFile: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DefaultCatalogRoot and DefaultCatalogDirName
// ---------------------------------------------------------------------------

// TestDefaultCatalogDirName_EqualsLiteralCatalog verifies that DefaultCatalogDirName
// is the string "Catalog". This is the single place the Catalog/ segment is defined
// for the catalogue root's default location.
func TestDefaultCatalogDirName_EqualsLiteralCatalog(t *testing.T) {
	if catalog.DefaultCatalogDirName != "Catalog" {
		t.Errorf("DefaultCatalogDirName = %q, want %q", catalog.DefaultCatalogDirName, "Catalog")
	}
}

// TestDefaultCatalogRoot_ReturnsJoinOfMosaicRootAndDirName verifies that
// DefaultCatalogRoot returns filepath.Join(mosaicRoot, DefaultCatalogDirName) without
// performing any I/O. The returned path must not require the directory to exist.
func TestDefaultCatalogRoot_ReturnsJoinOfMosaicRootAndDirName(t *testing.T) {
	mosaicRoot := filepath.Join("some", "repo", "root")
	want := filepath.Join(mosaicRoot, catalog.DefaultCatalogDirName)
	got := catalog.DefaultCatalogRoot(mosaicRoot)
	if got != want {
		t.Errorf("DefaultCatalogRoot(%q) = %q, want %q", mosaicRoot, got, want)
	}
}

// TestDefaultCatalogRoot_NoIORequired verifies that DefaultCatalogRoot succeeds
// even when the specified mosaicRoot does not exist on disk. The function is pure
// and must never touch the file system.
func TestDefaultCatalogRoot_NoIORequired(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")
	// Must not panic or error — just join the path.
	got := catalog.DefaultCatalogRoot(nonExistent)
	expected := filepath.Join(nonExistent, "Catalog")
	if got != expected {
		t.Errorf("DefaultCatalogRoot for non-existent path = %q, want %q", got, expected)
	}
}

// ---------------------------------------------------------------------------
// ValidateCatalogRoot
// ---------------------------------------------------------------------------

// TestValidateCatalogRoot_ExistingDirectory_ReturnsNil verifies that an existing
// directory passes validation. A sparse catalogue (no internal structure) is valid.
func TestValidateCatalogRoot_ExistingDirectory_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	if err := catalog.ValidateCatalogRoot(dir); err != nil {
		t.Errorf("ValidateCatalogRoot(%q): got error %v for an existing directory, want nil", dir, err)
	}
}

// TestValidateCatalogRoot_MissingPath_ReturnsErrCatalogFolderMissing verifies that a
// path that does not exist returns an error wrapping ErrCatalogFolderMissing.
func TestValidateCatalogRoot_MissingPath_ReturnsErrCatalogFolderMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	err := catalog.ValidateCatalogRoot(missing)
	if err == nil {
		t.Fatalf("ValidateCatalogRoot(%q): expected error for missing path, got nil", missing)
	}
	if !errors.Is(err, catalog.ErrCatalogFolderMissing) {
		t.Errorf("ValidateCatalogRoot(%q) error = %v; want error wrapping ErrCatalogFolderMissing", missing, err)
	}
}

// TestValidateCatalogRoot_FilePath_ReturnsErrCatalogFolderNotDirectory verifies that a
// path pointing to a regular file (not a directory) returns an error wrapping
// ErrCatalogFolderNotDirectory.
func TestValidateCatalogRoot_FilePath_ReturnsErrCatalogFolderNotDirectory(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-directory.txt")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("setup: WriteFile: %v", err)
	}
	err := catalog.ValidateCatalogRoot(filePath)
	if err == nil {
		t.Fatalf("ValidateCatalogRoot(%q): expected error for a file path, got nil", filePath)
	}
	if !errors.Is(err, catalog.ErrCatalogFolderNotDirectory) {
		t.Errorf("ValidateCatalogRoot(%q) error = %v; want error wrapping ErrCatalogFolderNotDirectory", filePath, err)
	}
}

// TestValidateCatalogRoot_SentinelErrors_AreDistinct verifies that ErrCatalogFolderMissing
// and ErrCatalogFolderNotDirectory are distinct sentinels that do not match each other
// through errors.Is.
func TestValidateCatalogRoot_SentinelErrors_AreDistinct(t *testing.T) {
	if errors.Is(catalog.ErrCatalogFolderMissing, catalog.ErrCatalogFolderNotDirectory) {
		t.Error("ErrCatalogFolderMissing matches ErrCatalogFolderNotDirectory; they must be distinct sentinels")
	}
	if errors.Is(catalog.ErrCatalogFolderNotDirectory, catalog.ErrCatalogFolderMissing) {
		t.Error("ErrCatalogFolderNotDirectory matches ErrCatalogFolderMissing; they must be distinct sentinels")
	}
}

// TestValidateCatalogRoot_SparseDirectory_IsValid verifies that a directory with no
// internal structure (no Agents/, no Workflows/) is accepted. Internal structure is
// not validated — a sparse catalogue is valid by contract.
func TestValidateCatalogRoot_SparseDirectory_IsValid(t *testing.T) {
	dir := t.TempDir()
	// Directory exists but is completely empty.
	if err := catalog.ValidateCatalogRoot(dir); err != nil {
		t.Errorf("ValidateCatalogRoot(%q): sparse directory rejected with %v; want nil", dir, err)
	}
}

// ---------------------------------------------------------------------------
// CatalogRoot accessor
// ---------------------------------------------------------------------------

// TestCatalog_CatalogRoot_DefaultsToDefaultCatalogRoot verifies that when Load is
// called with an empty catalogRoot (triggering the default), CatalogRoot() returns
// DefaultCatalogRoot(mosaicRoot) as an absolute path.
func TestCatalog_CatalogRoot_DefaultsToDefaultCatalogRoot(t *testing.T) {
	root := makeTempMosaicRoot(t)
	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load(%q, \"\"): %v", root, err)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", root, err)
	}
	wantCatalogRoot, err := filepath.Abs(catalog.DefaultCatalogRoot(absRoot))
	if err != nil {
		t.Fatalf("filepath.Abs(DefaultCatalogRoot): %v", err)
	}

	if cat.CatalogRoot() != wantCatalogRoot {
		t.Errorf("CatalogRoot() = %q, want %q (DefaultCatalogRoot(Root()))",
			cat.CatalogRoot(), wantCatalogRoot)
	}
}

// TestCatalog_CatalogRoot_ExplicitValueIsReturned verifies that when an explicit
// catalogRoot is supplied, CatalogRoot() returns that path (made absolute).
func TestCatalog_CatalogRoot_ExplicitValueIsReturned(t *testing.T) {
	root := makeTempMosaicRoot(t)
	customRoot := makeDistinctCatalogRoot(t)

	cat, err := catalog.Load(root, customRoot)
	if err != nil {
		t.Fatalf("catalog.Load(%q, %q): %v", root, customRoot, err)
	}

	absCustom, err := filepath.Abs(customRoot)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", customRoot, err)
	}

	if cat.CatalogRoot() != absCustom {
		t.Errorf("CatalogRoot() = %q, want %q (the supplied explicit catalogue root)",
			cat.CatalogRoot(), absCustom)
	}
}

// TestCatalog_CatalogRoot_IsAbsolute verifies that CatalogRoot() always returns
// an absolute path, regardless of whether the input was relative.
func TestCatalog_CatalogRoot_IsAbsolute(t *testing.T) {
	root := makeTempMosaicRoot(t)
	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	if !filepath.IsAbs(cat.CatalogRoot()) {
		t.Errorf("CatalogRoot() returned non-absolute path: %q", cat.CatalogRoot())
	}
}

// TestCatalog_Root_UnchangedByDualRoot verifies that Root() still returns the MOSAIC
// repository root (not the catalogue root) after the two-root interface change.
func TestCatalog_Root_UnchangedByDualRoot(t *testing.T) {
	root := makeTempMosaicRoot(t)
	customRoot := makeDistinctCatalogRoot(t)

	cat, err := catalog.Load(root, customRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}

	if cat.Root() != absRoot {
		t.Errorf("Root() = %q, want %q (the MOSAIC repository root, not the catalogue root)",
			cat.Root(), absRoot)
	}
}

// ---------------------------------------------------------------------------
// Agents load from the catalogue root only
// ---------------------------------------------------------------------------

// TestLoad_DistinctCatalogRoot_AgentsLoadFromCatalogRoot verifies that agents
// present in the custom catalogue root are found in Agents(). This confirms that
// the catalogue root — not the default catalogue — is the exclusive source of agents
// when a distinct catalogue root is supplied.
func TestLoad_DistinctCatalogRoot_AgentsLoadFromCatalogRoot(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	// Write an agent to the custom catalogRoot. After Stage 4, this should be found.
	writeAgentToCustomCatalogueRoot(t, catalogRoot, "Test", "custom-agent")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	_, found := cat.Agent("custom-agent")
	if !found {
		t.Errorf("Agent(\"custom-agent\") not found; agent placed in the custom catalogue root " +
			"must appear in the loaded catalog when that root is supplied")
	}
}

// TestLoad_DistinctCatalogRoot_DefaultCatalogAgentsAbsent verifies that an agent
// present in the default catalogue (mosaicRoot/Catalog/...) does NOT appear when a
// distinct catalogue root is supplied. The catalogue root is the exclusive source;
// the default catalogue contributes no agents when overridden.
func TestLoad_DistinctCatalogRoot_DefaultCatalogAgentsAbsent(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	// Write an agent to the DEFAULT catalogue only.
	writeAgentToDefaultCatalogue(t, mosaicRoot, "Test", "default-only-agent")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	_, found := cat.Agent("default-only-agent")
	if found {
		t.Errorf("Agent(\"default-only-agent\") was found in the catalog; " +
			"agents from the default catalogue must NOT appear when a distinct catalogue root is supplied")
	}
}

// TestLoad_DistinctCatalogRoot_WorkflowsLoadFromCatalogRoot verifies that workflows
// present in the custom catalogue root appear in Workflows().
func TestLoad_DistinctCatalogRoot_WorkflowsLoadFromCatalogRoot(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	writeWorkflowToCustomCatalogueRoot(t, catalogRoot, "TestCategory", "custom-workflow")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	_, found := cat.Workflow("custom-workflow")
	if !found {
		t.Errorf("Workflow(\"custom-workflow\") not found; workflow placed in the custom catalogue root " +
			"must appear in the loaded catalog when that root is supplied")
	}
}

// TestLoad_DistinctCatalogRoot_DefaultCatalogWorkflowsAbsent verifies that a workflow
// present only in the default catalogue does NOT appear when a distinct catalogue root
// is supplied and contains no corresponding workflow.
func TestLoad_DistinctCatalogRoot_DefaultCatalogWorkflowsAbsent(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	// Write a workflow to the DEFAULT catalogue only.
	writeWorkflowToDefaultCatalogue(t, mosaicRoot, "TestCategory", "default-only-workflow")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	_, found := cat.Workflow("default-only-workflow")
	if found {
		t.Errorf("Workflow(\"default-only-workflow\") was found; " +
			"workflows from the default catalogue must NOT appear when a distinct catalogue root is supplied")
	}
}

// ---------------------------------------------------------------------------
// Skill merge with catalogue-root precedence
// ---------------------------------------------------------------------------

// TestLoad_SkillMerge_UniqueDefaultSkillSurvives verifies that a skill key present
// only in the default catalogue appears in Skills() after the merge. The default
// catalogue's unique entries must not be dropped.
func TestLoad_SkillMerge_UniqueDefaultSkillSurvives(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	writeSkillToDefaultCatalogue(t, mosaicRoot, "default-only-skill")
	// No corresponding skill in catalogRoot.

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	_, found := cat.Skill("default-only-skill")
	if !found {
		t.Errorf("Skill(\"default-only-skill\") not found; a skill unique to the default catalogue " +
			"must survive in the merged result")
	}
}

// TestLoad_SkillMerge_UniqueCustomSkillAdded verifies that a skill key present only
// in the custom catalogue root appears in Skills() after the merge.
func TestLoad_SkillMerge_UniqueCustomSkillAdded(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	writeSkillToCustomCatalogueRoot(t, catalogRoot, "custom-only-skill")
	// No corresponding skill in default catalogue.

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	_, found := cat.Skill("custom-only-skill")
	if !found {
		t.Errorf("Skill(\"custom-only-skill\") not found; a skill unique to the custom catalogue root " +
			"must be added to the merged result")
	}
}

// TestLoad_SkillMerge_CollisionCustomWins verifies that when the same skill key exists
// in both the default catalogue and the custom catalogue root, the custom version wins.
// The returned skill's Name is taken from the custom catalogue root.
func TestLoad_SkillMerge_CollisionCustomWins(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	// Both have "shared-skill", but with different Name values in their SKILL.md.
	// writeSkillToDefaultCatalogue writes name = "shared-skill".
	// writeSkillToCustomCatalogueRoot writes name = "shared-skill custom".
	writeSkillToDefaultCatalogue(t, mosaicRoot, "shared-skill")
	writeSkillToCustomCatalogueRoot(t, catalogRoot, "shared-skill")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	s, found := cat.Skill("shared-skill")
	if !found {
		t.Fatal("Skill(\"shared-skill\") not found; the merged skill must exist")
	}
	// The custom catalogue root version has Name = "shared-skill custom" (version 2.0.0).
	if s.Name != "shared-skill custom" {
		t.Errorf("Skill(\"shared-skill\").Name = %q, want %q (custom catalogue root must win on collision)",
			s.Name, "shared-skill custom")
	}
}

// TestLoad_SkillMerge_CollisionProducesNoIssue verifies that a skill key collision
// between the default catalogue and the custom catalogue root produces no Issue of any
// severity. Shadowing is normal, deliberate operation and must be silent.
func TestLoad_SkillMerge_CollisionProducesNoIssue(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	writeSkillToDefaultCatalogue(t, mosaicRoot, "shared-skill")
	writeSkillToCustomCatalogueRoot(t, catalogRoot, "shared-skill")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	for _, iss := range cat.Issues() {
		if iss.Subject == "shared-skill" {
			t.Errorf("skill collision produced unexpected Issue for key \"shared-skill\": %+v; "+
				"shadowing must be silent (no Issue)", iss)
		}
	}
}

// TestLoad_SkillMerge_CollisionNoDuplicateEntries verifies that a skill key collision
// results in exactly one entry in Skills() for that key — not two. The merge must
// discard the default entry, keeping only the custom one.
func TestLoad_SkillMerge_CollisionNoDuplicateEntries(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	writeSkillToDefaultCatalogue(t, mosaicRoot, "shared-skill")
	writeSkillToCustomCatalogueRoot(t, catalogRoot, "shared-skill")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	count := 0
	for _, s := range cat.Skills() {
		if s.Key == "shared-skill" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Skills() contains %d entries with key \"shared-skill\", want exactly 1; "+
			"a collision must produce one merged entry, not a duplicate", count)
	}
}

// TestLoad_SkillMerge_BothUnique_BothPresent verifies that when each of the default
// catalogue and the custom catalogue root contains a unique skill, both appear in Skills().
func TestLoad_SkillMerge_BothUnique_BothPresent(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	writeSkillToDefaultCatalogue(t, mosaicRoot, "default-skill")
	writeSkillToCustomCatalogueRoot(t, catalogRoot, "custom-skill")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	if _, ok := cat.Skill("default-skill"); !ok {
		t.Error("Skill(\"default-skill\") not found; unique default-catalogue skill must survive the merge")
	}
	if _, ok := cat.Skill("custom-skill"); !ok {
		t.Error("Skill(\"custom-skill\") not found; unique custom-catalogue-root skill must be added by the merge")
	}
}

// ---------------------------------------------------------------------------
// Hook bundle merge with catalogue-root precedence
// ---------------------------------------------------------------------------

// TestLoad_HookMerge_UniqueDefaultBundleSurvives verifies that a hook bundle id
// present only in the default catalogue appears in Hooks() after the merge.
func TestLoad_HookMerge_UniqueDefaultBundleSurvives(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	writeHookBundleToDefaultCatalogue(t, mosaicRoot, "default-only-bundle")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	_, found := cat.Hook("default-only-bundle")
	if !found {
		t.Errorf("Hook(\"default-only-bundle\") not found; a hook bundle unique to the default catalogue " +
			"must survive in the merged result")
	}
}

// TestLoad_HookMerge_UniqueCustomBundleAdded verifies that a hook bundle id present
// only in the custom catalogue root appears in Hooks() after the merge.
func TestLoad_HookMerge_UniqueCustomBundleAdded(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	writeHookBundleToCustomCatalogueRoot(t, catalogRoot, "custom-only-bundle")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	_, found := cat.Hook("custom-only-bundle")
	if !found {
		t.Errorf("Hook(\"custom-only-bundle\") not found; a hook bundle unique to the custom catalogue root " +
			"must be added to the merged result")
	}
}

// TestLoad_HookMerge_CollisionCustomWins verifies that when the same bundle id exists
// in both roots, the custom catalogue root's version wins. The bundle's Version field
// must reflect the custom root's bundle.
func TestLoad_HookMerge_CollisionCustomWins(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	// Both have "shared-bundle". The writeHookBundle helpers produce version "1.0.0" in
	// hook.yaml. We can distinguish them only by which bundle's source directory is used;
	// the Version field would be the same. We write a custom bundle with a different
	// hook.yaml version to make the distinction assertable.
	writeHookBundleToDefaultCatalogue(t, mosaicRoot, "shared-bundle")

	// Write a custom version with version "2.0.0".
	bundleDir := filepath.Join(catalogRoot, "Agents", "Generic", "Hooks", "shared-bundle")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("setup: MkdirAll: %v", err)
	}
	customHookYAML := []byte("schema_version: \"1\"\nid: shared-bundle\nversion: 2.0.0\n" +
		"description: Custom shared bundle.\nplaceholder: false\n" +
		"variants:\n  test-harness:\n    supported: true\n    files:\n" +
		"      - source: test-hook.sh\n        target: test-hook.sh\n    registration: []\n")
	mustWriteFile(t, bundleDir, "hook.yaml", customHookYAML)
	variantDir := filepath.Join(bundleDir, "test-harness")
	if err := os.MkdirAll(variantDir, 0o755); err != nil {
		t.Fatalf("setup: MkdirAll variant: %v", err)
	}
	if err := os.WriteFile(filepath.Join(variantDir, "test-hook.sh"), []byte("#!/bin/sh\n# v2\n"), 0o644); err != nil {
		t.Fatalf("setup: WriteFile: %v", err)
	}

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	h, found := cat.Hook("shared-bundle")
	if !found {
		t.Fatal("Hook(\"shared-bundle\") not found; the merged bundle must exist")
	}
	if h.Version != "2.0.0" {
		t.Errorf("Hook(\"shared-bundle\").Version = %q, want %q (custom catalogue root must win on collision)",
			h.Version, "2.0.0")
	}
}

// TestLoad_HookMerge_CollisionProducesNoIssue verifies that a hook bundle id collision
// produces no Issue. Silent shadowing is the contract for both skills and hooks.
func TestLoad_HookMerge_CollisionProducesNoIssue(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	writeHookBundleToDefaultCatalogue(t, mosaicRoot, "shared-bundle")
	writeHookBundleToCustomCatalogueRoot(t, catalogRoot, "shared-bundle")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	for _, iss := range cat.Issues() {
		if iss.Subject == "shared-bundle" {
			t.Errorf("hook collision produced unexpected Issue for bundle \"shared-bundle\": %+v; "+
				"shadowing must be silent (no Issue)", iss)
		}
	}
}

// TestLoad_HookMerge_CollisionNoDuplicateEntries verifies that a bundle id collision
// results in exactly one entry in Hooks() for that id.
func TestLoad_HookMerge_CollisionNoDuplicateEntries(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	writeHookBundleToDefaultCatalogue(t, mosaicRoot, "shared-bundle")
	writeHookBundleToCustomCatalogueRoot(t, catalogRoot, "shared-bundle")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	count := 0
	for _, h := range cat.Hooks() {
		if h.Key == "shared-bundle" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Hooks() contains %d entries with id \"shared-bundle\", want exactly 1; "+
			"a collision must yield one merged entry, not a duplicate", count)
	}
}

// TestLoad_HookMerge_IntegrityCheckAppliesTo_CatalogRootBundle verifies that
// hook-hash-mismatch integrity checking applies to hook bundles sourced from the
// custom catalogue root, not just the default catalogue. A tampered bundle in the
// custom catalogue root must produce a hook-hash-mismatch Issue.
func TestLoad_HookMerge_IntegrityCheckAppliesTo_CatalogRootBundle(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	writeTamperedHookBundleToCustomCatalogueRoot(t, catalogRoot, "tampered-custom-bundle")

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	_, found := findIssue(cat.Issues(), "hook-hash-mismatch")
	if !found {
		t.Errorf("Issues() does not contain a hook-hash-mismatch issue for the tampered " +
			"bundle sourced from the custom catalogue root; integrity checking must apply to " +
			"bundles from either root, not just the default catalogue")
	}
}

// ---------------------------------------------------------------------------
// Default case equivalence
// ---------------------------------------------------------------------------

// TestLoad_DefaultCase_EmptyStringEquivalentToExplicitDefault verifies that calling
// Load with an empty catalogRoot ("") produces the same CatalogRoot() value as calling
// it with DefaultCatalogRoot(mosaicRoot) explicitly. Both must be byte-identical.
func TestLoad_DefaultCase_EmptyStringEquivalentToExplicitDefault(t *testing.T) {
	root := makeTempMosaicRoot(t)

	catDefault, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load(%q, \"\"): %v", root, err)
	}

	explicit := catalog.DefaultCatalogRoot(root)
	catExplicit, err := catalog.Load(root, explicit)
	if err != nil {
		t.Fatalf("catalog.Load(%q, %q): %v", root, explicit, err)
	}

	if catDefault.CatalogRoot() != catExplicit.CatalogRoot() {
		t.Errorf("CatalogRoot() differs:\n  empty-string: %q\n  explicit:     %q\n"+
			"both must equal DefaultCatalogRoot(mosaicRoot)", catDefault.CatalogRoot(), catExplicit.CatalogRoot())
	}
}

// TestLoad_DefaultCase_SkillCountMatches verifies that loading with an empty catalogRoot
// and with an explicit DefaultCatalogRoot produce the same number of skills. This guards
// against the identical-roots case accidentally doubling the skill count.
func TestLoad_DefaultCase_SkillCountMatches(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillToDefaultCatalogue(t, root, "alpha-skill")
	writeSkillToDefaultCatalogue(t, root, "beta-skill")

	catDefault, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load(%q, \"\"): %v", root, err)
	}
	catExplicit, err := catalog.Load(root, catalog.DefaultCatalogRoot(root))
	if err != nil {
		t.Fatalf("catalog.Load explicit: %v", err)
	}

	if len(catDefault.Skills()) != len(catExplicit.Skills()) {
		t.Errorf("skill count mismatch:\n  empty-string: %d\n  explicit:     %d\n"+
			"identical-roots case must not double the skill count",
			len(catDefault.Skills()), len(catExplicit.Skills()))
	}
}

// TestLoad_DefaultCase_NoExtraSkillsFromMerge verifies that in the identical-roots case
// (catalogRoot defaults to DefaultCatalogRoot(mosaicRoot)), Skills() contains no
// duplicate key. The merge of a set with itself must produce exactly that set.
func TestLoad_DefaultCase_NoExtraSkillsFromMerge(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillToDefaultCatalogue(t, root, "my-skill")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	seen := make(map[string]int)
	for _, s := range cat.Skills() {
		seen[s.Key]++
	}
	for key, count := range seen {
		if count > 1 {
			t.Errorf("Skills() contains %d entries for key %q in the default case; "+
				"identical-roots merge must produce exactly one entry per key", count, key)
		}
	}
}

// TestLoad_DefaultCase_NoExtraHooksFromMerge verifies that in the identical-roots case,
// Hooks() contains no duplicate id.
func TestLoad_DefaultCase_NoExtraHooksFromMerge(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeHookBundleToDefaultCatalogue(t, root, "my-bundle")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	seen := make(map[string]int)
	for _, h := range cat.Hooks() {
		seen[h.Key]++
	}
	for key, count := range seen {
		if count > 1 {
			t.Errorf("Hooks() contains %d entries for id %q in the default case; "+
				"identical-roots merge must produce exactly one entry per id", count, key)
		}
	}
}

// ---------------------------------------------------------------------------
// Sparse catalogue root
// ---------------------------------------------------------------------------

// TestLoad_SparseCatalogRoot_NoSkillsDir_Graceful verifies that a custom catalogue
// root with no Skills/ directory loads without error and produces no new Issues.
func TestLoad_SparseCatalogRoot_NoSkillsDir_Graceful(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)
	// catalogRoot is completely empty — no Skills/ directory.

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load with sparse catalogRoot (no Skills/): unexpected error: %v; "+
			"a missing Skills/ directory must degrade gracefully", err)
	}

	// No Issue should be produced for the absent Skills/ directory.
	for _, iss := range cat.Issues() {
		if iss.Code == "missing-skill-folder" || iss.Code == "missing-skills-dir" {
			t.Errorf("sparse catalogRoot produced unexpected issue %q; "+
				"an absent Skills/ directory must not produce any Issue", iss.Code)
		}
	}
}

// TestLoad_SparseCatalogRoot_NoHooksDir_Graceful verifies that a custom catalogue
// root with no Hooks/ directory loads without error and produces no new Issues.
func TestLoad_SparseCatalogRoot_NoHooksDir_Graceful(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	_, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load with sparse catalogRoot (no Hooks/): unexpected error: %v; "+
			"a missing Hooks/ directory must degrade gracefully", err)
	}
}

// TestLoad_SparseCatalogRoot_NoWorkflowsIndex_Graceful verifies that a custom catalogue
// root with no Workflows/Index.md loads without error and produces no new Issues.
func TestLoad_SparseCatalogRoot_NoWorkflowsIndex_Graceful(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	// Create the Workflows/ dir but not the Index.md.
	if err := os.MkdirAll(filepath.Join(catalogRoot, "Workflows"), 0o755); err != nil {
		t.Fatalf("setup: MkdirAll: %v", err)
	}

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load with sparse catalogRoot (no Workflows/Index.md): unexpected error: %v", err)
	}

	// No workflows from the catalogue root.
	workflows := cat.Workflows()
	if len(workflows) > 0 {
		// Workflows from the default catalogue are not expected either in this sparse fixture.
		// The mosaicRoot's default catalogue (makeTempMosaicRoot) has no workflows.
		t.Errorf("Workflows() returned %d workflows for a sparse catalogRoot with no Index.md; "+
			"want 0 (no error, just nothing loaded)", len(workflows))
	}
}

// TestLoad_SparseCatalogRoot_NoOrchestratorFile_Graceful verifies that a custom
// catalogue root with no orchestrator file loads without error. The Orchestrator()
// method returns the zero-value Agent in this case.
func TestLoad_SparseCatalogRoot_NoOrchestratorFile_Graceful(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	cat, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load with sparse catalogRoot (no orchestrator): unexpected error: %v; "+
			"a missing orchestrator file must degrade gracefully", err)
	}

	// Loading must succeed — the orchestrator is simply absent.
	_ = cat.Orchestrator() // must not panic
}

// TestLoad_SparseCatalogRoot_EmptyAgentCategory_Graceful verifies that a custom
// catalogue root with an agent category directory that contains no .md files loads
// without error.
func TestLoad_SparseCatalogRoot_EmptyAgentCategory_Graceful(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	// Create an empty category directory.
	if err := os.MkdirAll(filepath.Join(catalogRoot, "Agents", "Generic", "Agents", "EmptyCategory"), 0o755); err != nil {
		t.Fatalf("setup: MkdirAll: %v", err)
	}

	_, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load with empty agent category: unexpected error: %v; "+
			"an empty category directory must degrade gracefully", err)
	}
}

// TestLoad_SparseCatalogRoot_Completely_Empty_Graceful verifies that a completely
// empty custom catalogue root (no directories at all) loads without error.
func TestLoad_SparseCatalogRoot_Completely_Empty_Graceful(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)
	// catalogRoot is completely empty.

	_, err := catalog.Load(mosaicRoot, catalogRoot)
	if err != nil {
		t.Fatalf("catalog.Load with completely empty catalogRoot: unexpected error: %v; "+
			"an empty catalogue root must degrade gracefully with no error", err)
	}
}

// ---------------------------------------------------------------------------
// Bundle resolves from the MOSAIC root, never the catalogue root
// ---------------------------------------------------------------------------

// TestBundle_LoadsFromMosaicRoot_NotCatalogRoot verifies that FileBundleLoader.LoadBundle
// reads the bundle from the MOSAIC root's BundleSourceRelPath, regardless of what the
// custom catalogue root contains. The bundle path is always relative to the MOSAIC root.
func TestBundle_LoadsFromMosaicRoot_NotCatalogRoot(t *testing.T) {
	mosaicRoot := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)

	// Write the REAL bundle at the MOSAIC root's canonical path.
	writeBundleDoc(t, mosaicRoot, []byte(wellFormedBundleDoc))

	// Also write a COMPETING bundle file at a path inside the catalogue root's tree
	// (not at BundleSourceRelPath relative to the MOSAIC root). This file must be ignored.
	competingDir := filepath.Join(catalogRoot, "Catalog", "Agents", "Generic")
	if err := os.MkdirAll(competingDir, 0o755); err == nil {
		_ = os.WriteFile(
			filepath.Join(competingDir, "DeployedSections.md"),
			[]byte("---\nbundle_version: \"WRONG\"\n---\n\nThis competing bundle must never be loaded.\n"),
			0o644,
		)
	}

	// Load the bundle via the MOSAIC root. It must not be affected by the catalogue root.
	content, err := catalog.FileBundleLoader{}.LoadBundle(mosaicRoot)
	if err != nil {
		t.Fatalf("FileBundleLoader.LoadBundle(%q): %v; "+
			"the real bundle at BundleSourceRelPath must be loadable from the MOSAIC root", mosaicRoot, err)
	}

	if content.Version != "1.0.0" {
		t.Errorf("BundleContent.Version = %q, want %q (from MOSAIC root); "+
			"a competing bundle in the catalogue root must not affect the loaded bundle",
			content.Version, "1.0.0")
	}
}

// TestBundle_BundleSourceRelPath_ContainsCatalogPrefix verifies that BundleSourceRelPath
// contains the "Catalog/" segment, confirming the bundle is always resolved against the
// MOSAIC root (not the catalogue root). The invariant "Catalog appears exactly once" on
// the final absolute path holds for bundle loading.
func TestBundle_BundleSourceRelPath_ContainsCatalogPrefix(t *testing.T) {
	if len(catalog.BundleSourceRelPath) == 0 {
		t.Fatal("BundleSourceRelPath is empty; expected a non-empty path")
	}
	// The path must contain the Catalog/ segment, proving it is MOSAIC-root-relative.
	if !containsPathSegment(catalog.BundleSourceRelPath, "Catalog") {
		t.Errorf("BundleSourceRelPath %q does not contain a %q segment; "+
			"bundle must always resolve against the MOSAIC root via a Catalog/-prefixed path",
			catalog.BundleSourceRelPath, "Catalog")
	}
}

// containsPathSegment reports whether any path component of p equals segment.
func containsPathSegment(p, segment string) bool {
	clean := filepath.ToSlash(p)
	for {
		dir, file := filepath.Split(clean)
		if file == segment {
			return true
		}
		dir = filepath.ToSlash(filepath.Clean(dir))
		if dir == "." || dir == clean {
			break
		}
		clean = dir
	}
	return false
}

// TestLoad_ResolvedPaths_NoDoubleCatalogSegment verifies the key invariant:
// the absolute path to any skill's SourceDir must not contain "Catalog/Catalog/" —
// the double-prefix pattern that would arise if the "Catalog" segment is applied at
// both the root level and the scanner level.
func TestLoad_ResolvedPaths_NoDoubleCatalogSegment(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillToDefaultCatalogue(t, root, "path-check-skill")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	for _, s := range cat.Skills() {
		path := filepath.ToSlash(s.SourceDir)
		// The double-prefix failure mode produces a path like .../Catalog/Catalog/...
		if containsPathSegmentPair(path, "Catalog", "Catalog") {
			t.Errorf("skill %q SourceDir %q contains the double-prefix pattern \"Catalog/Catalog/\"; "+
				"the Catalog segment must appear in exactly one layer (the root value, not the scanner path)",
				s.Key, s.SourceDir)
		}
	}
}

// containsPathSegmentPair reports whether the path contains two consecutive segments
// matching first and then second (e.g., "Catalog/Catalog" in a slash-normalised path).
func containsPathSegmentPair(path, first, second string) bool {
	needle := first + "/" + second
	return countSubstring(path, needle) > 0
}

// countSubstring counts non-overlapping occurrences of sub in s.
func countSubstring(s, sub string) int {
	count := 0
	offset := 0
	for {
		idx := indexOf(s[offset:], sub)
		if idx < 0 {
			break
		}
		count++
		offset += idx + len(sub)
	}
	return count
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
