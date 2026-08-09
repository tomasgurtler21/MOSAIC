package catalog_test

// Tests for skill enumeration against the real repository tree.
//
// Skills live under Agents/Generic/Skills/{key}/SKILL.md. Two skills have companion
// files that must also be included in the skill's Files list:
//   - lean-tdd: EXAMPLES-CSHARP.md
//   - pr-scope-filtering: CONTEXT-ZONE.md
//
// The catalog must:
//   - Enumerate every skill folder present on disk
//   - Expose the version, key, name, and description from SKILL.md frontmatter
//   - Include every file in the skill folder in the Files list, relative to SourceDir
//   - Set EntryFile to "SKILL.md"
//
// Classification record: the exact skill count (was TestSkills_Count_Matches4) and the
// four-known-skill-keys allowlist (was TestSkills_AllFourKnownSkillsPresent) were
// content-coupled and are replaced by TestSkills_MatchOnDiskSkillFolders, a structural
// invariant comparing Skills() against a direct directory listing of
// Agents/Generic/Skills/. It detects a skill folder silently failing to load, or Skills()
// returning an entry with no backing folder, without naming any skill.
//
// The two exact companion-file counts (were TestSkill_LeanTDD_FilesCount and
// TestSkill_PrScopeFiltering_FilesCount) were content-coupled and are replaced by
// TestSkill_FilesCount_MatchesFolderContents on a synthetic fixture, preserving the same
// detection power (extra or missing files in a folder are still caught).
//
// Beyond this stage's explicit item list, but within AC7.2's scope: the two companion-file
// spot-checks and the single-file-skills check also named specific real skill keys
// (lean-tdd, pr-scope-filtering, efficient-file-reading, git-read-commands). They are
// replaced by TestSkill_Files_IncludesCompanionFilesByName and
// TestSkill_SkillWithoutCompanionFiles_HasExactlyOneFile on synthetic fixtures, preserving
// the same assertions.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// ---------------------------------------------------------------------------
// Enumeration
// ---------------------------------------------------------------------------

// TestSkills_MatchOnDiskSkillFolders verifies that Skills() returns exactly one entry
// per subdirectory of the real repository's Agents/Generic/Skills/ folder — no fewer
// (a folder failed to load) and no more (a phantom entry with no backing folder).
func TestSkills_MatchOnDiskSkillFolders(t *testing.T) {
	cat := loadRealCatalog(t)
	skillsDir := filepath.Join(cat.Root(), "Agents", "Generic", "Skills")

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("os.ReadDir(%q): %v", skillsDir, err)
	}
	onDisk := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() {
			onDisk[e.Name()] = true
		}
	}
	if len(onDisk) == 0 {
		t.Fatal("no skill folders found on disk under Agents/Generic/Skills/; cannot verify the invariant")
	}

	skills := cat.Skills()
	inCatalog := make(map[string]bool, len(skills))
	for _, s := range skills {
		inCatalog[s.Key] = true
	}

	for key := range onDisk {
		if !inCatalog[key] {
			t.Errorf("skill folder %q exists on disk but Skills() has no matching entry", key)
		}
	}
	for key := range inCatalog {
		if !onDisk[key] {
			t.Errorf("Skills() contains key %q with no matching on-disk folder under %q", key, skillsDir)
		}
	}
}

// ---------------------------------------------------------------------------
// Fields
// ---------------------------------------------------------------------------

// TestSkills_AllHaveNonEmptyKey verifies that every skill has a non-empty key.
func TestSkills_AllHaveNonEmptyKey(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, s := range cat.Skills() {
		if s.Key == "" {
			t.Errorf("skill at SourceDir %q has empty Key", s.SourceDir)
		}
	}
}

// TestSkills_AllHaveVersion verifies that every skill has a non-empty version string.
func TestSkills_AllHaveVersion(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, s := range cat.Skills() {
		if s.Version == "" {
			t.Errorf("skill %q has empty Version", s.Key)
		}
	}
}

// TestSkills_AllHaveName verifies that every skill has a non-empty name.
func TestSkills_AllHaveName(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, s := range cat.Skills() {
		if s.Name == "" {
			t.Errorf("skill %q has empty Name", s.Key)
		}
	}
}

// TestSkills_AllHaveDescription verifies that every skill has a non-empty description.
func TestSkills_AllHaveDescription(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, s := range cat.Skills() {
		if s.Description == "" {
			t.Errorf("skill %q has empty Description", s.Key)
		}
	}
}

// TestSkills_EntryFile_IsSKILLMd verifies that EntryFile is "SKILL.md" for every skill.
// SKILL.md is the conventional entry point file for a skill folder.
func TestSkills_EntryFile_IsSKILLMd(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, s := range cat.Skills() {
		if s.EntryFile != "SKILL.md" {
			t.Errorf("skill %q EntryFile = %q, want %q", s.Key, s.EntryFile, "SKILL.md")
		}
	}
}

// TestSkills_SourceDir_IsAbsolute verifies that SourceDir is an absolute path.
func TestSkills_SourceDir_IsAbsolute(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, s := range cat.Skills() {
		if !filepath.IsAbs(s.SourceDir) {
			t.Errorf("skill %q SourceDir is not absolute: %q", s.Key, s.SourceDir)
		}
	}
}

// TestSkills_SourceDir_Exists verifies that each skill's SourceDir exists on disk.
func TestSkills_SourceDir_Exists(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, s := range cat.Skills() {
		if _, err := os.Stat(s.SourceDir); os.IsNotExist(err) {
			t.Errorf("skill %q SourceDir %q does not exist on disk", s.Key, s.SourceDir)
		}
	}
}

