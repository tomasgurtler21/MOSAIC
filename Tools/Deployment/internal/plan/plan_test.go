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
// snapshot and deployed state are controlled by the caller.
func buildInputFull(
	agent domain.Agent,
	skill domain.Skill,
	snap manifest.Snapshot,
	deployedState map[string]domain.DeployedArtifactState,
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
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: deployedState,
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
// that when a manifest entry exists, the deployed file's hash matches the manifest's recorded
// hash, and all version stamps from the deployed file match the source, the plan classifies
// the item as ActionUnchanged.
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

	// Deployed file is present; hash matches; version stamps match source (version "1.0",
	// transform "1.0", injections "1.0" — all equal to the source from descriptor).
	ds := map[string]domain.DeployedArtifactState{
		agentTarget: deployedState(hash, "1.0", "1.0", "1.0"),
	}

	input := buildInputFull(agent, skill, snap, ds)

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
// mismatch between the deployed file's version stamp and the source causes ActionUpdate.
func TestBuild_Artifact_InManifest_VersionMismatch_ClassifiesAsUpdate(t *testing.T) {
	agent := makeAgent("test-agent", "1.1") // source is newer than what is deployed
	skill := makeSkill("a-skill", "1.0")

	const agentTarget = "agents/test-agent.md"
	const hash = "sha256:aaaaaa"

	// Manifest records version "1.0" (for conflict-hash lookup); source is "1.1".
	entry := makeManifestEntry(agentRef("test-agent"), agentTarget, "1.0", hash)
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	// Deployed file has version "1.0" (old); hash matches manifest (no local modification).
	// Source wants "1.1" → version delta → ActionUpdate.
	ds := map[string]domain.DeployedArtifactState{
		agentTarget: deployedState(hash, "1.0", "1.0", "1.0"),
	}

	input := buildInputFull(agent, skill, snap, ds)

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
	agent := makeAgent("test-agent", "1.1") // source newer than deployed
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
	// Deployed file has version "1.0" while source is "1.1" → stale.
	ds := map[string]domain.DeployedArtifactState{
		agentTarget: deployedState(hash, "1.0", "1.0", "1.0"),
	}

	input := buildInputFull(agent, skill, snap, ds)

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
// artifact's deployed hash differs from the manifest's recorded hash (locally modified), the
// item gets ActionConflict regardless of version staleness.
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

	// Deployed hash differs from manifest — locally modified.
	ds := map[string]domain.DeployedArtifactState{
		agentTarget: deployedState("sha256:modified", "1.0", "1.0", "1.0"),
	}

	input := buildInputFull(agent, skill, snap, ds)

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
// and DeployedState is nil (clean slate — no files on disk), every item in the plan is ActionCreate.
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
	// Deployed file present with matching hash and all version stamps matching source.
	ds := map[string]domain.DeployedArtifactState{
		agentTarget: deployedState(hash, "1.0", "1.0", "1.0"),
	}

	input := buildInputFull(agent, skill, snap, ds)

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

// ---------------------------------------------------------------------------
// T4.2 — No deployed file at the current target path → create, regardless of manifest
// ---------------------------------------------------------------------------

// TestBuild_AgentAbsentFromDeployedState_ClassifiesAsCreate_DespiteManifestEntry verifies
// that when the deployed state shows no file at an agent's target path, the item classifies
// as ActionCreate even when a manifest entry exists for that agent. The classifier's first
// step must short-circuit to create on absent presence flag before consulting the manifest.
func TestBuild_AgentAbsentFromDeployedState_ClassifiesAsCreate_DespiteManifestEntry(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	const recordedHash = "sha256:aaaaaa"

	agent := makeAgent("test-agent", "1.0")

	// Manifest has a valid, fully-matching entry for the agent.
	entry := makeManifestEntry(agentRef("test-agent"), targetPath, "1.0", recordedHash)
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	// Deployed state explicitly marks the file as absent at the target path.
	ds := map[string]domain.DeployedArtifactState{
		targetPath: {Present: false},
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionCreate {
		t.Errorf("test-agent: deployed file absent → Action = %q, want ActionCreate; manifest entry must not prevent create classification",
			item.Action)
	}
}

// TestBuild_SkillAbsentFromDeployedState_ClassifiesAsCreate_DespiteManifestEntry verifies
// the same absent-file short-circuit for skills. The three classifiers must implement the
// same decision tree.
func TestBuild_SkillAbsentFromDeployedState_ClassifiesAsCreate_DespiteManifestEntry(t *testing.T) {
	const skillTarget = "skills/lean-tdd/SKILL.md"
	const recordedHash = "sha256:bbbbbb"

	agent := makeAgent("test-agent", "1.0")
	agent.RequiredSkills = []string{"lean-tdd"}
	skill := makeSkill("lean-tdd", "1.5")

	// Manifest has a matching entry for the skill.
	skillEntry := makeManifestEntry(skillRef("lean-tdd"), skillTarget, "1.5", recordedHash)

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{skillEntry},
	})

	// Skill target path absent in deployed state.
	ds := map[string]domain.DeployedArtifactState{
		skillTarget: {Present: false},
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
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "lean-tdd")
	if !ok {
		t.Fatal("plan has no item for skill lean-tdd")
	}
	if item.Action != domain.ActionCreate {
		t.Errorf("lean-tdd: deployed file absent → Action = %q, want ActionCreate; manifest entry must not prevent create classification",
			item.Action)
	}
}

// TestBuild_AgentAbsent_DeployedState_MissingKey_TreatedAsAbsent verifies that a missing
// key in DeployedState is equivalent to an entry with Present: false — both must classify
// as ActionCreate even when a manifest entry exists. The zero value of
// domain.DeployedArtifactState has Present: false, making this map-lookup behaviour safe.
func TestBuild_AgentAbsent_DeployedState_MissingKey_TreatedAsAbsent(t *testing.T) {
	const targetPath = "agents/test-agent.md"

	agent := makeAgent("test-agent", "1.0")

	// Manifest has a fully-matching entry.
	entry := makeManifestEntry(agentRef("test-agent"), targetPath, "1.0", "sha256:aaaaaa")
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	// Empty DeployedState — key not present — must be equivalent to Present: false.
	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: map[string]domain.DeployedArtifactState{}, // empty — no entry for targetPath
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionCreate {
		t.Errorf("test-agent: missing key in DeployedState → Action = %q, want ActionCreate (same as Present: false)",
			item.Action)
	}
}

// ---------------------------------------------------------------------------
// T4.3 — Cross-harness scenario: artifacts planned at new target paths with no files
// ---------------------------------------------------------------------------

// TestBuild_CrossHarness_FilesAbsentAtNewTargetPaths_AllAgentsAreCreate verifies that when
// a manifest was recorded under a different harness (and therefore has entries at different
// target paths), planning against a new harness whose target paths have no files classifies
// every agent artifact as ActionCreate with no spurious version-downgrade update.
//
// The bug this test guards against: the old code used m.Lookup(Kind+Key) which finds the
// old-harness entry, then compares that entry's versions against source. If versions differ
// in the "downgrade" direction (new harness injections_version < old harness), the item was
// classified as ActionUpdate — for a file that doesn't even exist at the new target path.
func TestBuild_CrossHarness_FilesAbsentAtNewTargetPaths_AllAgentsAreCreate(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")

	// Old harness recorded the entry at a different target path.
	oldTargetPath := "old-harness/agents/test-agent.md"
	// New harness computes a different target path.
	newTargetPath := "new-harness/agents/test-agent.md"

	// Manifest has an entry for the agent at the OLD harness path.
	oldEntry := makeManifestEntry(agentRef("test-agent"), oldTargetPath, "1.0", "sha256:old")
	oldEntry.TransformVersion = "1.0"
	oldEntry.InjectionsVersion = "2.0" // old harness had a newer injections version

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "old-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{oldEntry},
	})

	// New harness module returns new-harness paths; no file exists there.
	module := newFakeModule()
	module.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactAgent && req.Key == "test-agent" {
			return newTargetPath, nil
		}
		if req.Kind == domain.ArtifactAgent && req.Key == "orchestrator" {
			return "new-harness/agents/orchestrator.md", nil
		}
		return "new-harness/" + req.Key, nil
	}

	// No files at new-harness paths — all absent.
	ds := map[string]domain.DeployedArtifactState{
		newTargetPath:                      {Present: false},
		"new-harness/agents/orchestrator.md": {Present: false},
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionCreate {
		t.Errorf("cross-harness test-agent: Action = %q, want ActionCreate; absent file at new target path must not produce ActionUpdate",
			item.Action)
	}
	if len(item.Stale) > 0 {
		t.Errorf("cross-harness test-agent: Stale = %v, want empty; a create item must carry no version deltas",
			item.Stale)
	}
}

