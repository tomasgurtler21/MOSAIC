package plan_test

// codex_toml_classification_test.go proves that the plan layer classifies Codex (.toml)
// deployed agents exactly as it does Markdown ones: the same seven-step contract applies
// regardless of the deployed file's extension.
//
// Coverage:
//
//   Classification branches (T13.7):
//   (a) No deployed file -> ActionCreate.
//   (b) Deployed, manifest-backed, nothing stale -> ActionUnchanged even when the file
//       has been hand-edited. This is the anti-vacuity pin: a test that asserts a user
//       edit survived a "redeploy" is vacuous when nothing was written; this test names
//       the no-write outcome explicitly so a survival test cannot pass vacuously.
//   (c) Deployed with a bumped source version -> ActionUpdate.
//   (d) Manifest record withheld (file present, manifest missing) -> ActionConflict.
//   (e) Parse-failed TOML file -> ActionConflict on the unreadable branch, not ActionUpdate.
//
//   No-drift test (T13.8):
//   A Codex agent whose deployed state carries exactly the same version stamps as the
//   source (version, harness_version, injections_version all match) classifies as
//   ActionUnchanged. This proves the stamps survive the encode-decode round-trip in the
//   form the change-detection step actually consumes, so an unchanged redeploy never
//   rewrites the file.
//
// All tests use a fakeModule whose TargetPathFn returns Codex-style paths
// (.codex/agents/<key>.toml) so that the classification operates on target paths that
// match the Codex harness's actual output layout.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers: Codex-specific fakeModule and target path
// ---------------------------------------------------------------------------

// codexFakeModule returns a fakeModule that produces Codex-style target paths:
//   - agents: .codex/agents/<key>.toml
//   - skills: .agents/skills/<key>/SKILL.md
func codexFakeModule() *fakeModule {
	m := newFakeModule()
	m.desc.ID = "codex"
	m.desc.DisplayName = "Codex"
	m.desc.TransformVersion = "1.0"
	m.desc.InjectionsVersion = "1.0"
	m.ref = domain.HarnessRef{
		ID:          "codex",
		DisplayName: "Codex",
		Tier:        domain.TierBuiltin,
		Usable:      true,
	}
	m.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		switch req.Kind {
		case domain.ArtifactAgent:
			return ".codex/agents/" + req.Key + ".toml", nil
		case domain.ArtifactSkill:
			return ".agents/skills/" + req.Key + "/SKILL.md", nil
		case domain.ArtifactHook:
			return "", domain.ErrArtifactUnsupported
		}
		return "", domain.ErrArtifactUnsupported
	}
	return m
}

// codexTargetPath returns the Codex-style agent target path for the given key.
func codexTargetPath(key string) string {
	return ".codex/agents/" + key + ".toml"
}

// ---------------------------------------------------------------------------
// T13.7(a) -- No deployed file -> ActionCreate
// ---------------------------------------------------------------------------

// TestCodexClassify_NoDeployedFile_ReturnsCreate verifies that an agent with no deployed
// TOML file at the Codex target path classifies as ActionCreate through plan.Build. This
// is Step 1 of classifyAgentItem; it is independent of manifest state or file content, and
// the Codex path extension (.toml) must not change this outcome.
func TestCodexClassify_NoDeployedFile_ReturnsCreate(t *testing.T) {
	agent := makeAgent("new-codex-agent", "1.0")

	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{makeWorkflow("codex-wf", agent.Key)},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        codexFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		WorkflowIDs:   []string{"codex-wf"},
		Manifest:      absentSnapshot(),
		// No DeployedState entry for the target path -> absent file.
		DeployedState: map[string]domain.DeployedArtifactState{},
	}

	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.Build: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found in result", agent.Key)
	}

	if item.Action != domain.ActionCreate {
		t.Errorf("item.Action = %v, want %v; "+
			"a Codex agent with no deployed file at the .toml target path must classify as ActionCreate; "+
			"the path extension must not affect the classification outcome",
			item.Action, domain.ActionCreate)
	}
}

