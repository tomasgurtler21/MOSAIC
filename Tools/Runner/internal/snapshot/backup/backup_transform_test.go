package backup_test

// Tests for TransformInPlace: in-place frontmatter transformation, idempotency,
// and line-ending preservation.

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/transform"
)

// ---------------------------------------------------------------------------
// T6.4: In-place transformation (TransformInPlace)
// ---------------------------------------------------------------------------

// TestTransformInPlace_AppliesRulesToMatchingFiles verifies that
// TransformInPlace rewrites .md files in agentsDir whose content matches
// the given rules.
func TestTransformInPlace_AppliesRulesToMatchingFiles(t *testing.T) {
	// Arrange
	agentsDir := t.TempDir()
	rules := transform.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n\nBody.\n"))

	// Act
	err := backup.TransformInPlace(agentsDir, rules)

	// Assert
	if err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}
	content := string(readFile(t, filepath.Join(agentsDir, "worker.md")))
	if !strings.Contains(content, "mode: primary") {
		t.Errorf("TransformInPlace: expected mode:primary after transform; got:\n%s", content)
	}
	if strings.Contains(content, "mode: subagent") {
		t.Errorf("TransformInPlace: mode:subagent should have been replaced; got:\n%s", content)
	}
}

// TestTransformInPlace_IsIdempotent verifies that applying TransformInPlace
// twice produces the same file content as applying it once.
func TestTransformInPlace_IsIdempotent(t *testing.T) {
	// Arrange
	agentsDir := t.TempDir()
	rules := transform.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\ntitle: Worker\n---\n\nBody.\n"))

	// Act
	if err := backup.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace (first): %v", err)
	}
	afterFirst := readFile(t, filepath.Join(agentsDir, "worker.md"))

	if err := backup.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace (second): %v", err)
	}
	afterSecond := readFile(t, filepath.Join(agentsDir, "worker.md"))

	// Assert
	if string(afterFirst) != string(afterSecond) {
		t.Errorf(
			"TransformInPlace is not idempotent:\nafter first:  %q\nafter second: %q",
			afterFirst,
			afterSecond,
		)
	}
}

// TestTransformInPlace_FilesWithNoMatchingRuleUnchanged verifies that files
// whose content does not match any rule are not modified by TransformInPlace.
func TestTransformInPlace_FilesWithNoMatchingRuleUnchanged(t *testing.T) {
	// Arrange
	agentsDir := t.TempDir()
	rules := transform.TransformationsFor("opencode")
	content := []byte("---\nmode: primary\ntitle: Orchestrator\n---\n\nBody.\n")
	writeFile(t, filepath.Join(agentsDir, "orchestrator.md"), content)

	// Act
	if err := backup.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}

	// Assert
	got := readFile(t, filepath.Join(agentsDir, "orchestrator.md"))
	if string(got) != string(content) {
		t.Errorf(
			"TransformInPlace modified a file with no matching rule:\ngot:  %q\nwant: %q",
			got,
			content,
		)
	}
}

// TestTransformInPlace_PreservesCRLFLineEndings verifies that CRLF line
// endings are preserved after in-place transformation.
func TestTransformInPlace_PreservesCRLFLineEndings(t *testing.T) {
	// Arrange
	agentsDir := t.TempDir()
	rules := transform.TransformationsFor("opencode")
	original := []byte("---\r\nmode: subagent\r\ntitle: Worker\r\n---\r\n\r\nBody.\r\n")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), original)

	// Act
	if err := backup.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}

	// Assert
	got := string(readFile(t, filepath.Join(agentsDir, "worker.md")))
	if !strings.Contains(got, "mode: primary\r\n") {
		t.Errorf("TransformInPlace: CRLF endings not preserved on transformed line; got: %q", got)
	}
	// The file must not have lost its \r on any line.
	if strings.Contains(got, "title: Worker\n") && !strings.Contains(got, "title: Worker\r\n") {
		t.Errorf("TransformInPlace: CRLF file line endings converted to LF; got: %q", got)
	}
}

// TestTransformInPlace_PreservesLFLineEndings verifies that LF-only files
// are not given \r bytes during in-place transformation.
func TestTransformInPlace_PreservesLFLineEndings(t *testing.T) {
	// Arrange
	agentsDir := t.TempDir()
	rules := transform.TransformationsFor("opencode")
	original := []byte("---\nmode: subagent\ntitle: Worker\n---\n\nBody.\n")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), original)

	// Act
	if err := backup.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("TransformInPlace: %v", err)
	}

	// Assert
	got := readFile(t, filepath.Join(agentsDir, "worker.md"))
	if strings.Contains(string(got), "\r") {
		t.Errorf("TransformInPlace: LF file gained \\r bytes; got: %q", got)
	}
	if !strings.Contains(string(got), "mode: primary\n") {
		t.Errorf("TransformInPlace: expected mode:primary in LF file; got: %q", got)
	}
}
