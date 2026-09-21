package catalog_test

// harness_content_test.go provides the structural load check for the live
// Catalog/HarnessInjections/Codex/ directory (I6.4).
//
// Every other harness test in this run points at testdata/frozen-catalog/, so
// nothing else proves the live catalog files parse correctly. This file loads
// the live Codex content directory through the production injection-content loader
// and asserts structure only: both files load without error, and the orchestrator
// file declares a non-empty version. No assertion is made against the wording,
// length or region content of the files.

import (
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog/catalogpaths"
	"mosaic-deploy/internal/harness/injectionfile"
)

// TestCodexHarnessContent_LiveDirectoryLoads verifies that the live
// Catalog/HarnessInjections/Codex/ directory parses through the production
// injection-content loader without structural error, and that the orchestrator
// content file declares a non-empty version.
//
// This check targets the live directory (not the frozen test fixture) to
// catch malformed files in the catalog that frozen-fixture tests would not
// detect. Assert structure only: no assertion is made against the wording,
// length, or region content of the files in this directory.
func TestCodexHarnessContent_LiveDirectoryLoads(t *testing.T) {
	contentDir := filepath.Join(repoRoot(), catalogpaths.HarnessContentDirCodex)

	content, err := injectionfile.LoadDir(contentDir)
	if err != nil {
		t.Fatalf("injectionfile.LoadDir(%q): %v; "+
			"the live Catalog/HarnessInjections/Codex/ directory must parse through "+
			"the production injection-content loader without error; "+
			"check that HarnessInjections.md and HarnessInjectionsOrchestrator.md "+
			"exist with well-formed frontmatter and properly closed managed regions",
			contentDir, err)
	}

	if content.OrchestratorVersion == "" {
		t.Errorf("HarnessInjectionsOrchestrator.md: version is empty; "+
			"the file must declare a non-empty version in its YAML frontmatter; "+
			"add 'version: \"1.0.0\"' (or later) to the frontmatter block")
	}
}
