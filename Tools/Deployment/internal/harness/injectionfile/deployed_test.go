package injectionfile_test

// Tests for ParseDeployedRegions, ParseDeployedRegionsWithVersion, and LoadDir.
//
// These tests specify the behaviour of the run-time harness content parsing layer that
// replaces the old ParseInjections / ParseInjectionsWithVersion functions. Harness content
// files use [[DEPLOYED:Name]] markers after the Stage 8 migration; this parser reads those
// markers.
//
// Coverage:
//
//   ParseDeployedRegions:
//   - A well-formed file with multiple [[DEPLOYED:Name]] regions returns a map with all
//     names as keys and their trimmed content as values.
//   - A file with no deployed regions returns a non-nil empty map with no error.
//   - A deployed region with empty content produces an entry with empty-string value,
//     distinguishable from an absent region (no key).
//   - [[INJECTION:]] and [[SECTION:]] regions are ignored — only DEPLOYED regions appear.
//   - An empty byte slice returns a non-nil empty map with no error.
//   - A malformed file (unclosed frontmatter) returns a non-nil error.
//
//   ParseDeployedRegionsWithVersion:
//   - Returns the frontmatter `version` scalar alongside the region map.
//   - Returns version "" when the frontmatter is absent.
//   - Returns version "" when the frontmatter has no `version` key.
//   - A file with no deployed regions and a version returns an empty map plus the version.
//
//   LoadDir:
//   - A directory with both HarnessInjections.md and HarnessInjectionsOrchestrator.md
//     returns a populated HarnessContent with Shared, Orchestrator, and OrchestratorVersion.
//   - A missing HarnessInjections.md returns a non-nil error naming the file.
//   - A missing HarnessInjectionsOrchestrator.md returns a non-nil error naming the file.
//   - An unparseable HarnessInjections.md returns a non-nil error naming the file.
//   - An unparseable HarnessInjectionsOrchestrator.md returns a non-nil error naming the file.
//   - OrchestratorVersion in the returned HarnessContent matches the `version` in
//     HarnessInjectionsOrchestrator.md's frontmatter.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/harness/injectionfile"
)

// ---------------------------------------------------------------------------
// Fixtures: harness content files using [[DEPLOYED:Name]] markers
// ---------------------------------------------------------------------------

// deployedMultiRegionFixture is a well-formed HarnessInjections.md with three DEPLOYED regions.
const deployedMultiRegionFixture = `---
version: "1.0.0"
---
# Harness Injections

[[DEPLOYED:HarnessConstraints]]
Do not invoke external APIs without explicit permission.
Check tool availability before each use.
[[/DEPLOYED:HarnessConstraints]]

[[DEPLOYED:LanguagePatterns]]
Use idiomatic Go patterns.
Prefer table-driven tests.
[[/DEPLOYED:LanguagePatterns]]

[[DEPLOYED:CustomConstraints]]
Project-specific rules apply.
[[/DEPLOYED:CustomConstraints]]
`

// deployedEmptyRegionFixture has one DEPLOYED region with no content between its tags.
// Used to verify that "declared but empty" produces a map entry with value "".
const deployedEmptyRegionFixture = `# Harness Injections

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
`

// deployedNoRegionsFixture is a valid Markdown file with no DEPLOYED regions at all.
const deployedNoRegionsFixture = `# Harness Injections

This harness has no content regions.
`

// deployedMixedMarkersFixture has all three marker kinds. Only DEPLOYED regions should appear
// in the result.
const deployedMixedMarkersFixture = `[[SECTION:Identity]]
Identity content here.
[[INJECTION:IdentityExtension]]
Project-authored identity content.
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[DEPLOYED:HarnessConstraints]]
Harness constraint from DEPLOYED marker.
[[/DEPLOYED:HarnessConstraints]]

[[DEPLOYED:LanguagePatterns]]
Language patterns from DEPLOYED marker.
[[/DEPLOYED:LanguagePatterns]]
`

// deployedWithVersionFixture has frontmatter with a version key and two DEPLOYED regions.
const deployedWithVersionFixture = `---
version: "2.3.1"
harness: test-harness
---
# Harness Injections

[[DEPLOYED:HarnessConstraints]]
Some constraint.
[[/DEPLOYED:HarnessConstraints]]
`

// deployedNoFrontmatterFixture has DEPLOYED regions but no frontmatter at all.
const deployedNoFrontmatterFixture = `# Harness Injections

[[DEPLOYED:LanguagePatterns]]
Some language patterns.
[[/DEPLOYED:LanguagePatterns]]
`

// deployedFrontmatterNoVersionFixture has frontmatter but no `version` key.
const deployedFrontmatterNoVersionFixture = `---
harness: no-version-harness
---
# Harness Injections

[[DEPLOYED:HarnessConstraints]]
Some constraint.
[[/DEPLOYED:HarnessConstraints]]
`

// malformedUnclosedFrontmatter causes docformat.Parse to return an error.
const malformedUnclosedFrontmatter = `---
harness: test
version: "1.0"
`

// ---------------------------------------------------------------------------
// Tests: ParseDeployedRegions — multiple regions
// ---------------------------------------------------------------------------

