package plan_test

// build_multifile_test.go covers Stage 4 requirements for multi-file skill plan building:
//
//   T4.3 — Build plan items multi-file: Build must produce one PlanItem per file in a
//   multi-file skill. Each item carries the correct SourcePath and TargetPath for its file.
//   A single-file skill (only SKILL.md) must continue to produce exactly one PlanItem.
//
//   T4.6 — Classification independence: each file in a multi-file skill is classified
//   independently based on its own deployed state and manifest entry. If one file is locally
//   modified (hash mismatch → ActionConflict) and another is unchanged (hash matches →
//   ActionUnchanged), the two PlanItems carry different Action values.
//
// These tests are written in the TDD RED phase. Build currently produces one PlanItem per
// skill (the skill loop uses paths.Path(ref) and calls classifySkillItem once), so all
// multi-file count assertions fail. The classification independence test also fails because
// only one item is produced, making the second file's classification untestable.

import (
	"context"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers for multi-file skill Build tests
// ---------------------------------------------------------------------------

// makeMultiFileSkill returns a domain.Skill with the given files list. The caller provides
// the files slice; the key, SourceDir, and EntryFile are set to conventional test values.
func makeMultiFileSkill(key string, files []string) domain.Skill {
	return domain.Skill{
		Key:       key,
		Name:      key + "-name",
		Version:   "1.0",
		SourceDir: "/fake/skills/" + key,
		EntryFile: "SKILL.md",
		Files:     files,
	}
}

// skillModuleWithPerFile returns a fakeModule whose TargetPath for skills returns
// "skills/<key>/<filename>" where filename is req.FileName. This enables multi-file
// skill path computation without hardcoding SKILL.md as the single target path.
func skillModuleWithPerFile() *fakeModule {
	mod := newFakeModule()
	mod.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		switch req.Kind {
		case domain.ArtifactAgent:
			return "agents/" + req.Key + ".md", nil
		case domain.ArtifactSkill:
			return "skills/" + req.Key + "/" + req.FileName, nil
		}
		return "", domain.ErrArtifactUnsupported
	}
	return mod
}

