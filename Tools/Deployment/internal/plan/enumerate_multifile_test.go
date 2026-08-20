package plan_test

// enumerate_multifile_test.go covers Stage 4 requirements for multi-file skill enumeration:
//
//   T4.2 — EnumerateTargetPaths multi-file: a skill with N files in skill.Files must
//   produce N PlannedPath entries (all sharing the same ArtifactRef) rather than one.
//   Each entry carries the correct target path for that file, including subdirectory
//   structure (e.g., "skills/<key>/subdir/ref.md").
//
//   T4.4 — PlannedPaths.PathsFor: the new PathsFor method returns all target paths for
//   a given ArtifactRef in enumeration order, supporting multi-file skills where Path()
//   (which returns only the first match) is insufficient. Returns nil for an unknown ref.
//
// These tests are written in the TDD RED phase. EnumerateTargetPaths currently produces
// one PlannedPath per skill (using EntryFile only), so multi-file assertions fail. PathsFor
// currently panics (stub body) because the implementation does not exist yet.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// EnumerateTargetPaths — multi-file skill produces one entry per file
// ---------------------------------------------------------------------------

// TestEnumerateTargetPaths_MultiFileSkill_ProducesOneEntryPerFile verifies that when a skill
// has Files = ["SKILL.md", "companion.md"], EnumerateTargetPaths produces two PlannedPath
// entries for that skill (both sharing the same ArtifactRef) rather than one. With the
// current implementation (single TargetPath call per skill using EntryFile), only one entry
// is produced and this test fails.
func TestEnumerateTargetPaths_MultiFileSkill_ProducesOneEntryPerFile(t *testing.T) {
	skill := domain.Skill{
		Key:       "lean-tdd",
		Name:      "lean-tdd-name",
		Version:   "1.0",
		SourceDir: "/fake/skills/lean-tdd",
		EntryFile: "SKILL.md",
		Files:     []string{"SKILL.md", "companion.md"},
	}
	set := plan.ArtifactSet{
		Skills: []domain.Skill{skill},
	}

	mod := newFakeModule()
	mod.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactSkill {
			// Return a path that includes the FileName, preserving subdirectory structure.
			return "skills/" + req.Key + "/" + req.FileName, nil
		}
		return "agents/" + req.Key + ".md", nil
	}

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")
	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}

	// Count entries for the skill ref.
	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}
	var skillPaths []string
	for _, pp := range paths {
		if pp.Ref == ref {
			skillPaths = append(skillPaths, pp.TargetPath)
		}
	}

	if len(skillPaths) != 2 {
		t.Errorf("multi-file skill produced %d PlannedPath entries, want 2\n"+
			"  EnumerateTargetPaths must call TargetPath once per file in skill.Files, not once per skill\n"+
			"  got paths: %v", len(skillPaths), skillPaths)
	}
}

// TestEnumerateTargetPaths_MultiFileSkill_EachFileHasCorrectTargetPath verifies that each
// of the N PlannedPath entries for a multi-file skill carries the correct target path for
// its corresponding file. The path for "examples/usage.md" must include the subdirectory
// prefix, not just the filename.
func TestEnumerateTargetPaths_MultiFileSkill_EachFileHasCorrectTargetPath(t *testing.T) {
	skill := domain.Skill{
		Key:       "lean-tdd",
		Name:      "lean-tdd-name",
		Version:   "1.0",
		SourceDir: "/fake/skills/lean-tdd",
		EntryFile: "SKILL.md",
		Files:     []string{"SKILL.md", "examples/usage.md"},
	}
	set := plan.ArtifactSet{
		Skills: []domain.Skill{skill},
	}

	mod := newFakeModule()
	mod.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactSkill {
			return "skills/" + req.Key + "/" + req.FileName, nil
		}
		return "agents/" + req.Key + ".md", nil
	}

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")
	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}

	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}
	pathSet := make(map[string]bool)
	for _, pp := range paths {
		if pp.Ref == ref {
			pathSet[pp.TargetPath] = true
		}
	}

	wantPaths := []string{
		"skills/lean-tdd/SKILL.md",
		"skills/lean-tdd/examples/usage.md",
	}
	for _, want := range wantPaths {
		if !pathSet[want] {
			t.Errorf("expected target path %q for multi-file skill, not found in PlannedPaths\n"+
				"  EnumerateTargetPaths must pass each file's relative path as req.FileName\n"+
				"  got paths: %v", want, pathSet)
		}
	}
}

