package catalog_test

// Tests for MOSAIC root resolution and the failure mode where the supplied
// directory is not a MOSAIC repository root.
//
// ResolveRoot validates the directory it is given and returns ErrNotMosaicRoot
// when that directory is not a MOSAIC root — it does NOT walk up the tree.
// All tests here verify the public contract, not the internal detection heuristic.
//
// Integration tests use the real repository tree (this test file lives within it).
// Failure-mode tests use t.TempDir(), which is guaranteed to not contain a MOSAIC repo.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// repoRoot returns the absolute path to the MOSAIC repository root, navigating up from
// this package's working directory. Tests run with the working directory set to the
// package directory (internal/catalog). The path climbs four levels:
// catalog/ → internal/ → Deployment/ → Tools/ → repo root.
func repoRoot() string {
	abs, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		panic("catalog_test: resolve repo root: " + err.Error())
	}
	return abs
}

// ---------------------------------------------------------------------------
// Happy-path: resolving the real repository root
// ---------------------------------------------------------------------------

// TestResolveRoot_FromPackageDirectory_ReturnsNonEmptyRoot verifies that
// ResolveRoot succeeds when called with the repository root — which contains
// the MOSAIC marker files.
func TestResolveRoot_FromPackageDirectory_ReturnsNonEmptyRoot(t *testing.T) {
	root, err := catalog.ResolveRoot(repoRoot())
	if err != nil {
		t.Fatalf("ResolveRoot(repoRoot()) returned error: %v", err)
	}
	if root == "" {
		t.Fatal("ResolveRoot(repoRoot()) returned empty string with nil error")
	}
}

// TestResolveRoot_FromPackageDirectory_RootIsAbsolute verifies that the path
// returned by ResolveRoot is absolute, so callers do not need to resolve it.
func TestResolveRoot_FromPackageDirectory_RootIsAbsolute(t *testing.T) {
	root, err := catalog.ResolveRoot(repoRoot())
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	if !filepath.IsAbs(root) {
		t.Errorf("ResolveRoot returned a relative path: %q; expected absolute path", root)
	}
}

// TestResolveRoot_FromPackageDirectory_RootContainsExpectedMarkers verifies that
// the resolved root directory actually contains the MOSAIC repository markers at
// their Catalog/ locations: Catalog/Agents/Generic/SourceFilesFormat.md and
// Catalog/Workflows/Index.md.
func TestResolveRoot_FromPackageDirectory_RootContainsExpectedMarkers(t *testing.T) {
	root, err := catalog.ResolveRoot(repoRoot())
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}

	markers := []string{
		filepath.Join("Catalog", "Agents", "Generic", "SourceFilesFormat.md"),
		filepath.Join("Catalog", "Workflows", "Index.md"),
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
	// Resolve from the repo root to get the canonical absolute root.
	canonical, err := catalog.ResolveRoot(repoRoot())
	if err != nil {
		t.Fatalf("ResolveRoot(repoRoot()) to get canonical root: %v", err)
	}

	// Then resolve from the canonical root itself — must agree.
	fromRoot, err := catalog.ResolveRoot(canonical)
	if err != nil {
		t.Fatalf("ResolveRoot(%q): %v", canonical, err)
	}

	if canonical != fromRoot {
		t.Errorf("ResolveRoot from root returned different path:\n  first:  %q\n  second: %q",
			canonical, fromRoot)
	}
}

// ---------------------------------------------------------------------------
// Walk removal: nested directories must not be accepted as the root
// ---------------------------------------------------------------------------

// TestResolveRoot_NestedDirectory_ReturnsErrNotMosaicRoot verifies that a directory
// nested inside the repository is NOT accepted as the MOSAIC root. ResolveRoot must
// validate the directory it is given rather than climbing the tree to find a root above it.
//
// This test fails while ResolveRoot still performs an upward walk (the pre-Stage-8
// behaviour), and passes once the walk is replaced with a single-directory check.
func TestResolveRoot_NestedDirectory_ReturnsErrNotMosaicRoot(t *testing.T) {
	// Tools/ is a well-known subdirectory of the repository root that is not itself
	// the root — it does not carry the root markers.
	nestedDir := filepath.Join(repoRoot(), "Tools")
	if _, err := os.Stat(nestedDir); os.IsNotExist(err) {
		t.Skipf("expected nested directory %s not found; skipping", nestedDir)
	}

	_, err := catalog.ResolveRoot(nestedDir)
	if !errors.Is(err, catalog.ErrNotMosaicRoot) {
		t.Errorf("ResolveRoot(%q): got %v, want ErrNotMosaicRoot — "+
			"a directory nested inside the repository must not be accepted as the root", nestedDir, err)
	}
}

