package setup

// Shared helpers for the setup screen tests.

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestTempFile creates a temporary directory and a file named "orch.md" inside it.
// It returns the absolute path to the file. The directory is cleaned up automatically
// by the test runner via t.Cleanup.
func newTestTempFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "orch.md")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("setup: could not create temp file: %v", err)
	}
	f.Close()
	return path
}
