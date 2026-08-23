package main

// log_dir_scan_test.go verifies that scanGlobalFlags correctly pre-scans the
// --log-dir flag alongside the other global flags (T7.3).
//
// NOTE: These tests reference scanGlobalFlags with the Stage-7 signature:
//
//     func scanGlobalFlags(args []string) (mosaicRoot, catalogFolder, logDir string, allowExternal bool)
//
// The current implementation returns only (mosaicRoot, catalogFolder string, allowExternal bool).
// Until the implementation adds logDir to the return, this file will not compile —
// that is the expected TDD RED compilation failure.
//
// Behaviours asserted:
//
//   - "--log-dir <value>" (space form) returns the value as logDir.
//   - "--log-dir=<value>" (equals form) returns the value as logDir.
//   - Both forms strip surrounding double quotes from the value, matching the
//     unquoting behaviour of --mosaic-root and --catalog-folder.
//   - When --log-dir is absent, logDir is the empty string.
//   - --log-dir coexists with --mosaic-root, --catalog-folder, and
//     --allow-external in any order; all four are extracted independently.

import "testing"

// ---------------------------------------------------------------------------
// Space form: --log-dir <value>
// ---------------------------------------------------------------------------

// TestScanGlobalFlags_LogDir_SpaceForm_Returned verifies that "--log-dir <value>"
// (space-separated form) causes scanGlobalFlags to return the value as logDir.
func TestScanGlobalFlags_LogDir_SpaceForm_Returned(t *testing.T) {
	rawPath := "/path/to/log-dir"

	_, _, got, _ := scanGlobalFlags([]string{"--log-dir", rawPath, "deploy"})

	if got != rawPath {
		t.Errorf("scanGlobalFlags --log-dir %q: logDir = %q, want %q; "+
			"space-form value must be returned as logDir",
			rawPath, got, rawPath)
	}
}

// TestScanGlobalFlags_LogDir_SpaceForm_QuotedValueStripped verifies that a
// --log-dir value wrapped in a matched pair of double quotes has those quotes
// removed, matching the unquoting behaviour applied to --mosaic-root and
// --catalog-folder.
func TestScanGlobalFlags_LogDir_SpaceForm_QuotedValueStripped(t *testing.T) {
	rawPath := "/path/to/log-dir"
	quotedPath := `"` + rawPath + `"`

	_, _, got, _ := scanGlobalFlags([]string{"--log-dir", quotedPath, "deploy"})

	if got != rawPath {
		t.Errorf("scanGlobalFlags --log-dir %q: logDir = %q, want %q; "+
			"surrounding double quotes must be stripped from the value",
			quotedPath, got, rawPath)
	}
}

// TestScanGlobalFlags_LogDir_SpaceForm_QuotedAndUnquotedAreIdentical verifies
// that a quoted path and its bare equivalent both yield the same logDir string.
// This is the core unquoting contract: callers must not observe whether the user
// surrounded the path in quotes.
func TestScanGlobalFlags_LogDir_SpaceForm_QuotedAndUnquotedAreIdentical(t *testing.T) {
	rawPath := "/path/to/log-dir"
	quotedPath := `"` + rawPath + `"`

	_, _, gotQuoted, _ := scanGlobalFlags([]string{"--log-dir", quotedPath, "deploy"})
	_, _, gotUnquoted, _ := scanGlobalFlags([]string{"--log-dir", rawPath, "deploy"})

	if gotQuoted != gotUnquoted {
		t.Errorf("scanGlobalFlags --log-dir: quoted form produced %q, "+
			"unquoted produced %q; both must yield the same logDir",
			gotQuoted, gotUnquoted)
	}
}

// ---------------------------------------------------------------------------
// Equals form: --log-dir=<value>
// ---------------------------------------------------------------------------

// TestScanGlobalFlags_LogDir_EqualsForm_Returned verifies that
// "--log-dir=<value>" (equals form) causes scanGlobalFlags to return the value as
// logDir, matching the behaviour of --mosaic-root=<value>.
func TestScanGlobalFlags_LogDir_EqualsForm_Returned(t *testing.T) {
	rawPath := "/path/to/log-dir"

	_, _, got, _ := scanGlobalFlags([]string{"--log-dir=" + rawPath, "deploy"})

	if got != rawPath {
		t.Errorf("scanGlobalFlags --log-dir=%q: logDir = %q, want %q; "+
			"equals-form value must be returned as logDir",
			rawPath, got, rawPath)
	}
}

// TestScanGlobalFlags_LogDir_EqualsForm_QuotedValueStripped verifies that a
// "--log-dir=<value>" value wrapped in double quotes has the quotes removed.
func TestScanGlobalFlags_LogDir_EqualsForm_QuotedValueStripped(t *testing.T) {
	rawPath := "/path/to/log-dir"
	quotedPath := `"` + rawPath + `"`

	_, _, got, _ := scanGlobalFlags([]string{"--log-dir=" + quotedPath, "deploy"})

	if got != rawPath {
		t.Errorf("scanGlobalFlags --log-dir=%q: logDir = %q, want %q; "+
			"surrounding double quotes must be stripped in equals form",
			quotedPath, got, rawPath)
	}
}

