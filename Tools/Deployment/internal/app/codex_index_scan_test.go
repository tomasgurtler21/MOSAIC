package app

// codex_index_scan_test.go covers T8.2 and T8.6.
//
// T8.2 — Extension-driven inventory (buildDeployedAgentIndex):
//   - A Codex harness descriptor with extension ".toml" causes the index to scan for .toml
//     files, not .md files.
//   - A .toml file whose stamp block carries a mosaic_id comment is indexed under that id.
//   - .md files in a Codex agents directory are skipped (wrong extension).
//
// T8.6 — Corrupt deployed file (scanWorkspaceAgents parse-failed path):
//   - A .toml file with invalid TOML syntax that matches a catalog agent key by filename
//     is reported in Matched with ParseFailed: true and MatchedBy == MatchByFileNameKeyParseFailed.
//   - The run does not abort; the file is classified, not skipped, because its filename key
//     resolves to a known catalog agent.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/domain"
	_ "mosaic-deploy/internal/agentformat/all"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// codexIndexDesc returns a HarnessDescriptor for Codex with the agent extension ".toml".
func codexIndexDesc() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		AgentFormatID: "codex-toml",
		Extensions: map[domain.ArtifactKind]string{
			domain.ArtifactAgent: ".toml",
		},
	}
}

// writeCodexFileWithID writes a minimal Codex TOML file at filepath.Join(dir, filename)
// whose stamp block carries the given mosaic_id. The TOML body is minimal but valid.
func writeCodexFileWithID(t *testing.T, dir, filename, numericID string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeCodexFileWithID MkdirAll(%q): %v", dir, err)
	}
	content := "# mosaic_id: " + numericID + "\n" +
		"# mosaic_version: 2.0\n" +
		"\n" +
		"sandbox_mode = \"read-only\"\n" +
		"developer_instructions = \"Deployed agent.\\n\"\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writeCodexFileWithID WriteFile: %v", err)
	}
}

// writeBrokenCodexFile writes a .toml file with invalid TOML syntax (duplicate key),
// which causes the Codex decoder to return a parse error.
func writeBrokenCodexFile(t *testing.T, dir, filename string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeBrokenCodexFile MkdirAll(%q): %v", dir, err)
	}
	// Duplicate key: invalid TOML, triggers decoder error.
	content := "model = \"gpt-6-astra\"\nmodel = \"duplicate\"\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writeBrokenCodexFile WriteFile: %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.2 — buildDeployedAgentIndex with Codex .toml extension
// ---------------------------------------------------------------------------

// TestBuildDeployedAgentIndex_CodexToml_FileWithStampID_IsIndexed verifies that the index
// scan picks up a .toml file when the harness descriptor declares ".toml" as the agent
// extension, and indexes the file under the id in its stamp block.
func TestBuildDeployedAgentIndex_CodexToml_FileWithStampID_IsIndexed(t *testing.T) {
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	writeCodexFileWithID(t, filepath.Join(workspace, agentsDir), "my-agent.toml", "99")

	index, _ := buildDeployedAgentIndex(workspace, agentsDir, codexIndexDesc())

	entries := index.Lookup("99")
	if len(entries) == 0 {
		t.Error("index.Lookup(\"99\") returned no entries; " +
			"a Codex .toml file with stamp id \"99\" must be indexed under that id")
	}
}

// TestBuildDeployedAgentIndex_CodexToml_EntryContainsRelativePath verifies that the indexed
// TargetPath is relative to the deployment root, matching the format probeDeployedArtifact
// expects.
func TestBuildDeployedAgentIndex_CodexToml_EntryContainsRelativePath(t *testing.T) {
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	writeCodexFileWithID(t, filepath.Join(workspace, agentsDir), "my-agent.toml", "99")

	index, _ := buildDeployedAgentIndex(workspace, agentsDir, codexIndexDesc())

	entries := index.Lookup("99")
	if len(entries) == 0 {
		t.Fatal("index.Lookup(\"99\") returned no entries; cannot check TargetPath")
	}
	wantPath := filepath.Join(agentsDir, "my-agent.toml")
	if entries[0].TargetPath != wantPath {
		t.Errorf("entry.TargetPath = %q, want %q; TargetPath must be relative to the workspace root",
			entries[0].TargetPath, wantPath)
	}
}