// TestEnumerateTargetPaths_MultiFileSkill_TotalCountReflectsAllFiles verifies that the total
// count of PlannedPath entries equals agents + (sum of files per skill) + supported hooks.
// A skill with 3 files contributes 3 entries, not 1.
func TestEnumerateTargetPaths_MultiFileSkill_TotalCountReflectsAllFiles(t *testing.T) {
	skill := domain.Skill{
		Key:       "lean-tdd",
		Name:      "lean-tdd-name",
		Version:   "1.0",
		SourceDir: "/fake/skills/lean-tdd",
		EntryFile: "SKILL.md",
		Files:     []string{"SKILL.md", "companion.md", "templates/default.md"},
	}
	set := plan.ArtifactSet{
		Agents: []domain.Agent{makeAgent("worker", "1.0")},
		Skills: []domain.Skill{skill},
	}

	mod := newFakeModule()
	mod.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		switch req.Kind {
		case domain.ArtifactAgent:
			return "agents/" + req.Key + ".md", nil
		case domain.ArtifactSkill:
			return "skills/" + req.Key + "/" + req.FileName, nil
		}
		return "", domain.ErrArtifactUnsupported
	}

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")
	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}

	// 1 agent + 3 skill files = 4 total
	const wantTotal = 4
	if len(paths) != wantTotal {
		t.Errorf("EnumerateTargetPaths returned %d entries, want %d (1 agent + 3 skill files)\n"+
			"  a skill with N files must contribute N PlannedPath entries",
			len(paths), wantTotal)
	}
}

// TestEnumerateTargetPaths_MultiFileSkill_FilesPassedAsFileName verifies that
// EnumerateTargetPaths passes each file's relative path (including subdirectory prefix)
// as the FileName field of TargetPathRequest. This is how the harness module produces
// the correct subdirectory-preserving target path for each file.
func TestEnumerateTargetPaths_MultiFileSkill_FilesPassedAsFileName(t *testing.T) {
	skill := domain.Skill{
		Key:       "lean-tdd",
		Name:      "lean-tdd-name",
		Version:   "1.0",
		SourceDir: "/fake/skills/lean-tdd",
		EntryFile: "SKILL.md",
		Files:     []string{"SKILL.md", "subdir/ref.md"},
	}
	set := plan.ArtifactSet{
		Skills: []domain.Skill{skill},
	}

	// Capture the FileName values that EnumerateTargetPaths passes to TargetPath.
	var capturedFileNames []string
	mod := newFakeModule()
	mod.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactSkill {
			capturedFileNames = append(capturedFileNames, req.FileName)
			return "skills/" + req.Key + "/" + req.FileName, nil
		}
		return "", domain.ErrArtifactUnsupported
	}

	_, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")
	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}

	wantFileNames := []string{"SKILL.md", "subdir/ref.md"}
	if len(capturedFileNames) != len(wantFileNames) {
		t.Fatalf("TargetPath called with %d FileName values, want %d: %v",
			len(capturedFileNames), len(wantFileNames), capturedFileNames)
	}
	for i, want := range wantFileNames {
		if capturedFileNames[i] != want {
			t.Errorf("FileName[%d] = %q, want %q", i, capturedFileNames[i], want)
		}
	}
}

// ---------------------------------------------------------------------------
// EnumerateTargetPaths — single-file skill backward compatibility
// ---------------------------------------------------------------------------

// TestEnumerateTargetPaths_SingleFileSkill_StillProducesOneEntry verifies that a skill
// with Files = ["SKILL.md"] (or an empty Files that falls back to EntryFile) continues to
// produce exactly one PlannedPath entry after the multi-file change. This is a regression
// guard for single-file skills.
func TestEnumerateTargetPaths_SingleFileSkill_StillProducesOneEntry(t *testing.T) {
	skill := domain.Skill{
		Key:       "lean-tdd",
		Name:      "lean-tdd-name",
		Version:   "1.0",
		SourceDir: "/fake/skills/lean-tdd",
		EntryFile: "SKILL.md",
		Files:     []string{"SKILL.md"},
	}
	set := plan.ArtifactSet{
		Skills: []domain.Skill{skill},
	}

	mod := newFakeModule()
	mod.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactSkill {
			return "skills/" + req.Key + "/" + req.FileName, nil
		}
		return "agents/" + req.Key + ".md", nil
	}

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")
	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}

	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}
	count := 0
	for _, pp := range paths {
		if pp.Ref == ref {
			count++
		}
	}
	if count != 1 {
		t.Errorf("single-file skill produced %d PlannedPath entries, want 1", count)
	}
}

// ---------------------------------------------------------------------------
// PlannedPaths.PathsFor — new multi-entry lookup method
// ---------------------------------------------------------------------------