// TestScanGlobalFlags_LogDir_EqualsForm_SpaceFormYieldSameResult verifies that the
// space form and the equals form of --log-dir produce the same logDir string, for
// the same bare path value.
func TestScanGlobalFlags_LogDir_EqualsForm_SpaceFormYieldSameResult(t *testing.T) {
	rawPath := "/path/to/log-dir"

	_, _, gotSpace, _ := scanGlobalFlags([]string{"--log-dir", rawPath, "deploy"})
	_, _, gotEquals, _ := scanGlobalFlags([]string{"--log-dir=" + rawPath, "deploy"})

	if gotSpace != gotEquals {
		t.Errorf("scanGlobalFlags: space form produced %q, equals form produced %q; "+
			"both forms of --log-dir must yield the same logDir",
			gotSpace, gotEquals)
	}
}

// ---------------------------------------------------------------------------
// Absence: flag not supplied
// ---------------------------------------------------------------------------

// TestScanGlobalFlags_LogDir_Absent_ReturnsEmpty verifies that when --log-dir is
// not present in the argument list, logDir is the empty string. Using the default
// log location is the logger's responsibility, not the pre-scan's.
func TestScanGlobalFlags_LogDir_Absent_ReturnsEmpty(t *testing.T) {
	_, _, got, _ := scanGlobalFlags([]string{"deploy", "--harness", "stub"})

	if got != "" {
		t.Errorf("scanGlobalFlags without --log-dir: logDir = %q, want \"\"; "+
			"the pre-scan must return an empty string when the flag is absent",
			got)
	}
}

// ---------------------------------------------------------------------------
// Coexistence with other global flags
// ---------------------------------------------------------------------------

// TestScanGlobalFlags_LogDir_CoexistsWithMosaicRoot verifies that --log-dir and
// --mosaic-root can appear together and both are extracted independently and
// correctly.
func TestScanGlobalFlags_LogDir_CoexistsWithMosaicRoot(t *testing.T) {
	mosaicRootPath := "/my/mosaic-root"
	logDirPath := "/my/log-dir"

	gotMosaicRoot, _, gotLogDir, _ := scanGlobalFlags([]string{
		"--mosaic-root", mosaicRootPath,
		"--log-dir", logDirPath,
		"deploy",
	})

	if gotMosaicRoot != mosaicRootPath {
		t.Errorf("mosaicRoot = %q, want %q; --mosaic-root must be unaffected by --log-dir",
			gotMosaicRoot, mosaicRootPath)
	}
	if gotLogDir != logDirPath {
		t.Errorf("logDir = %q, want %q; --log-dir must be extracted alongside --mosaic-root",
			gotLogDir, logDirPath)
	}
}

// TestScanGlobalFlags_LogDir_CoexistsWithCatalogFolder verifies that --log-dir and
// --catalog-folder can appear together and both are extracted independently.
func TestScanGlobalFlags_LogDir_CoexistsWithCatalogFolder(t *testing.T) {
	catalogFolderPath := "/my/catalog"
	logDirPath := "/my/log-dir"

	_, gotCatalogFolder, gotLogDir, _ := scanGlobalFlags([]string{
		"--catalog-folder", catalogFolderPath,
		"--log-dir", logDirPath,
		"deploy",
	})

	if gotCatalogFolder != catalogFolderPath {
		t.Errorf("catalogFolder = %q, want %q; --catalog-folder must be unaffected by --log-dir",
			gotCatalogFolder, catalogFolderPath)
	}
	if gotLogDir != logDirPath {
		t.Errorf("logDir = %q, want %q; --log-dir must be extracted alongside --catalog-folder",
			gotLogDir, logDirPath)
	}
}

// TestScanGlobalFlags_LogDir_CoexistsWithAllowExternal verifies that --log-dir and
// --allow-external can appear together and both are extracted independently.
func TestScanGlobalFlags_LogDir_CoexistsWithAllowExternal(t *testing.T) {
	logDirPath := "/my/log-dir"

	_, _, gotLogDir, gotAllowExternal := scanGlobalFlags([]string{
		"--log-dir", logDirPath,
		"--allow-external",
		"deploy",
	})

	if !gotAllowExternal {
		t.Errorf("allowExternal = false, want true; --allow-external must be recognised alongside --log-dir")
	}
	if gotLogDir != logDirPath {
		t.Errorf("logDir = %q, want %q; --log-dir must be extracted alongside --allow-external",
			gotLogDir, logDirPath)
	}
}

