package catalog_test

// Tests for MOSAIC root resolution and the failure mode where the working directory
// is not a MOSAIC repository.
//
// ResolveRoot walks up the directory tree from a starting directory looking for
// the MOSAIC repository markers. All tests here verify the public contract, not
// the internal detection heuristic.
//
// Integration tests use the real repository tree (this test file lives within it).
// Failure-mode tests use t.TempDir(), which is guaranteed to not contain a MOSAIC repo.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// repoRoot returns the MOSAIC repository root relative to this package's working directory.
// Tests run with the working directory set to the package directory (internal/catalog).
// The path climbs four levels: catalog/ → internal/ → Deployment/ → Tools/ → repo root.
func repoRoot() string {
	return filepath.Join("..", "..", "..", "..")
}

// ---------------------------------------------------------------------------
// Happy-path: resolving the real repository root
// ---------------------------------------------------------------------------

// TestResolveRoot_FromPackageDirectory_ReturnsNonEmptyRoot verifies that
// ResolveRoot succeeds when called from within this package directory — which is
// itself inside the MOSAIC repository.
func TestResolveRoot_FromPackageDirectory_ReturnsNonEmptyRoot(t *testing.T) {
	root, err := catalog.ResolveRoot(".")
	if err != nil {
		t.Fatalf("ResolveRoot(\".\") returned error: %v", err)
	}
	if root == "" {
		t.Fatal("ResolveRoot(\".\") returned empty string with nil error")
	}
}

// TestResolveRoot_FromPackageDirectory_RootIsAbsolute verifies that the path
// returned by ResolveRoot is absolute, so callers do not need to resolve it.
func TestResolveRoot_FromPackageDirectory_RootIsAbsolute(t *testing.T) {
	root, err := catalog.ResolveRoot(".")
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	if !filepath.IsAbs(root) {
		t.Errorf("ResolveRoot returned a relative path: %q; expected absolute path", root)
	}
}

// TestResolveRoot_FromPackageDirectory_RootContainsExpectedMarkers verifies that
// the resolved root directory actually contains the MOSAIC repository markers:
// Agents/Generic/SOURCE-FORMAT.md and Workflows/Index.md.
func TestResolveRoot_FromPackageDirectory_RootContainsExpectedMarkers(t *testing.T) {
	root, err := catalog.ResolveRoot(".")
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}

	markers := []string{
		filepath.Join("Agents", "Generic", "SOURCE-FORMAT.md"),
		filepath.Join("Workflows", "Index.md"),
	}
	for _, marker := range markers {
		full := filepath.Join(root, marker)
		if _, statErr := os.Stat(full); os.IsNotExist(statErr) {
			t.Errorf("resolved root %q does not contain expected marker file %s", root, marker)
		}
	}
}

// TestResolveRoot_FromRepoRoot_ReturnsStableResult verifies that calling
// ResolveRoot with the repo root directly returns the same root (idempotent).
func TestResolveRoot_FromRepoRoot_ReturnsStableResult(t *testing.T) {
	// First, resolve from within the package directory to get the canonical root.
	canonical, err := catalog.ResolveRoot(".")
	if err != nil {
		t.Fatalf("ResolveRoot(\".\") to get canonical root: %v", err)
	}

	// Then resolve from the canonical root itself.
	fromRoot, err := catalog.ResolveRoot(canonical)
	if err != nil {
		t.Fatalf("ResolveRoot(%q): %v", canonical, err)
	}

	// Both calls must agree on the root path.
	if canonical != fromRoot {
		t.Errorf("ResolveRoot from root returned different path:\n  first:  %q\n  second: %q",
			canonical, fromRoot)
	}
}

// TestResolveRoot_FromSubdirectory_FindsRoot verifies that ResolveRoot finds the
// repository root when called from a subdirectory that is nested deeper in the tree.
func TestResolveRoot_FromSubdirectory_FindsRoot(t *testing.T) {
	// Use a known deeply-nested directory in the repository.
	subdir := filepath.Join(repoRoot(), "Agents", "Generic", "Agents", "Execution")
	if _, err := os.Stat(subdir); os.IsNotExist(err) {
		t.Skipf("Skipping: expected subdirectory %s not found", subdir)
	}

	root, err := catalog.ResolveRoot(subdir)
	if err != nil {
		t.Fatalf("ResolveRoot(%q): %v", subdir, err)
	}
	if root == "" {
		t.Fatalf("ResolveRoot(%q): returned empty root with nil error", subdir)
	}

	// The resolved root must be an ancestor of the starting subdirectory.
	rel, relErr := filepath.Rel(root, subdir)
	if relErr != nil || !isDescendant(rel) {
		t.Errorf("resolved root %q is not an ancestor of starting dir %q (rel=%q)", root, subdir, rel)
	}
}

