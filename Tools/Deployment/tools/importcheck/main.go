// Command importcheck enforces the module's import-boundary rules.
//
// Run from the module root (Tools/Deployment/):
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
// The tool checks four categories of rule:
//
//  1. Dependency direction: packages at each layer may only import inward.
//     domain → nothing; transform → domain+docformat; app → core packages,
//     never tui or cli; tui/cli → app+domain.
//
//  2. I/O ban on transform: the transform package must be a pure function with
//     no filesystem, network, terminal, time, or randomness imports. This is
//     what makes byte-exact golden testing possible.
//
//  3. Translator-layer purity and direction: the translator layer
//     (internal/agentformat and its sub-packages) must be a pure function with
//     no I/O, clock, or randomness, and must never import a harness package, the
//     application layer, transform, the frontends, or any infrastructure package.
//     internal/agentfields is explicitly permitted: it is the repository's
//     authority for MOSAIC-owned and stamp key vocabulary, and its source file
//     declares no imports at all, making it a leaf vocabulary package.
//
//  4. Harness-name isolation: no package outside internal/harness/builtin/*
//     may import a harness builtin sub-package directly. All harness access
//     goes through the registry.
//
//  5. Read-boundary rule: application-layer code must not call the raw-bytes
//     docformat entry points (docformat.Parse and docformat.SplitFrontmatter)
//     directly. Only deployed_read.go (the decode funnel) and
//     parse_generic_source.go (the named generic-source operation) are permitted.
//
// # Scope
//
// Only non-test source files are checked. Test files may import test helpers
// from outside the module boundary and are explicitly excluded.
// Subdirectories within a package directory are not recursed into by the rule
// for that package; each package directory is checked independently.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// modulePrefix is the Go module name from go.mod. All imports within this
// module start with this prefix.
const modulePrefix = "mosaic-deploy/"

// builtinParentPrefix is the import path prefix for built-in harness packages.
// Only packages under this prefix may import sibling packages here.
const builtinParentPrefix = modulePrefix + "internal/harness/builtin/"

// rule describes one import-boundary constraint for a single package directory.
type rule struct {
	// dir is the directory path relative to the module root.
	dir string
	// desc is a human-readable description of the constraint, shown in violation messages.
	desc string
	// forbidPrefix lists import-path prefixes that are forbidden in this package.
	// An import is forbidden if it equals a prefix or starts with "<prefix>/".
	forbidPrefix []string
	// forbidExact lists import paths that are forbidden verbatim.
	forbidExact []string
	// forbidAllModuleImports, when true, bans any import that starts with modulePrefix.
	// This enforces "this package must not import anything from this module."
	forbidAllModuleImports bool
	// forbidModuleImportsExcept, when non-nil, bans any import starting with modulePrefix
	// unless it also starts with one of these exception prefixes. This enforces "this package
	// may only import the listed sub-paths from this module."
	forbidModuleImportsExcept []string
}

// callBoundaryRule describes a call-level constraint for a single package directory.
// It forbids direct calls to specific functions in a named import package from any
// file in the directory except those listed in allowedFiles.
type callBoundaryRule struct {
	// dir is the directory path relative to the module root.
	dir string
	// desc is a human-readable description of the constraint, shown in violation messages.
	desc string
	// forbidCalls maps import paths to the set of function names that are forbidden in
	// non-allowed files. Example: {"mosaic-common/docformat": ["Parse", "SplitFrontmatter"]}.
	forbidCalls map[string][]string
	// allowedFiles lists file names (base names only, not full paths) that are exempt
	// from the call ban. Files in this list may call any function without restriction.
	allowedFiles []string
}

// translatorForbidPrefix is the I/O, clock, and randomness prefix list that the
// translator layer and transform share. Stated once so the two rules cannot diverge.
var translatorForbidPrefix = []string{
	"os",            // filesystem and process — covers os, os/exec, os/signal, os/user
	"io/fs",         // filesystem abstraction
	"path/filepath", // filesystem path manipulation
	"net",           // network — covers net, net/http, net/url, net/rpc
	"time",          // clock reads — nondeterministic
	"math/rand",     // pseudo-random — nondeterministic
	"crypto/rand",   // cryptographic random — nondeterministic
	"log",           // terminal/stderr output — side-effectful
}

