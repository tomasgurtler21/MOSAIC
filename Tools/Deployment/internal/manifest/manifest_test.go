package manifest_test

// Tests for the manifest Store: write-then-read round-tripping, schema versioning,
// and handling of abnormal manifest states (absent, empty, corrupt, future-schema).
//
// All tests use t.TempDir() for isolation. The store under test is obtained from NewStore().
//
// Round-trip correctness:
//   - A manifest written by Save and read back by Load is byte-identical in all fields,
//     including ContentHash strings, version stamps, and ArtifactRef values.
//   - The returned Snapshot.State is StatePresent after a successful round-trip.
//
// Schema versioning:
//   - A manifest file with a schema_version this build does not recognise (future-schema)
//     produces Snapshot.State == StateFuture, never a silent empty Manifest.
//
// Abnormal states:
//   - A missing manifest file produces StateAbsent.
//   - An empty manifest file produces StateEmpty.
//   - A corrupt manifest file produces StateCorrupt.
//   - Each of the five states is a distinct string value: no two states collapse.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// makeWorkspace creates a temporary directory suitable for use as a workspace root.
// The .mosaic sub-directory is NOT created here; Save is responsible for creating it.
func makeWorkspace(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// writeManifestFile places content at <workspace>/.mosaic/manifest.yaml, creating
// the .mosaic directory first. Used to set up abnormal-state fixtures without going
// through Save, which may refuse to write invalid content.
func writeManifestFile(t *testing.T, workspace string, content []byte) {
	t.Helper()
	dir := filepath.Join(workspace, manifest.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeManifestFile: mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, manifest.FileName)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writeManifestFile: write %s: %v", path, err)
	}
}

// sampleEntry returns a ManifestEntry with all fields populated. DeployedAt is
// truncated to second precision so it survives YAML timestamp serialisation unchanged.
func sampleEntry(targetPath string, ref domain.ArtifactRef) domain.ManifestEntry {
	return domain.ManifestEntry{
		Ref:               ref,
		TargetPath:        targetPath,
		Version:           "1.2.3",
		HarnessVersion:    "2.0.0",
		InjectionsVersion: "3.1.0",
		ContentHash:       "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab",
		DeployedAt:        time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
	}
}

// sampleManifest returns a Manifest with two entries covering the agent and skill kinds.
// UpdatedAt is truncated to second precision for the same reason as sampleEntry.
func sampleManifest() domain.Manifest {
	return domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "claude-code",
		UpdatedAt:     time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC),
		Entries: []domain.ManifestEntry{
			sampleEntry(
				"Catalog/Subagents/Creation/test-writer.md",
				domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-writer"},
			),
			sampleEntry(
				"skills/lean-tdd/SKILL.md",
				domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"},
			),
		},
	}
}

// must fatals the test if err is non-nil.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: Snapshot state
// ---------------------------------------------------------------------------

// TestStore_WriteRead_ReturnsStatePresent verifies that a manifest written with Save
// and then read back with Load returns a Snapshot with State == StatePresent.
func TestStore_WriteRead_ReturnsStatePresent(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	must(t, store.Save(ws, sampleManifest()))

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State != manifest.StatePresent {
		t.Errorf("Load after Save: State = %q, want %q", snap.State, manifest.StatePresent)
	}
}

