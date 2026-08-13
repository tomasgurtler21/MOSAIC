package main

// e2e_catalog_folder_test.go verifies the end-to-end wiring of --catalog-folder:
// that parsing the flag via scanGlobalFlags, resolving it via resolveCatalogFolder,
// and loading the catalog via catalog.Load together deliver agents and workflows from
// the custom catalogue while the MOSAIC root continues to supply the bundle.
//
// Contract under test (T5.5):
//
//   Agent and workflow sourcing:
//     - With --catalog-folder pointing at a fixture catalogue, agents written into that
//       catalogue appear in the loaded catalog's Agents() result.
//     - Agents written only into the default catalogue ({mosaic-root}/Catalog) do NOT
//       appear when a distinct --catalog-folder is supplied.
//     - Workflows written into the custom catalogue appear in Workflows(); workflows only
//       in the default catalogue do not.
//
//   Bundle sourcing:
//     - The bundle (DeployedSections.md) is always read from the MOSAIC root via
//       BundleSourceRelPath, regardless of what the custom catalogue root contains.
//     - A competing bundle file placed inside the custom catalogue root's tree at
//       an arbitrary path does not affect FileBundleLoader.LoadBundle(mosaicRoot).
//
//   Root identity after wiring:
//     - catalog.Root() equals the resolved mosaicRoot (not the catalogFolder).
//     - catalog.CatalogRoot() equals the resolved catalogFolder (not mosaicRoot/Catalog).
//
// NOTE: This test calls scanGlobalFlags with the Stage-5 three-return signature
// (mosaicRoot, catalogFolder string, allowExternal bool). Until I5.1 updates the
// implementation, this file will not compile — that is the expected TDD RED state.
// Once I5.1 updates the signature and I5.2 implements resolveCatalogFolder, the test
// will compile but fail until I5.4 threads the resolved folder into catalog.Load.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/harness/registry"
)

// ---------------------------------------------------------------------------
// Fixture helpers (local to package main tests)
// ---------------------------------------------------------------------------

// makeE2EMosaicRoot creates a minimal MOSAIC repository root suitable for end-to-end
// wiring tests. It writes the two marker files that isMosaicRoot requires and a
// minimal (but structurally valid) bundle document so that FileBundleLoader can load it.
func makeE2EMosaicRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Marker files required by isMosaicRoot / ResolveRoot.
	mustMkdirMain(t, root, "Agents", "Generic")
	mustWriteFileMain(t, root, filepath.Join("Agents", "Generic", "SourceFilesFormat.md"),
		[]byte("# Source Files Format\n"))
	mustMkdirMain(t, root, "Workflows")
	mustWriteFileMain(t, root, filepath.Join("Workflows", "Index.md"),
		[]byte("# Workflows Index\n\n| ID | Category | Version | Name | Description | Hint | File |\n|----|----------|---------|------|-------------|------|------|\n"))

	// Minimal bundle document at the canonical BundleSourceRelPath.
	mustMkdirMain(t, root, "Catalog", "Agents", "Generic")
	mustWriteFileMain(t, root, catalog.BundleSourceRelPath, []byte(minimalBundleDoc))

	return root
}

// minimalBundleDoc is a structurally valid bundle document with a single block so that
// FileBundleLoader.LoadBundle succeeds during the E2E test. It is not used to verify
// bundle content — only that the bundle comes from the MOSAIC root.
const minimalBundleDoc = `---
bundle_version: "1.0.0"
blocks:
  - name: "ClosingProcedure:Subagent"
    target: ClosingProcedure
    applies_to: subagent
    specified_in: Development/Designs/AgentTemplateArchitecture.md
---

[[SECTION:ClosingProcedure:Subagent]]
Follow the closing procedure.
[[/SECTION:ClosingProcedure:Subagent]]
`

// writeE2EAgentToDefaultCatalogue writes a minimal worker agent into the default
// catalogue location inside the MOSAIC root: {mosaicRoot}/Catalog/Agents/Generic/Agents/{category}/{key}.md.
func writeE2EAgentToDefaultCatalogue(t *testing.T, mosaicRoot, category, key string) {
	t.Helper()
	mustMkdirMain(t, mosaicRoot, "Catalog", "Agents", "Generic", "Agents", category)
	fm := agentFrontmatter(key, "Default catalogue agent for E2E test.")
	mustWriteFileMain(t, mosaicRoot,
		filepath.Join("Catalog", "Agents", "Generic", "Agents", category, key+".md"),
		[]byte(fm))
}

