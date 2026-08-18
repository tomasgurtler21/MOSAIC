package main

// Tests for suite discovery: the discoverSuites function that lists authored
// suite files offered on the interactive frontend's suite-select screen.
//
// Every test here operates on a temporary directory created by t.TempDir and
// never reads any file outside this tool's own tree. This is the design
// requirement: live repository suite files change in the course of normal
// project evolution, and a test that reads them breaks on every such change
// even though nothing in the discovery logic is wrong.
//
// These tests drive the new discoverSuites(root string) signature. Until the
// implementation is updated to accept a root parameter, these tests will fail
// to compile — that is the expected TDD RED state.

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestDiscoverSuites_NestedSuitesAreFound verifies that *.suite.yaml files
// stored in subdirectories beneath the root are included in the result. A
// flat glob against the root alone would miss them; the recursive walk
// required by the design must descend into every non-hidden child directory.
func TestDiscoverSuites_NestedSuitesAreFound(t *testing.T) {
	dir := t.TempDir()

	sub := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	discoveryWriteFile(t, filepath.Join(dir, "b.suite.yaml"), "")
	discoveryWriteFile(t, filepath.Join(sub, "a.suite.yaml"), "")

	got := discoverSuites(dir)

	if len(got) != 2 {
		t.Fatalf("discoverSuites(%q) returned %d result(s), want 2: %v", dir, len(got), got)
	}
}

// TestDiscoverSuites_EmptyRootYieldsEmptyList verifies that a directory
// containing no *.suite.yaml files returns an empty (non-nil or nil) list
// and no error. An empty result is a legitimate "no suites here" state that
// the interactive frontend already renders; it must not be treated as a
// startup failure.
func TestDiscoverSuites_EmptyRootYieldsEmptyList(t *testing.T) {
	dir := t.TempDir()

	got := discoverSuites(dir)

	if len(got) != 0 {
		t.Errorf("discoverSuites(%q) on an empty directory = %v, want empty list", dir, got)
	}
}

// TestDiscoverSuites_MissingRootYieldsEmptyList verifies that a root path
// that does not exist on disk yields an empty list and no error. A
// first-time user who has not yet authored any suites sees the empty-suite
// screen rather than a crash.
func TestDiscoverSuites_MissingRootYieldsEmptyList(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	got := discoverSuites(dir)

	if len(got) != 0 {
		t.Errorf("discoverSuites(%q) on a missing root = %v, want empty list", dir, got)
	}
}

// TestDiscoverSuites_DirectSuitePathIsReturnedAsIs verifies that when root
// names an existing file, it is returned as a one-element list without any
// name check. A user who points the tool at a specific suite file gets
// exactly that suite, whatever its filename — the *.suite.yaml pattern is
// the directory-search heuristic, not a constraint on what counts as a suite.
func TestDiscoverSuites_DirectSuitePathIsReturnedAsIs(t *testing.T) {
	dir := t.TempDir()
	// Deliberately not *.suite.yaml to confirm the name is not re-checked.
	suitePath := filepath.Join(dir, "my-suite.yaml")
	discoveryWriteFile(t, suitePath, "")

	got := discoverSuites(suitePath)

	if len(got) != 1 {
		t.Fatalf("discoverSuites(%q) returned %d result(s), want 1: %v", suitePath, len(got), got)
	}
	if got[0] != suitePath {
		t.Errorf("discoverSuites(%q) = %v, want [%q]", suitePath, got, suitePath)
	}
}

// TestDiscoverSuites_DotDirectoriesAreSkipped verifies that directories
// whose name begins with "." are not descended into, so a repository root
// does not drag .git, .github, and similar tooling directories into the scan.
func TestDiscoverSuites_DotDirectoriesAreSkipped(t *testing.T) {
	dir := t.TempDir()

	hidden := filepath.Join(dir, ".hidden")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	discoveryWriteFile(t, filepath.Join(hidden, "hidden.suite.yaml"), "")
	discoveryWriteFile(t, filepath.Join(dir, "visible.suite.yaml"), "")

	got := discoverSuites(dir)

	// Only the visible suite must appear; the .hidden directory must not
	// be descended into.
	if len(got) != 1 {
		t.Fatalf("discoverSuites(%q) returned %d result(s), want 1 (hidden directories must be skipped): %v", dir, len(got), got)
	}
	want := filepath.Join(dir, "visible.suite.yaml")
	if got[0] != want {
		t.Errorf("discoverSuites(%q) = %v, want [%q]", dir, got, want)
	}
}

// TestDiscoverSuites_ResultsAreSorted verifies the returned list is in
// stable lexicographic order across invocations, so the suite-select list
// presented to the user is consistent regardless of filesystem traversal
// order.
func TestDiscoverSuites_ResultsAreSorted(t *testing.T) {
	dir := t.TempDir()

	// Write in non-alphabetical order to expose an unsorted implementation.
	for _, name := range []string{"z.suite.yaml", "a.suite.yaml", "m.suite.yaml"} {
		discoveryWriteFile(t, filepath.Join(dir, name), "")
	}

	got := discoverSuites(dir)

	if len(got) != 3 {
		t.Fatalf("discoverSuites(%q) returned %d result(s), want 3: %v", dir, len(got), got)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("discoverSuites(%q) results are not sorted: %v", dir, got)
	}
}

// TestDiscoverSuites_NonSuiteFilesAreIgnored verifies that files in the root
// tree that do not match the *.suite.yaml pattern are not included in the
// result. Only files with the suite naming convention are offered.
func TestDiscoverSuites_NonSuiteFilesAreIgnored(t *testing.T) {
	dir := t.TempDir()

	discoveryWriteFile(t, filepath.Join(dir, "real.suite.yaml"), "")
	discoveryWriteFile(t, filepath.Join(dir, "config.yaml"), "")
	discoveryWriteFile(t, filepath.Join(dir, "README.md"), "")
	discoveryWriteFile(t, filepath.Join(dir, "suite.yaml"), "") // matches *.yaml but not *.suite.yaml

	got := discoverSuites(dir)

	if len(got) != 1 {
		t.Fatalf("discoverSuites(%q) returned %d result(s), want 1 (only *.suite.yaml files): %v", dir, len(got), got)
	}
	want := filepath.Join(dir, "real.suite.yaml")
	if got[0] != want {
		t.Errorf("discoverSuites(%q) = %v, want [%q]", dir, got, want)
	}
}

// discoveryWriteFile is a test helper that creates a file at path with the
// given content, creating all parent directories as needed. It is named with
// a package-specific prefix to avoid colliding with any helper of the same
// name that another test file in this package might define.
func discoveryWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating parent dirs for %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %q: %v", path, err)
	}
}
