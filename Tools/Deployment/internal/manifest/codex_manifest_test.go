package manifest_test

// codex_manifest_test.go verifies that the manifest store correctly records and reads
// back manifest entries for a Codex deployment, including the split-root path layout
// where agent files live under .codex/agents and skill files live under .agents/skills.
//
// T12.6 -- Manifest records Codex split roots:
//   - A Codex manifest with agent entries under ".codex/agents" round-trips with the
//     correct HarnessID ("codex") and the correct agent TargetPaths.
//   - A Codex manifest with skill entries under ".agents/skills" round-trips with the
//     correct skill TargetPaths.
//   - Both paths are stored and read back independently; no path is derived by walking
//     from one root to the other.
//
// Evidence of the "generic already" verdict for the Manifest surface (I12.5 AC12.10):
//   The manifest store records HarnessID as a string and TargetPath per entry as a
//   string. Neither field constrains the harness ID or enforces a relationship between
//   agents and skills paths. The split-root layout is encoded entirely in the TargetPath
//   values that the harness descriptor supplies; the manifest store is path-agnostic.

import (
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// codexManifest returns a domain.Manifest representing a Codex deployment with:
//   - An agent entry under ".codex/agents/" (Codex-native agents root)
//   - A skill entry under ".agents/skills/" (shared skills root, different from agents root)
//
// These paths reflect the Codex harness descriptor's declared split-root layout and
// are used to prove that both roots are recorded and read back independently.
func codexManifest() domain.Manifest {
	return domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "codex",
		UpdatedAt:     time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC),
		Entries: []domain.ManifestEntry{
			{
				Ref:               domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "my-agent"},
				TargetPath:        ".codex/agents/my-agent.toml",
				Version:           "1.0.0",
				HarnessVersion:    "2.0.0",
				InjectionsVersion: "3.0.0",
				ContentHash:       "sha256:aabb1122aabb1122aabb1122aabb1122aabb1122aabb1122aabb1122aabb1122",
				DeployedAt:        time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
			},
			{
				Ref:               domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"},
				TargetPath:        ".agents/skills/lean-tdd/SKILL.md",
				Version:           "1.0.0",
				HarnessVersion:    "2.0.0",
				InjectionsVersion: "3.0.0",
				ContentHash:       "sha256:ccdd3344ccdd3344ccdd3344ccdd3344ccdd3344ccdd3344ccdd3344ccdd3344",
				DeployedAt:        time.Date(2024, 6, 15, 12, 1, 0, 0, time.UTC),
			},
		},
	}
}

// ---------------------------------------------------------------------------
// T12.6: Codex manifest round-trip with split roots
// ---------------------------------------------------------------------------

// TestCodexManifest_WriteRead_HarnessIDIsCodex verifies that after writing a Codex
// manifest and reading it back, the HarnessID is "codex". This confirms the manifest
// store is harness-agnostic and does not constrain the harness ID field.
func TestCodexManifest_WriteRead_HarnessIDIsCodex(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	must(t, store.Save(ws, codexManifest()))

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State != manifest.StatePresent {
		t.Fatalf("Load after Save: State = %q, want %q", snap.State, manifest.StatePresent)
	}
	if snap.Manifest.HarnessID != "codex" {
		t.Errorf("HarnessID = %q, want %q; the manifest store must record the Codex harness ID verbatim",
			snap.Manifest.HarnessID, "codex")
	}
}

// TestCodexManifest_WriteRead_AgentPathUnderCodexAgentsRoot verifies that the agent
// entry's TargetPath (.codex/agents/my-agent.toml) round-trips unchanged. The Codex
// harness deploys agents under .codex/agents rather than the shared agents root.
func TestCodexManifest_WriteRead_AgentPathUnderCodexAgentsRoot(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	must(t, store.Save(ws, codexManifest()))

	snap, err := store.Load(ws)
	must(t, err)

	const wantAgentPath = ".codex/agents/my-agent.toml"
	var found bool
	for _, e := range snap.Manifest.Entries {
		if e.Ref.Kind == domain.ArtifactAgent && e.Ref.Key == "my-agent" {
			found = true
			if e.TargetPath != wantAgentPath {
				t.Errorf("agent TargetPath = %q, want %q; the Codex agents root must be preserved verbatim",
					e.TargetPath, wantAgentPath)
			}
			break
		}
	}
	if !found {
		t.Errorf("agent entry for key %q not found in loaded manifest entries", "my-agent")
	}
}

// TestCodexManifest_WriteRead_SkillPathUnderSharedSkillsRoot verifies that the skill
// entry's TargetPath (.agents/skills/lean-tdd/SKILL.md) round-trips unchanged. Skills
// for Codex deployments live under the shared .agents/skills root, not under .codex/agents.
// This is the "split-root" property: agents and skills roots are independent.
func TestCodexManifest_WriteRead_SkillPathUnderSharedSkillsRoot(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	must(t, store.Save(ws, codexManifest()))

	snap, err := store.Load(ws)
	must(t, err)

	const wantSkillPath = ".agents/skills/lean-tdd/SKILL.md"
	var found bool
	for _, e := range snap.Manifest.Entries {
		if e.Ref.Kind == domain.ArtifactSkill && e.Ref.Key == "lean-tdd" {
			found = true
			if e.TargetPath != wantSkillPath {
				t.Errorf("skill TargetPath = %q, want %q; the shared skills root must be preserved verbatim",
					e.TargetPath, wantSkillPath)
			}
			break
		}
	}
	if !found {
		t.Errorf("skill entry for key %q not found in loaded manifest entries", "lean-tdd")
	}
}

// TestCodexManifest_WriteRead_AgentAndSkillRootsAreIndependent verifies that the
// agent root and skill root in a Codex manifest have no path relationship to each
// other: the skill path cannot be derived by rewriting the agent root prefix, and
// vice versa. This is the split-root invariant (AC12.8).
func TestCodexManifest_WriteRead_AgentAndSkillRootsAreIndependent(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	must(t, store.Save(ws, codexManifest()))

	snap, err := store.Load(ws)
	must(t, err)

	var agentPath, skillPath string
	for _, e := range snap.Manifest.Entries {
		switch e.Ref.Kind {
		case domain.ArtifactAgent:
			agentPath = e.TargetPath
		case domain.ArtifactSkill:
			skillPath = e.TargetPath
		}
	}

	if agentPath == "" {
		t.Fatal("agent entry absent from loaded manifest")
	}
	if skillPath == "" {
		t.Fatal("skill entry absent from loaded manifest")
	}

	// The agent root is ".codex/agents"; the skill root is ".agents/skills".
	// Neither can be derived from the other by simple prefix substitution.
	// Verify their roots are distinct.
	const wantAgentRoot = ".codex/agents"
	const wantSkillRoot = ".agents/skills"

	if len(agentPath) < len(wantAgentRoot) || agentPath[:len(wantAgentRoot)] != wantAgentRoot {
		t.Errorf("agent TargetPath %q does not start with %q; split-root layout requires Codex agents under .codex/agents",
			agentPath, wantAgentRoot)
	}
	if len(skillPath) < len(wantSkillRoot) || skillPath[:len(wantSkillRoot)] != wantSkillRoot {
		t.Errorf("skill TargetPath %q does not start with %q; split-root layout requires skills under .agents/skills",
			skillPath, wantSkillRoot)
	}
	if agentPath == skillPath {
		t.Error("agent and skill TargetPaths are identical; they must be at independent roots")
	}
}
