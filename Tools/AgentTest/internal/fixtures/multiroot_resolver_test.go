package fixtures_test

// Tests for multi-root fixture resolution: NewMultiRootResolver,
// NewResolverFactory, and ResolutionError.
//
// These tests are in the TDD RED phase: the stub implementations in
// multiroot.go allow compilation, but every test here fails until the
// implementation agent (I1.1) provides the real behaviour.
//
// Coverage:
//   - A ref in the document-local root resolves successfully.
//   - A ref absent from the local root but present in the shared fallback
//     root resolves successfully.
//   - A ref absent from all roots produces a *ResolutionError whose
//     SearchedRoots field names every searched location.
//   - A ref present in both roots resolves to the local root's copy
//     (document-local takes precedence).
//   - Containment guarantees (absolute paths, drive-letter paths, UNC
//     paths, ".." escape) are enforced against both roots.
//   - Cycle detection still fires when the ref chain loops.
//   - ResolverFactory produces per-document resolvers that follow the same
//     rules.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/fixtures"
)

// newDir creates a subdirectory under t.TempDir() and optionally populates
// it with files. It returns the absolute path to the directory.
func newDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", full, err)
		}
	}
	return dir
}

// --- NewMultiRootResolver: successful resolution ---

// TestNewMultiRootResolver_LocalRootHit verifies that a ref present in the
// first (document-local) root is resolved successfully and its content is
// returned, even when the same ref is absent from the second root.
func TestNewMultiRootResolver_LocalRootHit(t *testing.T) {
	localRoot := newDir(t, map[string]string{
		"fixture.md": "# Local fixture\n",
	})
	sharedRoot := newDir(t, nil) // does not contain fixture.md

	r, err := fixtures.NewMultiRootResolver(localRoot, sharedRoot)
	if err != nil {
		t.Fatalf("NewMultiRootResolver error = %v, want nil", err)
	}

	got, err := r.Resolve("fixture.md")
	if err != nil {
		t.Fatalf("Resolve(fixture.md) error = %v, want nil (file is in local root)", err)
	}
	if string(got) != "# Local fixture\n" {
		t.Errorf("Resolve content = %q, want %q", string(got), "# Local fixture\n")
	}
}

// TestNewMultiRootResolver_SharedRootFallback verifies that a ref absent
// from the first root is resolved from the second (shared fallback) root.
func TestNewMultiRootResolver_SharedRootFallback(t *testing.T) {
	localRoot := newDir(t, nil) // does not contain the ref
	sharedRoot := newDir(t, map[string]string{
		"shared.md": "# Shared fixture\n",
	})

	r, err := fixtures.NewMultiRootResolver(localRoot, sharedRoot)
	if err != nil {
		t.Fatalf("NewMultiRootResolver error = %v, want nil", err)
	}

	got, err := r.Resolve("shared.md")
	if err != nil {
		t.Fatalf("Resolve(shared.md) error = %v, want nil (file is in shared root)", err)
	}
	if string(got) != "# Shared fixture\n" {
		t.Errorf("Resolve content = %q, want %q", string(got), "# Shared fixture\n")
	}
}

// TestNewMultiRootResolver_LocalPrecedence verifies that when a ref exists
// in both roots, the local root's copy is returned (document-local wins).
func TestNewMultiRootResolver_LocalPrecedence(t *testing.T) {
	localRoot := newDir(t, map[string]string{
		"common.md": "# Local copy\n",
	})
	sharedRoot := newDir(t, map[string]string{
		"common.md": "# Shared copy\n",
	})

	r, err := fixtures.NewMultiRootResolver(localRoot, sharedRoot)
	if err != nil {
		t.Fatalf("NewMultiRootResolver error = %v, want nil", err)
	}

	got, err := r.Resolve("common.md")
	if err != nil {
		t.Fatalf("Resolve(common.md) error = %v, want nil", err)
	}
	if string(got) != "# Local copy\n" {
		t.Errorf("Resolve returned %q, want %q (local root must win)", string(got), "# Local copy\n")
	}
}

// --- NewMultiRootResolver: failure cases ---

