package main

// call_boundary_test.go verifies that the read-boundary guard fires for raw
// docformat entry-point calls in the application layer and accepts only the
// two permitted callers.
//
// Background: the application layer holds two categories of raw-bytes docformat
// consumption that must be structurally separated:
//
//  1. Deployed-file reads, which must go through the decode funnel in
//     deployed_read.go.
//  2. Generic-source reads (catalog sources, injection content, bundle-conformance
//     inputs, render inputs), which go through the named generic-source operation
//     in parse_generic_source.go.
//
// Every other application-layer file is a guarded caller that must NOT call the
// raw-bytes entry points (docformat.Parse or docformat.SplitFrontmatter) directly.
// The guard enforces this at the call level so that a bypass is a build failure
// rather than a code-review finding.
//
// Test strategy: each test passes a small fixture directory (under testdata/) to
// runChecks as its module root. The fixture contains exactly one scenario. The test
// asserts rejection or acceptance outcomes only, not the rule table's contents.
//
// These tests are written before the call-level inspection exists in the guard and
// are expected to fail (TDD RED) until the call-level inspection is implemented.

import (
	"path/filepath"
	"testing"
)

// TestCallBoundary_RawParse_InApp_IsRejected asserts that a file in the application
// layer that calls docformat.Parse directly is rejected by the read-boundary guard.
// The guard must fire for the Parse entry point regardless of which other entry
// points it also bans.
//
// This test is written before the call-level inspection is implemented and fails
// today because the guard performs imports-only checking. It will pass once the
// call-level AST inspection is added.
func TestCallBoundary_RawParse_InApp_IsRejected(t *testing.T) {
	assertGuardRejectsFixture(t,
		filepath.Join("testdata", "call_boundary", "raw_parse_in_app"),
		"raw docformat.Parse call in application layer should be rejected by the read-boundary guard",
	)
}

// TestCallBoundary_RawSplitFrontmatter_InApp_IsRejected asserts that a file in the
// application layer that calls docformat.SplitFrontmatter directly is rejected by
// the read-boundary guard. The banned set is both raw-bytes entry points; banning
// only Parse would leave a hole the size of the bug this guard exists to prevent.
//
// This test is written before the call-level inspection is implemented and fails
// today because the guard performs imports-only checking.
func TestCallBoundary_RawSplitFrontmatter_InApp_IsRejected(t *testing.T) {
	assertGuardRejectsFixture(t,
		filepath.Join("testdata", "call_boundary", "raw_splitfrontmatter_in_app"),
		"raw docformat.SplitFrontmatter call in application layer should be rejected by the read-boundary guard",
	)
}

// TestCallBoundary_FunnelFileCaller_IsAccepted asserts that the decode funnel
// (deployed_read.go) is accepted by the read-boundary guard even though it calls
// both banned entry points. The funnel is one of the two explicitly permitted
// callers; the guard must not degrade into rejecting all docformat entry-point
// calls.
//
// This test is expected to pass both before and after the call-level inspection is
// implemented, because the acceptance case is the natural state.
func TestCallBoundary_FunnelFileCaller_IsAccepted(t *testing.T) {
	assertGuardAcceptsFixture(t,
		filepath.Join("testdata", "call_boundary", "funnel_file_caller"),
		"decode funnel file (deployed_read.go) calling raw docformat entry points should be accepted",
	)
}

// TestCallBoundary_GenericSourceCaller_IsAccepted asserts that the named
// generic-source operation (parse_generic_source.go) is accepted by the
// read-boundary guard even though it calls docformat.Parse. The generic-source
// operation is the second of the two explicitly permitted callers.
//
// This fixture names a function that does not yet exist in the real production tree.
// The guard reads fixtures as text, so the named-but-absent function is valid here.
//
// This test is expected to pass both before and after the call-level inspection is
// implemented.
func TestCallBoundary_GenericSourceCaller_IsAccepted(t *testing.T) {
	assertGuardAcceptsFixture(t,
		filepath.Join("testdata", "call_boundary", "generic_source_caller"),
		"named generic-source operation (parse_generic_source.go) calling docformat.Parse should be accepted",
	)
}

// assertGuardRejectsFixture calls runChecks with fixtureRoot as the module root and
// fails the test if no violations are returned. A failure means the guard did not
// fire for the scenario the fixture represents.
func assertGuardRejectsFixture(t *testing.T, fixtureRoot, scenario string) {
	t.Helper()
	violations, errs := runChecks(fixtureRoot)
	if len(errs) > 0 {
		t.Fatalf("unexpected tool errors checking %q: %v", fixtureRoot, errs)
	}
	if len(violations) == 0 {
		t.Fatalf("expected at least one violation for: %s\n  (fixture: %s)\n  got no violations",
			scenario, fixtureRoot)
	}
}

// assertGuardAcceptsFixture calls runChecks with fixtureRoot as the module root and
// fails the test if any violations are returned. A failure means the guard is
// over-matching and rejecting a legitimately permitted caller.
func assertGuardAcceptsFixture(t *testing.T, fixtureRoot, scenario string) {
	t.Helper()
	violations, errs := runChecks(fixtureRoot)
	if len(errs) > 0 {
		t.Fatalf("unexpected tool errors checking %q: %v", fixtureRoot, errs)
	}
	if len(violations) > 0 {
		t.Fatalf("expected no violations for: %s\n  (fixture: %s)\n  got %d violation(s):\n%v",
			scenario, fixtureRoot, len(violations), violations)
	}
}