// TestSkills_Files_IncludesEntryFile verifies that every skill's Files list contains
// the EntryFile value (normally "SKILL.md"). The entry file is part of the skill.
func TestSkills_Files_IncludesEntryFile(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, s := range cat.Skills() {
		found := false
		for _, f := range s.Files {
			if f == s.EntryFile {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("skill %q Files list does not contain EntryFile %q: %v",
				s.Key, s.EntryFile, s.Files)
		}
	}
}

// TestSkills_Files_AreRelativePaths verifies that every file path in Files[] is relative
// (not absolute). Paths are relative to SourceDir.
func TestSkills_Files_AreRelativePaths(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, s := range cat.Skills() {
		for _, f := range s.Files {
			if filepath.IsAbs(f) {
				t.Errorf("skill %q Files contains absolute path %q; expected relative to SourceDir", s.Key, f)
			}
		}
	}
}

// TestSkills_Files_AllExistOnDisk verifies that every file listed in a skill's Files[]
// actually exists on disk under SourceDir.
func TestSkills_Files_AllExistOnDisk(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, s := range cat.Skills() {
		for _, f := range s.Files {
			full := filepath.Join(s.SourceDir, f)
			if _, err := os.Stat(full); os.IsNotExist(err) {
				t.Errorf("skill %q file %q does not exist at expected path %q", s.Key, f, full)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Companion files
// ---------------------------------------------------------------------------

// TestSkill_Files_IncludesCompanionFilesByName verifies, on a synthetic skill folder,
// that a companion file placed alongside SKILL.md appears in the Files list by its exact
// name. This proves the same behaviour the two real-skill spot-checks proved (a companion
// file is catalogued, not just counted) without naming a specific real skill.
func TestSkill_Files_IncludesCompanionFilesByName(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillFolder(t, root, "sample-skill")
	mustWriteFile(t, root, filepath.Join("Agents", "Generic", "Skills", "sample-skill", "COMPANION.md"), []byte("# Companion\n"))

	cat, err := catalog.Load(root)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	s, ok := cat.Skill("sample-skill")
	if !ok {
		t.Fatal("Skill(\"sample-skill\"): not found")
	}

	found := false
	for _, f := range s.Files {
		if f == "COMPANION.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("sample-skill does not include companion file %q in Files: %v", "COMPANION.md", s.Files)
	}
}

// TestSkill_FilesCount_MatchesFolderContents verifies, on a synthetic skill folder with
// two companion files besides SKILL.md, that the Files list has exactly as many entries
// as files placed on disk. This is the same detection power as asserting a literal count
// on a real skill (extra or missing files are caught), without coupling to which real
// skill happens to have companion files today.
func TestSkill_FilesCount_MatchesFolderContents(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillFolder(t, root, "sample-skill")
	mustWriteFile(t, root, filepath.Join("Agents", "Generic", "Skills", "sample-skill", "COMPANION-ONE.md"), []byte("# Companion One\n"))
	mustWriteFile(t, root, filepath.Join("Agents", "Generic", "Skills", "sample-skill", "COMPANION-TWO.md"), []byte("# Companion Two\n"))

	cat, err := catalog.Load(root)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	s, ok := cat.Skill("sample-skill")
	if !ok {
		t.Fatal("Skill(\"sample-skill\"): not found")
	}
	const wantCount = 3 // SKILL.md + two companions
	if len(s.Files) != wantCount {
		t.Errorf("sample-skill Files count = %d, want %d: %v", len(s.Files), wantCount, s.Files)
	}
}

// TestSkill_SkillWithoutCompanionFiles_HasExactlyOneFile verifies, on a synthetic skill
// folder containing only SKILL.md, that Files has exactly one entry. This proves the same
// behaviour the two named-real-skill checks proved (a skill with no companion files is
// not padded with phantom entries) without naming a specific real skill.
func TestSkill_SkillWithoutCompanionFiles_HasExactlyOneFile(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSkillFolder(t, root, "sample-skill")

	cat, err := catalog.Load(root)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	s, ok := cat.Skill("sample-skill")
	if !ok {
		t.Fatal("Skill(\"sample-skill\"): not found")
	}
	if len(s.Files) != 1 {
		t.Errorf("sample-skill should have 1 file (SKILL.md only), got %d: %v", len(s.Files), s.Files)
	}
}

// ---------------------------------------------------------------------------
// Lookup
// ---------------------------------------------------------------------------

// TestSkillLookup_AllSkills_FoundByKey verifies that every skill returned by Skills()
// can be looked up by key via Skill(key). This confirms the lookup index is consistent.
func TestSkillLookup_AllSkills_FoundByKey(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, s := range cat.Skills() {
		found, ok := cat.Skill(s.Key)
		if !ok {
			t.Errorf("Skill(%q): returned not-found, but key was returned by Skills()", s.Key)
			continue
		}
		if found.Key != s.Key {
			t.Errorf("Skill(%q).Key = %q, want %q", s.Key, found.Key, s.Key)
		}
	}
}

// TestSkillLookup_UnknownKey_ReturnsFalse verifies that Skill(key) returns false for
// a key that does not exist.
func TestSkillLookup_UnknownKey_ReturnsFalse(t *testing.T) {
	cat := loadRealCatalog(t)
	_, ok := cat.Skill("this-skill-does-not-exist")
	if ok {
		t.Error("Skill(\"this-skill-does-not-exist\"): returned true, want false")
	}
}

// TestSkill_KeyMatchesFolderName verifies that each skill's Key matches the folder name
// that contains SKILL.md. The key is the folder name by convention.
func TestSkill_KeyMatchesFolderName(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, s := range cat.Skills() {
		folderName := filepath.Base(s.SourceDir)
		if folderName != s.Key {
			t.Errorf("skill Key %q does not match folder name %q from SourceDir %q",
				s.Key, folderName, s.SourceDir)
		}
	}
}
