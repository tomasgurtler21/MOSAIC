package preflight_test

// Automated regression test for fixture $ref resolution against a nested
// fixtures/ subdirectory (FR-6). This test runs preflight validation against
// the permanent synthetic suite at tests/ref-subdirectory/ and asserts zero
// unresolvable-ref diagnostics. A failure here means the fixtures/ subdirectory
// search root has regressed and bare-filename refs no longer resolve correctly.

import (
	"path/filepath"
	"runtime"
	"testing"

	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/preflight"
)

// refSubdirectorySuitePath returns the path to the permanent synthetic suite's
// suite definition file, resolved relative to this test file's own location so
// the test works regardless of the working directory go test is invoked from.
func refSubdirectorySuitePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to resolve this test file's path")
	}
	// This file lives at internal/preflight/; the suite is at tests/ref-subdirectory/.
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "ref-subdirectory",
		"ref-subdirectory.suite.yaml")
}

// TestRegression_RefSubdirectory_NoUnresolvableRefDiagnostics runs preflight
// validation against the permanent tests/ref-subdirectory/ synthetic suite and
// asserts zero unresolvable-ref diagnostics. The suite uses bare-filename refs
// in seed_files that exist only inside the fixtures/ subdirectory of the suite
// root. Resolution requires NewResolverFactory to include docDir/fixtures/ as
// a search root between docDir and sharedRoot. If that behaviour regresses, this
// test fails with unresolvable-ref diagnostics before any manual discovery.
func TestRegression_RefSubdirectory_NoUnresolvableRefDiagnostics(t *testing.T) {
	suitePath := refSubdirectorySuitePath(t)

	factory, err := fixtures.NewResolverFactory("")
	if err != nil {
		t.Fatalf("fixtures.NewResolverFactory error = %v, want nil", err)
	}

	_, report := preflight.Validate(preflight.Input{
		SuitePath:       suitePath,
		FixtureRoot:     "",
		ResolverFactory: factory,
		HarnessID:       "claude-code",
	})

	for _, d := range report.Diagnostics {
		if d.Code == "unresolvable-ref" {
			t.Errorf("preflight reported unresolvable-ref for suite at %q: %s",
				suitePath, d.Message)
		}
	}
}
