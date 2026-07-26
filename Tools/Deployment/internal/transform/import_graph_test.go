package transform_test

// import_graph_test.go mechanically verifies AC8.2: the transform package must import no
// filesystem, network, terminal, time, or randomness package. This property is what makes
// byte-exact golden testing possible and guarantees the pipeline is a pure function.
//
// A future change that accidentally adds "os", "time", or "net" will be caught here
// without requiring a manual audit of import statements.

import (
	"go/build"
	"strings"
	"testing"
)

// forbiddenImportPrefixes are the package prefixes banned from the transform package per
// AC8.2. Any import whose path equals one of these or starts with "prefix/" is forbidden.
var forbiddenImportPrefixes = []string{
	"os",            // filesystem access — all os.* calls are I/O
	"io/fs",         // filesystem abstraction
	"path/filepath", // filesystem path manipulation
	"net",           // network access (also matches net/http, net/rpc, etc.)
	"time",          // clock reads — nondeterministic, breaks byte-exact golden testing
	"math/rand",     // pseudo-random — nondeterministic output
	"crypto/rand",   // cryptographic randomness
	"log",           // terminal/stderr output — side-effectful
}

// forbiddenExact are exact package paths that are forbidden and not caught by the prefix
// list above.
var forbiddenExact = []string{
	"os/exec",  // subprocess execution
	"syscall",  // OS-level access
	"flag",     // CLI flag parsing — not appropriate in a library package
}

// TestPackageHasNoForbiddenImports loads the direct import list of
// mosaic-deploy/internal/transform and asserts that no import matches any forbidden
// prefix or exact path.
//
// Only non-test source files are checked: test files are excluded because test helpers
// legitimately use "os" and "path/filepath" for fixture loading. The goal is to guard the
// production package, not the test package.
func TestPackageHasNoForbiddenImports(t *testing.T) {
	const pkgPath = "mosaic-deploy/internal/transform"

	ctx := build.Default
	pkg, err := ctx.Import(pkgPath, ".", build.ImportComment)
	if err != nil {
		t.Fatalf("import %q: %v\n"+
			"(ensure the module is built before running this test: go build ./internal/transform/...)",
			pkgPath, err)
	}

	// pkg.Imports contains only the direct imports of non-test source files, which is the
	// precise set we want to validate.
	for _, imp := range pkg.Imports {
		for _, prefix := range forbiddenImportPrefixes {
			if imp == prefix || strings.HasPrefix(imp, prefix+"/") {
				t.Errorf("transform package imports %q which matches forbidden prefix %q\n"+
					"The transform package must be free of filesystem, network, time, and "+
					"randomness imports (AC8.2). All inputs must arrive via transform.Request.",
					imp, prefix)
			}
		}
		for _, exact := range forbiddenExact {
			if imp == exact {
				t.Errorf("transform package imports %q which is explicitly forbidden\n"+
					"The transform package must not perform subprocess, OS-level, or CLI "+
					"operations (AC8.2).",
					imp)
			}
		}
	}
}