// TestStore_WriteRead_SnapshotErrIsNilOnSuccess verifies that a successfully loaded
// manifest produces a nil Snapshot.Err.
func TestStore_WriteRead_SnapshotErrIsNilOnSuccess(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	must(t, store.Save(ws, sampleManifest()))

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State == manifest.StatePresent && snap.Err != nil {
		t.Errorf("StatePresent snapshot has non-nil Err: %v", snap.Err)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: individual fields
// ---------------------------------------------------------------------------

// TestStore_WriteRead_SchemaVersionPreserved verifies that the SchemaVersion field
// survives a write-then-read cycle unchanged.
func TestStore_WriteRead_SchemaVersionPreserved(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	m := sampleManifest()

	must(t, store.Save(ws, m))
	snap, err := store.Load(ws)
	must(t, err)

	if snap.Manifest.SchemaVersion != m.SchemaVersion {
		t.Errorf("SchemaVersion: got %q, want %q", snap.Manifest.SchemaVersion, m.SchemaVersion)
	}
}

// TestStore_WriteRead_HarnessIDPreserved verifies that the HarnessID field survives
// a write-then-read cycle unchanged.
func TestStore_WriteRead_HarnessIDPreserved(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	m := sampleManifest()

	must(t, store.Save(ws, m))
	snap, err := store.Load(ws)
	must(t, err)

	if snap.Manifest.HarnessID != m.HarnessID {
		t.Errorf("HarnessID: got %q, want %q", snap.Manifest.HarnessID, m.HarnessID)
	}
}

// TestStore_WriteRead_UpdatedAtPreserved verifies that the UpdatedAt timestamp survives
// a write-then-read cycle at second-level precision.
func TestStore_WriteRead_UpdatedAtPreserved(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	m := sampleManifest()

	must(t, store.Save(ws, m))
	snap, err := store.Load(ws)
	must(t, err)

	got := snap.Manifest.UpdatedAt.UTC().Truncate(time.Second)
	want := m.UpdatedAt.UTC().Truncate(time.Second)
	if !got.Equal(want) {
		t.Errorf("UpdatedAt: got %v, want %v", got, want)
	}
}

// TestStore_WriteRead_EntryCountPreserved verifies that the number of entries in the
// saved manifest matches the number in the loaded manifest exactly.
func TestStore_WriteRead_EntryCountPreserved(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	m := sampleManifest()

	must(t, store.Save(ws, m))
	snap, err := store.Load(ws)
	must(t, err)

	if len(snap.Manifest.Entries) != len(m.Entries) {
		t.Errorf("Entries count: got %d, want %d", len(snap.Manifest.Entries), len(m.Entries))
	}
}

// TestStore_WriteRead_EntryFieldsPreserved verifies that all fields of every ManifestEntry
// survive a write-then-read cycle: Ref (Kind+Key), TargetPath, Version, HarnessVersion,
// InjectionsVersion, ContentHash, and DeployedAt.
func TestStore_WriteRead_EntryFieldsPreserved(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	m := sampleManifest()

	must(t, store.Save(ws, m))
	snap, err := store.Load(ws)
	must(t, err)

	if len(snap.Manifest.Entries) != len(m.Entries) {
		t.Fatalf("entry count mismatch: got %d, want %d — cannot compare individual entries",
			len(snap.Manifest.Entries), len(m.Entries))
	}

	for i, want := range m.Entries {
		got := snap.Manifest.Entries[i]

		if got.Ref != want.Ref {
			t.Errorf("Entries[%d].Ref: got %v, want %v", i, got.Ref, want.Ref)
		}
		if got.TargetPath != want.TargetPath {
			t.Errorf("Entries[%d].TargetPath: got %q, want %q", i, got.TargetPath, want.TargetPath)
		}
		if got.Version != want.Version {
			t.Errorf("Entries[%d].Version: got %q, want %q", i, got.Version, want.Version)
		}
		if got.HarnessVersion != want.HarnessVersion {
			t.Errorf("Entries[%d].HarnessVersion: got %q, want %q", i, got.HarnessVersion, want.HarnessVersion)
		}
		if got.InjectionsVersion != want.InjectionsVersion {
			t.Errorf("Entries[%d].InjectionsVersion: got %q, want %q", i, got.InjectionsVersion, want.InjectionsVersion)
		}
		if got.ContentHash != want.ContentHash {
			t.Errorf("Entries[%d].ContentHash: got %q, want %q", i, got.ContentHash, want.ContentHash)
		}
		gotAt := got.DeployedAt.UTC().Truncate(time.Second)
		wantAt := want.DeployedAt.UTC().Truncate(time.Second)
		if !gotAt.Equal(wantAt) {
			t.Errorf("Entries[%d].DeployedAt: got %v, want %v", i, gotAt, wantAt)
		}
	}
}

// TestStore_WriteRead_ContentHashIsExactString verifies that ContentHash is stored and
// returned as a literal string with no reformatting or case conversion. The hash value
// "sha256:<hex>" must survive the round-trip byte-identical.
func TestStore_WriteRead_ContentHashIsExactString(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	m := sampleManifest()
	wantHash := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab"
	m.Entries[0].ContentHash = wantHash

	must(t, store.Save(ws, m))
	snap, err := store.Load(ws)
	must(t, err)

	if len(snap.Manifest.Entries) == 0 {
		t.Fatal("no entries in loaded manifest")
	}
	if snap.Manifest.Entries[0].ContentHash != wantHash {
		t.Errorf("ContentHash: got %q, want %q", snap.Manifest.Entries[0].ContentHash, wantHash)
	}
}

// TestStore_WriteRead_ArtifactKindPreserved verifies that the ArtifactKind field of
// an entry's Ref survives the round-trip exactly, covering both agent and skill kinds.
func TestStore_WriteRead_ArtifactKindPreserved(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	m := sampleManifest()
	// Entries[0] is an agent, Entries[1] is a skill.
	must(t, store.Save(ws, m))
	snap, err := store.Load(ws)
	must(t, err)

	if len(snap.Manifest.Entries) < 2 {
		t.Fatalf("need at least 2 entries, got %d", len(snap.Manifest.Entries))
	}
	if snap.Manifest.Entries[0].Ref.Kind != domain.ArtifactAgent {
		t.Errorf("Entries[0].Ref.Kind: got %q, want %q", snap.Manifest.Entries[0].Ref.Kind, domain.ArtifactAgent)
	}
	if snap.Manifest.Entries[1].Ref.Kind != domain.ArtifactSkill {
		t.Errorf("Entries[1].Ref.Kind: got %q, want %q", snap.Manifest.Entries[1].Ref.Kind, domain.ArtifactSkill)
	}
}

// TestStore_WriteRead_HookKindPreserved verifies that an ArtifactHook entry's Kind
// survives a round-trip. This completes exhaustive coverage of all three defined
// ArtifactKind values: ArtifactAgent, ArtifactSkill, and ArtifactHook.
func TestStore_WriteRead_HookKindPreserved(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	m := domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "claude-code",
		UpdatedAt:     time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC),
		Entries: []domain.ManifestEntry{
			sampleEntry(
				"hooks/pre-deploy.sh",
				domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "pre-deploy"},
			),
		},
	}

	must(t, store.Save(ws, m))
	snap, err := store.Load(ws)
	must(t, err)

	if len(snap.Manifest.Entries) == 0 {
		t.Fatal("no entries in loaded manifest")
	}
	if snap.Manifest.Entries[0].Ref.Kind != domain.ArtifactHook {
		t.Errorf("hook entry Ref.Kind: got %q, want %q",
			snap.Manifest.Entries[0].Ref.Kind, domain.ArtifactHook)
	}
}

