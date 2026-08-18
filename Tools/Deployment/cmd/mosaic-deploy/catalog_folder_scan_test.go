package main

// catalog_folder_scan_test.go verifies that scanGlobalFlags correctly pre-scans the
// --catalog-folder flag alongside --mosaic-root and --allow-external.
//
// Contract under test (T5.1):
//
//   Pre-scan forms:
//     - "--catalog-folder <value>" (space form) returns the raw value as catalogFolder.
//     - "--catalog-folder=<value>" (equals form) returns the raw value as catalogFolder.
//     - Both forms apply quote-stripping via pathinput.Unquote to the extracted value.
//
//   Absence:
//     - When --catalog-folder is absent, catalogFolder is the empty string. Defaulting
//       to mosaic-root/Catalog is the responsibility of resolveCatalogFolder, not the scan.
//
//   Coexistence:
//     - --catalog-folder can appear alongside --mosaic-root, --allow-external, and a
//       subcommand in any order; all three are extracted independently.
//     - The presence of --catalog-folder does not alter the mosaicRoot or allowExternal results.
//     - The presence of --mosaic-root does not alter the catalogFolder result.
//
// NOTE: These tests reference scanGlobalFlags with its Stage-5 signature:
//
//     func scanGlobalFlags(args []string) (mosaicRoot, catalogFolder string, allowExternal bool)
//
// The current implementation returns only (mosaicRoot string, allowExternal bool). Until
// I5.1 updates the signature to include catalogFolder, this file will not compile — that
// is the expected TDD RED compilation failure.

import "testing"

// ---------------------------------------------------------------------------
// Space form: --catalog-folder <value>
// ---------------------------------------------------------------------------

// TestScanGlobalFlags_CatalogFolder_SpaceForm_Returned verifies that
// "--catalog-folder <value>" (space-separated form) causes scanGlobalFlags to return
// the value as the catalogFolder result.
func TestScanGlobalFlags_CatalogFolder_SpaceForm_Returned(t *testing.T) {
	rawPath := "/path/to/my-catalog"

	_, got, _ := scanGlobalFlags([]string{"--catalog-folder", rawPath, "deploy"})

	if got != rawPath {
		t.Errorf("scanGlobalFlags --catalog-folder %q: catalogFolder = %q, want %q; "+
			"space-form value must be returned as catalogFolder",
			rawPath, got, rawPath)
	}
}

// TestScanGlobalFlags_CatalogFolder_SpaceForm_QuotedValueStripped verifies that a
// --catalog-folder value wrapped in a matched pair of double quotes has those quotes
// removed, matching the unquoting behaviour applied to --mosaic-root.
func TestScanGlobalFlags_CatalogFolder_SpaceForm_QuotedValueStripped(t *testing.T) {
	rawPath := "/path/to/my-catalog"
	quotedPath := `"` + rawPath + `"`

	_, got, _ := scanGlobalFlags([]string{"--catalog-folder", quotedPath, "deploy"})

	if got != rawPath {
		t.Errorf("scanGlobalFlags --catalog-folder %q: catalogFolder = %q, want %q; "+
			"surrounding double quotes must be stripped from the value",
			quotedPath, got, rawPath)
	}
}

// TestScanGlobalFlags_CatalogFolder_SpaceForm_QuotedAndUnquotedAreIdentical verifies
// that a quoted path and its bare equivalent both yield the same catalogFolder string.
// This is the core unquoting contract: callers must not observe whether the user
// surrounded the path in quotes.
func TestScanGlobalFlags_CatalogFolder_SpaceForm_QuotedAndUnquotedAreIdentical(t *testing.T) {
	rawPath := "/path/to/my-catalog"
	quotedPath := `"` + rawPath + `"`

	_, gotQuoted, _ := scanGlobalFlags([]string{"--catalog-folder", quotedPath, "deploy"})
	_, gotUnquoted, _ := scanGlobalFlags([]string{"--catalog-folder", rawPath, "deploy"})

	if gotQuoted != gotUnquoted {
		t.Errorf("scanGlobalFlags --catalog-folder: quoted form produced %q, "+
			"unquoted produced %q; both must yield the same catalogFolder",
			gotQuoted, gotUnquoted)
	}
}

// ---------------------------------------------------------------------------
// Equals form: --catalog-folder=<value>
// ---------------------------------------------------------------------------

// TestScanGlobalFlags_CatalogFolder_EqualsForm_Returned verifies that the
// "--catalog-folder=<value>" (equals-form) causes scanGlobalFlags to return the value
// as the catalogFolder result, matching the behaviour of --mosaic-root=<value>.
func TestScanGlobalFlags_CatalogFolder_EqualsForm_Returned(t *testing.T) {
	rawPath := "/path/to/my-catalog"

	_, got, _ := scanGlobalFlags([]string{"--catalog-folder=" + rawPath, "deploy"})

	if got != rawPath {
		t.Errorf("scanGlobalFlags --catalog-folder=%q: catalogFolder = %q, want %q; "+
			"equals-form value must be returned as catalogFolder",
			rawPath, got, rawPath)
	}
}

