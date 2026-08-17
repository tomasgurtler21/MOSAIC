package app

// workspace_scan_test.go verifies scanWorkspaceAgents, the Stage 4 single-pass classifier
// that decides Update's agent-set membership. Because scanWorkspaceAgents is package-internal,
// this file belongs to package app (not package app_test), giving it direct access to the
// function and its supporting types.
//
// All tests in this file represent the TDD RED phase. The current implementation of
// scanWorkspaceAgents is a compilable stub that returns an empty WorkspaceAgentScan, so
// every test asserting a non-empty result correctly FAILS against the stub. After I4.1 lands,
// the tests must PASS.
//
// Verified behaviours:
//
// Catalog matching by numeric id (MatchByNumericID):
//   - A deployed file whose frontmatter `id` scalar resolves via AgentByNumericID is
//     reported in Matched with MatchedBy == MatchByNumericID and AgentKey from the catalog,
//     not the file name.
//   - A renamed file (file name no longer matches the catalog key) is still matched by
//     its numeric id, and its AgentKey reflects the catalog key, not the derived filename key.
//   - Numeric id lookup takes priority: when a file's numeric id resolves and its filename key
//     also resolves (to a different agent), only the numeric-id match is used.
//
// Catalog matching by filename key (MatchByFileNameKey):
//   - A deployed file with no numeric id whose filename key resolves via catalog.Agent is
//     reported in Matched with MatchedBy == MatchByFileNameKey.
//   - A file whose numeric id lookup fails and whose filename key resolves is matched by
//     filename key (fallback path).
//
// Harness-only (unmatched eligible):
//   - A file that fails both catalog lookups but passes the two-signal eligibility check
//     (transform_version + canonical sections) appears in HarnessOnly and not in Matched.
//
// Neither:
//   - A file that fails both catalog lookups and does not pass eligibility produces no entry
//     in either slice (the file is left byte-identical, no plan item, no warning).
//
// Disjoint by construction:
//   - A file in Matched must not also appear in HarnessOnly.
//   - A file in HarnessOnly must not also appear in Matched.
//
// Tolerance (silently skipped):
//   - A non-.md file in the agents directory is silently skipped.
//   - A directory entry inside the agents directory is silently skipped (non-recursive walk).
//   - A file that cannot be parsed (invalid YAML frontmatter) is silently skipped.
//   - A file with no frontmatter block is silently skipped.
//
// Empty/absent directory:
//   - An empty agentsDir string yields an empty scan without touching the workspace root.
//   - A non-existent agents directory yields an empty scan without returning an error.
//
// Ambiguous duplicate numeric id:
//   - Two deployed files declaring the same numeric id both appear in Matched (the scan does
//     not pick between them), and MatchedKeys() deduplicates so the key appears exactly once.
//
// Determinism:
//   - Matched and HarnessOnly slices are sorted by TargetPath.
//   - MatchedKeys() is sorted alphabetically and deduplicated.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// scanCatalog — minimal catalog.Catalog stub for scanWorkspaceAgents tests
// ---------------------------------------------------------------------------

// scanCatalog is a minimal catalog.Catalog stub used exclusively by workspace_scan_test.go.
// Only Agent() and AgentByNumericID() are implemented; all other methods panic to make misuse
// immediately visible. This matches the usage pattern scanWorkspaceAgents actually requires.
type scanCatalog struct {
	byNumericID map[string]domain.Agent
	byKey       map[string]domain.Agent
}

func (c *scanCatalog) Root() string                                       { return "" }
func (c *scanCatalog) CatalogRoot() string                                { return "" }
func (c *scanCatalog) Agents() []domain.Agent                             { panic("scanCatalog: Agents not used") }
func (c *scanCatalog) Agent(key string) (domain.Agent, bool)              { a, ok := c.byKey[key]; return a, ok }
func (c *scanCatalog) Orchestrator() domain.Agent                         { panic("scanCatalog: Orchestrator not used") }
func (c *scanCatalog) OrchestratorScript() (domain.Agent, bool)           { panic("scanCatalog: OrchestratorScript not used") }
func (c *scanCatalog) UtilityAgents() []domain.Agent                      { panic("scanCatalog: UtilityAgents not used") }
func (c *scanCatalog) InfrastructureAgents() []domain.Agent               { panic("scanCatalog: InfrastructureAgents not used") }
func (c *scanCatalog) StandaloneAgents() []domain.Agent                   { panic("scanCatalog: StandaloneAgents not used") }
func (c *scanCatalog) Skills() []domain.Skill                             { panic("scanCatalog: Skills not used") }
func (c *scanCatalog) Skill(key string) (domain.Skill, bool)              { panic("scanCatalog: Skill not used") }
func (c *scanCatalog) Hooks() []domain.HookBundle                         { panic("scanCatalog: Hooks not used") }
func (c *scanCatalog) Hook(key string) (domain.HookBundle, bool)          { panic("scanCatalog: Hook not used") }
func (c *scanCatalog) Workflows() []domain.Workflow                       { panic("scanCatalog: Workflows not used") }
func (c *scanCatalog) Workflow(id string) (domain.Workflow, bool)         { panic("scanCatalog: Workflow not used") }
func (c *scanCatalog) WorkflowCategories() []domain.WorkflowCategory      { panic("scanCatalog: WorkflowCategories not used") }
func (c *scanCatalog) Tiers() []domain.TierInfo                           { panic("scanCatalog: Tiers not used") }
func (c *scanCatalog) WorkflowSection(id string) ([]byte, error)          { panic("scanCatalog: WorkflowSection not used") }
func (c *scanCatalog) ReadSource(path string) ([]byte, error)             { panic("scanCatalog: ReadSource not used") }
func (c *scanCatalog) Issues() []catalog.Issue                            { return nil }
func (c *scanCatalog) AgentByNumericID(id string) (domain.Agent, bool)    { a, ok := c.byNumericID[id]; return a, ok }