// ---------------------------------------------------------------------------
// Sort order: entries must be sorted by TargetPath on load
// ---------------------------------------------------------------------------

// TestStore_WriteRead_EntriesSortedByTargetPath verifies that Load returns entries in
// ascending TargetPath order regardless of the order in which they were saved. This
// enforces the "sorted by TargetPath" invariant stated in domain.Manifest.Entries. An
// implementation that returns entries in insertion order rather than sorted order will
// fail this test.
func TestStore_WriteRead_EntriesSortedByTargetPath(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	// Supply entries in deliberately reversed alphabetical order so that an implementation
	// returning insertion order would produce the wrong result.
	// "skills/..." sorts after "Agents/..." in lexicographic order ('A' < 's').
	m := domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "claude-code",
		UpdatedAt:     time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC),
		Entries: []domain.ManifestEntry{
			sampleEntry(
				"skills/lean-tdd/SKILL.md", // lexicographically later — saved first
				domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"},
			),
			sampleEntry(
				"Catalog/Subagents/Creation/test-writer.md", // lexicographically earlier — saved second
				domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-writer"},
			),
		},
	}

	must(t, store.Save(ws, m))
	snap, err := store.Load(ws)
	must(t, err)

	if len(snap.Manifest.Entries) != 2 {
		t.Fatalf("entry count: got %d, want 2", len(snap.Manifest.Entries))
	}

	wantFirst := "Catalog/Subagents/Creation/test-writer.md"
	wantSecond := "skills/lean-tdd/SKILL.md"

	if snap.Manifest.Entries[0].TargetPath != wantFirst {
		t.Errorf("Entries[0].TargetPath = %q, want %q; "+
			"entries must be sorted ascending by TargetPath after Load",
			snap.Manifest.Entries[0].TargetPath, wantFirst)
	}
	if snap.Manifest.Entries[1].TargetPath != wantSecond {
		t.Errorf("Entries[1].TargetPath = %q, want %q; "+
			"entries must be sorted ascending by TargetPath after Load",
			snap.Manifest.Entries[1].TargetPath, wantSecond)
	}
}