// ---------------------------------------------------------------------------
// T4.4 — Manifest entry at a different target path does not satisfy "in manifest" branch
// ---------------------------------------------------------------------------

// TestBuild_ManifestEntryAtDifferentTargetPath_FileExistsAtCurrentPath_ClassifiesAsConflict
// verifies that when the manifest holds an entry for an artifact (same Kind+Key) recorded at
// target path A, but the current run's target path is B and a file exists at B, the classifier
// treats the file at B as having no manifest record — ActionConflict with ManifestMissing: true.
//
// This is the cross-harness "file exists at new path" scenario. Without path-aware lookup
// (LookupAt), m.Lookup(ref) would find the old entry at path A and potentially produce a
// false ActionUnchanged or ActionUpdate for a file the tool never wrote at path B.
func TestBuild_ManifestEntryAtDifferentTargetPath_FileExistsAtCurrentPath_ClassifiesAsConflict(t *testing.T) {
	agent := makeAgent("test-agent", "1.0")

	oldTargetPath := "old-harness/agents/test-agent.md"
	newTargetPath := "new-harness/agents/test-agent.md"

	// Manifest entry at old path.
	oldEntry := makeManifestEntry(agentRef("test-agent"), oldTargetPath, "1.0", "sha256:old")
	oldEntry.TransformVersion = "1.0"
	oldEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "old-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{oldEntry},
	})

	// Current target path is new path; a file exists there (independently deployed).
	module := newFakeModule()
	module.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactAgent && req.Key == "test-agent" {
			return newTargetPath, nil
		}
		if req.Kind == domain.ArtifactAgent && req.Key == "orchestrator" {
			return "new-harness/agents/orchestrator.md", nil
		}
		return "new-harness/" + req.Key, nil
	}

	// File IS present at new path; its hash doesn't match old manifest entry.
	ds := map[string]domain.DeployedArtifactState{
		newTargetPath: deployedState("sha256:independent", "1.0", "1.0", "1.0"),
		"new-harness/agents/orchestrator.md": {Present: false},
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	// File exists at new path but the manifest entry is at a different path: no record at
	// current target path → ActionConflict with ManifestMissing: true.
	if item.Action != domain.ActionConflict {
		t.Errorf("test-agent: manifest entry at different path, file at current path → Action = %q, want ActionConflict",
			item.Action)
	}
	if item.Conflict == nil {
		t.Fatal("test-agent: Conflict is nil; expected a populated LocalModification")
	}
	if !item.Conflict.ManifestMissing {
		t.Error("LocalModification.ManifestMissing = false; expected true because no manifest entry covers the current target path")
	}
}

