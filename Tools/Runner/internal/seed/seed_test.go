package seed_test

// Tests for the seed package.
//
// Coverage:
//
//   BuildPlan — plan building (happy paths):
//   - A single file source produces one entry with Dest equal to the file's base name.
//   - A directory source produces one entry per regular file with destinations
//     preserving structure relative to the named directory root.
//   - Nested directory structure is reflected correctly in destination paths.
//   - Multiple sources are applied in the order given; entries maintain that order.
//   - Destination paths use forward slashes regardless of OS path separator.
//   - A nil sources slice yields an empty plan and a nil error.
//   - An empty sources slice yields an empty plan and a nil error.
//   - Plan.IsEmpty returns true for a zero Plan and false for a non-empty Plan.
//   - Entry.SourceRoot equals Source for a file source.
//   - Entry.SourceRoot equals the user-supplied directory path for directory-sourced entries.
//
//   BuildPlan — refusals:
//   - A source path that does not exist on disk is refused; error names the source path.
//   - A source path that is itself a symlink is refused; error names the symlink path.
//   - A symlink encountered inside a directory source is refused; error names the symlink.
//   - Two sources whose entries share a destination are refused; error names BOTH sources.
//   - A destination matching a runner-managed path is refused; error names the destination.
//   - All refusals produce a *domain.RefusalError with Component "seed".
//   - The error Resource and/or Reason contain the offending path(s).
//   - On any refusal the returned Plan is the zero Plan (Entries is nil or empty).
//
//   ReservedDestinations:
//   - Returns a slice that includes "Orchestration.md".
//   - Mutating the returned slice does not affect subsequent calls.
//
//   Apply — plan application (happy paths):
//   - A file entry lands at the correct relative path inside targetFolder.
//   - File contents are byte-identical to the source; no frontmatter is injected.
//   - Nested destination directories are created as needed.
//   - Multiple entries from a directory source are all applied correctly.
//   - Applying an empty plan is a no-op and returns nil.
//
//   Apply — copy failure:
//   - When a copy fails (unreadable source), Apply returns a *domain.RefusalError
//     whose Resource contains the failing source path.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/seed"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeFile creates a file at path with the given content, creating intermediate
// directories as needed. It calls t.Fatal on failure.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("writeFile: MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}

// assertRefusalError asserts err is a *domain.RefusalError with Component "seed"
// and that every wantSubstr appears somewhere in the rendered error string.
func assertRefusalError(t *testing.T, err error, wantSubstrs ...string) {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	if re.Component != "seed" {
		t.Errorf("RefusalError.Component = %q, want \"seed\"", re.Component)
	}
	msg := re.Error()
	for _, sub := range wantSubstrs {
		if !strings.Contains(msg, sub) {
			t.Errorf("error message %q does not contain %q", msg, sub)
		}
	}
}

// assertZeroPlan asserts p is the zero Plan (contains no entries).
func assertZeroPlan(t *testing.T, p seed.Plan) {
	t.Helper()
	if !p.IsEmpty() {
		t.Errorf("expected zero Plan (IsEmpty true), got %d entries", len(p.Entries))
	}
}

// ---------------------------------------------------------------------------
// BuildPlan — empty / nil inputs
// ---------------------------------------------------------------------------

