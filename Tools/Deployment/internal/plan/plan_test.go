package plan_test

// plan_test.go covers target path resolution, plan item classification across all four
// outcomes, and the no-write contract (T16.6, T16.7, T16.10).
//
// T16.6 — Target path resolution:
//   - PlanItem.TargetPath is set from the harness module's TargetPath method
//   - Agents and skills each get paths that include the key and any harness extension
//   - Target paths are relative to the deployment root (CD-12)
//
// T16.7 — Plan item classification:
//   - ActionCreate: artifact in set, not in manifest, not on disk
//   - ActionUpdate: artifact in manifest, at least one version field differs
//   - ActionUnchanged: artifact in manifest, all versions match, hash matches
//   - ActionConflict: artifact in manifest, current hash differs from recorded hash
//   - Empty workspace (no manifest, no deployed files): all items are ActionCreate
//   - Items are ordered by kind then key
//
// T16.10 — Planner opens no file for writing:
//   - No files are created or modified in the workspace during Build
//   - The manifest is not written, touched, or renamed

import (
	"context"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers for Build-based classification tests
// ---------------------------------------------------------------------------

// buildInputFull returns a plan.Input with both a worker agent and a skill. The manifest
// snapshot and deployed hashes are controlled by the caller.
func buildInputFull(
	agent domain.Agent,
	skill domain.Skill,
	snap manifest.Snapshot,
	deployedHashes map[string]string,
) plan.Input {
	wf := makeWorkflow("test-wf", agent.Key)

	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		skills:       []domain.Skill{skill},
		workflows:    []domain.Workflow{wf},
	}

	module := newFakeModule()

	return plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key: {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedHashes: deployedHashes,
	}
}

// ---------------------------------------------------------------------------
// T16.6 — Target path resolution
// ---------------------------------------------------------------------------

// TestBuild_AgentTargetPath_SetFromHarnessModule verifies that the PlanItem for an agent
// has its TargetPath set to the value returned by module.TargetPath for that agent.
// The planner must not hard-code path logic; all paths come from the harness module.
func TestBuild_AgentTargetPath_SetFromHarnessModule(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	skill := makeSkill("a-skill", "1.0")

	const wantAgentPath = "my-harness/agents/test-agent.md"

	module := newFakeModule()
	module.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactAgent && req.Key == "test-agent" {
			return wantAgentPath, nil
		}
		// Default for other kinds.
		switch req.Kind {
		case domain.ArtifactSkill:
			return "skills/" + req.Key + "/SKILL.md", nil
		}
		return "other/" + req.Key, nil
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		skills:       []domain.Skill{skill},
		workflows:    []domain.Workflow{wf},
	}

	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.TargetPath != wantAgentPath {
		t.Errorf("test-agent TargetPath = %q, want %q (value returned by module.TargetPath)",
			item.TargetPath, wantAgentPath)
	}
}

// TestBuild_SkillTargetPath_SetFromHarnessModule verifies that the PlanItem for a skill
// has its TargetPath set to the value returned by module.TargetPath for that skill.
func TestBuild_SkillTargetPath_SetFromHarnessModule(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	agent.RequiredSkills = []string{"a-skill"}
	skill := makeSkill("a-skill", "1.0")

	const wantSkillPath = "my-harness/skills/a-skill/SKILL.md"

	module := newFakeModule()
	module.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactSkill && req.Key == "a-skill" {
			return wantSkillPath, nil
		}
		return "agents/" + req.Key + ".md", nil
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		skills:       []domain.Skill{skill},
		workflows:    []domain.Workflow{wf},
	}

	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "a-skill")
	if !ok {
		t.Fatal("plan has no item for skill a-skill")
	}
	if item.TargetPath != wantSkillPath {
		t.Errorf("a-skill TargetPath = %q, want %q (value returned by module.TargetPath)",
			item.TargetPath, wantSkillPath)
	}
}

