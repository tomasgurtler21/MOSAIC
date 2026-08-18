package deploy_test

// hook_catalog_regression_test.go covers the tests for the hook deployment catalog fix.
//
// Background: hook plan items carry SourcePath set to the bundle's directory (not a single
// file). The catalog only registers individual file paths in its sourcePaths whitelist, so
// when the executor routes hook items through req.Content / ReadSource, the call fails with
// "catalog: path ... was not emitted by this catalog". The fix must make hook items succeed
// without going through the content path.
//
// Three areas are covered:
//
// Regression (T3.1): A hook plan item whose SourcePath is a bundle directory and whose
// Content function returns a catalog error must not cause Execute to fail or report the item
// as TakenFailed. The hook must appear in Actions as TakenCreated (or TakenUpdated), and no
// spurious single file must be written at the item's TargetPath.
//
// Manifest entry preservation (T3.2): Hook plan items must still produce a manifest entry
// carrying the correct Ref, TargetPath, and version stamp (Version field) so that the
// staleness planner can compare entry.Version against bundle.Version on the next run.
//
// Agent/skill regression guard (T3.3): Agents and skills must continue to flow through the
// content path unchanged; their deployed bytes must match what req.Content returned, and
// their manifest entries must carry a content hash.

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// Local builders
// ---------------------------------------------------------------------------

// newHookItem builds a PlanItem for a hook bundle, with SourcePath set to a bundle directory
// (the path that classifyHookItem supplies and that the catalog never registers as a file).
func newHookItem(key, bundleDir, targetPath string, action domain.PlanAction) domain.PlanItem {
	return domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactHook, Key: key},
		SourcePath: bundleDir, // directory; deliberately not a registered catalog file path
		TargetPath: targetPath,
		Action:     action,
	}
}

// newSkillItem builds a PlanItem for a skill, using targetPath as the deployment path
// relative to the deployment root.
func newSkillItem(key, targetPath string, action domain.PlanAction) domain.PlanItem {
	return domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: key},
		SourcePath: filepath.Join("testdata", key+".md"),
		TargetPath: targetPath,
		Action:     action,
	}
}