// TestNewMultiRootResolver_NeitherRoot_ReturnsResolutionError verifies that
// when a ref is absent from all roots, the returned error is of type
// *ResolutionError and its SearchedRoots field lists every searched location.
func TestNewMultiRootResolver_NeitherRoot_ReturnsResolutionError(t *testing.T) {
	localRoot := newDir(t, nil)
	sharedRoot := newDir(t, nil)

	r, err := fixtures.NewMultiRootResolver(localRoot, sharedRoot)
	if err != nil {
		t.Fatalf("NewMultiRootResolver error = %v, want nil", err)
	}

	_, err = r.Resolve("absent.md")
	if err == nil {
		t.Fatal("Resolve(absent.md) error = nil, want non-nil")
	}

	// The error must satisfy errors.Is(err, ErrRefNotFound) for compatibility
	// with existing callers.
	if !errors.Is(err, fixtures.ErrRefNotFound) {
		t.Errorf("errors.Is(err, ErrRefNotFound) = false, want true; err = %v", err)
	}

	// The error must be a *ResolutionError naming every location searched.
	var resErr *fixtures.ResolutionError
	if !errors.As(err, &resErr) {
		t.Fatalf("errors.As(err, *ResolutionError) = false, want true; err type = %T", err)
	}
	if len(resErr.SearchedRoots) != 2 {
		t.Errorf("ResolutionError.SearchedRoots len = %d, want 2 (one per root); roots = %v",
			len(resErr.SearchedRoots), resErr.SearchedRoots)
	}
	if resErr.Ref != "absent.md" {
		t.Errorf("ResolutionError.Ref = %q, want %q", resErr.Ref, "absent.md")
	}
}

// TestNewMultiRootResolver_NeitherRoot_ErrorNamesAllLocations verifies that
// the error message from a failed multi-root resolution names every searched
// location, so the author can see where the resolver looked.
func TestNewMultiRootResolver_NeitherRoot_ErrorNamesAllLocations(t *testing.T) {
	localRoot := newDir(t, nil)
	sharedRoot := newDir(t, nil)

	r, err := fixtures.NewMultiRootResolver(localRoot, sharedRoot)
	if err != nil {
		t.Fatalf("NewMultiRootResolver error = %v, want nil", err)
	}

	_, err = r.Resolve("missing.md")
	if err == nil {
		t.Fatal("Resolve(missing.md) error = nil, want non-nil")
	}

	msg := err.Error()
	// The error message must contain both searched root paths, so the author
	// can immediately see where the resolver looked. filepath.Abs is used
	// because NewMultiRootResolver absolutizes its roots at construction time;
	// the message must name those absolute paths.
	absLocal, _ := filepath.Abs(localRoot)
	absShared, _ := filepath.Abs(sharedRoot)
	if !strings.Contains(msg, absLocal) {
		t.Errorf("error message does not contain local root path %q; message = %q", absLocal, msg)
	}
	if !strings.Contains(msg, absShared) {
		t.Errorf("error message does not contain shared root path %q; message = %q", absShared, msg)
	}
	// The ref itself must appear in the message so the author can see which
	// ref was unresolvable.
	if !strings.Contains(msg, "missing.md") {
		t.Errorf("error message does not contain the ref %q; message = %q", "missing.md", msg)
	}
}

// TestNewMultiRootResolver_Validate_NeitherRoot_ReturnsResolutionError
// verifies that Validate (not just Resolve) returns a *ResolutionError when
// the ref is absent from all roots. Preflight calls Validate, not Resolve.
func TestNewMultiRootResolver_Validate_NeitherRoot_ReturnsResolutionError(t *testing.T) {
	localRoot := newDir(t, nil)
	sharedRoot := newDir(t, nil)

	r, err := fixtures.NewMultiRootResolver(localRoot, sharedRoot)
	if err != nil {
		t.Fatalf("NewMultiRootResolver error = %v, want nil", err)
	}

	err = r.Validate("absent.md")
	if err == nil {
		t.Fatal("Validate(absent.md) error = nil, want non-nil")
	}

	var resErr *fixtures.ResolutionError
	if !errors.As(err, &resErr) {
		t.Fatalf("errors.As(err, *ResolutionError) = false, want true; err = %v", err)
	}
	if len(resErr.SearchedRoots) != 2 {
		t.Errorf("ResolutionError.SearchedRoots len = %d, want 2; roots = %v",
			len(resErr.SearchedRoots), resErr.SearchedRoots)
	}
}