// writeE2EAgentToCustomCatalogueRoot writes a minimal worker agent into the custom
// catalogue root: {catalogRoot}/Agents/Generic/Agents/{category}/{key}.md (no Catalog/ prefix).
func writeE2EAgentToCustomCatalogueRoot(t *testing.T, catalogRoot, category, key string) {
	t.Helper()
	mustMkdirMain(t, catalogRoot, "Agents", "Generic", "Agents", category)
	fm := agentFrontmatter(key, "Custom catalogue agent for E2E test.")
	mustWriteFileMain(t, catalogRoot,
		filepath.Join("Agents", "Generic", "Agents", category, key+".md"),
		[]byte(fm))
}

// writeE2EWorkflowToCustomCatalogueRoot writes a minimal workflow entry and file into
// the custom catalogue root: {catalogRoot}/Workflows/Index.md and {catalogRoot}/Workflows/{category}/{id}.md.
func writeE2EWorkflowToCustomCatalogueRoot(t *testing.T, catalogRoot, category, id string) {
	t.Helper()
	mustMkdirMain(t, catalogRoot, "Workflows")
	indexLine := "| " + id + " | " + category + " | 1.0.0 | " + id + " | A workflow. | Hint. | `" + category + "/" + id + ".md` |\n"
	indexContent := "# Workflows Index\n\n| ID | Category | Version | Name | Description | Hint | File |\n|----|----------|---------|------|-------------|------|------|\n" + indexLine
	mustWriteFileMain(t, catalogRoot, filepath.Join("Workflows", "Index.md"), []byte(indexContent))

	mustMkdirMain(t, catalogRoot, "Workflows", category)
	wfContent := "---\nid: " + id + "\nversion: \"1.0.0\"\nname: " + id + "\ndescription: A workflow.\nhint: Hint.\n---\nContent.\n"
	mustWriteFileMain(t, catalogRoot, filepath.Join("Workflows", category, id+".md"), []byte(wfContent))
}

// agentFrontmatter returns a minimal but valid agent frontmatter block.
func agentFrontmatter(key, description string) string {
	return "---\nid: \"" + key + "-001\"\nversion: \"1.0.0\"\nname: " + key + "\n" +
		"description: " + description + "\nrole: subagent\nrequired_skills: []\n---\nContent.\n"
}

// mustMkdirMain creates a directory under root, failing the test on error.
// Named distinctly from the catalog_test package's mustMkdir to avoid symbol conflicts
// in the package-level view.
func mustMkdirMain(t *testing.T, root string, parts ...string) {
	t.Helper()
	dir := filepath.Join(append([]string{root}, parts...)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mustMkdirMain %q: %v", dir, err)
	}
}

// mustWriteFileMain writes content to {root}/{relPath}, failing the test on error.
func mustWriteFileMain(t *testing.T, root, relPath string, content []byte) {
	t.Helper()
	p := filepath.Join(root, relPath)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("mustWriteFileMain %q: %v", p, err)
	}
}

// ---------------------------------------------------------------------------
// T5.5 — End-to-end wiring of --catalog-folder through the main-package seams
// ---------------------------------------------------------------------------

// TestE2E_CatalogFolder_AgentsSourcedFromFixtureCatalogue verifies the full wiring from
// argument parsing through to catalog loading: scanGlobalFlags extracts the
// --catalog-folder value, resolveCatalogFolder converts it to an absolute path, and
// catalog.Load supplies agents from that fixture catalogue rather than from the default.
func TestE2E_CatalogFolder_AgentsSourcedFromFixtureCatalogue(t *testing.T) {
	mosaicRoot := makeE2EMosaicRoot(t)
	customCatalogRoot := t.TempDir()

	// Write an agent only into the custom catalogue root.
	writeE2EAgentToCustomCatalogueRoot(t, customCatalogRoot, "Test", "fixture-e2e-agent")

	// Write a DIFFERENT agent only into the default catalogue.
	// With --catalog-folder this agent must NOT appear.
	writeE2EAgentToDefaultCatalogue(t, mosaicRoot, "Test", "default-only-agent")

	// Step 1: Parse --catalog-folder from the argument list (simulates main.go step 1).
	args := []string{"--catalog-folder", customCatalogRoot, "deploy", "--harness", "stub"}
	_, catalogFolderFlag, _ := scanGlobalFlags(args)

	// Step 2: Resolve the catalog folder (simulates main.go step 2 after root resolution).
	catalogFolder, err := resolveCatalogFolder(mosaicRoot, catalogFolderFlag)
	if err != nil {
		t.Fatalf("resolveCatalogFolder(%q, %q): %v; "+
			"a valid directory supplied via --catalog-folder must not produce an error",
			mosaicRoot, catalogFolderFlag, err)
	}

	// Step 3: Load the catalog using both roots (simulates catalog.Load in main.go step 5).
	cat, err := catalog.Load(mosaicRoot, catalogFolder)
	if err != nil {
		t.Fatalf("catalog.Load(%q, %q): %v; catalog must load without error", mosaicRoot, catalogFolder, err)
	}

	// Assert: fixture agent from custom catalogue is present.
	if _, found := cat.Agent("fixture-e2e-agent"); !found {
		t.Errorf("Agent(\"fixture-e2e-agent\") not found in the loaded catalog; "+
			"agents must be sourced from the --catalog-folder fixture, not the default catalogue")
	}

	// Assert: default-only agent is NOT present (custom catalogue is the exclusive source).
	if _, found := cat.Agent("default-only-agent"); found {
		t.Errorf("Agent(\"default-only-agent\") was found; "+
			"agents from the default catalogue must not appear when --catalog-folder is supplied")
	}
}

