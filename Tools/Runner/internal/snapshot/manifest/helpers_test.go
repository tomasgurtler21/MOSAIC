package manifest_test

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

// mustMkdir creates a directory, failing the test on error.
func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
}