// newScanCatalog returns an empty scanCatalog. Callers populate byNumericID and byKey
// to model whatever catalog state the test requires.
func newScanCatalog() *scanCatalog {
	return &scanCatalog{
		byNumericID: make(map[string]domain.Agent),
		byKey:       make(map[string]domain.Agent),
	}
}

// ---------------------------------------------------------------------------
// File-writing helpers
// ---------------------------------------------------------------------------

// writeScanFileWithID writes a deployed .md file with the given frontmatter `id` scalar and
// no transform_version, so the scan attempts catalog lookup but the file is not harness-only
// eligible on its own.
func writeScanFileWithID(t *testing.T, dir, filename, numericID string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeScanFileWithID: MkdirAll(%q): %v", dir, err)
	}
	content := "---\nid: \"" + numericID + "\"\nversion: \"1.0.0\"\n---\nDeployed content.\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writeScanFileWithID: WriteFile: %v", err)
	}
}

// writeScanFileNoID writes a deployed .md file with no `id` field but a version scalar, so
// the scan skips numeric-id lookup and falls through to filename-key lookup.
func writeScanFileNoID(t *testing.T, dir, filename string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeScanFileNoID: MkdirAll(%q): %v", dir, err)
	}
	content := "---\nversion: \"1.0.0\"\n---\nDeployed content with no id field.\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writeScanFileNoID: WriteFile: %v", err)
	}
}

// writeScanEligibleFile writes a .md file that passes both harness-only eligibility signals
// (transform_version in frontmatter, canonical <Identity type="core"> section in body) but
// has no id field. Used for harness-only tests where no catalog match is expected.
func writeScanEligibleFile(t *testing.T, dir, filename string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeScanEligibleFile: MkdirAll(%q): %v", dir, err)
	}
	content := "---\ntransform_version: \"1.0.0\"\nversion: \"2.0.0\"\n---\n" +
		"<Identity type=\"core\">\nHarness-only eligible content.\n</Identity>\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writeScanEligibleFile: WriteFile: %v", err)
	}
}

// writeScanNeitherFile writes a .md file that is neither catalog-matched nor harness-only
// eligible (no id, no transform_version, no canonical sections). Used for "neither" tests.
func writeScanNeitherFile(t *testing.T, dir, filename string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeScanNeitherFile: MkdirAll(%q): %v", dir, err)
	}
	content := "---\nversion: \"1.0.0\"\n---\nPlain markdown without MOSAIC boundary tags.\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writeScanNeitherFile: WriteFile: %v", err)
	}
}

// writeScanFileWithIDAndEligible writes a .md file that has BOTH a numeric id (for catalog
// matching) AND the two harness-only eligibility signals. Used to verify that a catalog-matched
// file never also appears in HarnessOnly (disjoint-by-construction invariant).
func writeScanFileWithIDAndEligible(t *testing.T, dir, filename, numericID string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeScanFileWithIDAndEligible: MkdirAll(%q): %v", dir, err)
	}
	content := "---\nid: \"" + numericID + "\"\ntransform_version: \"1.0.0\"\nversion: \"1.0.0\"\n---\n" +
		"<Identity type=\"core\">\nCatalog-backed agent also bearing harness-only signals.\n</Identity>\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writeScanFileWithIDAndEligible: WriteFile: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Catalog matching by numeric id (MatchByNumericID)
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_NumericIDMatch_FileInMatched verifies that a deployed file whose
// frontmatter `id` scalar resolves via catalog.AgentByNumericID is reported in Matched.
// This is the primary catalog-match path and must work regardless of the file's name.
func TestScanWorkspaceAgents_NumericIDMatch_FileInMatched(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := "agents"
	dir := filepath.Join(workspace, agentsDir)
	writeScanFileWithID(t, dir, "worker-agent.md", "5")

	cat := newScanCatalog()
	cat.byNumericID["5"] = domain.Agent{Key: "worker-agent", NumericID: "5", Role: domain.RoleSubagent}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert
	if len(scan.Matched) == 0 {
		t.Fatalf("scan.Matched is empty; expected one entry for file with id=5 matching catalog agent "+
			"worker-agent. scanWorkspaceAgents must return a Matched entry when AgentByNumericID resolves.")
	}
	if scan.Matched[0].FileName != "worker-agent.md" {
		t.Errorf("scan.Matched[0].FileName = %q, want %q; "+
			"FileName must be the base file name including extension, not a path",
			scan.Matched[0].FileName, "worker-agent.md")
	}
	wantTargetPath := filepath.Join(agentsDir, "worker-agent.md")
	if scan.Matched[0].TargetPath != wantTargetPath {
		t.Errorf("scan.Matched[0].TargetPath = %q, want %q; "+
			"TargetPath must be filepath.Join(agentsDir, fileName)",
			scan.Matched[0].TargetPath, wantTargetPath)
	}
}

