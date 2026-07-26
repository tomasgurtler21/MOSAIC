package catalog_test

// Tests for skill enumeration against the real repository tree.
//
// Skills live under Agents/Generic/Skills/{key}/SKILL.md. Two skills have companion
// files that must also be included in the skill's Files list:
//   - lean-tdd: EXAMPLES-CSHARP.md
//   - pr-scope-filtering: CONTEXT-ZONE.md
//
// The catalog must:
//   - Enumerate all 4 skills
//   - Expose the version, key, name, and description from SKILL.md frontmatter
//   - Include every file in the skill folder in the Files list, relative to SourceDir
//   - Set EntryFile to "SKILL.md"

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Enumeration
// ---------------------------------------------------------------------------

// TestSkills_Count_Matches4 verifies that the catalog returns exactly 4 skills,
// matching the known count in the repository.
func TestSkills_Count_Matches4(t *testing.T) {
	const wantCount = 4
	cat := loadRealCatalog(t)
	skills := cat.Skills()
	if len(skills) != wantCount {
		var keys []string
		for _, s := range skills {
			keys = append(keys, s.Key)
		}
		t.Errorf("Skills() returned %d skills, want %d (got: %v)", len(skills), wantCount, keys)
	}
}

// TestSkills_AllFourKnownSkillsPresent verifies that the four known skill folder names
// are all present in the catalog.
func TestSkills_AllFourKnownSkillsPresent(t *testing.T) {
	knownKeys := []string{
		"efficient-file-reading",
		"git-read-commands",
		"lean-tdd",
		"pr-scope-filtering",
	}
	cat := loadRealCatalog(t)
	for _, key := range knownKeys {
		if _, ok := cat.Skill(key); !ok {
			t.Errorf("Skill(%q): returned not-found; expected this skill to be present", key)
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

// TestSkill_LeanTDD_HasCompanionFile verifies that the lean-tdd skill includes
// EXAMPLES-CSHARP.md in its Files list. This file is a companion to SKILL.md and
// must be catalogued as part of the skill.
func TestSkill_LeanTDD_HasCompanionFile(t *testing.T) {
	cat := loadRealCatalog(t)
	s, ok := cat.Skill("lean-tdd")
	if !ok {
		t.Fatal("Skill(\"lean-tdd\"): not found")
	}

	const companionFile = "EXAMPLES-CSHARP.md"
	found := false
	for _, f := range s.Files {
		if f == companionFile {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("lean-tdd skill does not include companion file %q in Files: %v", companionFile, s.Files)
	}
}

// TestSkill_PrScopeFiltering_HasCompanionFile verifies that the pr-scope-filtering skill
// includes CONTEXT-ZONE.md in its Files list.
func TestSkill_PrScopeFiltering_HasCompanionFile(t *testing.T) {
	cat := loadRealCatalog(t)
	s, ok := cat.Skill("pr-scope-filtering")
	if !ok {
		t.Fatal("Skill(\"pr-scope-filtering\"): not found")
	}

	const companionFile = "CONTEXT-ZONE.md"
	found := false
	for _, f := range s.Files {
		if f == companionFile {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("pr-scope-filtering skill does not include companion file %q in Files: %v", companionFile, s.Files)
	}
}

// TestSkill_LeanTDD_FilesCount verifies that lean-tdd has exactly 2 files (SKILL.md +
// EXAMPLES-CSHARP.md). If extra files are added to the folder, this test will catch them.
func TestSkill_LeanTDD_FilesCount(t *testing.T) {
	const wantCount = 2
	cat := loadRealCatalog(t)
	s, ok := cat.Skill("lean-tdd")
	if !ok {
		t.Fatal("Skill(\"lean-tdd\"): not found")
	}
	if len(s.Files) != wantCount {
		t.Errorf("lean-tdd Files count = %d, want %d: %v", len(s.Files), wantCount, s.Files)
	}
}

// TestSkill_PrScopeFiltering_FilesCount verifies that pr-scope-filtering has exactly 2 files
// (SKILL.md + CONTEXT-ZONE.md).
func TestSkill_PrScopeFiltering_FilesCount(t *testing.T) {
	const wantCount = 2
	cat := loadRealCatalog(t)
	s, ok := cat.Skill("pr-scope-filtering")
	if !ok {
		t.Fatal("Skill(\"pr-scope-filtering\"): not found")
	}
	if len(s.Files) != wantCount {
		t.Errorf("pr-scope-filtering Files count = %d, want %d: %v", len(s.Files), wantCount, s.Files)
	}
}

// TestSkill_SkillsWithoutCompanionFiles_HaveExactlyOneFile verifies that skills without
// companion files (efficient-file-reading, git-read-commands) have exactly 1 file (SKILL.md).
func TestSkill_SkillsWithoutCompanionFiles_HaveExactlyOneFile(t *testing.T) {
	singleFileSkills := []string{"efficient-file-reading", "git-read-commands"}
	cat := loadRealCatalog(t)
	for _, key := range singleFileSkills {
		s, ok := cat.Skill(key)
		if !ok {
			t.Errorf("Skill(%q): not found", key)
			continue
		}
		if len(s.Files) != 1 {
			t.Errorf("skill %q should have 1 file (SKILL.md only), got %d: %v", key, len(s.Files), s.Files)
		}
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
