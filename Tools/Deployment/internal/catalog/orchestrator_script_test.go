package catalog_test

// orchestrator_script_test.go verifies the catalog's handling of the second
// orchestrator-role file, orchestrator-script.md.
//
// Verified behaviours (T7.1):
//
// catalogpaths helpers:
//   - OrchestratorScriptFileName constant equals "orchestrator-script.md"
//   - OrchestratorScriptFile returns a path whose base name is OrchestratorScriptFileName
//   - OrchestratorScriptFile path sits under the same OrchestratorDir as OrchestratorFile
//
// Catalog loading:
//   - When orchestrator-script.md is present in the fixture root, OrchestratorScript()
//     returns (agent, true) with Key == "orchestrator-script" and Role == RoleOrchestrator
//   - When orchestrator-script.md is absent, OrchestratorScript() returns (zero, false)
//     and catalog loading succeeds without error
//   - A fixture root lacking orchestrator-script.md does not produce any catalog Issue
//   - Agent("orchestrator-script") resolves the script agent when the file is present
//   - Orchestrator() continues to return the main orchestrator, unaffected by the script file
//   - Script file's agent has a non-nil Tools slice and empty ToolsPlaceholder (real list form)
//   - Script file is not included in Agents() (the worker-agent accessor)

import (
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/catalog/catalogpaths"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// catalogpaths helpers (T7.1 — path shape)
// ---------------------------------------------------------------------------

// TestCatalogpaths_OrchestratorScriptFileName_IsCorrectBaseName verifies that the
// OrchestratorScriptFileName constant equals the expected base name. This constant is
// the single source of truth for all path composition involving the script file.
func TestCatalogpaths_OrchestratorScriptFileName_IsCorrectBaseName(t *testing.T) {
	const want = "orchestrator-script.md"
	if catalogpaths.OrchestratorScriptFileName != want {
		t.Errorf("OrchestratorScriptFileName = %q, want %q", catalogpaths.OrchestratorScriptFileName, want)
	}
}

// TestCatalogpaths_OrchestratorScriptFile_BaseNameMatchesFileName verifies that the
// path returned by OrchestratorScriptFile has OrchestratorScriptFileName as its base name.
func TestCatalogpaths_OrchestratorScriptFile_BaseNameMatchesFileName(t *testing.T) {
	root := t.TempDir()
	got := catalogpaths.OrchestratorScriptFile(root)
	if filepath.Base(got) != catalogpaths.OrchestratorScriptFileName {
		t.Errorf("OrchestratorScriptFile base = %q, want %q",
			filepath.Base(got), catalogpaths.OrchestratorScriptFileName)
	}
}

// TestCatalogpaths_OrchestratorScriptFile_SameDirectoryAsOrchestratorFile verifies that
// OrchestratorScriptFile sits in the same directory as OrchestratorFile. Both files
// live in Orchestrator/ under the catalog root; placing them side-by-side is what allows
// loadAgents to discover the script file from the same directory scan.
func TestCatalogpaths_OrchestratorScriptFile_SameDirectoryAsOrchestratorFile(t *testing.T) {
	root := t.TempDir()
	orchPath := catalogpaths.OrchestratorFile(root)
	scriptPath := catalogpaths.OrchestratorScriptFile(root)
	if filepath.Dir(orchPath) != filepath.Dir(scriptPath) {
		t.Errorf("OrchestratorFile dir = %q, OrchestratorScriptFile dir = %q; want the same directory",
			filepath.Dir(orchPath), filepath.Dir(scriptPath))
	}
}

// ---------------------------------------------------------------------------
// Fixture helpers for catalog loading tests
// ---------------------------------------------------------------------------

// writeOrchestratorFile writes a minimal orchestrator.md to the Catalog/Orchestrator/
// subdirectory of the given MOSAIC root. catalog.Load resolves the catalog root as
// DefaultCatalogRoot(mosaicRoot) = mosaicRoot/Catalog, so files must be placed there.
func writeOrchestratorFile(t *testing.T, mosaicRoot string) {
	t.Helper()
	catalogRoot := filepath.Join(mosaicRoot, "Catalog")
	mustMkdir(t, catalogRoot, "Orchestrator")
	content := []byte(`---
role: orchestrator
version: "1.0"
name: Orchestrator
description: Main orchestrator.
tools: "{tool-permissions}"
---
Body.
`)
	mustWriteFile(t, catalogRoot, filepath.Join("Orchestrator", "orchestrator.md"), content)
}

// writeOrchestratorScriptFile writes a minimal orchestrator-script.md with a real tools
// list (not a placeholder) to the Catalog/Orchestrator/ subdirectory of the given MOSAIC root.
func writeOrchestratorScriptFile(t *testing.T, mosaicRoot string) {
	t.Helper()
	catalogRoot := filepath.Join(mosaicRoot, "Catalog")
	mustMkdir(t, catalogRoot, "Orchestrator")
	content := []byte(`---
role: orchestrator
version: "1.0"
name: Orchestrator Script
description: Script-mode orchestrator.
tools: [file_read, file_edit, file_search, content_search]
---
Body.
`)
	mustWriteFile(t, catalogRoot, filepath.Join("Orchestrator", "orchestrator-script.md"), content)
}

// makeMinimalMosaicRoot builds a temp MOSAIC root with a Catalog/SourceFilesFormat.md
// marker so catalog.Load succeeds.
func makeMinimalMosaicRoot(t *testing.T) string {
	t.Helper()
	root := makeTempMosaicRoot(t)
	return root
}

// ---------------------------------------------------------------------------
// T7.1: OrchestratorScript() present
// ---------------------------------------------------------------------------

// TestOrchestratorScript_WhenFilePresent_ReturnsAgentAndTrue verifies that when the
// fixture catalog root contains orchestrator-script.md, OrchestratorScript() returns
// (agent, true). This is the core loading contract for the script file.
func TestOrchestratorScript_WhenFilePresent_ReturnsAgentAndTrue(t *testing.T) {
	root := makeMinimalMosaicRoot(t)
	writeOrchestratorFile(t, root)
	writeOrchestratorScriptFile(t, root)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	agent, ok := cat.OrchestratorScript()
	if !ok {
		t.Fatal("OrchestratorScript() returned false; expected the script file to be loaded when present; " +
			"I7.2 (loading the second orchestrator-role file) has not been implemented yet")
	}
	_ = agent
}

// TestOrchestratorScript_WhenFilePresent_KeyIsOrchestratorScript verifies that when the
// script file is loaded, its Key is "orchestrator-script" (derived from the file base name).
func TestOrchestratorScript_WhenFilePresent_KeyIsOrchestratorScript(t *testing.T) {
	root := makeMinimalMosaicRoot(t)
	writeOrchestratorFile(t, root)
	writeOrchestratorScriptFile(t, root)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	agent, ok := cat.OrchestratorScript()
	if !ok {
		t.Skip("OrchestratorScript() returned false; I7.2 not yet implemented — cannot check key")
	}
	if agent.Key != "orchestrator-script" {
		t.Errorf("OrchestratorScript().Key = %q, want %q", agent.Key, "orchestrator-script")
	}
}

// TestOrchestratorScript_WhenFilePresent_RoleIsOrchestrator verifies that the loaded
// script agent has Role == RoleOrchestrator. Both orchestrator-role files declare
// `role: orchestrator` in frontmatter and must parse to the orchestrator role.
func TestOrchestratorScript_WhenFilePresent_RoleIsOrchestrator(t *testing.T) {
	root := makeMinimalMosaicRoot(t)
	writeOrchestratorFile(t, root)
	writeOrchestratorScriptFile(t, root)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	agent, ok := cat.OrchestratorScript()
	if !ok {
		t.Skip("OrchestratorScript() returned false; I7.2 not yet implemented — cannot check role")
	}
	if agent.Role != domain.RoleOrchestrator {
		t.Errorf("OrchestratorScript().Role = %q, want RoleOrchestrator", agent.Role)
	}
}

// TestOrchestratorScript_WhenFilePresent_HasRealToolsList verifies that the script agent
// has a non-nil Tools slice (the real list form) and an empty ToolsPlaceholder. This is
// the key frontmatter difference from the main orchestrator, which uses a placeholder.
func TestOrchestratorScript_WhenFilePresent_HasRealToolsList(t *testing.T) {
	root := makeMinimalMosaicRoot(t)
	writeOrchestratorFile(t, root)
	writeOrchestratorScriptFile(t, root)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	agent, ok := cat.OrchestratorScript()
	if !ok {
		t.Skip("OrchestratorScript() returned false; I7.2 not yet implemented — cannot check tools")
	}
	if agent.Tools == nil {
		t.Error("OrchestratorScript().Tools is nil; script file uses a real tools list, not a placeholder")
	}
	if agent.ToolsPlaceholder != "" {
		t.Errorf("OrchestratorScript().ToolsPlaceholder = %q, want empty; script file must not use a placeholder",
			agent.ToolsPlaceholder)
	}
}

// TestOrchestratorScript_WhenFilePresent_ReachableByAgentKey verifies that
// Agent("orchestrator-script") resolves the script agent when the file is present.
// The script agent must coexist in the agent index alongside the main orchestrator.
func TestOrchestratorScript_WhenFilePresent_ReachableByAgentKey(t *testing.T) {
	root := makeMinimalMosaicRoot(t)
	writeOrchestratorFile(t, root)
	writeOrchestratorScriptFile(t, root)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// This lookup must succeed when I7.2 is implemented.
	_, ok := cat.Agent("orchestrator-script")
	if !ok {
		t.Error("Agent(\"orchestrator-script\") returned false; " +
			"the script file must be indexed under its key so Agent(key) works; " +
			"I7.2 has not been implemented yet")
	}
}

// TestOrchestratorScript_MainOrchestratorUnchanged verifies that loading the script file
// does not affect Orchestrator(). The main orchestrator must still be returned by its own
// accessor regardless of whether the script file is present.
func TestOrchestratorScript_MainOrchestratorUnchanged(t *testing.T) {
	root := makeMinimalMosaicRoot(t)
	writeOrchestratorFile(t, root)
	writeOrchestratorScriptFile(t, root)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	orc := cat.Orchestrator()
	if orc.Key != "orchestrator" {
		t.Errorf("Orchestrator().Key = %q, want %q; main orchestrator must not be displaced by the script file",
			orc.Key, "orchestrator")
	}
	if orc.Role != domain.RoleOrchestrator {
		t.Errorf("Orchestrator().Role = %q, want RoleOrchestrator", orc.Role)
	}
}

// TestOrchestratorScript_ScriptFileDistinctKeyFromMainOrchestrator verifies that the script
// file's key differs from the main orchestrator's key, so both coexist in the agent index
// without collision.
func TestOrchestratorScript_ScriptFileDistinctKeyFromMainOrchestrator(t *testing.T) {
	root := makeMinimalMosaicRoot(t)
	writeOrchestratorFile(t, root)
	writeOrchestratorScriptFile(t, root)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	agent, ok := cat.OrchestratorScript()
	if !ok {
		t.Skip("OrchestratorScript() returned false; I7.2 not yet implemented")
	}
	orc := cat.Orchestrator()
	if agent.Key == orc.Key {
		t.Errorf("OrchestratorScript().Key == Orchestrator().Key == %q; keys must be distinct",
			agent.Key)
	}
}

// TestOrchestratorScript_ScriptFileNotInAgents verifies that the script file does not
// appear in Agents() (the worker-agent accessor). Only worker subagents belong there.
func TestOrchestratorScript_ScriptFileNotInAgents(t *testing.T) {
	root := makeMinimalMosaicRoot(t)
	writeOrchestratorFile(t, root)
	writeOrchestratorScriptFile(t, root)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	for _, a := range cat.Agents() {
		if a.Key == "orchestrator-script" {
			t.Error("orchestrator-script appeared in Agents(); it must be excluded — " +
				"only worker subagents belong in the Agents() accessor")
		}
	}
}

// ---------------------------------------------------------------------------
// T7.1: OrchestratorScript() absent — tolerant loading
// ---------------------------------------------------------------------------

// TestOrchestratorScript_WhenFileAbsent_ReturnsFalse verifies that when the fixture catalog
// root has no orchestrator-script.md, OrchestratorScript() returns (zero, false). Absence
// of the file is not an error; a catalog without it deploys normally.
func TestOrchestratorScript_WhenFileAbsent_ReturnsFalse(t *testing.T) {
	root := makeMinimalMosaicRoot(t)
	writeOrchestratorFile(t, root)
	// Deliberately do not write orchestrator-script.md.

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load failed when orchestrator-script.md is absent: %v; "+
			"a missing script file must not cause catalog loading to fail", err)
	}

	_, ok := cat.OrchestratorScript()
	if ok {
		t.Error("OrchestratorScript() returned true for a catalog root with no orchestrator-script.md; " +
			"expected false — absent file must produce (zero, false)")
	}
}

