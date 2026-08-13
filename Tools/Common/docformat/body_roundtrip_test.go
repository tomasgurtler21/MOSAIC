package docformat_test

// Tests for byte-exact round-trip fidelity after body parsing (T4.2).
//
// The existing TestRoundTrip_AllRepositoryMarkdownFiles test (roundtrip_test.go) verifies
// that doc.Bytes() reproduces the source without calling any Body methods. These tests
// add the critical step of accessing the body tree before checking Bytes(), ensuring that
// the body parser's internal state does not disturb the serialised output.
//
// Coverage:
//   - doc.Bytes() is byte-identical to the source after Body().Sections() is called.
//   - doc.Bytes() is byte-identical to the source for a generic agent with boundary tags.
//   - doc.Bytes() is byte-identical to the source for the orchestrator (with AvailableWorkflows injection).
//   - doc.Bytes() is byte-identical to the source for a deployed agent with filled injections.
//   - doc.Bytes() is byte-identical to the source for a workflow file with a compound section name.
//   - Content that appears outside any boundary tag in a workflow file survives verbatim.
//   - Walking all repository .md files: accessing Body().Sections() then Bytes() returns
//     the original bytes (repository-wide regression check).

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-common/docformat"
)

// triggerBodyParse accesses Body().Sections() to force the body parser to run, if parsing
// is lazy. This is the key step that distinguishes these tests from the existing round-trip
// tests: they ensure the body tree does not corrupt the serialised output.
func triggerBodyParse(t *testing.T, doc *docformat.Document) {
	t.Helper()
	// Calling Sections() returns (potentially empty) results and is sufficient to force any
	// body tree construction. The result is intentionally discarded here; individual tests
	// inspect the tree through separate calls.
	_ = doc.Body().Sections()
}

// --- Named reference files ---

func TestBodyRoundTrip_GenericAgent_ByteIdentical(t *testing.T) {
	// Generic agent with all seven sections and empty injections.
	fpath := filepath.Join(repoRoot(), "Catalog", "Subagents", "Execution", "test-runner.md")
	src, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	triggerBodyParse(t, doc)

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip failed after body access (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

func TestBodyRoundTrip_Orchestrator_ByteIdentical(t *testing.T) {
	// Orchestrator contains [[INJECTION:AvailableWorkflows]] inside the Identity section.
	fpath := filepath.Join(repoRoot(), "Catalog", "Orchestrator", "orchestrator.md")
	src, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	triggerBodyParse(t, doc)

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip failed after body access (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

func TestBodyRoundTrip_DeployedAgentWithFilledInjections_ByteIdentical(t *testing.T) {
	// A document with a filled injection — the case the update flow must lift content from.
	// Uses the filled-injection boundary fixture as a stand-in because no harness-specific
	// deployed file with this structure exists at a stable catalog path. The round-trip
	// property under test is identical: body parsing must not disturb the serialised output.
	src := boundaryFixtureBytes(t, "filled-injection.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	triggerBodyParse(t, doc)

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip failed for document with filled injection (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

func TestBodyRoundTrip_WorkflowFileWithCompoundSection_ByteIdentical(t *testing.T) {
	// Workflow file contains [[SECTION:Workflow:quick-fix]] — a compound section name.
	// The content after [[/SECTION:Workflow:quick-fix]] is outside any boundary tag.
	// Both must survive the round trip byte-for-byte.
	fpath := filepath.Join(repoRoot(), "Catalog", "Workflows", "Build", "quick-fix.md")
	src, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	triggerBodyParse(t, doc)

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip failed for workflow file with compound section (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

func TestBodyRoundTrip_ContentOutsideBoundary_PreservedVerbatim(t *testing.T) {
	// The compound-section fixture has content after the closing section tag.
	// That content must survive the round trip byte-for-byte.
	doc := parsedBoundaryFixture(t, "compound-section.md")
	src := boundaryFixtureBytes(t, "compound-section.md")
	triggerBodyParse(t, doc)

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("content outside boundary changed after round-trip (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
	if !bytes.Contains(got, []byte("Content outside any boundary.")) {
		t.Error("out-of-boundary content not found in round-tripped output")
	}
}

// --- Repository-wide body round-trip ---

func TestBodyRoundTrip_AllRepositoryMarkdownFiles_ByteIdentical(t *testing.T) {
	// Walk every .md file in the repository. For each, parse it, access the body to
	// trigger any lazy body parser, then verify that Bytes() reproduces the original.
	// This is the primary regression guard for AC4.1.
	root := repoRoot()

	var mdFiles []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			// Skip fixture files that are expected to fail Parse (contain "malformed" in
			// the path), since those tests live in separate test functions.
			if strings.Contains(strings.ToLower(filepath.ToSlash(path)), "malformed") {
				return nil
			}
			mdFiles = append(mdFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk repository: %v", err)
	}
	if len(mdFiles) == 0 {
		t.Fatalf("no .md files found under %s", root)
	}

	for _, fpath := range mdFiles {
		fpath := fpath
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

			// Trigger body parsing — this is the step the existing round-trip test skips.
			_ = doc.Body().Sections()

			got := doc.Bytes()
			if !bytes.Equal(src, got) {
				t.Errorf("round-trip failed after body access (original=%d bytes, got=%d bytes)", len(src), len(got))
				reportFirstDifference(t, src, got)
			}
		})
	}
}
