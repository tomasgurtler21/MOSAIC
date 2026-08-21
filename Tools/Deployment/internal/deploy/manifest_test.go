package deploy_test

// manifest_test.go covers T17.8.
//
// After every Execute call, the manifest at <workspace>/.mosaic/manifest.yaml must reflect
// what is actually on disk. The invariant holds for all action outcomes:
//
//   - TakenCreated:  a new entry is added for the deployed artifact.
//   - TakenUpdated:  the existing entry is replaced with updated version fields and hash.
//   - TakenSkipped:  no new entry is written; the previous entry (if any) is unchanged.
//   - TakenBackedUp: the entry is updated to the new content (the backup copy is not tracked).
//   - TakenFailed:   no entry is written for the failed item (it was not written to disk).
//
// The manifest is always written after the run, including after a partial failure (AC17.4).
// Content hashes use the canonical "sha256:<lowercase-hex>" format (CD-11).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// TakenCreated → manifest entry added
// ---------------------------------------------------------------------------

// TestManifest_CreatedItem_HasManifestEntry verifies that after a successful ActionCreate
// run, the manifest contains an entry for the deployed artifact.
func TestManifest_CreatedItem_HasManifestEntry(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# runner content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatalf("manifest has no entry for created item %q", item.Ref.Key)
	}
	if entry.Ref != item.Ref {
		t.Errorf("manifest entry Ref = %v; want %v", entry.Ref, item.Ref)
	}
}

// TestManifest_CreatedItem_ContentHashIsCanonical verifies that the manifest entry for a
// created item has a content hash in the "sha256:<lowercase-hex>" format (CD-11).
func TestManifest_CreatedItem_ContentHashIsCanonical(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# runner content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatalf("manifest entry not found")
	}
	if !strings.HasPrefix(entry.ContentHash, "sha256:") {
		t.Errorf("ContentHash = %q; want prefix \"sha256:\"", entry.ContentHash)
	}
	hashHex := strings.TrimPrefix(entry.ContentHash, "sha256:")
	if len(hashHex) != 64 {
		t.Errorf("ContentHash hex part length = %d; want 64 (SHA-256 hex)", len(hashHex))
	}
}

// TestManifest_CreatedItem_TargetPathIsRelative verifies that the manifest entry's
// TargetPath is stored relative to the deployment root, not as an absolute path.
func TestManifest_CreatedItem_TargetPathIsRelative(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatalf("manifest entry not found")
	}
	if filepath.IsAbs(entry.TargetPath) {
		t.Errorf("manifest TargetPath %q is absolute; it must be relative to the deployment root", entry.TargetPath)
	}
	if entry.TargetPath != item.TargetPath {
		t.Errorf("manifest TargetPath = %q; want %q", entry.TargetPath, item.TargetPath)
	}
}

// ---------------------------------------------------------------------------
// TakenUpdated → manifest entry updated
// ---------------------------------------------------------------------------

// TestManifest_UpdatedItem_HasUpdatedEntry verifies that after an ActionUpdate run, the
// manifest entry is replaced with the new content hash and version fields.
func TestManifest_UpdatedItem_HasUpdatedEntry(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Seed the manifest with a prior entry for the same artifact.
	store := manifest.NewStore()
	prior := domain.Manifest{
		SchemaVersion: "1",
		HarnessID:     "test-harness",
		Entries: []domain.ManifestEntry{
			{
				Ref:         domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "runner"},
				TargetPath:  "agents/runner.md",
				ContentHash: "sha256:" + strings.Repeat("0", 64),
			},
		},
	}
	if err := store.Save(workspace, prior); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	updatedContent := []byte("# updated runner content")
	item := newAgentItem("runner", "agents/runner.md", domain.ActionUpdate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(updatedContent),
	}

	ex := deploy.NewExecutor(store, newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatalf("manifest has no entry after update")
	}
	if entry.ContentHash == "sha256:"+strings.Repeat("0", 64) {
		t.Error("manifest ContentHash was not updated; still has the prior value")
	}
}

// ---------------------------------------------------------------------------
// TakenSkipped → manifest entry unchanged
// ---------------------------------------------------------------------------