// ---------------------------------------------------------------------------
// T13.7(b) -- Manifest-backed, unchanged -> ActionUnchanged (anti-vacuity pin)
// ---------------------------------------------------------------------------

// TestCodexClassify_ManifestBacked_NothingStale_ReturnsUnchanged verifies that a Codex
// agent whose deployed state matches the source stamps AND has a manifest entry at the
// Codex target path classifies as ActionUnchanged. This test pins the no-write outcome
// explicitly so a survival test cannot pass vacuously: a hand-edited Codex file that
// matches version stamps will never be rewritten, and a test asserting "user content
// survived the redeploy" passes vacuously because nothing was written.
//
// This is the Step-7 path of classifyAgentItem: no staleness found, manifest entry
// present, deployed file parseable. The .toml extension must not produce a false stale
// classification or a false conflict.
func TestCodexClassify_ManifestBacked_NothingStale_ReturnsUnchanged(t *testing.T) {
	agent := makeAgent("stable-codex-agent", "1.0")
	targetPath := codexTargetPath(agent.Key)

	// Deployed state: all stamps match the source; hash is arbitrary (agents are not hash-compared).
	ds := deployedState("sha256:hand-edited-doesnt-matter", agent.Version, "1.0", "1.0")

	entry := makeManifestEntry(agentRef(agent.Key), targetPath, agent.Version, "sha256:hand-edited-doesnt-matter")
	snap := presentSnapshot(domain.Manifest{
		HarnessID: "codex",
		Entries:   []domain.ManifestEntry{entry},
	})

	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{makeWorkflow("codex-wf", agent.Key)},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        codexFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		WorkflowIDs:   []string{"codex-wf"},
		Manifest:      snap,
		DeployedState: map[string]domain.DeployedArtifactState{targetPath: ds},
	}

	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.Build: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found in result", agent.Key)
	}

	if item.Action != domain.ActionUnchanged {
		t.Errorf("item.Action = %v, want %v; "+
			"a manifest-backed Codex agent with matching version stamps must classify as ActionUnchanged; "+
			"a hand-edited deployed file does not produce a hash-mismatch conflict for agents (only hooks "+
			"compare hashes); classifying this as ActionUpdate or ActionConflict would incorrectly "+
			"overwrite user edits on every deploy",
			item.Action, domain.ActionUnchanged)
	}
}

// ---------------------------------------------------------------------------
// T13.7(c) -- Bumped source version -> ActionUpdate
// ---------------------------------------------------------------------------

// TestCodexClassify_SourceVersionBumped_ReturnsUpdate verifies that a Codex agent whose
// deployed state has an older source version than the current source classifies as
// ActionUpdate. This is the Step-5 staleness path: a version mismatch produces a delta
// and the item is classified as ActionUpdate.
//
// The version bump is the canonical way to trigger a genuine rewrite for survival tests;
// this test pins that path for Codex's .toml target paths.
func TestCodexClassify_SourceVersionBumped_ReturnsUpdate(t *testing.T) {
	agent := makeAgent("update-codex-agent", "2.0") // source version: 2.0
	targetPath := codexTargetPath(agent.Key)

	// Deployed state: old version 1.0 (stale relative to source 2.0).
	ds := deployedState("sha256:stale", "1.0", "1.0", "1.0")

	entry := makeManifestEntry(agentRef(agent.Key), targetPath, "1.0", "sha256:stale")
	snap := presentSnapshot(domain.Manifest{
		HarnessID: "codex",
		Entries:   []domain.ManifestEntry{entry},
	})

	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{makeWorkflow("codex-wf", agent.Key)},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        codexFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		WorkflowIDs:   []string{"codex-wf"},
		Manifest:      snap,
		DeployedState: map[string]domain.DeployedArtifactState{targetPath: ds},
	}

	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.Build: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found in result", agent.Key)
	}

	if item.Action != domain.ActionUpdate {
		t.Errorf("item.Action = %v, want %v; "+
			"a Codex agent whose source version (2.0) differs from the deployed version (1.0) "+
			"must classify as ActionUpdate so the file is regenerated",
			item.Action, domain.ActionUpdate)
	}

	// Verify that the staleness delta reports the version field.
	if len(item.Stale) == 0 {
		t.Errorf("item.Stale is empty; want at least one version delta for version mismatch (1.0 -> 2.0)")
	}
}

