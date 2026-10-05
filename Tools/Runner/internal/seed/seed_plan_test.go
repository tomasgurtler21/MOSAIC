package seed_test

// Tests for NewPlan happy paths: nil/empty inputs, Plan.IsEmpty, single file
// source, directory source, multiple sources, and the Requirements.md naming rule.

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/seed"
)

// ---------------------------------------------------------------------------
// NewPlan — empty / nil inputs
// ---------------------------------------------------------------------------

func TestNewPlan_NilSources_ReturnsEmptyPlan(t *testing.T) {
	plan, err := seed.NewPlan(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !plan.IsEmpty() {
		t.Errorf("expected empty plan for nil sources, got %d entries", len(plan.Entries))
	}
}

func TestNewPlan_EmptySources_ReturnsEmptyPlan(t *testing.T) {
	plan, err := seed.NewPlan([]string{})
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
// NewPlan — single file source
// ---------------------------------------------------------------------------

// TestNewPlan_SingleFile_AlreadyNamedRequirements_NoOpRename pins the
// no-op case of the naming rule: a sole source already named Requirements.md
// is its own single match, so the rename changes nothing observable.
func TestNewPlan_SingleFile_AlreadyNamedRequirements_NoOpRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Requirements.md")
	writeFile(t, src, "# Requirements\n")

	plan, err := seed.NewPlan([]string{src})
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

func TestNewPlan_SingleFile_SourceRootEqualsSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Plan.md")
	writeFile(t, src, "# Plan\n")
	reqSrc := filepath.Join(dir, "Requirements.md")
	writeFile(t, reqSrc, "# Requirements\n")

	plan, err := seed.NewPlan([]string{src, reqSrc})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(plan.Entries))
	}

	var entry seed.Entry
	found := false
	for _, e := range plan.Entries {
		if e.Source == src {
			entry = e
			found = true
		}
	}
	if !found {
		t.Fatalf("no entry found for source %q", src)
	}
	if entry.SourceRoot != src {
		t.Errorf("SourceRoot = %q, want %q", entry.SourceRoot, src)
	}
	if entry.Source != src {
		t.Errorf("Source = %q, want %q", entry.Source, src)
	}
}

func TestNewPlan_SingleFile_DestUsesForwardSlash(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Input.md")
	writeFile(t, src, "content\n")
	reqSrc := filepath.Join(dir, "Requirements.md")
	writeFile(t, reqSrc, "# Requirements\n")

	plan, err := seed.NewPlan([]string{src, reqSrc})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(plan.Entries))
	}

	for _, e := range plan.Entries {
		if strings.Contains(e.Dest, "\\") {
			t.Errorf("Dest %q must not contain backslashes; destinations must use forward slashes", e.Dest)
		}
	}
}

// ---------------------------------------------------------------------------
// NewPlan — directory source
// ---------------------------------------------------------------------------

func TestNewPlan_DirectorySource_RecursiveDestMapping(t *testing.T) {
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "Plan.md"), "# Plan\n")
	writeFile(t, filepath.Join(srcDir, "Requirements.md"), "# Requirements\n")
	writeFile(t, filepath.Join(srcDir, "Sub", "A.md"), "# A\n")

	plan, err := seed.NewPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(plan.Entries))
	}

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

func TestNewPlan_DirectorySource_NestedStructurePreserved(t *testing.T) {
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "a", "b", "c.md"), "deep\n")
	writeFile(t, filepath.Join(srcDir, "Requirement.md"), "# req\n")

	plan, err := seed.NewPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(plan.Entries))
	}

	var deepEntry seed.Entry
	found := false
	for _, e := range plan.Entries {
		if e.Dest == "a/b/c.md" {
			deepEntry = e
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an entry with Dest %q, entries: %+v", "a/b/c.md", plan.Entries)
	}
	if deepEntry.Dest != "a/b/c.md" {
		t.Errorf("Dest = %q, want %q", deepEntry.Dest, "a/b/c.md")
	}
}

func TestNewPlan_DirectorySource_DestsUseForwardSlash(t *testing.T) {
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "Sub", "file.md"), "content\n")
	writeFile(t, filepath.Join(srcDir, "Requirement.md"), "# req\n")

	plan, err := seed.NewPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range plan.Entries {
		if strings.Contains(e.Dest, "\\") {
			t.Errorf("entry Dest %q contains backslash; destinations must use forward slashes", e.Dest)
		}
	}
}

