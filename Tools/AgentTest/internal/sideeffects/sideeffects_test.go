package sideeffects_test

// Tests for side-effect application and the teardown ledger: files created
// from literal content and from a $ref, recording precisely what was
// created, and a teardown ledger that removes exactly what was created and
// nothing else.
//
// Coverage follows T6.6 / AC6.8.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/sideeffects"
)

func newApplier(t *testing.T, fixtureRoot string) sideeffects.Applier {
	t.Helper()
	res, err := fixtures.NewResolver(fixtureRoot)
	if err != nil {
		t.Fatalf("fixtures.NewResolver(%q) returned unexpected error: %v", fixtureRoot, err)
	}
	return sideeffects.NewApplier(res)
}

func TestApply_LiteralContentEffect_WritesTheFileWithThatContent(t *testing.T) {
	subjectDir := t.TempDir()
	fixtureRoot := t.TempDir()
	applier := newApplier(t, fixtureRoot)

	effects := []domain.FileEffect{
		{Path: "notes/plan.md", Content: "seeded plan content"},
	}

	ledger, err := applier.Apply(subjectDir, effects)

	if err != nil {
		t.Fatalf("Apply returned unexpected error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(subjectDir, "notes", "plan.md"))
	if err != nil {
		t.Fatalf("expected the literal-content file to exist: %v", err)
	}
	if string(got) != "seeded plan content" {
		t.Errorf("file content = %q, want %q", got, "seeded plan content")
	}
	if !containsPath(ledger.Files, filepath.Join("notes", "plan.md")) {
		t.Errorf("ledger.Files = %v, want it to record notes/plan.md", ledger.Files)
	}
}

func TestApply_RefEffect_ResolvesTheFixtureAndWritesItsContent(t *testing.T) {
	subjectDir := t.TempDir()
	fixtureRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixtureRoot, "template.md"), []byte("fixture-sourced content"), 0o644); err != nil {
		t.Fatalf("failed to seed the fixture file: %v", err)
	}
	applier := newApplier(t, fixtureRoot)

	effects := []domain.FileEffect{
		{Path: "docs/from-ref.md", Ref: "template.md"},
	}

	ledger, err := applier.Apply(subjectDir, effects)

	if err != nil {
		t.Fatalf("Apply returned unexpected error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(subjectDir, "docs", "from-ref.md"))
	if err != nil {
		t.Fatalf("expected the $ref-resolved file to exist: %v", err)
	}
	if string(got) != "fixture-sourced content" {
		t.Errorf("file content = %q, want the fixture's content %q", got, "fixture-sourced content")
	}
	if !containsPath(ledger.Files, filepath.Join("docs", "from-ref.md")) {
		t.Errorf("ledger.Files = %v, want it to record docs/from-ref.md", ledger.Files)
	}
}

func TestApply_RecordsDirectoriesItCreated(t *testing.T) {
	subjectDir := t.TempDir()
	fixtureRoot := t.TempDir()
	applier := newApplier(t, fixtureRoot)

	effects := []domain.FileEffect{
		{Path: filepath.Join("a", "b", "c.txt"), Content: "nested"},
	}

	ledger, err := applier.Apply(subjectDir, effects)

	if err != nil {
		t.Fatalf("Apply returned unexpected error: %v", err)
	}
	if !containsPath(ledger.Dirs, "a") || !containsPath(ledger.Dirs, filepath.Join("a", "b")) {
		t.Errorf("ledger.Dirs = %v, want it to record both created directories a and a/b", ledger.Dirs)
	}
}

func TestApply_EffectResolvingOutsideSubjectDir_IsRefused(t *testing.T) {
	subjectDir := t.TempDir()
	fixtureRoot := t.TempDir()
	applier := newApplier(t, fixtureRoot)

	effects := []domain.FileEffect{
		{Path: filepath.Join("..", "escaped.txt"), Content: "should never be written"},
	}

	_, err := applier.Apply(subjectDir, effects)

	if err == nil {
		t.Fatal("expected Apply to refuse an effect whose path escapes subjectDir, got nil error")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(subjectDir), "escaped.txt")); statErr == nil {
		t.Error("Apply must not have written the escaping file, but it exists")
	}
}

func TestSaveLedger_ThenLoadLedger_RoundTripsTheDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "effects.json")
	want := domain.EffectLedger{
		Files: []string{filepath.Join("notes", "plan.md"), filepath.Join("docs", "from-ref.md")},
		Dirs:  []string{"notes", "docs"},
	}

	if err := sideeffects.SaveLedger(path, want); err != nil {
		t.Fatalf("SaveLedger returned unexpected error: %v", err)
	}
	got, err := sideeffects.LoadLedger(path)
	if err != nil {
		t.Fatalf("LoadLedger returned unexpected error: %v", err)
	}
	if len(got.Files) != len(want.Files) || len(got.Dirs) != len(want.Dirs) {
		t.Fatalf("LoadLedger did not round-trip SaveLedger's document: got %+v, want %+v", got, want)
	}
}