// buildInputWithMultiFileSkill returns a plan.Input containing one agent that requires the
// given skill. The module uses per-file skill paths.
func buildInputWithMultiFileSkill(
	skill domain.Skill,
	snap manifest.Snapshot,
	deployedState map[string]domain.DeployedArtifactState,
) plan.Input {
	agent := makeAgent("worker", "1.0")
	agent.RequiredSkills = []string{skill.Key}
	wf := makeWorkflow("test-wf", agent.Key)

	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		skills:       []domain.Skill{skill},
		workflows:    []domain.Workflow{wf},
	}

	return plan.Input{
		Catalog:       cat,
		Module:        skillModuleWithPerFile(),
		Mode:          domain.ModeUpdateWorkspace,
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

// allSkillItems returns all PlanItems from items whose Ref has the given key and
// Kind == ArtifactSkill. Unlike findItem, this returns all matching items, which
// is necessary for multi-file skills where multiple items share the same ArtifactRef.
func allSkillItems(items []domain.PlanItem, skillKey string) []domain.PlanItem {
	var result []domain.PlanItem
	for _, item := range items {
		if item.Ref.Kind == domain.ArtifactSkill && item.Ref.Key == skillKey {
			result = append(result, item)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// T4.3 — Build produces one PlanItem per file for multi-file skills
// ---------------------------------------------------------------------------

// TestBuild_MultiFileSkill_ProducesOnePlanItemPerFile verifies that Build produces one
// PlanItem for each file in a multi-file skill, not one item for the entire skill.
// With the current single-item-per-skill implementation this test fails because
// len(skillItems) == 1, not 2.
func TestBuild_MultiFileSkill_ProducesOnePlanItemPerFile(t *testing.T) {
	skill := makeMultiFileSkill("lean-tdd", []string{"SKILL.md", "companion.md"})

	input := buildInputWithMultiFileSkill(skill, absentSnapshot(), nil)
	p, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	skillItems := allSkillItems(p.Items, "lean-tdd")
	if len(skillItems) != 2 {
		t.Errorf("multi-file skill (2 files) produced %d PlanItem(s), want 2\n"+
			"  Build must call classifySkillItem once per file in skill.Files, not once per skill",
			len(skillItems))
	}
}

// TestBuild_MultiFileSkill_ThreeFiles_ProducesThreePlanItems verifies the general case:
// a skill with N files produces N PlanItems.
func TestBuild_MultiFileSkill_ThreeFiles_ProducesThreePlanItems(t *testing.T) {
	skill := makeMultiFileSkill("lean-tdd", []string{
		"SKILL.md",
		"examples/usage.md",
		"templates/default.md",
	})

	input := buildInputWithMultiFileSkill(skill, absentSnapshot(), nil)
	p, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	skillItems := allSkillItems(p.Items, "lean-tdd")
	if len(skillItems) != 3 {
		t.Errorf("multi-file skill (3 files) produced %d PlanItem(s), want 3", len(skillItems))
	}
}

// TestBuild_MultiFileSkill_AllItemsShareSameArtifactRef verifies that all PlanItems
// produced for a multi-file skill share the same ArtifactRef (same Kind and Key).
// The Ref identifies the skill, while SourcePath/TargetPath identify the specific file.
func TestBuild_MultiFileSkill_AllItemsShareSameArtifactRef(t *testing.T) {
	skill := makeMultiFileSkill("lean-tdd", []string{"SKILL.md", "companion.md"})

	input := buildInputWithMultiFileSkill(skill, absentSnapshot(), nil)
	p, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	skillItems := allSkillItems(p.Items, "lean-tdd")
	// Assert at least 2 items before the range loop so this test is RED when multi-file
	// Build is absent (1 item) and does not pass vacuously with 0 or 1 items.
	if len(skillItems) < 2 {
		t.Fatalf("multi-file skill (2 files) produced %d PlanItem(s), want >= 2; "+
			"Build must produce one PlanItem per file before the ArtifactRef assertion can be meaningful",
			len(skillItems))
	}
	wantRef := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}
	for i, item := range skillItems {
		if item.Ref != wantRef {
			t.Errorf("PlanItem[%d].Ref = %+v, want %+v", i, item.Ref, wantRef)
		}
	}
}

// TestBuild_MultiFileSkill_PlanItemsHaveCorrectSourcePaths verifies that each PlanItem
// for a multi-file skill has SourcePath set to filepath.Join(skill.SourceDir, file),
// where file is the corresponding element from skill.Files.
func TestBuild_MultiFileSkill_PlanItemsHaveCorrectSourcePaths(t *testing.T) {
	skill := makeMultiFileSkill("lean-tdd", []string{"SKILL.md", "examples/usage.md"})

	input := buildInputWithMultiFileSkill(skill, absentSnapshot(), nil)
	p, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	skillItems := allSkillItems(p.Items, "lean-tdd")
	if len(skillItems) != 2 {
		t.Fatalf("expected 2 skill PlanItems, got %d (Build must produce one per file)", len(skillItems))
	}

	wantSourcePaths := map[string]bool{
		filepath.Join("/fake/skills/lean-tdd", "SKILL.md"):         false,
		filepath.Join("/fake/skills/lean-tdd", "examples/usage.md"): false,
	}
	for _, item := range skillItems {
		if _, expected := wantSourcePaths[item.SourcePath]; expected {
			wantSourcePaths[item.SourcePath] = true
		} else {
			t.Errorf("PlanItem has unexpected SourcePath %q", item.SourcePath)
		}
	}
	for path, found := range wantSourcePaths {
		if !found {
			t.Errorf("no PlanItem has SourcePath %q", path)
		}
	}
}

// TestBuild_MultiFileSkill_PlanItemsHaveCorrectTargetPaths verifies that each PlanItem
// has TargetPath set to the value returned by the harness module for that file. The
// module receives req.FileName = the file's relative path (e.g., "examples/usage.md")
// and returns "skills/<key>/<filename>".
func TestBuild_MultiFileSkill_PlanItemsHaveCorrectTargetPaths(t *testing.T) {
	skill := makeMultiFileSkill("lean-tdd", []string{"SKILL.md", "examples/usage.md"})

	input := buildInputWithMultiFileSkill(skill, absentSnapshot(), nil)
	p, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	skillItems := allSkillItems(p.Items, "lean-tdd")
	if len(skillItems) != 2 {
		t.Fatalf("expected 2 skill PlanItems, got %d", len(skillItems))
	}

	wantTargetPaths := map[string]bool{
		"skills/lean-tdd/SKILL.md":         false,
		"skills/lean-tdd/examples/usage.md": false,
	}
	for _, item := range skillItems {
		if _, expected := wantTargetPaths[item.TargetPath]; expected {
			wantTargetPaths[item.TargetPath] = true
		} else {
			t.Errorf("PlanItem has unexpected TargetPath %q", item.TargetPath)
		}
	}
	for path, found := range wantTargetPaths {
		if !found {
			t.Errorf("no PlanItem has TargetPath %q", path)
		}
	}
}

// ---------------------------------------------------------------------------
// T4.3 — Single-file skill backward compatibility
// ---------------------------------------------------------------------------

// TestBuild_SingleFileSkill_ProducesExactlyOnePlanItem verifies that a skill with
// Files = ["SKILL.md"] (or only EntryFile) continues to produce exactly one PlanItem
// after the multi-file change. This is a regression guard.
func TestBuild_SingleFileSkill_ProducesExactlyOnePlanItem(t *testing.T) {
	skill := makeMultiFileSkill("lean-tdd", []string{"SKILL.md"})

	input := buildInputWithMultiFileSkill(skill, absentSnapshot(), nil)
	p, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	skillItems := allSkillItems(p.Items, "lean-tdd")
	if len(skillItems) != 1 {
		t.Errorf("single-file skill produced %d PlanItem(s), want 1", len(skillItems))
	}
}

// ---------------------------------------------------------------------------
// T4.3 — Multi-file skill action: new deployment (no deployed files)
// ---------------------------------------------------------------------------

// TestBuild_MultiFileSkill_NewDeployment_AllFilesAreActionCreate verifies that when none
// of a multi-file skill's files exist at their target paths, every PlanItem has
// Action == ActionCreate. This is the initial-deployment scenario.
func TestBuild_MultiFileSkill_NewDeployment_AllFilesAreActionCreate(t *testing.T) {
	skill := makeMultiFileSkill("lean-tdd", []string{"SKILL.md", "companion.md"})

	// No deployed state: all files are absent.
	input := buildInputWithMultiFileSkill(skill, absentSnapshot(), nil)
	p, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	skillItems := allSkillItems(p.Items, "lean-tdd")
	if len(skillItems) != 2 {
		t.Fatalf("expected 2 skill PlanItems for new deployment, got %d", len(skillItems))
	}
	for _, item := range skillItems {
		if item.Action != domain.ActionCreate {
			t.Errorf("new-deployment file %q has Action = %v, want ActionCreate",
				item.TargetPath, item.Action)
		}
	}
}

// ---------------------------------------------------------------------------
// T4.6 — Classification independence: each file classified independently
// ---------------------------------------------------------------------------

// TestBuild_MultiFileSkill_ClassificationIsIndependentPerFile verifies that when a
// multi-file skill is partially locally modified, each file is classified based on its
// own deployed state and manifest entry. The locally-modified file gets ActionConflict;
// the unmodified file gets ActionUnchanged.
//
// This test requires Build to produce 2 PlanItems (one per file), so it depends on T4.3
// being met. With the current single-item implementation, the test fails at the count
// assertion before reaching the action assertions.
func TestBuild_MultiFileSkill_ClassificationIsIndependentPerFile(t *testing.T) {
	skill := makeMultiFileSkill("lean-tdd", []string{"SKILL.md", "companion.md"})

	const entryTargetPath    = "skills/lean-tdd/SKILL.md"
	const companionTargetPath = "skills/lean-tdd/companion.md"
	const recordedHash       = "abc123"
	const modifiedHash       = "xyz789" // differs from recorded → conflict

	skillRef := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}

	// Manifest has entries for both files with the same recorded hash.
	m := domain.Manifest{
		Entries: []domain.ManifestEntry{
			makeManifestEntry(skillRef, entryTargetPath, "1.0", recordedHash),
			makeManifestEntry(skillRef, companionTargetPath, "1.0", recordedHash),
		},
	}

	// SKILL.md: unchanged (deployed hash matches recorded hash).
	// companion.md: locally modified (deployed hash differs from recorded hash).
	deployedState := map[string]domain.DeployedArtifactState{
		entryTargetPath:    deployedState(recordedHash, "1.0", "1.0", "1.0"),
		companionTargetPath: deployedState(modifiedHash, "", "", ""),
	}

	input := buildInputWithMultiFileSkill(skill, presentSnapshot(m), deployedState)
	p, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	skillItems := allSkillItems(p.Items, "lean-tdd")
	if len(skillItems) != 2 {
		t.Fatalf("expected 2 skill PlanItems for classification independence test, got %d\n"+
			"  Build must produce one PlanItem per file to enable per-file classification",
			len(skillItems))
	}

	// Find items by target path.
	itemByPath := make(map[string]domain.PlanItem, 2)
	for _, item := range skillItems {
		itemByPath[item.TargetPath] = item
	}

	// SKILL.md: deployed hash matches manifest → ActionUnchanged.
	entryItem, ok := itemByPath[entryTargetPath]
	if !ok {
		t.Errorf("no PlanItem found for entry file target path %q", entryTargetPath)
	} else if entryItem.Action != domain.ActionUnchanged {
		t.Errorf("entry file (hash matches manifest) has Action = %v, want ActionUnchanged",
			entryItem.Action)
	}

	// companion.md: deployed hash differs from manifest → ActionConflict.
	companionItem, ok := itemByPath[companionTargetPath]
	if !ok {
		t.Errorf("no PlanItem found for companion file target path %q", companionTargetPath)
	} else if companionItem.Action != domain.ActionConflict {
		t.Errorf("companion file (hash mismatch) has Action = %v, want ActionConflict",
			companionItem.Action)
	}
}

// TestBuild_MultiFileSkill_EntryFileVersionStale_OnlyEntryFileUpdated verifies the
// staleness propagation contract: when skill.Version changes, only the entry file
// (isEntryFile == true) is marked ActionUpdate via SkillStaleness. Non-entry files whose
// content hash matches the manifest entry are not affected by the version change and
// remain ActionUnchanged.
//
// This requires Build to call classifySkillItem with an isEntryFile flag and to skip
// SkillStaleness for non-entry files. Both behaviors are absent from the current
// implementation, so this test fails when multi-file support is absent (count = 1).
func TestBuild_MultiFileSkill_EntryFileVersionStale_OnlyEntryFileUpdated(t *testing.T) {
	// skill.Version is "2.0" but manifest was written for "1.0".
	skill := domain.Skill{
		Key:       "lean-tdd",
		Name:      "lean-tdd-name",
		Version:   "2.0",
		SourceDir: "/fake/skills/lean-tdd",
		EntryFile: "SKILL.md",
		Files:     []string{"SKILL.md", "companion.md"},
	}

	const entryTargetPath    = "skills/lean-tdd/SKILL.md"
	const companionTargetPath = "skills/lean-tdd/companion.md"
	const contentHash        = "stablehash"

	skillRef := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}

	// Manifest records version "1.0" for the entry file, same content hash for both.
	m := domain.Manifest{
		Entries: []domain.ManifestEntry{
			makeManifestEntry(skillRef, entryTargetPath, "1.0", contentHash),
			makeManifestEntry(skillRef, companionTargetPath, "1.0", contentHash),
		},
	}

	// Both files exist with the same content hash as manifest (no local modification).
	// But the entry file will be stale because skill.Version ("2.0") != deployed.Version ("1.0").
	deployed := map[string]domain.DeployedArtifactState{
		entryTargetPath:    deployedState(contentHash, "1.0", "1.0", "1.0"),
		companionTargetPath: deployedState(contentHash, "", "", ""),
	}

	input := buildInputWithMultiFileSkill(skill, presentSnapshot(m), deployed)
	p, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	skillItems := allSkillItems(p.Items, "lean-tdd")
	if len(skillItems) != 2 {
		t.Fatalf("expected 2 skill PlanItems, got %d (multi-file required for this test)",
			len(skillItems))
	}

	itemByPath := make(map[string]domain.PlanItem, 2)
	for _, item := range skillItems {
		itemByPath[item.TargetPath] = item
	}

	// Entry file: version changed → ActionUpdate.
	entryItem, ok := itemByPath[entryTargetPath]
	if !ok {
		t.Errorf("no PlanItem for entry file %q", entryTargetPath)
	} else if entryItem.Action != domain.ActionUpdate {
		t.Errorf("entry file (version 1.0→2.0) has Action = %v, want ActionUpdate",
			entryItem.Action)
	}

	// Companion file (non-entry): content hash matches; SkillStaleness must NOT be called
	// (isEntryFile == false), so the spurious version delta is suppressed → ActionUnchanged.
	companionItem, ok := itemByPath[companionTargetPath]
	if !ok {
		t.Errorf("no PlanItem for companion file %q", companionTargetPath)
	} else if companionItem.Action != domain.ActionUnchanged {
		t.Errorf("companion file (content unchanged, non-entry, version delta suppressed) "+
			"has Action = %v, want ActionUnchanged", companionItem.Action)
	}
}