// ---------------------------------------------------------------------------
// T4.5 — Local-modification conflict detection uses manifest's recorded hash and takes
//         precedence over version staleness
// ---------------------------------------------------------------------------

// TestBuild_HashMismatch_TakesPrecedenceOverVersionStaleness verifies that when a deployed
// file's hash differs from the manifest's recorded hash (locally modified), the item is
// ActionConflict even when version staleness would also fire. Conflict detection is applied
// before staleness in the classification decision tree, and its result is final.
func TestBuild_HashMismatch_TakesPrecedenceOverVersionStaleness(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	const recordedHash = "sha256:recorded"

	// Source version "2.0"; deployed file has version "1.0" and a modified hash.
	// Without conflict short-circuit this would be ActionUpdate. With it: ActionConflict.
	agent := makeAgent("test-agent", "2.0")

	entry := makeManifestEntry(agentRef("test-agent"), targetPath, "2.0", recordedHash)
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	// Deployed file: hash differs from manifest (locally modified) AND version is stale.
	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedState("sha256:modified-by-hand", "1.0", "1.0", "1.0"),
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionConflict {
		t.Errorf("test-agent: hash mismatch + version staleness → Action = %q, want ActionConflict; conflict must take precedence",
			item.Action)
	}
}

// TestBuild_HashMatch_ConflictNotFired_VersionStalenessClassifiedAsUpdate verifies that
// when the deployed file's hash matches the manifest's recorded hash (no local modification),
// version staleness is evaluated and an update is returned. This confirms the conflict check
// uses the manifest's recorded hash, not the deployed file's version.
func TestBuild_HashMatch_ConflictNotFired_VersionStalenessClassifiedAsUpdate(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	const hash = "sha256:recorded"

	// Source: version "2.0"; deployed file: version "1.0" (stale), hash = recorded hash.
	agent := makeAgent("test-agent", "2.0")

	entry := makeManifestEntry(agentRef("test-agent"), targetPath, "1.0", hash)
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	// Deployed file: hash matches manifest (no local mod), version is old.
	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedState(hash, "1.0", "1.0", "1.0"),
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("test-agent: hash matches + version stale → Action = %q, want ActionUpdate",
			item.Action)
	}
}