// TestStore_WriteRead_EmptyEntries verifies that a manifest with no entries round-trips
// correctly: no entries are fabricated during save or load.
func TestStore_WriteRead_EmptyEntries(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	m := domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "claude-code",
		UpdatedAt:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Entries:       nil,
	}

	must(t, store.Save(ws, m))
	snap, err := store.Load(ws)
	must(t, err)

	if snap.State != manifest.StatePresent {
		t.Errorf("empty entries: State = %q, want %q", snap.State, manifest.StatePresent)
	}
	if len(snap.Manifest.Entries) != 0 {
		t.Errorf("empty entries: got %d entries after round-trip, want 0", len(snap.Manifest.Entries))
	}
}

// TestStore_WriteRead_SnapshotPathIsPopulated verifies that Snapshot.Path is a non-empty
// string after a successful load.
func TestStore_WriteRead_SnapshotPathIsPopulated(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	must(t, store.Save(ws, sampleManifest()))
	snap, err := store.Load(ws)
	must(t, err)

	if snap.Path == "" {
		t.Error("Snapshot.Path is empty after successful load; expected absolute path to manifest.yaml")
	}
}

// ---------------------------------------------------------------------------
// File system effects of Save
// ---------------------------------------------------------------------------

// TestStore_Save_CreatesDotMosaicDirectory verifies that Save creates the .mosaic
// directory when it does not already exist.
func TestStore_Save_CreatesDotMosaicDirectory(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t) // no .mosaic dir yet

	must(t, store.Save(ws, sampleManifest()))

	dir := filepath.Join(ws, manifest.Dir)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("Save did not create .mosaic directory at %s", dir)
	}
}

// TestStore_Save_WritesManifestAtCanonicalPath verifies that Save writes the manifest
// file at <workspace>/.mosaic/manifest.yaml (the canonical fixed location).
func TestStore_Save_WritesManifestAtCanonicalPath(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	must(t, store.Save(ws, sampleManifest()))

	path := filepath.Join(ws, manifest.Dir, manifest.FileName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("Save did not write manifest file at canonical path %s", path)
	}
}

// TestStore_WriteRead_SecondSaveOverwritesFirst verifies that calling Save a second time
// replaces the manifest file entirely rather than appending to it. An implementation
// that appends to the file would produce corrupt YAML or a doubled entry set on Load.
func TestStore_WriteRead_SecondSaveOverwritesFirst(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	first := domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "claude-code",
		UpdatedAt:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Entries: []domain.ManifestEntry{
			sampleEntry("first/path.md", domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "first-agent"}),
		},
	}
	second := domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "claude-code",
		UpdatedAt:     time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		Entries: []domain.ManifestEntry{
			sampleEntry("second/path.md", domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "second-agent"}),
		},
	}

	must(t, store.Save(ws, first))
	must(t, store.Save(ws, second))

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State != manifest.StatePresent {
		t.Errorf("after two saves: State = %q, want %q", snap.State, manifest.StatePresent)
	}
	if len(snap.Manifest.Entries) != 1 {
		t.Errorf("after second save: got %d entries, want 1; second Save must replace first, not append",
			len(snap.Manifest.Entries))
	}
	if len(snap.Manifest.Entries) == 1 && snap.Manifest.Entries[0].TargetPath != "second/path.md" {
		t.Errorf("after second save: TargetPath = %q, want %q; second Save must overwrite first",
			snap.Manifest.Entries[0].TargetPath, "second/path.md")
	}
}