// --- Containment guarantees still enforced with both roots ---
//
// Implementation note for the implementation agent (I1.1):
// The four tests below assert errors.Is(err, ErrRefEscapesRoot) for structural
// violations (absolute paths, drive letters, UNC paths, ".." escape). The
// implementation MUST propagate ErrRefEscapesRoot directly — not wrapped inside
// a *ResolutionError — for these cases. *ResolutionError.Unwrap() returns
// ErrRefNotFound, so if the implementation wraps a structural violation inside
// *ResolutionError, errors.Is(err, ErrRefEscapesRoot) returns false and all
// four tests fail for an unexpected reason. Structural invalidity (ref format
// is wrong regardless of any root) must be distinguished from "file not found
// after searching all roots."

// TestNewMultiRootResolver_AbsolutePath_RejectedByBothRoots verifies that an
// absolute path reference is still refused with ErrRefEscapesRoot, regardless
// of which roots are configured.
func TestNewMultiRootResolver_AbsolutePath_RejectedByBothRoots(t *testing.T) {
	localRoot := newDir(t, nil)
	sharedRoot := newDir(t, nil)

	r, err := fixtures.NewMultiRootResolver(localRoot, sharedRoot)
	if err != nil {
		t.Fatalf("NewMultiRootResolver error = %v, want nil", err)
	}

	absRef := filepath.Join(localRoot, "fixture.md") // absolute path
	err = r.Validate(absRef)
	if !errors.Is(err, fixtures.ErrRefEscapesRoot) {
		t.Errorf("Validate(absolute path) error = %v, want errors.Is(err, ErrRefEscapesRoot)", err)
	}
}

// TestNewMultiRootResolver_DotDotEscape_RejectedByBothRoots verifies that a
// reference using ".." to escape the fixture root is refused with
// ErrRefEscapesRoot, even though two roots are configured. The containment
// check applies independently to each root — a ref that escapes root A is
// also tested against root B, but "../outside.md" escapes every rooted path.
func TestNewMultiRootResolver_DotDotEscape_RejectedByBothRoots(t *testing.T) {
	localRoot := newDir(t, nil)
	sharedRoot := newDir(t, nil)

	// Place a real file outside both roots so the refusal is about
	// traversal, not mere absence.
	outside := filepath.Join(filepath.Dir(localRoot), "outside-both.md")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", outside, err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	r, err := fixtures.NewMultiRootResolver(localRoot, sharedRoot)
	if err != nil {
		t.Fatalf("NewMultiRootResolver error = %v, want nil", err)
	}

	err = r.Validate("../outside-both.md")
	if !errors.Is(err, fixtures.ErrRefEscapesRoot) {
		t.Errorf("Validate(../outside-both.md) error = %v, want errors.Is(err, ErrRefEscapesRoot)", err)
	}
}

// TestNewMultiRootResolver_DriveLetter_RejectedByBothRoots verifies that a
// Windows drive-letter reference is refused with ErrRefEscapesRoot.
func TestNewMultiRootResolver_DriveLetter_RejectedByBothRoots(t *testing.T) {
	localRoot := newDir(t, nil)
	sharedRoot := newDir(t, nil)

	r, err := fixtures.NewMultiRootResolver(localRoot, sharedRoot)
	if err != nil {
		t.Fatalf("NewMultiRootResolver error = %v, want nil", err)
	}

	err = r.Validate("C:\\secret.txt")
	if !errors.Is(err, fixtures.ErrRefEscapesRoot) {
		t.Errorf("Validate(drive letter) error = %v, want errors.Is(err, ErrRefEscapesRoot)", err)
	}
}

// TestNewMultiRootResolver_UNCPath_RejectedByBothRoots verifies that a UNC
// path reference is refused with ErrRefEscapesRoot.
func TestNewMultiRootResolver_UNCPath_RejectedByBothRoots(t *testing.T) {
	localRoot := newDir(t, nil)
	sharedRoot := newDir(t, nil)

	r, err := fixtures.NewMultiRootResolver(localRoot, sharedRoot)
	if err != nil {
		t.Fatalf("NewMultiRootResolver error = %v, want nil", err)
	}

	err = r.Validate("\\\\server\\share\\file.md")
	if !errors.Is(err, fixtures.ErrRefEscapesRoot) {
		t.Errorf("Validate(UNC) error = %v, want errors.Is(err, ErrRefEscapesRoot)", err)
	}
}

