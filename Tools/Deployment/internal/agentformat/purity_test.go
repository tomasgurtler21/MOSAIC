package agentformat_test

// purity_test.go covers the translator-layer purity self-check. It walks the
// non-test source files of the translator layer (internal/agentformat and
// internal/agentformat/codextoml) and asserts none of them imports any package
// from the purity forbid-list.
//
// The forbid-list for the translator layer:
//   - Standard library: "os", "io/fs", "path/filepath", "net", "time",
//     "math/rand", "crypto/rand", "log", "syscall", "flag", "bufio"
//   - Internal module: any "mosaic-deploy/internal/harness/..." package,
//     any "mosaic-deploy/internal/app/..." package,
//     any "mosaic-deploy/internal/transform/..." package.
//
// Purpose: the datetime decision in I4.1 classifies TOML datetimes as marker-
// carried specifically to avoid importing "time" into the translator layer. This
// test detects a violation at the stage that introduced it, rather than at a later
// enforcement stage.
//
// Only non-test files are checked. Test files may import any package including
// test helpers from outside the module boundary.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenImports is the set of import paths forbidden in translator-layer
// non-test source files.
var forbiddenImports = []string{
	// Standard-library I/O and system packages.
	"os",
	"io/fs",
	"path/filepath",
	"net",
	"time",
	"math/rand",
	"crypto/rand",
	"log",
	"syscall",
	"flag",
	"bufio",
}

// forbiddenInternalPrefixes are module-internal import path prefixes that are
// forbidden in translator-layer source files.
var forbiddenInternalPrefixes = []string{
	"mosaic-deploy/internal/harness/",
	"mosaic-deploy/internal/app/",
	"mosaic-deploy/internal/transform/",
}

// TestTranslatorLayerPurity_NoForbiddenImports walks the non-test source files in
// internal/agentformat/ and internal/agentformat/codextoml/ and asserts that none
// import a package on the forbid-list. The test cwd when running is the package
// directory (internal/agentformat/), so "." refers to agentformat and
// "codextoml" refers to the codextoml sub-package.
func TestTranslatorLayerPurity_NoForbiddenImports(t *testing.T) {
	// The two translator-layer directories relative to the test cwd.
	dirs := []string{".", filepath.Join(".", "codextoml")}

	for _, dir := range dirs {
		dir := dir
		t.Run(dir, func(t *testing.T) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("ReadDir(%q): %v", dir, err)
			}

			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				if !strings.HasSuffix(name, ".go") {
					continue
				}
				// Skip test files.
				if strings.HasSuffix(name, "_test.go") {
					continue
				}

				path := filepath.Join(dir, name)
				checkFileImports(t, path)
			}
		})
	}
}

// checkFileImports parses the Go source file at path and asserts none of its
// imports are on the purity forbid-list.
func checkFileImports(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Errorf("parse %q: %v", path, err)
		return
	}

	for _, imp := range f.Imports {
		// imp.Path.Value is a double-quoted Go string; strip the quotes.
		raw := imp.Path.Value
		if len(raw) < 2 {
			continue
		}
		importPath := raw[1 : len(raw)-1] // strip leading and trailing '"'

		// Check against the explicit forbidden list.
		for _, forbidden := range forbiddenImports {
			if importPath == forbidden {
				t.Errorf("%s: imports %q which is on the translator-layer purity forbid-list", path, importPath)
			}
		}

		// Check against forbidden internal path prefixes.
		for _, prefix := range forbiddenInternalPrefixes {
			if strings.HasPrefix(importPath, prefix) {
				t.Errorf("%s: imports %q which starts with forbidden prefix %q; the translator layer must not import harness, app or transform packages", path, importPath, prefix)
			}
		}
	}
}