// TestE2E_CatalogFolder_WorkflowsSourcedFromFixtureCatalogue verifies that workflows
// are sourced from the custom catalogue root specified by --catalog-folder, and that
// workflows present only in the default catalogue do not appear.
func TestE2E_CatalogFolder_WorkflowsSourcedFromFixtureCatalogue(t *testing.T) {
	mosaicRoot := makeE2EMosaicRoot(t)
	customCatalogRoot := t.TempDir()

	writeE2EWorkflowToCustomCatalogueRoot(t, customCatalogRoot, "TestCategory", "fixture-wf")

	args := []string{"--catalog-folder", customCatalogRoot, "workflows"}
	_, catalogFolderFlag, _ := scanGlobalFlags(args)

	catalogFolder, err := resolveCatalogFolder(mosaicRoot, catalogFolderFlag)
	if err != nil {
		t.Fatalf("resolveCatalogFolder: %v", err)
	}

	cat, err := catalog.Load(mosaicRoot, catalogFolder)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	if _, found := cat.Workflow("fixture-wf"); !found {
		t.Errorf("Workflow(\"fixture-wf\") not found; "+
			"workflows must be sourced from the --catalog-folder fixture")
	}
}

// TestE2E_CatalogFolder_BundleSourcedFromMosaicRoot verifies that the bundle is always
// read from the MOSAIC root via BundleSourceRelPath, regardless of what the custom
// catalogue root contains. Placing a competing "bundle" file inside the custom catalogue
// root at an arbitrary path must not affect FileBundleLoader.LoadBundle(mosaicRoot).
func TestE2E_CatalogFolder_BundleSourcedFromMosaicRoot(t *testing.T) {
	mosaicRoot := makeE2EMosaicRoot(t)
	customCatalogRoot := t.TempDir()

	// Place a file inside the custom catalogue root at a path that mimics BundleSourceRelPath.
	// This file must not interfere with bundle loading.
	competingDir := filepath.Join(customCatalogRoot, "Catalog", "Agents", "Generic")
	if err := os.MkdirAll(competingDir, 0o755); err == nil {
		_ = os.WriteFile(
			filepath.Join(competingDir, "DeployedSections.md"),
			[]byte("---\nbundle_version: \"WRONG-CUSTOM\"\nblocks: []\n---\nWrong bundle.\n"),
			0o644,
		)
	}

	args := []string{"--catalog-folder", customCatalogRoot, "deploy"}
	_, catalogFolderFlag, _ := scanGlobalFlags(args)

	catalogFolder, err := resolveCatalogFolder(mosaicRoot, catalogFolderFlag)
	if err != nil {
		t.Fatalf("resolveCatalogFolder: %v", err)
	}

	// Load the bundle from the MOSAIC root. It must not be affected by the custom catalogue.
	bundleContent, err := catalog.FileBundleLoader{}.LoadBundle(mosaicRoot)
	if err != nil {
		t.Fatalf("FileBundleLoader.LoadBundle(%q): %v; "+
			"the real bundle at BundleSourceRelPath must be loadable from the MOSAIC root",
			mosaicRoot, err)
	}

	if bundleContent.Version != "1.0.0" {
		t.Errorf("BundleContent.Version = %q, want %q (from MOSAIC root); "+
			"a competing file in the custom catalogue root must not affect the loaded bundle",
			bundleContent.Version, "1.0.0")
	}

	// Confirm that the catalogFolder is correctly resolved to the custom root.
	absCustom, _ := filepath.Abs(customCatalogRoot)
	if catalogFolder != absCustom {
		t.Errorf("resolved catalogFolder = %q, want %q (absolute form of custom catalogue root)",
			catalogFolder, absCustom)
	}
}

