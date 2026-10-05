package seed_test

// Tests for Apply: empty plan no-op, single file landing, byte-identical
// copy, no frontmatter injection, nested directory creation, multiple entries,
// and copy-failure error shape.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/seed"
)

// ---------------------------------------------------------------------------
// Apply — empty plan
// ---------------------------------------------------------------------------

func TestApply_EmptyPlan_NoOp(t *testing.T) {
	target := t.TempDir()
	var plan seed.Plan
	if err := seed.Apply(plan, target); err != nil {
		t.Fatalf("Apply empty plan: unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Apply — single file
// ---------------------------------------------------------------------------

func TestApply_SingleFile_LandsAtCorrectPath(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir()

	content := "hello seeded world\n"
	srcFile := filepath.Join(dir, "Seed.md")
	writeFile(t, srcFile, content)

	plan := seed.Plan{
		Entries: []seed.Entry{
			{Source: srcFile, Dest: "Seed.md", SourceRoot: srcFile},
		},
	}
	if err := seed.Apply(plan, target); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	destPath := filepath.Join(target, "Seed.md")
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", destPath, err)
	}
	if string(got) != content {
		t.Errorf("contents = %q, want %q", string(got), content)
	}
}

func TestApply_SingleFile_ContentsByteIdentical(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir()

	// Binary-ish content with varied bytes to detect accidental templating.
	content := "---\nrun_id: abc\n---\n\n# Doc\n\nSome content with special chars: $VAR {{placeholder}}\n"
	srcFile := filepath.Join(dir, "Doc.md")
	writeFile(t, srcFile, content)

	plan := seed.Plan{
		Entries: []seed.Entry{
			{Source: srcFile, Dest: "Doc.md", SourceRoot: srcFile},
		},
	}
	if err := seed.Apply(plan, target); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(target, "Doc.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != content {
		t.Errorf("file content was modified during copy:\n  got: %q\n want: %q", string(got), content)
	}
}

func TestApply_SingleFile_NoFrontmatterInjected(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir()

	// Source has no frontmatter; the copy must not gain any.
	content := "# Plain document\n\nNo frontmatter here.\n"
	srcFile := filepath.Join(dir, "Plain.md")
	writeFile(t, srcFile, content)

	plan := seed.Plan{
		Entries: []seed.Entry{
			{Source: srcFile, Dest: "Plain.md", SourceRoot: srcFile},
		},
	}
	if err := seed.Apply(plan, target); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(target, "Plain.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != content {
		t.Errorf("Apply injected content:\n  got: %q\n want: %q", string(got), content)
	}
}

// ---------------------------------------------------------------------------
// Apply — nested destination directories
// ---------------------------------------------------------------------------

func TestApply_NestedDest_IntermediateDirectoriesCreated(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir()

	srcFile := filepath.Join(dir, "deep.md")
	writeFile(t, srcFile, "deep content\n")

	plan := seed.Plan{
		Entries: []seed.Entry{
			{Source: srcFile, Dest: "a/b/c/deep.md", SourceRoot: srcFile},
		},
	}
	if err := seed.Apply(plan, target); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	destPath := filepath.Join(target, "a", "b", "c", "deep.md")
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", destPath, err)
	}
	if string(got) != "deep content\n" {
		t.Errorf("contents = %q, want %q", string(got), "deep content\n")
	}
}

// ---------------------------------------------------------------------------
// Apply — multiple entries (full directory plan)
// ---------------------------------------------------------------------------

func TestApply_MultipleEntries_AllApplied(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir()

	writeFile(t, filepath.Join(dir, "A.md"), "aaa\n")
	writeFile(t, filepath.Join(dir, "B.md"), "bbb\n")

	plan := seed.Plan{
		Entries: []seed.Entry{
			{Source: filepath.Join(dir, "A.md"), Dest: "A.md", SourceRoot: dir},
			{Source: filepath.Join(dir, "B.md"), Dest: "B.md", SourceRoot: dir},
		},
	}
	if err := seed.Apply(plan, target); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, name := range []string{"A.md", "B.md"} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Errorf("expected file %s to exist in target: %v", name, err)
		}
	}
}

func TestApply_DirectorySourcePlan_PreservesRelativeStructure(t *testing.T) {
	srcDir := t.TempDir()
	target := t.TempDir()

	writeFile(t, filepath.Join(srcDir, "Root.md"), "root\n")
	writeFile(t, filepath.Join(srcDir, "Sub", "Child.md"), "child\n")

	plan := seed.Plan{
		Entries: []seed.Entry{
			{Source: filepath.Join(srcDir, "Root.md"), Dest: "Root.md", SourceRoot: srcDir},
			{Source: filepath.Join(srcDir, "Sub", "Child.md"), Dest: "Sub/Child.md", SourceRoot: srcDir},
		},
	}
	if err := seed.Apply(plan, target); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	cases := []struct{ dest, want string }{
		{"Root.md", "root\n"},
		{filepath.Join("Sub", "Child.md"), "child\n"},
	}
	for _, c := range cases {
		got, err := os.ReadFile(filepath.Join(target, c.dest))
		if err != nil {
			t.Errorf("ReadFile %s: %v", c.dest, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s: got %q, want %q", c.dest, string(got), c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Apply — copy failure identifies the failing source
// ---------------------------------------------------------------------------

func TestApply_CopyFailure_ErrorNamesFailingSource(t *testing.T) {
	target := t.TempDir()

	// Use a path that does not exist as the source, so the read will fail.
	nonExistentSrc := filepath.Join(t.TempDir(), "ghost.md")

	plan := seed.Plan{
		Entries: []seed.Entry{
			{Source: nonExistentSrc, Dest: "ghost.md", SourceRoot: nonExistentSrc},
		},
	}
	err := seed.Apply(plan, target)
	if err == nil {
		t.Fatal("expected an error for unreadable source, got nil")
	}
	assertRefusalError(t, err, nonExistentSrc)
}
