package catalog_test

// skills_recursive_test.go covers the Stage 4 requirement that parseSkillDir (via
// catalog.Load) recursively enumerates all files in a skill folder, including files
// in nested subdirectories. These tests are written in the TDD RED phase: they
// specify the required behavior and fail with the current non-recursive implementation
// (which uses os.ReadDir and skips subdirectory contents).
//
// Verified contracts:
//
//   Nested file inclusion: a file placed in a subdirectory appears in skill.Files
//   with its relative path including the subdirectory prefix (e.g., "subdir/ref.md").
//
//   Deep nesting: a file in a multiply-nested subdirectory appears with its full
//   relative path (e.g., "a/b/nested.md").
//
//   Directory exclusion: subdirectory entries themselves do not appear in skill.Files;
//   only regular files are listed.
//
//   File count: skill.Files count includes both flat (root-level) files and all
//   files from nested subdirectories, not just root-level files.
//
//   Ordering: the returned file list uses filepath.WalkDir's lexicographic depth-first
//   order — entries at the root level appear before subdirectory entries only when the
//   root-level name sorts before the subdirectory name; within a subdirectory, entries
//   are lexicographically ordered.
//
//   Single-file backward compatibility: a skill folder containing only SKILL.md
//   continues to produce exactly one entry in Files (regression guard).