// TestParseDeployedRegions_MultipleRegions_AllNamesPresent verifies that all three DEPLOYED
// region names appear in the returned map.
func TestParseDeployedRegions_MultipleRegions_AllNamesPresent(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(deployedMultiRegionFixture))
	if err != nil {
		t.Fatalf("ParseDeployedRegions returned unexpected error: %v", err) // RED: fails until I6.1 implemented
	}
	wantNames := []string{"HarnessConstraints", "LanguagePatterns", "CustomConstraints"}
	for _, name := range wantNames {
		if _, ok := result[name]; !ok {
			t.Errorf("map is missing expected region name %q", name)
		}
	}
}

// TestParseDeployedRegions_MultipleRegions_MapSizeMatchesCount verifies the map has exactly
// as many entries as there are DEPLOYED regions in the file.
func TestParseDeployedRegions_MultipleRegions_MapSizeMatchesCount(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(deployedMultiRegionFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const wantCount = 3
	if len(result) != wantCount {
		t.Errorf("map size: want %d, got %d (entries: %v)", wantCount, len(result), result)
	}
}

// TestParseDeployedRegions_MultipleRegions_ContentCorrect verifies the content for one region.
func TestParseDeployedRegions_MultipleRegions_ContentCorrect(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(deployedMultiRegionFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, ok := result["HarnessConstraints"]
	if !ok {
		t.Fatal("map does not contain HarnessConstraints")
	}
	if !strings.Contains(content, "Do not invoke external APIs without explicit permission.") {
		t.Errorf("HarnessConstraints content missing expected text; got: %q", content)
	}
	if !strings.Contains(content, "Check tool availability before each use.") {
		t.Errorf("HarnessConstraints content missing expected text; got: %q", content)
	}
}

// ---------------------------------------------------------------------------
// Tests: ParseDeployedRegions — no regions
// ---------------------------------------------------------------------------

// TestParseDeployedRegions_NoRegions_ReturnsNilError verifies that a file with no DEPLOYED
// regions is valid and returns no error.
func TestParseDeployedRegions_NoRegions_ReturnsNilError(t *testing.T) {
	_, err := injectionfile.ParseDeployedRegions([]byte(deployedNoRegionsFixture))
	if err != nil {
		t.Errorf("expected nil error for valid file with no deployed regions, got: %v", err)
	}
}

// TestParseDeployedRegions_NoRegions_ReturnsNonNilEmptyMap verifies that a file with no
// DEPLOYED regions returns a non-nil, empty map.
func TestParseDeployedRegions_NoRegions_ReturnsNonNilEmptyMap(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(deployedNoRegionsFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil map for file with no deployed regions, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries: %v", len(result), result)
	}
}

// ---------------------------------------------------------------------------
// Tests: ParseDeployedRegions — empty region content
// ---------------------------------------------------------------------------

// TestParseDeployedRegions_EmptyContentRegion_KeyPresentInMap verifies that a DEPLOYED region
// with empty content between its tags produces a map entry (declared-but-empty).
func TestParseDeployedRegions_EmptyContentRegion_KeyPresentInMap(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(deployedEmptyRegionFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["HarnessConstraints"]; !ok {
		t.Error("map does not contain HarnessConstraints; declared-but-empty DEPLOYED regions must produce a map entry")
	}
}

// TestParseDeployedRegions_EmptyContentRegion_ValueIsEmptyString verifies the value for a
// declared-but-empty DEPLOYED region is "".
func TestParseDeployedRegions_EmptyContentRegion_ValueIsEmptyString(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(deployedEmptyRegionFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, ok := result["HarnessConstraints"]
	if !ok {
		t.Fatal("map does not contain HarnessConstraints")
	}
	if content != "" {
		t.Errorf("expected empty string for declared-but-empty region, got: %q", content)
	}
}

// TestParseDeployedRegions_EmptyContentRegion_AbsentRegionHasNoKey verifies that a region
// name not declared at all produces NO key in the map, distinguishing absent from empty.
func TestParseDeployedRegions_EmptyContentRegion_AbsentRegionHasNoKey(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(deployedEmptyRegionFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "LanguagePatterns" is not declared in deployedEmptyRegionFixture at all.
	if _, ok := result["LanguagePatterns"]; ok {
		t.Error("map must NOT contain LanguagePatterns; it is not declared in the fixture")
	}
}

// ---------------------------------------------------------------------------
// Tests: ParseDeployedRegions — ignores non-DEPLOYED markers
// ---------------------------------------------------------------------------

// TestParseDeployedRegions_MixedMarkers_OnlyDeployedRegionsReturned verifies that
// [[INJECTION:]] and [[SECTION:]] regions are silently ignored.
func TestParseDeployedRegions_MixedMarkers_OnlyDeployedRegionsReturned(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(deployedMixedMarkersFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the two DEPLOYED regions should appear.
	if len(result) != 2 {
		t.Errorf("expected exactly 2 entries (DEPLOYED regions only), got %d: %v", len(result), result)
	}
	if _, ok := result["HarnessConstraints"]; !ok {
		t.Error("map is missing HarnessConstraints (a DEPLOYED region)")
	}
	if _, ok := result["LanguagePatterns"]; !ok {
		t.Error("map is missing LanguagePatterns (a DEPLOYED region)")
	}
	// INJECTION and SECTION regions must NOT appear.
	if _, ok := result["IdentityExtension"]; ok {
		t.Error("map must not contain IdentityExtension (it is an INJECTION region, not DEPLOYED)")
	}
	if _, ok := result["Identity"]; ok {
		t.Error("map must not contain Identity (it is a SECTION region, not DEPLOYED)")
	}
}

// ---------------------------------------------------------------------------
// Tests: ParseDeployedRegions — empty input
// ---------------------------------------------------------------------------

// TestParseDeployedRegions_EmptyInput_ReturnsNilError verifies that an empty byte slice
// is treated as a valid (empty) file.
func TestParseDeployedRegions_EmptyInput_ReturnsNilError(t *testing.T) {
	_, err := injectionfile.ParseDeployedRegions([]byte{})
	if err != nil {
		t.Errorf("expected nil error for empty input, got: %v", err)
	}
}

// TestParseDeployedRegions_EmptyInput_ReturnsNonNilEmptyMap verifies the returned map is
// non-nil and empty for empty input.
func TestParseDeployedRegions_EmptyInput_ReturnsNonNilEmptyMap(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil map for empty input, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries: %v", len(result), result)
	}
}

// ---------------------------------------------------------------------------
// Tests: ParseDeployedRegions — malformed input
// ---------------------------------------------------------------------------

// TestParseDeployedRegions_MalformedFrontmatter_ReturnsError verifies that a file that
// docformat cannot parse yields a non-nil error.
func TestParseDeployedRegions_MalformedFrontmatter_ReturnsError(t *testing.T) {
	_, err := injectionfile.ParseDeployedRegions([]byte(malformedUnclosedFrontmatter))
	if err == nil {
		t.Error("expected non-nil error for input with unclosed frontmatter block, got nil")
	}
}

// TestParseDeployedRegions_UnclosedRegion_ReturnsError verifies that a [[DEPLOYED:Name]]
// region that is opened but never closed is treated as malformed by ParseDeployedRegions.
// This pins the public API surface that built-in harnesses depend on, independent of whether
// the underlying docformat parser also rejects unclosed regions in its own suite.
func TestParseDeployedRegions_UnclosedRegion_ReturnsError(t *testing.T) {
	const src = `# Harness Injections

[[DEPLOYED:HarnessConstraints]]
Content without a closing tag.
`
	_, err := injectionfile.ParseDeployedRegions([]byte(src))
	if err == nil {
		t.Error("expected non-nil error for [[DEPLOYED:]] region opened but never closed, got nil")
	}
}

// TestParseDeployedRegions_DuplicateRegionName_ReturnsError verifies that two [[DEPLOYED:]]
// regions with the same name in a single file are treated as malformed. The design states
// "Each boundary name appears at most once per document, across all three kinds."
func TestParseDeployedRegions_DuplicateRegionName_ReturnsError(t *testing.T) {
	const src = `# Harness Injections

[[DEPLOYED:HarnessConstraints]]
First declaration.
[[/DEPLOYED:HarnessConstraints]]

[[DEPLOYED:HarnessConstraints]]
Second declaration with same name — duplicate.
[[/DEPLOYED:HarnessConstraints]]
`
	_, err := injectionfile.ParseDeployedRegions([]byte(src))
	if err == nil {
		t.Error("expected non-nil error for duplicate [[DEPLOYED:HarnessConstraints]] region name, got nil; " +
			"the design requires each boundary name to appear at most once per document")
	}
}

// ---------------------------------------------------------------------------
// Tests: ParseDeployedRegionsWithVersion
// ---------------------------------------------------------------------------

// TestParseDeployedRegionsWithVersion_ReturnsVersion verifies the frontmatter version scalar
// is returned alongside the region map.
func TestParseDeployedRegionsWithVersion_ReturnsVersion(t *testing.T) {
	_, version, err := injectionfile.ParseDeployedRegionsWithVersion([]byte(deployedWithVersionFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "2.3.1"
	if version != want {
		t.Errorf("version: want %q, got %q", want, version)
	}
}

// TestParseDeployedRegionsWithVersion_NoFrontmatter_VersionIsEmpty verifies that when the
// file has no frontmatter at all, the returned version is "".
func TestParseDeployedRegionsWithVersion_NoFrontmatter_VersionIsEmpty(t *testing.T) {
	_, version, err := injectionfile.ParseDeployedRegionsWithVersion([]byte(deployedNoFrontmatterFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "" {
		t.Errorf("expected empty version for file without frontmatter, got: %q", version)
	}
}

// TestParseDeployedRegionsWithVersion_FrontmatterWithoutVersionKey_VersionIsEmpty verifies
// that frontmatter without a `version` key returns version "".
func TestParseDeployedRegionsWithVersion_FrontmatterWithoutVersionKey_VersionIsEmpty(t *testing.T) {
	_, version, err := injectionfile.ParseDeployedRegionsWithVersion([]byte(deployedFrontmatterNoVersionFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "" {
		t.Errorf("expected empty version for frontmatter with no `version` key, got: %q", version)
	}
}

// TestParseDeployedRegionsWithVersion_RegionsAlsoReturned verifies that the region map is
// correctly populated alongside the version.
func TestParseDeployedRegionsWithVersion_RegionsAlsoReturned(t *testing.T) {
	result, version, err := injectionfile.ParseDeployedRegionsWithVersion([]byte(deployedWithVersionFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "2.3.1" {
		t.Errorf("version: want %q, got %q", "2.3.1", version)
	}
	if _, ok := result["HarnessConstraints"]; !ok {
		t.Error("map is missing HarnessConstraints")
	}
	if len(result) != 1 {
		t.Errorf("expected exactly 1 region, got %d: %v", len(result), result)
	}
}

// TestParseDeployedRegionsWithVersion_NoRegionsWithVersion_EmptyMapAndVersion verifies that
// a file with a version but no DEPLOYED regions returns an empty map and the version.
func TestParseDeployedRegionsWithVersion_NoRegionsWithVersion_EmptyMapAndVersion(t *testing.T) {
	src := `---
version: "3.0.0"
---
# No regions here.
`
	result, version, err := injectionfile.ParseDeployedRegionsWithVersion([]byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil map, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries: %v", len(result), result)
	}
	if version != "3.0.0" {
		t.Errorf("version: want %q, got %q", "3.0.0", version)
	}
}

// ---------------------------------------------------------------------------
// Tests: LoadDir — success path
// ---------------------------------------------------------------------------

// writeContentFile writes a harness content file to dir/<filename> with the given src bytes.
func writeContentFile(t *testing.T, dir, filename string, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(src), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
}

// TestLoadDir_BothFilesPresent_PopulatesHarnessContent verifies that LoadDir reads both
// HarnessInjections.md and HarnessInjectionsOrchestrator.md from the given directory and
// returns a populated HarnessContent.
func TestLoadDir_BothFilesPresent_PopulatesHarnessContent(t *testing.T) {
	dir := t.TempDir()
	writeContentFile(t, dir, injectionfile.SharedFileName, `[[DEPLOYED:LanguagePatterns]]
Use Go idioms.
[[/DEPLOYED:LanguagePatterns]]

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
`)
	writeContentFile(t, dir, injectionfile.OrchestratorFileName, `---
version: "1.2.3"
---
[[DEPLOYED:HarnessConstraints]]
Orchestrator-only constraint.
[[/DEPLOYED:HarnessConstraints]]
`)

	content, err := injectionfile.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir returned unexpected error: %v", err) // RED: fails until I6.1/I6.5 implemented
	}

	// Shared regions.
	if _, ok := content.Shared["LanguagePatterns"]; !ok {
		t.Error("Shared map is missing LanguagePatterns")
	}
	if _, ok := content.Shared["HarnessConstraints"]; !ok {
		t.Error("Shared map is missing HarnessConstraints (declared-but-empty)")
	}
	if content.Shared["LanguagePatterns"] == "" {
		t.Error("Shared LanguagePatterns should not be empty in this fixture")
	}
	if content.Shared["HarnessConstraints"] != "" {
		t.Errorf("Shared HarnessConstraints should be empty (declared-but-empty), got: %q", content.Shared["HarnessConstraints"])
	}

	// Orchestrator regions.
	if _, ok := content.Orchestrator["HarnessConstraints"]; !ok {
		t.Error("Orchestrator map is missing HarnessConstraints")
	}
	if !strings.Contains(content.Orchestrator["HarnessConstraints"], "Orchestrator-only constraint.") {
		t.Errorf("Orchestrator HarnessConstraints missing expected text; got: %q", content.Orchestrator["HarnessConstraints"])
	}

	// OrchestratorVersion from frontmatter.
	if content.OrchestratorVersion != "1.2.3" {
		t.Errorf("OrchestratorVersion: want %q, got %q", "1.2.3", content.OrchestratorVersion)
	}
}

// TestLoadDir_SharedContent_EmptyOrchestratorVersion_WhenNoFrontmatterVersion verifies that
// when HarnessInjectionsOrchestrator.md has no frontmatter version, OrchestratorVersion is "".
func TestLoadDir_SharedContent_EmptyOrchestratorVersion_WhenNoFrontmatterVersion(t *testing.T) {
	dir := t.TempDir()
	writeContentFile(t, dir, injectionfile.SharedFileName, `[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
`)
	writeContentFile(t, dir, injectionfile.OrchestratorFileName, `# No frontmatter, no version.

[[DEPLOYED:HarnessConstraints]]
Orch content.
[[/DEPLOYED:HarnessConstraints]]
`)

	content, err := injectionfile.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir returned unexpected error: %v", err)
	}
	if content.OrchestratorVersion != "" {
		t.Errorf("OrchestratorVersion: want empty, got %q", content.OrchestratorVersion)
	}
}

// ---------------------------------------------------------------------------
// Tests: LoadDir — failure modes (missing files)
// ---------------------------------------------------------------------------

// TestLoadDir_MissingSharedFile_ReturnsError verifies that LoadDir fails with a non-nil error
// when HarnessInjections.md is absent.
func TestLoadDir_MissingSharedFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	// Only write the orchestrator file; shared file is absent.
	writeContentFile(t, dir, injectionfile.OrchestratorFileName, `[[DEPLOYED:HarnessConstraints]]
Orch content.
[[/DEPLOYED:HarnessConstraints]]
`)

	_, err := injectionfile.LoadDir(dir)
	if err == nil {
		t.Error("expected non-nil error when HarnessInjections.md is absent, got nil")
	}
}

// TestLoadDir_MissingSharedFile_ErrorNamesFile verifies that the error for a missing
// HarnessInjections.md names the missing file so the user can diagnose the problem.
func TestLoadDir_MissingSharedFile_ErrorNamesFile(t *testing.T) {
	dir := t.TempDir()
	writeContentFile(t, dir, injectionfile.OrchestratorFileName, `[[DEPLOYED:HarnessConstraints]]
Orch.
[[/DEPLOYED:HarnessConstraints]]
`)

	_, err := injectionfile.LoadDir(dir)
	if err == nil {
		t.Fatal("expected non-nil error when HarnessInjections.md is absent")
	}
	if !strings.Contains(err.Error(), injectionfile.SharedFileName) &&
		!strings.Contains(err.Error(), "HarnessInjections") {
		t.Errorf("error does not name the missing file; got: %q", err.Error())
	}
}

// TestLoadDir_MissingOrchestratorFile_ReturnsError verifies that LoadDir fails with a
// non-nil error when HarnessInjectionsOrchestrator.md is absent.
func TestLoadDir_MissingOrchestratorFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	// Only write the shared file; orchestrator file is absent.
	writeContentFile(t, dir, injectionfile.SharedFileName, `[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
`)

	_, err := injectionfile.LoadDir(dir)
	if err == nil {
		t.Error("expected non-nil error when HarnessInjectionsOrchestrator.md is absent, got nil")
	}
}

// TestLoadDir_MissingOrchestratorFile_ErrorNamesFile verifies that the error for a missing
// HarnessInjectionsOrchestrator.md names the missing file.
func TestLoadDir_MissingOrchestratorFile_ErrorNamesFile(t *testing.T) {
	dir := t.TempDir()
	writeContentFile(t, dir, injectionfile.SharedFileName, `[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
`)

	_, err := injectionfile.LoadDir(dir)
	if err == nil {
		t.Fatal("expected non-nil error when HarnessInjectionsOrchestrator.md is absent")
	}
	if !strings.Contains(err.Error(), injectionfile.OrchestratorFileName) &&
		!strings.Contains(err.Error(), "HarnessInjectionsOrchestrator") {
		t.Errorf("error does not name the missing file; got: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests: LoadDir — failure modes (unparseable files)
// ---------------------------------------------------------------------------

// TestLoadDir_UnparseableSharedFile_ReturnsError verifies that an unparseable
// HarnessInjections.md causes LoadDir to return a non-nil error.
func TestLoadDir_UnparseableSharedFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeContentFile(t, dir, injectionfile.SharedFileName, malformedUnclosedFrontmatter)
	writeContentFile(t, dir, injectionfile.OrchestratorFileName, `[[DEPLOYED:HarnessConstraints]]
Orch.
[[/DEPLOYED:HarnessConstraints]]
`)

	_, err := injectionfile.LoadDir(dir)
	if err == nil {
		t.Error("expected non-nil error for unparseable HarnessInjections.md, got nil")
	}
}

// TestLoadDir_UnparseableSharedFile_ErrorNamesFile verifies that the error for an
// unparseable HarnessInjections.md names the file.
func TestLoadDir_UnparseableSharedFile_ErrorNamesFile(t *testing.T) {
	dir := t.TempDir()
	writeContentFile(t, dir, injectionfile.SharedFileName, malformedUnclosedFrontmatter)
	writeContentFile(t, dir, injectionfile.OrchestratorFileName, `[[DEPLOYED:HarnessConstraints]]
Orch.
[[/DEPLOYED:HarnessConstraints]]
`)

	_, err := injectionfile.LoadDir(dir)
	if err == nil {
		t.Fatal("expected non-nil error for unparseable HarnessInjections.md")
	}
	if !strings.Contains(err.Error(), injectionfile.SharedFileName) &&
		!strings.Contains(err.Error(), "HarnessInjections") {
		t.Errorf("error does not name the unparseable file; got: %q", err.Error())
	}
}

// TestLoadDir_UnparseableOrchestratorFile_ReturnsError verifies that an unparseable
// HarnessInjectionsOrchestrator.md causes LoadDir to return a non-nil error.
func TestLoadDir_UnparseableOrchestratorFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeContentFile(t, dir, injectionfile.SharedFileName, `[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
`)
	writeContentFile(t, dir, injectionfile.OrchestratorFileName, malformedUnclosedFrontmatter)

	_, err := injectionfile.LoadDir(dir)
	if err == nil {
		t.Error("expected non-nil error for unparseable HarnessInjectionsOrchestrator.md, got nil")
	}
}

// TestLoadDir_UnparseableOrchestratorFile_ErrorNamesFile verifies that the error for an
// unparseable HarnessInjectionsOrchestrator.md names the file.
func TestLoadDir_UnparseableOrchestratorFile_ErrorNamesFile(t *testing.T) {
	dir := t.TempDir()
	writeContentFile(t, dir, injectionfile.SharedFileName, `[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
`)
	writeContentFile(t, dir, injectionfile.OrchestratorFileName, malformedUnclosedFrontmatter)

	_, err := injectionfile.LoadDir(dir)
	if err == nil {
		t.Fatal("expected non-nil error for unparseable HarnessInjectionsOrchestrator.md")
	}
	if !strings.Contains(err.Error(), injectionfile.OrchestratorFileName) &&
		!strings.Contains(err.Error(), "HarnessInjectionsOrchestrator") {
		t.Errorf("error does not name the unparseable file; got: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests: ParseDeployedRegions — CRLF line endings normalised to LF
//
// These tests construct CRLF input directly (replacing every \n with \r\n) so
// that the behaviour is reproducible on any machine regardless of checkout
// settings. They specify that ParseDeployedRegions must return content that is
// byte-for-byte identical whether the source file uses CRLF or LF endings.
//
// TDD RED phase: without a normalisation step inside ParseDeployedRegionsWithVersion
// the multi-line cases will fail because strings.TrimSpace strips surrounding
// whitespace but leaves \r characters embedded in interior lines.
// ---------------------------------------------------------------------------

// crlfify replaces every LF with CRLF, simulating a file that was stored (or
// edited on Windows) with CRLF line endings. It is intentionally simple and
// only adds \r before bare \n — it does not double-convert existing \r\n pairs,
// so it is safe to call on LF-only strings.
func crlfify(s string) string {
	// Replace bare \n (not already preceded by \r) with \r\n. Since our
	// fixture strings are LF-only, a simple ReplaceAll is sufficient.
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// lfMultiLineRegionFixture is a harness content file with a single multi-line
// DEPLOYED region, stored with LF endings. crlfify produces the Windows variant.
const lfMultiLineRegionFixture = `# Harness Injections

[[DEPLOYED:HarnessConstraints]]
Do not invoke external APIs without explicit permission.
Check tool availability before each use.
Prefer read-only operations when write access is not required.
[[/DEPLOYED:HarnessConstraints]]
`

// lfMultipleRegionsFixture has two multi-line DEPLOYED regions.
const lfMultipleRegionsFixture = `# Harness Injections

[[DEPLOYED:HarnessConstraints]]
Constraint line one.
Constraint line two.
[[/DEPLOYED:HarnessConstraints]]

[[DEPLOYED:LanguagePatterns]]
Use idiomatic Go patterns.
Prefer table-driven tests over repeated test functions.
[[/DEPLOYED:LanguagePatterns]]
`

// lfWithVersionAndRegionFixture has frontmatter with a version and a multi-line region.
const lfWithVersionAndRegionFixture = `---
version: "4.0.0"
---
# Harness Injections

[[DEPLOYED:HarnessConstraints]]
First constraint.
Second constraint.
[[/DEPLOYED:HarnessConstraints]]
`

// TestParseDeployedRegions_CRLF_MultiLineContent_MatchesLF verifies that a harness content
// file stored with CRLF line endings returns the same region content as the same file stored
// with LF endings. This is the core normalisation requirement: the injected content must be
// line-ending independent.
//
// Without the CRLF→LF normalisation this test fails because strings.TrimSpace strips
// surrounding whitespace but leaves \r embedded between interior lines.
func TestParseDeployedRegions_CRLF_MultiLineContent_MatchesLF(t *testing.T) {
	lfResult, err := injectionfile.ParseDeployedRegions([]byte(lfMultiLineRegionFixture))
	if err != nil {
		t.Fatalf("ParseDeployedRegions (LF) returned unexpected error: %v", err)
	}
	crlfResult, err := injectionfile.ParseDeployedRegions([]byte(crlfify(lfMultiLineRegionFixture)))
	if err != nil {
		t.Fatalf("ParseDeployedRegions (CRLF) returned unexpected error: %v", err)
	}
	lfContent, ok := lfResult["HarnessConstraints"]
	if !ok {
		t.Fatal("LF result: map missing HarnessConstraints")
	}
	crlfContent, crlfOk := crlfResult["HarnessConstraints"]
	if !crlfOk {
		t.Fatal("CRLF result: map missing HarnessConstraints")
	}
	if lfContent != crlfContent {
		t.Errorf("CRLF and LF inputs produced different content for HarnessConstraints:\n  LF:   %q\n  CRLF: %q",
			lfContent, crlfContent)
	}
}

// TestParseDeployedRegions_CRLF_ContentHasNoCarriageReturns verifies that the returned
// region content contains no \r characters, regardless of the line endings in the source
// file. A \r in injected content would cause mismatch with LF-literal comparisons in
// harness tests running on a machine where the file was stored with CRLF endings.
//
// Without normalisation this test fails for multi-line content because TrimSpace leaves
// \r characters between interior lines.
func TestParseDeployedRegions_CRLF_ContentHasNoCarriageReturns(t *testing.T) {
	result, err := injectionfile.ParseDeployedRegions([]byte(crlfify(lfMultiLineRegionFixture)))
	if err != nil {
		t.Fatalf("ParseDeployedRegions (CRLF) returned unexpected error: %v", err)
	}
	content, ok := result["HarnessConstraints"]
	if !ok {
		t.Fatal("CRLF result: map missing HarnessConstraints")
	}
	if strings.Contains(content, "\r") {
		t.Errorf("region content returned for CRLF input contains \\r; want all \\r removed:\n  content: %q", content)
	}
}

// TestParseDeployedRegions_CRLF_MultipleRegions_MatchLF verifies that all regions in a
// file with multiple multi-line DEPLOYED regions match their LF equivalents when the file
// uses CRLF endings.
//
// Without normalisation each region with multi-line content fails the equality check.
func TestParseDeployedRegions_CRLF_MultipleRegions_MatchLF(t *testing.T) {
	lfResult, err := injectionfile.ParseDeployedRegions([]byte(lfMultipleRegionsFixture))
	if err != nil {
		t.Fatalf("ParseDeployedRegions (LF) returned unexpected error: %v", err)
	}
	crlfResult, err := injectionfile.ParseDeployedRegions([]byte(crlfify(lfMultipleRegionsFixture)))
	if err != nil {
		t.Fatalf("ParseDeployedRegions (CRLF) returned unexpected error: %v", err)
	}
	for _, name := range []string{"HarnessConstraints", "LanguagePatterns"} {
		lfContent, lfOk := lfResult[name]
		crlfContent, crlfOk := crlfResult[name]
		if !lfOk {
			t.Errorf("LF result: map missing %q", name)
			continue
		}
		if !crlfOk {
			t.Errorf("CRLF result: map missing %q", name)
			continue
		}
		if lfContent != crlfContent {
			t.Errorf("region %q: CRLF and LF inputs produced different content:\n  LF:   %q\n  CRLF: %q",
				name, lfContent, crlfContent)
		}
	}
}

// TestParseDeployedRegions_CRLF_MapSizeUnchanged verifies that parsing a CRLF file returns
// the same number of region entries as parsing the equivalent LF file. This guards against
// a faulty normalisation that might corrupt the boundary markers themselves.
func TestParseDeployedRegions_CRLF_MapSizeUnchanged(t *testing.T) {
	lfResult, err := injectionfile.ParseDeployedRegions([]byte(lfMultipleRegionsFixture))
	if err != nil {
		t.Fatalf("ParseDeployedRegions (LF) returned unexpected error: %v", err)
	}
	crlfResult, err := injectionfile.ParseDeployedRegions([]byte(crlfify(lfMultipleRegionsFixture)))
	if err != nil {
		t.Fatalf("ParseDeployedRegions (CRLF) returned unexpected error: %v", err)
	}
	if len(lfResult) != len(crlfResult) {
		t.Errorf("map size differs: LF produced %d entries, CRLF produced %d entries",
			len(lfResult), len(crlfResult))
	}
}

// TestParseDeployedRegions_CRLF_EmptyRegion_StillEmpty verifies that a declared-but-empty
// DEPLOYED region in a CRLF file still produces an entry with an empty-string value.
// This guards the boundary between "absent region" (no key) and "empty region" (key with
// value "") for CRLF-stored files.
func TestParseDeployedRegions_CRLF_EmptyRegion_StillEmpty(t *testing.T) {
	// deployedEmptyRegionFixture has one DEPLOYED region with nothing between its tags.
	crlfSrc := crlfify(deployedEmptyRegionFixture)
	result, err := injectionfile.ParseDeployedRegions([]byte(crlfSrc))
	if err != nil {
		t.Fatalf("ParseDeployedRegions (CRLF, empty region) returned unexpected error: %v", err)
	}
	content, ok := result["HarnessConstraints"]
	if !ok {
		t.Fatal("CRLF result: map missing HarnessConstraints; declared-but-empty region must produce a map entry")
	}
	if content != "" {
		t.Errorf("CRLF empty region: expected empty-string value, got: %q", content)
	}
}

// TestParseDeployedRegionsWithVersion_CRLF_VersionAndRegionsMatchLF verifies that
// ParseDeployedRegionsWithVersion returns the same version string and the same region
// content for a CRLF file as it does for the equivalent LF file.
//
// Without normalisation the version string is unaffected (frontmatter scalars are
// extracted before region content processing) but the multi-line region content differs.
func TestParseDeployedRegionsWithVersion_CRLF_VersionAndRegionsMatchLF(t *testing.T) {
	lfRegions, lfVersion, err := injectionfile.ParseDeployedRegionsWithVersion([]byte(lfWithVersionAndRegionFixture))
	if err != nil {
		t.Fatalf("ParseDeployedRegionsWithVersion (LF) returned unexpected error: %v", err)
	}
	crlfRegions, crlfVersion, err := injectionfile.ParseDeployedRegionsWithVersion([]byte(crlfify(lfWithVersionAndRegionFixture)))
	if err != nil {
		t.Fatalf("ParseDeployedRegionsWithVersion (CRLF) returned unexpected error: %v", err)
	}

	// Version must match regardless of line endings.
	if lfVersion != crlfVersion {
		t.Errorf("version mismatch: LF=%q, CRLF=%q", lfVersion, crlfVersion)
	}
	if crlfVersion != "4.0.0" {
		t.Errorf("version: want %q, got %q", "4.0.0", crlfVersion)
	}

	// Region content must match regardless of line endings.
	lfContent, lfOk := lfRegions["HarnessConstraints"]
	crlfContent, crlfOk := crlfRegions["HarnessConstraints"]
	if !lfOk {
		t.Fatal("LF result: map missing HarnessConstraints")
	}
	if !crlfOk {
		t.Fatal("CRLF result: map missing HarnessConstraints")
	}
	if lfContent != crlfContent {
		t.Errorf("HarnessConstraints content mismatch:\n  LF:   %q\n  CRLF: %q", lfContent, crlfContent)
	}
}

// TestLoadDir_CRLF_StoredFiles_MatchLFStoredFiles verifies the end-to-end LoadDir path:
// writing both harness content files with CRLF endings produces a HarnessContent whose
// Shared and Orchestrator maps are byte-for-byte equal to the result of writing the same
// files with LF endings. This covers the route that the four built-in harness packages
// take at run-time.
//
// Without normalisation multi-line region values contain \r, causing the CRLF-loaded
// content to differ from the LF-loaded content. On a Windows developer machine the actual
// harness files are checked out with CRLF, so this mismatch surfaces as a test failure
// in the opencode and vscodeghcp packages.
func TestLoadDir_CRLF_StoredFiles_MatchLFStoredFiles(t *testing.T) {
	const sharedLF = `# Harness Injections

[[DEPLOYED:HarnessConstraints]]
Do not invoke external APIs without explicit permission.
Check tool availability before each use.
[[/DEPLOYED:HarnessConstraints]]

[[DEPLOYED:LanguagePatterns]]
Use idiomatic Go patterns.
Prefer table-driven tests.
[[/DEPLOYED:LanguagePatterns]]
`
	const orchLF = `---
version: "2.0.0"
---
# Harness Injections (Orchestrator)

[[DEPLOYED:HarnessConstraints]]
Orchestrator constraint line one.
Orchestrator constraint line two.
[[/DEPLOYED:HarnessConstraints]]
`

	// Write LF versions.
	lfDir := t.TempDir()
	writeContentFile(t, lfDir, injectionfile.SharedFileName, sharedLF)
	writeContentFile(t, lfDir, injectionfile.OrchestratorFileName, orchLF)

	// Write CRLF versions.
	crlfDir := t.TempDir()
	writeContentFile(t, crlfDir, injectionfile.SharedFileName, crlfify(sharedLF))
	writeContentFile(t, crlfDir, injectionfile.OrchestratorFileName, crlfify(orchLF))

	lfContent, err := injectionfile.LoadDir(lfDir)
	if err != nil {
		t.Fatalf("LoadDir (LF) returned unexpected error: %v", err)
	}
	crlfContent, err := injectionfile.LoadDir(crlfDir)
	if err != nil {
		t.Fatalf("LoadDir (CRLF) returned unexpected error: %v", err)
	}

	// OrchestratorVersion must match.
	if lfContent.OrchestratorVersion != crlfContent.OrchestratorVersion {
		t.Errorf("OrchestratorVersion mismatch: LF=%q, CRLF=%q",
			lfContent.OrchestratorVersion, crlfContent.OrchestratorVersion)
	}

	// Every Shared region value must match.
	for name, lfVal := range lfContent.Shared {
		crlfVal, ok := crlfContent.Shared[name]
		if !ok {
			t.Errorf("CRLF Shared map missing region %q", name)
			continue
		}
		if lfVal != crlfVal {
			t.Errorf("Shared[%q] mismatch:\n  LF:   %q\n  CRLF: %q", name, lfVal, crlfVal)
		}
	}
	for name := range crlfContent.Shared {
		if _, ok := lfContent.Shared[name]; !ok {
			t.Errorf("CRLF Shared map contains unexpected region %q", name)
		}
	}

	// Every Orchestrator region value must match.
	for name, lfVal := range lfContent.Orchestrator {
		crlfVal, ok := crlfContent.Orchestrator[name]
		if !ok {
			t.Errorf("CRLF Orchestrator map missing region %q", name)
			continue
		}
		if lfVal != crlfVal {
			t.Errorf("Orchestrator[%q] mismatch:\n  LF:   %q\n  CRLF: %q", name, lfVal, crlfVal)
		}
	}
	for name := range crlfContent.Orchestrator {
		if _, ok := lfContent.Orchestrator[name]; !ok {
			t.Errorf("CRLF Orchestrator map contains unexpected region %q", name)
		}
	}
}