// TestOrchestratorScript_WhenFileAbsent_CatalogLoadsWithoutError verifies that catalog.Load
// succeeds even when orchestrator-script.md is absent. The file's absence must not be a hard
// error at any layer.
func TestOrchestratorScript_WhenFileAbsent_CatalogLoadsWithoutError(t *testing.T) {
	root := makeMinimalMosaicRoot(t)
	writeOrchestratorFile(t, root)
	// Deliberately do not write orchestrator-script.md.

	_, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load returned error when orchestrator-script.md is absent: %v; "+
			"absence of the script file must never cause catalog loading to fail", err)
	}
}

// TestOrchestratorScript_WhenFileAbsent_NoNewIssues verifies that when the script file is
// absent, catalog.Load does not produce any catalog.Issue related to the script file. The
// absence is tolerated silently, not reported as a diagnostic.
func TestOrchestratorScript_WhenFileAbsent_NoNewIssues(t *testing.T) {
	root := makeMinimalMosaicRoot(t)
	writeOrchestratorFile(t, root)
	// Deliberately do not write orchestrator-script.md.

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	for _, iss := range cat.Issues() {
		if iss.Subject == "orchestrator-script" {
			t.Errorf("unexpected issue for missing orchestrator-script.md: [%s] %s; "+
				"absence must be tolerated without producing an issue", iss.Code, iss.Message)
		}
	}
}

// TestOrchestratorScript_WhenFileAbsent_AgentLookupReturnsFalse verifies that
// Agent("orchestrator-script") returns false when the file is absent. No phantom entry
// should appear in the agent index.
func TestOrchestratorScript_WhenFileAbsent_AgentLookupReturnsFalse(t *testing.T) {
	root := makeMinimalMosaicRoot(t)
	writeOrchestratorFile(t, root)
	// Deliberately do not write orchestrator-script.md.

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	_, ok := cat.Agent("orchestrator-script")
	if ok {
		t.Error("Agent(\"orchestrator-script\") returned true for a catalog root with no script file; " +
			"expected false — no phantom entry must appear in the agent index when the file is absent")
	}
}