// ---------------------------------------------------------------------------
// Schema versioning: future-schema manifests
// ---------------------------------------------------------------------------

// TestStore_FutureSchema_ReturnsStateFuture verifies that a manifest whose schema_version
// is not recognised by this build produces State == StateFuture. The state is reported via
// Snapshot, not via the Load error return.
func TestStore_FutureSchema_ReturnsStateFuture(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte("schema_version: \"99\"\nharness_id: \"claude-code\"\nupdated_at: \"2099-01-01T00:00:00Z\"\nentries: []\n"))

	snap, err := store.Load(ws)
	must(t, err) // Load itself must not return an error

	if snap.State != manifest.StateFuture {
		t.Errorf("future-schema manifest: State = %q, want %q", snap.State, manifest.StateFuture)
	}
}

// TestStore_FutureSchema_LoadDoesNotReturnError verifies that an unrecognised schema
// version is reported via Snapshot.State, not via the Load error return.
func TestStore_FutureSchema_LoadDoesNotReturnError(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte("schema_version: \"99\"\nharness_id: \"claude-code\"\nupdated_at: \"2099-01-01T00:00:00Z\"\nentries: []\n"))

	_, err := store.Load(ws)
	if err != nil {
		t.Errorf("future-schema manifest: Load returned error %v; the state must be reported via Snapshot.State", err)
	}
}

// TestStore_FutureSchema_ErrIsNonNil verifies that a future-schema Snapshot carries a
// non-nil Err explaining the schema version mismatch, so callers can log the reason.
func TestStore_FutureSchema_ErrIsNonNil(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte("schema_version: \"99\"\nharness_id: \"claude-code\"\nupdated_at: \"2099-01-01T00:00:00Z\"\nentries: []\n"))

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State == manifest.StateFuture && snap.Err == nil {
		t.Error("StateFuture snapshot has nil Err; expected a non-nil error explaining the schema version mismatch")
	}
}

// TestStore_FutureSchema_ManifestIsZeroValue verifies that a future-schema Snapshot
// carries a zero-value domain.Manifest. Callers must not read partially-parsed data
// when the schema is unrecognised.
func TestStore_FutureSchema_ManifestIsZeroValue(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte("schema_version: \"99\"\nharness_id: \"claude-code\"\nupdated_at: \"2099-01-01T00:00:00Z\"\nentries: []\n"))

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State == manifest.StateFuture {
		if snap.Manifest.SchemaVersion != "" || snap.Manifest.HarnessID != "" || len(snap.Manifest.Entries) != 0 {
			t.Error("StateFuture snapshot has non-zero Manifest; expected zero-value domain.Manifest " +
				"because the schema cannot be interpreted")
		}
	}
}

// TestStore_FutureSchema_PathIsPopulated verifies that even a future-schema Snapshot
// reports the path of the file, so callers can include it in diagnostics.
func TestStore_FutureSchema_PathIsPopulated(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte("schema_version: \"99\"\nharness_id: \"claude-code\"\nupdated_at: \"2099-01-01T00:00:00Z\"\nentries: []\n"))

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State == manifest.StateFuture && snap.Path == "" {
		t.Error("StateFuture snapshot has empty Path; expected the path to the future-schema manifest file")
	}
}

// ---------------------------------------------------------------------------
// Absent manifest
// ---------------------------------------------------------------------------

