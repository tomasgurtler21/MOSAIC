// Command sizecheck enforces file-size and function-size limits in the
// mosaic-run module.
//
// Run from the module root (Tools/Runner/):
//
//	go run ./tools/sizecheck [PATH ...]
//
// Or via the Taskfile:
//
//	task check:size
//
// Exit codes:
//
//	0  no violations (size mode) or report printed (-exports mode)
//	1  one or more violations (size mode only)
//	2  usage error, path-not-found, non-.go file argument, or parse error
//
// Size rules:
//
//   - File rule: a file with more than 500 physical lines ('\n' count) is a violation.
//   - Function rule: an *ast.FuncDecl with a body that spans more than 150 lines
//     (from the func keyword line to the closing brace line, inclusive; doc comment
//     excluded) is a violation. Function literals count toward their enclosing declaration.
//
// See ContractsDesign.md "sizecheck CLI (Stage 1)" for the full specification.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// violationKind identifies which size rule was broken.
type violationKind int

const (
	fileTooLong violationKind = iota + 1
	funcTooLong
)

// violation represents one size-rule breach.
type violation struct {
	Kind  violationKind
	Path  string // slash-separated, relative to the module root
	Lines int    // measured line count
	Func  string // funcTooLong only: "Name", "Recv.Name" or "(*Recv).Name"
	Start int    // funcTooLong only: line of the `func` keyword (1-based)
	End   int    // funcTooLong only: line of the closing brace (1-based)
}

// packageExports holds the exported top-level symbol count for one package directory.
type packageExports struct {
	Dir   string // slash-separated package directory, relative to the module root
	Count int    // exported top-level identifiers in non-test files
}

// String renders one violation line as specified in the CLI contract:
//
//	FILE <relpath>: <n> lines (limit 500)
//	FUNC <relpath>:<start>-<end> <name>: <n> lines (limit 150)
func (v violation) String() string {
	switch v.Kind {
	case fileTooLong:
		return fmt.Sprintf("FILE %s: %d lines (limit 500)", v.Path, v.Lines)
	case funcTooLong:
		return fmt.Sprintf("FUNC %s:%d-%d %s: %d lines (limit 150)", v.Path, v.Start, v.End, v.Func, v.Lines)
	}
	return ""
}

// run is the whole CLI entry point.
// main() does only: os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)).
// It returns the exit code (0, 1 or 2) as described in the CLI contract.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sizecheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress flag's own output; we write our own messages
	var root string
	var exportsMode bool
	fs.StringVar(&root, "root", ".", "module root directory")
	fs.BoolVar(&exportsMode, "exports", false, "print exported symbol counts per package")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "sizecheck: %v\n", err)
		return 2
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(stderr, "sizecheck: %v\n", err)
		return 2
	}

	paths := fs.Args()

	if exportsMode {
		files, err := collectGoFiles(absRoot, paths, false)
		if err != nil {
			fmt.Fprintf(stderr, "sizecheck: %v\n", err)
			return 2
		}
		exports, err := countExports(absRoot, files)
		if err != nil {
			fmt.Fprintf(stderr, "sizecheck: %v\n", err)
			return 2
		}
		for _, pe := range exports {
			fmt.Fprintf(stdout, "EXPORTS %s: %d\n", pe.Dir, pe.Count)
		}
		return 0
	}

	// Size mode: collect all violations before printing any output.
	files, err := collectGoFiles(absRoot, paths, true)
	if err != nil {
		fmt.Fprintf(stderr, "sizecheck: %v\n", err)
		return 2
	}

	var allViolations []violation
	for _, f := range files {
		vs, err := checkFile(absRoot, f)
		if err != nil {
			fmt.Fprintf(stderr, "sizecheck: %v\n", err)
			return 2
		}
		allViolations = append(allViolations, vs...)
	}

	// Sort violations: by path, then file violation before func violations,
	// then func violations by start line.
	sort.Slice(allViolations, func(i, j int) bool {
		a, b := allViolations[i], allViolations[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind // fileTooLong(1) before funcTooLong(2)
		}
		return a.Start < b.Start
	})

	for _, v := range allViolations {
		fmt.Fprintln(stdout, v.String())
	}
	if len(allViolations) == 0 {
		fmt.Fprintln(stdout, "size limits OK")
		return 0
	}
	fmt.Fprintf(stdout, "%d size violation(s)\n", len(allViolations))
	return 1
}

