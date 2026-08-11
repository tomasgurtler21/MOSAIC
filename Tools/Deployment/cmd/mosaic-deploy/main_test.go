package main

// main_test.go verifies that scanGlobalFlags strips one surrounding pair of double-quote
// characters from the --mosaic-root value. A quoted path and its bare equivalent must
// produce the same string so that dependency wiring in main() receives a clean path
// regardless of whether the user quoted the flag on the command line.

import "testing"

// TestScanGlobalFlags_QuotedMosaicRoot_Stripped verifies that a --mosaic-root value
// surrounded by a matched pair of double quotes has those quotes removed before the
// value is returned.
func TestScanGlobalFlags_QuotedMosaicRoot_Stripped(t *testing.T) {
	rawPath := "/path/to/mosaic-root"
	quotedPath := `"` + rawPath + `"`

	got, _ := scanGlobalFlags([]string{"--mosaic-root", quotedPath, "deploy"})

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

	got, _ := scanGlobalFlags([]string{"--mosaic-root=" + quotedPath, "deploy"})

	if got != rawPath {
		t.Errorf("scanGlobalFlags --mosaic-root=%q = %q, want %q; quoted value in --flag=value form must be stripped",
			quotedPath, got, rawPath)
	}
}

// TestScanGlobalFlags_UnquotedMosaicRoot_PassedThroughUnchanged verifies that a bare
// --mosaic-root value without surrounding quotes is returned unchanged.
func TestScanGlobalFlags_UnquotedMosaicRoot_PassedThroughUnchanged(t *testing.T) {
	rawPath := "/path/to/mosaic-root"

	got, _ := scanGlobalFlags([]string{"--mosaic-root", rawPath, "deploy"})

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

	gotQuoted, _ := scanGlobalFlags([]string{"--mosaic-root", quotedPath, "deploy"})
	gotUnquoted, _ := scanGlobalFlags([]string{"--mosaic-root", rawPath, "deploy"})

	if gotQuoted != gotUnquoted {
		t.Errorf("quoted --mosaic-root produced %q, unquoted produced %q; both forms must yield the same value",
			gotQuoted, gotUnquoted)
	}
}
