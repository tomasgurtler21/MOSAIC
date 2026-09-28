package manifest_test

// Tests for WriteManifest and ReadManifest: recovery manifest write/read
// and error handling for missing or corrupt manifests.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/manifest"
	"mosaic-run/internal/snapshot/transform"
)

// ---------------------------------------------------------------------------
// T6.2: Recovery manifest (WriteManifest, ReadManifest)
// ---------------------------------------------------------------------------

// TestWriteManifest_CreatesManifestFile verifies that WriteManifest creates
// recovery-manifest.json in the backup directory.
func TestWriteManifest_CreatesManifestFile(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	rules := transform.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))

	// Act
	err := manifest.WriteManifest(backupDir, agentsDir, rules)

	// Assert
	if err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	manifestPath := filepath.Join(backupDir, manifest.ManifestFileName)
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("manifest file not created: %v", err)
	}
}

// TestWriteManifest_DryRunIncludesMatchingFiles verifies that WriteManifest
// computes the file list by dry-running rules: files whose content would be
// transformed appear in the manifest; others do not.
func TestWriteManifest_DryRunIncludesMatchingFiles(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	rules := transform.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	writeFile(t, filepath.Join(agentsDir, "orchestrator.md"), []byte("---\nmode: primary\n---\n"))
	writeFile(t, filepath.Join(agentsDir, "plain.md"), []byte("# No frontmatter\n"))

	if err := manifest.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Act
	manifest, err := manifest.ReadManifest(backupDir)

	// Assert
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	foundWorker := false
	for _, entry := range manifest.Files {
		switch entry.Filename {
		case "worker.md":
			foundWorker = true
		case "orchestrator.md":
			t.Error("orchestrator.md should not appear in manifest (no matching rule content)")
		case "plain.md":
			t.Error("plain.md should not appear in manifest (no matching rule)")
		}
	}
	if !foundWorker {
		t.Error("worker.md (has matching rule) should appear in manifest")
	}
}

// TestWriteManifest_NoFilesMatchRules_WritesEmptyFilesSlice verifies that
// WriteManifest writes a manifest with an empty Files slice when no files
// match any rule. The manifest is still created.
func TestWriteManifest_NoFilesMatchRules_WritesEmptyFilesSlice(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	rules := transform.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "orchestrator.md"), []byte("---\nmode: primary\n---\n"))

	if err := manifest.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Act
	manifest, err := manifest.ReadManifest(backupDir)

	// Assert
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if manifest.Files == nil {
		t.Error("manifest.Files should be an empty slice, not nil")
	}
	if len(manifest.Files) != 0 {
		t.Errorf("expected empty manifest.Files, got %d entries", len(manifest.Files))
	}
}

// TestReadManifest_ReadsBackManifestCorrectly verifies that ReadManifest reads
// and correctly parses a manifest produced by WriteManifest.
func TestReadManifest_ReadsBackManifestCorrectly(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	rules := transform.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	if err := manifest.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Act
	manifest, err := manifest.ReadManifest(backupDir)

	// Assert
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if manifest == nil {
		t.Fatal("ReadManifest returned nil manifest")
	}
	if manifest.Timestamp.IsZero() {
		t.Error("manifest.Timestamp should be non-zero")
	}
	if manifest.AgentsDir != agentsDir {
		t.Errorf("manifest.AgentsDir: got %q, want %q", manifest.AgentsDir, agentsDir)
	}
}

// TestReadManifest_ManifestEntryHasCorrectFields verifies that manifest entries
// include Filename, Field, and OriginalValue for transformed files.
func TestReadManifest_ManifestEntryHasCorrectFields(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	rules := transform.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "worker.md"), []byte("---\nmode: subagent\n---\n"))
	if err := manifest.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Act
	manifest, err := manifest.ReadManifest(backupDir)

	// Assert
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(manifest.Files) == 0 {
		t.Fatal("expected at least one manifest entry for worker.md")
	}
	entry := manifest.Files[0]
	if entry.Filename != "worker.md" {
		t.Errorf("entry.Filename: got %q, want %q", entry.Filename, "worker.md")
	}
	if entry.Field != "mode" {
		t.Errorf("entry.Field: got %q, want %q", entry.Field, "mode")
	}
	if entry.OriginalValue != "subagent" {
		t.Errorf("entry.OriginalValue: got %q, want %q", entry.OriginalValue, "subagent")
	}
}

// TestWriteManifest_LexicographicOrdering verifies that manifest entries are
// ordered by filename in lexicographic order, enabling deterministic byte-for-byte
// comparison in downstream tests.
func TestWriteManifest_LexicographicOrdering(t *testing.T) {
	// Arrange: write two matching files in reverse alphabetical order to
	// confirm ordering is not dependent on filesystem enumeration order.
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	backupDir := filepath.Join(base, backup.BackupDirName)
	mustMkdir(t, agentsDir)
	mustMkdir(t, backupDir)
	rules := transform.TransformationsFor("opencode")
	writeFile(t, filepath.Join(agentsDir, "zebra.md"), []byte("---\nmode: subagent\n---\n"))
	writeFile(t, filepath.Join(agentsDir, "aardvark.md"), []byte("---\nmode: subagent\n---\n"))

	if err := manifest.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Act
	manifest, err := manifest.ReadManifest(backupDir)

	// Assert
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(manifest.Files) < 2 {
		t.Fatalf("expected at least 2 manifest entries, got %d", len(manifest.Files))
	}
	if manifest.Files[0].Filename >= manifest.Files[1].Filename {
		t.Errorf(
			"manifest entries not in lexicographic order: got %q before %q",
			manifest.Files[0].Filename,
			manifest.Files[1].Filename,
		)
	}
}

