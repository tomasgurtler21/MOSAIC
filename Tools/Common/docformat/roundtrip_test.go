package docformat_test

// Tests for byte-exact round-trip fidelity (T3.3).
//
// Parse then Bytes must reproduce the original bytes exactly for every .md file in the
// repository. This test walks the entire repository tree so no file with frontmatter can be
// missed by a hand-curated sample list.
//
// Individual named subtests make failures easy to identify by file path.

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-common/docformat"
)

// repoRoot returns the MOSAIC repository root relative to this package's working directory.
// Tests run with the working directory set to the package directory (Common/docformat).
func repoRoot() string {
	return filepath.Join("..", "..", "..")
}

func TestRoundTrip_AllRepositoryMarkdownFiles(t *testing.T) {
	root := repoRoot()

	var mdFiles []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip directories that will never contain MOSAIC-format files.
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return filepath.SkipDir
			}
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			// Skip intentionally malformed fixture files. These files are required to
			// cause Parse errors (tested in malformed_test.go), so including them in
			// the round-trip walk would directly contradict that contract.
			if strings.Contains(strings.ToLower(filepath.ToSlash(path)), "malformed") {
				return nil
			}
			mdFiles = append(mdFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk repository at %s: %v", root, err)
	}
	if len(mdFiles) == 0 {
		t.Fatalf("no .md files found under %s — check the relative path from the test package", root)
	}

	// Track how many files have frontmatter to make sure we're actually exercising the parser.
	filesWithFrontmatter := 0

	for _, fpath := range mdFiles {
		fpath := fpath // capture for subtest
		rel, _ := filepath.Rel(root, fpath)
		t.Run(rel, func(t *testing.T) {
			src, err := os.ReadFile(fpath)
			if err != nil {
				t.Fatalf("read file: %v", err)
			}

			doc, err := docformat.Parse(src)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}

			got := doc.Bytes()
			if !bytes.Equal(src, got) {
				t.Errorf("round-trip produced different bytes (original=%d bytes, got=%d bytes)", len(src), len(got))
				reportFirstDifference(t, src, got)
			}

			if doc.Frontmatter().Present() {
				filesWithFrontmatter++
			}
		})
	}

	// Sanity-check that we actually exercised files with frontmatter.
	if filesWithFrontmatter == 0 {
		t.Errorf("none of the %d .md files had frontmatter — something is wrong with the walk or the parser", len(mdFiles))
	}
}

// roundtripReferenceDir holds local copies of representative catalog files used by
// TestRoundTrip_SpecificReferenceFiles. Local copies insulate the test from renames,
// moves, or structural edits to live catalog entries during normal project work.
const roundtripReferenceDir = "../testdata/roundtrip-reference"

// TestRoundTrip_SpecificReferenceFiles verifies round-trip fidelity for a small set of
// representative file shapes: a generic agent, the orchestrator, a workflow file, and a
// skill file. Each covers a distinct frontmatter variety (unquoted scalars, placeholder
// values, quoted scalars with special characters, block lists, etc.).
func TestRoundTrip_SpecificReferenceFiles(t *testing.T) {
	referenceFiles := []struct {
		name string
		desc string
	}{
		// Generic agent: unquoted scalars, flow tools list.
		{"generic-agent.md", "generic agent with unquoted scalars and flow tools list"},
		// Orchestrator: tools: {tool-permissions} placeholder.
		{"orchestrator.md", "orchestrator with tools: {tool-permissions} placeholder"},
		// Workflow: quoted scalars, block lists, values with * and {}.
		{"quick-fix.md", "workflow file with quoted scalars, block lists, and * / {} values"},
		// Skill: minimal frontmatter, long unquoted description with commas and parens.
		{"git-read-commands-skill.md", "skill file with minimal frontmatter and long unquoted description"},
	}

	for _, rf := range referenceFiles {
		rf := rf // capture
		t.Run(rf.name, func(t *testing.T) {
			fpath := filepath.Join(roundtripReferenceDir, rf.name)

			src, err := os.ReadFile(fpath)
			if err != nil {
				t.Fatalf("read reference file %s: %v", rf.name, err)
			}

			doc, err := docformat.Parse(src)
			if err != nil {
				t.Fatalf("Parse(%s): %v", rf.name, err)
			}

			got := doc.Bytes()
			if !bytes.Equal(src, got) {
				t.Errorf("round-trip failed for %s (original=%d bytes, got=%d bytes)", rf.name, len(src), len(got))
				reportFirstDifference(t, src, got)
			}
		})
	}
}

// reportFirstDifference logs the byte offset and context of the first byte that differs.
func reportFirstDifference(t *testing.T, want, got []byte) {
	t.Helper()
	shorter := len(want)
	if len(got) < shorter {
		shorter = len(got)
	}
	for i := range shorter {
		if want[i] != got[i] {
			lo := max(0, i-20)
			hiW := min(i+20, len(want))
			hiG := min(i+20, len(got))
			t.Errorf("first difference at byte %d:\n  want context: %q\n  got  context: %q",
				i, want[lo:hiW], got[lo:hiG])
			return
		}
	}
	// Lengths differ.
	t.Errorf("contents match for the first %d bytes but lengths differ: want %d, got %d", shorter, len(want), len(got))
}