// TestStore_AbsentManifest_ReturnsStateAbsent verifies that loading from a workspace
// that has no .mosaic/manifest.yaml produces State == StateAbsent.
func TestStore_AbsentManifest_ReturnsStateAbsent(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State != manifest.StateAbsent {
		t.Errorf("absent manifest: State = %q, want %q", snap.State, manifest.StateAbsent)
	}
}

// TestStore_AbsentManifest_LoadDoesNotReturnError verifies that a missing manifest file
// is communicated via Snapshot.State, not via the Load error return.
func TestStore_AbsentManifest_LoadDoesNotReturnError(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	_, err := store.Load(ws)
	if err != nil {
		t.Errorf("absent manifest: Load returned error %v; absence must be reported via Snapshot.State, not as an error", err)
	}
}

// TestStore_AbsentManifest_ManifestIsZeroValue verifies that an absent Snapshot
// carries a zero-value domain.Manifest.
func TestStore_AbsentManifest_ManifestIsZeroValue(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State == manifest.StateAbsent {
		if snap.Manifest.SchemaVersion != "" || snap.Manifest.HarnessID != "" || len(snap.Manifest.Entries) != 0 {
			t.Error("StateAbsent snapshot has non-zero Manifest; expected zero-value domain.Manifest")
		}
	}
}

// TestStore_AbsentManifest_PathIsPopulated verifies that an absent Snapshot still
// carries the expected manifest path, so callers know where Save would write.
func TestStore_AbsentManifest_PathIsPopulated(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State == manifest.StateAbsent && snap.Path == "" {
		t.Error("StateAbsent snapshot has empty Path; expected the path where the manifest would be created by Save")
	}
}

// ---------------------------------------------------------------------------
// Empty manifest file
// ---------------------------------------------------------------------------

// TestStore_EmptyManifest_ReturnsStateEmpty verifies that a manifest file that exists
// but contains zero bytes produces State == StateEmpty.
func TestStore_EmptyManifest_ReturnsStateEmpty(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte{})

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State != manifest.StateEmpty {
		t.Errorf("zero-byte manifest file: State = %q, want %q", snap.State, manifest.StateEmpty)
	}
}

// TestStore_EmptyManifest_LoadDoesNotReturnError verifies that an empty manifest file
// is communicated via Snapshot.State, not via the Load error return.
func TestStore_EmptyManifest_LoadDoesNotReturnError(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte{})

	_, err := store.Load(ws)
	if err != nil {
		t.Errorf("empty manifest file: Load returned error %v; emptiness must be reported via Snapshot.State", err)
	}
}

// TestStore_EmptyManifest_ManifestIsZeroValue verifies that an empty Snapshot carries
// a zero-value domain.Manifest with no entries.
func TestStore_EmptyManifest_ManifestIsZeroValue(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte{})

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State == manifest.StateEmpty && len(snap.Manifest.Entries) != 0 {
		t.Errorf("StateEmpty snapshot has %d entries; expected zero-value domain.Manifest", len(snap.Manifest.Entries))
	}
}

// TestStore_EmptyManifest_WhitespaceOnlyIsAlsoEmpty verifies that a file containing
// only whitespace is treated as empty (equivalent to zero bytes).
func TestStore_EmptyManifest_WhitespaceOnlyIsAlsoEmpty(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte("   \n\t\n  \n"))

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State != manifest.StateEmpty {
		t.Errorf("whitespace-only manifest file: State = %q, want %q", snap.State, manifest.StateEmpty)
	}
}

// ---------------------------------------------------------------------------
// Corrupt manifest file
// ---------------------------------------------------------------------------

// TestStore_CorruptManifest_ReturnsStateCorrupt verifies that a manifest file with
// invalid YAML content produces State == StateCorrupt.
func TestStore_CorruptManifest_ReturnsStateCorrupt(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte("schema_version: \"1\"\nharness_id: not_a_valid: yaml_key: {broken\nentries: [unclosed\n"))

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State != manifest.StateCorrupt {
		t.Errorf("corrupt manifest: State = %q, want %q", snap.State, manifest.StateCorrupt)
	}
}

