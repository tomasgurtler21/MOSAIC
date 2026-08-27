package snapshot_test

// Tests for CreateSnapshot.
//
// Coverage:
//
//   Happy path - flat copy:
//   - A flat source directory of .md files is reproduced exactly in the target
//     directory (same filenames, same file count).
//   - File contents in the target match the source (no transformations applied
//     when rules is nil).
//   - Subdirectories in the source are not copied to the target (flat-only semantics).
//   - Non-.md files in the source are not copied (agentresolve convention).
//
//   Happy path - copy with transformation:
//   - After CreateSnapshot with opencode rules, every .md file in the target
//     that originally had "mode: subagent" now has "mode: primary".
//   - Files without "mode: subagent" in the source are copied byte-for-byte.
//   - Files without any frontmatter are copied byte-for-byte.
//
//   Failure cases:
//   - Source directory does not exist: returns *domain.RefusalError with
//     Component "snapshot".
//   - Target directory already exists: returns *domain.RefusalError with
//     Component "snapshot" (collision guard).
//   - On error, CreateSnapshot performs best-effort cleanup of a partially
//     created target directory.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/snapshot"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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

// assertRefusalError asserts that err is a *domain.RefusalError with the given
// Component value.
func assertRefusalError(t *testing.T, err error, wantComponent string) {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("expected *domain.RefusalError, got %T: %v", err, err)
	}
	if re.Component != wantComponent {
		t.Errorf("RefusalError.Component: got %q, want %q", re.Component, wantComponent)
	}
}

// ---------------------------------------------------------------------------
// Flat copy (no transformations)
// ---------------------------------------------------------------------------

func TestCreateSnapshot_CopiesAllMdFiles(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "snapshot")

	writeFile(t, filepath.Join(src, "agent-a.md"), []byte("# Agent A\n"))
	writeFile(t, filepath.Join(src, "agent-b.md"), []byte("# Agent B\n"))
	writeFile(t, filepath.Join(src, "agent-c.md"), []byte("# Agent C\n"))

	if err := snapshot.CreateSnapshot(src, dst, nil); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	for _, name := range []string{"agent-a.md", "agent-b.md", "agent-c.md"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Errorf("expected file %q in snapshot, got: %v", name, err)
		}
	}
}

func TestCreateSnapshot_FileContentsMatchSource(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "snapshot")

	content := []byte("---\ntitle: My Agent\n---\n\nBody content.\n")
	writeFile(t, filepath.Join(src, "my-agent.md"), content)

	if err := snapshot.CreateSnapshot(src, dst, nil); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	got := readFile(t, filepath.Join(dst, "my-agent.md"))
	if string(got) != string(content) {
		t.Errorf("file content mismatch:\ngot:  %q\nwant: %q", got, content)
	}
}

func TestCreateSnapshot_SubdirectoriesAreNotCopied(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "snapshot")

	// Create a subdirectory in src with a file inside.
	subdir := filepath.Join(src, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	writeFile(t, filepath.Join(subdir, "nested.md"), []byte("# Nested\n"))
	writeFile(t, filepath.Join(src, "top-level.md"), []byte("# Top\n"))

	if err := snapshot.CreateSnapshot(src, dst, nil); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	// The subdirectory itself should not appear in the snapshot.
	if _, err := os.Stat(filepath.Join(dst, "subdir")); !os.IsNotExist(err) {
		t.Error("expected subdir to be absent from snapshot, but it was found")
	}
	// The top-level file should be there.
	if _, err := os.Stat(filepath.Join(dst, "top-level.md")); err != nil {
		t.Errorf("expected top-level.md in snapshot: %v", err)
	}
}

func TestCreateSnapshot_NonMdFilesAreNotCopied(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "snapshot")

	writeFile(t, filepath.Join(src, "agent.md"), []byte("# Agent\n"))
	writeFile(t, filepath.Join(src, "README.txt"), []byte("readme\n"))
	writeFile(t, filepath.Join(src, "config.yaml"), []byte("key: value\n"))

	if err := snapshot.CreateSnapshot(src, dst, nil); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	// Only .md files should be copied.
	if _, err := os.Stat(filepath.Join(dst, "README.txt")); !os.IsNotExist(err) {
		t.Error("expected README.txt to be absent from snapshot")
	}
	if _, err := os.Stat(filepath.Join(dst, "config.yaml")); !os.IsNotExist(err) {
		t.Error("expected config.yaml to be absent from snapshot")
	}
	if _, err := os.Stat(filepath.Join(dst, "agent.md")); err != nil {
		t.Errorf("expected agent.md in snapshot: %v", err)
	}
}

func TestCreateSnapshot_EmptySourceDir_ProducesEmptySnapshot(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "snapshot")

	if err := snapshot.CreateSnapshot(src, dst, nil); err != nil {
		t.Fatalf("CreateSnapshot on empty source: %v", err)
	}

	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatalf("ReadDir snapshot: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty snapshot dir, got %d entries", len(entries))
	}
}