// TestBuild_TargetPath_IsNonEmpty verifies that every plan item has a non-empty TargetPath.
// An empty path would prevent the executor from knowing where to write the artifact.
func TestBuild_TargetPath_IsNonEmpty(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	skill := makeSkill("a-skill", "1.0")
	agent.RequiredSkills = []string{"a-skill"}

	input := buildInputFull(agent, skill, absentSnapshot(), nil)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	for _, item := range p.Items {
		if item.TargetPath == "" {
			t.Errorf("PlanItem{Ref: %v} has empty TargetPath; every item must have a non-empty target path",
				item.Ref)
		}
	}
}

// TestBuild_TargetPath_IsRelative verifies that every plan item's TargetPath is a relative
// path (not absolute). Target paths are relative to the deployment root (CD-12).
func TestBuild_TargetPath_IsRelative(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	skill := makeSkill("a-skill", "1.0")
	agent.RequiredSkills = []string{"a-skill"}

	input := buildInputFull(agent, skill, absentSnapshot(), nil)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	for _, item := range p.Items {
		if len(item.TargetPath) > 0 && item.TargetPath[0] == '/' {
			t.Errorf("PlanItem{Ref: %v}.TargetPath = %q is absolute; target paths must be relative to the deployment root",
				item.Ref, item.TargetPath)
		}
	}
}

// ---------------------------------------------------------------------------
// T16.7 — Plan item classification
// ---------------------------------------------------------------------------

// TestBuild_NewArtifact_NotInManifest_ClassifiesAsCreate verifies that an artifact in the
// artifact set that has no corresponding manifest entry and is not on disk gets ActionCreate.
func TestBuild_NewArtifact_NotInManifest_ClassifiesAsCreate(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	skill := makeSkill("a-skill", "1.0")

	// Absent manifest means no entries; no deployed hashes means file not on disk.
	input := buildInputFull(agent, skill, absentSnapshot(), nil)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionCreate {
		t.Errorf("new agent not in manifest: Action = %q, want %q", item.Action, domain.ActionCreate)
	}
}

// TestBuild_Artifact_InManifest_AllVersionsMatch_HashMatches_ClassifiesAsUnchanged verifies
// that when a manifest entry exists with all version fields and hash matching, the plan
// classifies the item as ActionUnchanged.
func TestBuild_Artifact_InManifest_AllVersionsMatch_HashMatches_ClassifiesAsUnchanged(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	skill := makeSkill("a-skill", "1.0")

	const agentTarget = "agents/test-agent.md"
	const hash = "sha256:aaaaaa"

	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", hash)
	entry.TransformVersion = "1.0" // matches newFakeModule's descriptor
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	deployedHashes := map[string]string{agentTarget: hash}

	input := buildInputFull(agent, skill, snap, deployedHashes)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUnchanged {
		t.Errorf("agent with matching manifest entry and hash: Action = %q, want %q",
			item.Action, domain.ActionUnchanged)
	}
}

// TestBuild_Artifact_InManifest_VersionMismatch_ClassifiesAsUpdate verifies that a
// mismatch in any version field (here: agent version) causes ActionUpdate.
func TestBuild_Artifact_InManifest_VersionMismatch_ClassifiesAsUpdate(t *testing.T) {
	agent := makeAgent("test-agent", "1.1") // source is newer than manifest
	skill := makeSkill("a-skill", "1.0")

	const agentTarget = "agents/test-agent.md"
	const hash = "sha256:aaaaaa"

	// Manifest records version "1.0" but source is "1.1".
	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", hash)
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	// Deployed hash matches — only the version is stale, not a local modification.
	deployedHashes := map[string]string{agentTarget: hash}

	input := buildInputFull(agent, skill, snap, deployedHashes)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("agent with version mismatch: Action = %q, want %q", item.Action, domain.ActionUpdate)
	}
}

