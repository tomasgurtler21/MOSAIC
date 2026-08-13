package main

// catalog_folder_test.go verifies the resolveCatalogFolder function.
//
// Contract under test (T5.2 — resolution and defaulting):
//
//   Default behaviour (flagValue == ""):
//     - Returns DefaultCatalogRoot(mosaicRoot) made absolute.
//     - The returned path is always absolute (filepath.IsAbs returns true).
//     - When mosaicRoot changes, the returned default path changes accordingly.
//     - No validation is performed on the default path: if {mosaic-root}/Catalog does
//       not exist, no error is returned (only explicitly supplied paths are validated).
//
//   Explicit behaviour (flagValue != ""):
//     - When a valid directory is supplied, it is returned as an absolute path.
//     - The returned value is byte-equal to filepath.Abs(flagValue) for a valid directory.
//     - An explicit value wins over the default: the returned path equals the supplied
//       value (made absolute), not DefaultCatalogRoot(mosaicRoot).
//
// Contract under test (T5.3 — fail-fast validation):
//
//   Nonexistent explicit path:
//     - Returns a non-nil error.
//     - The error names the offending path (its text contains the path string).
//     - The error wraps catalog.ErrCatalogFolderMissing, discriminable via errors.Is.
//
//   File path (not a directory):
//     - Returns a non-nil error.
//     - The error wraps catalog.ErrCatalogFolderNotDirectory, discriminable via errors.Is.
//
//   Valid directory:
//     - Returns nil error.
//     - The returned path equals filepath.Abs of the supplied directory.
//
//   Default path is not validated:
//     - Calling resolveCatalogFolder with flagValue=="" and a mosaicRoot whose
//       {mosaicRoot}/Catalog directory does not exist returns nil error.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// ---------------------------------------------------------------------------
// T5.2 — Resolution and defaulting
// ---------------------------------------------------------------------------

// TestResolveCatalogFolder_AbsentFlag_DefaultsToMosaicRootCatalog verifies that
// when no explicit --catalog-folder value is supplied (flagValue == ""), resolveCatalogFolder
// returns the DefaultCatalogRoot of the given mosaicRoot, made absolute.
func TestResolveCatalogFolder_AbsentFlag_DefaultsToMosaicRootCatalog(t *testing.T) {
	mosaicRoot := t.TempDir()

	got, err := resolveCatalogFolder(mosaicRoot, "")
	if err != nil {
		t.Fatalf("resolveCatalogFolder(%q, \"\"): unexpected error: %v; "+
			"an absent flag must default without error", mosaicRoot, err)
	}

	wantBase, err2 := filepath.Abs(catalog.DefaultCatalogRoot(mosaicRoot))
	if err2 != nil {
		t.Fatalf("filepath.Abs(DefaultCatalogRoot): %v", err2)
	}
	if got != wantBase {
		t.Errorf("resolveCatalogFolder(%q, \"\") = %q, want %q; "+
			"absent flag must yield DefaultCatalogRoot(mosaicRoot) made absolute",
			mosaicRoot, got, wantBase)
	}
}