func TestBuildPlan_NilSources_ReturnsEmptyPlan(t *testing.T) {
	plan, err := seed.BuildPlan(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !plan.IsEmpty() {
		t.Errorf("expected empty plan for nil sources, got %d entries", len(plan.Entries))
	}
}

func TestBuildPlan_EmptySources_ReturnsEmptyPlan(t *testing.T) {
	plan, err := seed.BuildPlan([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !plan.IsEmpty() {
		t.Errorf("expected empty plan for empty sources, got %d entries", len(plan.Entries))
	}
}

// ---------------------------------------------------------------------------
// Plan.IsEmpty
// ---------------------------------------------------------------------------

func TestPlan_IsEmpty_ZeroPlan(t *testing.T) {
	var p seed.Plan
	if !p.IsEmpty() {
		t.Error("zero Plan.IsEmpty() should return true")
	}
}

func TestPlan_IsEmpty_NonEmptyPlan(t *testing.T) {
	p := seed.Plan{Entries: []seed.Entry{{Source: "a", Dest: "a", SourceRoot: "a"}}}
	if p.IsEmpty() {
		t.Error("non-empty Plan.IsEmpty() should return false")
	}
}

// ---------------------------------------------------------------------------
// BuildPlan — single file source
// ---------------------------------------------------------------------------

func TestBuildPlan_SingleFile_DestIsBaseName(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Requirements.md")
	writeFile(t, src, "# Requirements\n")

	plan, err := seed.BuildPlan([]string{src})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(plan.Entries))
	}

	entry := plan.Entries[0]
	if entry.Dest != "Requirements.md" {
		t.Errorf("Dest = %q, want %q", entry.Dest, "Requirements.md")
	}
}

func TestBuildPlan_SingleFile_SourceRootEqualsSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Plan.md")
	writeFile(t, src, "# Plan\n")

	plan, err := seed.BuildPlan([]string{src})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(plan.Entries))
	}

	entry := plan.Entries[0]
	if entry.SourceRoot != src {
		t.Errorf("SourceRoot = %q, want %q", entry.SourceRoot, src)
	}
	if entry.Source != src {
		t.Errorf("Source = %q, want %q", entry.Source, src)
	}
}

func TestBuildPlan_SingleFile_DestUsesForwardSlash(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Input.md")
	writeFile(t, src, "content\n")

	plan, err := seed.BuildPlan([]string{src})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(plan.Entries))
	}

	// Dest must use forward slashes, not OS-native separators.
	if strings.Contains(plan.Entries[0].Dest, "\\") {
		t.Errorf("Dest %q must not contain backslashes; destinations must use forward slashes", plan.Entries[0].Dest)
	}
}

// ---------------------------------------------------------------------------
// BuildPlan — directory source
// ---------------------------------------------------------------------------

func TestBuildPlan_DirectorySource_RecursiveDestMapping(t *testing.T) {
	// Layout:
	//   srcDir/
	//     Plan.md
	//     Sub/
	//       A.md
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "Plan.md"), "# Plan\n")
	writeFile(t, filepath.Join(srcDir, "Sub", "A.md"), "# A\n")

	plan, err := seed.BuildPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(plan.Entries))
	}

	// Collect dest set for order-independent assertion.
	dests := make(map[string]bool)
	for _, e := range plan.Entries {
		dests[e.Dest] = true
	}
	if !dests["Plan.md"] {
		t.Error("expected dest \"Plan.md\" from directory source")
	}
	if !dests["Sub/A.md"] {
		t.Error("expected dest \"Sub/A.md\" from directory source")
	}
}

func TestBuildPlan_DirectorySource_NestedStructurePreserved(t *testing.T) {
	// Deep nesting: a/b/c.md → "a/b/c.md" relative to the directory root.
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "a", "b", "c.md"), "deep\n")

	plan, err := seed.BuildPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(plan.Entries))
	}
	if plan.Entries[0].Dest != "a/b/c.md" {
		t.Errorf("Dest = %q, want %q", plan.Entries[0].Dest, "a/b/c.md")
	}
}

func TestBuildPlan_DirectorySource_DestsUseForwardSlash(t *testing.T) {
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "Sub", "file.md"), "content\n")

	plan, err := seed.BuildPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range plan.Entries {
		if strings.Contains(e.Dest, "\\") {
			t.Errorf("entry Dest %q contains backslash; destinations must use forward slashes", e.Dest)
		}
	}
}

func TestBuildPlan_DirectorySource_SourceRootIsTheGivenDirectory(t *testing.T) {
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "A.md"), "a\n")
	writeFile(t, filepath.Join(srcDir, "B.md"), "b\n")

	plan, err := seed.BuildPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range plan.Entries {
		if e.SourceRoot != srcDir {
			t.Errorf("entry SourceRoot = %q, want directory %q", e.SourceRoot, srcDir)
		}
	}
}