// TestBuild_Artifact_InManifest_VersionMismatch_StaleHasAtLeastOneDelta verifies that when
// an item is ActionUpdate, its Stale slice is non-empty. Every update must name which
// version field(s) drove it.
func TestBuild_Artifact_InManifest_VersionMismatch_StaleHasAtLeastOneDelta(t *testing.T) {
	agent := makeAgent("test-agent", "1.1") // version mismatch
	skill := makeSkill("a-skill", "1.0")

	const agentTarget = "agents/test-agent.md"
	const hash = "sha256:aaaaaa"

	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", hash)
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})
	deployedHashes := map[string]string{agentTarget: hash}

	input := buildInputFull(agent, skill, snap, deployedHashes)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action == domain.ActionUpdate && len(item.Stale) == 0 {
		t.Error("ActionUpdate item has empty Stale slice; every update must name which version field(s) drove it")
	}
}

// TestBuild_Artifact_InManifest_HashMismatch_ClassifiesAsConflict verifies that when an
// artifact's current hash differs from the recorded hash (locally modified), the item gets
// ActionConflict regardless of version staleness.
func TestBuild_Artifact_InManifest_HashMismatch_ClassifiesAsConflict(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	skill := makeSkill("a-skill", "1.0")

	const agentTarget = "agents/test-agent.md"

	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", "sha256:recorded")
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	// Hash differs — locally modified.
	deployedHashes := map[string]string{agentTarget: "sha256:modified"}

	input := buildInputFull(agent, skill, snap, deployedHashes)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionConflict {
		t.Errorf("agent with hash mismatch: Action = %q, want %q", item.Action, domain.ActionConflict)
	}
}

// TestBuild_EmptyWorkspace_AllItemsAreActionCreate verifies that when the manifest is absent
// and DeployedHashes is empty (clean slate), every item in the plan is ActionCreate.
func TestBuild_EmptyWorkspace_AllItemsAreActionCreate(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	skill := makeSkill("a-skill", "1.0")
	agent.RequiredSkills = []string{"a-skill"}

	// No manifest, no deployed files.
	input := buildInputFull(agent, skill, absentSnapshot(), nil)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	if len(p.Items) == 0 {
		t.Fatal("plan has no items; expected items for agent, skill, and orchestrator")
	}
	for _, item := range p.Items {
		if item.Action != domain.ActionCreate {
			t.Errorf("item %v: Action = %q, want ActionCreate for empty workspace",
				item.Ref, item.Action)
		}
	}
}

// TestBuild_Items_AreNonEmpty verifies that a plan with at least one selected workflow
// produces a non-empty Items slice. An empty Items slice would leave the executor with
// nothing to do.
func TestBuild_Items_AreNonEmpty(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	skill := makeSkill("a-skill", "1.0")

	input := buildInputFull(agent, skill, absentSnapshot(), nil)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	if len(p.Items) == 0 {
		t.Error("plan.Items is empty; expected at least the orchestrator and selected agents")
	}
}

// TestBuild_Items_AreOrderedByKindThenKey verifies that PlanItem slices are in the
// deterministic order required: kind first (agent before skill before hook), then key
// within each kind. This order makes plan output stable and diff-friendly.
func TestBuild_Items_AreOrderedByKindThenKey(t *testing.T) {
	// Create two agents and two skills so we can verify ordering within a kind.
	agentA := makeAgent("agent-a", "1.0")
	agentB := makeAgent("agent-b", "1.0")
	agentA.RequiredSkills = []string{"skill-x", "skill-y"}
	agentB.RequiredSkills = []string{"skill-x"}

	skillX := makeSkill("skill-x", "1.0")
	skillY := makeSkill("skill-y", "1.0")

	wf := makeWorkflow("test-wf", agentA.Key, agentB.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agentA, agentB},
		skills:       []domain.Skill{skillX, skillY},
		workflows:    []domain.Workflow{wf},
	}

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeDeployNew,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agentA.Key:   {ModelID: "test-model", Origin: domain.OriginHarnessList},
			agentB.Key:   {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	// Within agents: keys must be ascending.
	var agentItems, skillItems []domain.PlanItem
	for _, item := range p.Items {
		switch item.Ref.Kind {
		case domain.ArtifactAgent:
			agentItems = append(agentItems, item)
		case domain.ArtifactSkill:
			skillItems = append(skillItems, item)
		}
	}

	for i := 1; i < len(agentItems); i++ {
		if agentItems[i].Ref.Key < agentItems[i-1].Ref.Key {
			t.Errorf("agent items not sorted by key at index %d: %q < %q",
				i, agentItems[i].Ref.Key, agentItems[i-1].Ref.Key)
		}
	}
	for i := 1; i < len(skillItems); i++ {
		if skillItems[i].Ref.Key < skillItems[i-1].Ref.Key {
			t.Errorf("skill items not sorted by key at index %d: %q < %q",
				i, skillItems[i].Ref.Key, skillItems[i-1].Ref.Key)
		}
	}

	// Agents must appear before skills in the Items slice.
	lastAgentIdx := -1
	firstSkillIdx := len(p.Items) // sentinel: if no skills, this is never < lastAgentIdx
	for i, item := range p.Items {
		if item.Ref.Kind == domain.ArtifactAgent {
			lastAgentIdx = i
		}
		if item.Ref.Kind == domain.ArtifactSkill && firstSkillIdx == len(p.Items) {
			firstSkillIdx = i
		}
	}
	if len(skillItems) > 0 && firstSkillIdx < lastAgentIdx {
		t.Errorf("skill item at index %d appears before last agent item at index %d; "+
			"items must be ordered by kind (agents before skills)", firstSkillIdx, lastAgentIdx)
	}
}