func TestRemove_DeletesExactlyTheLedgeredFilesAndDirectories(t *testing.T) {
	subjectDir := t.TempDir()
	fixtureRoot := t.TempDir()
	applier := newApplier(t, fixtureRoot)

	ledger, err := applier.Apply(subjectDir, []domain.FileEffect{
		{Path: filepath.Join("owned", "created.txt"), Content: "created by the applier"},
	})
	if err != nil {
		t.Fatalf("Apply returned unexpected error: %v", err)
	}

	// A file the applier never created, sitting in a directory it did not
	// own, must survive teardown untouched: Remove deletes exactly what the
	// ledger recorded and nothing else.
	untouchedDir := filepath.Join(subjectDir, "pre-existing")
	if err := os.MkdirAll(untouchedDir, 0o755); err != nil {
		t.Fatalf("failed to seed a pre-existing directory: %v", err)
	}
	untouchedFile := filepath.Join(untouchedDir, "keep-me.txt")
	if err := os.WriteFile(untouchedFile, []byte("must survive teardown"), 0o644); err != nil {
		t.Fatalf("failed to seed a pre-existing file: %v", err)
	}

	if err := sideeffects.Remove(subjectDir, ledger); err != nil {
		t.Fatalf("Remove returned unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(subjectDir, "owned", "created.txt")); !os.IsNotExist(err) {
		t.Errorf("expected the ledgered file to be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(subjectDir, "owned")); !os.IsNotExist(err) {
		t.Errorf("expected the now-empty ledgered directory to be removed, stat err = %v", err)
	}
	if _, err := os.Stat(untouchedFile); err != nil {
		t.Errorf("expected the untouched pre-existing file to survive teardown, stat err = %v", err)
	}
}

// TestApply_CalledTwiceAgainstSameSubjectDir_LedgersAreCumulativeSafeWhenMerged
// exercises the Applier doc comment's contract that a returned ledger "is
// cumulative-safe: it may be merged with a previously persisted ledger" —
// merging two ledgers from separate Apply calls against the same subjectDir
// must lose nothing and must let teardown remove everything either call
// created.
func TestApply_CalledTwiceAgainstSameSubjectDir_LedgersAreCumulativeSafeWhenMerged(t *testing.T) {
	subjectDir := t.TempDir()
	fixtureRoot := t.TempDir()
	applier := newApplier(t, fixtureRoot)

	firstLedger, err := applier.Apply(subjectDir, []domain.FileEffect{
		{Path: "first.txt", Content: "from the first Apply call"},
	})
	if err != nil {
		t.Fatalf("first Apply returned unexpected error: %v", err)
	}
	secondLedger, err := applier.Apply(subjectDir, []domain.FileEffect{
		{Path: "second.txt", Content: "from the second Apply call"},
	})
	if err != nil {
		t.Fatalf("second Apply returned unexpected error: %v", err)
	}

	merged := firstLedger
	merged.Merge(secondLedger)

	if !containsPath(merged.Files, "first.txt") || !containsPath(merged.Files, "second.txt") {
		t.Fatalf("merged ledger.Files = %v, want both first.txt and second.txt", merged.Files)
	}
	if len(merged.Files) != len(firstLedger.Files)+len(secondLedger.Files) {
		t.Errorf("merged ledger.Files lost or duplicated entries: got %v, from %v and %v", merged.Files, firstLedger.Files, secondLedger.Files)
	}

	if err := sideeffects.Remove(subjectDir, merged); err != nil {
		t.Fatalf("Remove returned unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(subjectDir, "first.txt")); !os.IsNotExist(err) {
		t.Errorf("expected first.txt to be removed by the merged ledger, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(subjectDir, "second.txt")); !os.IsNotExist(err) {
		t.Errorf("expected second.txt to be removed by the merged ledger, stat err = %v", err)
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if filepath.Clean(p) == filepath.Clean(want) {
			return true
		}
	}
	return false
}