// ---------------------------------------------------------------------------
// T4.2 — Unversioned deployed file is treated as stale (present but no version stamps)
// ---------------------------------------------------------------------------

// TestBuild_UnversionedDeployedFile_ClassifiesAsUpdate verifies that a present deployed file
// carrying no readable version stamps is classified as ActionUpdate, not ActionUnchanged.
// An unversioned file cannot be proven current; the conservative policy treats it as stale.
func TestBuild_UnversionedDeployedFile_ClassifiesAsUpdate(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	const hash = "sha256:recorded"

	agent := makeAgent("test-agent", "1.0")

	// Manifest entry present and hash matches (no local modification).
	entry := makeManifestEntry(agentRef("test-agent"), targetPath, "1.0", hash)
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	// Deployed file is present with matching hash but NO version stamps at all.
	ds := map[string]domain.DeployedArtifactState{
		targetPath: {
			Present:           true,
			ContentHash:       hash,
			Version:           "",  // no version info
			TransformVersion:  "",
			InjectionsVersion: "",
		},
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("test-agent: present but unversioned deployed file → Action = %q, want ActionUpdate; unversioned files cannot be proven current",
			item.Action)
	}
}

// ---------------------------------------------------------------------------
// T4.2 — Hook variant: no deployed file → create, regardless of manifest
// ---------------------------------------------------------------------------

// TestBuild_HookAbsentFromDeployedState_ClassifiesAsCreate_DespiteManifestEntry verifies
// that when the deployed state shows no file at a hook's target path, the hook item
// classifies as ActionCreate even when a manifest entry exists for that hook. The classifier
// must apply the same absent-file short-circuit for hooks as it does for agents and skills.
func TestBuild_HookAbsentFromDeployedState_ClassifiesAsCreate_DespiteManifestEntry(t *testing.T) {
	const hookTarget = "hooks/my-hooks"
	const recordedHash = "sha256:cccccc"

	agent := makeAgent("test-agent", "1.0")
	hook := makeHookBundle("my-hooks", "2.0")

	// Manifest has a valid entry for the hook at the target path.
	hookEntry := makeManifestEntry(hookRef("my-hooks"), hookTarget, "2.0", recordedHash)

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{hookEntry},
	})

	// Hook target path explicitly absent in deployed state.
	ds := map[string]domain.DeployedArtifactState{
		hookTarget: {Present: false},
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		hooks:        []domain.HookBundle{hook},
		workflows:    []domain.Workflow{wf},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		HookIDs:       []string{"my-hooks"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "my-hooks")
	if !ok {
		t.Fatal("plan has no item for hook my-hooks")
	}
	if item.Action != domain.ActionCreate {
		t.Errorf("my-hooks: deployed file absent → Action = %q, want ActionCreate; manifest entry must not prevent create classification",
			item.Action)
	}
}