// TestNewMultiRootResolver_Cycle_Detected verifies that a reference cycle
// is still detected (ErrRefCycle) when using a multi-root resolver. The
// cycle detection applies once a root is selected and the recursive traversal
// begins.
func TestNewMultiRootResolver_Cycle_Detected(t *testing.T) {
	localRoot := newDir(t, map[string]string{
		"a.md": `{"$ref": "b.md"}`,
		"b.md": `{"$ref": "a.md"}`,
	})
	sharedRoot := newDir(t, nil)

	r, err := fixtures.NewMultiRootResolver(localRoot, sharedRoot)
	if err != nil {
		t.Fatalf("NewMultiRootResolver error = %v, want nil", err)
	}

	err = r.Validate("a.md")
	if !errors.Is(err, fixtures.ErrRefCycle) {
		t.Errorf("Validate(cycle) error = %v, want errors.Is(err, ErrRefCycle)", err)
	}
}

// --- ResolverFactory ---

// TestNewResolverFactory_DocDirOnly verifies that a factory built with an
// empty sharedRoot produces resolvers that search only the document directory.
// A ref present in docDir resolves; a ref absent from it does not.
func TestNewResolverFactory_DocDirOnly(t *testing.T) {
	docDir := newDir(t, map[string]string{
		"local.md": "# local content\n",
	})

	factory, err := fixtures.NewResolverFactory("") // no shared root
	if err != nil {
		t.Fatalf("NewResolverFactory error = %v, want nil", err)
	}

	r, err := factory(docDir)
	if err != nil {
		t.Fatalf("factory(%q) error = %v, want nil", docDir, err)
	}

	// Ref in docDir resolves.
	got, err := r.Resolve("local.md")
	if err != nil {
		t.Fatalf("Resolve(local.md) error = %v, want nil", err)
	}
	if string(got) != "# local content\n" {
		t.Errorf("Resolve content = %q, want %q", string(got), "# local content\n")
	}
}

// TestNewResolverFactory_DocDirFirstSharedRootFallback verifies that a
// factory built with a non-empty sharedRoot produces resolvers that search
// docDir first, then sharedRoot. A ref absent from docDir but present in
// sharedRoot resolves successfully.
func TestNewResolverFactory_DocDirFirstSharedRootFallback(t *testing.T) {
	docDir := newDir(t, nil) // does not contain "in-shared.md"
	sharedRoot := newDir(t, map[string]string{
		"in-shared.md": "# shared content\n",
	})

	factory, err := fixtures.NewResolverFactory(sharedRoot)
	if err != nil {
		t.Fatalf("NewResolverFactory(%q) error = %v, want nil", sharedRoot, err)
	}

	r, err := factory(docDir)
	if err != nil {
		t.Fatalf("factory(%q) error = %v, want nil", docDir, err)
	}

	got, err := r.Resolve("in-shared.md")
	if err != nil {
		t.Fatalf("Resolve(in-shared.md) error = %v, want nil (file is in shared root)", err)
	}
	if string(got) != "# shared content\n" {
		t.Errorf("Resolve content = %q, want %q", string(got), "# shared content\n")
	}
}

// TestNewResolverFactory_DocDirWinsOverSharedRoot verifies that when the
// same ref exists in both docDir and sharedRoot, the docDir copy wins.
func TestNewResolverFactory_DocDirWinsOverSharedRoot(t *testing.T) {
	docDir := newDir(t, map[string]string{
		"common.md": "# doc-dir copy\n",
	})
	sharedRoot := newDir(t, map[string]string{
		"common.md": "# shared-root copy\n",
	})

	factory, err := fixtures.NewResolverFactory(sharedRoot)
	if err != nil {
		t.Fatalf("NewResolverFactory error = %v, want nil", err)
	}

	r, err := factory(docDir)
	if err != nil {
		t.Fatalf("factory(%q) error = %v, want nil", docDir, err)
	}

	got, err := r.Resolve("common.md")
	if err != nil {
		t.Fatalf("Resolve(common.md) error = %v, want nil", err)
	}
	if string(got) != "# doc-dir copy\n" {
		t.Errorf("Resolve returned %q, want doc-dir copy %q", string(got), "# doc-dir copy\n")
	}
}