func TestNewPlan_DirectorySource_SourceRootIsTheGivenDirectory(t *testing.T) {
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "A.md"), "a\n")
	writeFile(t, filepath.Join(srcDir, "B.md"), "b\n")
	writeFile(t, filepath.Join(srcDir, "Requirement.md"), "# req\n")

	plan, err := seed.NewPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range plan.Entries {
		if e.SourceRoot != srcDir {
			t.Errorf("entry SourceRoot = %q, want directory %q", e.SourceRoot, srcDir)
		}
	}
}

func TestNewPlan_DirectorySource_NoEntryForDirectoriesThemselves(t *testing.T) {
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "Sub", "file.md"), "content\n")
	writeFile(t, filepath.Join(srcDir, "Requirement.md"), "# req\n")

	plan, err := seed.NewPlan([]string{srcDir})
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
// NewPlan — multiple sources, ordering
// ---------------------------------------------------------------------------

func TestNewPlan_MultipleSources_OrderPreserved(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "First.md")
	src2 := filepath.Join(dir, "Second.md")
	src3 := filepath.Join(dir, "Requirement.md")
	writeFile(t, src1, "first\n")
	writeFile(t, src2, "second\n")
	writeFile(t, src3, "req\n")

	plan, err := seed.NewPlan([]string{src1, src2, src3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(plan.Entries))
	}
	if plan.Entries[0].Dest != "First.md" {
		t.Errorf("first entry Dest = %q, want \"First.md\"", plan.Entries[0].Dest)
	}
	if plan.Entries[1].Dest != "Second.md" {
		t.Errorf("second entry Dest = %q, want \"Second.md\"", plan.Entries[1].Dest)
	}
}

func TestNewPlan_MultipleSources_FileAndDirectory(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "Root.md")
	srcDirPath := filepath.Join(dir, "srcdir")
	reqFile := filepath.Join(dir, "Requirement.md")
	writeFile(t, srcFile, "root\n")
	writeFile(t, filepath.Join(srcDirPath, "Inside.md"), "inside\n")
	writeFile(t, reqFile, "req\n")

	plan, err := seed.NewPlan([]string{srcFile, srcDirPath, reqFile})
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
// NewPlan — Requirements.md naming rule
// ---------------------------------------------------------------------------

// TestNewPlan_SingleFile_RequirementsMatch_VariousCasings pins case-
// insensitive matching of the Requirement* pattern for a file source.
func TestNewPlan_SingleFile_RequirementsMatch_VariousCasings(t *testing.T) {
	names := []string{"Requirements.md", "requirement-draft.md", "REQUIREMENTS.MD"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, name)
			writeFile(t, src, "# req\n")

			plan, err := seed.NewPlan([]string{src})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(plan.Entries) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(plan.Entries))
			}
			if plan.Entries[0].Dest != "Requirements.md" {
				t.Errorf("Dest = %q, want %q", plan.Entries[0].Dest, "Requirements.md")
			}
		})
	}
}

// TestNewPlan_DirectorySource_TopLevelRequirementsMatch_Renamed pins that a
// single top-level Requirement* match inside a directory source is renamed.
func TestNewPlan_DirectorySource_TopLevelRequirementsMatch_Renamed(t *testing.T) {
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "requirement-notes.md"), "# req\n")
	writeFile(t, filepath.Join(srcDir, "Other.md"), "other\n")

	plan, err := seed.NewPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dests := make(map[string]bool)
	for _, e := range plan.Entries {
		dests[e.Dest] = true
	}
	if !dests["Requirements.md"] {
		t.Errorf("expected the top-level candidate renamed to Requirements.md, got dests %v", dests)
	}
	if dests["requirement-notes.md"] {
		t.Error("the candidate's original destination should not remain in the plan")
	}
	if !dests["Other.md"] {
		t.Error("expected the unrelated entry to keep its existing destination")
	}
}

