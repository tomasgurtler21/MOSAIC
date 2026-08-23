package main

// main_test.go verifies that scanGlobalFlags correctly pre-scans global flags needed for
// dependency wiring. The function strips surrounding double-quote characters from path-valued
// flags and returns the raw pre-scan value (before defaulting or validation).
//
// scanGlobalFlags signature: (args []string) (mosaicRoot, catalogFolder, logDir string, allowExternal bool)
//
// Existing tests verify the --mosaic-root behaviour. Tests for --catalog-folder pre-scan
// are in catalog_folder_scan_test.go and tests for --log-dir pre-scan are in
// log_dir_scan_test.go, both in this same package.

import "testing"

// TestScanGlobalFlags_QuotedMosaicRoot_Stripped verifies that a --mosaic-root value
// surrounded by a matched pair of double quotes has those quotes removed before the
// value is returned.
func TestScanGlobalFlags_QuotedMosaicRoot_Stripped(t *testing.T) {
	rawPath := "/path/to/mosaic-root"
	quotedPath := `"` + rawPath + `"`

	got, _, _, _ := scanGlobalFlags([]string{"--mosaic-root", quotedPath, "deploy"})

	if got != rawPath {
		t.Errorf("scanGlobalFlags --mosaic-root %q = %q, want %q; quoted value must be stripped to match the unquoted form",
			quotedPath, got, rawPath)
	}
}

// TestScanGlobalFlags_QuotedMosaicRootEqualForm_Stripped verifies that the --flag=value
// form of --mosaic-root also has surrounding double quotes stripped.
func TestScanGlobalFlags_QuotedMosaicRootEqualForm_Stripped(t *testing.T) {
	rawPath := "/path/to/mosaic-root"
	quotedPath := `"` + rawPath + `"`

	got, _, _, _ := scanGlobalFlags([]string{"--mosaic-root=" + quotedPath, "deploy"})

	if got != rawPath {
		t.Errorf("scanGlobalFlags --mosaic-root=%q = %q, want %q; quoted value in --flag=value form must be stripped",
			quotedPath, got, rawPath)
	}
}

// TestScanGlobalFlags_UnquotedMosaicRoot_PassedThroughUnchanged verifies that a bare
// --mosaic-root value without surrounding quotes is returned unchanged.
func TestScanGlobalFlags_UnquotedMosaicRoot_PassedThroughUnchanged(t *testing.T) {
	rawPath := "/path/to/mosaic-root"

	got, _, _, _ := scanGlobalFlags([]string{"--mosaic-root", rawPath, "deploy"})

	if got != rawPath {
		t.Errorf("scanGlobalFlags --mosaic-root %q = %q, want %q; unquoted value must be returned unchanged",
			rawPath, got, rawPath)
	}
}

// TestScanGlobalFlags_QuotedAndUnquotedMosaicRoot_ProduceIdenticalValues verifies that a
// quoted and an unquoted --mosaic-root value that name the same path yield the same string
// from scanGlobalFlags. This is the core contract: the caller must not observe the
// difference between quoted and unquoted input.
func TestScanGlobalFlags_QuotedAndUnquotedMosaicRoot_ProduceIdenticalValues(t *testing.T) {
	rawPath := "/path/to/mosaic-root"
	quotedPath := `"` + rawPath + `"`

	gotQuoted, _, _, _ := scanGlobalFlags([]string{"--mosaic-root", quotedPath, "deploy"})
	gotUnquoted, _, _, _ := scanGlobalFlags([]string{"--mosaic-root", rawPath, "deploy"})

	if gotQuoted != gotUnquoted {
		t.Errorf("quoted --mosaic-root produced %q, unquoted produced %q; both forms must yield the same value",
			gotQuoted, gotUnquoted)
	}
}
