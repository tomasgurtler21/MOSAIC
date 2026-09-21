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
//
// T2.8a lives here: a package main test that verifies both translator format IDs resolve
// through the composition root (main.go's blank import of agentformat/all). See the T2.8a
// test functions below for the reasoning.

import (
	"testing"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/agentformat/formatid"
	"mosaic-deploy/internal/harness/registry"
)

// IMPORT BLOCK NOTE: The agentformat and formatid imports above are load-bearing for
// TestCompositionRoot_BothFormatIDsResolvable and TestCompositionRoot_CodexHarnessResolvable.
// Do NOT add agentformat/all, codextoml, or internal/harness/builtin/codex to this
// file's imports. This test works because:
//   - main.go's blank import of agentformat/all is what links the CodexTOML translator;
//   - main.go's blank import of internal/harness/builtin/codex is what registers the
//     Codex harness in the harness registry.
// Deleting either blank import from main.go is the only way to make the corresponding
// tests fail; adding either package to THIS file's imports would silently disarm the
// test by providing an alternative registration path.

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

// --- T2.8a: Composition-root registration tests ---
//
// These tests verify that both the Markdown and CodexTOML format IDs resolve through
// Lookup when this test binary is compiled as package main alongside main.go. The
// CodexTOML translator reaches the binary only through main.go's blank import of
// agentformat/all; this test file does not import that package, so removing the blank
// import from main.go is the only way to make these tests fail.
//
// This property is what distinguishes T2.8a from T2.8 in registration_test.go: T2.8
// imports agentformat/all itself, so it stays green if main.go's import is deleted.
// T2.8a cannot stay green without main.go's import.

// TestCompositionRoot_MarkdownFormatIDResolvable verifies that the Markdown format ID
// resolves through Lookup when this test binary is built with main.go. Markdown
// self-registers from the agentformat package's own init(); this test confirms the
// composition root wires the translator layer at all.
func TestCompositionRoot_MarkdownFormatIDResolvable(t *testing.T) {
	tr, err := agentformat.Lookup(formatid.Markdown)
	if err != nil {
		t.Fatalf("Lookup(Markdown) returned error: %v", err)
	}
	if tr == nil {
		t.Fatal("Lookup(Markdown) returned nil translator with nil error")
	}
}

// TestCompositionRoot_CodexTOMLFormatIDResolvable verifies that the CodexTOML format
// ID resolves through Lookup when this test binary is built with main.go. The CodexTOML
// translator is registered by main.go's blank import of agentformat/all; this test
// fails if that import is deleted.
//
// IMPORT BLOCK IS LOAD-BEARING: this file must not import agentformat/all or codextoml.
// Adding either here would provide an alternative registration path and silently disarm
// this test -- it would then stay green even with the blank import deleted from main.go.
func TestCompositionRoot_CodexTOMLFormatIDResolvable(t *testing.T) {
	tr, err := agentformat.Lookup(formatid.CodexTOML)
	if err != nil {
		t.Fatalf("Lookup(CodexTOML) returned error: %v; want non-nil translator -- is the blank import of agentformat/all present in main.go?", err)
	}
	if tr == nil {
		t.Fatal("Lookup(CodexTOML) returned nil translator with nil error")
	}
}

// TestCompositionRoot_BothFormatIDsResolvable verifies in a single sweep that every
// format ID in the vocabulary resolves through Lookup via the composition root. This
// is the consolidated assertion that AC2.7(b) pins.
func TestCompositionRoot_BothFormatIDsResolvable(t *testing.T) {
	for _, id := range formatid.All() {
		id := id
		t.Run(string(id), func(t *testing.T) {
			tr, err := agentformat.Lookup(id)
			if err != nil {
				t.Fatalf("Lookup(%q) returned error: %v; want non-nil translator", id, err)
			}
			if tr == nil {
				t.Fatalf("Lookup(%q) returned nil translator with nil error", id)
			}
		})
	}
}

// --- Codex harness blank-import guard ---
//
// TestCompositionRoot_CodexHarnessResolvable verifies that the Codex harness ID
// resolves in the harness registry when this test binary is compiled as package main
// alongside main.go. The Codex harness module reaches the binary only through main.go's
// blank import of internal/harness/builtin/codex; this test file does not import that
// package, so removing the blank import from main.go is the only way to make this test
// fail.
//
// IMPORT BLOCK IS LOAD-BEARING: this file must not import
// internal/harness/builtin/codex. Adding it here would provide an alternative
// registration path and silently disarm this test -- it would then stay green even
// with the blank import deleted from main.go.
//
// This is the harness-registry counterpart to TestCompositionRoot_CodexTOMLFormatIDResolvable
// for the translator registry: one test guards each blank import in main.go.

// TestCompositionRoot_CodexHarnessResolvable verifies that the "codex" harness ID is
// registered in the harness registry when this test binary is built with main.go. The
// Codex harness is registered by main.go's blank import of internal/harness/builtin/codex;
// this test fails if that import is deleted.
//
// The test discovers harnesses over a temp directory (no on-disk harnesses needed);
// the built-in registration from the blank import is sufficient. Because the Codex
// built-in requires its injection content files to initialise, a failure to load those
// files causes Usable:false with a non-empty UnusableReason rather than Resolve returning
// an error. The test therefore only asserts that the harness ID appears in the registry
// list -- confirming the blank import ran its init() -- and does not require Usable:true,
// which depends on the live Catalog directory that may not exist in all test environments.
func TestCompositionRoot_CodexHarnessResolvable(t *testing.T) {
	// Discover with a temp root so the registry contains only built-ins.
	// The Codex harness will be present because main.go's blank import registers it.
	root := t.TempDir()
	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("registry.Discover: %v", err)
	}

	// Find "codex" in the list. Its presence proves the blank import ran.
	found := false
	for _, ref := range reg.List() {
		if ref.ID == "codex" {
			found = true
			break
		}
	}
	if !found {
		t.Error("harness ID \"codex\" is absent from registry.List(); " +
			"want it present -- is the blank import of internal/harness/builtin/codex present in main.go?")
	}
}