func TestBuildPlan_DirectorySource_NoEntryForDirectoriesThemselves(t *testing.T) {
	// Directories should not appear as entries; only regular files do.
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "Sub", "file.md"), "content\n")
	// srcDir/Sub itself must not appear as an entry.

	plan, err := seed.BuildPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range plan.Entries {
		if e.Dest == "Sub" || e.Dest == "Sub/" {
			t.Errorf("directory itself should not be an entry, but found Dest=%q", e.Dest)
		}
	}
}

// ---------------------------------------------------------------------------
// BuildPlan — multiple sources, ordering
// ---------------------------------------------------------------------------

func TestBuildPlan_MultipleSources_OrderPreserved(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "First.md")
	src2 := filepath.Join(dir, "Second.md")
	writeFile(t, src1, "first\n")
	writeFile(t, src2, "second\n")

	plan, err := seed.BuildPlan([]string{src1, src2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(plan.Entries))
	}
	if plan.Entries[0].Dest != "First.md" {
		t.Errorf("first entry Dest = %q, want \"First.md\"", plan.Entries[0].Dest)
	}
	if plan.Entries[1].Dest != "Second.md" {
		t.Errorf("second entry Dest = %q, want \"Second.md\"", plan.Entries[1].Dest)
	}
}

func TestBuildPlan_MultipleSources_FileAndDirectory(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "Root.md")
	srcDirPath := filepath.Join(dir, "srcdir")
	writeFile(t, srcFile, "root\n")
	writeFile(t, filepath.Join(srcDirPath, "Inside.md"), "inside\n")

	plan, err := seed.BuildPlan([]string{srcFile, srcDirPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dests := make(map[string]bool)
	for _, e := range plan.Entries {
		dests[e.Dest] = true
	}
	if !dests["Root.md"] {
		t.Error("expected dest \"Root.md\" from file source")
	}
	if !dests["Inside.md"] {
		t.Error("expected dest \"Inside.md\" from directory source")
	}
}

// ---------------------------------------------------------------------------
// BuildPlan — refusal: non-existent source
// ---------------------------------------------------------------------------

func TestBuildPlan_NonExistentSource_Refused(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist.md")

	plan, err := seed.BuildPlan([]string{nonExistent})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	assertZeroPlan(t, plan)
	assertRefusalError(t, err, nonExistent)
}

// ---------------------------------------------------------------------------
// BuildPlan — refusal: symlink as source path
// ---------------------------------------------------------------------------

func TestBuildPlan_SymlinkAsSource_Refused(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Symlink creation requires elevated privileges on Windows; skip rather
		// than weakening the production check to accommodate the test environment.
		t.Skip("symlink creation not available without elevated privileges on Windows")
	}

	dir := t.TempDir()
	realFile := filepath.Join(dir, "real.md")
	writeFile(t, realFile, "content\n")
	linkPath := filepath.Join(dir, "link.md")
	if err := os.Symlink(realFile, linkPath); err != nil {
		t.Fatalf("os.Symlink: %v", err)
	}

	plan, err := seed.BuildPlan([]string{linkPath})
	if err == nil {
		t.Fatal("expected an error for symlink source, got nil")
	}
	assertZeroPlan(t, plan)
	assertRefusalError(t, err, linkPath)
}

// ---------------------------------------------------------------------------
// BuildPlan — refusal: symlink inside a directory source
// ---------------------------------------------------------------------------

func TestBuildPlan_SymlinkInsideDirectory_Refused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation not available without elevated privileges on Windows")
	}

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "srcdir")
	writeFile(t, filepath.Join(srcDir, "regular.md"), "ok\n")

	// Create a symlink inside srcDir.
	realTarget := filepath.Join(dir, "external.md")
	writeFile(t, realTarget, "external\n")
	linkInside := filepath.Join(srcDir, "linked.md")
	if err := os.Symlink(realTarget, linkInside); err != nil {
		t.Fatalf("os.Symlink: %v", err)
	}

	plan, err := seed.BuildPlan([]string{srcDir})
	if err == nil {
		t.Fatal("expected an error for symlink inside directory, got nil")
	}
	assertZeroPlan(t, plan)
	assertRefusalError(t, err, linkInside)
}