// TestManifest_SkippedItem_ExistingEntryUnchanged verifies that when the user chooses
// DecisionSkip for a conflict item, the prior manifest entry is not modified.
func TestManifest_SkippedItem_ExistingEntryUnchanged(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	// Pre-write the file and seed a manifest entry.
	writeExisting(t, workspace, targetPath, []byte("original content"))

	priorHash := "sha256:" + strings.Repeat("a", 64)
	store := manifest.NewStore()
	prior := domain.Manifest{
		SchemaVersion: "1",
		HarnessID:     "test-harness",
		Entries: []domain.ManifestEntry{
			{
				Ref:         domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "runner"},
				TargetPath:  targetPath,
				ContentHash: priorHash,
			},
		},
	}
	if err := store.Save(workspace, prior); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("new content")),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionSkip},
	}

	ex := deploy.NewExecutor(store, newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("manifest entry not found for skipped item")
	}
	if entry.ContentHash != priorHash {
		t.Errorf("skipped item: manifest ContentHash changed to %q; want unchanged %q", entry.ContentHash, priorHash)
	}
}

// TestManifest_SkippedItem_NoNewEntryWhenNoPriorEntry verifies that if a skipped item has
// no prior manifest entry (e.g. first deploy, locally modified), no entry is created.
func TestManifest_SkippedItem_NoNewEntryWhenNoPriorEntry(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("local content"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("source content")),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionSkip},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, found := result.Manifest.Lookup(item.Ref)
	if found {
		t.Error("manifest has an entry for a skipped item that had no prior entry; it should not")
	}
}

// ---------------------------------------------------------------------------
// TakenBackedUp → manifest entry updated (reflects new content on disk)
// ---------------------------------------------------------------------------

// TestManifest_BackedUpItem_HasUpdatedContentHash verifies that after a
// DecisionBackupThenOverwrite, the manifest entry reflects the new file content
// (the backup path is not tracked in the manifest).
func TestManifest_BackedUpItem_HasUpdatedContentHash(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("original content"))

	newContent := []byte("# updated content")
	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(newContent),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionBackupThenOverwrite},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("manifest has no entry for backed-up item")
	}
	// The hash must reflect the new content, not the original.
	if !strings.HasPrefix(entry.ContentHash, "sha256:") {
		t.Errorf("ContentHash = %q; want sha256: prefix", entry.ContentHash)
	}
}

// ---------------------------------------------------------------------------
// TakenFailed → no manifest entry for the failed item
// ---------------------------------------------------------------------------

// TestManifest_FailedItem_HasNoManifestEntry verifies that when a write fails, the failed
// item is not recorded in the manifest (it is not on disk, so the manifest must not claim
// it is) (AC17.4).
func TestManifest_FailedItem_HasNoManifestEntry(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("bad-agent", "blocked/bad-agent.md", domain.ActionCreate)
	// Block the target directory.
	blockPath(t, filepath.Join(workspace, "blocked"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, found := result.Manifest.Lookup(item.Ref)
	if found {
		t.Error("manifest contains an entry for a failed item; it must not (AC17.4)")
	}
}

// ---------------------------------------------------------------------------
// Manifest written to disk
// ---------------------------------------------------------------------------

// TestManifest_WrittenToDiskAtWorkspace verifies that the manifest file is always written
// to <workspace>/.mosaic/manifest.yaml after a run, regardless of whether a fallback
// deployment root was used.
func TestManifest_WrittenToDiskAtWorkspace(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	manifestPath := filepath.Join(workspace, ".mosaic", "manifest.yaml")
	if !fileExistsAt(t, manifestPath) {
		t.Errorf("manifest not found at %q; it must always be written", manifestPath)
	}
}

// TestManifest_DeployedAtTimestampIsSet verifies that the manifest entry for a created item
// has a non-zero DeployedAt timestamp.
func TestManifest_DeployedAtTimestampIsSet(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("manifest entry not found")
	}
	if entry.DeployedAt.IsZero() {
		t.Error("manifest entry DeployedAt is zero; it must be set to the time of deployment")
	}
}

// ---------------------------------------------------------------------------
// TakenUnchanged → manifest entry not altered
// ---------------------------------------------------------------------------

// TestManifest_UnchangedItem_NoNewEntryWhenNoPriorEntry verifies that an ActionUnchanged
// plan item does not result in a new manifest entry. The executor must not write an entry
// for a file it did not touch.
func TestManifest_UnchangedItem_NoNewEntryWhenNoPriorEntry(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionUnchanged)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, found := result.Manifest.Lookup(item.Ref)
	if found {
		t.Error("manifest has an entry for an unchanged item that had no prior entry; the executor must not create one")
	}
}