// TestNewPlan_MultipleSources_CandidateFromFileSource_DirectoryContributesNone
// pins that the candidate pool spans all sources combined.
func TestNewPlan_MultipleSources_CandidateFromFileSource_DirectoryContributesNone(t *testing.T) {
	dir := t.TempDir()
	reqFile := filepath.Join(dir, "Requirement-file.md")
	writeFile(t, reqFile, "# req\n")
	srcDir := filepath.Join(dir, "srcdir")
	writeFile(t, filepath.Join(srcDir, "Notes.md"), "notes\n")

	plan, err := seed.NewPlan([]string{reqFile, srcDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dests := make(map[string]bool)
	for _, e := range plan.Entries {
		dests[e.Dest] = true
	}
	if !dests["Requirements.md"] {
		t.Error("expected the file-source candidate to resolve to Requirements.md")
	}
	if dests["Requirement-file.md"] {
		t.Error("the candidate's original destination should not remain in the plan")
	}
	if !dests["Notes.md"] {
		t.Error("expected the directory-source file to keep its existing destination")
	}
}

// TestNewPlan_SingleFile_SubstringMatch_NotPrefix_NotCandidate pins that the
// Requirement* pattern is prefix-anchored, not a substring match.
func TestNewPlan_SingleFile_SubstringMatch_NotPrefix_NotCandidate(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "MyRequirementsDraft.md")
	writeFile(t, src, "draft\n")

	plan, err := seed.NewPlan([]string{src})
	if err == nil {
		t.Fatal("expected refusal: a substring (non-prefix) match must not count as a candidate")
	}
	assertZeroPlan(t, plan)
	assertRefusalError(t, err, "Requirement")
}

// TestNewPlan_DirectorySource_MultipleTopLevelRequirementsMatches_Refused
// pins that more than one top-level Requirement* match from a single directory is refused.
func TestNewPlan_DirectorySource_MultipleTopLevelRequirementsMatches_Refused(t *testing.T) {
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "Requirements.md"), "one\n")
	writeFile(t, filepath.Join(srcDir, "requirement-draft.md"), "two\n")

	plan, err := seed.NewPlan([]string{srcDir})
	if err == nil {
		t.Fatal("expected refusal for multiple Requirement* matches from a single directory source, got nil error")
	}
	assertZeroPlan(t, plan)
	assertRefusalError(t, err, "Requirement")
}

// TestNewPlan_DirectorySource_NestedRequirementsFile_ExcludedFromCandidatePool
// pins that a Requirement*-named file nested beneath a directory source is excluded.
func TestNewPlan_DirectorySource_NestedRequirementsFile_ExcludedFromCandidatePool(t *testing.T) {
	srcDir := t.TempDir()
	writeFile(t, filepath.Join(srcDir, "Requirement-top.md"), "top\n")
	writeFile(t, filepath.Join(srcDir, "Sub", "Requirements.md"), "nested\n")

	plan, err := seed.NewPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dests := make(map[string]bool)
	for _, e := range plan.Entries {
		dests[e.Dest] = true
	}
	if !dests["Requirements.md"] {
		t.Errorf("expected the top-level candidate renamed to Requirements.md, got dests %v", dests)
	}
	if !dests["Sub/Requirements.md"] {
		t.Error("expected the nested Requirements.md to be copied unrenamed under its relative path")
	}
}

// TestNewPlan_ZeroRequirementsMatches_Refused pins that a non-empty seed set
// with no Requirement* candidate is refused.
func TestNewPlan_ZeroRequirementsMatches_Refused(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "NotesA.md")
	src2 := filepath.Join(dir, "NotesB.md")
	writeFile(t, src1, "a\n")
	writeFile(t, src2, "b\n")

	plan, err := seed.NewPlan([]string{src1, src2})
	if err == nil {
		t.Fatal("expected refusal for zero Requirement* matches, got nil error")
	}
	assertZeroPlan(t, plan)
	assertRefusalError(t, err, "Requirement")
}

// TestNewPlan_MultipleRequirementsMatches_Refused_NamesMatchedFiles pins
// that more than one Requirement* match across the combined source set is refused.
func TestNewPlan_MultipleRequirementsMatches_Refused_NamesMatchedFiles(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "Requirements.md")
	src2 := filepath.Join(dir, "requirement-draft.md")
	writeFile(t, src1, "one\n")
	writeFile(t, src2, "two\n")

	plan, err := seed.NewPlan([]string{src1, src2})
	if err == nil {
		t.Fatal("expected refusal for multiple Requirement* matches, got nil error")
	}
	assertZeroPlan(t, plan)
	assertRefusalError(t, err, src1, src2)
}
