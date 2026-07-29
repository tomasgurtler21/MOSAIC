// Command importcheck enforces the mosaic-run module's import-boundary rules.
//
// Run from the module root (Tools/Runner/):
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
// The tool checks three categories of rule:
//
//  1. Dependency direction: packages at each layer may only import inward.
//     domain → nothing internal; engine → domain only; session → core packages,
//     never tui or cli; tui/cli → session and domain, but not each other.
//
//  2. Engine purity: the engine package must have no filesystem, network, or
//     randomness imports. This preserves the pure-function property that makes
//     golden-file fixture testing possible. Note: importing "time" for the
//     time.Time parameter type is allowed -- only clock-reading packages are banned.
//
//  3. Harness isolation: no production package inside internal/ (excluding harness
//     itself) may import mosaic-run/internal/harness directly. All harness access
//     from use-case and adapter code goes through the domain.HarnessAdapter port.
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
const modulePrefix = "mosaic-run/"

// harnessPackage is the import path for the harness package. This is the concrete
// adapter package that should not be imported by production code in internal/.
const harnessPackage = modulePrefix + "internal/harness"

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

// rules is the authoritative list of import-boundary constraints.
// The dependency direction the architecture requires is:
//
//	cmd        → anything (composition root; no restrictions)
//	tui, cli   → session, domain (not each other)
//	session    → core packages (not tui or cli)
//	engine     → domain only, no I/O packages
//	domain     → nothing in this module
//
// Adding a violation means the architecture has drifted; fix the code, not this list.
var rules = []rule{
	{
		dir:  "internal/domain",
		desc: "domain is the base layer: must not import any package from this module",
		forbidAllModuleImports: true,
	},
	{
		dir:  "internal/engine",
		desc: "engine is the pure decision core: must only import domain from this module, and must not import I/O packages",
		forbidPrefix: []string{
			// All internal packages except domain are forbidden.
			modulePrefix + "internal/agentresolve",
			modulePrefix + "internal/artifact",
			modulePrefix + "internal/cli",
			modulePrefix + "internal/compat",
			modulePrefix + "internal/deviation",
			modulePrefix + "internal/harness",
			modulePrefix + "internal/orchfile",
			modulePrefix + "internal/planstages",
			modulePrefix + "internal/session",
			modulePrefix + "internal/tui",
			modulePrefix + "internal/workflow",
			// I/O bans: packages that provide filesystem, network, or randomness.
			// "os" covers os, os/exec, os/signal, os/user.
			"os",
			// "net" covers net, net/http, net/url, net/rpc, etc.
			"net",
			// "math/rand" is banned for nondeterminism; crypto/rand too.
			"math/rand",
			"crypto/rand",
		},
		forbidExact: []string{
			// syscall provides direct OS access, banned for the same reasons as os.
			"syscall",
		},
	},
	{
		dir:  "internal/session",
		desc: "session is the use-case layer: must not import frontend packages (tui or cli depend on session, not the reverse)",
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

// runChecks applies all rules and also checks harness-isolation across the source tree.
func runChecks(moduleRoot string) (violations []string, errs []string) {
	// Apply per-package rules.
	for _, r := range rules {
		vs, es := checkRule(moduleRoot, r)
		violations = append(violations, vs...)
		errs = append(errs, es...)
	}

	// Check harness isolation: no production package inside internal/ (other than
	// harness itself) may directly import mosaic-run/internal/harness.
	vs, es := checkHarnessIsolation(moduleRoot)
	violations = append(violations, vs...)
	errs = append(errs, es...)

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

// checkHarnessIsolation scans every non-test .go file in the internal/ directory
// and reports any production package (outside internal/harness) that imports
// mosaic-run/internal/harness directly. All harness access must go through the
// domain.HarnessAdapter port.
//
// The cmd/ directory is excluded: composition roots (like cmd/mosaic-run/main.go)
// are the appropriate place to wire the concrete harness implementation.
func checkHarnessIsolation(moduleRoot string) (violations []string, errs []string) {
	internalDir := filepath.Join(moduleRoot, "internal")

	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries
		}

		// Compute relative path within internal/ for readable messages.
		rel, relErr := filepath.Rel(internalDir, path)
		if relErr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)

		if d.IsDir() {
			// The harness package itself may import its own internals.
			if relSlash == "harness" || strings.HasPrefix(relSlash, "harness/") {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		imports, err := parseImports(fset, path)
		if err != nil {
			return nil // parse errors are reported by per-package rules
		}

		for _, imp := range imports {
			if imp == harnessPackage || strings.HasPrefix(imp, harnessPackage+"/") {
				violations = append(violations, fmt.Sprintf(
					"internal/%s: imports %q — production code outside internal/harness must not import the concrete harness package; use domain.HarnessAdapter instead",
					relSlash, imp,
				))
			}
		}

		return nil
	})

	if err != nil {
		errs = append(errs, fmt.Sprintf("WalkDir(%q): %v", internalDir, err))
	}

	return violations, errs
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