// TestNewResolverFactory_DifferentDocDirsGetDifferentResolvers verifies that
// the factory produces independent resolvers for different document directories.
// This is the core property that enables per-document resolution: each test
// definition's refs are resolved relative to that definition's own directory,
// not to some other definition's directory.
func TestNewResolverFactory_DifferentDocDirsGetDifferentResolvers(t *testing.T) {
	dirA := newDir(t, map[string]string{
		"only-in-a.md": "# in A\n",
	})
	dirB := newDir(t, map[string]string{
		"only-in-b.md": "# in B\n",
	})

	factory, err := fixtures.NewResolverFactory("")
	if err != nil {
		t.Fatalf("NewResolverFactory error = %v, want nil", err)
	}

	rA, err := factory(dirA)
	if err != nil {
		t.Fatalf("factory(dirA) error = %v, want nil", err)
	}
	rB, err := factory(dirB)
	if err != nil {
		t.Fatalf("factory(dirB) error = %v, want nil", err)
	}

	// Resolver A can find its file but not B's.
	if _, err := rA.Resolve("only-in-a.md"); err != nil {
		t.Errorf("rA.Resolve(only-in-a.md) error = %v, want nil", err)
	}
	if _, err := rA.Resolve("only-in-b.md"); err == nil {
		t.Error("rA.Resolve(only-in-b.md) error = nil, want error (file is not in dirA)")
	}

	// Resolver B can find its file but not A's.
	if _, err := rB.Resolve("only-in-b.md"); err != nil {
		t.Errorf("rB.Resolve(only-in-b.md) error = %v, want nil", err)
	}
	if _, err := rB.Resolve("only-in-a.md"); err == nil {
		t.Error("rB.Resolve(only-in-a.md) error = nil, want error (file is not in dirB)")
	}
}

// --- ResolverFactory: docDir/fixtures/ subdirectory (TDD RED for I1.1) ---
//
// The four tests below specify the behaviour introduced by I1.1: when
// NewResolverFactory builds a resolver for a docDir that contains a
// fixtures/ subdirectory, that subdirectory is inserted as a search root
// between docDir and sharedRoot. Precedence order after the fix:
//
//   (1) docDir          -- highest
//   (2) docDir/fixtures/
//   (3) sharedRoot      -- lowest (fallback)
//
// When no fixtures/ subdirectory exists the resolver must behave exactly as
// before (two roots only, no phantom entry in SearchedRoots).
//
// Tests (a) and (c) fail with the current stub implementation and turn green
// only after I1.1. Tests (b) and (d) verify that existing correct behaviour
// is preserved; they may already pass and must continue to do so.

// TestNewResolverFactory_FixturesSubdir_RefResolvesFromSubdir verifies that
// a bare-filename ref that exists only inside docDir/fixtures/ resolves
// successfully. This is the primary new behaviour: the nested fixtures/
// subdirectory becomes a search root when it exists on disk.
func TestNewResolverFactory_FixturesSubdir_RefResolvesFromSubdir(t *testing.T) {
	// docDir has a fixtures/ subdir; the ref exists only there.
	docDir := newDir(t, map[string]string{
		"fixtures/in-subdir.md": "# in fixtures subdir\n",
	})
	sharedRoot := newDir(t, nil)

	factory, err := fixtures.NewResolverFactory(sharedRoot)
	if err != nil {
		t.Fatalf("NewResolverFactory error = %v, want nil", err)
	}

	r, err := factory(docDir)
	if err != nil {
		t.Fatalf("factory(%q) error = %v, want nil", docDir, err)
	}

	// The bare filename must resolve from docDir/fixtures/ (requires I1.1).
	got, err := r.Resolve("in-subdir.md")
	if err != nil {
		t.Fatalf("Resolve(in-subdir.md) error = %v, want nil "+
			"(file exists only in docDir/fixtures/; requires I1.1 to pass)", err)
	}
	if string(got) != "# in fixtures subdir\n" {
		t.Errorf("Resolve content = %q, want %q", string(got), "# in fixtures subdir\n")
	}
}