// catalogErrorContent returns a Content function that returns the catalog error message for
// any item whose SourcePath matches bundleDir, and returns fixed bytes for all other items.
// This reproduces the exact failure mode: the catalog rejects unregistered directory paths.
func catalogErrorContent(bundleDir string) func(domain.PlanItem) ([]byte, error) {
	return func(item domain.PlanItem) ([]byte, error) {
		if item.SourcePath == bundleDir {
			return nil, fmt.Errorf("catalog: path %q was not emitted by this catalog", bundleDir)
		}
		return []byte("# content"), nil
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Regression: hook item with bundle directory SourcePath must not fail
// ---------------------------------------------------------------------------

// TestHookItem_Create_DoesNotFailWhenSourcePathIsDirectory reproduces the bug: a hook plan
// item whose SourcePath is a bundle directory causes req.Content to return the catalog error
// "was not emitted by this catalog". After the fix, Execute must succeed and the hook item
// must be reported as TakenCreated, not TakenFailed.
//
// Before the fix this test fails at two points where req.Content is called with the hook
// item: first in resolveDeploymentRoot (which uses the hook item as the writability probe
// item, calling Content on it and returning an error before any items are processed), and
// second in executeItem which routes ActionCreate/ActionUpdate items through Content. Both
// entry points must be fixed to exclude hook items from the content path.
func TestHookItem_Create_DoesNotFailWhenSourcePathIsDirectory(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir() // bundle directory — never registered as a catalog file path

	hookItem := newHookItem("subagent-logger", bundleDir, "hooks/subagent-logger", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem),
		MosaicRoot: mosaicRoot,
		Content:    catalogErrorContent(bundleDir),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	// Find the action record for the hook item.
	var found bool
	for _, a := range result.Actions {
		if a.Ref == hookItem.Ref {
			found = true
			if a.Taken == domain.TakenFailed {
				t.Errorf("hook item reported TakenFailed with error %q; want TakenCreated", a.Err)
			}
			if a.Taken != domain.TakenCreated {
				t.Errorf("hook item Taken = %q; want TakenCreated", a.Taken)
			}
			break
		}
	}
	if !found {
		t.Errorf("no action record found for hook item %q in result.Actions", hookItem.Ref)
	}
}

// TestHookItem_Update_DoesNotFailWhenSourcePathIsDirectory verifies that the fix covers
// ActionUpdate hook items as well as ActionCreate: an update whose Content function returns
// the catalog error must still be reported as TakenUpdated, not TakenFailed.
func TestHookItem_Update_DoesNotFailWhenSourcePathIsDirectory(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir()

	hookItem := newHookItem("subagent-logger", bundleDir, "hooks/subagent-logger", domain.ActionUpdate)
	hookItem.Stale = []domain.VersionDelta{
		{Field: "version", Deployed: "1.0.0", Source: "2.0.0"},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem),
		MosaicRoot: mosaicRoot,
		Content:    catalogErrorContent(bundleDir),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	for _, a := range result.Actions {
		if a.Ref == hookItem.Ref {
			if a.Taken == domain.TakenFailed {
				t.Errorf("hook update item reported TakenFailed with error %q; want TakenUpdated", a.Err)
			}
			if a.Taken != domain.TakenUpdated {
				t.Errorf("hook update item Taken = %q; want TakenUpdated", a.Taken)
			}
			return
		}
	}
	t.Errorf("no action record found for hook item %q", hookItem.Ref)
}

// TestHookItem_Create_NoSpuriousSingleFileWrittenAtTargetPath verifies that the fix does not
// accidentally write a single file at the hook item's TargetPath inside the workspace. Hook
// bundle files are managed by deployHooks (writing per-file entries under TargetDir); the
// per-item path must remain absent so no bogus single-file artifact is created there.
func TestHookItem_Create_NoSpuriousSingleFileWrittenAtTargetPath(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir()

	targetPath := "hooks/subagent-logger"
	hookItem := newHookItem("subagent-logger", bundleDir, targetPath, domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem),
		MosaicRoot: mosaicRoot,
		Content:    catalogErrorContent(bundleDir),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	spuriousPath := filepath.Join(workspace, targetPath)
	if fileExistsAt(t, spuriousPath) {
		t.Errorf("spurious single file found at %q; hook items must not produce a single-file write at their TargetPath", spuriousPath)
	}
}

// TestHookItem_DryRun_ReportsCreatedWithoutError verifies that dry-run simulation for hook
// items reports TakenCreated (simulated) and does not surface the catalog error. Dry-run
// must remain coherent with the fix: if the item would be created in a real run, the
// dry-run must report the same intent.
func TestHookItem_DryRun_ReportsCreatedWithoutError(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir()

	hookItem := newHookItem("subagent-logger", bundleDir, "hooks/subagent-logger", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem),
		MosaicRoot: mosaicRoot,
		Content:    catalogErrorContent(bundleDir),
		DryRun:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute dry-run: %v", err)
	}

	for _, a := range result.Actions {
		if a.Ref == hookItem.Ref {
			if a.Taken != domain.TakenCreated {
				t.Errorf("dry-run hook item Taken = %q; want TakenCreated", a.Taken)
			}
			return
		}
	}
	t.Errorf("no action record for hook item in dry-run result")
}

// ---------------------------------------------------------------------------
// T3.2 — Hook manifest entry creation and version-stamp preservation
// ---------------------------------------------------------------------------

// TestHookItem_Create_ManifestEntryProduced verifies that a successfully processed
// ActionCreate hook item produces a manifest entry. Before the fix, the catalog error causes
// TakenFailed and no entry is written. After the fix an entry must be present so that a
// subsequent run can detect the hook as unchanged (staleness detection requires the entry).
func TestHookItem_Create_ManifestEntryProduced(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir()

	hookItem := newHookItem("subagent-logger", bundleDir, "hooks/subagent-logger", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem),
		MosaicRoot: mosaicRoot,
		Content:    catalogErrorContent(bundleDir),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, found := result.Manifest.Lookup(hookItem.Ref)
	if !found {
		t.Errorf("manifest has no entry for hook item %q; entry is required for staleness detection", hookItem.Ref)
	}
}

// TestHookItem_Create_ManifestEntryHasCorrectRef verifies that the manifest entry produced
// for a hook item carries the exact ArtifactRef from the plan item.
func TestHookItem_Create_ManifestEntryHasCorrectRef(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir()

	hookItem := newHookItem("subagent-logger", bundleDir, "hooks/subagent-logger", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem),
		MosaicRoot: mosaicRoot,
		Content:    catalogErrorContent(bundleDir),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(hookItem.Ref)
	if !found {
		t.Fatalf("manifest has no entry for hook item %q", hookItem.Ref)
	}
	if entry.Ref != hookItem.Ref {
		t.Errorf("manifest entry Ref = %v; want %v", entry.Ref, hookItem.Ref)
	}
}

// TestHookItem_Create_ManifestEntryHasCorrectTargetPath verifies that the manifest entry
// stores the hook item's TargetPath (relative to deployment root) as-is, since this is the
// key the staleness planner uses to find the entry on subsequent runs.
func TestHookItem_Create_ManifestEntryHasCorrectTargetPath(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir()

	targetPath := "hooks/subagent-logger"
	hookItem := newHookItem("subagent-logger", bundleDir, targetPath, domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem),
		MosaicRoot: mosaicRoot,
		Content:    catalogErrorContent(bundleDir),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(hookItem.Ref)
	if !found {
		t.Fatalf("manifest has no entry for hook item")
	}
	if entry.TargetPath != targetPath {
		t.Errorf("manifest entry TargetPath = %q; want %q", entry.TargetPath, targetPath)
	}
}

// TestHookItem_Create_ManifestEntryCarriesVersion verifies that the Version field in the
// manifest entry matches the value supplied in VersionStamps. This is the field that
// HookStaleness compares against bundle.Version to decide whether to re-deploy a hook.
// Without this field, every subsequent run would misclassify an unchanged hook as stale.
func TestHookItem_Create_ManifestEntryCarriesVersion(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir()

	targetPath := "hooks/subagent-logger"
	hookItem := newHookItem("subagent-logger", bundleDir, targetPath, domain.ActionCreate)
	expectedVersion := "1.2.3"

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem),
		MosaicRoot: mosaicRoot,
		Content:    catalogErrorContent(bundleDir),
		VersionStamps: map[string]domain.VersionStamp{
			targetPath: {Version: expectedVersion},
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(hookItem.Ref)
	if !found {
		t.Fatalf("manifest has no entry for hook item; cannot verify version stamp")
	}
	if entry.Version != expectedVersion {
		t.Errorf("manifest entry Version = %q; want %q", entry.Version, expectedVersion)
	}
}

// TestHookItem_Update_ManifestEntryVersionFromStaleDelta verifies that for an ActionUpdate
// hook item, the Version field in the manifest entry is taken from the item's Stale version
// delta (the new source version), not from the VersionStamps map. This mirrors the
// resolveVersionStamp logic used for all item types.
func TestHookItem_Update_ManifestEntryVersionFromStaleDelta(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir()

	targetPath := "hooks/subagent-logger"
	hookItem := newHookItem("subagent-logger", bundleDir, targetPath, domain.ActionUpdate)
	hookItem.Stale = []domain.VersionDelta{
		{Field: "version", Deployed: "1.0.0", Source: "2.0.0"},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem),
		MosaicRoot: mosaicRoot,
		Content:    catalogErrorContent(bundleDir),
		VersionStamps: map[string]domain.VersionStamp{
			targetPath: {Version: "1.0.0"}, // stale delta must override this
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(hookItem.Ref)
	if !found {
		t.Fatalf("manifest has no entry for updated hook item; cannot verify version stamp")
	}
	if entry.Version != "2.0.0" {
		t.Errorf("manifest entry Version = %q; want %q (from stale delta Source)", entry.Version, "2.0.0")
	}
}

// TestHookItem_Update_PriorManifestEntryReplaced verifies that an ActionUpdate hook item
// replaces the prior manifest entry rather than leaving the old version stamp in place.
func TestHookItem_Update_PriorManifestEntryReplaced(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir()

	targetPath := "hooks/subagent-logger"
	hookRef := domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "subagent-logger"}

	// Seed the manifest with a prior entry carrying version "1.0.0".
	store := manifest.NewStore()
	prior := domain.Manifest{
		SchemaVersion: "1",
		HarnessID:     "test-harness",
		Entries: []domain.ManifestEntry{
			{Ref: hookRef, TargetPath: targetPath, Version: "1.0.0"},
		},
	}
	if err := store.Save(workspace, prior); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	hookItem := newHookItem("subagent-logger", bundleDir, targetPath, domain.ActionUpdate)
	hookItem.Stale = []domain.VersionDelta{
		{Field: "version", Deployed: "1.0.0", Source: "2.0.0"},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem),
		MosaicRoot: mosaicRoot,
		Content:    catalogErrorContent(bundleDir),
	}

	ex := deploy.NewExecutor(store, newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(hookRef)
	if !found {
		t.Fatal("manifest has no entry for updated hook item")
	}
	if entry.Version == "1.0.0" {
		t.Errorf("manifest entry Version is still the prior value %q; it must be updated to the new version", entry.Version)
	}
}

// ---------------------------------------------------------------------------
// Hook conflict + overwrite regression: ActionConflict with DecisionOverwrite
// or DecisionBackupThenOverwrite must not call req.Content for hook items.
// ---------------------------------------------------------------------------

// TestHookItem_Conflict_Overwrite_DoesNotCallContent verifies that when a hook plan item has
// ActionConflict and the user chooses DecisionOverwrite, the executor does not call
// req.Content (which would return the catalog error). The hook must be reported as
// TakenUpdated and a manifest entry must be produced.
func TestHookItem_Conflict_Overwrite_DoesNotCallContent(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir()

	targetPath := "hooks/subagent-logger"
	hookItem := newHookItem("subagent-logger", bundleDir, targetPath, domain.ActionConflict)
	hookItem.Conflict = &domain.LocalModification{
		RecordedHash: "sha256:aaa",
		CurrentHash:  "sha256:bbb",
	}
	expectedVersion := "3.0.0"

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem),
		MosaicRoot: mosaicRoot,
		Content:    catalogErrorContent(bundleDir),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionOverwrite},
		VersionStamps: map[string]domain.VersionStamp{
			targetPath: {Version: expectedVersion},
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	var found bool
	for _, a := range result.Actions {
		if a.Ref == hookItem.Ref {
			found = true
			if a.Taken == domain.TakenFailed {
				t.Errorf("hook conflict+overwrite reported TakenFailed with error %q; want TakenUpdated", a.Err)
			}
			if a.Taken != domain.TakenUpdated {
				t.Errorf("hook conflict+overwrite Taken = %q; want TakenUpdated", a.Taken)
			}
			break
		}
	}
	if !found {
		t.Fatalf("no action record found for hook item in result.Actions")
	}

	entry, ok := result.Manifest.Lookup(hookItem.Ref)
	if !ok {
		t.Fatalf("manifest has no entry for hook conflict+overwrite item")
	}
	if entry.Version != expectedVersion {
		t.Errorf("manifest entry Version = %q; want %q", entry.Version, expectedVersion)
	}
}

