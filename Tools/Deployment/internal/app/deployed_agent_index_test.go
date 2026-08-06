package app

// deployed_agent_index_test.go verifies buildDeployedAgentIndex (workspace scan) and
// resolveDeployedPath (per-agent resolution), both introduced in Stage 8.
//
// Because buildDeployedAgentIndex and resolveDeployedPath are package-internal, this file
// belongs to package app (not package app_test), giving it direct access to both functions.
//
// Verified behaviors:
//
// Workspace scan (buildDeployedAgentIndex):
//   - A file whose frontmatter carries an `id` scalar is indexed under that id.
//   - The indexed TargetPath is relative to the deployment root (workspace).
//   - A file with no `id` in frontmatter is skipped silently; no index entry is created.
//   - A file whose YAML frontmatter is unparseable is skipped silently.
//   - An empty agentsDir produces an empty index without panicking.
//
// Renamed-file detection (ID-based identification):
//   - A deployed file whose base name differs from the catalog agent's key is still found
//     by its frontmatter `id`, at its actual path.
//
// Per-agent resolution (resolveDeployedPath):
//   - An agent with NumericID matched by exactly one index entry returns that entry's path.
//   - An agent with NumericID that has zero index entries returns "", nil (not deployed).
//     No name-based fallback path is returned.
//   - An agent with NumericID that has more than one index entry returns ErrAmbiguousDeployedID.
//   - An agent with an empty NumericID (utility, orchestrator) returns fallbackPath.
//
// Collision detection:
//   - Two deployed files declaring the same id both appear as index entries.
//
// End-to-end:
//   - The whole-location scan correctly indexes all id-carrying files in the agents directory.
//   - The full pipeline (scan + resolution) correctly handles a mix of id-based, renamed,
//     not-deployed, and no-id agents.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// writeDeployedFileWithID writes a minimal deployed file at filepath.Join(dir, filename)
// with the given frontmatter `id` scalar. The file is parseable by docformat.Parse.
func writeDeployedFileWithID(t *testing.T, dir, filename, numericID string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeDeployedFileWithID MkdirAll(%q): %v", dir, err)
	}
	content := "---\nid: \"" + numericID + "\"\nversion: \"1.0.0\"\n---\nDeployed content.\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writeDeployedFileWithID WriteFile: %v", err)
	}
}

// writeDeployedFileNoID writes a minimal deployed file at filepath.Join(dir, filename)
// with no `id` field in the frontmatter. Such files must be skipped by the index scan.
func writeDeployedFileNoID(t *testing.T, dir, filename string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeDeployedFileNoID MkdirAll(%q): %v", dir, err)
	}
	content := "---\nversion: \"1.0.0\"\n---\nDeployed content.\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writeDeployedFileNoID WriteFile: %v", err)
	}
}

// writeUnparseableDeployedFile writes a file at filepath.Join(dir, filename) with invalid
// YAML frontmatter (duplicate key). docformat.Parse fails on such files; they must be
// skipped without crashing the scan.
func writeUnparseableDeployedFile(t *testing.T, dir, filename string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeUnparseableDeployedFile MkdirAll(%q): %v", dir, err)
	}
	// Duplicate `id` key causes docformat.Parse to fail.
	content := "---\nid: \"1\"\nid: \"2\"\n---\nContent.\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writeUnparseableDeployedFile WriteFile: %v", err)
	}
}

// ---------------------------------------------------------------------------
// buildDeployedAgentIndex — workspace scan behavior
// ---------------------------------------------------------------------------

// TestBuildDeployedAgentIndex_FileWithID_IsIndexed verifies that a deployed file whose
// frontmatter carries an `id` scalar is indexed under that id by the workspace scan.
func TestBuildDeployedAgentIndex_FileWithID_IsIndexed(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".mosaic"
	writeDeployedFileWithID(t, filepath.Join(workspace, agentsDir), "my-agent.md", "42")

	// Act
	index := buildDeployedAgentIndex(workspace, agentsDir)

	// Assert
	entries := index.Lookup("42")
	if len(entries) == 0 {
		t.Error("index.Lookup(\"42\") returned no entries; " +
			"a deployed file with frontmatter id \"42\" must be indexed under that id")
	}
}

