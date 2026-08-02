// Command importcheck enforces the mosaic-log-analyzer module's import-boundary rules.
//
// Run from the module root (Tools/LogAnalyzer/):
//
//	go run ./tools/importcheck
//
// Or via the Taskfile:
//
//	task check:imports
//
// Exit codes:
//
//	0  all boundaries respected
//	1  one or more violations found (details written to stderr)
//	2  tool error (cannot read source tree)
//
// # Rules enforced
//
// The tool checks the following dependency-direction and purity constraints:
//
//  1. domain must import nothing from this module. It is the base vocabulary
//     layer and all arrows point inward toward it.
//
//  2. analysis (and analysis/cost) may import only domain from this module and
//     must not import I/O packages (os, net, syscall, math/rand, crypto/rand).
//     This preserves the pure-function property that makes fixture testing easy.
//
//  3. app must not import internal/tui or internal/cli. Both frontends drive app,
//     never the reverse.
//
//  4. tui and cli must not import each other. They are sibling frontend adapters.
//
// # Scope
//
// Only non-test source files are checked. Test files may import test helpers from
// outside the module boundary and are explicitly excluded.
// Subdirectories within a package directory are not recursed into by the rule for
// that package; each package directory is checked independently.
package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// modulePrefix is the Go module name from go.mod. All internal imports within
// this module start with this prefix.
const modulePrefix = "mosaic-log-analyzer/"

// rule describes one import-boundary constraint for a single package directory.
type rule struct {
	// dir is the directory path relative to the module root.
	dir string
	// desc is a human-readable description of the constraint, shown in violation messages.
	desc string
	// forbidPrefix lists import-path prefixes that are forbidden in this package.
	// An import is forbidden if it equals the prefix or starts with "<prefix>/".
	forbidPrefix []string
	// forbidExact lists import paths that are forbidden verbatim.
	forbidExact []string
	// forbidAllModuleImports, when true, bans any import that starts with modulePrefix.
	// This enforces "this package must not import any package from this module."
	forbidAllModuleImports bool
}

// rules is the authoritative list of import-boundary constraints for this module.
// The dependency direction the architecture requires is:
//
//	cmd                  → anything (composition root; no restrictions)
//	tui, cli             → app, domain (not each other)
//	app                  → domain, analysis, adapters via ports (not tui or cli)
//	analysis, cost       → domain only, no I/O packages
//	logscan, logread,    → domain, permitted stdlib (adapters)
//	  pricing
//	domain               → nothing in this module
//
// Adding a violation means the architecture has drifted; fix the code, not this list.
var rules = []rule{
	{
		dir:                    "internal/domain",
		desc:                   "domain is the base layer: must not import any package from this module",
		forbidAllModuleImports: true,
	},
	{
		dir:  "internal/analysis",
		desc: "analysis is the pure decision core: must only import domain from this module, and must not import I/O packages",
		forbidPrefix: []string{
			// All internal packages except domain and analysis/cost are forbidden —
			// analysis/cost is a sub-package of this core, not an I/O adapter.
			modulePrefix + "internal/app",
			modulePrefix + "internal/cli",
			modulePrefix + "internal/logread",
			modulePrefix + "internal/logscan",
			modulePrefix + "internal/pricing",
			modulePrefix + "internal/tui",
			// I/O bans: packages that provide filesystem, network, or randomness.
			"os",
			"net",
			"math/rand",
			"crypto/rand",
		},
		forbidExact: []string{
			"syscall",
		},
	},
	{
		dir:  "internal/analysis/cost",
		desc: "analysis/cost is a pure pricing sub-package: must only import domain from this module, and must not import I/O packages",
		forbidPrefix: []string{
			modulePrefix + "internal/app",
			modulePrefix + "internal/cli",
			modulePrefix + "internal/logread",
			modulePrefix + "internal/logscan",
			modulePrefix + "internal/pricing",
			modulePrefix + "internal/tui",
			"os",
			"net",
			"math/rand",
			"crypto/rand",
		},
		forbidExact: []string{
			"syscall",
		},
	},
	{
		dir:  "internal/app",
		desc: "app is the use-case layer: must not import frontend packages (tui and cli drive app, not the reverse)",
		forbidPrefix: []string{
			modulePrefix + "internal/tui",
			modulePrefix + "internal/cli",
		},
	},
	{
		dir:  "internal/cli",
		desc: "cli is an adapter frontend: must not import the tui sibling frontend",
		forbidPrefix: []string{
			modulePrefix + "internal/tui",
		},
	},
	{
		dir:  "internal/tui",
		desc: "tui is an adapter frontend: must not import the cli sibling frontend",
		forbidPrefix: []string{
			modulePrefix + "internal/cli",
		},
	},
	{
		dir:  "internal/tui/screens",
		desc: "tui/screens is an adapter frontend sub-package: must not import the cli sibling frontend",
		forbidPrefix: []string{
			modulePrefix + "internal/cli",
		},
	},
}