// TestE2E_CatalogFolder_CatalogRootAccessor_ReflectsCustomFolder verifies that after the
// full wiring (parse → resolve → load), catalog.CatalogRoot() returns the custom
// catalogue folder and catalog.Root() returns the MOSAIC root. The two accessors must
// reflect the two distinct roots independently.
func TestE2E_CatalogFolder_CatalogRootAccessor_ReflectsCustomFolder(t *testing.T) {
	mosaicRoot := makeE2EMosaicRoot(t)
	customCatalogRoot := t.TempDir()

	args := []string{"--catalog-folder", customCatalogRoot, "deploy"}
	_, catalogFolderFlag, _ := scanGlobalFlags(args)

	catalogFolder, err := resolveCatalogFolder(mosaicRoot, catalogFolderFlag)
	if err != nil {
		t.Fatalf("resolveCatalogFolder: %v", err)
	}

	cat, err := catalog.Load(mosaicRoot, catalogFolder)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	absCustom, _ := filepath.Abs(customCatalogRoot)
	absMosaic, _ := filepath.Abs(mosaicRoot)

	if cat.CatalogRoot() != absCustom {
		t.Errorf("cat.CatalogRoot() = %q, want %q (the custom catalogue root); "+
			"CatalogRoot() must reflect the --catalog-folder value after full wiring",
			cat.CatalogRoot(), absCustom)
	}
	if cat.Root() != absMosaic {
		t.Errorf("cat.Root() = %q, want %q (the MOSAIC root); "+
			"Root() must continue to reflect the MOSAIC root, not the catalogue root",
			cat.Root(), absMosaic)
	}
}

