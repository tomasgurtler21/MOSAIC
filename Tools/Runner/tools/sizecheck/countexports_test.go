package main

// Tests for countExports: counting exported symbols by directory,
// grouping, sorting, and filtering rules.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const exportsPkg = `package p

// ExportedFunc is a function without a receiver.
func ExportedFunc() {}

// unexportedFunc is not counted.
func unexportedFunc() {}

// ExportedType is a type.
type ExportedType struct{}

// unexportedType is not counted.
type unexportedType struct{}

// Method is not counted (has a receiver).
func (e ExportedType) Method() {}

// Pointer-receiver method is not counted.
func (e *ExportedType) PtrMethod() {}

// ExportedConst is counted. unexportedConst is not.
const (
	ExportedConst   = 1
	unexportedConst = 2
)

// ExportedVar is counted. unexportedVar is not.
var (
	ExportedVar   int
	unexportedVar int
)

// Blank identifier is not counted.
const _ = 3
`

func TestCountExports_CountsCorrectly(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg.go"), []byte(exportsPkg), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []string{filepath.Join(dir, "pkg.go")}
	result, err := countExports(root, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d package entries, want 1", len(result))
	}
	// ExportedFunc, ExportedType, ExportedConst, ExportedVar = 4
	const wantCount = 4
	if result[0].Count != wantCount {
		t.Errorf("Count = %d, want %d", result[0].Count, wantCount)
	}
}

func TestCountExports_MethodsNotCounted(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const onlyMethod = `package p

type T struct{}

// This method must not be counted.
func (t T) Exported() {}
func (t *T) ExportedPtr() {}
`
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte(onlyMethod), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []string{filepath.Join(dir, "m.go")}
	result, err := countExports(root, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d entries, want 1", len(result))
	}
	// Only T is exported (TypeSpec). Methods do not count.
	if result[0].Count != 1 {
		t.Errorf("Count = %d, want 1 (only the type)", result[0].Count)
	}
}

func TestCountExports_BlankIdentifierNotCounted(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const src = `package p

const _ = 1
var _ int
`
	if err := os.WriteFile(filepath.Join(dir, "blank.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := countExports(root, []string{filepath.Join(dir, "blank.go")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("got %d entries, want 0 (blank identifiers must not be counted)", len(result))
	}
}

func TestCountExports_GroupsByDirectory(t *testing.T) {
	root := t.TempDir()
	const small = "package p\nfunc Foo() {}\n"
	for _, name := range []string{"pkg1", "pkg2"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(small), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := []string{
		filepath.Join(root, "pkg1", "a.go"),
		filepath.Join(root, "pkg2", "a.go"),
	}
	result, err := countExports(root, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("got %d entries, want 2", len(result))
	}
	for _, pe := range result {
		if pe.Count != 1 {
			t.Errorf("pkg %s: Count = %d, want 1", pe.Dir, pe.Count)
		}
	}
}

func TestCountExports_SortedByDir(t *testing.T) {
	root := t.TempDir()
	const small = "package p\nfunc Foo() {}\n"
	for _, name := range []string{"zzz", "aaa", "mmm"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(small), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := []string{
		filepath.Join(root, "zzz", "a.go"),
		filepath.Join(root, "aaa", "a.go"),
		filepath.Join(root, "mmm", "a.go"),
	}
	result, err := countExports(root, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 1; i < len(result); i++ {
		if result[i].Dir < result[i-1].Dir {
			t.Errorf("result not sorted by Dir: %q before %q", result[i-1].Dir, result[i].Dir)
		}
	}
}

func TestCountExports_DirUsesSlashes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sub", "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := countExports(root, []string{filepath.Join(dir, "a.go")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d entries, want 1", len(result))
	}
	if strings.Contains(result[0].Dir, "\\") {
		t.Errorf("Dir uses backslashes: %q", result[0].Dir)
	}
}

func TestCountExports_DirWithOnlyTestFilesOmitted(t *testing.T) {
	// A package directory whose only .go files are *_test.go files must be
	// omitted from countExports output entirely (not appear with Count: 0).
	// This exercises the contract: "one line per package directory that has
	// at least one non-test .go file".
	//
	// The test mirrors the situation produced by collectGoFiles(includeTests=false):
	// it receives no files from the test-only directory, so no entry for it
	// should appear in the result.
	root := t.TempDir()
	// realDir has a non-test production file and must appear in the output.
	realDir := filepath.Join(root, "realpkg")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "pkg.go"), []byte("package p\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// testOnlyDir exists on disk but its only .go file is a test file.
	// It is not included in the files slice passed to countExports (as
	// collectGoFiles with includeTests=false would do).
	testOnlyDir := filepath.Join(root, "testonlypkg")
	if err := os.MkdirAll(testOnlyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{filepath.Join(realDir, "pkg.go")}
	result, err := countExports(root, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("got %d entries, want exactly 1 (test-only directory must be omitted): %v", len(result), result)
	}
	for _, pe := range result {
		if strings.Contains(pe.Dir, "testonlypkg") {
			t.Errorf("test-only directory appeared in countExports output: %v", pe)
		}
	}
}
