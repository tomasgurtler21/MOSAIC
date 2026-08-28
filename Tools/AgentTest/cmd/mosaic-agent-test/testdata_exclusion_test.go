package main

// Tests for testdata/ directory exclusion in suite discovery.
//
// Go's convention reserves the name "testdata" as a fixture directory that
// belongs to the test runner, not to production tooling. Suite files placed
// under any directory named "testdata" in the walk tree are fixture inputs for
// tests, not authored suites meant to appear in the TUI or CLI.
//
// These tests will fail (TDD RED) until the implementation skips directories
// named "testdata" in discoverSuites, mirroring the existing dot-directory
// exclusion.

import (
	"path/filepath"
	"testing"
)

// TestDiscoverSuites_TestdataDirectoryIsExcluded verifies that suite files
// nested directly under a "testdata" directory are not returned. The testdata/
// convention is Go's reserved fixture space; suites stored there are test
// fixtures, not production suites.
func TestDiscoverSuites_TestdataDirectoryIsExcluded(t *testing.T) {
	dir := t.TempDir()

	// Create a suite inside testdata/ -- must be excluded.
	tdPath := filepath.Join(dir, "testdata", "fixture.suite.yaml")
	discoveryWriteFile(t, tdPath, "")

	got := discoverSuites(dir)

	if len(got) != 0 {
		t.Errorf("discoverSuites(%q) returned %d result(s), want 0 (testdata/ must be excluded): %v", dir, len(got), got)
	}
}

// TestDiscoverSuites_TestdataE2ESubdirectoryIsExcluded verifies that suites
// nested inside testdata/e2e/ (a common layout for end-to-end fixtures) are
// excluded. Exclusion must apply to the entire subtree under testdata/, not
// only to direct children.
func TestDiscoverSuites_TestdataE2ESubdirectoryIsExcluded(t *testing.T) {
	dir := t.TempDir()

	// Mirrors the real layout: Tools/AgentTest/testdata/e2e/*.suite.yaml.
	e2eSuite := filepath.Join(dir, "testdata", "e2e", "e2e-fixture.suite.yaml")
	discoveryWriteFile(t, e2eSuite, "")

	got := discoverSuites(dir)

	if len(got) != 0 {
		t.Errorf("discoverSuites(%q) returned %d result(s), want 0 (testdata/e2e/ must be excluded): %v", dir, len(got), got)
	}
}

// TestDiscoverSuites_RealSuiteAlongsideTestdataIsIncluded verifies the mixed
// case: a real authored suite and a testdata/ fixture suite coexist in the same
// root. Only the real suite must appear in the result; testdata/ must be skipped
// while the walk continues into peer directories.
func TestDiscoverSuites_RealSuiteAlongsideTestdataIsIncluded(t *testing.T) {
	dir := t.TempDir()

	// Real suite -- must be returned.
	realSuite := filepath.Join(dir, "my-agent.suite.yaml")
	discoveryWriteFile(t, realSuite, "")

	// Fixture suite inside testdata/ -- must be excluded.
	fixtureSuite := filepath.Join(dir, "testdata", "e2e", "fixture.suite.yaml")
	discoveryWriteFile(t, fixtureSuite, "")

	got := discoverSuites(dir)

	if len(got) != 1 {
		t.Fatalf("discoverSuites(%q) returned %d result(s), want 1 (only the real suite): %v", dir, len(got), got)
	}
	if got[0] != realSuite {
		t.Errorf("discoverSuites(%q) = %v, want [%q]", dir, got, realSuite)
	}
}

// TestDiscoverSuites_MultipleRealSuitesAlongsideTestdata verifies that
// excluding testdata/ does not interfere with discovering multiple real suites
// in different peer directories.
func TestDiscoverSuites_MultipleRealSuitesAlongsideTestdata(t *testing.T) {
	dir := t.TempDir()

	// Two real suites in different subdirectories.
	suiteA := filepath.Join(dir, "alpha", "alpha.suite.yaml")
	suiteB := filepath.Join(dir, "beta", "beta.suite.yaml")
	discoveryWriteFile(t, suiteA, "")
	discoveryWriteFile(t, suiteB, "")

	// Multiple fixture suites inside testdata/ -- all must be excluded.
	discoveryWriteFile(t, filepath.Join(dir, "testdata", "fixture-a.suite.yaml"), "")
	discoveryWriteFile(t, filepath.Join(dir, "testdata", "e2e", "fixture-b.suite.yaml"), "")
	discoveryWriteFile(t, filepath.Join(dir, "testdata", "e2e", "nested", "fixture-c.suite.yaml"), "")

	got := discoverSuites(dir)

	if len(got) != 2 {
		t.Fatalf("discoverSuites(%q) returned %d result(s), want 2 (both real suites only): %v", dir, len(got), got)
	}
	for _, path := range got {
		if containsTestdataSegment(path) {
			t.Errorf("discoverSuites(%q) result includes a testdata/ path that must be excluded: %q", dir, path)
		}
	}
}

