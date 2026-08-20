package deploy_test

// multifile_manifest_test.go covers Stage 5 requirements for multi-file skill support
// in the executor's manifest tracking:
//
//   T5.1 — buildEntryMap composite key round-trip: a manifest with N files for one skill
//   (same ArtifactRef, distinct TargetPath values) must retain all N entries after a
//   buildEntryMap -> buildManifest round-trip. The current implementation keys buildEntryMap
//   by ArtifactRef alone, which silently drops all but the last file per multi-file skill.
//
//   T5.2 — buildManifest sorting: the resulting Manifest.Entries are sorted by TargetPath
//   regardless of plan-item insertion order, including multiple entries for the same ArtifactRef.
//
//   T5.4 — Manifest round-trip for multi-file skills: when all files in a multi-file skill
//   are ActionUnchanged (content identical to last deployment), the result manifest retains
//   every prior entry — the buildEntryMap -> buildManifest round-trip must not collapse
//   multiple entries for one ArtifactRef into one.
//
// All tests exercise the executor's public Execute API. The composite key behavior is visible
// through Execute because buildEntryMap and buildManifest are private to executor.go.
//
// These tests are written in the TDD RED phase; they fail until I5.1 (composite key fix)
// changes buildEntryMap to key by composite (ArtifactRef, normalised TargetPath).

import (
	"context"
	"strings"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// countManifestEntriesForRef counts entries in m that match the given ArtifactRef.
// Used throughout this file to assert that all N files for a multi-file skill have
// their own manifest entry (not collapsed to one by a single-key bug).
// ---------------------------------------------------------------------------

func countManifestEntriesForRef(m domain.Manifest, ref domain.ArtifactRef) int {
	count := 0
	for _, e := range m.Entries {
		if e.Ref == ref {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// T5.1 — All N entries retained after execution of a multi-file skill
// ---------------------------------------------------------------------------

// TestExecute_MultiFileSkill_AllEntriesRetainedInManifest verifies that when a plan contains
// two ActionCreate items for the same skill (same ArtifactRef, distinct TargetPaths), the
// resulting manifest contains two entries — one per file. This exposes the composite-key bug:
// the current buildEntryMap is keyed by ArtifactRef alone, so the second item's entry
// silently overwrites the first, producing only one manifest entry instead of two.
func TestExecute_MultiFileSkill_AllEntriesRetainedInManifest(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}

	entryFile    := newSkillItem("lean-tdd", "skills/lean-tdd/SKILL.md",      domain.ActionCreate)
	companionFile := newSkillItem("lean-tdd", "skills/lean-tdd/companion.md", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, entryFile, companionFile),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# skill content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := countManifestEntriesForRef(result.Manifest, ref)
	if got != 2 {
		t.Errorf("manifest has %d entry(entries) for multi-file skill %q; want 2\n"+
			"  buildEntryMap must use composite key (ArtifactRef, TargetPath) so each file\n"+
			"  in a multi-file skill retains its own manifest entry",
			got, ref.Key)
	}

	// Verify each file is individually findable by LookupAt.
	if _, ok := result.Manifest.LookupAt(ref, "skills/lean-tdd/SKILL.md"); !ok {
		t.Errorf("manifest has no LookupAt entry for %q; the composite-key bug overwrote it",
			"skills/lean-tdd/SKILL.md")
	}
	if _, ok := result.Manifest.LookupAt(ref, "skills/lean-tdd/companion.md"); !ok {
		t.Errorf("manifest has no LookupAt entry for %q; the composite-key bug overwrote it",
			"skills/lean-tdd/companion.md")
	}
}

// TestExecute_MultiFileSkill_ThreeFiles_AllEntriesRetained verifies the general case
// for N > 2: a skill with three files produces three manifest entries. This guards
// against a fix that handles exactly two files but fails for larger counts.
func TestExecute_MultiFileSkill_ThreeFiles_AllEntriesRetained(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}

	items := []domain.PlanItem{
		newSkillItem("lean-tdd", "skills/lean-tdd/SKILL.md",             domain.ActionCreate),
		newSkillItem("lean-tdd", "skills/lean-tdd/examples/usage.md",    domain.ActionCreate),
		newSkillItem("lean-tdd", "skills/lean-tdd/templates/default.md", domain.ActionCreate),
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, items...),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# skill content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := countManifestEntriesForRef(result.Manifest, ref)
	if got != 3 {
		t.Errorf("manifest has %d entry(entries) for 3-file skill %q; want 3",
			got, ref.Key)
	}
}

// ---------------------------------------------------------------------------
// T5.1 — Updating one file does not overwrite another file's prior manifest entry
// ---------------------------------------------------------------------------

// TestExecute_MultiFileSkill_UpdateOneFileDoesNotDropOther verifies that when a
// multi-file skill's entry file is ActionUpdate while the companion file is
// ActionUnchanged, both entries remain in the result manifest.
//
// This tests the prior-manifest side of the composite key bug: buildEntryMap is called on
// the prior manifest which has two entries for the same ArtifactRef. The current
// implementation collapses them to one (the last entry read), so even before the item
// loop runs, the prior companion entry is already lost.
func TestExecute_MultiFileSkill_UpdateOneFileDoesNotDropOther(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}

	// Seed the manifest with two prior entries for the same skill.
	store := manifest.NewStore()
	prior := domain.Manifest{
		SchemaVersion: "1",
		HarnessID:     "test-harness",
		Entries: []domain.ManifestEntry{
			{
				Ref:         ref,
				TargetPath:  "skills/lean-tdd/SKILL.md",
				Version:     "1.0",
				ContentHash: "sha256:" + strings.Repeat("a", 64),
			},
			{
				Ref:         ref,
				TargetPath:  "skills/lean-tdd/companion.md",
				ContentHash: "sha256:" + strings.Repeat("b", 64),
			},
		},
	}
	if err := store.Save(workspace, prior); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	// Update the entry file; companion.md is unchanged (no write needed).
	entryFileUpdate    := newSkillItem("lean-tdd", "skills/lean-tdd/SKILL.md",     domain.ActionUpdate)
	companionUnchanged := newSkillItem("lean-tdd", "skills/lean-tdd/companion.md", domain.ActionUnchanged)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, entryFileUpdate, companionUnchanged),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# updated skill content")),
	}

	ex := deploy.NewExecutor(store, newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := countManifestEntriesForRef(result.Manifest, ref)
	if got != 2 {
		t.Errorf("after update+unchanged run, manifest has %d entry(entries) for skill %q; want 2\n"+
			"  buildEntryMap must use composite key so prior entries for all skill files are preserved",
			got, ref.Key)
	}
}