// TestResolveCatalogFolder_AbsentFlag_ResultIsAbsolute verifies that the path returned
// for the default case (flagValue == "") is always absolute.
func TestResolveCatalogFolder_AbsentFlag_ResultIsAbsolute(t *testing.T) {
	mosaicRoot := t.TempDir()

	got, err := resolveCatalogFolder(mosaicRoot, "")
	if err != nil {
		t.Fatalf("resolveCatalogFolder: unexpected error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("resolveCatalogFolder(%q, \"\") = %q; want an absolute path; "+
			"the resolved catalogue folder must always be absolute", mosaicRoot, got)
	}
}

// TestResolveCatalogFolder_AbsentFlag_DifferentMosaicRoots_YieldDifferentDefaults
// verifies that using different mosaicRoot values produces different default catalogue
// folders. This confirms the result is derived from mosaicRoot and not hardcoded.
func TestResolveCatalogFolder_AbsentFlag_DifferentMosaicRoots_YieldDifferentDefaults(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()

	got1, err1 := resolveCatalogFolder(root1, "")
	if err1 != nil {
		t.Fatalf("resolveCatalogFolder(%q, \"\"): %v", root1, err1)
	}
	got2, err2 := resolveCatalogFolder(root2, "")
	if err2 != nil {
		t.Fatalf("resolveCatalogFolder(%q, \"\"): %v", root2, err2)
	}

	if got1 == got2 {
		t.Errorf("resolveCatalogFolder with different mosaicRoots both returned %q; "+
			"different MOSAIC roots must yield different default catalogue roots", got1)
	}
}

// TestResolveCatalogFolder_ExplicitValue_WinsOverDefault verifies that when a non-empty
// flagValue is supplied, the returned path is derived from flagValue rather than from
// DefaultCatalogRoot(mosaicRoot).
func TestResolveCatalogFolder_ExplicitValue_WinsOverDefault(t *testing.T) {
	mosaicRoot := t.TempDir()
	explicitCatalog := t.TempDir() // valid directory; distinct from mosaicRoot/Catalog

	got, err := resolveCatalogFolder(mosaicRoot, explicitCatalog)
	if err != nil {
		t.Fatalf("resolveCatalogFolder(%q, %q): unexpected error: %v", mosaicRoot, explicitCatalog, err)
	}

	absExplicit, _ := filepath.Abs(explicitCatalog)
	defaultPath, _ := filepath.Abs(catalog.DefaultCatalogRoot(mosaicRoot))

	if got != absExplicit {
		t.Errorf("resolveCatalogFolder(%q, %q) = %q, want %q (explicit value made absolute); "+
			"an explicit flag value must win over the default",
			mosaicRoot, explicitCatalog, got, absExplicit)
	}
	if got == defaultPath {
		t.Errorf("resolveCatalogFolder returned the default path %q even though an explicit value %q was supplied; "+
			"the explicit value must override the default",
			defaultPath, explicitCatalog)
	}
}

// TestResolveCatalogFolder_ExplicitValue_ReturnsAbsolutePath verifies that when an
// explicit flagValue is supplied and is valid, the returned path is absolute regardless
// of whether flagValue itself was relative or absolute.
func TestResolveCatalogFolder_ExplicitValue_ReturnsAbsolutePath(t *testing.T) {
	mosaicRoot := t.TempDir()
	explicitCatalog := t.TempDir()

	got, err := resolveCatalogFolder(mosaicRoot, explicitCatalog)
	if err != nil {
		t.Fatalf("resolveCatalogFolder: unexpected error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("resolveCatalogFolder(%q, %q) = %q; want an absolute path; "+
			"the resolved path must always be built with filepath helpers",
			mosaicRoot, explicitCatalog, got)
	}
}

// TestResolveCatalogFolder_ExplicitValue_ByteEqualToAbsOfInput verifies that the
// resolved path is byte-identical to filepath.Abs of the supplied flagValue, when
// flagValue is an existing directory.
func TestResolveCatalogFolder_ExplicitValue_ByteEqualToAbsOfInput(t *testing.T) {
	mosaicRoot := t.TempDir()
	explicitCatalog := t.TempDir()
	absExplicit, _ := filepath.Abs(explicitCatalog)

	got, err := resolveCatalogFolder(mosaicRoot, explicitCatalog)
	if err != nil {
		t.Fatalf("resolveCatalogFolder: unexpected error: %v", err)
	}
	if got != absExplicit {
		t.Errorf("resolveCatalogFolder(%q, %q) = %q, want %q (filepath.Abs of input); "+
			"the resolved path must be byte-identical to filepath.Abs(flagValue)",
			mosaicRoot, explicitCatalog, got, absExplicit)
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Fail-fast validation of explicitly supplied paths
// ---------------------------------------------------------------------------

// TestResolveCatalogFolder_NonexistentExplicitPath_ReturnsError verifies that when an
// explicitly supplied --catalog-folder path does not exist, resolveCatalogFolder returns
// a non-nil error. This is the fail-fast validation that runs before dependency construction.
func TestResolveCatalogFolder_NonexistentExplicitPath_ReturnsError(t *testing.T) {
	mosaicRoot := t.TempDir()
	nonexistent := filepath.Join(t.TempDir(), "does-not-exist-catalog")

	_, err := resolveCatalogFolder(mosaicRoot, nonexistent)
	if err == nil {
		t.Errorf("resolveCatalogFolder(%q, %q): expected error for a nonexistent path, got nil; "+
			"an explicitly supplied path that does not exist must abort before dependency construction",
			mosaicRoot, nonexistent)
	}
}

// TestResolveCatalogFolder_NonexistentExplicitPath_WrapsErrCatalogFolderMissing verifies
// that the error for a nonexistent explicit path wraps catalog.ErrCatalogFolderMissing,
// making it discriminable via errors.Is at the call site in main.
func TestResolveCatalogFolder_NonexistentExplicitPath_WrapsErrCatalogFolderMissing(t *testing.T) {
	mosaicRoot := t.TempDir()
	nonexistent := filepath.Join(t.TempDir(), "does-not-exist-catalog")

	_, err := resolveCatalogFolder(mosaicRoot, nonexistent)
	if err == nil {
		t.Skip("error was nil; the missing-path sentinel wrapping is covered by the _ReturnsError companion")
	}
	if !errors.Is(err, catalog.ErrCatalogFolderMissing) {
		t.Errorf("resolveCatalogFolder with nonexistent path: error = %v; "+
			"want an error wrapping catalog.ErrCatalogFolderMissing (discriminable via errors.Is)",
			err)
	}
}

// TestResolveCatalogFolder_NonexistentExplicitPath_ErrorNamesPath verifies that the
// error message for a nonexistent path contains the offending path string, so the
// user can immediately identify which directory was rejected.
func TestResolveCatalogFolder_NonexistentExplicitPath_ErrorNamesPath(t *testing.T) {
	mosaicRoot := t.TempDir()
	nonexistent := filepath.Join(t.TempDir(), "does-not-exist-catalog")

	_, err := resolveCatalogFolder(mosaicRoot, nonexistent)
	if err == nil {
		t.Skip("error was nil; path-naming is covered by the _ReturnsError companion")
	}
	absNonexistent, _ := filepath.Abs(nonexistent)
	if !strings.Contains(err.Error(), nonexistent) && !strings.Contains(err.Error(), absNonexistent) {
		t.Errorf("resolveCatalogFolder with nonexistent path: error %q does not name the offending path %q; "+
			"the error message must identify the bad path",
			err.Error(), nonexistent)
	}
}

// TestResolveCatalogFolder_FileExplicitPath_ReturnsError verifies that when an explicitly
// supplied --catalog-folder path points to a regular file (not a directory), resolveCatalogFolder
// returns a non-nil error.
func TestResolveCatalogFolder_FileExplicitPath_ReturnsError(t *testing.T) {
	mosaicRoot := t.TempDir()
	filePath := filepath.Join(t.TempDir(), "not-a-directory.txt")
	if err := os.WriteFile(filePath, []byte("not a catalogue"), 0o644); err != nil {
		t.Fatalf("setup: WriteFile: %v", err)
	}

	_, err := resolveCatalogFolder(mosaicRoot, filePath)
	if err == nil {
		t.Errorf("resolveCatalogFolder(%q, %q): expected error for a file path, got nil; "+
			"a path that is a regular file must be rejected before dependency construction",
			mosaicRoot, filePath)
	}
}

// TestResolveCatalogFolder_FileExplicitPath_WrapsErrCatalogFolderNotDirectory verifies
// that the error for a file-path input wraps catalog.ErrCatalogFolderNotDirectory,
// making the failure mode discriminable at the call site.
func TestResolveCatalogFolder_FileExplicitPath_WrapsErrCatalogFolderNotDirectory(t *testing.T) {
	mosaicRoot := t.TempDir()
	filePath := filepath.Join(t.TempDir(), "not-a-directory.txt")
	if err := os.WriteFile(filePath, []byte("not a catalogue"), 0o644); err != nil {
		t.Fatalf("setup: WriteFile: %v", err)
	}

	_, err := resolveCatalogFolder(mosaicRoot, filePath)
	if err == nil {
		t.Skip("error was nil; the not-directory sentinel wrapping is covered by the _ReturnsError companion")
	}
	if !errors.Is(err, catalog.ErrCatalogFolderNotDirectory) {
		t.Errorf("resolveCatalogFolder with file path: error = %v; "+
			"want an error wrapping catalog.ErrCatalogFolderNotDirectory (discriminable via errors.Is)",
			err)
	}
}

// TestResolveCatalogFolder_ValidDirectoryExplicitPath_Succeeds verifies that when an
// explicitly supplied path is an existing directory, resolveCatalogFolder returns nil
// error and the absolute form of the path. A sparse directory (no Agents/, Workflows/)
// is valid — internal structure is never validated.
func TestResolveCatalogFolder_ValidDirectoryExplicitPath_Succeeds(t *testing.T) {
	mosaicRoot := t.TempDir()
	validDir := t.TempDir() // existing but completely empty directory

	got, err := resolveCatalogFolder(mosaicRoot, validDir)
	if err != nil {
		t.Fatalf("resolveCatalogFolder(%q, %q): unexpected error for an existing directory: %v; "+
			"an existing directory (even a sparse one) must succeed",
			mosaicRoot, validDir, err)
	}
	absValid, _ := filepath.Abs(validDir)
	if got != absValid {
		t.Errorf("resolveCatalogFolder(%q, %q) = %q, want %q",
			mosaicRoot, validDir, got, absValid)
	}
}

// TestResolveCatalogFolder_DefaultPath_NotValidated_MissingDefaultIsOK verifies that
// calling resolveCatalogFolder with an empty flagValue does NOT validate the default
// path even when {mosaicRoot}/Catalog does not exist. Only an explicitly supplied path
// is validated; a missing default must not introduce a new failure mode.
func TestResolveCatalogFolder_DefaultPath_NotValidated_MissingDefaultIsOK(t *testing.T) {
	// Create a mosaicRoot whose /Catalog subdirectory does NOT exist.
	mosaicRoot := t.TempDir()
	// Confirm the default catalogue directory truly does not exist.
	defaultCatalogDir := catalog.DefaultCatalogRoot(mosaicRoot)
	if _, err := os.Stat(defaultCatalogDir); !os.IsNotExist(err) {
		t.Skipf("default catalogue directory %q unexpectedly exists; skipping", defaultCatalogDir)
	}

	_, err := resolveCatalogFolder(mosaicRoot, "")
	if err != nil {
		t.Errorf("resolveCatalogFolder(%q, \"\"): expected nil error when the default catalogue "+
			"directory %q does not exist; only explicitly supplied paths are validated, got: %v",
			mosaicRoot, defaultCatalogDir, err)
	}
}

// TestResolveCatalogFolder_TwoDistinctSentinels_AreDistinct verifies that
// ErrCatalogFolderMissing and ErrCatalogFolderNotDirectory are different sentinel
// values, ensuring callers can distinguish the two failure modes.
func TestResolveCatalogFolder_TwoDistinctSentinels_AreDistinct(t *testing.T) {
	if errors.Is(catalog.ErrCatalogFolderMissing, catalog.ErrCatalogFolderNotDirectory) {
		t.Error("ErrCatalogFolderMissing matches ErrCatalogFolderNotDirectory; they must be distinct sentinels")
	}
	if errors.Is(catalog.ErrCatalogFolderNotDirectory, catalog.ErrCatalogFolderMissing) {
		t.Error("ErrCatalogFolderNotDirectory matches ErrCatalogFolderMissing; they must be distinct sentinels")
	}
}