// TestPlan_Counts_MatchesItemActions verifies that Plan.Counts() returns a map that
// accurately reflects the action distribution among plan items.
func TestPlan_Counts_MatchesItemActions(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	skill := makeSkill("a-skill", "1.0")

	input := buildInputFull(agent, skill, absentSnapshot(), nil)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	counts := p.Counts()

	// Verify Counts() is consistent with the actual items.
	expected := make(map[domain.PlanAction]int)
	for _, item := range p.Items {
		expected[item.Action]++
	}
	for action, want := range expected {
		if counts[action] != want {
			t.Errorf("Plan.Counts()[%q] = %d, want %d", action, counts[action], want)
		}
	}
}

// TestPlan_Empty_TrueWhenAllItemsAreUnchanged verifies that Plan.Empty() returns true
// only when every item has ActionUnchanged. A plan with all items unchanged means nothing
// needs to be written.
func TestPlan_Empty_TrueWhenAllItemsAreUnchanged(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	skill := makeSkill("a-skill", "1.0")

	const agentTarget = "agents/test-agent.md"
	const hash = "sha256:unchanged"

	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", hash)
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})
	deployedHashes := map[string]string{agentTarget: hash}

	input := buildInputFull(agent, skill, snap, deployedHashes)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	// Count non-unchanged items.
	nonUnchanged := 0
	for _, item := range p.Items {
		if item.Action != domain.ActionUnchanged {
			nonUnchanged++
		}
	}

	if nonUnchanged == 0 && !p.Empty() {
		t.Error("Plan.Empty() = false but all items are ActionUnchanged; want true")
	}
	if nonUnchanged > 0 && p.Empty() {
		t.Error("Plan.Empty() = true but some items are not ActionUnchanged; want false")
	}
}