// ---------------------------------------------------------------------------
// T5.2 — buildManifest sorting: entries sorted by TargetPath
// ---------------------------------------------------------------------------

// TestExecute_MultiFileSkill_ManifestEntriesSortedByTargetPath verifies that the
// manifest entries for a multi-file skill are sorted by TargetPath (ascending),
// regardless of the order the plan items appear in the plan.
//
// The count assertion (want 3) must hold first; sorting is only observable when
// all three entries are present in the result manifest.
func TestExecute_MultiFileSkill_ManifestEntriesSortedByTargetPath(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}

	// Plan items are deliberately in reverse-alphabetical order; result must be sorted.
	items := []domain.PlanItem{
		newSkillItem("lean-tdd", "skills/lean-tdd/z-last.md",  domain.ActionCreate),
		newSkillItem("lean-tdd", "skills/lean-tdd/SKILL.md",   domain.ActionCreate),
		newSkillItem("lean-tdd", "skills/lean-tdd/a-first.md", domain.ActionCreate),
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, items...),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := countManifestEntriesForRef(result.Manifest, ref)
	if got != 3 {
		t.Fatalf("manifest has %d entry(entries) for 3-file skill; want 3 before testing sort order",
			got)
	}

	// Collect entries for this skill and verify ascending TargetPath order.
	var skillEntries []domain.ManifestEntry
	for _, e := range result.Manifest.Entries {
		if e.Ref == ref {
			skillEntries = append(skillEntries, e)
		}
	}
	for i := 1; i < len(skillEntries); i++ {
		if skillEntries[i].TargetPath < skillEntries[i-1].TargetPath {
			t.Errorf("manifest entries not sorted: entry[%d].TargetPath %q < entry[%d].TargetPath %q",
				i, skillEntries[i].TargetPath, i-1, skillEntries[i-1].TargetPath)
		}
	}
}

// ---------------------------------------------------------------------------
// T5.4 — Manifest round-trip: ActionUnchanged preserves all prior entries
// ---------------------------------------------------------------------------