// ---------------------------------------------------------------------------
// T4.3 — Skill/hook cross-harness variants: files absent at new target paths → create
// ---------------------------------------------------------------------------

// TestBuild_CrossHarness_FilesAbsentAtNewTargetPaths_SkillIsCreate verifies the same
// cross-harness behaviour for skill artifacts: a manifest entry at the old harness's skill
// path must not produce ActionUpdate when no file exists at the new harness's skill path.
func TestBuild_CrossHarness_FilesAbsentAtNewTargetPaths_SkillIsCreate(t *testing.T) {
	oldSkillPath := "old-harness/skills/lean-tdd/SKILL.md"
	newSkillPath := "new-harness/skills/lean-tdd/SKILL.md"

	agent := makeAgent("test-agent", "1.0")
	agent.RequiredSkills = []string{"lean-tdd"}
	skill := makeSkill("lean-tdd", "1.5")

	// Manifest holds the skill entry at the OLD harness path.
	oldSkillEntry := makeManifestEntry(skillRef("lean-tdd"), oldSkillPath, "1.5", "sha256:old-skill")

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "old-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{oldSkillEntry},
	})

	// New harness module returns new-harness paths; no file exists there.
	module := newFakeModule()
	module.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		switch {
		case req.Kind == domain.ArtifactSkill && req.Key == "lean-tdd":
			return newSkillPath, nil
		case req.Kind == domain.ArtifactAgent && req.Key == "test-agent":
			return "new-harness/agents/test-agent.md", nil
		case req.Kind == domain.ArtifactAgent && req.Key == "orchestrator":
			return "new-harness/agents/orchestrator.md", nil
		}
		return "new-harness/" + req.Key, nil
	}

	// No files at new-harness skill path — absent.
	ds := map[string]domain.DeployedArtifactState{
		newSkillPath:                        {Present: false},
		"new-harness/agents/test-agent.md":  {Present: false},
		"new-harness/agents/orchestrator.md": {Present: false},
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
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "lean-tdd")
	if !ok {
		t.Fatal("plan has no item for skill lean-tdd")
	}
	if item.Action != domain.ActionCreate {
		t.Errorf("cross-harness lean-tdd: Action = %q, want ActionCreate; absent file at new target path must not produce ActionUpdate",
			item.Action)
	}
	if len(item.Stale) > 0 {
		t.Errorf("cross-harness lean-tdd: Stale = %v, want empty; a create item must carry no version deltas",
			item.Stale)
	}
}

