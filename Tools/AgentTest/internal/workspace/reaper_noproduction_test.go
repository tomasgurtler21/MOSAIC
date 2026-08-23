package workspace_test

// Regression test asserting that no production call path reaches the workspace
// reaper.
//
// workspace.Manager.Reap judges a sandbox live by the lock holder's process id.
// The holder is a short-lived interceptor process that is normally dead between
// interceptions; wired into a concurrent run unmodified it would classify live
// sandboxes as dead and delete them. Until the liveness check is corrected,
// no production code may call Reap — and this test is what makes that a hard
// requirement rather than a comment.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReap_IsNotCalledFromProductionCode scans every non-test Go source file
// in the module — excluding the workspace package itself, where Reap is defined
// — and asserts that none of them contains a call to .Reap(. A call found here
// means a production path would invoke the reaper, which classifies live
// sandboxes as dead and deletes them.
//
// This is a source-text scan, not a semantic analysis. That is intentional:
// the reaper is never meant to be wired at all, so any textual appearance of
// .Reap( in production code is a violation — even an indirect or interface-
// mediated call is wrong here because the same incorrect liveness check applies
// regardless of how the code reaches the method.
func TestReap_IsNotCalledFromProductionCode(t *testing.T) {
	moduleRoot := findModuleRoot(t)

	// workspacePkgRel is the package directory relative to the module root,
	// using forward slashes for consistent comparison.
	const workspacePkgRel = "internal/workspace"

	var violations []string
	err := filepath.Walk(moduleRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			// Skip directories that never contain module source.
			if base == "vendor" || base == "testdata" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip test files: they may legitimately call Reap (e.g., to test the
		// reaper itself in isolation) without wiring it into production.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		// Skip the workspace package itself, where Reap is defined and
		// implemented.
		rel, relErr := filepath.Rel(moduleRoot, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)
		if strings.HasPrefix(relSlash, workspacePkgRel+"/") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(data, []byte(".Reap(")) {
			violations = append(violations, relSlash)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("error walking module source tree: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf(
			"found calls to .Reap( in production source files — the workspace reaper "+
				"must remain unwired until its holder-liveness check is corrected "+
				"(it currently judges liveness by interceptor process id, which is "+
				"normally dead between interceptions and would delete live sandboxes):\n  %s",
			strings.Join(violations, "\n  "),
		)
	}
}

// findModuleRoot locates the module root by walking up from the test's working
// directory (which Go sets to the package directory) until go.mod is found.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found while walking up from working directory — is the test run from inside the module?")
		}
		dir = parent
	}
}