// TestHookItem_Conflict_BackupThenOverwrite_DoesNotCallContent verifies that when a hook
// plan item has ActionConflict and the user chooses DecisionBackupThenOverwrite, the executor
// does not call req.Content (which would return the catalog error). The hook must be reported
// as TakenBackedUp and a manifest entry must be produced.
func TestHookItem_Conflict_BackupThenOverwrite_DoesNotCallContent(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir()

	targetPath := "hooks/subagent-logger"
	hookItem := newHookItem("subagent-logger", bundleDir, targetPath, domain.ActionConflict)
	hookItem.Conflict = &domain.LocalModification{
		RecordedHash: "sha256:aaa",
		CurrentHash:  "sha256:bbb",
	}
	expectedVersion := "4.0.0"

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem),
		MosaicRoot: mosaicRoot,
		Content:    catalogErrorContent(bundleDir),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionBackupThenOverwrite},
		VersionStamps: map[string]domain.VersionStamp{
			targetPath: {Version: expectedVersion},
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	var found bool
	for _, a := range result.Actions {
		if a.Ref == hookItem.Ref {
			found = true
			if a.Taken == domain.TakenFailed {
				t.Errorf("hook conflict+backup reported TakenFailed with error %q; want TakenBackedUp", a.Err)
			}
			if a.Taken != domain.TakenBackedUp {
				t.Errorf("hook conflict+backup Taken = %q; want TakenBackedUp", a.Taken)
			}
			break
		}
	}
	if !found {
		t.Fatalf("no action record found for hook item in result.Actions")
	}

	entry, ok := result.Manifest.Lookup(hookItem.Ref)
	if !ok {
		t.Fatalf("manifest has no entry for hook conflict+backup item")
	}
	if entry.Version != expectedVersion {
		t.Errorf("manifest entry Version = %q; want %q", entry.Version, expectedVersion)
	}
}