// isDescendant reports whether rel is a valid non-empty relative path that does not
// escape its base directory (i.e., does not start with "..").
//
// filepath.SplitList must NOT be used here: it splits by the OS PATH list separator
// (';' on Windows, ':' on Unix), not by the directory separator. A path like
// "../sibling" would be returned as a single-element slice, and parts[0] != ".."
// would incorrectly evaluate to true (treating it as a valid descendant).
// Instead, ToSlash normalises the separator and strings.Split splits by directory.
func isDescendant(rel string) bool {
	if rel == "" || rel == "." {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return len(parts) > 0 && parts[0] != ".."
}

// ---------------------------------------------------------------------------
// Failure mode: directory is not a MOSAIC repository
// ---------------------------------------------------------------------------

// TestResolveRoot_EmptyTempDir_ReturnsError verifies that ResolveRoot returns an
// error when called from a temp directory that is not a MOSAIC repository.
func TestResolveRoot_EmptyTempDir_ReturnsError(t *testing.T) {
	dir := t.TempDir()

	_, err := catalog.ResolveRoot(dir)
	if err == nil {
		t.Fatalf("ResolveRoot(%q) returned nil error for an empty temp directory", dir)
	}
}

// TestResolveRoot_EmptyTempDir_WrapsErrNotMosaicRoot verifies that the error
// returned for a non-MOSAIC directory wraps catalog.ErrNotMosaicRoot, allowing
// callers to use errors.Is for type-stable checking.
func TestResolveRoot_EmptyTempDir_WrapsErrNotMosaicRoot(t *testing.T) {
	dir := t.TempDir()

	_, err := catalog.ResolveRoot(dir)
	if !errors.Is(err, catalog.ErrNotMosaicRoot) {
		t.Errorf("ResolveRoot(%q): got error %v, want an error wrapping ErrNotMosaicRoot", dir, err)
	}
}

// TestResolveRoot_DirectoryWithPartialMarkers_ReturnsErrNotMosaicRoot verifies
// that a directory containing only some repository markers (but not all) is not
// recognised as a MOSAIC root. This prevents false positives on partially cloned
// or incomplete checkouts.
func TestResolveRoot_DirectoryWithPartialMarkers_ReturnsErrNotMosaicRoot(t *testing.T) {
	dir := t.TempDir()

	// Create only the Agents/Generic directory — not the Workflows/Index.md marker.
	if err := os.MkdirAll(filepath.Join(dir, "Agents", "Generic"), 0o755); err != nil {
		t.Fatalf("setup: MkdirAll: %v", err)
	}

	_, err := catalog.ResolveRoot(dir)
	if !errors.Is(err, catalog.ErrNotMosaicRoot) {
		t.Errorf("ResolveRoot with partial markers: got error %v, want ErrNotMosaicRoot", err)
	}
}

// TestResolveRoot_NonExistentDirectory_ReturnsError verifies that ResolveRoot
// returns a non-nil error (not necessarily ErrNotMosaicRoot) for a path that does
// not exist on disk. The contract does not specify the exact error type for an
// unreadable path, only that an error is returned.
func TestResolveRoot_NonExistentDirectory_ReturnsError(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := catalog.ResolveRoot(nonExistent)
	if err == nil {
		t.Fatalf("ResolveRoot(%q): expected error for non-existent path, got nil", nonExistent)
	}
}

// TestResolveRoot_KnownNonMosaicFixture_ReturnsErrNotMosaicRoot verifies the
// testdata fixture tree that is explicitly designed to not be a MOSAIC root.
func TestResolveRoot_KnownNonMosaicFixture_ReturnsErrNotMosaicRoot(t *testing.T) {
	fixtureDir := filepath.Join("..", "..", "testdata", "catalog", "not-a-repo")
	if _, err := os.Stat(fixtureDir); os.IsNotExist(err) {
		t.Skipf("Skipping: fixture directory %s not found", fixtureDir)
	}

	_, err := catalog.ResolveRoot(fixtureDir)
	if !errors.Is(err, catalog.ErrNotMosaicRoot) {
		t.Errorf("ResolveRoot(%q): got error %v, want ErrNotMosaicRoot", fixtureDir, err)
	}
}
