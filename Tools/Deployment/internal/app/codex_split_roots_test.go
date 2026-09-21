package app

// codex_split_roots_test.go covers T8.5: split agent and skills roots across the converted
// surfaces. The Codex harness declares agents at ".codex/agents" and skills at ".agents/skills"
// — different parent directories. This test verifies that the index scan and workspace scan
// only operate on the declared agents directory and do not touch or accidentally enumerate the
// skills directory.
//
// Verified behaviours:
//   - buildDeployedAgentIndex with agentsDir ".codex/agents" finds .toml agents there and
//     ignores any files in ".agents/skills".
//   - scanWorkspaceAgents with agentsDir ".codex/agents" enumerates only .toml agents there;
//     files in ".agents/skills" are never read or classified.
//   - Skills files present in ".agents/skills" produce no Matched or HarnessOnly entries.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/domain"
	_ "mosaic-deploy/internal/agentformat/all"
)

// codexSplitRootsDesc returns a HarnessDescriptor for the Codex harness with split roots:
// agents at ".codex/agents" (passed separately) and skills at ".agents/skills" (not used
// in these tests since all scan functions take an explicit agentsDir argument).
func codexSplitRootsDesc() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		AgentFormatID: "codex-toml",
		Extensions: map[domain.ArtifactKind]string{
			domain.ArtifactAgent: ".toml",
		},
		Paths: domain.PathSpec{
			Agents: domain.ScopedPaths{
				Supported: true,
				Project:   ".codex/agents",
			},
			Skills: domain.ScopedPaths{
				Supported: true,
				Project:   ".agents/skills",
			},
		},
	}
}

// writeSkillFile writes a minimal TOML file in the skills directory. Its content is
// irrelevant to these tests (skills are not processed by the agent scan/index), but the
// file must exist to prove the scan leaves it untouched.
func writeSkillFile(t *testing.T, workspace, relPath string) {
	t.Helper()
	fullPath := filepath.Join(workspace, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("writeSkillFile MkdirAll: %v", err)
	}
	content := []byte("# This is a skill file; the agent scan must never touch it.\n" +
		"sandbox_mode = \"read-only\"\n")
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		t.Fatalf("writeSkillFile WriteFile: %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.5 — buildDeployedAgentIndex: skills directory untouched
// ---------------------------------------------------------------------------

// TestBuildDeployedAgentIndex_SplitRoots_AgentsFound_SkillsIgnored verifies that when
// buildDeployedAgentIndex is called with the Codex agents directory ".codex/agents", it
// correctly indexes .toml agent files there but does not produce any entry for files in the
// ".agents/skills" directory.
func TestBuildDeployedAgentIndex_SplitRoots_AgentsFound_SkillsIgnored(t *testing.T) {
	workspace := t.TempDir()

	// Place a .toml agent in the agents directory.
	agentsDir := ".codex/agents"
	writeCodexFileWithID(t, filepath.Join(workspace, agentsDir), "worker.toml", "42")

	// Place a .toml file in the skills directory (different root).
	// If the index scan accidentally walked into ".agents/skills", it might index this file.
	writeSkillFile(t, workspace, ".agents/skills/my-skill.toml")

	index, _ := buildDeployedAgentIndex(workspace, agentsDir, codexSplitRootsDesc())

	// The agent at ".codex/agents/worker.toml" must be indexed.
	entries := index.Lookup("42")
	if len(entries) == 0 {
		t.Error("index.Lookup(\"42\") returned no entries; " +
			"agent at \".codex/agents/worker.toml\" must be indexed when agentsDir is \".codex/agents\"")
	}

	// The index must have exactly one entry (the agent), not two (agent + skill).
	if len(index) != 1 {
		t.Errorf("index has %d distinct ids, want 1; "+
			"the skills file at \".agents/skills/my-skill.toml\" must not be indexed "+
			"when agentsDir is \".codex/agents\"",
			len(index))
	}
}

// ---------------------------------------------------------------------------
// T8.5 — scanWorkspaceAgents: skills directory untouched
// ---------------------------------------------------------------------------

// TestScanWorkspaceAgents_SplitRoots_AgentsMatched_SkillsIgnored verifies that
// scanWorkspaceAgents scanned with agentsDir ".codex/agents" finds the Codex agent there
// and does not produce any Matched or HarnessOnly entry for files in ".agents/skills".
func TestScanWorkspaceAgents_SplitRoots_AgentsMatched_SkillsIgnored(t *testing.T) {
	workspace := t.TempDir()

	agentsDir := ".codex/agents"
	// Agent: valid .toml with mosaic_id stamp and catalog entry.
	writeCodexFileWithID(t, filepath.Join(workspace, agentsDir), "worker.toml", "42")

	// Skill: a .toml file in the skills directory.
	writeSkillFile(t, workspace, ".agents/skills/my-skill.toml")

	cat := newScanCatalog()
	cat.byNumericID["42"] = domain.Agent{Key: "catalog-worker", NumericID: "42"}

	scan, _ := scanWorkspaceAgents(workspace, agentsDir, cat, codexSplitRootsDesc())

	// The agent must appear in Matched.
	if len(scan.Matched) == 0 {
		t.Fatal("expected worker.toml to appear in Matched; scan must enumerate agents in \".codex/agents\"")
	}

	// No entry in Matched or HarnessOnly must originate from the skills directory.
	for _, m := range scan.Matched {
		if filepath.Dir(m.TargetPath) == ".agents/skills" ||
			filepath.HasPrefix(m.TargetPath, ".agents/skills") {
			t.Errorf("Matched entry %q originates from the skills directory; "+
				"scanWorkspaceAgents must not enumerate files outside agentsDir",
				m.TargetPath)
		}
	}
	for _, h := range scan.HarnessOnly {
		if filepath.Dir(h.TargetPath) == ".agents/skills" ||
			filepath.HasPrefix(h.TargetPath, ".agents/skills") {
			t.Errorf("HarnessOnly entry %q originates from the skills directory; "+
				"scanWorkspaceAgents must not enumerate files outside agentsDir",
				h.TargetPath)
		}
	}
}

// TestScanWorkspaceAgents_SplitRoots_EmptyAgentsDir_NoEntriesFromSkillsDir verifies that
// when the agents directory is empty, scanWorkspaceAgents produces an empty result even if
// the skills directory contains files. The scan must not accidentally walk the project root
// or adjacent directories.
func TestScanWorkspaceAgents_SplitRoots_EmptyAgentsDir_NoEntriesFromSkillsDir(t *testing.T) {
	workspace := t.TempDir()

	agentsDir := ".codex/agents"
	// Create the empty agents directory.
	if err := os.MkdirAll(filepath.Join(workspace, agentsDir), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Skills directory contains a file that the scan must not see.
	writeSkillFile(t, workspace, ".agents/skills/only-skill.toml")

	cat := newScanCatalog()
	scan, _ := scanWorkspaceAgents(workspace, agentsDir, cat, codexSplitRootsDesc())

	if len(scan.Matched) != 0 || len(scan.HarnessOnly) != 0 {
		t.Errorf("scan produced %d Matched and %d HarnessOnly entries for an empty agents directory; "+
			"skills files must not appear in the scan result",
			len(scan.Matched), len(scan.HarnessOnly))
	}
}