// TestNewResolverFactory_FixturesSubdir_DocDirWinsOverFixturesSubdir verifies
// that when the same ref exists in both docDir and docDir/fixtures/, the
// docDir copy wins (docDir has the highest precedence).
func TestNewResolverFactory_FixturesSubdir_DocDirWinsOverFixturesSubdir(t *testing.T) {
	docDir := newDir(t, map[string]string{
		"common.md":          "# in docDir\n",
		"fixtures/common.md": "# in fixtures subdir\n",
	})
	sharedRoot := newDir(t, nil)

	factory, err := fixtures.NewResolverFactory(sharedRoot)
	if err != nil {
		t.Fatalf("NewResolverFactory error = %v, want nil", err)
	}

	r, err := factory(docDir)
	if err != nil {
		t.Fatalf("factory(%q) error = %v, want nil", docDir, err)
	}

	got, err := r.Resolve("common.md")
	if err != nil {
		t.Fatalf("Resolve(common.md) error = %v, want nil", err)
	}
	if string(got) != "# in docDir\n" {
		t.Errorf("Resolve returned %q, want docDir copy %q "+
			"(docDir must win over docDir/fixtures/)", string(got), "# in docDir\n")
	}
}

// TestNewResolverFactory_FixturesSubdir_WinsOverSharedRoot verifies that when
// a ref exists in docDir/fixtures/ but not in docDir itself, the
// docDir/fixtures/ copy is returned before falling through to sharedRoot.
// This confirms the middle-root precedence: docDir/fixtures/ beats sharedRoot.
func TestNewResolverFactory_FixturesSubdir_WinsOverSharedRoot(t *testing.T) {
	docDir := newDir(t, map[string]string{
		// "in-subdir.md" is NOT at docDir root -- only inside fixtures/.
		"fixtures/in-subdir.md": "# in fixtures subdir\n",
	})
	sharedRoot := newDir(t, map[string]string{
		"in-subdir.md": "# in shared root\n",
	})

	factory, err := fixtures.NewResolverFactory(sharedRoot)
	if err != nil {
		t.Fatalf("NewResolverFactory error = %v, want nil", err)
	}

	r, err := factory(docDir)
	if err != nil {
		t.Fatalf("factory(%q) error = %v, want nil", docDir, err)
	}

	got, err := r.Resolve("in-subdir.md")
	if err != nil {
		t.Fatalf("Resolve(in-subdir.md) error = %v, want nil "+
			"(requires I1.1: docDir/fixtures/ must beat sharedRoot)", err)
	}
	if string(got) != "# in fixtures subdir\n" {
		t.Errorf("Resolve returned %q, want fixtures-subdir copy %q "+
			"(docDir/fixtures/ must have higher precedence than sharedRoot)",
			string(got), "# in fixtures subdir\n")
	}
}

// TestNewResolverFactory_NoFixturesSubdir_UnaffectedBehavior verifies that
// when docDir has no fixtures/ subdirectory the resolver searches exactly two
// roots (docDir and sharedRoot) and a failed resolution names exactly two roots
// in SearchedRoots. No phantom docDir/fixtures/ entry must appear in error
// messages for suites that have no such directory.
func TestNewResolverFactory_NoFixturesSubdir_UnaffectedBehavior(t *testing.T) {
	docDir := newDir(t, nil)     // no fixtures/ subdirectory
	sharedRoot := newDir(t, nil) // no fixture files either

	factory, err := fixtures.NewResolverFactory(sharedRoot)
	if err != nil {
		t.Fatalf("NewResolverFactory error = %v, want nil", err)
	}

	r, err := factory(docDir)
	if err != nil {
		t.Fatalf("factory(%q) error = %v, want nil", docDir, err)
	}

	// A ref absent from all roots must produce a ResolutionError that names
	// exactly two roots (docDir + sharedRoot). After I1.1, the absence of a
	// fixtures/ subdirectory must not cause a spurious third root entry.
	_, err = r.Resolve("absent.md")
	if err == nil {
		t.Fatal("Resolve(absent.md) error = nil, want non-nil")
	}

	var resErr *fixtures.ResolutionError
	if !errors.As(err, &resErr) {
		t.Fatalf("errors.As(err, *ResolutionError) = false; err = %v", err)
	}
	if len(resErr.SearchedRoots) != 2 {
		t.Errorf("SearchedRoots len = %d, want exactly 2 "+
			"(docDir + sharedRoot; no phantom fixtures/ entry when subdirectory is absent); "+
			"roots = %v", len(resErr.SearchedRoots), resErr.SearchedRoots)
	}
}
