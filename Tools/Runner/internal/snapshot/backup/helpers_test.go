package backup_test

import (
	"os"
	"testing"
)

// writeFile creates a file at path with the given content.
func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writeFile %q: %v", path, err)
	}
}

// readFile reads and returns the content of path, failing the test on error.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readFile %q: %v", path, err)
	}
	return b
}
