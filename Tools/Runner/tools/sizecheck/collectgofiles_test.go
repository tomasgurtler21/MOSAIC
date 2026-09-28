package main

// Tests for collectGoFiles walk behavior and path filtering.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestTree creates a temporary module-root tree that exercises walk rules:
//
//	<root>/
//	  pkg/
//	    a.go
//	    b_test.go
//	  vendor/dep/dep.go         (walk rule: skip vendor/)
//	  testdata/fixture.go       (walk rule: skip testdata/)
//	  .hidden/h.go              (walk rule: skip dot-dirs)
//	  _ignored/x.go             (walk rule: skip underscore-dirs)
func newTestTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	writeMin := func(path string) {
		must(os.MkdirAll(filepath.Dir(path), 0o755))
		must(os.WriteFile(path, []byte("package p\n"), 0o644))
	}
	writeMin(filepath.Join(root, "pkg", "a.go"))
	writeMin(filepath.Join(root, "pkg", "b_test.go"))
	writeMin(filepath.Join(root, "vendor", "dep", "dep.go"))
	writeMin(filepath.Join(root, "testdata", "fixture.go"))
	writeMin(filepath.Join(root, ".hidden", "h.go"))
	writeMin(filepath.Join(root, "_ignored", "x.go"))
	return root
}

func TestCollectGoFiles_DefaultsToModuleRoot(t *testing.T) {
	root := newTestTree(t)
	files, err := collectGoFiles(root, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only pkg/a.go and pkg/b_test.go should be found.
	// vendor/, testdata/, .hidden/, _ignored/ must be skipped.
	if len(files) != 2 {
		t.Errorf("got %d files, want 2: %v", len(files), files)
	}
}

func TestCollectGoFiles_SkipsTestFiles_WhenIncludeTestsFalse(t *testing.T) {
	root := newTestTree(t)
	files, err := collectGoFiles(root, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only pkg/a.go; b_test.go must be excluded.
	if len(files) != 1 {
		t.Errorf("got %d files, want 1: %v", len(files), files)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			t.Errorf("included test file when includeTests=false: %s", f)
		}
	}
}

func TestCollectGoFiles_SkipsVendorDirectory(t *testing.T) {
	root := newTestTree(t)
	files, err := collectGoFiles(root, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range files {
		if strings.Contains(filepath.ToSlash(f), "/vendor/") {
			t.Errorf("included file from vendor/: %s", f)
		}
	}
}

func TestCollectGoFiles_SkipsTestdataDirectory(t *testing.T) {
	root := newTestTree(t)
	files, err := collectGoFiles(root, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range files {
		if strings.Contains(filepath.ToSlash(f), "/testdata/") {
			t.Errorf("included file from testdata/: %s", f)
		}
	}
}

func TestCollectGoFiles_SkipsDotDirectories(t *testing.T) {
	root := newTestTree(t)
	files, err := collectGoFiles(root, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range files {
		if strings.Contains(filepath.ToSlash(f), "/.hidden/") {
			t.Errorf("included file from dot-directory: %s", f)
		}
	}
}

func TestCollectGoFiles_SkipsUnderscoreDirectories(t *testing.T) {
	root := newTestTree(t)
	files, err := collectGoFiles(root, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range files {
		if strings.Contains(filepath.ToSlash(f), "/_ignored/") {
			t.Errorf("included file from underscore-directory: %s", f)
		}
	}
}

func TestCollectGoFiles_AcceptsFileArgument(t *testing.T) {
	root := newTestTree(t)
	target := filepath.Join(root, "pkg", "a.go")
	files, err := collectGoFiles(root, []string{target}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != target {
		t.Errorf("got %v, want [%s]", files, target)
	}
}

func TestCollectGoFiles_RejectsNonGoFileArgument(t *testing.T) {
	root := t.TempDir()
	notGo := filepath.Join(root, "README.md")
	if err := os.WriteFile(notGo, []byte("# readme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := collectGoFiles(root, []string{notGo}, true)
	if err == nil {
		t.Error("expected error for non-.go file argument, got nil")
	}
}

func TestCollectGoFiles_RejectsNonExistentPath(t *testing.T) {
	root := t.TempDir()
	_, err := collectGoFiles(root, []string{filepath.Join(root, "does-not-exist")}, true)
	if err == nil {
		t.Error("expected error for non-existent path, got nil")
	}
}

func TestCollectGoFiles_DeduplicatesOverlappingPaths(t *testing.T) {
	root := newTestTree(t)
	pkgDir := filepath.Join(root, "pkg")
	aFile := filepath.Join(root, "pkg", "a.go")
	// Passing both the directory and a file inside it should not duplicate the file.
	files, err := collectGoFiles(root, []string{pkgDir, aFile}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	seen := make(map[string]int)
	for _, f := range files {
		seen[f]++
	}
	for f, count := range seen {
		if count > 1 {
			t.Errorf("file %s appeared %d times, want 1", f, count)
		}
	}
}

func TestCollectGoFiles_ReturnsSortedPaths(t *testing.T) {
	root := newTestTree(t)
	files, err := collectGoFiles(root, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 1; i < len(files); i++ {
		if files[i] < files[i-1] {
			t.Errorf("files not sorted: %s before %s", files[i-1], files[i])
		}
	}
}