// TestE2E_CatalogFolder_HarnessDiscovery_UsesMosaicRoot_NotCatalogFolder verifies
// the isolation contract from AC5.7: registry.Discover must receive the MOSAIC root,
// never the catalogue folder. This test demonstrates the consequence of the violation
// by exercising registry.Discover with both values and showing that harness discovery
// is root-sensitive — only the correct MOSAIC root yields the expected harness.
//
// The test exercises the same seams as the other E2E tests (scanGlobalFlags,
// resolveCatalogFolder) to confirm the two roots are distinct after the full pipeline,
// then directly shows that swapping mosaicRoot for catalogFolder in registry.Options
// breaks harness discovery.
//
// NOTE: This test calls scanGlobalFlags with the Stage-5 three-return signature and
// will not compile until I5.1 updates the implementation — that is the expected RED state.
func TestE2E_CatalogFolder_HarnessDiscovery_UsesMosaicRoot_NotCatalogFolder(t *testing.T) {
	mosaicRoot := makeE2EMosaicRoot(t)
	catalogFolder := t.TempDir() // a plain directory, not a MOSAIC root

	// Write a descriptor-only harness under the MOSAIC root's MosaicDeploy/harnesses/ tree.
	// registry.Discover discovers on-disk harnesses from <MosaicRoot>/MosaicDeploy/harnesses/
	// (not from the catalogue root), so this harness must appear when mosaicRoot is used and
	// must NOT appear when catalogFolder is used.
	harnessID := "e2e-disc-test-harness"
	harnessDir := filepath.Join(mosaicRoot, "MosaicDeploy", "harnesses", harnessID)
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatalf("MkdirAll harness dir %q: %v", harnessDir, err)
	}
	harnessYAML := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"E2E Discovery Test Harness\"\n", harnessID)
	if err := os.WriteFile(filepath.Join(harnessDir, "harness.yaml"), []byte(harnessYAML), 0o644); err != nil {
		t.Fatalf("write harness.yaml: %v", err)
	}

	// Step 1: Parse --catalog-folder from the argument list (simulates main.go step 1).
	// This call also demonstrates the three-return signature that I5.1 must implement.
	args := []string{"--catalog-folder", catalogFolder, "deploy", "--harness", harnessID}
	_, catalogFolderFlag, _ := scanGlobalFlags(args)

	// Step 2: Resolve the catalogue folder (simulates main.go step 2).
	resolvedCatalogFolder, err := resolveCatalogFolder(mosaicRoot, catalogFolderFlag)
	if err != nil {
		t.Fatalf("resolveCatalogFolder(%q, %q): %v; "+
			"a valid directory supplied via --catalog-folder must not produce an error",
			mosaicRoot, catalogFolderFlag, err)
	}

	// Confirm the two roots are distinct after resolution; the test is not meaningful
	// if they happen to refer to the same directory.
	absMosaicRoot, _ := filepath.Abs(mosaicRoot)
	if resolvedCatalogFolder == absMosaicRoot {
		t.Fatal("test setup error: mosaicRoot and resolvedCatalogFolder are the same directory; " +
			"they must be distinct for this test to verify the isolation between the two roots")
	}

	// Step 6 — correct wiring: registry.Discover with MosaicRoot = mosaicRoot.
	// The harness written to {mosaicRoot}/MosaicDeploy/harnesses/ must appear in the result.
	regCorrect, err := registry.Discover(registry.Options{
		MosaicRoot: mosaicRoot,
		GOOS:       runtime.GOOS,
	})
	if err != nil {
		t.Fatalf("registry.Discover(MosaicRoot=mosaicRoot=%q): %v; "+
			"discover must succeed when given the correct MOSAIC root", mosaicRoot, err)
	}

	foundInMosaicRoot := false
	for _, ref := range regCorrect.List() {
		if ref.ID == harnessID {
			foundInMosaicRoot = true
			break
		}
	}
	if !foundInMosaicRoot {
		t.Errorf("harness %q not found when registry.Discover used MosaicRoot=%q; "+
			"the descriptor at {mosaicRoot}/MosaicDeploy/harnesses/%s/harness.yaml must be "+
			"discovered when the MOSAIC root is passed correctly",
			harnessID, mosaicRoot, harnessID)
	}

	// Step 6 — bug case: registry.Discover with MosaicRoot = resolvedCatalogFolder.
	// This simulates the AC5.7 violation: accidentally passing catalogFolder to registry.Discover
	// instead of mosaicRoot. The harness written under mosaicRoot's harnesses directory must NOT
	// appear, because the wrong root was used and catalogFolder has no MosaicDeploy/harnesses/ tree.
	// If this assertion fails, it means mosaicRoot and catalogFolder are the same directory
	// (caught above) or the test setup wrote the harness to the wrong location.
	regWrong, err := registry.Discover(registry.Options{
		MosaicRoot: resolvedCatalogFolder, // deliberate wrong value — demonstrates the violation
		GOOS:       runtime.GOOS,
	})
	if err != nil {
		t.Fatalf("registry.Discover(MosaicRoot=catalogFolder=%q): %v; "+
			"discover must not error on a directory without a MosaicDeploy/harnesses/ tree",
			resolvedCatalogFolder, err)
	}

	foundInCatalogFolder := false
	for _, ref := range regWrong.List() {
		if ref.ID == harnessID {
			foundInCatalogFolder = true
			break
		}
	}
	if foundInCatalogFolder {
		t.Errorf("harness %q found when registry.Discover used MosaicRoot=catalogFolder=%q; "+
			"the test harness lives under mosaicRoot, not catalogFolder, so it must not be "+
			"discovered when the catalogue folder is passed as MosaicRoot — "+
			"this confirms that main.go must pass mosaicRoot (not catalogFolder) to registry.Discover",
			harnessID, resolvedCatalogFolder)
	}
}

// TestE2E_CatalogFolder_AbsentFlag_UsesDefaultCatalogue verifies that when
// --catalog-folder is absent (flagValue == ""), the complete wiring produces a
// catalog whose CatalogRoot() equals DefaultCatalogRoot(mosaicRoot), preserving
// backwards-compatible behaviour for ordinary runs.
func TestE2E_CatalogFolder_AbsentFlag_UsesDefaultCatalogue(t *testing.T) {
	mosaicRoot := makeE2EMosaicRoot(t)

	// No --catalog-folder in args.
	args := []string{"deploy", "--harness", "stub"}
	_, catalogFolderFlag, _ := scanGlobalFlags(args)

	if catalogFolderFlag != "" {
		t.Fatalf("scanGlobalFlags returned catalogFolderFlag = %q, want \"\"; "+
			"absent --catalog-folder must yield an empty pre-scan value", catalogFolderFlag)
	}

	catalogFolder, err := resolveCatalogFolder(mosaicRoot, "")
	if err != nil {
		t.Fatalf("resolveCatalogFolder with empty flagValue: %v", err)
	}

	cat, err := catalog.Load(mosaicRoot, catalogFolder)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	wantCatalogRoot, _ := filepath.Abs(catalog.DefaultCatalogRoot(mosaicRoot))
	if cat.CatalogRoot() != wantCatalogRoot {
		t.Errorf("cat.CatalogRoot() = %q, want %q (DefaultCatalogRoot(mosaicRoot)); "+
			"absent --catalog-folder must preserve the default catalogue root behaviour",
			cat.CatalogRoot(), wantCatalogRoot)
	}
}