// TestExecute_MultiFileSkill_UnchangedRoundTrip_AllEntriesPreserved verifies that when
// all files in a multi-file skill are ActionUnchanged (content is identical to last
// deployment), the result manifest retains every prior entry for the skill.
//
// This is the end-to-end test that catches the composite-key bug: with the current
// implementation, buildEntryMap(prior) on a manifest with N entries for the same
// ArtifactRef retains only 1 (the last entry read from the prior manifest), so the result
// manifest has 1 entry instead of N even though no files were written.
func TestExecute_MultiFileSkill_UnchangedRoundTrip_AllEntriesPreserved(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}

	// Seed the manifest with two prior entries (simulating a successful prior deployment).
	store := manifest.NewStore()
	priorHash1 := "sha256:" + strings.Repeat("a", 64)
	priorHash2 := "sha256:" + strings.Repeat("b", 64)
	prior := domain.Manifest{
		SchemaVersion: "1",
		HarnessID:     "test-harness",
		Entries: []domain.ManifestEntry{
			{Ref: ref, TargetPath: "skills/lean-tdd/SKILL.md",     ContentHash: priorHash1},
			{Ref: ref, TargetPath: "skills/lean-tdd/companion.md", ContentHash: priorHash2},
		},
	}
	if err := store.Save(workspace, prior); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	// Both files are unchanged on this run; no writes occur.
	items := []domain.PlanItem{
		newSkillItem("lean-tdd", "skills/lean-tdd/SKILL.md",     domain.ActionUnchanged),
		newSkillItem("lean-tdd", "skills/lean-tdd/companion.md", domain.ActionUnchanged),
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, items...),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	ex := deploy.NewExecutor(store, newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := countManifestEntriesForRef(result.Manifest, ref)
	if got != 2 {
		t.Errorf("manifest round-trip with ActionUnchanged: got %d entry(entries) for skill %q; want 2\n"+
			"  buildEntryMap must use composite key so the prior manifest's N entries for a\n"+
			"  multi-file skill are all carried through the round-trip, not collapsed to 1",
			got, ref.Key)
	}

	// Verify the specific prior entries are preserved with their original content hashes.
	e1, ok1 := result.Manifest.LookupAt(ref, "skills/lean-tdd/SKILL.md")
	if !ok1 {
		t.Errorf("round-trip lost the entry for %q", "skills/lean-tdd/SKILL.md")
	} else if e1.ContentHash != priorHash1 {
		t.Errorf("SKILL.md entry ContentHash = %q; want preserved prior hash %q",
			e1.ContentHash, priorHash1)
	}

	e2, ok2 := result.Manifest.LookupAt(ref, "skills/lean-tdd/companion.md")
	if !ok2 {
		t.Errorf("round-trip lost the entry for %q", "skills/lean-tdd/companion.md")
	} else if e2.ContentHash != priorHash2 {
		t.Errorf("companion.md entry ContentHash = %q; want preserved prior hash %q",
			e2.ContentHash, priorHash2)
	}
}

// ---------------------------------------------------------------------------
// T5.4 — Per-file content hashes are independent (AC5.4)
// ---------------------------------------------------------------------------

// TestExecute_MultiFileSkill_PerFileContentHashes_AreIndependent verifies that when two
// files in a multi-file skill are deployed with different content, their manifest entries
// carry distinct content hashes. The content hash must be computed per-file, not as a
// single hash for the entire skill.
func TestExecute_MultiFileSkill_PerFileContentHashes_AreIndependent(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}

	entryContent    := []byte("---\nversion: 1.0\n---\n# SKILL.md content")
	companionContent := []byte("# companion reference content, no frontmatter")

	contentByPath := map[string][]byte{
		"skills/lean-tdd/SKILL.md":     entryContent,
		"skills/lean-tdd/companion.md": companionContent,
	}
	contentFn := func(item domain.PlanItem) ([]byte, error) {
		if data, ok := contentByPath[item.TargetPath]; ok {
			return data, nil
		}
		return []byte("default content"), nil
	}

	items := []domain.PlanItem{
		newSkillItem("lean-tdd", "skills/lean-tdd/SKILL.md",     domain.ActionCreate),
		newSkillItem("lean-tdd", "skills/lean-tdd/companion.md", domain.ActionCreate),
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, items...),
		MosaicRoot: mosaicRoot,
		Content:    contentFn,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := countManifestEntriesForRef(result.Manifest, ref)
	if got != 2 {
		t.Fatalf("manifest has %d entry(entries); want 2 before testing per-file hashes", got)
	}

	e1, ok1 := result.Manifest.LookupAt(ref, "skills/lean-tdd/SKILL.md")
	e2, ok2 := result.Manifest.LookupAt(ref, "skills/lean-tdd/companion.md")
	if !ok1 || !ok2 {
		t.Fatal("could not find both manifest entries by LookupAt")
	}

	if e1.ContentHash == e2.ContentHash {
		t.Errorf("both files carry the same ContentHash %q; per-file hashes must be independent "+
			"(different file content must produce different hashes)", e1.ContentHash)
	}
	if !strings.HasPrefix(e1.ContentHash, "sha256:") {
		t.Errorf("SKILL.md ContentHash = %q; want sha256: prefix", e1.ContentHash)
	}
	if !strings.HasPrefix(e2.ContentHash, "sha256:") {
		t.Errorf("companion.md ContentHash = %q; want sha256: prefix", e2.ContentHash)
	}
}
