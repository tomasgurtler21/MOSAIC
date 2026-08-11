// Command importcheck enforces the mosaic-agent-test module's
// import-boundary rules.
//
// Run from the module root (Tools/AgentTest/):
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
// Every package directory under internal/ and cmd/ is classified into
// exactly one layer. A package this table does not classify is itself a
// violation — silently permitting an unclassified package would let the
// layering erode as new packages are added, one "obviously fine" import at a
// time.
//
//  1. domain: must not import anything else in this module.
//  2. pure cores (stubmatch, intercept, evaluate, concurrency,
//     protocolcheck): may import only domain and each other; must not
//     import the I/O packages os, io/ioutil, net, os/exec; must not call
//     time.Now (ambient clock reads break determinism — time enters as a
//     parameter).
//  3. adapters and stores (workspace, runstate, invlog, harness and its
//     subpackages, launch, orchstate, cost, report, sideeffects, authoring,
//     fixtures, preflight): may import only domain and the pure cores, with
//     one named exception: preflight may import authoring and fixtures,
//     since composing them is its entire job (see adapterImportExceptions).
//  4. use cases (interceptor, runner, suite): may import domain, the pure
//     cores and the adapters, but never cli or tui.
//  5. frontends (cli, tui): may import domain and the use cases, never each
//     other, with one named exception: both may import report, preflight
//     and authoring — the single result model and its renderers, and the
//     pre-flight report they select between and render (see
//     frontendImportExceptions).
//  6. harness isolation: no package outside internal/harness/ may import a
//     concrete harness adapter package (harness/claudecode, harness/opencode,
//     harness/fake) directly, with one narrow exception: cmd/mosaic-agent-test,
//     the binary composition root, is the only place a concrete adapter is
//     constructed. The exemption covers exactly that one package path — a
//     sibling cmd/ package, or a package beneath cmd/mosaic-agent-test/
//     that is not the root itself, gets no benefit from it (see
//     compositionRootPackage).
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// modulePrefix is the Go module name from go.mod. All internal imports
// within this module start with this prefix.
const modulePrefix = "mosaic-agent-test/"

// compositionRootPackage is the module's one binary composition root — the
// only package this gate exempts from harness isolation. The exemption is
// narrow: it names exactly this path, never a prefix, so a package that
// merely sits beneath cmd/mosaic-agent-test/ (a subpackage of the root) or a
// sibling cmd/ package gets no benefit from it and is classified and
// harness-isolated exactly as an internal/ package would be.
const compositionRootPackage = "cmd/mosaic-agent-test"

// layer identifies which layer of the architecture a package belongs to.
type layer string

const (
	layerDomain          layer = "domain"
	layerPureCore        layer = "pure_core"
	layerAdapter         layer = "adapter"
	layerUseCase         layer = "use_case"
	layerFrontend        layer = "frontend"
	layerCompositionRoot layer = "composition_root"
	layerUnknown         layer = ""
)

// pureCorePackages are the packages classified as pure decision cores.
var pureCorePackages = map[string]bool{
	"internal/stubmatch":     true,
	"internal/intercept":     true,
	"internal/evaluate":      true,
	"internal/concurrency":   true,
	"internal/protocolcheck": true,
}

// adapterPackages are the packages classified as adapters and stores.
var adapterPackages = map[string]bool{
	"internal/workspace":          true,
	"internal/runstate":           true,
	"internal/invlog":             true,
	"internal/harness":            true,
	"internal/harness/contract":   true,
	"internal/harness/fake":       true,
	"internal/harness/claudecode": true,
	"internal/harness/opencode":   true,
	"internal/launch":             true,
	"internal/orchstate":          true,
	"internal/cost":               true,
	"internal/agentdeploy":        true,
	"internal/report":             true,
	"internal/sideeffects":        true,
	"internal/authoring":          true,
	"internal/fixtures":           true,
	"internal/preflight":          true,
}

// useCasePackages are the packages classified as use cases.
var useCasePackages = map[string]bool{
	"internal/interceptor": true,
	"internal/runner":      true,
	"internal/suite":       true,
}

// frontendPackages are the packages classified as frontends.
var frontendPackages = map[string]bool{
	"internal/cli": true,
	"internal/tui": true,
}

// adapterImportExceptions narrows the "adapters may import only domain and
// the pure cores" rule for the specific adapter-to-adapter pairs the
// Integration Points table requires. internal/preflight's entire job is to
// compose internal/authoring and internal/fixtures into one aggregated
// report — that composition is the contract, not a hardening bypass — so
// those two imports are named here explicitly rather than widening the
// generic adapter rule for every adapter. internal/sideeffects resolves a
// stub's $ref file effects through internal/fixtures for the same reason:
// that composition is its contract, named in the Integration Points table.
var adapterImportExceptions = map[string]map[string]bool{
	"internal/preflight": {
		"internal/authoring":  true,
		"internal/fixtures":   true,
		"internal/agentdeploy": true, // preflight uses agentdeploy for dry-run declaration validation
	},
	"internal/sideeffects": {
		"internal/fixtures": true,
	},
}