// TestBuild_CrossHarness_FilesAbsentAtNewTargetPaths_HookIsCreate verifies the same
// cross-harness behaviour for hook artifacts: an absent file at the new harness's hook
// path must classify as ActionCreate with no version deltas.
func TestBuild_CrossHarness_FilesAbsentAtNewTargetPaths_HookIsCreate(t *testing.T) {
	oldHookPath := "old-harness/hooks/my-hooks"
	newHookPath := "new-harness/hooks/my-hooks"

	agent := makeAgent("test-agent", "1.0")
	hook := makeHookBundle("my-hooks", "2.0")

	// Manifest holds the hook entry at the OLD harness path.
	oldHookEntry := makeManifestEntry(hookRef("my-hooks"), oldHookPath, "2.0", "sha256:old-hook")

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "old-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{oldHookEntry},
	})

	// New harness module returns new-harness paths.
	module := newFakeModule()
	module.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		switch {
		case req.Kind == domain.ArtifactHook && req.Key == "my-hooks":
			return newHookPath, nil
		case req.Kind == domain.ArtifactAgent && req.Key == "test-agent":
			return "new-harness/agents/test-agent.md", nil
		case req.Kind == domain.ArtifactAgent && req.Key == "orchestrator":
			return "new-harness/agents/orchestrator.md", nil
		}
		return "new-harness/" + req.Key, nil
	}

	// No files at new-harness hook path.
	ds := map[string]domain.DeployedArtifactState{
		newHookPath:                         {Present: false},
		"new-harness/agents/test-agent.md":  {Present: false},
		"new-harness/agents/orchestrator.md": {Present: false},
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		hooks:        []domain.HookBundle{hook},
		workflows:    []domain.Workflow{wf},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		HookIDs:       []string{"my-hooks"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "my-hooks")
	if !ok {
		t.Fatal("plan has no item for hook my-hooks")
	}
	if item.Action != domain.ActionCreate {
		t.Errorf("cross-harness my-hooks: Action = %q, want ActionCreate; absent file at new target path must not produce ActionUpdate",
			item.Action)
	}
	if len(item.Stale) > 0 {
		t.Errorf("cross-harness my-hooks: Stale = %v, want empty; a create item must carry no version deltas",
			item.Stale)
	}
}

// ---------------------------------------------------------------------------
// T4.4 — Skill/hook variants: manifest entry at different target path does not apply
// ---------------------------------------------------------------------------

// TestBuild_ManifestEntryAtDifferentTargetPath_SkillFileExistsAtCurrentPath_ClassifiesAsConflict
// verifies the same path-aware lookup behaviour for skill artifacts: when the manifest holds
// an entry at path A but the current run's skill path is B with a file present, the skill
// classifies as ActionConflict (no applicable manifest entry at path B).
func TestBuild_ManifestEntryAtDifferentTargetPath_SkillFileExistsAtCurrentPath_ClassifiesAsConflict(t *testing.T) {
	oldSkillPath := "old-harness/skills/lean-tdd/SKILL.md"
	newSkillPath := "new-harness/skills/lean-tdd/SKILL.md"

	agent := makeAgent("test-agent", "1.0")
	agent.RequiredSkills = []string{"lean-tdd"}
	skill := makeSkill("lean-tdd", "1.5")

	// Manifest entry at old skill path.
	oldSkillEntry := makeManifestEntry(skillRef("lean-tdd"), oldSkillPath, "1.5", "sha256:old-skill")

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "old-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{oldSkillEntry},
	})

	// Current target path is new path; a file exists there with a different hash.
	module := newFakeModule()
	module.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		switch {
		case req.Kind == domain.ArtifactSkill && req.Key == "lean-tdd":
			return newSkillPath, nil
		case req.Kind == domain.ArtifactAgent && req.Key == "test-agent":
			return "new-harness/agents/test-agent.md", nil
		case req.Kind == domain.ArtifactAgent && req.Key == "orchestrator":
			return "new-harness/agents/orchestrator.md", nil
		}
		return "new-harness/" + req.Key, nil
	}

	ds := map[string]domain.DeployedArtifactState{
		newSkillPath:                        deployedState("sha256:independent-skill", "1.5", "", ""),
		"new-harness/agents/test-agent.md":  {Present: false},
		"new-harness/agents/orchestrator.md": {Present: false},
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
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "lean-tdd")
	if !ok {
		t.Fatal("plan has no item for skill lean-tdd")
	}
	if item.Action != domain.ActionConflict {
		t.Errorf("lean-tdd: manifest entry at different path, file at current path → Action = %q, want ActionConflict",
			item.Action)
	}
	if item.Conflict == nil {
		t.Fatal("lean-tdd: Conflict is nil; expected a populated LocalModification")
	}
	if !item.Conflict.ManifestMissing {
		t.Error("LocalModification.ManifestMissing = false; expected true because no manifest entry covers the current skill target path")
	}
}