// TestReadManifest_CorruptManifest_ReturnsManifestCorruptError verifies that
// ReadManifest returns *ManifestError{Kind: ManifestCorrupt} for a corrupt
// manifest file, and that ManifestError.Path holds the path to the manifest.
func TestReadManifest_CorruptManifest_ReturnsManifestCorruptError(t *testing.T) {
	// Arrange
	backupDir := t.TempDir()
	writeFile(t, filepath.Join(backupDir, manifest.ManifestFileName), []byte("{corrupt json{{"))

	// Act
	_, err := manifest.ReadManifest(backupDir)

	// Assert
	if err == nil {
		t.Fatal("expected error for corrupt manifest, got nil")
	}
	var me *manifest.ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("expected *manifest.ManifestError, got %T: %v", err, err)
	}
	if me.Kind != manifest.ManifestCorrupt {
		t.Errorf("ManifestError.Kind: got %v, want ManifestCorrupt", me.Kind)
	}
	wantPath := filepath.Join(backupDir, manifest.ManifestFileName)
	if me.Path != wantPath {
		t.Errorf("ManifestError.Path: got %q, want %q", me.Path, wantPath)
	}
}

// TestReadManifest_TruncatedManifest_ReturnsManifestCorruptError verifies that
// a truncated (incomplete) JSON file also returns ManifestCorrupt, and that
// ManifestError.Path holds the path to the manifest.
func TestReadManifest_TruncatedManifest_ReturnsManifestCorruptError(t *testing.T) {
	// Arrange
	backupDir := t.TempDir()
	writeFile(t, filepath.Join(backupDir, manifest.ManifestFileName), []byte(`{"timestamp":`))

	// Act
	_, err := manifest.ReadManifest(backupDir)

	// Assert
	if err == nil {
		t.Fatal("expected error for truncated manifest, got nil")
	}
	var me *manifest.ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("expected *manifest.ManifestError, got %T: %v", err, err)
	}
	if me.Kind != manifest.ManifestCorrupt {
		t.Errorf("ManifestError.Kind: got %v, want ManifestCorrupt", me.Kind)
	}
	wantPath := filepath.Join(backupDir, manifest.ManifestFileName)
	if me.Path != wantPath {
		t.Errorf("ManifestError.Path: got %q, want %q", me.Path, wantPath)
	}
}

// TestReadManifest_MissingManifest_ReturnsManifestMissingError verifies that
// ReadManifest returns *ManifestError{Kind: ManifestMissing} when the manifest
// file does not exist, and that ManifestError.Path holds the expected path.
func TestReadManifest_MissingManifest_ReturnsManifestMissingError(t *testing.T) {
	// Arrange: empty backup directory, no manifest file written.
	backupDir := t.TempDir()

	// Act
	_, err := manifest.ReadManifest(backupDir)

	// Assert
	if err == nil {
		t.Fatal("expected error for missing manifest, got nil")
	}
	var me *manifest.ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("expected *manifest.ManifestError, got %T: %v", err, err)
	}
	if me.Kind != manifest.ManifestMissing {
		t.Errorf("ManifestError.Kind: got %v, want ManifestMissing", me.Kind)
	}
	wantPath := filepath.Join(backupDir, manifest.ManifestFileName)
	if me.Path != wantPath {
		t.Errorf("ManifestError.Path: got %q, want %q", me.Path, wantPath)
	}
}

// TestReadManifest_DistinctErrorKindForMissingVsCorrupt verifies that the
// ManifestError.Kind values are distinct for missing vs corrupt manifest files,
// enabling callers to handle each case differently.
func TestReadManifest_DistinctErrorKindForMissingVsCorrupt(t *testing.T) {
	// Arrange
	dir := t.TempDir()

	// Act: missing
	_, missingErr := manifest.ReadManifest(dir)
	var missingME *manifest.ManifestError
	if !errors.As(missingErr, &missingME) {
		t.Fatalf("missing: expected *manifest.ManifestError, got %T", missingErr)
	}

	// Act: corrupt
	writeFile(t, filepath.Join(dir, manifest.ManifestFileName), []byte("not json"))
	_, corruptErr := manifest.ReadManifest(dir)
	var corruptME *manifest.ManifestError
	if !errors.As(corruptErr, &corruptME) {
		t.Fatalf("corrupt: expected *manifest.ManifestError, got %T", corruptErr)
	}

	// Assert
	if missingME.Kind == corruptME.Kind {
		t.Errorf(
			"missing and corrupt manifest errors must have distinct Kind values, both got %v",
			missingME.Kind,
		)
	}
}
