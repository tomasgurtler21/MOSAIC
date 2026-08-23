package contract

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// CheckScopeAccounting asserts that every configuration scope the adapter
// enumerates outside the sandbox is accounted for by exactly one finding, and
// that each such finding either reports the scope relocated into the run's
// sandbox (Neutralized) or carries a non-empty Detail naming why it is not.
//
// A scope enumerated and not inspected, inspected twice, or reported with
// neither neutralization nor an explanation, fails the check. This is what
// stops "we did not look" from reading like "there was nothing there", which
// is the failure mode a future adapter is most likely to reintroduce.
//
// The existing neutralization obligation still applies on top of it:
// Neutralized && !PreservesSubjectFunction is a failure, because isolating a
// scope at the cost of the subject's ability to run is not a pass.
func CheckScopeAccounting(t *testing.T, cfg Config) error {
	t.Helper()

	adapter, _, prov := provisionForTest(t, cfg)

	// ConfigScopes declares what should be inspected.
	scopes := adapter.ConfigScopes()

	// ScopeFindings from Provisioning are sandbox-derived: Provision must
	// populate them with sandbox-resolved paths, not the abstract paths that
	// ConfigScopes() returns (which take no sandbox).
	findings := prov.ScopeFindings

	// Index findings by scope name to detect missing and duplicate entries.
	byName := make(map[string][]domain.ScopeFinding)
	for _, f := range findings {
		byName[f.Scope.Name] = append(byName[f.Scope.Name], f)
	}

	var errs []string
	for _, s := range scopes {
		if s.InSandbox {
			continue // in-sandbox scope needs no external accounting
		}

		got, ok := byName[s.Name]
		if !ok {
			errs = append(errs, fmt.Sprintf(
				"scope %q is enumerated by ConfigScopes but has no finding in Provisioning.ScopeFindings; "+
					"every non-sandbox scope must be accounted for",
				s.Name,
			))
			continue
		}
		if len(got) > 1 {
			errs = append(errs, fmt.Sprintf(
				"scope %q appears in Provisioning.ScopeFindings %d times, want exactly once",
				s.Name, len(got),
			))
			continue
		}

		f := got[0]
		if !f.Neutralized && f.Detail == "" {
			errs = append(errs, fmt.Sprintf(
				"scope %q is neither neutralized (Neutralized=false) nor explained (Detail is empty); "+
					"every non-sandbox scope finding must either relocate the scope into the sandbox "+
					"or carry a non-empty Detail naming why it was not",
				s.Name,
			))
		}
		if f.Neutralized && !f.PreservesSubjectFunction {
			errs = append(errs, fmt.Sprintf(
				"scope %q is reported Neutralized=true but PreservesSubjectFunction=false (FunctionDetail: %q); "+
					"isolating a scope at the cost of the subject's ability to run must fail this check",
				s.Name, f.FunctionDetail,
			))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("CheckScopeAccounting: %s", strings.Join(errs, "; "))
	}
	return nil
}

// CheckScopeIsolationAcrossRuns provisions two adapters against two distinct
// sandboxes at the same time and asserts that no scope path either
// provisioning reports equals, contains, or is contained by a scope path the
// other reports.
//
// This is the shape parallel execution actually creates, and it is the shape
// no existing evidence covered: relocation was previously confirmed for one
// process only. Paths are compared after cleaning, so a trailing separator or
// a redundant path element cannot make two identical locations look distinct.
//
// It reads Provisioning.ScopeFindings rather than ConfigScopes(), because
// only the former is sandbox-derived: the port method takes no sandbox, so
// the enumeration it returns carries no real root. An adapter must therefore
// populate Provisioning.ScopeFindings with sandbox-resolved paths — that is
// an obligation of Provision, asserted here.
func CheckScopeIsolationAcrossRuns(t *testing.T, cfg Config) error {
	t.Helper()

	dir1 := t.TempDir()
	dir2 := t.TempDir()

	adapter1 := cfg.New(t, dir1)
	adapter2 := cfg.New(t, dir2)

	sb1 := newSandbox(t, dir1)
	sb2 := newSandbox(t, dir2)

	prov1, err := adapter1.Provision(testContext(), buildProvisionRequest(sb1, cfg.Subject))
	if err != nil {
		return fmt.Errorf("CheckScopeIsolationAcrossRuns: Provision (run 1): %v", err)
	}
	t.Cleanup(func() { _ = adapter1.Deprovision(testContext(), prov1) })

	prov2, err := adapter2.Provision(testContext(), buildProvisionRequest(sb2, cfg.Subject))
	if err != nil {
		return fmt.Errorf("CheckScopeIsolationAcrossRuns: Provision (run 2): %v", err)
	}
	t.Cleanup(func() { _ = adapter2.Deprovision(testContext(), prov2) })

	// Collect the non-sandbox scope paths from each provisioning.
	paths1 := scopePathsFromFindings(prov1.ScopeFindings)
	paths2 := scopePathsFromFindings(prov2.ScopeFindings)

	if len(paths1) == 0 && len(paths2) == 0 {
		// No non-sandbox scopes declared — nothing to isolate, nothing to check.
		return nil
	}

	var conflicts []string
	for _, p1 := range paths1 {
		for _, p2 := range paths2 {
			if pathsOverlap(p1, p2) {
				conflicts = append(conflicts, fmt.Sprintf(
					"run 1 scope path %q overlaps with run 2 scope path %q; "+
						"two simultaneously provisioned runs must receive non-overlapping scope locations",
					p1, p2,
				))
			}
		}
	}

	if len(conflicts) > 0 {
		return fmt.Errorf("CheckScopeIsolationAcrossRuns: %s", strings.Join(conflicts, "; "))
	}
	return nil
}

// scopePathsFromFindings returns the cleaned, non-empty paths from the
// non-sandbox scope findings in a provisioning.
func scopePathsFromFindings(findings []domain.ScopeFinding) []string {
	var paths []string
	for _, f := range findings {
		if f.Scope.InSandbox {
			continue
		}
		if f.Scope.Path == "" {
			continue
		}
		paths = append(paths, filepath.Clean(f.Scope.Path))
	}
	return paths
}

// pathsOverlap reports whether two cleaned absolute paths are equal, or one
// is a prefix of the other (i.e., contains or is contained by it). A
// trailing separator is stripped before comparison so "a/b" and "a/b/" are
// not treated as distinct.
func pathsOverlap(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(b, a+sep) || strings.HasPrefix(a, b+sep)
}

// testScopeAccounting is the internal wrapper that drives CheckScopeAccounting
// as a t.Run subtest.
func testScopeAccounting(t *testing.T, cfg Config) {
	t.Helper()
	if err := CheckScopeAccounting(t, cfg); err != nil {
		t.Error(err)
	}
}

// testScopeIsolationAcrossRuns is the internal wrapper that drives
// CheckScopeIsolationAcrossRuns as a t.Run subtest.
func testScopeIsolationAcrossRuns(t *testing.T, cfg Config) {
	t.Helper()
	if err := CheckScopeIsolationAcrossRuns(t, cfg); err != nil {
		t.Error(err)
	}
}

// testRealScopeUntouched asserts that the adapter's Provision does not write
// into the real user-scope location. The check works by directing
// XDG_CONFIG_HOME at a fresh temp directory and verifying that directory's
// contents are unchanged after Provision returns.
//
// For adapters that relocate the scope into the sandbox — the correct outcome
// — the real location is never touched because the spawn plan points the
// spawned process away from it, and Provision itself must not write there.
// For adapters that have not yet implemented relocation, this test confirms no
// writes occur during provisioning itself.
func testRealScopeUntouched(t *testing.T, cfg Config) {
	t.Helper()

	// Direct XDG_CONFIG_HOME at a fresh temp directory so we know where the
	// "real" user config would land and can observe it precisely.
	realHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", realHome)

	// Record the file tree before provisioning.
	before := collectPaths(t, realHome)

	dir := t.TempDir()
	adapter := cfg.New(t, dir)
	sb := newSandbox(t, dir)

	prov, err := adapter.Provision(testContext(), buildProvisionRequest(sb, cfg.Subject))
	if err != nil {
		t.Fatalf("testRealScopeUntouched: Provision: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Deprovision(testContext(), prov) })

	// Record the file tree after provisioning.
	after := collectPaths(t, realHome)

	for path := range after {
		if _, existed := before[path]; !existed {
			t.Errorf(
				"testRealScopeUntouched: Provision created %q inside the real user-scope directory %q; "+
					"Provision must not write outside the sandbox",
				path, realHome,
			)
		}
	}
}

// collectPaths returns a set of all file and directory paths found under root,
// walking the tree recursively. An absent root is treated as an empty set.
func collectPaths(t *testing.T, root string) map[string]struct{} {
	t.Helper()
	result := make(map[string]struct{})
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than aborting
		}
		if path != root {
			result[path] = struct{}{}
		}
		return nil
	})
	return result
}
