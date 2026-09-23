package testcatalog_test

// infra_test.go covers the InfrastructureAgentKeys method on *Catalog.
//
// These tests are written for the TDD RED phase. They compile and run, but
// most fail because the stub implementation always returns nil. When the real
// implementation is delivered, all tests must pass.
//
// Coverage:
//   - Returns sorted keys for agents with an infrastructure: frontmatter field.
//   - Returns keys sorted alphabetically (filename stem order).
//   - Returns nil when the Subagents/MosaicTest/ directory does not exist.
//   - Returns a non-nil empty []string{} when the directory exists but no
//     agents have an infrastructure: field.
//   - Returns nil when the Subagents/MosaicTest path is a file, not a directory
//     (portable "unreadable" simulation across platforms including Windows).
//   - Skips .md files that lack an infrastructure: field.
//   - Skips non-.md files (e.g. .txt, README).
//   - Skips subdirectories inside Subagents/MosaicTest/.
//   - Skips files with malformed frontmatter without propagating an error.
//   - When malformed file is skipped, the remaining valid agents are returned.

import (
	"testing"

	"mosaic-run/internal/testcatalog"
)

// infraCatalogDir returns the absolute path of a named testdata fixture
// used by the infra tests.
func infraCatalogDir(t *testing.T, name string) string {
	t.Helper()
	return fixtureDir(t, name)
}

// ---- InfrastructureAgentKeys: populated directory ----

// TestInfrastructureAgentKeys_WithInfraAgents_ReturnsSortedKeys verifies that
// InfrastructureAgentKeys returns the filename stems of all .md files in
// Subagents/MosaicTest/ that declare an infrastructure: field, sorted
// alphabetically. Files without the field and non-.md entries are excluded.
func TestInfrastructureAgentKeys_WithInfraAgents_ReturnsSortedKeys(t *testing.T) {
	cat, err := testcatalog.Load(infraCatalogDir(t, "infra-with-agents"))
	if err != nil {
		t.Fatalf("Load(infra-with-agents): %v", err)
	}
	got := cat.InfrastructureAgentKeys()
	want := []string{"mosaictest-checkpoint", "mosaictest-commit", "mosaictest-review"}
	if got == nil {
		t.Fatal("InfrastructureAgentKeys() = nil; want sorted keys from infra-with-agents fixture")
	}
	if len(got) != len(want) {
		t.Fatalf("InfrastructureAgentKeys() = %v (len %d), want %v (len %d)",
			got, len(got), want, len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("InfrastructureAgentKeys()[%d] = %q, want %q (keys must be sorted alphabetically)",
				i, got[i], w)
		}
	}
}

// TestInfrastructureAgentKeys_WithInfraAgents_ExcludesNonInfraFile verifies
// that mosaictest-scripted.md, which has no infrastructure: field, is NOT
// returned by InfrastructureAgentKeys even though it lives in Subagents/MosaicTest/.
func TestInfrastructureAgentKeys_WithInfraAgents_ExcludesNonInfraFile(t *testing.T) {
	cat, err := testcatalog.Load(infraCatalogDir(t, "infra-with-agents"))
	if err != nil {
		t.Fatalf("Load(infra-with-agents): %v", err)
	}
	got := cat.InfrastructureAgentKeys()
	for _, key := range got {
		if key == "mosaictest-scripted" {
			t.Errorf("InfrastructureAgentKeys() contains %q; files without an infrastructure: field must be excluded",
				key)
		}
	}
}

// TestInfrastructureAgentKeys_WithInfraAgents_ExcludesNonMdFile verifies that
// non-.md files (e.g. readme.txt) in Subagents/MosaicTest/ are not included
// in the returned key list.
func TestInfrastructureAgentKeys_WithInfraAgents_ExcludesNonMdFile(t *testing.T) {
	cat, err := testcatalog.Load(infraCatalogDir(t, "infra-with-agents"))
	if err != nil {
		t.Fatalf("Load(infra-with-agents): %v", err)
	}
	got := cat.InfrastructureAgentKeys()
	for _, key := range got {
		if key == "readme" {
			t.Errorf("InfrastructureAgentKeys() contains %q; non-.md files must be excluded", key)
		}
	}
}

// TestInfrastructureAgentKeys_WithInfraAgents_ExcludesSubdirectory verifies
// that subdirectories inside Subagents/MosaicTest/ are ignored and do not
// produce a key in the result.
func TestInfrastructureAgentKeys_WithInfraAgents_ExcludesSubdirectory(t *testing.T) {
	cat, err := testcatalog.Load(infraCatalogDir(t, "infra-with-agents"))
	if err != nil {
		t.Fatalf("Load(infra-with-agents): %v", err)
	}
	got := cat.InfrastructureAgentKeys()
	for _, key := range got {
		if key == "subdir" {
			t.Errorf("InfrastructureAgentKeys() contains %q; subdirectories must be excluded", key)
		}
	}
}

// ---- InfrastructureAgentKeys: empty directory ----

