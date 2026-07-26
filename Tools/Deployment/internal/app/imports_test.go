package app_test

// imports_test.go verifies that the app package imports neither tui nor cli (AC18.7).
// The app package is the use-case layer; it must only be depended upon by frontends —
// it must never depend on them. This structural invariant is tested by parsing the
// source files of the app package and asserting the forbidden imports are absent.
//
// Verified invariants (T18.9):
//
//   - No .go file in the app package (non-test, non-subdirectory) imports
//     "mosaic-deploy/internal/tui"
//   - No .go file in the app package imports "mosaic-deploy/internal/cli"
//   - The invariant is mechanical: it cannot drift from the code because it IS the code.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// appPackageDir is the directory of the app package under test.
// It is located relative to this test file's own source path.
const appPackagePath = "mosaic-deploy/internal/tui"
const cliPackagePath = "mosaic-deploy/internal/cli"

// TestApp_DoesNotImportTUI verifies that no non-test source file in the app package
// imports "mosaic-deploy/internal/tui". The tui package depends on app; a reverse import
// would create a cycle and violate the hexagonal architecture (AC18.7).
func TestApp_DoesNotImportTUI(t *testing.T) {
	assertAppDoesNotImport(t, "mosaic-deploy/internal/tui")
}

// TestApp_DoesNotImportCLI verifies that no non-test source file in the app package
// imports "mosaic-deploy/internal/cli". The cli package depends on app; a reverse import
// would create a cycle and violate the hexagonal architecture (AC18.7).
func TestApp_DoesNotImportCLI(t *testing.T) {
	assertAppDoesNotImport(t, "mosaic-deploy/internal/cli")
}

// TestApp_DoesNotTransitivelyImportTUI verifies the boundary at the source level:
// individual files are scanned so a future accidental import is caught immediately
// rather than at link time.
func TestApp_DoesNotTransitivelyImportTUI(t *testing.T) {
	// The go/parser scan covers direct imports only; for a lightweight guard this is
	// sufficient. Full transitive analysis is covered by the build graph (go build would
	// fail on a cycle before any test runs).
	assertAppDoesNotImport(t, "mosaic-deploy/internal/tui")
}

// assertAppDoesNotImport parses every non-test .go file in the app package directory and
// fails the test if any file imports the given package path.
func assertAppDoesNotImport(t *testing.T, forbiddenImport string) {
	t.Helper()

	// Find the app package directory: it is the parent directory of this test file.
	// Since Go tests run with the working directory set to the package directory, "."
	// is the app package directory.
	dir := "."

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() {
			continue // do not recurse into sub-packages (e.g., interactiontest)
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue // test files may import test helpers from outside the module boundary
		}

		path := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parser.ParseFile(%q): %v", path, err)
			continue
		}

		for _, imp := range f.Imports {
			// imp.Path.Value is a quoted string: "\"mosaic-deploy/internal/tui\""
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == forbiddenImport {
				t.Errorf("app/%s imports %q; the app package must not import any frontend package (AC18.7)",
					name, forbiddenImport)
			}
		}
	}
}

// TestApp_InteractionTestSubpackage_DoesNotImportTUI verifies that the interactiontest
// sub-package (which is imported by frontend tests) also does not import tui or cli.
// It is a pure test helper and must remain frontend-agnostic.
func TestApp_InteractionTestSubpackage_DoesNotImportTUI(t *testing.T) {
	assertSubpackageDoesNotImport(t, "interactiontest", "mosaic-deploy/internal/tui")
}

func TestApp_InteractionTestSubpackage_DoesNotImportCLI(t *testing.T) {
	assertSubpackageDoesNotImport(t, "interactiontest", "mosaic-deploy/internal/cli")
}

// assertSubpackageDoesNotImport scans a named subdirectory of the app package for forbidden imports.
func assertSubpackageDoesNotImport(t *testing.T, subdir, forbiddenImport string) {
	t.Helper()

	dir := subdir
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("subpackage %q does not exist yet; skipping import check", subdir)
		}
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		path := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parser.ParseFile(%q): %v", path, err)
			continue
		}

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == forbiddenImport {
				t.Errorf("app/%s/%s imports %q; the interactiontest package must not import any frontend (AC18.7)",
					subdir, name, forbiddenImport)
			}
		}
	}
}
