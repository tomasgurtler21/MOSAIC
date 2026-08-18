package fixtures_test

// Tests for Resolver: $ref resolution against a fixture root.
//
// Coverage follows T4.3: a reference resolving to file content, a reference
// to a path that does not exist, a reference escaping the fixture root, and
// a reference cycle. Path traversal outside the fixture root must be
// refused rather than resolved (AC4.5).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/fixtures"
)

// newRootWithFiles creates a fixture root under t.TempDir() containing the
// given relative-path -> content entries, and returns the root and a ready
// Resolver.
func newRootWithFiles(t *testing.T, files map[string]string) (string, fixtures.Resolver) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", full, err)
		}
	}

	resolver, err := fixtures.NewResolver(root)
	if err != nil {
		t.Fatalf("NewResolver(%q) error = %v, want nil", root, err)
	}
	return root, resolver
}

func TestResolve_ExistingFile_ReturnsContent(t *testing.T) {
	_, resolver := newRootWithFiles(t, map[string]string{
		"library-research.md": "# Library Research\n",
	})

	got, err := resolver.Resolve("library-research.md")
	if err != nil {
		t.Fatalf("Resolve error = %v, want nil", err)
	}
	if string(got) != "# Library Research\n" {
		t.Errorf("Resolve content = %q, want %q", got, "# Library Research\n")
	}
}

func TestResolve_MissingFile_ReturnsErrRefNotFound(t *testing.T) {
	_, resolver := newRootWithFiles(t, map[string]string{
		"present.md": "content",
	})

	_, err := resolver.Resolve("absent.md")
	if !errors.Is(err, fixtures.ErrRefNotFound) {
		t.Errorf("Resolve error = %v, want errors.Is(err, ErrRefNotFound)", err)
	}
}

func TestResolve_PathEscapingRoot_ReturnsErrRefEscapesRoot(t *testing.T) {
	root, resolver := newRootWithFiles(t, map[string]string{
		"present.md": "content",
	})

	// A file that genuinely exists just outside the fixture root — proves
	// the refusal is about traversal, not mere absence.
	outside := filepath.Join(filepath.Dir(root), "outside.md")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", outside, err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	_, err := resolver.Resolve("../outside.md")
	if !errors.Is(err, fixtures.ErrRefEscapesRoot) {
		t.Errorf("Resolve error = %v, want errors.Is(err, ErrRefEscapesRoot)", err)
	}
}

func TestResolve_ReferenceCycle_ReturnsErrRefCycle(t *testing.T) {
	// a.md refers to b.md which refers back to a.md. The exact in-file
	// $ref syntax is an authoring-format concern (internal/authoring); the
	// resolver only needs to be given a root whose content graph forms a
	// cycle to prove it refuses to loop forever.
	_, resolver := newRootWithFiles(t, map[string]string{
		"a.md": `{"$ref": "b.md"}`,
		"b.md": `{"$ref": "a.md"}`,
	})

	_, err := resolver.Resolve("a.md")
	if !errors.Is(err, fixtures.ErrRefCycle) {
		t.Errorf("Resolve error = %v, want errors.Is(err, ErrRefCycle)", err)
	}
}

func TestValidate_ExistingFile_NoError(t *testing.T) {
	_, resolver := newRootWithFiles(t, map[string]string{
		"present.md": "content",
	})

	if err := resolver.Validate("present.md"); err != nil {
		t.Errorf("Validate error = %v, want nil", err)
	}
}

// TestValidate_MirrorsResolveErrors proves Validate performs the same
// checks as Resolve without requiring the caller to load any content, so
// pre-flight can verify every reference in one pass before running anything.
func TestValidate_MirrorsResolveErrors(t *testing.T) {
	root, resolver := newRootWithFiles(t, map[string]string{
		"present.md": "content",
		"cycle-a.md": `{"$ref": "cycle-b.md"}`,
		"cycle-b.md": `{"$ref": "cycle-a.md"}`,
	})
	outside := filepath.Join(filepath.Dir(root), "outside.md")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", outside, err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	cases := []struct {
		name    string
		ref     string
		wantErr error
	}{
		{"missing", "absent.md", fixtures.ErrRefNotFound},
		{"escaping", "../outside.md", fixtures.ErrRefEscapesRoot},
		{"cycle", "cycle-a.md", fixtures.ErrRefCycle},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := resolver.Validate(tc.ref)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Validate(%q) error = %v, want errors.Is(err, %v)", tc.ref, err, tc.wantErr)
			}
		})
	}
}

func TestResolve_ErrorNamesTheOffendingReference(t *testing.T) {
	_, resolver := newRootWithFiles(t, map[string]string{
		"present.md": "content",
	})

	_, err := resolver.Resolve("does-not-exist.md")
	if err == nil {
		t.Fatal("Resolve error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist.md") {
		t.Errorf("Resolve error %q does not name the offending reference %q", err.Error(), "does-not-exist.md")
	}
}