// translatorForbidExact is the exact-match forbidden set for the translator layer and transform.
var translatorForbidExact = []string{
	"syscall", // OS-level access
	"flag",    // CLI flag parsing
	"bufio",   // typically used for I/O buffering
}

// translatorForbidModulePrefix is the set of module-internal package prefixes that
// the translator layer must never import. Importing any of these would invert the
// dependency direction: the translator is below all of them in the architecture.
var translatorForbidModulePrefix = []string{
	modulePrefix + "internal/harness",
	modulePrefix + "internal/app",
	modulePrefix + "internal/transform",
	modulePrefix + "internal/cli",
	modulePrefix + "internal/tui",
	modulePrefix + "internal/deploy",
	modulePrefix + "internal/catalog",
	modulePrefix + "internal/manifest",
	modulePrefix + "internal/plan",
	modulePrefix + "internal/config",
	modulePrefix + "internal/logging",
}

// rules is the authoritative list of import-boundary constraints.
// The dependency direction the architecture requires is:
//
//	tui, cli → app → core packages → domain → (nothing in this module)
//	transform → domain, docformat (no I/O)
//	agentformat → domain, agentfields, docformat, mosaic (no I/O, no upward deps)
//
// Adding a violation here means the architecture has drifted; fix the code,
// not this list.
var rules = []rule{
	{
		dir:                    "internal/domain",
		desc:                   "domain is the base layer and must not import any other package in this module",
		forbidAllModuleImports: true,
	},
	{
		dir:          "internal/transform",
		desc:         "transform must be a pure function: no filesystem, network, terminal, time, or randomness imports",
		forbidPrefix: translatorForbidPrefix,
		forbidExact:  translatorForbidExact,
	},
	{
		dir:  "internal/app",
		desc: "app is the use-case layer: it must not import any frontend package (tui or cli depend on app, not the reverse)",
		forbidPrefix: []string{
			modulePrefix + "internal/tui",
			modulePrefix + "internal/cli",
		},
	},
	{
		dir: "internal/cli",
		desc: "cli is an adapter: it must not reach past app into core infrastructure packages " +
			"(catalog, deploy, manifest, plan, transform, harness, config, logging, docformat) or import the tui sibling",
		// Forbidden: every core infrastructure package that bypasses app.Service.
		// Allowed: app, domain, todo (todo is a rendering utility, not infrastructure).
		forbidPrefix: []string{
			modulePrefix + "internal/tui",
			modulePrefix + "internal/catalog",
			modulePrefix + "internal/deploy",
			modulePrefix + "internal/manifest",
			modulePrefix + "internal/plan",
			modulePrefix + "internal/transform",
			modulePrefix + "internal/harness",
			modulePrefix + "internal/config",
			modulePrefix + "internal/logging",
			modulePrefix + "internal/docformat",
		},
	},
	{
		dir: "internal/tui",
		desc: "tui is an adapter: it must not reach past app into core infrastructure packages " +
			"(catalog, deploy, manifest, plan, transform, harness, config, logging, docformat) or import the cli sibling",
		// Forbidden: every core infrastructure package that bypasses app.Service.
		// Allowed: app, domain, todo (todo is a rendering utility, not infrastructure).
		forbidPrefix: []string{
			modulePrefix + "internal/cli",
			modulePrefix + "internal/catalog",
			modulePrefix + "internal/deploy",
			modulePrefix + "internal/manifest",
			modulePrefix + "internal/plan",
			modulePrefix + "internal/transform",
			modulePrefix + "internal/harness",
			modulePrefix + "internal/config",
			modulePrefix + "internal/logging",
			modulePrefix + "internal/docformat",
		},
	},
	{
		dir: "internal/tui/screens",
		desc: "tui/screens is an adapter sub-package: it must not reach past app into core infrastructure packages " +
			"(catalog, deploy, manifest, plan, transform, harness, config, logging, docformat) or import the cli sibling",
		// Forbidden: every core infrastructure package that bypasses app.Service.
		// Allowed: app, domain, todo (todo is a rendering utility, not infrastructure).
		forbidPrefix: []string{
			modulePrefix + "internal/cli",
			modulePrefix + "internal/catalog",
			modulePrefix + "internal/deploy",
			modulePrefix + "internal/manifest",
			modulePrefix + "internal/plan",
			modulePrefix + "internal/transform",
			modulePrefix + "internal/harness",
			modulePrefix + "internal/config",
			modulePrefix + "internal/logging",
			modulePrefix + "internal/docformat",
		},
	},
	{
		dir: "internal/tui/widgets",
		desc: "tui/widgets is an adapter sub-package: it must not reach past app into core infrastructure packages " +
			"(catalog, deploy, manifest, plan, transform, harness, config, logging, docformat) or import the cli sibling",
		// Forbidden: every core infrastructure package that bypasses app.Service.
		// Allowed: app, domain, todo (todo is a rendering utility, not infrastructure).
		forbidPrefix: []string{
			modulePrefix + "internal/cli",
			modulePrefix + "internal/catalog",
			modulePrefix + "internal/deploy",
			modulePrefix + "internal/manifest",
			modulePrefix + "internal/plan",
			modulePrefix + "internal/transform",
			modulePrefix + "internal/harness",
			modulePrefix + "internal/config",
			modulePrefix + "internal/logging",
			modulePrefix + "internal/docformat",
		},
	},
	// Translator-layer purity and direction rules (one per directory, since rules do
	// not cover sub-packages). The translator layer is a pure function with no I/O,
	// clock, or randomness, and must depend only downward in the architecture.
	//
	// internal/agentfields is on the permitted list deliberately: it is the repository's
	// authority for MOSAIC-owned and stamp key vocabulary. Forbidding it would force the
	// translator into a duplicated key list. Its source file declares no imports of its
	// own, making it a leaf vocabulary package equivalent to internal/domain.
	{
		dir: "internal/agentformat",
		desc: "the translator layer (agentformat) must be a pure function: no I/O, clock, or randomness; " +
			"and must depend only downward — it must never import harness, app, transform, cli, tui, or " +
			"infrastructure packages. Permitted module dependencies: internal/domain, internal/agentfields, " +
			"internal/agentformat siblings, mosaic-common/docformat, mosaic-common/mosaic",
		forbidPrefix: append(translatorForbidPrefix, translatorForbidModulePrefix...),
		forbidExact:  translatorForbidExact,
	},
	{
		dir: "internal/agentformat/codextoml",
		desc: "the translator layer (agentformat/codextoml) must be a pure function: no I/O, clock, or randomness; " +
			"and must depend only downward — it must never import harness, app, transform, cli, tui, or " +
			"infrastructure packages. Permitted module dependencies: internal/domain, internal/agentfields, " +
			"internal/agentformat, mosaic-common/docformat, mosaic-common/mosaic",
		forbidPrefix: append(translatorForbidPrefix, translatorForbidModulePrefix...),
		forbidExact:  translatorForbidExact,
	},
	{
		dir:                    "internal/agentformat/formatid",
		desc:                   "formatid is a leaf vocabulary package and must not import any package from this module",
		forbidAllModuleImports: true,
	},
	{
		dir: "internal/agentformat/all",
		desc: "the agentformat/all wiring package must only import translator sub-packages from this module; " +
			"importing any other module-internal package would widen the wiring package's scope beyond its " +
			"single responsibility of registering translator implementations",
		forbidModuleImportsExcept: []string{
			modulePrefix + "internal/agentformat/",
		},
	},
}