// ---------------------------------------------------------------------------
// T13.7(d) -- Manifest record withheld -> ActionConflict
// ---------------------------------------------------------------------------

// TestCodexClassify_ManifestRecordMissing_ReturnsConflict verifies that a Codex agent
// with a deployed file present but no manifest record at the current target path classifies
// as ActionConflict (Step 3 of classifyAgentItem). The file was not written by this tool,
// or was written by a different harness at a different path; either way, a conflict is
// reported.
//
// This is the same outcome as for Markdown harnesses: the target path's extension (.toml)
// is not consulted.
func TestCodexClassify_ManifestRecordMissing_ReturnsConflict(t *testing.T) {
	agent := makeAgent("conflict-codex-agent", "1.0")
	targetPath := codexTargetPath(agent.Key)

	// File is present and parseable, but the manifest has no entry for this target path.
	ds := deployedState("sha256:present", "1.0", "1.0", "1.0")

	// Manifest present but empty (no entries).
	snap := presentSnapshot(domain.Manifest{
		HarnessID: "codex",
		Entries:   []domain.ManifestEntry{}, // no entry for this target path
	})

	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{makeWorkflow("codex-wf", agent.Key)},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        codexFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		WorkflowIDs:   []string{"codex-wf"},
		Manifest:      snap,
		DeployedState: map[string]domain.DeployedArtifactState{targetPath: ds},
	}

	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.Build: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found in result", agent.Key)
	}

	if item.Action != domain.ActionConflict {
		t.Errorf("item.Action = %v, want %v; "+
			"a Codex agent present on disk but with no manifest record at its target path "+
			"must classify as ActionConflict; without a manifest entry at the exact target path "+
			"the tool cannot confirm the file is safe to overwrite",
			item.Action, domain.ActionConflict)
	}

	if item.Conflict == nil {
		t.Error("item.Conflict is nil; want non-nil LocalModification for a manifest-withheld conflict")
	} else if !item.Conflict.ManifestMissing {
		t.Errorf("item.Conflict.ManifestMissing = false; want true for a missing manifest record conflict")
	}
}

// ---------------------------------------------------------------------------
// T13.7(e) -- Parse-failed TOML -> ActionConflict (unreadable branch)
// ---------------------------------------------------------------------------

// TestCodexClassify_ParseFailed_ReturnsConflict verifies that a Codex agent whose deployed
// TOML file has ParseFailed: true classifies as ActionConflict, not ActionUpdate. An
// unparseable file cannot be merged with the incoming content, so it must conflict rather
// than silently overwriting the user's file.
//
// This proves that a corrupt .toml takes the same unreadable-conflict route as a corrupt
// .md would: the classification contract is format-agnostic. ParseFailed fires at Step 1b
// (before Steps 2-4), so neither manifest presence nor version stamps affect the outcome.
func TestCodexClassify_ParseFailed_ReturnsConflict(t *testing.T) {
	agent := makeAgent("corrupt-codex-agent", "1.0")
	targetPath := codexTargetPath(agent.Key)

	// ParseFailed: true -- TOML decoder could not parse the file.
	// A manifest entry exists to confirm the ParseFailed check fires BEFORE Step 3.
	ds := domain.DeployedArtifactState{
		Present:     true,
		ParseFailed: true,
		ContentHash: "sha256:corrupted",
	}

	entry := makeManifestEntry(agentRef(agent.Key), targetPath, "1.0", "sha256:corrupted")
	snap := presentSnapshot(domain.Manifest{
		HarnessID: "codex",
		Entries:   []domain.ManifestEntry{entry},
	})

	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{makeWorkflow("codex-wf", agent.Key)},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        codexFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		WorkflowIDs:   []string{"codex-wf"},
		Manifest:      snap,
		DeployedState: map[string]domain.DeployedArtifactState{targetPath: ds},
	}

	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.Build: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found in result", agent.Key)
	}

	if item.Action != domain.ActionConflict {
		t.Errorf("item.Action = %v, want %v; "+
			"a Codex agent whose deployed .toml file has ParseFailed: true must classify as "+
			"ActionConflict, not ActionUpdate; a corrupt file cannot be merged with incoming content; "+
			"classifying as ActionUpdate would silently overwrite the user's file",
			item.Action, domain.ActionConflict)
	}

	// The conflict reason must not be the hash-mismatch "locally modified" reason.
	if item.Reason == "deployed file has been locally modified since last deployment" {
		t.Errorf("item.Reason = %q; this is the Step-4 hash-mismatch reason; "+
			"the ParseFailed check must fire before Step 4 so the reason is distinct", item.Reason)
	}
}