// TestScanWorkspaceAgents_NumericIDMatch_MatchedByIsNumericID verifies that when a file is
// resolved via AgentByNumericID, the resulting ScannedAgentMatch carries MatchedBy == MatchByNumericID.
func TestScanWorkspaceAgents_NumericIDMatch_MatchedByIsNumericID(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := "agents"
	writeScanFileWithID(t, filepath.Join(workspace, agentsDir), "worker-agent.md", "5")

	cat := newScanCatalog()
	cat.byNumericID["5"] = domain.Agent{Key: "worker-agent", NumericID: "5", Role: domain.RoleSubagent}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert
	if len(scan.Matched) == 0 {
		t.Fatal("scan.Matched is empty; cannot check MatchedBy")
	}
	if scan.Matched[0].MatchedBy != MatchByNumericID {
		t.Errorf("scan.Matched[0].MatchedBy = %q, want %q; "+
			"a file resolved by AgentByNumericID must carry MatchedBy == MatchByNumericID",
			scan.Matched[0].MatchedBy, MatchByNumericID)
	}
}

// TestScanWorkspaceAgents_NumericIDMatch_AgentKeyFromCatalog verifies that AgentKey in the
// Matched entry comes from the catalog record, not the file name. When both agree this is
// trivially satisfied; when they differ (renamed file), the catalog key must win.
func TestScanWorkspaceAgents_NumericIDMatch_AgentKeyFromCatalog(t *testing.T) {
	// Arrange — file named "old-name.md" (key "old-name") but catalog key is "new-name"
	workspace := t.TempDir()
	agentsDir := "agents"
	writeScanFileWithID(t, filepath.Join(workspace, agentsDir), "old-name.md", "7")

	cat := newScanCatalog()
	cat.byNumericID["7"] = domain.Agent{Key: "new-name", NumericID: "7", Role: domain.RoleSubagent}
	// "old-name" key deliberately absent from catalog so the filename-key lookup would fail.

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert
	if len(scan.Matched) == 0 {
		t.Fatal("scan.Matched is empty; a renamed file must still be matched by numeric id")
	}
	if scan.Matched[0].AgentKey != "new-name" {
		t.Errorf("scan.Matched[0].AgentKey = %q, want %q; "+
			"AgentKey must come from the catalog record, not the file name, "+
			"so a renamed deployed file carries its true catalog key",
			scan.Matched[0].AgentKey, "new-name")
	}
}

