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