// collectGoFiles resolves PATH arguments against moduleRoot and applies the walk rules.
// Empty paths means the whole module root. It returns absolute, de-duplicated,
// sorted file paths. includeTests=false drops *_test.go files (exports mode).
// Errors: a path that does not exist, or a file argument that does not end in .go.
func collectGoFiles(moduleRoot string, paths []string, includeTests bool) ([]string, error) {
	if len(paths) == 0 {
		paths = []string{moduleRoot}
	}

	seen := make(map[string]bool)

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}

		if !info.IsDir() {
			// Direct file argument: must end in .go.
			if !strings.HasSuffix(p, ".go") {
				return nil, fmt.Errorf("file argument does not end in .go: %s", p)
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				return nil, err
			}
			seen[abs] = true
			continue
		}

		// Directory: walk recursively, applying skip rules to subdirectories.
		err = filepath.Walk(p, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if fi.IsDir() {
				if path == p {
					return nil // never skip the root being walked
				}
				name := fi.Name()
				if name == "vendor" || name == "testdata" ||
					strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if !includeTests && strings.HasSuffix(path, "_test.go") {
				return nil
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			seen[abs] = true
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	result := make([]string, 0, len(seen))
	for f := range seen {
		result = append(result, f)
	}
	sort.Strings(result)
	return result, nil
}

// checkFile applies the file rule and the function rule to one Go file.
// Returned violations use paths relative to moduleRoot and are in output order
// (file violation first, then function violations sorted by start line).
// Errors: read error or parse error.
func checkFile(moduleRoot, path string) ([]violation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	lineCount := strings.Count(string(data), "\n")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, data, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	relPath, err := filepath.Rel(moduleRoot, path)
	if err != nil {
		relPath = path
	}
	relPath = filepath.ToSlash(relPath)

	var violations []violation

	// File rule: more than 500 physical lines.
	if lineCount > 500 {
		violations = append(violations, violation{
			Kind:  fileTooLong,
			Path:  relPath,
			Lines: lineCount,
		})
	}

	// Function rule: FuncDecl with body spanning more than 150 lines.
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		startLine := fset.Position(fd.Pos()).Line
		endLine := fset.Position(fd.Body.Rbrace).Line
		span := endLine - startLine + 1
		if span > 150 {
			violations = append(violations, violation{
				Kind:  funcTooLong,
				Path:  relPath,
				Lines: span,
				Func:  funcDeclName(fd),
				Start: startLine,
				End:   endLine,
			})
		}
	}

	// Sort: file violation first, then func violations by start line.
	sort.Slice(violations, func(i, j int) bool {
		a, b := violations[i], violations[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Start < b.Start
	})

	return violations, nil
}

// funcDeclName returns the display name for a function declaration:
// "Name" for functions, "Recv.Name" for value receivers, "(*Recv).Name" for pointer receivers.
// Type parameters are omitted.
func funcDeclName(decl *ast.FuncDecl) string {
	name := decl.Name.Name
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return name
	}
	recvName, isPtr := recvTypeName(decl.Recv.List[0].Type)
	if isPtr {
		return "(*" + recvName + ")." + name
	}
	return recvName + "." + name
}

// recvTypeName extracts the base type name and pointer flag from a receiver type expression.
func recvTypeName(expr ast.Expr) (name string, pointer bool) {
	switch r := expr.(type) {
	case *ast.Ident:
		return r.Name, false
	case *ast.StarExpr:
		n, _ := recvTypeName(r.X)
		return n, true
	case *ast.IndexExpr: // generic: T[P]
		n, _ := recvTypeName(r.X)
		return n, false
	}
	// ast.IndexListExpr (T[P, Q]) and other cases: fall through to empty string.
	return "", false
}

// countExports groups the given non-test files by directory and counts the
// exported top-level identifiers per directory. The result is sorted by Dir.
// Counted identifiers: FuncDecl without a receiver, TypeSpec, and each exported
// name in a const or var ValueSpec. Methods, blank identifiers, and unexported
// names are not counted. Directories with a count of zero are omitted.
func countExports(moduleRoot string, files []string) ([]packageExports, error) {
	// Group files by their directory.
	dirFiles := make(map[string][]string)
	for _, f := range files {
		dir := filepath.Dir(f)
		dirFiles[dir] = append(dirFiles[dir], f)
	}

	var result []packageExports
	for dir, filePaths := range dirFiles {
		count := 0
		for _, fp := range filePaths {
			data, err := os.ReadFile(fp)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", fp, err)
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, fp, data, 0)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", fp, err)
			}
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					// Top-level functions only (no receiver).
					if d.Recv == nil && ast.IsExported(d.Name.Name) {
						count++
					}
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						switch s := spec.(type) {
						case *ast.TypeSpec:
							if ast.IsExported(s.Name.Name) {
								count++
							}
						case *ast.ValueSpec:
							for _, nm := range s.Names {
								if nm.Name != "_" && ast.IsExported(nm.Name) {
									count++
								}
							}
						}
					}
				}
			}
		}

		if count == 0 {
			continue
		}

		relDir, err := filepath.Rel(moduleRoot, dir)
		if err != nil {
			relDir = dir
		}
		relDir = filepath.ToSlash(relDir)

		result = append(result, packageExports{Dir: relDir, Count: count})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Dir < result[j].Dir
	})

	return result, nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
