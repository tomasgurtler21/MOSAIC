package descriptor_test

// TestNoSandboxModeInDescriptorAndContracttest is a static assertion that the Go
// string literal for the sandbox_mode key does not appear in any non-test source
// file under internal/harness/descriptor or internal/harness/contracttest.
//
// sandbox_mode is a Codex-owned key. Its literal form belongs in the Codex module
// package and in the Codex package test (Stage 5). Allowing it to appear as a string
// literal in the descriptor or contracttest packages would couple those packages to one
// harness's internal key vocabulary. This check is the cheapest test that catches the
// leak returning once it has been removed.
//
// Scope: non-test .go files only. Test files (_test.go) in those packages may
// reference sandbox_mode as assertion targets and are deliberately excluded.
//
// This is the grep-level assertion described in ContractsDesign.md Testability Notes.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoSandboxModeInDescriptorAndContracttest(t *testing.T) {
	root := repoRootFromDescriptor(t)

	searchDirs := []string{
		filepath.Join(root, "Tools", "Deployment", "internal", "harness", "descriptor"),
		filepath.Join(root, "Tools", "Deployment", "internal", "harness", "contracttest"),
	}

	// target is the Go string literal form of the key. A match in comments is not caught
	// because comment-only occurrences (e.g. "// e.g. sandbox_mode") do not contain the
	// surrounding double-quote characters. Only string literals in code are detected.
	const target = `"sandbox_mode"`

	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir(%q): %v", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") {
				continue
			}
			if strings.HasSuffix(name, "_test.go") {
				// Test files may reference the key as an assertion target. Only
				// production source files are constrained by this check.
				continue
			}

			filePath := filepath.Join(dir, name)
			contents, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("ReadFile(%q): %v", filePath, err)
			}

			if strings.Contains(string(contents), target) {
				rel, _ := filepath.Rel(root, filePath)
				t.Errorf("file %q contains the Go string literal %s; "+
					"sandbox_mode is a Codex-owned key and must not appear as a "+
					"string literal in the descriptor or contracttest packages; "+
					"the only sanctioned test-side occurrence is in the Codex "+
					"module's own package test (Stage 5); "+
					"if a new harness introduces this key legitimately, remove "+
					"this check and add a deliberate acceptance note",
					filepath.ToSlash(rel), target)
			}
		}
	}
}