// TestBuild_ManifestEntryAtDifferentTargetPath_HookFileExistsAtCurrentPath_ClassifiesAsConflict
// verifies the same path-aware lookup behaviour for hook artifacts.
func TestBuild_ManifestEntryAtDifferentTargetPath_HookFileExistsAtCurrentPath_ClassifiesAsConflict(t *testing.T) {
	oldHookPath := "old-harness/hooks/my-hooks"
	newHookPath := "new-harness/hooks/my-hooks"

	agent := makeAgent("test-agent", "1.0")
	hook := makeHookBundle("my-hooks", "2.0")

	// Manifest entry at old hook path.
	oldHookEntry := makeManifestEntry(hookRef("my-hooks"), oldHookPath, "2.0", "sha256:old-hook")

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "old-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{oldHookEntry},
	})

	// Current target path is new path; a file exists there.
	module := newFakeModule()
	module.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		switch {
		case req.Kind == domain.ArtifactHook && req.Key == "my-hooks":
			return newHookPath, nil
		case req.Kind == domain.ArtifactAgent && req.Key == "test-agent":
			return "new-harness/agents/test-agent.md", nil
		case req.Kind == domain.ArtifactAgent && req.Key == "orchestrator":
			return "new-harness/agents/orchestrator.md", nil
		}
		return "new-harness/" + req.Key, nil
	}

	ds := map[string]domain.DeployedArtifactState{
		newHookPath:                         deployedState("sha256:independent-hook", "2.0", "", ""),
		"new-harness/agents/test-agent.md":  {Present: false},
		"new-harness/agents/orchestrator.md": {Present: false},
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		hooks:        []domain.HookBundle{hook},
		workflows:    []domain.Workflow{wf},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		HookIDs:       []string{"my-hooks"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "my-hooks")
	if !ok {
		t.Fatal("plan has no item for hook my-hooks")
	}
	if item.Action != domain.ActionConflict {
		t.Errorf("my-hooks: manifest entry at different path, file at current path → Action = %q, want ActionConflict",
			item.Action)
	}
	if item.Conflict == nil {
		t.Fatal("my-hooks: Conflict is nil; expected a populated LocalModification")
	}
	if !item.Conflict.ManifestMissing {
		t.Error("LocalModification.ManifestMissing = false; expected true because no manifest entry covers the current hook target path")
	}
}

// ---------------------------------------------------------------------------
// Version-downgrade staleness: any version difference is stale, regardless of direction
// ---------------------------------------------------------------------------

// TestBuild_VersionDowngrade_StillClassifiesAsUpdate verifies that when the deployed file
// carries a version that is higher than the source version (as would happen after a catalog
// rollback or a manual version stamp edit), the item still classifies as ActionUpdate.
// Version delta direction is irrelevant: any difference between deployed and source is stale.
func TestBuild_VersionDowngrade_StillClassifiesAsUpdate(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	const hash = "sha256:recorded"

	// Source is "1.0" — older than what is currently deployed ("2.0").
	agent := makeAgent("test-agent", "1.0")

	entry := makeManifestEntry(agentRef("test-agent"), targetPath, "2.0", hash)
	entry.TransformVersion = "1.0"
	entry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{entry},
	})

	// Deployed file has version "2.0" (newer than source "1.0"); hash matches (no local mod).
	ds := map[string]domain.DeployedArtifactState{
		targetPath: deployedState(hash, "2.0", "1.0", "1.0"),
	}

	wf := makeWorkflow("test-wf", agent.Key)
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows:    []domain.Workflow{wf},
	}
	input := plan.Input{
		Catalog:       cat,
		Module:        newFakeModule(),
		Mode:          domain.ModeUpdate,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		WorkflowIDs:   []string{"test-wf"},
		Models: map[string]domain.ModelSelection{
			agent.Key:    {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: ds,
	}

	p, err := plan.New().Build(context.Background(), input)
	must(t, err)

	item, ok := findItem(p.Items, "test-agent")
	if !ok {
		t.Fatal("plan has no item for test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("test-agent: deployed version newer than source → Action = %q, want ActionUpdate; version delta direction is irrelevant",
			item.Action)
	}
}