// TestScanGlobalFlags_AllFourGlobalFlags_AllExtracted verifies that
// --mosaic-root, --catalog-folder, --log-dir, and --allow-external can all appear
// together and all four are extracted correctly and independently.
func TestScanGlobalFlags_AllFourGlobalFlags_AllExtracted(t *testing.T) {
	mosaicRootPath := "/my/mosaic-root"
	catalogFolderPath := "/my/catalog"
	logDirPath := "/my/log-dir"

	gotMosaicRoot, gotCatalogFolder, gotLogDir, gotAllowExternal := scanGlobalFlags([]string{
		"--mosaic-root", mosaicRootPath,
		"--catalog-folder", catalogFolderPath,
		"--log-dir", logDirPath,
		"--allow-external",
		"deploy",
	})

	if gotMosaicRoot != mosaicRootPath {
		t.Errorf("mosaicRoot = %q, want %q", gotMosaicRoot, mosaicRootPath)
	}
	if gotCatalogFolder != catalogFolderPath {
		t.Errorf("catalogFolder = %q, want %q", gotCatalogFolder, catalogFolderPath)
	}
	if gotLogDir != logDirPath {
		t.Errorf("logDir = %q, want %q", gotLogDir, logDirPath)
	}
	if !gotAllowExternal {
		t.Errorf("allowExternal = false, want true")
	}
}

// TestScanGlobalFlags_LogDir_DoesNotAffectMosaicRootResult verifies that adding
// --log-dir to an argument list does not change the mosaicRoot returned for the
// same --mosaic-root value.
func TestScanGlobalFlags_LogDir_DoesNotAffectMosaicRootResult(t *testing.T) {
	mosaicRootPath := "/my/mosaic-root"
	logDirPath := "/my/log-dir"

	withoutLogDir, _, _, _ := scanGlobalFlags([]string{"--mosaic-root", mosaicRootPath, "deploy"})
	withLogDir, _, _, _ := scanGlobalFlags([]string{
		"--mosaic-root", mosaicRootPath,
		"--log-dir", logDirPath,
		"deploy",
	})

	if withoutLogDir != withLogDir {
		t.Errorf("mosaicRoot changes when --log-dir is added: without = %q, with = %q; "+
			"--log-dir must not influence the mosaicRoot result",
			withoutLogDir, withLogDir)
	}
}

// TestScanGlobalFlags_LogDir_DoesNotAffectCatalogFolderResult verifies that adding
// --log-dir to an argument list does not change the catalogFolder returned for the
// same --catalog-folder value.
func TestScanGlobalFlags_LogDir_DoesNotAffectCatalogFolderResult(t *testing.T) {
	catalogFolderPath := "/my/catalog"
	logDirPath := "/my/log-dir"

	_, withoutLogDir, _, _ := scanGlobalFlags([]string{"--catalog-folder", catalogFolderPath, "deploy"})
	_, withLogDir, _, _ := scanGlobalFlags([]string{
		"--catalog-folder", catalogFolderPath,
		"--log-dir", logDirPath,
		"deploy",
	})

	if withoutLogDir != withLogDir {
		t.Errorf("catalogFolder changes when --log-dir is added: without = %q, with = %q; "+
			"--log-dir must not influence the catalogFolder result",
			withoutLogDir, withLogDir)
	}
}

// TestScanGlobalFlags_AllFourGlobalFlags_ReversedOrder_AllExtracted verifies that
// all four global flags are extracted correctly when they appear in reversed order
// relative to the forward-order test above.
func TestScanGlobalFlags_AllFourGlobalFlags_ReversedOrder_AllExtracted(t *testing.T) {
	mosaicRootPath := "/my/mosaic-root"
	catalogFolderPath := "/my/catalog"
	logDirPath := "/my/log-dir"

	gotMosaicRoot, gotCatalogFolder, gotLogDir, gotAllowExternal := scanGlobalFlags([]string{
		"--allow-external",
		"--log-dir", logDirPath,
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
	if gotLogDir != logDirPath {
		t.Errorf("logDir = %q, want %q (reversed order)", gotLogDir, logDirPath)
	}
	if !gotAllowExternal {
		t.Errorf("allowExternal = false, want true (reversed order); "+
			"all four global flags must be extracted regardless of their order in the argument list")
	}
}

// TestScanGlobalFlags_LogDir_EqualsForm_CoexistsWithOtherEqualsForm verifies that
// --log-dir and the other path-valued flags can all use the equals form
// simultaneously.
func TestScanGlobalFlags_LogDir_EqualsForm_CoexistsWithOtherEqualsForm(t *testing.T) {
	mosaicRootPath := "/my/mosaic-root"
	catalogFolderPath := "/my/catalog"
	logDirPath := "/my/log-dir"

	gotMosaicRoot, gotCatalogFolder, gotLogDir, _ := scanGlobalFlags([]string{
		"--mosaic-root=" + mosaicRootPath,
		"--catalog-folder=" + catalogFolderPath,
		"--log-dir=" + logDirPath,
		"deploy",
	})

	if gotMosaicRoot != mosaicRootPath {
		t.Errorf("mosaicRoot = %q, want %q (equals form)", gotMosaicRoot, mosaicRootPath)
	}
	if gotCatalogFolder != catalogFolderPath {
		t.Errorf("catalogFolder = %q, want %q (equals form)", gotCatalogFolder, catalogFolderPath)
	}
	if gotLogDir != logDirPath {
		t.Errorf("logDir = %q, want %q (equals form)", gotLogDir, logDirPath)
	}
}