// TestInfrastructureAgentKeys_NoInfraAgents_ReturnsNonNilEmpty verifies that
// when Subagents/MosaicTest/ exists but contains no .md files with an
// infrastructure: field, InfrastructureAgentKeys returns a non-nil empty
// []string{} (not nil). This distinguishes "explicitly none" from "don't know".
func TestInfrastructureAgentKeys_NoInfraAgents_ReturnsNonNilEmpty(t *testing.T) {
	cat, err := testcatalog.Load(infraCatalogDir(t, "infra-no-agents"))
	if err != nil {
		t.Fatalf("Load(infra-no-agents): %v", err)
	}
	got := cat.InfrastructureAgentKeys()
	if got == nil {
		t.Error("InfrastructureAgentKeys() = nil; want non-nil empty []string{} when the " +
			"Subagents directory exists but has no agents with an infrastructure: field. " +
			"nil means 'don't know' (deploy tool asks interactively); non-nil empty means " +
			"'explicitly none' (deploy no infra agents without asking)")
	}
	if len(got) != 0 {
		t.Errorf("InfrastructureAgentKeys() = %v, want non-nil empty slice", got)
	}
}

// ---- InfrastructureAgentKeys: missing directory ----

// TestInfrastructureAgentKeys_SubagentsDirMissing_ReturnsNil verifies that
// when the Subagents/MosaicTest/ directory does not exist under the catalog
// root, InfrastructureAgentKeys returns nil (not an error, not an empty slice).
// nil is the "don't know" signal: the deploy tool will ask interactively.
func TestInfrastructureAgentKeys_SubagentsDirMissing_ReturnsNil(t *testing.T) {
	cat, err := testcatalog.Load(infraCatalogDir(t, "infra-no-subagents"))
	if err != nil {
		t.Fatalf("Load(infra-no-subagents): %v", err)
	}
	got := cat.InfrastructureAgentKeys()
	if got != nil {
		t.Errorf("InfrastructureAgentKeys() = %v, want nil when Subagents/MosaicTest/ does not exist. "+
			"nil signals 'don't know' so the deploy tool will ask interactively; "+
			"returning a non-nil value would suppress the interactive prompt incorrectly",
			got)
	}
}

// ---- InfrastructureAgentKeys: unreadable directory ----

// TestInfrastructureAgentKeys_SubagentsDirUnreadable_ReturnsNil verifies that
// when the Subagents/MosaicTest path exists as a regular file (not a directory),
// InfrastructureAgentKeys returns nil without propagating the os.ReadDir error.
// Using a file at the directory path is a portable "unreadable directory"
// simulation that works on Windows without requiring permission manipulation.
func TestInfrastructureAgentKeys_SubagentsDirUnreadable_ReturnsNil(t *testing.T) {
	cat, err := testcatalog.Load(infraCatalogDir(t, "infra-unreadable"))
	if err != nil {
		t.Fatalf("Load(infra-unreadable): %v", err)
	}
	got := cat.InfrastructureAgentKeys()
	if got != nil {
		t.Errorf("InfrastructureAgentKeys() = %v, want nil when Subagents/MosaicTest cannot be "+
			"read (path is a file, not a directory). "+
			"Infrastructure discovery is optional; errors must not block deployment",
			got)
	}
}

// ---- InfrastructureAgentKeys: malformed frontmatter ----

// TestInfrastructureAgentKeys_MalformedFrontmatter_SkipsFileWithoutError verifies
// that a .md file with malformed YAML frontmatter is skipped silently. The valid
// agents in the same directory are still returned. No error is returned.
func TestInfrastructureAgentKeys_MalformedFrontmatter_SkipsFileWithoutError(t *testing.T) {
	cat, err := testcatalog.Load(infraCatalogDir(t, "infra-malformed"))
	if err != nil {
		t.Fatalf("Load(infra-malformed): %v", err)
	}
	got := cat.InfrastructureAgentKeys()
	// The bad-frontmatter.md file must be skipped; mosaictest-review.md is valid.
	want := []string{"mosaictest-review"}
	if got == nil {
		t.Fatal("InfrastructureAgentKeys() = nil; want the valid agent key even when " +
			"a malformed file is present in the same directory")
	}
	if len(got) != len(want) {
		t.Fatalf("InfrastructureAgentKeys() = %v (len %d), want %v (len %d); "+
			"malformed frontmatter file must be skipped, valid agents must be returned",
			got, len(got), want, len(want))
	}
	if got[0] != want[0] {
		t.Errorf("InfrastructureAgentKeys()[0] = %q, want %q", got[0], want[0])
	}
}

// TestInfrastructureAgentKeys_MalformedFrontmatter_DoesNotContainBadFile verifies
// that the bad-frontmatter.md file does not appear as a key in the results.
func TestInfrastructureAgentKeys_MalformedFrontmatter_DoesNotContainBadFile(t *testing.T) {
	cat, err := testcatalog.Load(infraCatalogDir(t, "infra-malformed"))
	if err != nil {
		t.Fatalf("Load(infra-malformed): %v", err)
	}
	got := cat.InfrastructureAgentKeys()
	for _, key := range got {
		if key == "bad-frontmatter" {
			t.Errorf("InfrastructureAgentKeys() contains %q; files with malformed YAML frontmatter must be skipped",
				key)
		}
	}
}

// ---- InfrastructureAgentKeys: sorted order ----

// TestInfrastructureAgentKeys_SortedAlphabetically verifies that the returned
// keys are sorted in ascending alphabetical order by filename stem, regardless
// of the order in which the filesystem returns directory entries.
func TestInfrastructureAgentKeys_SortedAlphabetically(t *testing.T) {
	cat, err := testcatalog.Load(infraCatalogDir(t, "infra-with-agents"))
	if err != nil {
		t.Fatalf("Load(infra-with-agents): %v", err)
	}
	got := cat.InfrastructureAgentKeys()
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Errorf("InfrastructureAgentKeys() not sorted: got[%d]=%q follows got[%d]=%q; "+
				"keys must be sorted alphabetically", i, got[i], i-1, got[i-1])
		}
	}
}
