package opencode_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// newSandbox builds a domain.Sandbox rooted under dir, creating the subject
// and control directories the adapter is allowed to write into. Mirrors
// internal/harness/claudecode's own helper of the same purpose.
func newSandbox(t *testing.T, dir string) domain.Sandbox {
	t.Helper()

	sb := domain.Sandbox{
		Key:        domain.RunKey{RunID: "opencode-test-run", TestID: "opencode-test", RunNumber: 1},
		Root:       dir,
		SubjectDir: filepath.Join(dir, "subject"),
		ControlDir: filepath.Join(dir, "control"),
	}
	if err := os.MkdirAll(sb.SubjectDir, 0o755); err != nil {
		t.Fatalf("opencode test: create subject dir: %v", err)
	}
	if err := os.MkdirAll(sb.ControlDir, 0o755); err != nil {
		t.Fatalf("opencode test: create control dir: %v", err)
	}
	return sb
}

func testContext() context.Context {
	return context.Background()
}

// fixtureBundleDir returns the absolute path of one of this package's own
// testdata bundle fixtures, by directory name under testdata/opencode/.
// Mirrors internal/harness/claudecode's own helper of the same purpose.
func fixtureBundleDir(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "opencode", name))
	if err != nil {
		t.Fatalf("opencode test: resolving fixture bundle dir: %v", err)
	}
	return abs
}