// TestBuildDeployedAgentIndex_FileWithID_EntryContainsRelativePath verifies that the
// TargetPath in each index entry is relative to the deployment root (workspace), matching
// the format that probeDeployedArtifact expects.
func TestBuildDeployedAgentIndex_FileWithID_EntryContainsRelativePath(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".mosaic"
	writeDeployedFileWithID(t, filepath.Join(workspace, agentsDir), "my-agent.md", "42")

	// Act
	index := buildDeployedAgentIndex(workspace, agentsDir)

	// Assert
	entries := index.Lookup("42")
	if len(entries) == 0 {
		t.Fatal("index.Lookup(\"42\") returned no entries; cannot check TargetPath")
	}
	wantPath := filepath.Join(agentsDir, "my-agent.md")
	if entries[0].TargetPath != wantPath {
		t.Errorf("entry.TargetPath = %q, want %q; "+
			"TargetPath must be relative to the workspace root in the same format probeDeployedArtifact expects",
			entries[0].TargetPath, wantPath)
	}
}

// TestBuildDeployedAgentIndex_FileWithNoID_IsSkipped verifies that a deployed file whose
// frontmatter carries no `id` field is silently skipped — it produces no index entry and
// does not cause the scan to fail or panic.
func TestBuildDeployedAgentIndex_FileWithNoID_IsSkipped(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".mosaic"
	writeDeployedFileNoID(t, filepath.Join(workspace, agentsDir), "no-id.md")

	// Act — must return without panicking
	index := buildDeployedAgentIndex(workspace, agentsDir)

	// Assert — the index must be non-nil and contain no entries.
	if index == nil {
		t.Error("buildDeployedAgentIndex returned nil; expected a non-nil empty index")
	}
	if len(index) != 0 {
		t.Errorf("index has %d entries after scanning a file with no `id`; want 0", len(index))
	}
	// Lookup with empty key must not return the id-less file.
	if entries := index.Lookup(""); len(entries) != 0 {
		t.Error("index.Lookup(\"\") returned entries; id-less files must not be indexed under any key")
	}
}

// TestBuildDeployedAgentIndex_UnparseableFile_IsSkipped verifies that a file whose YAML
// frontmatter cannot be parsed is silently skipped. The scan must tolerate such files
// without panicking or returning an error.
func TestBuildDeployedAgentIndex_UnparseableFile_IsSkipped(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".mosaic"
	writeUnparseableDeployedFile(t, filepath.Join(workspace, agentsDir), "bad.md")

	// Act — must not panic
	index := buildDeployedAgentIndex(workspace, agentsDir)

	// Assert — the unparseable file must have produced no index entries.
	if index == nil {
		t.Error("buildDeployedAgentIndex returned nil; expected a non-nil empty index")
	}
	if len(index) != 0 {
		t.Errorf("index has %d entries after scanning only an unparseable file; "+
			"unparseable files must be skipped silently", len(index))
	}
}