// frontendImportExceptions narrows the "frontends may import only domain
// and the use cases" rule for the specific frontend-to-adapter pairs the
// Integration Points table requires. Both frontends select between
// internal/report's two renderings of the single Result model rather than
// computing a rendering of their own, and both render the pre-flight report
// internal/preflight produces (internal/cli.PreflightFunc and
// internal/tui.Options.Preflight share that exact signature) — internal/
// authoring is the diagnostic type that report carries, not a new
// composition either frontend performs itself.
var frontendImportExceptions = map[string]map[string]bool{
	"internal/cli": {
		"internal/report":    true,
		"internal/preflight": true,
		"internal/authoring": true,
	},
	"internal/tui": {
		"internal/report":    true,
		"internal/preflight": true,
		"internal/authoring": true,
	},
}

// concreteHarnessPackages are the harness adapter packages subject to the
// harness-isolation rule: only internal/harness/ itself, and the cmd/
// composition root, may import them.
var concreteHarnessPackages = map[string]bool{
	"internal/harness/claudecode": true,
	"internal/harness/opencode":   true,
	"internal/harness/fake":       true,
}

// bannedIOPackages are stdlib import paths forbidden in the pure cores.
// An import is forbidden if it equals an entry or starts with "<entry>/".
var bannedIOPackages = []string{"os", "io/ioutil", "net", "os/exec"}

// classify reports the layer a package (given a slash-separated path
// relative to the module root, e.g. "internal/harness/claudecode")
// belongs to.
func classify(pkgPath string) layer {
	switch {
	case pkgPath == "internal/domain":
		return layerDomain
	case pkgPath == compositionRootPackage:
		return layerCompositionRoot
	case pureCorePackages[pkgPath]:
		return layerPureCore
	case adapterPackages[pkgPath]:
		return layerAdapter
	case useCasePackages[pkgPath]:
		return layerUseCase
	case frontendPackages[pkgPath]:
		return layerFrontend
	default:
		return layerUnknown
	}
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

// pkgSource is one non-test .go file's parsed information.
type pkgSource struct {
	fileName     string
	imports      []string
	hasClockRead bool
}

// runChecks walks internal/ and cmd/ beneath moduleRoot, classifies every
// package directory it finds, and applies the layer rules plus the
// harness-isolation rule. cmd/ is walked too, and not merely trusted, so the
// composition root's exemption (compositionRootPackage) is checked against a
// real classification rather than assumed: everything under cmd/ other than
// that one path is classified — and harness-isolated — exactly as an
// internal/ package would be.
func runChecks(moduleRoot string) (violations []string, errs []string) {
	pkgDirs := map[string]string{}

	for _, sub := range []string{"internal", "cmd"} {
		dir := filepath.Join(moduleRoot, sub)
		found, err := discoverPackageDirs(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			errs = append(errs, fmt.Sprintf("walk %q: %v", dir, err))
			continue
		}
		for k, v := range found {
			pkgDirs[k] = v
		}
	}

	if len(errs) > 0 {
		return nil, errs
	}

	// Sort for deterministic output.
	dirKeys := make([]string, 0, len(pkgDirs))
	for k := range pkgDirs {
		dirKeys = append(dirKeys, k)
	}
	sort.Strings(dirKeys)

	for _, pkgPath := range dirKeys {
		absDir := pkgDirs[pkgPath]
		l := classify(pkgPath)

		sources, es := parsePackageSources(absDir)
		errs = append(errs, es...)

		if l == layerUnknown {
			violations = append(violations, fmt.Sprintf(
				"%s: package is not classified in the import checker's layer table — add it to tools/importcheck/main.go before it can pass",
				pkgPath))
			// Still run the harness-isolation check below; an unclassified
			// package gets no other benefit of the doubt.
		} else {
			violations = append(violations, checkLayerRule(pkgPath, l, sources)...)
		}

		violations = append(violations, checkHarnessIsolation(pkgPath, sources)...)
	}

	return violations, errs
}

// discoverPackageDirs walks dir (e.g. "<moduleRoot>/internal" or
// "<moduleRoot>/cmd") and returns a map of package path (relative to the
// module root, slash-separated, e.g. "internal/harness/claudecode" or
// "cmd/mosaic-agent-test") to its absolute directory, for every directory
// containing at least one non-test .go file.
func discoverPackageDirs(dir string) (map[string]string, error) {
	result := map[string]string{}

	moduleRoot := filepath.Dir(dir)

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		dir := filepath.Dir(path)
		rel, relErr := filepath.Rel(moduleRoot, dir)
		if relErr != nil {
			return nil
		}
		result[filepath.ToSlash(rel)] = dir
		return nil
	})

	return result, err
}