import (
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// ---------------------------------------------------------------------------
// Nested subdirectory file inclusion
// ---------------------------------------------------------------------------

// TestSkill_RecursiveWalk_NestedFileAppearsWithRelativePath verifies that a file placed
// in a direct subdirectory of the skill folder appears in skill.Files with the
// subdirectory prefix as part of the relative path (e.g., "examples/usage.md").
// With the current non-recursive implementation this test fails: the file is silently
// omitted because os.ReadDir only reads the top-level directory entries.
func TestSkill_RecursiveWalk_NestedFileAppearsWithRelativePath(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillFolder(t, root, "sample-skill")
	// Create a subdirectory with a file inside the skill folder.
	mustMkdir(t, root, "Catalog", "Skills", "sample-skill", "examples")
	mustWriteFile(t, root,
		filepath.Join("Catalog", "Skills", "sample-skill", "examples", "usage.md"),
		[]byte("# Usage\n"))

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	s, ok := cat.Skill("sample-skill")
	if !ok {
		t.Fatal("Skill(\"sample-skill\"): not found")
	}

	const wantFile = "examples/usage.md"
	found := false
	for _, f := range s.Files {
		if filepath.ToSlash(f) == wantFile {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("sample-skill.Files does not contain %q; got %v\n"+
			"parseSkillDir must use filepath.WalkDir to enumerate files recursively",
			wantFile, s.Files)
	}
}

// TestSkill_RecursiveWalk_DeeplyNestedFileAppearsWithFullRelativePath verifies that a
// file nested several directory levels deep appears in skill.Files with its full relative
// path (e.g., "a/b/nested.md"). The relative path must preserve the complete directory
// structure from the skill root.
func TestSkill_RecursiveWalk_DeeplyNestedFileAppearsWithFullRelativePath(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillFolder(t, root, "sample-skill")
	mustMkdir(t, root, "Catalog", "Skills", "sample-skill", "a", "b")
	mustWriteFile(t, root,
		filepath.Join("Catalog", "Skills", "sample-skill", "a", "b", "nested.md"),
		[]byte("# Nested\n"))

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	s, ok := cat.Skill("sample-skill")
	if !ok {
		t.Fatal("Skill(\"sample-skill\"): not found")
	}

	const wantFile = "a/b/nested.md"
	found := false
	for _, f := range s.Files {
		if filepath.ToSlash(f) == wantFile {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("sample-skill.Files does not contain deeply nested path %q; got %v\n"+
			"parseSkillDir must recurse into nested subdirectories",
			wantFile, s.Files)
	}
}

// ---------------------------------------------------------------------------
// Directory exclusion
// ---------------------------------------------------------------------------

// TestSkill_RecursiveWalk_SubdirectoryEntriesNotInFiles verifies that subdirectory entries
// themselves (e.g., "examples") do not appear in skill.Files. Only regular files should
// be listed; directory entries are excluded. This invariant applies to both the current
// flat implementation and the recursive implementation being introduced.
func TestSkill_RecursiveWalk_SubdirectoryEntriesNotInFiles(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillFolder(t, root, "sample-skill")
	mustMkdir(t, root, "Catalog", "Skills", "sample-skill", "examples")
	mustWriteFile(t, root,
		filepath.Join("Catalog", "Skills", "sample-skill", "examples", "usage.md"),
		[]byte("# Usage\n"))

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	s, ok := cat.Skill("sample-skill")
	if !ok {
		t.Fatal("Skill(\"sample-skill\"): not found")
	}

	for _, f := range s.Files {
		slash := filepath.ToSlash(f)
		if slash == "examples" {
			t.Errorf("sample-skill.Files contains directory entry %q; only regular files must be listed", f)
		}
	}
}

// ---------------------------------------------------------------------------
// File count with nested subdirectories
// ---------------------------------------------------------------------------

// TestSkill_RecursiveWalk_TotalFileCountIncludesNestedFiles verifies that the count of
// skill.Files equals the total number of regular files across all directories (root +
// subdirectories), not just the number of root-level entries. With the current non-recursive
// implementation, nested files are omitted and the count is too low.
func TestSkill_RecursiveWalk_TotalFileCountIncludesNestedFiles(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillFolder(t, root, "sample-skill") // SKILL.md at root
	mustMkdir(t, root, "Catalog", "Skills", "sample-skill", "examples")
	mustWriteFile(t, root,
		filepath.Join("Catalog", "Skills", "sample-skill", "examples", "usage.md"),
		[]byte("# Usage\n"))
	mustWriteFile(t, root,
		filepath.Join("Catalog", "Skills", "sample-skill", "examples", "advanced.md"),
		[]byte("# Advanced\n"))

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	s, ok := cat.Skill("sample-skill")
	if !ok {
		t.Fatal("Skill(\"sample-skill\"): not found")
	}

	// Expect: SKILL.md + examples/usage.md + examples/advanced.md = 3
	const wantCount = 3
	if len(s.Files) != wantCount {
		t.Errorf("sample-skill Files count = %d, want %d\n"+
			"  got: %v\n"+
			"  parseSkillDir must count files across all directories, not just the root",
			len(s.Files), wantCount, s.Files)
	}
}

// TestSkill_RecursiveWalk_MixedFlatAndNestedFiles verifies that a skill folder with both
// root-level companion files and files in subdirectories yields all of them in skill.Files.
// This is the most realistic multi-file scenario: a skill has its main SKILL.md, some
// flat companion files, and some organised-by-subdirectory support files.
func TestSkill_RecursiveWalk_MixedFlatAndNestedFiles(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillFolder(t, root, "sample-skill")                    // SKILL.md
	mustWriteFile(t, root,                                        // flat companion
		filepath.Join("Catalog", "Skills", "sample-skill", "EXAMPLES.md"),
		[]byte("# Examples\n"))
	mustMkdir(t, root, "Catalog", "Skills", "sample-skill", "templates")
	mustWriteFile(t, root,                                        // nested file
		filepath.Join("Catalog", "Skills", "sample-skill", "templates", "default.md"),
		[]byte("# Default template\n"))

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	s, ok := cat.Skill("sample-skill")
	if !ok {
		t.Fatal("Skill(\"sample-skill\"): not found")
	}

	// All three files must appear.
	wantFiles := map[string]bool{
		"SKILL.md":              false,
		"EXAMPLES.md":           false,
		"templates/default.md":  false,
	}
	for _, f := range s.Files {
		wantFiles[filepath.ToSlash(f)] = true
	}
	for name, found := range wantFiles {
		if !found {
			t.Errorf("sample-skill.Files missing %q; got %v", name, s.Files)
		}
	}
}

// ---------------------------------------------------------------------------
// Lexicographic depth-first ordering
// ---------------------------------------------------------------------------

// TestSkill_RecursiveWalk_LexicographicDepthFirstOrder verifies that skill.Files is ordered
// following filepath.WalkDir semantics: lexicographic depth-first. A file whose name sorts
// after a subdirectory name appears after all files in that subdirectory; a file whose name
// sorts before a subdirectory name appears before the subdirectory's contents.
//
// Fixture layout:
//   SKILL.md        (root, sorts before "a" → comes first)
//   a/first.md      (subdirectory "a" sorts before root-level "z.md")
//   a/second.md
//   z.md            (root, sorts after "a" → comes after a/*)
//
// Expected WalkDir order: SKILL.md, a/first.md, a/second.md, z.md
func TestSkill_RecursiveWalk_LexicographicDepthFirstOrder(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillFolder(t, root, "sample-skill") // SKILL.md
	mustMkdir(t, root, "Catalog", "Skills", "sample-skill", "a")
	mustWriteFile(t, root,
		filepath.Join("Catalog", "Skills", "sample-skill", "a", "first.md"),
		[]byte("# First\n"))
	mustWriteFile(t, root,
		filepath.Join("Catalog", "Skills", "sample-skill", "a", "second.md"),
		[]byte("# Second\n"))
	mustWriteFile(t, root,
		filepath.Join("Catalog", "Skills", "sample-skill", "z.md"),
		[]byte("# Z\n"))

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	s, ok := cat.Skill("sample-skill")
	if !ok {
		t.Fatal("Skill(\"sample-skill\"): not found")
	}

	// Normalize to forward slashes for comparison.
	got := make([]string, len(s.Files))
	for i, f := range s.Files {
		got[i] = filepath.ToSlash(f)
	}

	want := []string{"SKILL.md", "a/first.md", "a/second.md", "z.md"}

	if len(got) != len(want) {
		t.Fatalf("sample-skill Files length = %d, want %d\n  got: %v\n  want: %v",
			len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Files[%d] = %q, want %q (lexicographic depth-first order)\n  got:  %v\n  want: %v",
				i, got[i], want[i], got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Backward compatibility: single-file skill
// ---------------------------------------------------------------------------

// TestSkill_RecursiveWalk_SingleFileSkill_StillProducesOneEntry verifies that a skill folder
// containing only SKILL.md (no subdirectories, no companion files) continues to produce
// exactly one entry in skill.Files after the recursive walk change. This is a regression
// guard: the new implementation must not add phantom entries for the directory itself.
func TestSkill_RecursiveWalk_SingleFileSkill_StillProducesOneEntry(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillFolder(t, root, "sample-skill") // only SKILL.md

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	s, ok := cat.Skill("sample-skill")
	if !ok {
		t.Fatal("Skill(\"sample-skill\"): not found")
	}

	if len(s.Files) != 1 {
		t.Errorf("single-file skill Files count = %d, want 1; got %v", len(s.Files), s.Files)
	}
	if len(s.Files) == 1 && filepath.ToSlash(s.Files[0]) != "SKILL.md" {
		t.Errorf("single-file skill Files[0] = %q, want %q", s.Files[0], "SKILL.md")
	}
}