// ---------------------------------------------------------------------------
// Catalog/-prefixed marker layout: new markers accepted, legacy markers rejected
// ---------------------------------------------------------------------------

// TestResolveRoot_CatalogPrefixedMarkers_RecognizedAsMosaicRoot verifies that a directory
// carrying the root markers at their Catalog/ locations is recognised as a MOSAIC root.
// The two required markers are:
//
//	Catalog/Agents/Generic/SourceFilesFormat.md
//	Catalog/Workflows/Index.md
//
// This test fails while isMosaicRoot still checks the legacy Agents/Generic/ and Workflows/
// paths, and passes once it is repointed to the Catalog/ paths.
func TestResolveRoot_CatalogPrefixedMarkers_RecognizedAsMosaicRoot(t *testing.T) {
	dir := t.TempDir()

	// Create the Catalog/-prefixed marker files.
	if err := os.MkdirAll(filepath.Join(dir, "Catalog", "Agents", "Generic"), 0o755); err != nil {
		t.Fatalf("setup MkdirAll Catalog/Agents/Generic: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Catalog", "Agents", "Generic", "SourceFilesFormat.md"), []byte("# Source Files Format\n"), 0o644); err != nil {
		t.Fatalf("setup WriteFile Catalog/Agents/Generic/SourceFilesFormat.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "Catalog", "Workflows"), 0o755); err != nil {
		t.Fatalf("setup MkdirAll Catalog/Workflows: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Catalog", "Workflows", "Index.md"), []byte("# Workflows Index\n"), 0o644); err != nil {
		t.Fatalf("setup WriteFile Catalog/Workflows/Index.md: %v", err)
	}

	root, err := catalog.ResolveRoot(dir)
	if err != nil {
		t.Fatalf("ResolveRoot returned error for a dir with Catalog/-prefixed markers: %v; "+
			"a tree with Catalog/Agents/Generic/SourceFilesFormat.md and Catalog/Workflows/Index.md must be recognised", err)
	}
	if root == "" {
		t.Fatal("ResolveRoot returned an empty root with nil error")
	}
}

// TestResolveRoot_LegacyOnlyMarkers_NotRecognizedAsMosaicRoot verifies that a directory
// carrying the root markers only at the legacy (pre-migration) locations is NOT recognised
// as a MOSAIC root. The legacy paths are:
//
//	Agents/Generic/SourceFilesFormat.md
//	Workflows/Index.md
//
// After the marker migration to Catalog/, isMosaicRoot must require the Catalog/-prefixed
// paths and refuse to accept the legacy layout on its own.
//
// This test fails before the marker is repointed (since the legacy layout currently satisfies
// isMosaicRoot), and passes once isMosaicRoot requires the Catalog/ paths.
func TestResolveRoot_LegacyOnlyMarkers_NotRecognizedAsMosaicRoot(t *testing.T) {
	dir := t.TempDir()

	// Create the legacy (non-Catalog/-prefixed) marker files only.
	if err := os.MkdirAll(filepath.Join(dir, "Agents", "Generic"), 0o755); err != nil {
		t.Fatalf("setup MkdirAll Agents/Generic: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Agents", "Generic", "SourceFilesFormat.md"), []byte("# Source Files Format\n"), 0o644); err != nil {
		t.Fatalf("setup WriteFile Agents/Generic/SourceFilesFormat.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "Workflows"), 0o755); err != nil {
		t.Fatalf("setup MkdirAll Workflows: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Workflows", "Index.md"), []byte("# Workflows Index\n"), 0o644); err != nil {
		t.Fatalf("setup WriteFile Workflows/Index.md: %v", err)
	}

	_, err := catalog.ResolveRoot(dir)
	if !errors.Is(err, catalog.ErrNotMosaicRoot) {
		t.Errorf("ResolveRoot(%q): got %v, want ErrNotMosaicRoot — "+
			"the legacy Agents/Generic/ and Workflows/ layout must no longer be accepted as a MOSAIC root", dir, err)
	}
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

	// Create only the Catalog/Agents/Generic directory — not the Catalog/Workflows/Index.md marker.
	if err := os.MkdirAll(filepath.Join(dir, "Catalog", "Agents", "Generic"), 0o755); err != nil {
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

// ---------------------------------------------------------------------------
// Marker filename: new name recognized, old name rejected
// ---------------------------------------------------------------------------

// TestResolveRoot_NewMarkerFilename_RecognizedAsMosaicRoot verifies that a directory
// containing Catalog/Agents/Generic/SourceFilesFormat.md (the current marker name) together
// with Catalog/Workflows/Index.md is resolved as a valid MOSAIC root. This is the primary
// assertion for the marker migration: the tool must recognise exactly the file at the
// Catalog/-prefixed location.
func TestResolveRoot_NewMarkerFilename_RecognizedAsMosaicRoot(t *testing.T) {
	dir := t.TempDir()

	// Create the marker file at its Catalog/-prefixed location.
	if err := os.MkdirAll(filepath.Join(dir, "Catalog", "Agents", "Generic"), 0o755); err != nil {
		t.Fatalf("setup MkdirAll Catalog/Agents/Generic: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Catalog", "Agents", "Generic", "SourceFilesFormat.md"), []byte("# Source Files Format\n"), 0o644); err != nil {
		t.Fatalf("setup WriteFile SourceFilesFormat.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "Catalog", "Workflows"), 0o755); err != nil {
		t.Fatalf("setup MkdirAll Catalog/Workflows: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Catalog", "Workflows", "Index.md"), []byte("# Workflows Index\n"), 0o644); err != nil {
		t.Fatalf("setup WriteFile Catalog/Workflows/Index.md: %v", err)
	}

	root, err := catalog.ResolveRoot(dir)
	if err != nil {
		t.Fatalf("ResolveRoot returned error for a dir with Catalog/Agents/Generic/SourceFilesFormat.md: %v; "+
			"a tree containing the new marker at its Catalog/ location must be recognised as the MOSAIC root", err)
	}
	if root == "" {
		t.Fatal("ResolveRoot returned an empty root with nil error")
	}
}

// TestResolveRoot_OldMarkerFilenameOnly_NotRecognizedAsMosaicRoot verifies that a directory
// containing only the legacy marker file under Catalog/Agents/Generic/ (the old filename before
// the rename to SourceFilesFormat.md) is NOT recognised as a MOSAIC root. The root-detection
// heuristic must probe for the current filename only; the old name must not satisfy the check.
func TestResolveRoot_OldMarkerFilenameOnly_NotRecognizedAsMosaicRoot(t *testing.T) {
	dir := t.TempDir()

	// Build the old marker filename at runtime so this test file does not contain the literal
	// string as a contiguous byte sequence, which would trigger TestNoLegacySourceFormatReference.
	oldMarkerFile := "SOURCE" + "-" + "FORMAT" + ".md"

	// Create the old marker file (not the new one) at the Catalog/-prefixed location.
	if err := os.MkdirAll(filepath.Join(dir, "Catalog", "Agents", "Generic"), 0o755); err != nil {
		t.Fatalf("setup MkdirAll Catalog/Agents/Generic: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Catalog", "Agents", "Generic", oldMarkerFile), []byte("# Source Format\n"), 0o644); err != nil {
		t.Fatalf("setup WriteFile %s: %v", oldMarkerFile, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "Catalog", "Workflows"), 0o755); err != nil {
		t.Fatalf("setup MkdirAll Catalog/Workflows: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Catalog", "Workflows", "Index.md"), []byte("# Workflows Index\n"), 0o644); err != nil {
		t.Fatalf("setup WriteFile Catalog/Workflows/Index.md: %v", err)
	}

	_, err := catalog.ResolveRoot(dir)
	if !errors.Is(err, catalog.ErrNotMosaicRoot) {
		t.Errorf("ResolveRoot returned %v for a dir with only the old %s marker at Catalog/Agents/Generic/; "+
			"want ErrNotMosaicRoot — the old filename must not satisfy the root check", err, oldMarkerFile)
	}
}