// TestScanGlobalFlags_CatalogFolder_EqualsForm_QuotedValueStripped verifies that a
// "--catalog-folder=<value>" value wrapped in double quotes has the quotes removed.
func TestScanGlobalFlags_CatalogFolder_EqualsForm_QuotedValueStripped(t *testing.T) {
	rawPath := "/path/to/my-catalog"
	quotedPath := `"` + rawPath + `"`

	_, got, _ := scanGlobalFlags([]string{"--catalog-folder=" + quotedPath, "deploy"})

	if got != rawPath {
		t.Errorf("scanGlobalFlags --catalog-folder=%q: catalogFolder = %q, want %q; "+
			"surrounding double quotes must be stripped in equals form",
			quotedPath, got, rawPath)
	}
}

// TestScanGlobalFlags_CatalogFolder_EqualsForm_SpaceFormYieldSameResult verifies that
// the space form and the equals form of --catalog-folder produce the same catalogFolder
// string from scanGlobalFlags, for the same bare path value.
func TestScanGlobalFlags_CatalogFolder_EqualsForm_SpaceFormYieldSameResult(t *testing.T) {
	rawPath := "/path/to/my-catalog"

	_, gotSpace, _ := scanGlobalFlags([]string{"--catalog-folder", rawPath, "deploy"})
	_, gotEquals, _ := scanGlobalFlags([]string{"--catalog-folder=" + rawPath, "deploy"})

	if gotSpace != gotEquals {
		t.Errorf("scanGlobalFlags: space form produced %q, equals form produced %q; "+
			"both forms of --catalog-folder must yield the same catalogFolder",
			gotSpace, gotEquals)
	}
}

// ---------------------------------------------------------------------------
// Absence: flag not supplied
// ---------------------------------------------------------------------------

// TestScanGlobalFlags_CatalogFolder_Absent_ReturnsEmpty verifies that when
// --catalog-folder is not present in the argument list, catalogFolder is the empty
// string. Defaulting to {mosaic-root}/Catalog is the responsibility of
// resolveCatalogFolder, not of the pre-scan.
func TestScanGlobalFlags_CatalogFolder_Absent_ReturnsEmpty(t *testing.T) {
	_, got, _ := scanGlobalFlags([]string{"deploy", "--harness", "stub"})

	if got != "" {
		t.Errorf("scanGlobalFlags without --catalog-folder: catalogFolder = %q, want \"\"; "+
			"the pre-scan must return an empty string when the flag is absent",
			got)
	}
}

// ---------------------------------------------------------------------------
// Coexistence with --mosaic-root and --allow-external
// ---------------------------------------------------------------------------

// TestScanGlobalFlags_CatalogFolder_CoexistsWithMosaicRoot verifies that
// --catalog-folder and --mosaic-root can appear together in the argument list and
// both are extracted independently and correctly.
func TestScanGlobalFlags_CatalogFolder_CoexistsWithMosaicRoot(t *testing.T) {
	mosaicRootPath := "/my/mosaic-root"
	catalogFolderPath := "/my/catalog"

	gotMosaicRoot, gotCatalogFolder, _ := scanGlobalFlags([]string{
		"--mosaic-root", mosaicRootPath,
		"--catalog-folder", catalogFolderPath,
		"deploy",
	})

	if gotMosaicRoot != mosaicRootPath {
		t.Errorf("scanGlobalFlags: mosaicRoot = %q, want %q; "+
			"--mosaic-root must be unaffected by the presence of --catalog-folder",
			gotMosaicRoot, mosaicRootPath)
	}
	if gotCatalogFolder != catalogFolderPath {
		t.Errorf("scanGlobalFlags: catalogFolder = %q, want %q; "+
			"--catalog-folder must be extracted correctly alongside --mosaic-root",
			gotCatalogFolder, catalogFolderPath)
	}
}

// TestScanGlobalFlags_CatalogFolder_CoexistsWithAllowExternal verifies that
// --catalog-folder and --allow-external can appear together; both are extracted
// independently.
func TestScanGlobalFlags_CatalogFolder_CoexistsWithAllowExternal(t *testing.T) {
	catalogFolderPath := "/my/catalog"

	_, gotCatalogFolder, gotAllowExternal := scanGlobalFlags([]string{
		"--catalog-folder", catalogFolderPath,
		"--allow-external",
		"deploy",
	})

	if !gotAllowExternal {
		t.Errorf("scanGlobalFlags: allowExternal = false, want true; " +
			"--allow-external must be recognized alongside --catalog-folder")
	}
	if gotCatalogFolder != catalogFolderPath {
		t.Errorf("scanGlobalFlags: catalogFolder = %q, want %q; "+
			"--catalog-folder must be extracted correctly alongside --allow-external",
			gotCatalogFolder, catalogFolderPath)
	}
}

