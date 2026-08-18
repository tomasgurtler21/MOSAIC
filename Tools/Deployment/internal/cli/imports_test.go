package cli_test

// imports_test.go verifies that the cli package's non-test source files import only
// app and domain from the internal packages. The cli package is a thin adapter; it must
// not reach into domain-layer packages that belong to the application core (transform,
// catalog, harness/*, deploy, manifest, config, logging, plan, docformat) and must not
// import the sibling frontend (tui). These constraints enforce the hexagonal architecture
// and ensure the adapter stays decoupled from implementation details.
//
// Note: the cli package may also import the todo package because NewInteraction's contract
// requires todo.Collector. This is architecturally sound (todo only imports domain) and is
// therefore permitted by these tests.
//
// Verified invariants (T19.9):
//
//   - No non-test .go file in the cli package imports "mosaic-deploy/internal/tui"
//   - No non-test .go file imports domain-layer internals:
//       transform, catalog, harness (any sub-path), deploy, manifest, config,
//       logging, plan, docformat
//   - The invariant is mechanical: it reads the actual source files, so it cannot drift.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLI_DoesNotImportTUI verifies that no non-test source file in the cli package imports
// "mosaic-deploy/internal/tui". The cli package and the tui package are sibling frontends;
// importing one from the other would violate the single-responsibility separation and could
// create import cycles.
func TestCLI_DoesNotImportTUI(t *testing.T) {
	assertCLIDoesNotImport(t, "mosaic-deploy/internal/tui")
}

// TestCLI_DoesNotImportTransform verifies that the cli package does not import the
// transform package. Transformation is an app concern, not a CLI adapter concern.
func TestCLI_DoesNotImportTransform(t *testing.T) {
	assertCLIDoesNotImport(t, "mosaic-deploy/internal/transform")
}

// TestCLI_DoesNotImportCatalog verifies that the cli package does not import the
// catalog package. Catalog loading is an infrastructure concern wired by the entry point.
func TestCLI_DoesNotImportCatalog(t *testing.T) {
	assertCLIDoesNotImport(t, "mosaic-deploy/internal/catalog")
}

// TestCLI_DoesNotImportDeploy verifies that the cli package does not import the
// deploy package. The executor is wired by the entry point, not the adapter.
func TestCLI_DoesNotImportDeploy(t *testing.T) {
	assertCLIDoesNotImport(t, "mosaic-deploy/internal/deploy")
}

// TestCLI_DoesNotImportManifest verifies that the cli package does not import the
// manifest package.
func TestCLI_DoesNotImportManifest(t *testing.T) {
	assertCLIDoesNotImport(t, "mosaic-deploy/internal/manifest")
}

// TestCLI_DoesNotImportConfig verifies that the cli package does not import the
// config package. Configuration stores are wired by the entry point.
func TestCLI_DoesNotImportConfig(t *testing.T) {
	assertCLIDoesNotImport(t, "mosaic-deploy/internal/config")
}

// TestCLI_DoesNotImportLogging verifies that the cli package does not import the
// logging package. The logger is wired by the entry point.
func TestCLI_DoesNotImportLogging(t *testing.T) {
	assertCLIDoesNotImport(t, "mosaic-deploy/internal/logging")
}

// TestCLI_DoesNotImportPlan verifies that the cli package does not import the
// plan package. Planning is an app concern.
func TestCLI_DoesNotImportPlan(t *testing.T) {
	assertCLIDoesNotImport(t, "mosaic-deploy/internal/plan")
}

// TestCLI_DoesNotImportDocformat verifies that the cli package does not import the
// docformat package. Document parsing is a domain concern used only by the core packages.
func TestCLI_DoesNotImportDocformat(t *testing.T) {
	assertCLIDoesNotImport(t, "mosaic-common/docformat")
}

// TestCLI_DoesNotImportHarness verifies that the cli package does not import any
// harness sub-package. Harness selection and wiring is the entry point's responsibility.
func TestCLI_DoesNotImportHarness(t *testing.T) {
	// All harness sub-packages share the prefix; scan for any import that begins with it.
	assertCLIDoesNotImportPrefix(t, "mosaic-deploy/internal/harness")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// assertCLIDoesNotImport parses every non-test .go file in the cli package directory
// and fails the test if any file imports the given package path.
func assertCLIDoesNotImport(t *testing.T, forbiddenImport string) {
	t.Helper()

	// Go tests run with the working directory set to the package under test.
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue // test files may import test helpers from other packages
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
				t.Errorf("cli/%s imports %q; the cli package must not import this package (AC19.7)",
					name, forbiddenImport)
			}
		}
	}
}

// assertCLIDoesNotImportPrefix fails the test if any non-test .go file in the cli package
// imports a package whose path starts with the given prefix. Used for catching imports of
// any sub-package of a given root (e.g., harness/*).
func assertCLIDoesNotImportPrefix(t *testing.T, forbiddenPrefix string) {
	t.Helper()

	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
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
			if strings.HasPrefix(importPath, forbiddenPrefix) {
				t.Errorf("cli/%s imports %q; the cli package must not import any harness sub-package (AC19.7)",
					name, importPath)
			}
		}
	}
}