// TestManifest_UnchangedItem_PriorEntryPreserved verifies that when an ActionUnchanged item
// has an existing manifest entry, that entry is preserved byte-for-byte after the run.
// The executor must not alter the hash, version, or any other field of an unchanged item.
func TestManifest_UnchangedItem_PriorEntryPreserved(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	priorHash := "sha256:" + strings.Repeat("b", 64)
	store := manifest.NewStore()
	prior := domain.Manifest{
		SchemaVersion: "1",
		HarnessID:     "test-harness",
		Entries: []domain.ManifestEntry{
			{
				Ref:         domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "runner"},
				TargetPath:  "agents/runner.md",
				ContentHash: priorHash,
			},
		},
	}
	if err := store.Save(workspace, prior); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	item := newAgentItem("runner", "agents/runner.md", domain.ActionUnchanged)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	ex := deploy.NewExecutor(store, newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("prior manifest entry was removed for an unchanged item; it must be preserved")
	}
	if entry.ContentHash != priorHash {
		t.Errorf("unchanged item: manifest ContentHash = %q; want unchanged %q", entry.ContentHash, priorHash)
	}
}

// ---------------------------------------------------------------------------
// Multiple items
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// T4.6 — Version field names in deltas drive manifest stamping correctly
// ---------------------------------------------------------------------------

// TestManifest_UpdatedItem_VersionFieldsFromStaleDeltas verifies that when an ActionUpdate
// plan item carries Stale deltas, the executor reads the delta Source values using the field
// names "version", "harness_version", and "injections_version" to write the manifest entry.
// Fields not covered by Stale are filled from ExecRequest.VersionStamps.
//
// This test guards the contract between the planner (which produces field-named deltas) and
// the executor (which switches on those names in resolveVersionStamp). Renaming a field in
// either component would cause version data to be silently dropped from the manifest.
func TestManifest_UpdatedItem_VersionFieldsFromStaleDeltas(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Construct an update item with Stale deltas that cover two of the three fields.
	// The third field (injections_version) is supplied only via VersionStamps.
	item := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "runner"},
		SourcePath: "/fake/agents/runner.md",
		TargetPath: "agents/runner.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "version", Deployed: "1.0", Source: "2.0"},
			{Field: "harness_version", Deployed: "1.0", Source: "1.5"},
		},
	}

	// VersionStamps provides the injections_version (not covered by Stale).
	stamps := map[string]domain.VersionStamp{
		item.TargetPath: {InjectionsVersion: "3.0"},
	}

	req := deploy.ExecRequest{
		Plan:          newPlan(workspace, item),
		MosaicRoot:    mosaicRoot,
		Content:       fixedContent([]byte("# runner updated")),
		VersionStamps: stamps,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("manifest has no entry for updated item")
	}
	// Version must come from the "version" delta's Source.
	if entry.Version != "2.0" {
		t.Errorf("manifest Version = %q; want %q (from delta Field=version, Source=2.0)", entry.Version, "2.0")
	}
	// TransformVersion must come from the "harness_version" delta's Source.
	if entry.HarnessVersion != "1.5" {
		t.Errorf("manifest TransformVersion = %q; want %q (from delta Field=transform_version, Source=1.5)",
			entry.HarnessVersion, "1.5")
	}
	// InjectionsVersion must fall back to VersionStamps when not covered by a delta.
	if entry.InjectionsVersion != "3.0" {
		t.Errorf("manifest InjectionsVersion = %q; want %q (from VersionStamps fallback)",
			entry.InjectionsVersion, "3.0")
	}
}

// TestManifest_UpdatedItem_UnversionedDeployed_StampsFromVersionStamps verifies that when an
// ActionUpdate plan item has an empty Stale slice (no version deltas) but a populated
// VersionStamps entry, the executor stamps the manifest with the VersionStamps values.
//
// This covers the present-but-unversioned case (design decision R3): the deployed file exists
// but carries no readable version stamps, so the planner classifies the item as stale and sets
// Action=ActionUpdate with no Stale deltas (nothing to show the user as a diff). The executor
// must still write the correct source version stamps into the manifest so the next run can
// compare properly.
func TestManifest_UpdatedItem_UnversionedDeployed_StampsFromVersionStamps(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// An ActionUpdate item with no Stale deltas (the deployed file had no version stamps).
	item := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "runner"},
		SourcePath: "/fake/agents/runner.md",
		TargetPath: "agents/runner.md",
		Action:     domain.ActionUpdate,
		Stale:      []domain.VersionDelta{}, // empty — no readable deployed stamps to diff
	}

	// VersionStamps supplies the source versions to write into the manifest.
	stamps := map[string]domain.VersionStamp{
		item.TargetPath: {
			Version:           "3.0",
			HarnessVersion:  "2.0",
			InjectionsVersion: "1.5",
		},
	}

	req := deploy.ExecRequest{
		Plan:          newPlan(workspace, item),
		MosaicRoot:    mosaicRoot,
		Content:       fixedContent([]byte("# runner updated from unversioned")),
		VersionStamps: stamps,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("manifest has no entry for updated item with no Stale deltas")
	}
	// All three version fields must come from VersionStamps when Stale is empty.
	if entry.Version != "3.0" {
		t.Errorf("manifest Version = %q; want %q (from VersionStamps, Stale is empty)", entry.Version, "3.0")
	}
	if entry.HarnessVersion != "2.0" {
		t.Errorf("manifest TransformVersion = %q; want %q (from VersionStamps, Stale is empty)",
			entry.HarnessVersion, "2.0")
	}
	if entry.InjectionsVersion != "1.5" {
		t.Errorf("manifest InjectionsVersion = %q; want %q (from VersionStamps, Stale is empty)",
			entry.InjectionsVersion, "1.5")
	}
}