// TestScanWorkspaceAgents_NumericIDMatch_NumericIDCarried verifies that the NumericID field
// of the Matched entry contains the frontmatter `id` scalar as written in the deployed file.
func TestScanWorkspaceAgents_NumericIDMatch_NumericIDCarried(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := "agents"
	writeScanFileWithID(t, filepath.Join(workspace, agentsDir), "worker-agent.md", "5")

	cat := newScanCatalog()
	cat.byNumericID["5"] = domain.Agent{Key: "worker-agent", NumericID: "5"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert
	if len(scan.Matched) == 0 {
		t.Fatal("scan.Matched is empty; cannot check NumericID")
	}
	if scan.Matched[0].NumericID != "5" {
		t.Errorf("scan.Matched[0].NumericID = %q, want %q; "+
			"NumericID must be the frontmatter `id` scalar as written in the deployed file",
			scan.Matched[0].NumericID, "5")
	}
}

// TestScanWorkspaceAgents_NumericIDTakesPriorityOverFilenameKey verifies that when a file's
// numeric id resolves to catalog agent A and its filename key resolves to catalog agent B,
// the numeric-id match wins (first-hit rule). The file is reported as Matched for agent A.
func TestScanWorkspaceAgents_NumericIDTakesPriorityOverFilenameKey(t *testing.T) {
	// Arrange — file "agent-b.md" has id "5" which matches "agent-a" by numeric id,
	// and the filename key "agent-b" also exists in the catalog.
	workspace := t.TempDir()
	agentsDir := "agents"
	writeScanFileWithID(t, filepath.Join(workspace, agentsDir), "agent-b.md", "5")

	cat := newScanCatalog()
	cat.byNumericID["5"] = domain.Agent{Key: "agent-a", NumericID: "5"}
	cat.byKey["agent-b"] = domain.Agent{Key: "agent-b", NumericID: "9"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert
	if len(scan.Matched) == 0 {
		t.Fatal("scan.Matched is empty; cannot check priority")
	}
	if scan.Matched[0].AgentKey != "agent-a" {
		t.Errorf("scan.Matched[0].AgentKey = %q, want %q; "+
			"numeric-id lookup must win over filename-key lookup when both resolve (first-hit rule)",
			scan.Matched[0].AgentKey, "agent-a")
	}
	if scan.Matched[0].MatchedBy != MatchByNumericID {
		t.Errorf("scan.Matched[0].MatchedBy = %q, want %q; "+
			"MatchedBy must reflect that the numeric-id path resolved first",
			scan.Matched[0].MatchedBy, MatchByNumericID)
	}
}

// ---------------------------------------------------------------------------
// Catalog matching by filename key (MatchByFileNameKey)
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_FilenameKeyMatch_FileInMatched verifies that a deployed file with
// no numeric id whose filename key resolves via catalog.Agent is reported in Matched.
func TestScanWorkspaceAgents_FilenameKeyMatch_FileInMatched(t *testing.T) {
	// Arrange — file has no id field; its filename key "utility-tool" is in the catalog.
	workspace := t.TempDir()
	agentsDir := "agents"
	writeScanFileNoID(t, filepath.Join(workspace, agentsDir), "utility-tool.md")

	cat := newScanCatalog()
	cat.byKey["utility-tool"] = domain.Agent{Key: "utility-tool", Role: domain.RoleUtility}
	// byNumericID is empty — numeric-id lookup will fail.

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert
	if len(scan.Matched) == 0 {
		t.Fatalf("scan.Matched is empty; expected one Matched entry for file utility-tool.md "+
			"whose filename key resolves via catalog.Agent. "+
			"scanWorkspaceAgents must use the filename-key fallback when no numeric id is present.")
	}
}

// TestScanWorkspaceAgents_FilenameKeyMatch_MatchedByIsFilenameKey verifies that when a file
// is resolved via catalog.Agent (filename-key fallback), the Matched entry carries
// MatchedBy == MatchByFileNameKey.
func TestScanWorkspaceAgents_FilenameKeyMatch_MatchedByIsFilenameKey(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := "agents"
	writeScanFileNoID(t, filepath.Join(workspace, agentsDir), "utility-tool.md")

	cat := newScanCatalog()
	cat.byKey["utility-tool"] = domain.Agent{Key: "utility-tool"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert
	if len(scan.Matched) == 0 {
		t.Fatal("scan.Matched is empty; cannot check MatchedBy")
	}
	if scan.Matched[0].MatchedBy != MatchByFileNameKey {
		t.Errorf("scan.Matched[0].MatchedBy = %q, want %q; "+
			"a file resolved via catalog.Agent (filename key) must carry MatchedBy == MatchByFileNameKey",
			scan.Matched[0].MatchedBy, MatchByFileNameKey)
	}
}

// TestScanWorkspaceAgents_FilenameKeyFallback_NumericIDFailsThenKeySucceeds verifies the
// fallback path: a file whose numeric id is not in the catalog but whose filename key is
// resolved via catalog.Agent ends up in Matched via the filename-key mechanism.
func TestScanWorkspaceAgents_FilenameKeyFallback_NumericIDFailsThenKeySucceeds(t *testing.T) {
	// Arrange — file has id "99" which is NOT in catalog by numeric id, but filename key
	// "known-agent" IS in catalog.
	workspace := t.TempDir()
	agentsDir := "agents"
	writeScanFileWithID(t, filepath.Join(workspace, agentsDir), "known-agent.md", "99")

	cat := newScanCatalog()
	// byNumericID has no entry for "99" — numeric-id lookup fails.
	cat.byKey["known-agent"] = domain.Agent{Key: "known-agent", NumericID: "42"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert
	if len(scan.Matched) == 0 {
		t.Fatal("scan.Matched is empty; expected a Matched entry via filename key when numeric id fails")
	}
	if scan.Matched[0].MatchedBy != MatchByFileNameKey {
		t.Errorf("scan.Matched[0].MatchedBy = %q, want %q; "+
			"must use filename-key fallback when numeric-id lookup returns no match",
			scan.Matched[0].MatchedBy, MatchByFileNameKey)
	}
}

// TestScanWorkspaceAgents_AgentDotMdSuffix_StrippedCorrectly verifies that a file named
// "orchestrator.agent.md" derives key "orchestrator" (not "orchestrator.agent") and can be
// matched via that key in the catalog.
func TestScanWorkspaceAgents_AgentDotMdSuffix_StrippedCorrectly(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := "agents"
	writeScanFileNoID(t, filepath.Join(workspace, agentsDir), "orchestrator.agent.md")

	cat := newScanCatalog()
	cat.byKey["orchestrator"] = domain.Agent{Key: "orchestrator", Role: domain.RoleOrchestrator}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert
	if len(scan.Matched) == 0 {
		t.Fatal("scan.Matched is empty; orchestrator.agent.md must match via key 'orchestrator' " +
			"after stripping the .agent.md suffix")
	}
	if scan.Matched[0].AgentKey != "orchestrator" {
		t.Errorf("scan.Matched[0].AgentKey = %q, want %q; "+
			"agentKeyFromFileName must strip .agent.md suffix so orchestrator.agent.md derives key 'orchestrator'",
			scan.Matched[0].AgentKey, "orchestrator")
	}
}

// ---------------------------------------------------------------------------
// Harness-only: unmatched eligible files
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_UnmatchedEligible_InHarnessOnly verifies that a file that fails
// both catalog lookups but passes the two-signal eligibility check appears in HarnessOnly.
func TestScanWorkspaceAgents_UnmatchedEligible_InHarnessOnly(t *testing.T) {
	// Arrange — eligible file with no catalog match.
	workspace := t.TempDir()
	agentsDir := "agents"
	writeScanEligibleFile(t, filepath.Join(workspace, agentsDir), "external-tool.md")

	cat := newScanCatalog() // empty catalog — no matches

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert
	if len(scan.HarnessOnly) == 0 {
		t.Fatalf("scan.HarnessOnly is empty; expected external-tool.md to appear in HarnessOnly "+
			"(no catalog match, both eligibility signals present). "+
			"scanWorkspaceAgents must route unmatched eligible files to HarnessOnly.")
	}
}

// TestScanWorkspaceAgents_UnmatchedEligible_NotInMatched verifies that an unmatched
// harness-only eligible file does not also appear in Matched.
func TestScanWorkspaceAgents_UnmatchedEligible_NotInMatched(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := "agents"
	writeScanEligibleFile(t, filepath.Join(workspace, agentsDir), "external-tool.md")

	cat := newScanCatalog()

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert — Matched must be empty (the file is harness-only, not catalog-matched).
	if len(scan.Matched) != 0 {
		t.Errorf("scan.Matched has %d entries, want 0; "+
			"a file in HarnessOnly must not also appear in Matched (disjoint-by-construction invariant)",
			len(scan.Matched))
	}
}

// ---------------------------------------------------------------------------
// Neither: unmatched ineligible files
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_NeitherMatchedNorEligible_AbsentFromBothSlices verifies that a
// file that fails both catalog lookups and does not pass eligibility produces no entry in
// either slice. The file must be left byte-identical and completely ignored.
func TestScanWorkspaceAgents_NeitherMatchedNorEligible_AbsentFromBothSlices(t *testing.T) {
	// Arrange — a plain .md file with no id, no transform_version, no canonical sections.
	workspace := t.TempDir()
	agentsDir := "agents"
	writeScanNeitherFile(t, filepath.Join(workspace, agentsDir), "random.md")

	cat := newScanCatalog()

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert — both slices must be empty.
	if len(scan.Matched) != 0 {
		t.Errorf("scan.Matched has %d entries, want 0; "+
			"a plain non-MOSAIC file must not appear in Matched", len(scan.Matched))
	}
	if len(scan.HarnessOnly) != 0 {
		t.Errorf("scan.HarnessOnly has %d entries, want 0; "+
			"a plain non-MOSAIC file must not appear in HarnessOnly", len(scan.HarnessOnly))
	}
}

// ---------------------------------------------------------------------------
// Disjoint invariant
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_CatalogMatchedFile_NeverInHarnessOnly verifies the disjoint
// invariant: a file that is in Matched (resolved by numeric id) must never also appear in
// HarnessOnly, even when it carries both harness-only eligibility signals.
func TestScanWorkspaceAgents_CatalogMatchedFile_NeverInHarnessOnly(t *testing.T) {
	// Arrange — file carries both id="5" and transform_version + canonical section.
	// It should be Matched (catalog wins), not HarnessOnly.
	workspace := t.TempDir()
	agentsDir := "agents"
	writeScanFileWithIDAndEligible(t, filepath.Join(workspace, agentsDir), "dual-signal.md", "5")

	cat := newScanCatalog()
	cat.byNumericID["5"] = domain.Agent{Key: "worker-agent", NumericID: "5"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert — the file must appear in Matched, not in HarnessOnly.
	if len(scan.Matched) == 0 {
		t.Error("scan.Matched is empty; file with id=5 matching catalog must be in Matched")
	}
	for _, h := range scan.HarnessOnly {
		if h.FileName == "dual-signal.md" {
			t.Errorf("scan.HarnessOnly contains dual-signal.md; a catalog-matched file must "+
				"never also appear in HarnessOnly (disjoint-by-construction invariant). "+
				"TargetPath: %q", h.TargetPath)
		}
	}
}

// ---------------------------------------------------------------------------
// Tolerance: silently skipped entries
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_NonMdFile_Skipped verifies that a non-.md file in the agents
// directory produces no entry in either slice and does not cause the scan to fail.
func TestScanWorkspaceAgents_NonMdFile_Skipped(t *testing.T) {
	// Arrange — a .yaml file that would be a catalog match if it had the right extension.
	workspace := t.TempDir()
	agentsDir := "agents"
	dir := filepath.Join(workspace, agentsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("id: \"5\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cat := newScanCatalog()
	cat.byNumericID["5"] = domain.Agent{Key: "worker-agent", NumericID: "5"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert — non-.md file must not appear in either slice.
	if len(scan.Matched) != 0 || len(scan.HarnessOnly) != 0 {
		t.Errorf("scan has unexpected entries (Matched=%d, HarnessOnly=%d) for a non-.md file; "+
			"only .md files may be considered", len(scan.Matched), len(scan.HarnessOnly))
	}
}

// TestScanWorkspaceAgents_SubdirectoryEntry_Skipped verifies that a directory entry inside
// the agents directory does not cause the scan to recurse or fail. The walk is non-recursive.
func TestScanWorkspaceAgents_SubdirectoryEntry_Skipped(t *testing.T) {
	// Arrange — create a subdirectory inside the agents dir.
	workspace := t.TempDir()
	agentsDir := "agents"
	subdir := filepath.Join(workspace, agentsDir, "subdir.md") // directory ending in .md
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(subdir): %v", err)
	}
	// Also create a regular file inside the subdir to confirm non-recursion.
	if err := os.WriteFile(filepath.Join(subdir, "nested.md"), []byte("---\nid: \"5\"\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile nested: %v", err)
	}

	cat := newScanCatalog()
	cat.byNumericID["5"] = domain.Agent{Key: "worker-agent", NumericID: "5"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert — the subdirectory must be skipped; the nested file must not be found.
	if len(scan.Matched) != 0 || len(scan.HarnessOnly) != 0 {
		t.Errorf("scan has unexpected entries (Matched=%d, HarnessOnly=%d) for a subdirectory; "+
			"the walk must be non-recursive and must skip directory entries", len(scan.Matched), len(scan.HarnessOnly))
	}
}

// TestScanWorkspaceAgents_UnparseableFile_SkippedSilently verifies that a file with invalid
// YAML frontmatter is skipped silently without crashing the scan or returning an error.
func TestScanWorkspaceAgents_UnparseableFile_SkippedSilently(t *testing.T) {
	// Arrange — duplicate key makes YAML unparseable.
	workspace := t.TempDir()
	agentsDir := "agents"
	dir := filepath.Join(workspace, agentsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Duplicate `id` key causes docformat.Parse to fail on this file.
	if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte("---\nid: \"1\"\nid: \"2\"\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cat := newScanCatalog()
	cat.byNumericID["1"] = domain.Agent{Key: "some-agent", NumericID: "1"}

	// Act — must not panic
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert — the unparseable file must produce no entries.
	if len(scan.Matched) != 0 || len(scan.HarnessOnly) != 0 {
		t.Errorf("scan has unexpected entries (Matched=%d, HarnessOnly=%d) for an unparseable file; "+
			"unparseable files must be skipped silently", len(scan.Matched), len(scan.HarnessOnly))
	}
}

// TestScanWorkspaceAgents_NoFrontmatterFile_SkippedSilently verifies that a file with no
// frontmatter block is skipped silently. Without frontmatter there is no id to look up, and
// the harness-only eligibility check also requires frontmatter, so such a file is neither.
func TestScanWorkspaceAgents_NoFrontmatterFile_SkippedSilently(t *testing.T) {
	// Arrange — plain markdown file, no frontmatter delimiters.
	workspace := t.TempDir()
	agentsDir := "agents"
	dir := filepath.Join(workspace, agentsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nofm.md"), []byte("# Just a markdown file\nNo frontmatter.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cat := newScanCatalog()

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert
	if len(scan.Matched) != 0 || len(scan.HarnessOnly) != 0 {
		t.Errorf("scan has unexpected entries (Matched=%d, HarnessOnly=%d) for a file without frontmatter; "+
			"such files must be skipped silently", len(scan.Matched), len(scan.HarnessOnly))
	}
}

// ---------------------------------------------------------------------------
// Empty / absent directory
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_EmptyAgentsDir_ReturnsEmptyScan verifies that an empty agentsDir
// string yields an empty scan without touching the workspace root.
func TestScanWorkspaceAgents_EmptyAgentsDir_ReturnsEmptyScan(t *testing.T) {
	// Arrange — workspace root has a .md file that should NOT be scanned.
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "should-not-be-scanned.md"),
		[]byte("---\nid: \"5\"\n---\nContent.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cat := newScanCatalog()
	cat.byNumericID["5"] = domain.Agent{Key: "some-agent", NumericID: "5"}

	// Act — empty agentsDir must yield empty scan
	scan := scanWorkspaceAgents(workspace, "", cat)

	// Assert
	if len(scan.Matched) != 0 || len(scan.HarnessOnly) != 0 {
		t.Errorf("scan has entries (Matched=%d, HarnessOnly=%d) for empty agentsDir; "+
			"an empty agentsDir must yield an empty scan without touching the workspace root",
			len(scan.Matched), len(scan.HarnessOnly))
	}
}

// TestScanWorkspaceAgents_AbsentDirectory_ReturnsEmptyScan verifies that a non-existent
// agents directory yields an empty scan rather than an error.
func TestScanWorkspaceAgents_AbsentDirectory_ReturnsEmptyScan(t *testing.T) {
	// Arrange — workspace exists but the agents dir does not.
	workspace := t.TempDir()
	cat := newScanCatalog()

	// Act — must not panic; directory simply doesn't exist
	scan := scanWorkspaceAgents(workspace, "nonexistent-agents-dir", cat)

	// Assert
	if len(scan.Matched) != 0 || len(scan.HarnessOnly) != 0 {
		t.Errorf("scan has entries (Matched=%d, HarnessOnly=%d) for an absent directory; "+
			"an absent or unreadable directory must yield an empty scan rather than an error",
			len(scan.Matched), len(scan.HarnessOnly))
	}
}

// ---------------------------------------------------------------------------
// Ambiguous duplicate numeric id
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_DuplicateNumericID_BothFilesInMatched verifies that when two
// deployed files declare the same numeric id, both appear in Matched. The scan does not
// pick between them — it reports both and leaves the collision to resolveDeployedPath.
func TestScanWorkspaceAgents_DuplicateNumericID_BothFilesInMatched(t *testing.T) {
	// Arrange — two files with the same numeric id "12".
	workspace := t.TempDir()
	agentsDir := "agents"
	dir := filepath.Join(workspace, agentsDir)
	writeScanFileWithID(t, dir, "alpha.md", "12")
	writeScanFileWithID(t, dir, "beta.md", "12")

	cat := newScanCatalog()
	cat.byNumericID["12"] = domain.Agent{Key: "worker-agent", NumericID: "12"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert — both files must appear in Matched.
	if len(scan.Matched) < 2 {
		t.Errorf("scan.Matched has %d entries, want >= 2; "+
			"both files declaring id=12 must be reported in Matched "+
			"so the collision can be surfaced by resolveDeployedPath",
			len(scan.Matched))
	}
}

// TestScanWorkspaceAgents_DuplicateNumericID_MatchedKeysDeduplicates verifies that
// MatchedKeys() deduplicates when two files resolve to the same catalog key, so the key
// appears exactly once in the returned slice.
func TestScanWorkspaceAgents_DuplicateNumericID_MatchedKeysDeduplicates(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := "agents"
	dir := filepath.Join(workspace, agentsDir)
	writeScanFileWithID(t, dir, "alpha.md", "12")
	writeScanFileWithID(t, dir, "beta.md", "12")

	cat := newScanCatalog()
	cat.byNumericID["12"] = domain.Agent{Key: "worker-agent", NumericID: "12"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)
	keys := scan.MatchedKeys()

	// Assert — key must appear exactly once.
	count := 0
	for _, k := range keys {
		if k == "worker-agent" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("MatchedKeys() contains %d occurrences of %q, want 1; "+
			"MatchedKeys() must deduplicate so a key contributed by multiple files appears once",
			count, "worker-agent")
	}
}

// ---------------------------------------------------------------------------
// Determinism: sorted results
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_MatchedSlice_SortedByTargetPath verifies that the Matched slice
// is sorted by TargetPath. Deterministic ordering prevents snapshot-diff flakiness and
// makes test assertions on indices reliable.
func TestScanWorkspaceAgents_MatchedSlice_SortedByTargetPath(t *testing.T) {
	// Arrange — three files whose alphabetical TargetPath order is c < a < b when joined.
	// We name them to produce a deterministic TargetPath order.
	workspace := t.TempDir()
	agentsDir := "agents"
	dir := filepath.Join(workspace, agentsDir)
	writeScanFileWithID(t, dir, "charlie.md", "1")
	writeScanFileWithID(t, dir, "alpha.md", "2")
	writeScanFileWithID(t, dir, "bravo.md", "3")

	cat := newScanCatalog()
	cat.byNumericID["1"] = domain.Agent{Key: "charlie-agent", NumericID: "1"}
	cat.byNumericID["2"] = domain.Agent{Key: "alpha-agent", NumericID: "2"}
	cat.byNumericID["3"] = domain.Agent{Key: "bravo-agent", NumericID: "3"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert — sorted by TargetPath (agents/alpha.md < agents/bravo.md < agents/charlie.md).
	if len(scan.Matched) < 3 {
		t.Fatalf("scan.Matched has %d entries, want 3; cannot check sort order", len(scan.Matched))
	}
	for i := 1; i < len(scan.Matched); i++ {
		if scan.Matched[i].TargetPath < scan.Matched[i-1].TargetPath {
			t.Errorf("scan.Matched is not sorted by TargetPath: entry[%d].TargetPath=%q < entry[%d].TargetPath=%q; "+
				"Matched must be sorted ascending by TargetPath for determinism",
				i, scan.Matched[i].TargetPath, i-1, scan.Matched[i-1].TargetPath)
		}
	}
}

// TestScanWorkspaceAgents_MatchedKeys_Sorted verifies that MatchedKeys() returns a
// lexicographically sorted slice of agent keys.
func TestScanWorkspaceAgents_MatchedKeys_Sorted(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := "agents"
	dir := filepath.Join(workspace, agentsDir)
	writeScanFileWithID(t, dir, "z-file.md", "1")
	writeScanFileWithID(t, dir, "a-file.md", "2")
	writeScanFileWithID(t, dir, "m-file.md", "3")

	cat := newScanCatalog()
	cat.byNumericID["1"] = domain.Agent{Key: "z-agent", NumericID: "1"}
	cat.byNumericID["2"] = domain.Agent{Key: "a-agent", NumericID: "2"}
	cat.byNumericID["3"] = domain.Agent{Key: "m-agent", NumericID: "3"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)
	keys := scan.MatchedKeys()

	// Assert — must be sorted: a-agent < m-agent < z-agent.
	if len(keys) < 3 {
		t.Fatalf("MatchedKeys() returned %d keys, want 3; cannot check sort order", len(keys))
	}
	for i := 1; i < len(keys); i++ {
		if keys[i] < keys[i-1] {
			t.Errorf("MatchedKeys() is not sorted: keys[%d]=%q < keys[%d]=%q; "+
				"MatchedKeys() must return a lexicographically sorted slice",
				i, keys[i], i-1, keys[i-1])
		}
	}
}

// TestScanWorkspaceAgents_HarnessOnlySlice_SortedByTargetPath verifies that the HarnessOnly
// slice is sorted by TargetPath.
func TestScanWorkspaceAgents_HarnessOnlySlice_SortedByTargetPath(t *testing.T) {
	// Arrange — two eligible files.
	workspace := t.TempDir()
	agentsDir := "agents"
	dir := filepath.Join(workspace, agentsDir)
	writeScanEligibleFile(t, dir, "z-tool.md")
	writeScanEligibleFile(t, dir, "a-tool.md")

	cat := newScanCatalog() // empty — both files have no catalog match

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert — sorted by TargetPath (agents/a-tool.md < agents/z-tool.md).
	if len(scan.HarnessOnly) < 2 {
		t.Fatalf("scan.HarnessOnly has %d entries, want 2; cannot check sort order", len(scan.HarnessOnly))
	}
	for i := 1; i < len(scan.HarnessOnly); i++ {
		if scan.HarnessOnly[i].TargetPath < scan.HarnessOnly[i-1].TargetPath {
			t.Errorf("scan.HarnessOnly is not sorted by TargetPath: entry[%d]=%q < entry[%d]=%q",
				i, scan.HarnessOnly[i].TargetPath, i-1, scan.HarnessOnly[i-1].TargetPath)
		}
	}
}

// ---------------------------------------------------------------------------
// Mixed directory: all three outcomes coexist
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_MixedDirectory_ThreeOutcomesClassifiedCorrectly verifies the full
// three-way classification over a realistic mixed agents directory: one catalog-matched file,
// one harness-only file, and one neither-file — all coexisting in the same directory.
func TestScanWorkspaceAgents_MixedDirectory_ThreeOutcomesClassifiedCorrectly(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := "agents"
	dir := filepath.Join(workspace, agentsDir)
	// Catalog-matched by numeric id.
	writeScanFileWithID(t, dir, "catalog-agent.md", "10")
	// Harness-only eligible (no catalog match).
	writeScanEligibleFile(t, dir, "external-tool.md")
	// Neither matched nor eligible.
	writeScanNeitherFile(t, dir, "random-file.md")

	cat := newScanCatalog()
	cat.byNumericID["10"] = domain.Agent{Key: "catalog-agent", NumericID: "10"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert — exactly one entry in each slice.
	if len(scan.Matched) != 1 {
		t.Errorf("scan.Matched has %d entries, want 1 (catalog-agent.md)", len(scan.Matched))
	}
	if len(scan.HarnessOnly) != 1 {
		t.Errorf("scan.HarnessOnly has %d entries, want 1 (external-tool.md)", len(scan.HarnessOnly))
	}
	// Verify the Matched entry is for the right agent.
	if len(scan.Matched) > 0 && scan.Matched[0].AgentKey != "catalog-agent" {
		t.Errorf("scan.Matched[0].AgentKey = %q, want %q", scan.Matched[0].AgentKey, "catalog-agent")
	}
	// Verify the HarnessOnly entry is for the right file.
	if len(scan.HarnessOnly) > 0 && scan.HarnessOnly[0].FileName != "external-tool.md" {
		t.Errorf("scan.HarnessOnly[0].FileName = %q, want %q", scan.HarnessOnly[0].FileName, "external-tool.md")
	}
}

// ---------------------------------------------------------------------------
// Tolerance: unreadable file (Linux/macOS only)
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_UnreadableFile_SkippedSilently verifies that a file whose read
// permission has been removed is skipped silently without crashing the scan or returning an
// error. The walk's tolerance contract (identical to buildDeployedAgentIndex) must apply to
// unreadable files as well as unparseable ones.
//
// This test is skipped on Windows because os.Chmod(file, 0) does not reliably prevent reads
// on that platform — the kernel enforces NTFS ACLs separately from the POSIX permission bits
// that Chmod sets, and the running process often retains read access regardless.
func TestScanWorkspaceAgents_UnreadableFile_SkippedSilently(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skipf("chmod 0 does not reliably prevent reads on Windows; "+
			"this test requires Linux or macOS where POSIX permission bits are enforced")
	}
	// Arrange — create a catalog-matching .md file, then remove its read permission.
	workspace := t.TempDir()
	agentsDir := "agents"
	dir := filepath.Join(workspace, agentsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	target := filepath.Join(dir, "locked-agent.md")
	if err := os.WriteFile(target, []byte("---\nid: \"5\"\n---\nContent.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(target, 0o000); err != nil {
		t.Fatalf("Chmod 0: %v", err)
	}
	t.Cleanup(func() {
		// Restore permission so TempDir cleanup can remove the file.
		_ = os.Chmod(target, 0o644)
	})

	cat := newScanCatalog()
	cat.byNumericID["5"] = domain.Agent{Key: "locked-agent", NumericID: "5"}

	// Act — must not panic; the unreadable file must be skipped silently.
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert — the unreadable file must produce no entries in either slice.
	if len(scan.Matched) != 0 || len(scan.HarnessOnly) != 0 {
		t.Errorf("scan has unexpected entries (Matched=%d, HarnessOnly=%d) for an unreadable file; "+
			"an unreadable file must be skipped silently, matching the tolerance contract of buildDeployedAgentIndex",
			len(scan.Matched), len(scan.HarnessOnly))
	}
}

// ---------------------------------------------------------------------------
// MatchedKeys() contract
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_EmptyScan_MatchedKeysReturnsNil verifies that MatchedKeys() on an
// empty WorkspaceAgentScan returns nil (or an empty slice) without panicking.
func TestScanWorkspaceAgents_EmptyScan_MatchedKeysReturnsNil(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := "agents"
	if err := os.MkdirAll(filepath.Join(workspace, agentsDir), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cat := newScanCatalog()

	// Act — must not panic
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)
	keys := scan.MatchedKeys()

	// Assert — nil or empty slice, no panic.
	if len(keys) != 0 {
		t.Errorf("MatchedKeys() returned %d keys for an empty scan, want 0", len(keys))
	}
}