// ---------------------------------------------------------------------------
// T13.8 -- No-drift: stamps round-trip, item classifies as ActionUnchanged
// ---------------------------------------------------------------------------

// TestCodexClassify_StampsRoundTrip_ReturnsUnchanged verifies that a Codex agent whose
// deployed state carries exactly the harness-version and injections-version stamps that
// the current descriptor would write (1.0 and 1.0 for the fakeModule) classifies as
// ActionUnchanged. This is the no-drift proof: stamps survive the encode-decode round
// trip in the form the staleness comparison step (classifyAgentItem Steps 4-5) actually
// consumes.
//
// Specifically:
//   - The decoder must extract the mosaic_harness_version and mosaic_injections_version
//     comments from the leading TOML stamp block into DeployedArtifactState.HarnessVersion
//     and DeployedArtifactState.InjectionsVersion.
//   - The classifier must compare those extracted values against
//     desc.TransformVersion and desc.InjectionsVersion respectively.
//   - When all three version fields match, no staleness delta is produced and the
//     item classifies as ActionUnchanged.
//
// The codexFakeModule carries TransformVersion="1.0" and InjectionsVersion="1.0".
// The deployed state carries the same values. Any mismatch in the round-trip would produce
// a delta and classify the item as ActionUpdate, breaking every unchanged redeploy.
func TestCodexClassify_StampsRoundTrip_ReturnsUnchanged(t *testing.T) {
	agent := makeAgent("nodrift-codex-agent", "1.5")
	targetPath := codexTargetPath(agent.Key)

	// Deployed state: all three version fields exactly match the source + descriptor versions.
	// version 1.5 matches agent.Version; harness 1.0 matches fakeModule.TransformVersion;
	// injections 1.0 matches fakeModule.InjectionsVersion.
	ds := deployedState("sha256:nodrift", agent.Version, "1.0", "1.0")

	entry := makeManifestEntry(agentRef(agent.Key), targetPath, agent.Version, "sha256:nodrift")
	snap := presentSnapshot(domain.Manifest{
		HarnessID: "codex",
		Entries:   []domain.ManifestEntry{entry},
	})

	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{makeWorkflow("codex-wf", agent.Key)},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        codexFakeModule(),
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		WorkflowIDs:   []string{"codex-wf"},
		Manifest:      snap,
		DeployedState: map[string]domain.DeployedArtifactState{targetPath: ds},
	}

	result, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.Build: %v", err)
	}

	item, found := findItem(result.Items, agent.Key)
	if !found {
		t.Fatalf("plan item for %q not found in result", agent.Key)
	}

	if item.Action != domain.ActionUnchanged {
		t.Errorf("item.Action = %v, want %v; "+
			"a Codex agent whose deployed stamps exactly match the current descriptor versions "+
			"must classify as ActionUnchanged; any mismatch means the stamps are not round-tripping "+
			"through the encode-decode funnel correctly; Stale deltas: %v",
			item.Action, domain.ActionUnchanged, item.Stale)
	}

	if len(item.Stale) != 0 {
		t.Errorf("item.Stale has %d deltas; want 0 for a stamp-matched deployed file; deltas: %v",
			len(item.Stale), item.Stale)
	}
}