// TestPlannedPaths_PathsFor_ReturnsAllTargetPathsForRef verifies that PathsFor returns
// all target paths for a given ArtifactRef, in the order they appear in the PlannedPaths
// slice. When two PlannedPath entries share the same Ref (as multi-file skills produce),
// both target paths are returned — unlike Path(), which returns only the first.
//
// With the current nil-returning stub, this test fails via assertion: PathsFor returns nil
// so len(got) == 0, not 2.
func TestPlannedPaths_PathsFor_ReturnsAllTargetPathsForRef(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}
	paths := plan.PlannedPaths{
		{Ref: ref, TargetPath: "skills/lean-tdd/SKILL.md"},
		{Ref: ref, TargetPath: "skills/lean-tdd/companion.md"},
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "worker"}, TargetPath: "agents/worker.md"},
	}

	got := paths.PathsFor(ref)

	if len(got) != 2 {
		t.Fatalf("PathsFor returned %d paths, want 2: %v", len(got), got)
	}
	if got[0] != "skills/lean-tdd/SKILL.md" {
		t.Errorf("PathsFor[0] = %q, want %q", got[0], "skills/lean-tdd/SKILL.md")
	}
	if got[1] != "skills/lean-tdd/companion.md" {
		t.Errorf("PathsFor[1] = %q, want %q", got[1], "skills/lean-tdd/companion.md")
	}
}

// TestPlannedPaths_PathsFor_ReturnsNilForUnknownRef verifies that PathsFor returns nil
// (not an empty non-nil slice) when the given ref has no entries in PlannedPaths. Callers
// distinguish "no entries" from "ref present with zero paths" by nil vs. empty slice.
func TestPlannedPaths_PathsFor_ReturnsNilForUnknownRef(t *testing.T) {
	paths := plan.PlannedPaths{
		{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"},
			TargetPath: "skills/lean-tdd/SKILL.md",
		},
	}

	result := paths.PathsFor(domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "missing-skill"})

	if result != nil {
		t.Errorf("PathsFor for unknown ref returned %v, want nil", result)
	}
}

// TestPlannedPaths_PathsFor_PreservesEnumerationOrder verifies that the slice returned by
// PathsFor preserves the order in which entries appear in PlannedPaths. The i-th element
// of PathsFor(ref) must correspond to the i-th file in skill.Files because
// EnumerateTargetPaths iterates skill.Files in order.
func TestPlannedPaths_PathsFor_PreservesEnumerationOrder(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}
	paths := plan.PlannedPaths{
		{Ref: ref, TargetPath: "skills/lean-tdd/SKILL.md"},       // position 0
		{Ref: ref, TargetPath: "skills/lean-tdd/examples/a.md"},  // position 1
		{Ref: ref, TargetPath: "skills/lean-tdd/examples/b.md"},  // position 2
	}

	got := paths.PathsFor(ref)

	want := []string{
		"skills/lean-tdd/SKILL.md",
		"skills/lean-tdd/examples/a.md",
		"skills/lean-tdd/examples/b.md",
	}
	if len(got) != len(want) {
		t.Fatalf("PathsFor returned %d paths, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("PathsFor[%d] = %q, want %q (order must match PlannedPaths slice order)",
				i, got[i], want[i])
		}
	}
}

// TestPlannedPaths_PathsFor_EmptyPlannedPaths_ReturnsNil verifies that PathsFor on an
// empty PlannedPaths returns nil for any ref.
func TestPlannedPaths_PathsFor_EmptyPlannedPaths_ReturnsNil(t *testing.T) {
	paths := plan.PlannedPaths{}
	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}

	result := paths.PathsFor(ref)

	if result != nil {
		t.Errorf("PathsFor on empty PlannedPaths returned %v, want nil", result)
	}
}

// TestPlannedPaths_PathsFor_SingleEntryRef_ReturnsSingleElementSlice verifies that PathsFor
// returns a single-element slice (not nil) when exactly one PlannedPath matches the ref.
// This covers the 1:1 case (agents, single-file skills) where PathsFor is equivalent to Path.
func TestPlannedPaths_PathsFor_SingleEntryRef_ReturnsSingleElementSlice(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "worker"}
	paths := plan.PlannedPaths{
		{Ref: ref, TargetPath: "agents/worker.md"},
	}

	got := paths.PathsFor(ref)

	if len(got) != 1 {
		t.Fatalf("PathsFor returned %d paths for single-entry ref, want 1: %v", len(got), got)
	}
	if got[0] != "agents/worker.md" {
		t.Errorf("PathsFor[0] = %q, want %q", got[0], "agents/worker.md")
	}
}