// ---------------------------------------------------------------------------
// T3.3 — Agent and skill content path is not affected by the hook fix
// ---------------------------------------------------------------------------

// TestAgentItem_Create_ContentBytesWrittenToDisk verifies that for an ActionCreate agent
// item, the bytes returned by req.Content are written verbatim to the deployment target.
// This guards against an implementation change that accidentally removes the content-write
// path for agents when routing hook items away from it.
func TestAgentItem_Create_ContentBytesWrittenToDisk(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	agentContent := []byte("# My Agent\nversion: 1.0.0\n")
	item := newAgentItem("my-agent", "agents/my-agent.md", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(agentContent),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	deployedPath := filepath.Join(workspace, item.TargetPath)
	if !fileExistsAt(t, deployedPath) {
		t.Fatalf("agent file not found at %q after deploy", deployedPath)
	}
	deployed := readFile(t, deployedPath)
	if string(deployed) != string(agentContent) {
		t.Errorf("deployed agent content:\ngot:  %q\nwant: %q", deployed, agentContent)
	}
}

// TestSkillItem_Create_ContentBytesWrittenToDisk verifies that skill items still flow
// through the content path: the bytes from req.Content are written to the deployment target,
// and the skill is not accidentally treated as a bundle that bypasses the content write.
func TestSkillItem_Create_ContentBytesWrittenToDisk(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	skillContent := []byte("# Skill\nversion: 2.0.0\n")
	item := newSkillItem("lean-tdd", "skills/lean-tdd/SKILL.md", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(skillContent),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	deployedPath := filepath.Join(workspace, item.TargetPath)
	if !fileExistsAt(t, deployedPath) {
		t.Fatalf("skill file not found at %q after deploy", deployedPath)
	}
	deployed := readFile(t, deployedPath)
	if string(deployed) != string(skillContent) {
		t.Errorf("deployed skill content:\ngot:  %q\nwant: %q", deployed, skillContent)
	}
}

// TestAgentItem_Create_ManifestEntryHasContentHash verifies that the manifest entry for a
// deployed agent item contains a content hash derived from the deployed bytes. A missing
// hash would break conflict detection on subsequent runs.
func TestAgentItem_Create_ManifestEntryHasContentHash(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("my-agent", "agents/my-agent.md", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# agent content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatalf("manifest has no entry for agent item")
	}
	if entry.ContentHash == "" {
		t.Errorf("agent manifest entry ContentHash is empty; content path must still hash agent bytes")
	}
}

// TestSkillItem_Create_ManifestEntryHasContentHash verifies that skill manifest entries
// still carry a content hash, confirming the content path is unchanged for skills.
func TestSkillItem_Create_ManifestEntryHasContentHash(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newSkillItem("lean-tdd", "skills/lean-tdd/SKILL.md", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# skill content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatalf("manifest has no entry for skill item")
	}
	if entry.ContentHash == "" {
		t.Errorf("skill manifest entry ContentHash is empty; content path must still hash skill bytes")
	}
}

// TestMixedPlan_HookAndAgentItems_AgentSucceedsRegardlessOfHookBehavior verifies that when
// a plan contains both a hook item and an agent item, the agent is deployed successfully
// regardless of how the hook item is handled. This ensures the hook fix is narrowly scoped
// and does not affect items of other kinds.
func TestMixedPlan_HookAndAgentItems_AgentSucceedsRegardlessOfHookBehavior(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	bundleDir := t.TempDir()

	hookItem := newHookItem("subagent-logger", bundleDir, "hooks/subagent-logger", domain.ActionCreate)
	agentItem := newAgentItem("my-agent", "agents/my-agent.md", domain.ActionCreate)

	agentContent := []byte("# agent bytes")

	content := func(item domain.PlanItem) ([]byte, error) {
		if item.SourcePath == bundleDir {
			return nil, fmt.Errorf("catalog: path %q was not emitted by this catalog", bundleDir)
		}
		return agentContent, nil
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, hookItem, agentItem),
		MosaicRoot: mosaicRoot,
		Content:    content,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The agent must have been deployed successfully.
	agentPath := filepath.Join(workspace, agentItem.TargetPath)
	if !fileExistsAt(t, agentPath) {
		t.Errorf("agent file not found at %q; mixed-plan agent must still be deployed", agentPath)
	}

	_, found := result.Manifest.Lookup(agentItem.Ref)
	if !found {
		t.Errorf("manifest has no entry for agent item in mixed plan; agent manifest entry must always be produced")
	}
}