// parsePackageSources parses every non-test .go file directly within dir
// (not recursing into subdirectories — each subdirectory is its own
// package with its own rule).
func parsePackageSources(dir string) (sources []pkgSource, errs []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []string{fmt.Sprintf("ReadDir(%q): %v", dir, err)}
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

		filePath := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if err != nil {
			errs = append(errs, fmt.Sprintf("parse %q: %v", filePath, err))
			continue
		}

		imports := make([]string, 0, len(f.Imports))
		for _, imp := range f.Imports {
			imports = append(imports, strings.Trim(imp.Path.Value, `"`))
		}

		sources = append(sources, pkgSource{
			fileName:     name,
			imports:      imports,
			hasClockRead: containsClockRead(f),
		})
	}

	return sources, errs
}

// containsClockRead reports whether f contains a call to time.Now.
func containsClockRead(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name != "Now" {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != "time" {
			return true
		}
		found = true
		return false
	})
	return found
}

// checkLayerRule applies the import rules for the given layer to every
// source file in the package.
func checkLayerRule(pkgPath string, l layer, sources []pkgSource) (violations []string) {
	for _, src := range sources {
		location := pkgPath + "/" + src.fileName

		if l == layerPureCore && src.hasClockRead {
			violations = append(violations, fmt.Sprintf(
				"%s: calls time.Now — pure cores must take time as an explicit parameter",
				location))
		}

		for _, imp := range src.imports {
			if v := checkImportAgainstLayer(imp, pkgPath, l, location); v != "" {
				violations = append(violations, v)
			}
		}
	}
	return violations
}

// checkImportAgainstLayer returns a violation message if imp violates the
// rule for layer l, or "" if it is allowed.
func checkImportAgainstLayer(imp, pkgPath string, l layer, location string) string {
	switch l {
	case layerDomain:
		if strings.HasPrefix(imp, modulePrefix) {
			return fmt.Sprintf(
				"%s: imports %q — domain is the base layer and must not import any other package of this module",
				location, imp)
		}

	case layerPureCore:
		for _, banned := range bannedIOPackages {
			if imp == banned || strings.HasPrefix(imp, banned+"/") {
				return fmt.Sprintf(
					"%s: imports %q — pure cores must be free of I/O packages",
					location, imp)
			}
		}
		if strings.HasPrefix(imp, modulePrefix) {
			target := strings.TrimPrefix(imp, modulePrefix)
			tl := classify(target)
			if tl != layerDomain && tl != layerPureCore {
				return fmt.Sprintf(
					"%s: imports %q — pure cores may import only domain and other pure cores",
					location, imp)
			}
		}

	case layerAdapter:
		if strings.HasPrefix(imp, modulePrefix) {
			target := strings.TrimPrefix(imp, modulePrefix)
			tl := classify(target)
			if tl != layerDomain && tl != layerPureCore && !adapterImportExceptions[pkgPath][target] {
				return fmt.Sprintf(
					"%s: imports %q — adapters and stores may import only domain and the pure cores",
					location, imp)
			}
		}

	case layerUseCase:
		if strings.HasPrefix(imp, modulePrefix) {
			target := strings.TrimPrefix(imp, modulePrefix)
			tl := classify(target)
			if tl == layerFrontend {
				return fmt.Sprintf(
					"%s: imports %q — use cases must not import a frontend (cli or tui)",
					location, imp)
			}
			if tl == layerUnknown {
				return fmt.Sprintf(
					"%s: imports %q — target package is not classified in the layer table",
					location, imp)
			}
		}

	case layerFrontend:
		if strings.HasPrefix(imp, modulePrefix) {
			target := strings.TrimPrefix(imp, modulePrefix)
			tl := classify(target)
			if tl == layerFrontend {
				return fmt.Sprintf(
					"%s: imports %q — the two frontends must not import each other",
					location, imp)
			}
			if tl != layerDomain && tl != layerUseCase && !frontendImportExceptions[pkgPath][target] {
				return fmt.Sprintf(
					"%s: imports %q — frontends may import only domain and the use cases",
					location, imp)
			}
		}
	}

	return ""
}

// checkHarnessIsolation reports any import of a concrete harness adapter
// package from outside internal/harness/.
func checkHarnessIsolation(pkgPath string, sources []pkgSource) (violations []string) {
	if pkgPath == "internal/harness" || strings.HasPrefix(pkgPath, "internal/harness/") {
		return nil // the harness package tree may import its own subpackages
	}
	if pkgPath == compositionRootPackage {
		// The narrow exemption: only the binary composition root itself,
		// never a package that merely shares its path prefix, may import a
		// concrete adapter directly.
		return nil
	}

	for _, src := range sources {
		location := pkgPath + "/" + src.fileName
		for _, imp := range src.imports {
			if !strings.HasPrefix(imp, modulePrefix) {
				continue
			}
			target := strings.TrimPrefix(imp, modulePrefix)
			if concreteHarnessPackages[target] {
				violations = append(violations, fmt.Sprintf(
					"%s: imports %q — no package outside internal/harness/ may import a concrete harness adapter; use domain.HarnessAdapter instead",
					location, imp))
			}
		}
	}

	return violations
}