// TestStore_CorruptManifest_LoadDoesNotReturnError verifies that a corrupt manifest is
// reported via Snapshot.State, not via the Load error return.
func TestStore_CorruptManifest_LoadDoesNotReturnError(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte("schema_version: \"1\"\nharness_id: not_a_valid: yaml_key: {broken\nentries: [unclosed\n"))

	_, err := store.Load(ws)
	if err != nil {
		t.Errorf("corrupt manifest: Load returned error %v; corruption must be reported via Snapshot.State", err)
	}
}

// TestStore_CorruptManifest_ErrIsNonNil verifies that a corrupt Snapshot carries a
// non-nil Err that explains the parse failure, so callers can log the reason.
func TestStore_CorruptManifest_ErrIsNonNil(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte("schema_version: \"1\"\nharness_id: not_a_valid: yaml_key: {broken\nentries: [unclosed\n"))

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State == manifest.StateCorrupt && snap.Err == nil {
		t.Error("StateCorrupt snapshot has nil Err; expected a non-nil error explaining the parse failure")
	}
}

// TestStore_CorruptManifest_PathIsPopulated verifies that a corrupt Snapshot carries
// the path of the corrupt file, so callers can include it in diagnostics.
func TestStore_CorruptManifest_PathIsPopulated(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte("schema_version: \"1\"\nharness_id: not_a_valid: yaml_key: {broken\nentries: [unclosed\n"))

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State == manifest.StateCorrupt && snap.Path == "" {
		t.Error("StateCorrupt snapshot has empty Path; expected the path to the corrupt manifest file")
	}
}

// TestStore_CorruptManifest_ManifestIsZeroValue verifies that a corrupt Snapshot carries
// a zero-value domain.Manifest. An implementation that returns partially-parsed data when
// the YAML is corrupt would allow callers to accidentally use undefined field values.
func TestStore_CorruptManifest_ManifestIsZeroValue(t *testing.T) {
	store := manifest.NewStore()
	ws := makeWorkspace(t)
	writeManifestFile(t, ws, []byte("schema_version: \"1\"\nharness_id: not_a_valid: yaml_key: {broken\nentries: [unclosed\n"))

	snap, err := store.Load(ws)
	must(t, err)

	if snap.State == manifest.StateCorrupt {
		if snap.Manifest.SchemaVersion != "" || snap.Manifest.HarnessID != "" || len(snap.Manifest.Entries) != 0 {
			t.Error("StateCorrupt snapshot has non-zero Manifest; expected zero-value domain.Manifest " +
				"because the YAML cannot be parsed")
		}
	}
}

// ---------------------------------------------------------------------------
// All five State constants are mutually distinct
// ---------------------------------------------------------------------------

// TestState_AllFiveStatesAreMutuallyDistinct verifies that no two State constants share
// the same string value. Each state must be distinguishable without run-time tricks.
func TestState_AllFiveStatesAreMutuallyDistinct(t *testing.T) {
	states := map[manifest.State]string{
		manifest.StatePresent: "StatePresent",
		manifest.StateAbsent:  "StateAbsent",
		manifest.StateEmpty:   "StateEmpty",
		manifest.StateCorrupt: "StateCorrupt",
		manifest.StateFuture:  "StateFuture",
	}
	// Map de-duplicates by value. If two constants share a string value, the map has fewer
	// than 5 entries.
	if len(states) != 5 {
		t.Errorf("State constants are not mutually distinct: only %d unique values among 5 constants; "+
			"each state must have a unique string value", len(states))
	}
}

// TestState_AllFiveStates_NonEmpty verifies that no State constant is the empty string.
func TestState_AllFiveStates_NonEmpty(t *testing.T) {
	all := []manifest.State{
		manifest.StatePresent,
		manifest.StateAbsent,
		manifest.StateEmpty,
		manifest.StateCorrupt,
		manifest.StateFuture,
	}
	for _, s := range all {
		if s == "" {
			t.Errorf("State constant with value %q is the empty string; all states must be non-empty", s)
		}
	}
}