func main() {
	moduleRoot := "."
	if len(os.Args) > 1 {
		moduleRoot = os.Args[1]
	}

	violations, errs := runChecks(moduleRoot)

	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "tool error: %s\n", e)
	}
	if len(errs) > 0 {
		os.Exit(2)
	}

	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "import boundary violations (%d):\n", len(violations))
		for _, v := range violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintf(os.Stderr, "\nFix the imports above to restore the dependency direction.\n")
		fmt.Fprintf(os.Stderr, "Run `task check:imports` to re-check after editing.\n")
		os.Exit(1)
	}

	fmt.Println("all import boundaries OK")
}

// runChecks applies all rules to the source tree.
func runChecks(moduleRoot string) (violations []string, errs []string) {
	for _, r := range rules {
		vs, es := checkRule(moduleRoot, r)
		violations = append(violations, vs...)
		errs = append(errs, es...)
	}
	return violations, errs
}

// checkRule scans every non-test .go file in the named directory and reports
// any import that violates the rule's constraints.
func checkRule(moduleRoot string, r rule) (violations []string, errs []string) {
	pkgDir := filepath.Join(moduleRoot, filepath.FromSlash(r.dir))

	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		// A missing directory is not a tool error — it may be an optional package.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("ReadDir(%q): %v", pkgDir, err)}
	}

	fset := token.NewFileSet()

	for _, entry := range entries {
		if entry.IsDir() {
			continue // rules are per-directory; sub-packages have their own rules
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue // test files may import test helpers; only production code is constrained
		}

		filePath := filepath.Join(pkgDir, name)
		imports, err := parseImports(fset, filePath)
		if err != nil {
			errs = append(errs, fmt.Sprintf("parse %q: %v", filePath, err))
			continue
		}

		for _, imp := range imports {
			if v := checkImport(imp, r, name); v != "" {
				violations = append(violations, v)
			}
		}
	}

	return violations, errs
}

// checkImport returns a violation message if imp violates the rule, or "" if it is allowed.
func checkImport(imp string, r rule, fileName string) string {
	location := r.dir + "/" + fileName

	// Module-wide ban: this package must not import any package within this module.
	if r.forbidAllModuleImports && strings.HasPrefix(imp, modulePrefix) {
		return fmt.Sprintf("%s: imports %q — %s", location, imp, r.desc)
	}

	// Prefix-based bans.
	for _, prefix := range r.forbidPrefix {
		if imp == prefix || strings.HasPrefix(imp, prefix+"/") {
			return fmt.Sprintf("%s: imports %q (matches forbidden prefix %q) — %s",
				location, imp, prefix, r.desc)
		}
	}

	// Exact bans.
	for _, exact := range r.forbidExact {
		if imp == exact {
			return fmt.Sprintf("%s: imports %q (explicitly forbidden) — %s",
				location, imp, r.desc)
		}
	}

	return ""
}

// parseImports parses only the import declarations from a Go source file,
// returning the unquoted import paths.
func parseImports(fset *token.FileSet, filePath string) ([]string, error) {
	f, err := parser.ParseFile(fset, filePath, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	imports := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		// imp.Path.Value is a double-quoted string literal: `"pkg/path"`
		path := strings.Trim(imp.Path.Value, `"`)
		imports = append(imports, path)
	}

	return imports, nil
}