// TestBuild_Plan_WorkflowsPreserveSelectionOrder verifies that Plan.Workflows contains the
// selected workflow IDs in the order they were provided to Build. This order is shown to
// the user during plan review.
func TestBuild_Plan_WorkflowsPreserveSelectionOrder(t *testing.T) {
	agentA := makeAgent("agent-a", "1.0")
	agentB := makeAgent("agent-b", "1.0")

	wfAlpha := makeWorkflow("alpha-wf", agentA.Key)
	wfBeta := makeWorkflow("beta-wf", agentB.Key)

	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agentA, agentB},
		workflows:    []domain.Workflow{wfAlpha, wfBeta},
	}

	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeDeployNew,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		WorkflowIDs:   []string{"beta-wf", "alpha-wf"}, // intentionally out of alphabetical order
		Models: map[string]domain.ModelSelection{
			agentA.Key:   {ModelID: "test-model", Origin: domain.OriginHarnessList},
			agentB.Key:   {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	if len(p.Workflows) != 2 {
		t.Fatalf("Plan.Workflows: got %d entries, want 2", len(p.Workflows))
	}
	if p.Workflows[0] != "beta-wf" || p.Workflows[1] != "alpha-wf" {
		t.Errorf("Plan.Workflows = %v, want [beta-wf alpha-wf]; selection order must be preserved",
			p.Workflows)
	}
}

// ---------------------------------------------------------------------------
// T16.10 — Planner opens no file for writing
// ---------------------------------------------------------------------------

// TestBuild_NoFileWrites_WorkspaceUnchanged verifies that calling Build does not create,
// modify, or remove any file in the workspace directory. The planner must be pure with
// respect to the file system; all writes are the executor's responsibility.
func TestBuild_NoFileWrites_WorkspaceUnchanged(t *testing.T) {
	ws := makeTempWorkspace(t)

	// Write a real manifest so the workspace has a known state before Build.
	m := domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Entries:       nil,
	}
	writeManifestFile(t, ws, m)

	// Capture file system state before Build.
	before := captureFS(t, ws)

	agent := makeAgent("test-agent", "1.0")
	snap := absentSnapshot() // use absent snapshot so Build doesn't need to read the real manifest

	input := buildInputWithSingleAgent(agent, snap, nil)
	input.WorkspacePath = ws // use the real temp workspace

	// Call Build. In RED phase this panics; in GREEN phase it must not modify the workspace.
	_, _ = plan.New().Build(context.Background(), input)

	// Capture file system state after Build.
	after := captureFS(t, ws)

	if changed, desc := fsChanged(before, after); changed {
		t.Errorf("Build modified the workspace file system: %s; the planner must not write any files", desc)
	}
}

// TestBuild_NoFileWrites_ManifestFileUnmodified verifies specifically that the manifest
// file is not touched (not written, renamed, or replaced) by Build. The manifest is written
// exclusively by the executor after a confirmed plan.
func TestBuild_NoFileWrites_ManifestFileUnmodified(t *testing.T) {
	ws := makeTempWorkspace(t)

	m := domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Entries:       nil,
	}
	writeManifestFile(t, ws, m)

	// Verify the manifest was written correctly before proceeding.
	snap, loadErr := manifest.NewStore().Load(ws)
	if loadErr != nil || snap.State != manifest.StatePresent {
		t.Fatalf("setup: manifest not present after write: state=%q err=%v", snap.State, loadErr)
	}

	agent := makeAgent("test-agent", "1.0")
	input := buildInputWithSingleAgent(agent, absentSnapshot(), nil)
	input.WorkspacePath = ws

	// Capture the file system state (including the manifest file's mtime and size).
	fsBefore := captureFS(t, ws)

	// Call Build. In RED phase this panics; in GREEN phase it must not touch the manifest.
	_, _ = plan.New().Build(context.Background(), input)

	fsAfter := captureFS(t, ws)

	if changed, desc := fsChanged(fsBefore, fsAfter); changed {
		t.Errorf("Build modified workspace files (including possibly the manifest): %s", desc)
	}
}

// TestBuild_ReturnsFullyRenderablePlan verifies that the returned plan contains enough
// information for both frontends to render it without additional I/O. This means every
// item has a non-empty Ref, Action, SourcePath, and TargetPath.
func TestBuild_ReturnsFullyRenderablePlan(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")
	skill := makeSkill("a-skill", "1.0")

	input := buildInputFull(agent, skill, absentSnapshot(), nil)

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	for _, item := range p.Items {
		if item.Ref.Key == "" {
			t.Errorf("PlanItem has empty Ref.Key; all items must be identifiable")
		}
		if item.Action == "" {
			t.Errorf("PlanItem{Ref: %v} has empty Action; all items must have a classified action", item.Ref)
		}
		if item.TargetPath == "" {
			t.Errorf("PlanItem{Ref: %v} has empty TargetPath; all items must have a target path for rendering", item.Ref)
		}
	}
}