// TestDiscoverSuites_RefSubdirectoryUnderTestsIsIncluded verifies that a suite
// stored under a "tests/" subdirectory (not "testdata/") is discoverable.
// This mirrors the permanent synthetic suite at tests/ref-subdirectory/ which
// must remain visible after the testdata exclusion is added.
func TestDiscoverSuites_RefSubdirectoryUnderTestsIsIncluded(t *testing.T) {
	dir := t.TempDir()

	// Mirror the permanent tests/ref-subdirectory/ layout.
	refSuite := filepath.Join(dir, "tests", "ref-subdirectory", "ref-subdirectory.suite.yaml")
	discoveryWriteFile(t, refSuite, "")

	// Add a testdata/ fixture that must NOT appear.
	discoveryWriteFile(t, filepath.Join(dir, "testdata", "e2e", "ignored.suite.yaml"), "")

	got := discoverSuites(dir)

	if len(got) != 1 {
		t.Fatalf("discoverSuites(%q) returned %d result(s), want 1 (ref-subdirectory only): %v", dir, len(got), got)
	}
	if got[0] != refSuite {
		t.Errorf("discoverSuites(%q) = %v, want [%q]", dir, got, refSuite)
	}
}

// TestDiscoverSuites_TestdataExclusionIsExactNameMatch verifies that only a
// directory whose name is exactly "testdata" is skipped. A directory named
// "testdata-extra" or "my-testdata" must not be confused with the reserved
// name and must be descended into normally.
func TestDiscoverSuites_TestdataExclusionIsExactNameMatch(t *testing.T) {
	dir := t.TempDir()

	// These directories have "testdata" as a substring but not as the full name
	// -- their suites must still be discovered.
	discoveryWriteFile(t, filepath.Join(dir, "testdata-extra", "extra.suite.yaml"), "")
	discoveryWriteFile(t, filepath.Join(dir, "my-testdata", "prefix.suite.yaml"), "")
	discoveryWriteFile(t, filepath.Join(dir, "testdatafiles", "suffix.suite.yaml"), "")

	// The directory named exactly "testdata" must be excluded.
	discoveryWriteFile(t, filepath.Join(dir, "testdata", "excluded.suite.yaml"), "")

	got := discoverSuites(dir)

	// Exactly the three suites in directories that are NOT "testdata" must appear.
	if len(got) != 3 {
		t.Fatalf("discoverSuites(%q) returned %d result(s), want 3 (only exact 'testdata' name excluded): %v", dir, len(got), got)
	}
	for _, path := range got {
		if containsTestdataSegment(path) {
			t.Errorf("discoverSuites(%q) result includes a testdata/ path: %q", dir, path)
		}
	}
}

// TestDiscoverSuites_NestedTestdataIsExcluded verifies that a "testdata"
// directory nested inside a non-testdata subdirectory is also excluded. The
// skip must apply at every level of the tree, not only at the root's immediate
// children.
func TestDiscoverSuites_NestedTestdataIsExcluded(t *testing.T) {
	dir := t.TempDir()

	// Real suite in a regular subtree.
	realSuite := filepath.Join(dir, "suites", "real.suite.yaml")
	discoveryWriteFile(t, realSuite, "")

	// testdata/ nested two levels deep -- must still be excluded.
	nestedFixture := filepath.Join(dir, "suites", "testdata", "deep-fixture.suite.yaml")
	discoveryWriteFile(t, nestedFixture, "")

	got := discoverSuites(dir)

	if len(got) != 1 {
		t.Fatalf("discoverSuites(%q) returned %d result(s), want 1 (nested testdata/ must be excluded): %v", dir, len(got), got)
	}
	if got[0] != realSuite {
		t.Errorf("discoverSuites(%q) = %v, want [%q]", dir, got, realSuite)
	}
}

// containsTestdataSegment reports whether path contains a path segment named
// exactly "testdata". It is a test helper for asserting that no excluded path
// leaked into the discovery result.
func containsTestdataSegment(path string) bool {
	for _, seg := range splitPathSegments(path) {
		if seg == "testdata" {
			return true
		}
	}
	return false
}

// splitPathSegments splits a file path into its directory-name components,
// returning only non-empty segments. It is used exclusively by the test helper
// containsTestdataSegment.
func splitPathSegments(path string) []string {
	path = filepath.ToSlash(path)
	var segments []string
	for _, seg := range filepath.SplitList(path) {
		if seg != "" {
			segments = append(segments, seg)
		}
	}
	// filepath.SplitList splits PATH env var lists, not path components.
	// Use a manual split on "/" instead.
	segments = nil
	for _, seg := range splitOnSlash(path) {
		if seg != "" {
			segments = append(segments, seg)
		}
	}
	return segments
}

// splitOnSlash splits s on every "/" character, returning all parts including
// empty ones. It is a minimal helper used only by splitPathSegments.
func splitOnSlash(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}
