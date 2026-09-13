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
	"mosaic-deploy/internal/catalog/catalogpaths"
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

// TestResolveRoot_FromPackageDirectory_RootContainsRequiredMarker verifies that
// the resolved root directory contains the required MOSAIC root marker at its
// Catalog/ location: Catalog/CatalogFilesFormat.md.
// Catalog/Workflows/Index.md is no longer a required root marker.
func TestResolveRoot_FromPackageDirectory_RootContainsRequiredMarker(t *testing.T) {
	root, err := catalog.ResolveRoot(repoRoot())
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}

	marker := catalogpaths.MosaicRelSourceFilesFormatFile
	full := filepath.Join(root, marker)
	if _, statErr := os.Stat(full); os.IsNotExist(statErr) {
		t.Errorf("resolved root %q does not contain required marker file %s", root, marker)
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
// carrying the required root marker at its Catalog/ location is recognised as a MOSAIC root.
// The required marker is:
//
//	Catalog/CatalogFilesFormat.md
//
// Catalog/Workflows/Index.md is no longer a required root marker; this test deliberately
// omits it to verify that the root is recognised on CatalogFilesFormat.md alone.
func TestResolveRoot_CatalogPrefixedMarkers_RecognizedAsMosaicRoot(t *testing.T) {
	dir := t.TempDir()

	// Create only CatalogFilesFormat.md — no Catalog/Workflows/Index.md.
	if err := os.MkdirAll(filepath.Join(dir, "Catalog"), 0o755); err != nil {
		t.Fatalf("setup MkdirAll Catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, catalogpaths.MosaicRelSourceFilesFormatFile), []byte("# CatalogFilesFormat\n"), 0o644); err != nil {
		t.Fatalf("setup WriteFile %s: %v", catalogpaths.MosaicRelSourceFilesFormatFile, err)
	}

	root, err := catalog.ResolveRoot(dir)
	if err != nil {
		t.Fatalf("ResolveRoot returned error for a dir with %s: %v; "+
			"a tree carrying this marker must be recognised as the MOSAIC root even without Catalog/Workflows/Index.md",
			catalogpaths.MosaicRelSourceFilesFormatFile, err)
	}
	if root == "" {
		t.Fatal("ResolveRoot returned an empty root with nil error")
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

// TestResolveRoot_WithoutIndexMd_Succeeds verifies that a directory carrying
// Catalog/CatalogFilesFormat.md is recognised as a MOSAIC root even when
// Catalog/Workflows/Index.md is absent. Index.md is no longer a required root marker.
func TestResolveRoot_WithoutIndexMd_Succeeds(t *testing.T) {
	dir := t.TempDir()

	// Create ONLY CatalogFilesFormat.md — no Catalog/Workflows/Index.md.
	if err := os.MkdirAll(filepath.Join(dir, "Catalog"), 0o755); err != nil {
		t.Fatalf("setup MkdirAll Catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, catalogpaths.MosaicRelSourceFilesFormatFile), []byte("# CatalogFilesFormat\n"), 0o644); err != nil {
		t.Fatalf("setup WriteFile %s: %v", catalogpaths.MosaicRelSourceFilesFormatFile, err)
	}

	root, err := catalog.ResolveRoot(dir)
	if err != nil {
		t.Fatalf("ResolveRoot returned error for a dir with %s but no Index.md: %v; "+
			"Catalog/Workflows/Index.md must not be a required root marker",
			catalogpaths.MosaicRelSourceFilesFormatFile, err)
	}
	if root == "" {
		t.Fatal("ResolveRoot returned an empty root with nil error")
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
// containing Catalog/CatalogFilesFormat.md (the sole required marker) is resolved as a
// valid MOSAIC root. Catalog/Workflows/Index.md is deliberately omitted to confirm it is
// not required.
func TestResolveRoot_NewMarkerFilename_RecognizedAsMosaicRoot(t *testing.T) {
	dir := t.TempDir()

	// Create the marker file at its Catalog/ location only. No Index.md.
	if err := os.MkdirAll(filepath.Join(dir, "Catalog"), 0o755); err != nil {
		t.Fatalf("setup MkdirAll Catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, catalogpaths.MosaicRelSourceFilesFormatFile), []byte("# CatalogFilesFormat\n"), 0o644); err != nil {
		t.Fatalf("setup WriteFile %s: %v", catalogpaths.MosaicRelSourceFilesFormatFile, err)
	}

	root, err := catalog.ResolveRoot(dir)
	if err != nil {
		t.Fatalf("ResolveRoot returned error for a dir with %s: %v; "+
			"a tree containing this marker must be recognised as the MOSAIC root",
			catalogpaths.MosaicRelSourceFilesFormatFile, err)
	}
	if root == "" {
		t.Fatal("ResolveRoot returned an empty root with nil error")
	}
}