// TestBuildDeployedAgentIndex_CodexToml_MdFileSkipped verifies that a .md file in a Codex
// agents directory is NOT indexed when the harness descriptor declares ".toml". The index
// scan must filter by the declared extension.
func TestBuildDeployedAgentIndex_CodexToml_MdFileSkipped(t *testing.T) {
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	dir := filepath.Join(workspace, agentsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Write a .md file with a valid frontmatter id — should be skipped for Codex descriptor.
	content := []byte("---\nid: \"77\"\nversion: \"1.0\"\n---\nMarkdown agent.\n")
	if err := os.WriteFile(filepath.Join(dir, "md-agent.md"), content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	index, _ := buildDeployedAgentIndex(workspace, agentsDir, codexIndexDesc())

	if len(index) != 0 {
		t.Errorf("index has %d entries; .md files must be skipped when the harness declares .toml extension", len(index))
	}
}

// TestBuildDeployedAgentIndex_CodexToml_MultipleFiles_AllIndexed verifies that all .toml files
// with distinct stamp ids in a Codex agents directory are indexed in a single scan.
func TestBuildDeployedAgentIndex_CodexToml_MultipleFiles_AllIndexed(t *testing.T) {
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	writeCodexFileWithID(t, filepath.Join(workspace, agentsDir), "alpha.toml", "10")
	writeCodexFileWithID(t, filepath.Join(workspace, agentsDir), "beta.toml", "20")
	writeCodexFileWithID(t, filepath.Join(workspace, agentsDir), "gamma.toml", "30")

	index, _ := buildDeployedAgentIndex(workspace, agentsDir, codexIndexDesc())

	for _, id := range []string{"10", "20", "30"} {
		if entries := index.Lookup(id); len(entries) == 0 {
			t.Errorf("index.Lookup(%q) returned no entries; all Codex .toml files must be indexed", id)
		}
	}
	if len(index) != 3 {
		t.Errorf("index has %d distinct ids, want 3", len(index))
	}
}

// ---------------------------------------------------------------------------
// T8.6 — scanWorkspaceAgents: broken .toml classified as ParseFailed
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_CodexToml_BrokenFile_ParseFailedTrue verifies that when a .toml
// file cannot be decoded (TOML syntax error), and the file's name matches a catalog agent key
// after stripping the ".toml" suffix, the scan reports a Matched entry with ParseFailed: true
// and MatchedBy == MatchByFileNameKeyParseFailed. The run is not aborted; the entry is
// classified so that probeDeployedStateWithIndex can propagate the parse failure to plan.Build,
// which will classify the agent as ActionConflict.
func TestScanWorkspaceAgents_CodexToml_BrokenFile_ParseFailedTrue(t *testing.T) {
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	// Write a broken .toml file whose base name matches catalog key "broken-agent"
	// (filename "broken-agent.toml" -> key "broken-agent" after stripping ".toml").
	writeBrokenCodexFile(t, filepath.Join(workspace, agentsDir), "broken-agent.toml")

	cat := newScanCatalog()
	cat.byKey["broken-agent"] = domain.Agent{Key: "broken-agent", NumericID: "55"}

	scan, _ := scanWorkspaceAgents(workspace, agentsDir, cat, codexIndexDesc())

	if len(scan.Matched) == 0 {
		t.Fatal("expected broken-agent.toml to appear in Matched via filename-key fallback; " +
			"a broken .toml whose filename key resolves must be classified, not skipped")
	}
	found := scan.Matched[0]
	if !found.ParseFailed {
		t.Errorf("ParseFailed = false, want true; a file with a TOML decode error must be classified with ParseFailed: true")
	}
	if found.MatchedBy != MatchByFileNameKeyParseFailed {
		t.Errorf("MatchedBy = %q, want %q; parse-failed match must use MatchByFileNameKeyParseFailed",
			found.MatchedBy, MatchByFileNameKeyParseFailed)
	}
	if found.AgentKey != "broken-agent" {
		t.Errorf("AgentKey = %q, want %q", found.AgentKey, "broken-agent")
	}
}

// TestScanWorkspaceAgents_CodexToml_BrokenFile_NoMatchingCatalogKey_IsSkipped verifies that
// a broken .toml file whose filename key does NOT resolve to any catalog agent is silently
// skipped, not classified as a parse-failed match. Only files identifiable as MOSAIC-managed
// (by their filename key) are classified on decode failure.
func TestScanWorkspaceAgents_CodexToml_BrokenFile_NoMatchingCatalogKey_IsSkipped(t *testing.T) {
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	// Broken .toml whose base name "unknown-agent" has no catalog entry.
	writeBrokenCodexFile(t, filepath.Join(workspace, agentsDir), "unknown-agent.toml")

	cat := newScanCatalog()
	// Catalog is empty — no match for "unknown-agent".

	scan, _ := scanWorkspaceAgents(workspace, agentsDir, cat, codexIndexDesc())

	if len(scan.Matched) != 0 {
		t.Errorf("Matched has %d entries, want 0; a broken .toml with no catalog match must be skipped",
			len(scan.Matched))
	}
}

// TestScanWorkspaceAgents_CodexToml_ValidFile_MatchedByNumericID verifies that a valid .toml
// file with a mosaic_id stamp is matched through the numeric-id catalog lookup path, not the
// filename-key fallback.
func TestScanWorkspaceAgents_CodexToml_ValidFile_MatchedByNumericID(t *testing.T) {
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	writeCodexFileWithID(t, filepath.Join(workspace, agentsDir), "worker.toml", "88")

	cat := newScanCatalog()
	cat.byNumericID["88"] = domain.Agent{Key: "catalog-worker", NumericID: "88"}
	// No filename-key match to confirm numeric-id path is used.

	scan, _ := scanWorkspaceAgents(workspace, agentsDir, cat, codexIndexDesc())

	if len(scan.Matched) == 0 {
		t.Fatal("expected worker.toml to appear in Matched via numeric-id lookup")
	}
	m := scan.Matched[0]
	if m.MatchedBy != MatchByNumericID {
		t.Errorf("MatchedBy = %q, want %q; valid .toml with stamp id must match by numeric id",
			m.MatchedBy, MatchByNumericID)
	}
	if m.AgentKey != "catalog-worker" {
		t.Errorf("AgentKey = %q, want %q", m.AgentKey, "catalog-worker")
	}
	if m.ParseFailed {
		t.Error("ParseFailed must be false for a successfully decoded .toml file")
	}
}