// TestScanGlobalFlags_AllThreeGlobalFlags_AllExtracted verifies that
// --mosaic-root, --catalog-folder, and --allow-external can all appear together in
// the argument list and all three are extracted correctly and independently.
func TestScanGlobalFlags_AllThreeGlobalFlags_AllExtracted(t *testing.T) {
	mosaicRootPath := "/my/mosaic-root"
	catalogFolderPath := "/my/catalog"

	gotMosaicRoot, gotCatalogFolder, gotAllowExternal := scanGlobalFlags([]string{
		"--mosaic-root", mosaicRootPath,
		"--catalog-folder", catalogFolderPath,
		"--allow-external",
		"deploy",
	})

	if gotMosaicRoot != mosaicRootPath {
		t.Errorf("mosaicRoot = %q, want %q", gotMosaicRoot, mosaicRootPath)
	}
	if gotCatalogFolder != catalogFolderPath {
		t.Errorf("catalogFolder = %q, want %q", gotCatalogFolder, catalogFolderPath)
	}
	if !gotAllowExternal {
		t.Errorf("allowExternal = false, want true")
	}
}

// TestScanGlobalFlags_CatalogFolder_DoesNotAffectMosaicRootResult verifies that
// adding --catalog-folder to an argument list does not change the mosaicRoot
// returned for the same --mosaic-root value.
func TestScanGlobalFlags_CatalogFolder_DoesNotAffectMosaicRootResult(t *testing.T) {
	mosaicRootPath := "/my/mosaic-root"
	catalogFolderPath := "/my/catalog"

	withoutCatalogFolder, _, _ := scanGlobalFlags([]string{"--mosaic-root", mosaicRootPath, "deploy"})
	withCatalogFolder, _, _ := scanGlobalFlags([]string{
		"--mosaic-root", mosaicRootPath,
		"--catalog-folder", catalogFolderPath,
		"deploy",
	})

	if withoutCatalogFolder != withCatalogFolder {
		t.Errorf("mosaicRoot changes when --catalog-folder is added: "+
			"without = %q, with = %q; "+
			"--catalog-folder must not influence the mosaicRoot result",
			withoutCatalogFolder, withCatalogFolder)
	}
}

// TestScanGlobalFlags_CatalogFolder_EqualsForm_CoexistsWithMosaicRootEqualsForm verifies
// that both flags can use the equals form simultaneously.
func TestScanGlobalFlags_CatalogFolder_EqualsForm_CoexistsWithMosaicRootEqualsForm(t *testing.T) {
	mosaicRootPath := "/my/mosaic-root"
	catalogFolderPath := "/my/catalog"

	gotMosaicRoot, gotCatalogFolder, _ := scanGlobalFlags([]string{
		"--mosaic-root=" + mosaicRootPath,
		"--catalog-folder=" + catalogFolderPath,
		"deploy",
	})

	if gotMosaicRoot != mosaicRootPath {
		t.Errorf("mosaicRoot = %q, want %q (equals form)", gotMosaicRoot, mosaicRootPath)
	}
	if gotCatalogFolder != catalogFolderPath {
		t.Errorf("catalogFolder = %q, want %q (equals form)", gotCatalogFolder, catalogFolderPath)
	}
}

// ---------------------------------------------------------------------------
// Reversed-order coexistence
// ---------------------------------------------------------------------------

// TestScanGlobalFlags_AllThreeGlobalFlags_ReversedOrder_AllExtracted verifies that
// --allow-external, --catalog-folder, and --mosaic-root are all extracted correctly
// when they appear in reversed order relative to the forward-order tests above.
// The doc comment on this file claims "any order" is supported; this test backs
// that claim for the fully reversed permutation.
func TestScanGlobalFlags_AllThreeGlobalFlags_ReversedOrder_AllExtracted(t *testing.T) {
	mosaicRootPath := "/my/mosaic-root"
	catalogFolderPath := "/my/catalog"

	// Reversed: --allow-external first, then --catalog-folder, then --mosaic-root,
	// followed by the subcommand. The forward-order test uses the opposite sequence.
	gotMosaicRoot, gotCatalogFolder, gotAllowExternal := scanGlobalFlags([]string{
		"--allow-external",
		"--catalog-folder", catalogFolderPath,
		"--mosaic-root", mosaicRootPath,
		"deploy",
	})

	if gotMosaicRoot != mosaicRootPath {
		t.Errorf("mosaicRoot = %q, want %q (reversed order)", gotMosaicRoot, mosaicRootPath)
	}
	if gotCatalogFolder != catalogFolderPath {
		t.Errorf("catalogFolder = %q, want %q (reversed order)", gotCatalogFolder, catalogFolderPath)
	}
	if !gotAllowExternal {
		t.Errorf("allowExternal = false, want true (reversed order); "+
			"all three global flags must be extracted regardless of their order in the argument list")
	}
}