// callBoundaryRules is the authoritative list of call-level constraints.
// Each rule names a package directory and the set of function calls forbidden
// from non-permitted files within that directory.
var callBoundaryRules = []callBoundaryRule{
	{
		dir: "internal/app",
		desc: "application-layer code must not call raw-bytes docformat entry points directly " +
			"(docformat.Parse and docformat.SplitFrontmatter consume raw document bytes and bypass " +
			"the format-selection logic needed for non-Markdown harnesses such as Codex TOML); " +
			"use the decode funnel (deployed_read.go: decodeDeployedBytes, readDeployedArtifact) " +
			"for deployed agent files, or ParseGenericSource (parse_generic_source.go) for catalog " +
			"sources, injection content, render inputs, and other non-deployed documents. " +
			"The remaining docformat functions (Validate, ClassifyRegion, CanonicalDeployed, " +
			"CanonicalSections, CanonicalOrder, NodeDeployed, DeployedParent, RenderOpenTagLine, " +
			"RenderCloseTagLine, RetypeOpenTagLine) operate on an already-parsed *docformat.Document " +
			"or on region vocabulary, not on raw bytes, and are not banned",
		forbidCalls: map[string][]string{
			"mosaic-common/docformat": {"Parse", "SplitFrontmatter"},
		},
		allowedFiles: []string{
			"deployed_read.go",
			"parse_generic_source.go",
		},
	},
}