// ---------------------------------------------------------------------------
// BuildPlan — refusal: cross-source destination collision (names BOTH sources)
// ---------------------------------------------------------------------------

func TestBuildPlan_CrossSourceCollision_RefusedNamesBothSources(t *testing.T) {
	dir := t.TempDir()
	// Both files have the same base name → both map to dest "Plan.md".
	src1 := filepath.Join(dir, "dir1", "Plan.md")
	src2 := filepath.Join(dir, "dir2", "Plan.md")
	writeFile(t, src1, "plan from dir1\n")
	writeFile(t, src2, "plan from dir2\n")

	plan, err := seed.BuildPlan([]string{src1, src2})
	if err == nil {
		t.Fatal("expected an error for cross-source collision, got nil")
	}
	assertZeroPlan(t, plan)
	// Error must name both offending source paths.
	assertRefusalError(t, err, src1, src2)
}

func TestBuildPlan_CrossSourceCollision_DirectoryAndFile(t *testing.T) {
	// A directory containing "Report.md" and a separate file source also named
	// "Report.md" → both map to dest "Report.md".
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "srcdir")
	writeFile(t, filepath.Join(srcDir, "Report.md"), "from dir\n")
	srcFile := filepath.Join(dir, "Report.md")
	writeFile(t, srcFile, "from file\n")

	plan, err := seed.BuildPlan([]string{srcDir, srcFile})
	if err == nil {
		t.Fatal("expected cross-source collision error, got nil")
	}
	assertZeroPlan(t, plan)
	// Both source roots must appear.
	assertRefusalError(t, err, srcDir, srcFile)
}

// ---------------------------------------------------------------------------
// BuildPlan — refusal: runner-managed destination
// ---------------------------------------------------------------------------

func TestBuildPlan_RunnerManagedDestination_Refused(t *testing.T) {
	dir := t.TempDir()
	// A file named "Orchestration.md" maps to dest "Orchestration.md", which is
	// reserved.
	src := filepath.Join(dir, "Orchestration.md")
	writeFile(t, src, "# fake orchestration\n")

	plan, err := seed.BuildPlan([]string{src})
	if err == nil {
		t.Fatal("expected an error for runner-managed destination, got nil")
	}
	assertZeroPlan(t, plan)
	assertRefusalError(t, err, "Orchestration.md")
}

func TestBuildPlan_RunnerManagedDestinationInSubdir_NotRefused(t *testing.T) {
	// "Sub/Orchestration.md" is NOT reserved — only the exact root-level path is.
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "srcdir")
	writeFile(t, filepath.Join(srcDir, "Sub", "Orchestration.md"), "nested\n")

	plan, err := seed.BuildPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error for Sub/Orchestration.md (not reserved): %v", err)
	}
	if plan.IsEmpty() {
		t.Error("expected a non-empty plan, got empty")
	}
}

// ---------------------------------------------------------------------------
// ReservedDestinations
// ---------------------------------------------------------------------------

func TestReservedDestinations_ContainsOrchestrationMd(t *testing.T) {
	reserved := seed.ReservedDestinations()
	found := false
	for _, r := range reserved {
		if r == "Orchestration.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ReservedDestinations() = %v; want slice containing \"Orchestration.md\"", reserved)
	}
}

func TestReservedDestinations_MutatingReturnedSliceDoesNotAffectSubsequentCalls(t *testing.T) {
	r1 := seed.ReservedDestinations()
	// Clobber the first element.
	if len(r1) > 0 {
		r1[0] = "tampered"
	}
	r2 := seed.ReservedDestinations()
	for _, r := range r2 {
		if r == "tampered" {
			t.Error("mutating returned slice affected subsequent ReservedDestinations() call")
		}
	}
}

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
