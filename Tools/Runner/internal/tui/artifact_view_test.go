package tui

// artifact_view_test.go covers readArtifactContent behaviour for Stage 3:
// Orchestration.md Path Correctness.
//
// readArtifactContent is exercised by constructing a rootModel directly inside
// package tui, the same technique used by flow_test.go and navigation_test.go.
//
// Covered:
//
//   Empty runFolder (T3.1):
//   - Returns ArtifactNotYetCreatedMessage (contains "not yet created").
//   - Decoy Orchestration.md placed next to the orchestrator file is never read:
//     its content does not appear in the result.
//
//   Populated runFolder (T3.2):
//   - Existing file at {runFolder}/Orchestration.md: content returned verbatim.
//   - Missing file at {runFolder}/Orchestration.md: returns ArtifactNotYetCreatedMessage.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tuicommon "mosaic-common/tui"
)

// notYetCreatedSubstring is the stable substring that tests assert on.
// The design mandates asserting a substring rather than the full message so
// that wording can be polished without breaking tests.
const notYetCreatedSubstring = "not yet created"

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newArtifactViewModel creates a minimal rootModel for readArtifactContent tests.
// sess is a stub so the model satisfies the type; only selections are exercised.
func newArtifactViewModel() *rootModel {
	sess := &stubFlowSession{}
	return newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
	})
}

// ---------------------------------------------------------------------------
// T3.1 — empty runFolder
// ---------------------------------------------------------------------------

// TestReadArtifactContent_EmptyRunFolder_ReturnsNotYetCreatedMessage verifies
// that when selections.runFolder is empty, readArtifactContent returns a message
// containing "not yet created" rather than guessing a path.
func TestReadArtifactContent_EmptyRunFolder_ReturnsNotYetCreatedMessage(t *testing.T) {
	m := newArtifactViewModel()
	m.selections.runFolder = ""

	result := m.readArtifactContent()

	if !strings.Contains(result, notYetCreatedSubstring) {
		t.Errorf("readArtifactContent with empty runFolder = %q; want substring %q",
			result, notYetCreatedSubstring)
	}
}

// TestReadArtifactContent_EmptyRunFolder_NeverReadsDecoyFile verifies that when
// selections.runFolder is empty, readArtifactContent does not fall back to the
// orchestrator file's directory. A decoy Orchestration.md is placed next to the
// orchestrator file; the decoy's content must not appear in the result.
func TestReadArtifactContent_EmptyRunFolder_NeverReadsDecoyFile(t *testing.T) {
	// Place a decoy Orchestration.md next to a fake orchestrator file.
	dir := t.TempDir()
	decoyContent := "DECOY_CONTENT_MUST_NOT_APPEAR_IN_RESULT"
	decoyPath := filepath.Join(dir, "Orchestration.md")
	if err := os.WriteFile(decoyPath, []byte(decoyContent), 0644); err != nil {
		t.Fatalf("writing decoy file: %v", err)
	}

	m := newArtifactViewModel()
	m.selections.runFolder = ""
	// Point the orchestratorFile into the same directory as the decoy.
	m.selections.orchestratorFile = filepath.Join(dir, "workflow.md")

	result := m.readArtifactContent()

	// The decoy's distinctive content must not be present.
	if strings.Contains(result, decoyContent) {
		t.Errorf("readArtifactContent returned decoy content %q; orchestrator directory must never be consulted", decoyContent)
	}
	// The result must still be the not-yet-created message.
	if !strings.Contains(result, notYetCreatedSubstring) {
		t.Errorf("readArtifactContent with empty runFolder = %q; want substring %q",
			result, notYetCreatedSubstring)
	}
}

// ---------------------------------------------------------------------------
// T3.2 — populated runFolder
// ---------------------------------------------------------------------------

// TestReadArtifactContent_PopulatedRunFolder_ExistingFile_ReturnsContent verifies
// that when runFolder is set and {runFolder}/Orchestration.md exists, its content
// is returned verbatim.
func TestReadArtifactContent_PopulatedRunFolder_ExistingFile_ReturnsContent(t *testing.T) {
	runFolder := t.TempDir()
	wantContent := "# Orchestration artifact content for test"
	artPath := filepath.Join(runFolder, "Orchestration.md")
	if err := os.WriteFile(artPath, []byte(wantContent), 0644); err != nil {
		t.Fatalf("writing artifact file: %v", err)
	}

	m := newArtifactViewModel()
	m.selections.runFolder = runFolder

	result := m.readArtifactContent()

	if result != wantContent {
		t.Errorf("readArtifactContent = %q, want %q", result, wantContent)
	}
}

// TestReadArtifactContent_PopulatedRunFolder_MissingFile_ReturnsNotYetCreatedMessage
// verifies that when runFolder is set but {runFolder}/Orchestration.md does not
// exist, readArtifactContent returns a message containing "not yet created" rather
// than a raw OS error string.
func TestReadArtifactContent_PopulatedRunFolder_MissingFile_ReturnsNotYetCreatedMessage(t *testing.T) {
	// The run folder exists but Orchestration.md was never created inside it
	// (simulates a run refused before the artifact store was initialised).
	runFolder := t.TempDir()

	m := newArtifactViewModel()
	m.selections.runFolder = runFolder

	result := m.readArtifactContent()

	if !strings.Contains(result, notYetCreatedSubstring) {
		t.Errorf("readArtifactContent with missing artifact = %q; want substring %q",
			result, notYetCreatedSubstring)
	}
}