// ---------------------------------------------------------------------------
// Copy with transformation (opencode rules)
// ---------------------------------------------------------------------------

func TestCreateSnapshot_AppliesTransformationToSubagentFiles(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "snapshot")

	rules := snapshot.TransformationsFor("opencode")
	content := []byte("---\nmode: subagent\ntitle: Worker\n---\n\nBody.\n")
	writeFile(t, filepath.Join(src, "worker.md"), content)

	if err := snapshot.CreateSnapshot(src, dst, rules); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	got := readFile(t, filepath.Join(dst, "worker.md"))
	if !strings.Contains(string(got), "mode: primary") {
		t.Errorf("expected 'mode: primary' in snapshot file; got:\n%s", got)
	}
	if strings.Contains(string(got), "mode: subagent") {
		t.Errorf("expected 'mode: subagent' to be replaced; got:\n%s", got)
	}
}

func TestCreateSnapshot_FilesWithoutSubagentModeAreCopiedUnchanged(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "snapshot")

	rules := snapshot.TransformationsFor("opencode")
	content := []byte("---\nmode: primary\ntitle: Orchestrator\n---\n\nBody.\n")
	writeFile(t, filepath.Join(src, "orchestrator.md"), content)

	if err := snapshot.CreateSnapshot(src, dst, rules); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	got := readFile(t, filepath.Join(dst, "orchestrator.md"))
	if string(got) != string(content) {
		t.Errorf("file without mode:subagent was modified:\ngot:  %q\nwant: %q", got, content)
	}
}

func TestCreateSnapshot_FilesWithoutFrontmatterAreCopiedUnchanged(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "snapshot")

	rules := snapshot.TransformationsFor("opencode")
	content := []byte("# No Frontmatter Agent\n\nJust markdown, no YAML block.\n")
	writeFile(t, filepath.Join(src, "no-fm.md"), content)

	if err := snapshot.CreateSnapshot(src, dst, rules); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	got := readFile(t, filepath.Join(dst, "no-fm.md"))
	if string(got) != string(content) {
		t.Errorf("file without frontmatter was modified:\ngot:  %q\nwant: %q", got, content)
	}
}

func TestCreateSnapshot_MixedFiles_OnlySubagentFilesTransformed(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "snapshot")

	rules := snapshot.TransformationsFor("opencode")
	subagentContent := []byte("---\nmode: subagent\n---\n\nWorker body.\n")
	primaryContent := []byte("---\nmode: primary\n---\n\nOrchestrator body.\n")
	noFmContent := []byte("# Plain markdown\n")

	writeFile(t, filepath.Join(src, "worker.md"), subagentContent)
	writeFile(t, filepath.Join(src, "orchestrator.md"), primaryContent)
	writeFile(t, filepath.Join(src, "plain.md"), noFmContent)

	if err := snapshot.CreateSnapshot(src, dst, rules); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	workerGot := readFile(t, filepath.Join(dst, "worker.md"))
	if !strings.Contains(string(workerGot), "mode: primary") {
		t.Errorf("worker.md: expected mode:primary after transform; got:\n%s", workerGot)
	}

	orchGot := readFile(t, filepath.Join(dst, "orchestrator.md"))
	if string(orchGot) != string(primaryContent) {
		t.Errorf("orchestrator.md: content changed unexpectedly; got:\n%s", orchGot)
	}

	plainGot := readFile(t, filepath.Join(dst, "plain.md"))
	if string(plainGot) != string(noFmContent) {
		t.Errorf("plain.md: content changed unexpectedly; got:\n%s", plainGot)
	}
}

// ---------------------------------------------------------------------------
// Failure cases
// ---------------------------------------------------------------------------

func TestCreateSnapshot_SourceDirNotExist_ReturnsRefusalError(t *testing.T) {
	src := filepath.Join(t.TempDir(), "does-not-exist")
	dst := filepath.Join(t.TempDir(), "snapshot")

	err := snapshot.CreateSnapshot(src, dst, nil)
	if err == nil {
		t.Fatal("expected error for non-existent source dir, got nil")
	}
	assertRefusalError(t, err, "snapshot")
}

func TestCreateSnapshot_TargetAlreadyExists_ReturnsRefusalError(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir() // already exists

	writeFile(t, filepath.Join(src, "agent.md"), []byte("# Agent\n"))

	err := snapshot.CreateSnapshot(src, dst, nil)
	if err == nil {
		t.Fatal("expected error when target dir already exists, got nil")
	}
	assertRefusalError(t, err, "snapshot")
}

func TestCreateSnapshot_SourceDirNotExist_TargetNotCreated(t *testing.T) {
	// When source does not exist, CreateSnapshot must not leave a partial
	// target directory behind.
	base := t.TempDir()
	src := filepath.Join(base, "missing-src")
	dst := filepath.Join(base, "snapshot")

	_ = snapshot.CreateSnapshot(src, dst, nil)

	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("expected snapshot dir to be absent after source-not-found error")
	}
}