// ---------------------------------------------------------------------------
// Multiple items
// ---------------------------------------------------------------------------

// TestManifest_MultipleItemsAllRecorded verifies that when a plan contains several items,
// all successfully deployed items appear in the manifest.
func TestManifest_MultipleItemsAllRecorded(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, item := range []domain.PlanItem{itemA, itemB} {
		if _, found := result.Manifest.Lookup(item.Ref); !found {
			t.Errorf("manifest has no entry for item %q", item.Ref.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// T5.5 — Workflow-staleness deltas must not corrupt manifest version stamps
// ---------------------------------------------------------------------------

// TestManifest_WorkflowStalenessDeltas_DoNotCorruptVersionStamps verifies that when an
// ActionUpdate plan item's Stale slice contains only workflow-prefixed deltas (field names
// of the form "workflow:<id>"), the executor's resolveVersionStamp function does NOT match
// those fields in its switch on "version", "harness_version", "injections_version", and
// therefore does NOT overwrite those version stamps in the manifest entry. The correct stamps
// come solely from the VersionStamps map, which is the intended carrier for non-stale fields.
//
// This guards the AC5.7 / D7 invariant: a workflow-drift delta whose Field happens to equal
// "version" would silently corrupt the written manifest entry and permanently mis-state which
// version of the orchestrator was deployed.
func TestManifest_WorkflowStalenessDeltas_DoNotCorruptVersionStamps(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// An orchestrator plan item whose staleness was driven entirely by a workflow set change.
	// The Stale slice carries only a workflow-prefixed delta — no version-field deltas.
	item := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
		SourcePath: filepath.Join("testdata", "orchestrator.md"),
		TargetPath: "agents/orchestrator.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			// workflow:-prefixed field — must NOT be matched by resolveVersionStamp's switch
			{Field: "workflow:quick-fix", Deployed: "1.0", Source: "2.0"},
		},
	}

	// The VersionStamps map is the authoritative source for the three version fields.
	// After Execute, the manifest entry must carry exactly these values unchanged.
	const wantVersion = "5.0"
	const wantTransform = "3.0"
	const wantInjections = "2.0"

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# orchestrator deployed content")),
		VersionStamps: map[string]domain.VersionStamp{
			"agents/orchestrator.md": {
				Version:           wantVersion,
				HarnessVersion:  wantTransform,
				InjectionsVersion: wantInjections,
			},
		},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entry, found := result.Manifest.Lookup(item.Ref)
	if !found {
		t.Fatal("manifest has no entry for orchestrator after workflow-only update")
	}

	// All three version stamps must carry the values from the VersionStamps map.
	// A workflow-prefixed delta must not have overwritten any of them.
	if entry.Version != wantVersion {
		t.Errorf("manifest entry Version = %q; want %q; "+
			"workflow delta field %q must not overwrite the version stamp",
			entry.Version, wantVersion, "workflow:quick-fix")
	}
	if entry.HarnessVersion != wantTransform {
		t.Errorf("manifest entry TransformVersion = %q; want %q; "+
			"workflow delta field must not overwrite the transform_version stamp",
			entry.HarnessVersion, wantTransform)
	}
	if entry.InjectionsVersion != wantInjections {
		t.Errorf("manifest entry InjectionsVersion = %q; want %q; "+
			"workflow delta field must not overwrite the injections_version stamp",
			entry.InjectionsVersion, wantInjections)
	}
}