// harnessBuiltinPackages lists the import paths for every built-in harness
// sub-package. Any import of these paths from outside the builtinParentPrefix
// namespace is a violation (harness names must not appear outside their own
// packages; all harness access goes through the registry).
var harnessBuiltinPackages = []string{
	modulePrefix + "internal/harness/builtin/claudecode",
	modulePrefix + "internal/harness/builtin/codex",
	modulePrefix + "internal/harness/builtin/ghcpcli",
	modulePrefix + "internal/harness/builtin/opencode",
	modulePrefix + "internal/harness/builtin/vscodeghcp",
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

// runChecks applies all rules and also checks harness-name isolation across
// the whole source tree.
func runChecks(moduleRoot string) (violations []string, errs []string) {
	// Apply per-package import rules.
	for _, r := range rules {
		vs, es := checkRule(moduleRoot, r)
		violations = append(violations, vs...)
		errs = append(errs, es...)
	}

	// Apply per-package call-boundary rules.
	for _, r := range callBoundaryRules {
		vs, es := checkCallBoundary(moduleRoot, r)
		violations = append(violations, vs...)
		errs = append(errs, es...)
	}

	// Check harness-name isolation: built-in packages may not be imported
	// outside the builtin parent namespace.
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

	// Module-wide ban with exceptions: this package may only import the listed
	// sub-paths from this module.
	if len(r.forbidModuleImportsExcept) > 0 && strings.HasPrefix(imp, modulePrefix) {
		allowed := false
		for _, exc := range r.forbidModuleImportsExcept {
			if strings.HasPrefix(imp, exc) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Sprintf("%s: imports %q — %s", location, imp, r.desc)
		}
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

// checkCallBoundary scans every non-test .go file in the named directory for calls
// to the forbidden functions, and reports any call from a non-allowed file.
func checkCallBoundary(moduleRoot string, r callBoundaryRule) (violations []string, errs []string) {
	pkgDir := filepath.Join(moduleRoot, filepath.FromSlash(r.dir))

	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		// A missing directory is not a tool error.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("ReadDir(%q): %v", pkgDir, err)}
	}

	fset := token.NewFileSet()

	// Build a set of allowed file names for fast lookup.
	allowedSet := make(map[string]bool, len(r.allowedFiles))
	for _, f := range r.allowedFiles {
		allowedSet[f] = true
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if allowedSet[name] {
			continue // this file is explicitly permitted to make these calls
		}

		filePath := filepath.Join(pkgDir, name)
		f, parseErr := parser.ParseFile(fset, filePath, nil, 0)
		if parseErr != nil {
			errs = append(errs, fmt.Sprintf("parse %q: %v", filePath, parseErr))
			continue
		}

		// Build a map from local package alias to import path.
		aliases := buildImportAliasMap(f)

		// Walk the AST looking for calls to forbidden functions.
		ast.Inspect(f, func(n ast.Node) bool {
			callExpr, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			selExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgIdent, ok := selExpr.X.(*ast.Ident)
			if !ok {
				return true
			}

			importPath, found := aliases[pkgIdent.Name]
			if !found {
				return true
			}

			bannedFns, hasBan := r.forbidCalls[importPath]
			if !hasBan {
				return true
			}

			for _, fn := range bannedFns {
				if selExpr.Sel.Name == fn {
					pos := fset.Position(callExpr.Pos())
					violations = append(violations, fmt.Sprintf(
						"%s/%s:%d: calls %s.%s — %s",
						r.dir, name, pos.Line,
						pkgIdent.Name, fn,
						r.desc,
					))
				}
			}
			return true
		})
	}

	return violations, errs
}

// buildImportAliasMap returns a map from local package name to import path for
// the imports in f. Blank imports ("_") and dot imports (".") are excluded.
// When a package has an explicit alias, the alias is used; otherwise the last
// path component of the import path is used.
func buildImportAliasMap(f *ast.File) map[string]string {
	m := make(map[string]string, len(f.Imports))
	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		var localName string
		if imp.Name != nil {
			localName = imp.Name.Name
		} else {
			parts := strings.Split(importPath, "/")
			localName = parts[len(parts)-1]
		}
		if localName == "_" || localName == "." {
			continue
		}
		m[localName] = importPath
	}
	return m
}

// checkHarnessIsolation scans every non-test .go file in the module and
// reports any import of a built-in harness package from outside the builtin
// parent namespace. This enforces the rule that harness names do not appear
// outside their own packages: all harness access goes through the registry.
func checkHarnessIsolation(moduleRoot string) (violations []string, errs []string) {
	err := filepath.WalkDir(moduleRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		// Skip the dist/ and tools/ directories (generated output and dev tools).
		rel, relErr := filepath.Rel(moduleRoot, path)
		if relErr != nil {
			return nil
		}
		// Normalise to forward slashes for prefix checks.
		relSlash := filepath.ToSlash(rel)

		if d.IsDir() {
			// Skip non-source and non-library directories.
			//
			// cmd/ executables are allowed to import any package, including built-in harness
			// packages. The isolation rule applies to library packages only (internal/).
			// The reference external module (cmd/harness-opencode-module) intentionally
			// imports internal/harness/builtin/opencode by design (AC23.2).
			switch {
			case relSlash == "dist",
				relSlash == "tools",
				relSlash == "cmd",
				relSlash == "testdata",
				strings.HasPrefix(relSlash, "dist/"),
				strings.HasPrefix(relSlash, "tools/"),
				strings.HasPrefix(relSlash, "cmd/"),
				strings.HasPrefix(relSlash, "testdata/"):
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		// Determine whether this file is itself inside the builtin namespace.
		fileDir := filepath.ToSlash(filepath.Dir(relSlash))
		insideBuiltin := strings.HasPrefix(fileDir+"/", strings.TrimPrefix(builtinParentPrefix, modulePrefix))

		if insideBuiltin {
			return nil // builtin packages may import sibling builtin packages
		}

		fset := token.NewFileSet()
		imports, err := parseImports(fset, path)
		if err != nil {
			return nil // parse errors are reported by per-package rules
		}

		for _, imp := range imports {
			for _, harnessImport := range harnessBuiltinPackages {
				if imp == harnessImport || strings.HasPrefix(imp, harnessImport+"/") {
					violations = append(violations, fmt.Sprintf(
						"%s: imports harness builtin %q — harness packages must not be imported "+
							"outside internal/harness/builtin/*; use the registry to obtain a "+
							"domain.HarnessModule",
						relSlash, imp,
					))
				}
			}
		}

		return nil
	})

	if err != nil {
		errs = append(errs, fmt.Sprintf("WalkDir(%q): %v", moduleRoot, err))
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