// TestBuildDeployedAgentIndex_EmptyAgentsDir_ReturnsEmptyIndex verifies that when the
// agents directory is empty (no deployed files), the function returns a non-nil empty
// index without panicking.
func TestBuildDeployedAgentIndex_EmptyAgentsDir_ReturnsEmptyIndex(t *testing.T) {
	// Arrange — create the agents directory but leave it empty.
	workspace := t.TempDir()
	agentsDir := ".mosaic"
	if err := os.MkdirAll(filepath.Join(workspace, agentsDir), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Act
	index := buildDeployedAgentIndex(workspace, agentsDir)

	// Assert
	if index == nil {
		t.Error("buildDeployedAgentIndex returned nil for an empty directory; expected a non-nil empty index")
	}
	if len(index) != 0 {
		t.Errorf("index has %d entries for an empty directory; want 0", len(index))
	}
}

// ---------------------------------------------------------------------------
// T8.3 — Renamed file still found by id
// ---------------------------------------------------------------------------

// TestBuildDeployedAgentIndex_RenamedFile_FoundByID verifies that a deployed file whose
// base name no longer matches the catalog agent's key is still found by the numeric `id`
// in its frontmatter. This is the fundamental guarantee of ID-based identification: the
// durable identifier is the `id`, not the filename.
func TestBuildDeployedAgentIndex_RenamedFile_FoundByID(t *testing.T) {
	// Arrange
	// Catalog agent key:     "new-agent-name"  (after rename)
	// Deployed file name:    "old-agent-name.md"  (before rename, still on disk)
	// Frontmatter id:        "7"
	workspace := t.TempDir()
	agentsDir := ".mosaic"
	numericID := "7"
	writeDeployedFileWithID(t, filepath.Join(workspace, agentsDir), "old-agent-name.md", numericID)

	// Act
	index := buildDeployedAgentIndex(workspace, agentsDir)

	// Assert — the file must be found by id "7", not by its filename.
	entries := index.Lookup(numericID)
	if len(entries) == 0 {
		t.Errorf("index.Lookup(%q) returned no entries; "+
			"a deployed file must be found by its frontmatter `id` even when its filename "+
			"no longer matches the catalog agent's key", numericID)
		return
	}
	wantPath := filepath.Join(agentsDir, "old-agent-name.md")
	if entries[0].TargetPath != wantPath {
		t.Errorf("entry.TargetPath = %q, want %q; "+
			"the index entry must point to the file's actual path, not a name-derived path",
			entries[0].TargetPath, wantPath)
	}
}

// ---------------------------------------------------------------------------
// T8.4 — No name fallback for workflow / infrastructure agents
// ---------------------------------------------------------------------------

// TestResolveDeployedPath_WorkflowAgent_NoIndexEntry_ReturnsNotDeployed verifies that when
// a workflow-referenced agent has a numeric id and the id index contains no entry for that
// id, resolveDeployedPath returns ("", nil). No name-based fallback path is returned because
// the numeric id is the durable identifier for workflow and infrastructure agents.
func TestResolveDeployedPath_WorkflowAgent_NoIndexEntry_ReturnsNotDeployed(t *testing.T) {
	// Arrange — agent has a NumericID; the index has no entry for it.
	agent := domain.Agent{
		Key:       "my-workflow-agent",
		NumericID: "55",
		Role:      domain.RoleSubagent,
	}
	index := DeployedAgentIndex{} // empty — no deployed files
	fallbackPath := ".mosaic/my-workflow-agent.md"

	// Act
	path, err := resolveDeployedPath(agent, index, fallbackPath)

	// Assert
	if err != nil {
		t.Fatalf("resolveDeployedPath returned unexpected error: %v", err)
	}
	// The fallback path must NOT be returned — workflow agents have no name-based fallback.
	if path == fallbackPath {
		t.Errorf("resolveDeployedPath returned the name-based fallbackPath %q for an agent "+
			"with NumericID %q that has no index entry; "+
			"workflow agents must not fall back to name-based path matching when the id is absent",
			fallbackPath, agent.NumericID)
	}
	// A not-deployed result is represented by an empty path.
	if path != "" {
		t.Errorf("resolveDeployedPath = %q, want \"\" (not deployed); "+
			"zero index entries for a NumericID agent must yield an empty path, not a fallback",
			path)
	}
}

// TestResolveDeployedPath_InfrastructureAgent_NoIndexEntry_ReturnsNotDeployed verifies that
// the no-name-fallback rule applies equally to infrastructure agents. Infrastructure agents
// are MOSAIC agents with numeric ids; when their id is absent from the index, the run must
// treat them as not deployed rather than falling back to a name-derived path.
func TestResolveDeployedPath_InfrastructureAgent_NoIndexEntry_ReturnsNotDeployed(t *testing.T) {
	// Arrange
	agent := domain.Agent{
		Key:            "checkpoint-manager",
		NumericID:      "33",
		Role:           domain.RoleSubagent,
		Infrastructure: "checkpoint",
	}
	index := DeployedAgentIndex{} // empty
	fallbackPath := ".mosaic/checkpoint-manager.md"

	// Act
	path, err := resolveDeployedPath(agent, index, fallbackPath)

	// Assert
	if err != nil {
		t.Fatalf("resolveDeployedPath returned unexpected error: %v", err)
	}
	if path != "" {
		t.Errorf("resolveDeployedPath = %q, want \"\" (not deployed); "+
			"infrastructure agents must not fall back to name-based path matching "+
			"when their numeric id is absent from the index", path)
	}
}

// TestResolveDeployedPath_WorkflowAgent_IndexEntry_ReturnsIndexedPath verifies that when
// an agent's numeric id has exactly one index entry, resolveDeployedPath returns that
// entry's TargetPath. This is the happy-path for ID-based resolution.
func TestResolveDeployedPath_WorkflowAgent_IndexEntry_ReturnsIndexedPath(t *testing.T) {
	// Arrange
	agent := domain.Agent{Key: "my-agent", NumericID: "10"}
	index := DeployedAgentIndex{
		"10": {{NumericID: "10", TargetPath: ".mosaic/my-agent.md"}},
	}

	// Act
	path, err := resolveDeployedPath(agent, index, ".mosaic/different-name.md")

	// Assert
	if err != nil {
		t.Fatalf("resolveDeployedPath returned unexpected error: %v", err)
	}
	if path != ".mosaic/my-agent.md" {
		t.Errorf("resolveDeployedPath = %q, want %q; "+
			"a single index entry for the agent's NumericID must be returned as the target path",
			path, ".mosaic/my-agent.md")
	}
}

// ---------------------------------------------------------------------------
// T8.5 — Utility / non-MOSAIC agents use name-based fallback
// ---------------------------------------------------------------------------

// TestResolveDeployedPath_UtilityAgent_NoNumericID_UsesFallbackPath verifies that a
// utility agent with no numeric id is located via the harness-computed fallback path. The
// id index is not consulted for agents with an empty NumericID; the fallback path is
// returned unconditionally.
func TestResolveDeployedPath_UtilityAgent_NoNumericID_UsesFallbackPath(t *testing.T) {
	// Arrange — utility agent with no numeric id.
	agent := domain.Agent{
		Key:       "my-utility",
		NumericID: "", // no id
		Role:      domain.RoleUtility,
	}
	index := DeployedAgentIndex{} // irrelevant for no-id agents
	fallbackPath := ".mosaic/my-utility.md"

	// Act
	path, err := resolveDeployedPath(agent, index, fallbackPath)

	// Assert
	if err != nil {
		t.Fatalf("resolveDeployedPath returned unexpected error for utility agent: %v", err)
	}
	if path != fallbackPath {
		t.Errorf("resolveDeployedPath = %q, want fallbackPath %q; "+
			"a utility agent with no numeric id must use the harness-computed path",
			path, fallbackPath)
	}
}

// TestResolveDeployedPath_ThirdPartyAgent_NoNumericID_UsesFallbackPath verifies that a
// non-MOSAIC or third-party agent present in the workspace without a numeric id also uses
// the fallback path. The scan must not fail when it encounters such agents.
func TestResolveDeployedPath_ThirdPartyAgent_NoNumericID_UsesFallbackPath(t *testing.T) {
	// Arrange — third-party agent: no NumericID, no infrastructure class, no role set.
	agent := domain.Agent{
		Key:       "third-party-agent",
		NumericID: "",
	}
	index := DeployedAgentIndex{}
	fallbackPath := ".mosaic/third-party-agent.md"

	// Act
	path, err := resolveDeployedPath(agent, index, fallbackPath)

	// Assert
	if err != nil {
		t.Fatalf("resolveDeployedPath returned unexpected error for third-party agent: %v", err)
	}
	if path != fallbackPath {
		t.Errorf("resolveDeployedPath = %q, want %q; "+
			"a third-party agent with no numeric id must use the harness-computed fallback path",
			path, fallbackPath)
	}
}

// ---------------------------------------------------------------------------
// Ambiguous id — collision detection
// ---------------------------------------------------------------------------

// TestBuildDeployedAgentIndex_TwoFilesWithSameID_BothIndexed verifies that when two
// deployed files declare the same numeric id, the index keeps both entries. Silently
// discarding either would prevent reporting the collision to the operator.
func TestBuildDeployedAgentIndex_TwoFilesWithSameID_BothIndexed(t *testing.T) {
	// Arrange — two files in the same agents directory, both declaring id "12".
	workspace := t.TempDir()
	agentsDir := ".mosaic"
	writeDeployedFileWithID(t, filepath.Join(workspace, agentsDir), "alpha.md", "12")
	writeDeployedFileWithID(t, filepath.Join(workspace, agentsDir), "beta.md", "12")

	// Act
	index := buildDeployedAgentIndex(workspace, agentsDir)

	// Assert — both entries must be present so the caller can report the collision.
	entries := index.Lookup("12")
	if len(entries) < 2 {
		t.Errorf("index.Lookup(\"12\") returned %d entries, want >= 2; "+
			"both files declaring id \"12\" must be kept in the index for collision reporting",
			len(entries))
	}
}

// TestResolveDeployedPath_AmbiguousID_ReturnsErrAmbiguousDeployedID verifies that when the
// index contains two or more entries for a given id, resolveDeployedPath returns
// ErrAmbiguousDeployedID. The run must not silently pick one of the colliding files.
func TestResolveDeployedPath_AmbiguousID_ReturnsErrAmbiguousDeployedID(t *testing.T) {
	// Arrange — index with two entries for the same id (collision).
	agent := domain.Agent{Key: "my-agent", NumericID: "12"}
	index := DeployedAgentIndex{
		"12": {
			{NumericID: "12", TargetPath: ".mosaic/alpha.md"},
			{NumericID: "12", TargetPath: ".mosaic/beta.md"},
		},
	}

	// Act
	_, err := resolveDeployedPath(agent, index, ".mosaic/my-agent.md")

	// Assert
	if err == nil {
		t.Fatal("resolveDeployedPath returned nil error for an ambiguous id; expected ErrAmbiguousDeployedID")
	}
	if !errors.Is(err, ErrAmbiguousDeployedID) {
		t.Errorf("resolveDeployedPath error = %v; want an error wrapping ErrAmbiguousDeployedID", err)
	}
}

// ---------------------------------------------------------------------------
// T8.6 — End-to-end: whole-location scan and deploy resolution pipeline
// ---------------------------------------------------------------------------

// TestDeployedAgentIndex_EndToEnd_WholeLocationScan verifies that buildDeployedAgentIndex
// correctly indexes all id-carrying files in the agents directory in a single scan. This
// covers the "whole-location scan" required by the update flow — every deployed file under
// the agents directory is examined and indexed, not just the ones the flow already knows about.
func TestDeployedAgentIndex_EndToEnd_WholeLocationScan(t *testing.T) {
	// Arrange — populate a workspace with a mix:
	//   - three files with distinct ids
	//   - one file with no id (must be skipped)
	//   - one unparseable file (must be skipped)
	workspace := t.TempDir()
	agentsDir := ".mosaic"
	writeDeployedFileWithID(t, filepath.Join(workspace, agentsDir), "agent-a.md", "10")
	writeDeployedFileWithID(t, filepath.Join(workspace, agentsDir), "agent-b.md", "20")
	writeDeployedFileWithID(t, filepath.Join(workspace, agentsDir), "agent-c.md", "30")
	writeDeployedFileNoID(t, filepath.Join(workspace, agentsDir), "no-id.md")
	writeUnparseableDeployedFile(t, filepath.Join(workspace, agentsDir), "bad.md")

	// Act
	index := buildDeployedAgentIndex(workspace, agentsDir)

	// Assert — all three id-carrying files must be indexed.
	for _, id := range []string{"10", "20", "30"} {
		entries := index.Lookup(id)
		if len(entries) == 0 {
			t.Errorf("index.Lookup(%q) returned no entries; "+
				"all id-carrying files in the agents directory must be indexed", id)
		}
	}
	// The id-less and unparseable files must not produce any entries.
	if entries := index.Lookup(""); len(entries) != 0 {
		t.Errorf("index.Lookup(\"\") returned %d entries; id-less files must not be indexed", len(entries))
	}
	// Exactly three distinct ids must be present.
	if len(index) != 3 {
		t.Errorf("index has %d distinct ids, want 3; "+
			"only the three id-carrying files must be indexed", len(index))
	}
}

// TestDeployedAgentIndex_EndToEnd_DeployResolutionPipeline verifies the full pipeline:
// build the index from a workspace once, then resolve the target path for several agents
// covering the renamed, not-deployed, and no-id cases. This mirrors the deploy flow's
// use of the index: scan once, then resolve per agent using the shared index.
func TestDeployedAgentIndex_EndToEnd_DeployResolutionPipeline(t *testing.T) {
	// Arrange — workspace with two deployed files.
	workspace := t.TempDir()
	agentsDir := ".mosaic"
	// Agent A is deployed at its expected name.
	writeDeployedFileWithID(t, filepath.Join(workspace, agentsDir), "agent-a.md", "10")
	// Agent B was renamed: catalog key is "agent-b" but deployed file is "renamed-b.md".
	writeDeployedFileWithID(t, filepath.Join(workspace, agentsDir), "renamed-b.md", "20")

	// Build the index once (as the deploy flow does).
	index := buildDeployedAgentIndex(workspace, agentsDir)

	// Define four agents covering distinct resolution paths.
	agentA := domain.Agent{Key: "agent-a", NumericID: "10", Role: domain.RoleSubagent}
	agentB := domain.Agent{Key: "agent-b", NumericID: "20", Role: domain.RoleSubagent} // renamed
	agentC := domain.Agent{Key: "agent-c", NumericID: "99", Role: domain.RoleSubagent} // not deployed
	utilityD := domain.Agent{Key: "utility-d", NumericID: "", Role: domain.RoleUtility}

	fallbackA := filepath.Join(agentsDir, "agent-a.md")
	fallbackB := filepath.Join(agentsDir, "agent-b.md") // name-based, but file is at "renamed-b.md"
	fallbackC := filepath.Join(agentsDir, "agent-c.md")
	fallbackD := filepath.Join(agentsDir, "utility-d.md")

	// --- Agent A: found at agent-a.md via id "10" ---
	pathA, errA := resolveDeployedPath(agentA, index, fallbackA)
	if errA != nil {
		t.Errorf("agentA: unexpected error: %v", errA)
	}
	if pathA == "" {
		t.Error("agentA: resolveDeployedPath returned not-deployed; " +
			"agent-a is deployed at agent-a.md with id \"10\"")
	}

	// --- Agent B: must be found at renamed-b.md via id "20", NOT at name-based fallbackB ---
	pathB, errB := resolveDeployedPath(agentB, index, fallbackB)
	if errB != nil {
		t.Errorf("agentB: unexpected error: %v", errB)
	}
	if pathB == fallbackB {
		t.Errorf("agentB: resolveDeployedPath returned the name-based path %q; "+
			"a renamed deployed file must be found at its actual path via id, not the name-derived path",
			fallbackB)
	}
	if pathB == "" {
		t.Error("agentB: resolveDeployedPath returned not-deployed; " +
			"agent-b (deployed as renamed-b.md with id \"20\") must be found by id")
	}

	// --- Agent C: not deployed — must return "" without error ---
	pathC, errC := resolveDeployedPath(agentC, index, fallbackC)
	if errC != nil {
		t.Errorf("agentC: unexpected error: %v", errC)
	}
	if pathC != "" {
		t.Errorf("agentC: resolveDeployedPath = %q, want \"\" (not deployed); "+
			"no file with id \"99\" exists in the workspace", pathC)
	}

	// --- Utility D: no NumericID — must return fallbackD without consulting the index ---
	pathD, errD := resolveDeployedPath(utilityD, index, fallbackD)
	if errD != nil {
		t.Errorf("utilityD: unexpected error: %v", errD)
	}
	if pathD != fallbackD {
		t.Errorf("utilityD: resolveDeployedPath = %q, want fallbackPath %q; "+
			"a utility agent with no numeric id must use the harness-computed path",
			pathD, fallbackD)
	}
}
